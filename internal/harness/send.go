package harness

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"testing"

	"github.com/cieons/koko/internal/client/devicefiles"
	"github.com/cieons/koko/internal/client/serverconn"
	"github.com/cieons/koko/internal/shared/api"
	"github.com/cieons/koko/internal/shared/wire"
)

// Send calls the operation o, with body as its JSON request, over the Unix
// socket as the Local caller, and returns the HTTP status and the response
// body as the koko server wrote them: for a test that calls as no command
// does, or compares answers byte for byte. The CLI's API client decodes what
// it reads, so this one keeps the bytes.
func (h *Harness) Send(o api.Operation, body []byte) (int, []byte) {
	h.t.Helper()
	conn, err := serverconn.DialSocket(h.t.Context(), h.socket)
	if err != nil {
		h.t.Fatalf("reach the koko server over its Unix socket: %v", err)
	}
	defer func() { _ = conn.Close() }()
	return send(h.t, conn, o, body)
}

// Send calls the operation o, as Harness.Send does, on a Device Connection
// of the device, as its Device Session.
func (d *Device) Send(o api.Operation, body []byte) (int, []byte) {
	d.t.Helper()
	c, key, err := devicefiles.Read(d.home)
	if err != nil {
		d.t.Fatalf("read the device's join: %v", err)
	}
	conn, err := serverconn.DialDevice(d.t.Context(), serverconn.Device{Address: c.ServerAddress, HostKeys: c.HostKeys, Key: key})
	if err != nil {
		d.t.Fatalf("reach the koko server as %s's %s: %v", c.User, c.Device, err)
	}
	defer func() { _ = conn.Close() }()
	return send(d.t, conn, o, body)
}

// send posts body to the operation o in one api@koko channel of conn, a
// write with an Idempotency-Key of its own, as the CLI sends it.
func send(t testing.TB, conn serverconn.Conn, o api.Operation, body []byte) (int, []byte) {
	t.Helper()
	name := o.Info().Name
	tr := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			ch, err := conn.OpenChannel(ctx, wire.ChannelAPI, nil)
			if err != nil {
				return nil, err
			}
			return wire.Conn(ch), nil
		},
		DisableCompression: true,
	}
	defer tr.CloseIdleConnections()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "http://koko"+api.PathPrefix+name, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("call %s: %v", name, err)
	}
	req.Header.Set("Content-Type", "application/json")
	if o.Info().Write {
		req.Header.Set(api.IdempotencyKeyHeader, api.NewIdempotencyKey())
	}
	resp, err := (&http.Client{Transport: tr}).Do(req)
	if err != nil {
		t.Fatalf("call %s: %v", name, err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read the answer to %s: %v", name, err)
	}
	return resp.StatusCode, b
}
