package harness

import (
	"context"
	"net"
	"sync"
	"sync/atomic"
	"testing"
)

// Forwarder is a TCP forwarder on a loopback port: it passes each
// connection it accepts to its target, which the test can change, and
// counts them. A koko server initialised with the forwarder's address as its
// Server Address issues Join Codes that lead wherever the test points it:
// first to an impostor, then to the koko server itself; and a test sees that
// a refusal made no connection. It also cuts a connection when the test
// arms it to (CutWhen), as a network drop does, and stalls the connections
// it carries (Stall), as a network path that went dead does.
type Forwarder struct {
	ln net.Listener
	// stop ends the dials in progress when the forwarder stops.
	stop context.CancelFunc

	mu     sync.Mutex
	target string
	conns  int
	open   map[net.Conn]struct{}
	// cut, when armed, says when to cut the next connection whose target
	// sends anything.
	cut func() bool
	// stalls holds, for each connection it carries, whether Stall stalled
	// it.
	stalls map[*atomic.Bool]struct{}
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
	f := &Forwarder{ln: ln, stop: stop, target: target, open: map[net.Conn]struct{}{}, stalls: map[*atomic.Bool]struct{}{}}
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

// CutWhen arms one cut: the first bytes the target sends, on any
// connection, once cut returns true, are dropped with that connection, both
// ways, and the cut is spent. A test cuts a call mid-flight with it: cut
// turns true once the koko server has logged the call's answer, and the
// answer, which it sends after, never reaches the device.
func (f *Forwarder) CutWhen(cut func() bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cut = cut
}

// Stall makes every connection the forwarder carries now pass nothing more,
// either way, without closing it, as a network path that went dead does:
// what either end sends is lost, and neither end hears of it. Connections it
// accepts after pass as before, as on the device's new path.
func (f *Forwarder) Stall() {
	f.mu.Lock()
	defer f.mu.Unlock()
	for s := range f.stalls {
		s.Store(true)
	}
}

// cutNow tells whether an armed cut falls now, and spends it if so. It asks
// cut without holding the lock, so cut may call the forwarder.
func (f *Forwarder) cutNow() bool {
	f.mu.Lock()
	cut := f.cut
	f.mu.Unlock()
	if cut == nil || !cut() {
		return false
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.cut == nil {
		return false // another connection spent it meanwhile
	}
	f.cut = nil
	return true
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
	stalled := new(atomic.Bool)
	f.mu.Lock()
	f.open[out] = struct{}{}
	f.stalls[stalled] = struct{}{}
	f.mu.Unlock()
	defer f.forget(out)
	defer func() {
		f.mu.Lock()
		delete(f.stalls, stalled)
		f.mu.Unlock()
	}()
	done := make(chan struct{}, 2)
	f.wg.Go(func() { f.relay(out, in, stalled, false); done <- struct{}{} })
	f.wg.Go(func() { f.relay(in, out, stalled, true); done <- struct{}{} })
	<-done
}

// relay passes what arrives on from to to, until either side ends, or, when
// cuttable, as for what the target sends back, an armed cut falls on it.
// Once stalled, what arrives is dropped.
func (f *Forwarder) relay(to, from net.Conn, stalled *atomic.Bool, cuttable bool) {
	buf := make([]byte, 32<<10)
	for {
		n, err := from.Read(buf)
		if n > 0 && !stalled.Load() {
			if cuttable && f.cutNow() {
				return
			}
			if _, err := to.Write(buf[:n]); err != nil {
				return
			}
		}
		if err != nil {
			return
		}
	}
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
