package harness

import (
	"context"
	"io"
	"net"
	"sync"
	"testing"
)

// Forwarder is a TCP forwarder on a loopback port: it passes each
// connection it accepts to its target, which the test can change, and
// counts them. A koko server initialised with the forwarder's address as its
// Server Address issues Join Codes that lead wherever the test points it:
// first to an impostor, then to the koko server itself; and a test sees that
// a refusal made no connection.
type Forwarder struct {
	ln net.Listener
	// stop ends the dials in progress when the forwarder stops.
	stop context.CancelFunc

	mu     sync.Mutex
	target string
	conns  int
	open   map[net.Conn]struct{}
	wg     sync.WaitGroup
}

// Forward starts a Forwarder to target, a host:port, which stops when the
// test ends.
func Forward(t testing.TB, target string) *Forwarder {
	t.Helper()
	var lc net.ListenConfig
	ln, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, stop := context.WithCancel(context.Background())
	f := &Forwarder{ln: ln, stop: stop, target: target, open: map[net.Conn]struct{}{}}
	f.wg.Go(func() { f.accept(ctx) })
	t.Cleanup(f.close)
	return f
}

// Addr is the forwarder's own address, host:port.
func (f *Forwarder) Addr() string {
	return f.ln.Addr().String()
}

// To points the connections the forwarder accepts from now on at target.
func (f *Forwarder) To(target string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.target = target
}

// Conns counts the connections the forwarder has accepted.
func (f *Forwarder) Conns() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.conns
}

func (f *Forwarder) accept(ctx context.Context) {
	for {
		in, err := f.ln.Accept()
		if err != nil {
			return
		}
		f.mu.Lock()
		f.conns++
		target := f.target
		f.open[in] = struct{}{}
		f.mu.Unlock()
		f.wg.Go(func() { f.pass(ctx, in, target) })
	}
}

// pass copies bytes both ways between in and a new connection to target
// until either side ends.
func (f *Forwarder) pass(ctx context.Context, in net.Conn, target string) {
	defer f.forget(in)
	var d net.Dialer
	out, err := d.DialContext(ctx, "tcp", target)
	if err != nil {
		return
	}
	f.mu.Lock()
	f.open[out] = struct{}{}
	f.mu.Unlock()
	defer f.forget(out)
	done := make(chan struct{}, 2)
	f.wg.Go(func() { _, _ = io.Copy(out, in); done <- struct{}{} })
	f.wg.Go(func() { _, _ = io.Copy(in, out); done <- struct{}{} })
	<-done
}

func (f *Forwarder) forget(c net.Conn) {
	_ = c.Close()
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.open, c)
}

func (f *Forwarder) close() {
	f.stop()
	_ = f.ln.Close()
	f.mu.Lock()
	for c := range f.open {
		_ = c.Close()
	}
	f.mu.Unlock()
	f.wg.Wait()
}
