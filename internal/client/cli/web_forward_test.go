package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/cieons/koko/internal/client/serverconn"
	"github.com/cieons/koko/internal/shared/timing"
	"github.com/cieons/koko/internal/shared/wire"
)

// What the koko server sent on a browser connection's channel before the
// connection to it ended, such as the answer to a call in flight when the
// Device Session was Revoked, reaches the browser in full before koko web
// closes it. The answer is far longer than the loopback buffers hold, so
// most of it is still in koko web when the connection ends, and the browser
// reads it only then.
func TestKokoWebPassesOnWhatTheKokoServerSentBeforeTheConnectionEnded(t *testing.T) {
	t.Parallel()
	answer := bytes.Repeat([]byte("answer, "), 16<<20/8)
	conn := newEndingConn(t, answer)
	served, port := serveForwarder(t, conn, timing.PassOn)
	browser := dialBrowser(t, port)
	waitForClose(t, conn.opened, "the browser connection's channel to open")

	conn.end()
	got, err := io.ReadAll(browser)
	if !bytes.Equal(got, answer) {
		t.Errorf("the browser got %d bytes of the koko server's %d-byte answer before its connection closed (%v)", len(got), len(answer), err)
	}
	wantRevoked(t, served)
}

// A browser that does not read what the koko server sent before the
// connection to it ended keeps koko web from ending no longer than
// timing.PassOn: here nothing but passOn ends the wait, as the browser
// never reads the answer and its channel never ends it.
func TestABrowserThatDoesNotReadKeepsKokoWebForPassOnAtMost(t *testing.T) {
	t.Parallel()
	conn := newEndingConn(t, bytes.Repeat([]byte("answer, "), 16<<20/8))
	served, port := serveForwarder(t, conn, 100*time.Millisecond)
	dialBrowser(t, port)
	waitForClose(t, conn.opened, "the browser connection's channel to open")

	conn.end()
	wantRevoked(t, served)
}

// serveForwarder runs koko web's forwarder over conn on a port of its own,
// letting each browser connection pass on what the koko server sent for
// passOn once conn ends, and returns what serve returns and the port.
func serveForwarder(t *testing.T, conn serverconn.Conn, passOn time.Duration) (<-chan error, int) {
	t.Helper()
	var lc net.ListenConfig
	ln, err := lc.Listen(t.Context(), "tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, stop := context.WithCancel(t.Context())
	served, done := make(chan error, 1), make(chan struct{})
	f := forwarder{conn: conn, payload: wire.WebPayload(1, 1), stderr: io.Discard, passOn: passOn}
	go func() {
		defer close(done)
		served <- f.serve(ctx, ln)
	}()
	t.Cleanup(func() {
		stop()
		<-done
	})
	return served, ln.Addr().(*net.TCPAddr).Port
}

// dialBrowser opens a browser connection to koko web on port.
func dialBrowser(t *testing.T, port int) net.Conn {
	t.Helper()
	var d net.Dialer
	c, err := d.DialContext(t.Context(), "tcp4", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	_ = c.SetReadDeadline(time.Now().Add(10 * time.Second))
	return c
}

// waitForClose fails the test unless ch closes within 10 s.
func waitForClose(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(10 * time.Second):
		t.Fatalf("waited 10 s for %s", what)
	}
}

// wantRevoked fails the test unless serve returns within 10 s, saying the
// connection to the koko server ended as revoked.
func wantRevoked(t *testing.T, served <-chan error) {
	t.Helper()
	select {
	case err := <-served:
		if lost, ok := errors.AsType[*connectionEnded](err); !ok || !errors.Is(lost.err, serverconn.ErrRevoked) {
			t.Errorf("koko web's forwarder ended with %v, want the connection ended as revoked", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("koko web's forwarder did not end within 10 s of the connection to the koko server")
	}
}

// endingConn is a connection to the koko server whose channels each answer
// with answer, as the koko server's had before it ended the connection,
// and which ends, revoked, at end, or at the test's end. Like an SSH
// channel, each channel's reads return what arrived before the end, then
// io.EOF.
type endingConn struct {
	answer []byte
	// opened closes once a channel opens.
	opened     chan struct{}
	openedOnce func()
	ended      chan struct{}
	end        func()
}

func newEndingConn(t *testing.T, answer []byte) *endingConn {
	c := &endingConn{answer: answer, opened: make(chan struct{}), ended: make(chan struct{})}
	c.openedOnce = sync.OnceFunc(func() { close(c.opened) })
	c.end = sync.OnceFunc(func() { close(c.ended) })
	t.Cleanup(c.end)
	return c
}

func (c *endingConn) Report(context.Context) (wire.Report, error) { return wire.Report{}, nil }

func (c *endingConn) OpenChannel(context.Context, string, []byte) (io.ReadWriteCloser, error) {
	defer c.openedOnce()
	ch := &answeredChannel{answer: bytes.NewReader(c.answer), ended: c.ended, closed: make(chan struct{})}
	ch.close = sync.OnceFunc(func() { close(ch.closed) })
	return ch, nil
}

func (c *endingConn) Wait() error {
	<-c.ended
	return serverconn.ErrRevoked
}

func (c *endingConn) Disconnected() <-chan struct{} { return nil }

func (c *endingConn) Close() error { return nil }

// answeredChannel is one of endingConn's channels.
type answeredChannel struct {
	answer *bytes.Reader
	ended  <-chan struct{}
	closed chan struct{}
	close  func()
}

func (ch *answeredChannel) Read(p []byte) (int, error) {
	if ch.answer.Len() > 0 {
		return ch.answer.Read(p)
	}
	select {
	case <-ch.ended:
		return 0, io.EOF
	case <-ch.closed:
		return 0, net.ErrClosed
	}
}

func (ch *answeredChannel) Write(p []byte) (int, error) {
	select {
	case <-ch.closed:
		return 0, net.ErrClosed
	default:
		return len(p), nil
	}
}

func (ch *answeredChannel) Close() error {
	ch.close()
	return nil
}
