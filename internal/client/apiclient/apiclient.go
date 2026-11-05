// Package apiclient calls the koko server's API operations over api@koko
// channels. HTTP/1.1 keep-alive lets one channel carry a command's calls in
// turn; a call made while another is in flight opens one more channel.
//
// Before each call it checks the koko server's version and operations
// report, and sends no operation the koko server lacks. Every write carries
// an Idempotency-Key, and a call the connection drops is sent once more, on
// a new connection, with the same key, so a write applies once and a
// command never reports an unknown outcome (P0 spec: reach).
package apiclient

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"slices"
	"sync"

	"github.com/cieons/koko/internal/client/serverconn"
	"github.com/cieons/koko/internal/shared/api"
	"github.com/cieons/koko/internal/shared/wire"
)

// ErrUnreachable marks a call the connection could not carry, even when
// sent again: no channel opened, or the connection dropped before the
// answer came.
var ErrUnreachable = errors.New("the koko server did not answer")

// MissingOperationError says the koko server does not report an operation
// this CLI calls: it is older than the CLI. Nothing was sent.
type MissingOperationError struct {
	// Operation is the operation's name, such as "space.add".
	Operation string
	// ServerVersion is the koko server's version, as it reported it.
	ServerVersion string
}

func (e *MissingOperationError) Error() string {
	return fmt.Sprintf("the koko server runs koko %s, which has no %s", e.ServerVersion, e.Operation)
}

// Dial opens a connection to the koko server. A Client dials on its first
// call, and again after a drop.
type Dial func(ctx context.Context) (serverconn.Conn, error)

// Client calls operations on a connection to the koko server it dials.
type Client struct {
	dial Dial

	mu sync.Mutex
	// conn is the connection calls go through, nil until the first call
	// and after a drop.
	conn *conn
}

// conn is one connection to the koko server and the HTTP client whose
// requests go into its api@koko channels.
type conn struct {
	serverconn.Conn
	transport *http.Transport
	http      *http.Client
}

// New returns a Client whose calls go through the connections dial opens.
func New(dial Dial) *Client {
	return &Client{dial: dial}
}

// Conn returns the connection the client's calls go through, dialling it
// if there is none, for a command that opens channels of its own beside its
// calls, as koko web does.
func (c *Client) Conn(ctx context.Context) (serverconn.Conn, error) {
	cc, err := c.current(ctx)
	if err != nil {
		return nil, err
	}
	return cc.Conn, nil
}

// current returns the connection calls go through, dialling one if there
// is none. A dial's error is returned as it is.
func (c *Client) current(ctx context.Context) (*conn, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		return c.conn, nil
	}
	sc, err := c.dial(ctx)
	if err != nil {
		return nil, err
	}
	t := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			ch, err := sc.OpenChannel(ctx, wire.ChannelAPI, nil)
			if err != nil {
				return nil, err
			}
			return wire.Conn(ch), nil
		},
		DisableCompression: true,
	}
	c.conn = &conn{Conn: sc, transport: t, http: &http.Client{Transport: t}}
	return c.conn, nil
}

// drop closes cc after it dropped a call, so the next call dials anew.
func (c *Client) drop(cc *conn) {
	c.mu.Lock()
	if c.conn == cc {
		c.conn = nil
	}
	c.mu.Unlock()
	cc.close()
}

func (cc *conn) close() {
	cc.transport.CloseIdleConnections()
	_ = cc.Close()
}

// Call calls op with req. A failure the koko server answered is an
// *api.Error with its code; response fields this CLI does not know are
// ignored. An operation the koko server does not report is not sent, and
// fails with a *MissingOperationError. A call the connection drops is sent
// once more on a new connection, a write with the same Idempotency-Key, and
// fails wrapping ErrUnreachable when that one drops too.
func (c *Client) Call[Req, Resp any](ctx context.Context, op api.Op[Req, Resp], req Req) (Resp, error) {
	var resp Resp
	body, err := json.Marshal(req)
	if err != nil {
		return resp, fmt.Errorf("encode %s request: %w", op.Name, err)
	}
	var key string
	if op.Write {
		key = api.NewIdempotencyKey()
	}
	b, err := c.send(ctx, op.Name, op.Path(), body, key)
	if errors.Is(err, ErrUnreachable) && ctx.Err() == nil {
		b, err = c.send(ctx, op.Name, op.Path(), body, key)
	}
	if err != nil {
		return resp, err
	}
	if err := json.Unmarshal(b, &resp); err != nil {
		return resp, fmt.Errorf("decode %s response: %w", op.Name, err)
	}
	return resp, nil
}

// send makes one attempt at a call of the operation name, at path, with
// body, carrying key when it is not "", and returns the response's body. It
// drops the connection when the call fails wrapping ErrUnreachable.
func (c *Client) send(ctx context.Context, name, path string, body []byte, key string) ([]byte, error) {
	cc, err := c.current(ctx)
	if err != nil {
		return nil, err
	}
	report, err := cc.Report(ctx)
	if err != nil {
		c.drop(cc)
		return nil, fmt.Errorf("call %s: %w: %w", name, ErrUnreachable, err)
	}
	if !slices.Contains(report.Operations, name) {
		return nil, &MissingOperationError{Operation: name, ServerVersion: report.Version}
	}
	// The host is never dialled: every request goes into an api@koko channel.
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://koko"+path, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("call %s: %w", name, err)
	}
	hreq.Header.Set("Content-Type", "application/json")
	if key != "" {
		hreq.Header.Set(api.IdempotencyKeyHeader, key)
	}
	hresp, err := cc.http.Do(hreq)
	if err != nil {
		c.drop(cc)
		return nil, fmt.Errorf("call %s: %w: %w", name, ErrUnreachable, err)
	}
	defer func() { _ = hresp.Body.Close() }()
	b, err := io.ReadAll(hresp.Body)
	if err != nil {
		c.drop(cc)
		return nil, fmt.Errorf("read %s response: %w: %w", name, ErrUnreachable, err)
	}
	if hresp.StatusCode != http.StatusOK {
		var e api.ErrorResponse
		if err := json.Unmarshal(b, &e); err != nil || e.Error.Code == "" {
			return nil, fmt.Errorf("call %s: the koko server answered %s without an error object", name, hresp.Status)
		}
		return nil, &e.Error
	}
	return b, nil
}

// Close closes the connection calls go through, and the channels kept open
// between calls.
func (c *Client) Close() {
	c.mu.Lock()
	cc := c.conn
	c.conn = nil
	c.mu.Unlock()
	if cc != nil {
		cc.close()
	}
}
