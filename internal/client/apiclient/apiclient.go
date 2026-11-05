// Package apiclient calls the koko server's API operations over api@koko
// channels. HTTP/1.1 keep-alive lets one channel carry a command's calls in
// turn; a call made while another is in flight opens one more channel.
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

	"github.com/cieons/koko/internal/client/serverconn"
	"github.com/cieons/koko/internal/shared/api"
	"github.com/cieons/koko/internal/shared/wire"
)

// ErrUnreachable marks a call the connection could not carry: no channel
// opened, or the connection dropped before the answer came.
var ErrUnreachable = errors.New("the koko server did not answer")

// Client calls operations on one connection to the koko server.
type Client struct {
	transport *http.Transport
	http      *http.Client
}

// New returns a Client whose calls go through conn.
func New(conn serverconn.Conn) *Client {
	t := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			ch, err := conn.OpenChannel(ctx, wire.ChannelAPI)
			if err != nil {
				return nil, err
			}
			return wire.Conn(ch), nil
		},
		DisableCompression: true,
	}
	return &Client{transport: t, http: &http.Client{Transport: t}}
}

// Call calls op with req. A failure the koko server answered is an
// *api.Error with its code; response fields this CLI does not know are
// ignored.
func (c *Client) Call[Req, Resp any](ctx context.Context, op api.Op[Req, Resp], req Req) (Resp, error) {
	var resp Resp
	body, err := json.Marshal(req)
	if err != nil {
		return resp, fmt.Errorf("encode %s request: %w", op.Name, err)
	}
	// The host is never dialled: every request goes into an api@koko channel.
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://koko"+op.Path(), bytes.NewReader(body))
	if err != nil {
		return resp, fmt.Errorf("call %s: %w", op.Name, err)
	}
	hreq.Header.Set("Content-Type", "application/json")
	hresp, err := c.http.Do(hreq)
	if err != nil {
		return resp, fmt.Errorf("call %s: %w: %w", op.Name, ErrUnreachable, err)
	}
	defer func() { _ = hresp.Body.Close() }()
	b, err := io.ReadAll(hresp.Body)
	if err != nil {
		return resp, fmt.Errorf("read %s response: %w: %w", op.Name, ErrUnreachable, err)
	}
	if hresp.StatusCode != http.StatusOK {
		var e api.ErrorResponse
		if err := json.Unmarshal(b, &e); err != nil || e.Error.Code == "" {
			return resp, fmt.Errorf("call %s: the koko server answered %s without an error object", op.Name, hresp.Status)
		}
		return resp, &e.Error
	}
	if err := json.Unmarshal(b, &resp); err != nil {
		return resp, fmt.Errorf("decode %s response: %w", op.Name, err)
	}
	return resp, nil
}

// Close closes the channels kept open between calls.
func (c *Client) Close() {
	c.transport.CloseIdleConnections()
}
