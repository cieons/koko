package cli_test

import (
	"net"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cieons/koko/internal/client/cli"
	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/shared/api"
	"github.com/cieons/koko/internal/shared/timing"
	"github.com/cieons/koko/internal/shared/wire"
)

// initThroughWith is initThrough with timings as the koko server's
// timing-constants hook: a koko server reached through a forwarder, whose
// Join Codes name the forwarder. It returns the address the koko server
// itself listens on beside them.
func initThroughWith(t *testing.T, timings timing.Timings) (*harness.Harness, *harness.Forwarder, string, api.ServerInitResponse) {
	t.Helper()
	listen := harness.FreeAddr(t)
	h := harness.Start(t, harness.NoInit(), harness.Listen(listen), harness.Timings(timings))
	fwd := harness.Forward(t, listen)
	init, err := h.Call(api.ServerInit, api.ServerInitRequest{Owner: harness.Owner, Address: fwd.Addr()})
	if err != nil {
		t.Fatalf("server.init: %v", err)
	}
	return h, fwd, listen, init
}

// tries is a listener that stands where the koko server was: it notes when
// each connection arrives, and closes it.
type tries struct {
	ln net.Listener
	mu sync.Mutex
	at []time.Time
}

// recordTries starts a tries listener, stopped when the test ends.
func recordTries(t *testing.T) *tries {
	t.Helper()
	var lc net.ListenConfig
	ln, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	r := &tries{ln: ln}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			r.mu.Lock()
			r.at = append(r.at, time.Now())
			r.mu.Unlock()
			_ = c.Close()
		}
	}()
	t.Cleanup(func() {
		_ = ln.Close()
		<-done
	})
	return r
}

func (r *tries) addr() string { return r.ln.Addr().String() }

func (r *tries) times() []time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]time.Time(nil), r.at...)
}

// waitLogs waits until the device's Device Muxes have logged substr n
// times, at most 10 s.
func waitLogs(t *testing.T, d *harness.Device, substr string, n int) {
	t.Helper()
	eventually(t, "the Device Mux logs "+substr, func() bool { return strings.Count(d.MuxLog(), substr) >= n })
}

// A command waits at most timing.CommandWait for the Device Connection,
// trying again meanwhile on the direct path, while through the Device Mux
// the Device Mux tries; then it fails with server_unreachable, saying it
// cannot reach the koko server and how to give the device a new address.
func TestACommandWaitsAtMostCommandWaitForTheDeviceConnection(t *testing.T) {
	const wait = 500 * time.Millisecond
	timings := timing.Timings{CommandWait: wait, ReconnectFirst: 20 * time.Millisecond, ReconnectMax: 40 * time.Millisecond}
	h, fwd, _, init := initThroughWith(t, timing.Timings{})
	direct := harness.NewDevice(t)
	direct.UseTimings(timings)
	direct.Koko("join", init.JoinCode.Code).WantSuccess(t)
	mux := harness.NewMuxDevice(t, timings)
	mux.Koko("join", deviceAdd(t, h, "mux").Code).WantSuccess(t)
	waitLogs(t, mux, `msg="device connection opened"`, 1)
	h.Stop()
	waitLogs(t, mux, `msg="device connection dropped"`, 1)

	for name, d := range map[string]*harness.Device{"on the direct path": direct, "through the Device Mux": mux} {
		before := fwd.Conns()
		start := time.Now()
		msg := d.Koko("space", "ls", "--json").WantError(t, 1, "server_unreachable")
		took := time.Since(start)
		if took < wait || took > wait+5*time.Second {
			t.Errorf("%s, the command failed after %v, want after waiting %v", name, took, wait)
		}
		if !strings.Contains(msg, "cannot reach the koko server at "+fwd.Addr()) || !strings.Contains(msg, "koko connect <host:port>") {
			t.Errorf("%s, the command says %q", name, msg)
		}
		if n := fwd.Conns() - before; n < 3 {
			t.Errorf("%s, the device tried %d time(s) while the command waited, want again and again", name, n)
		}
	}
}

// A koko server that lets a command in but never reports its version is
// one the command cannot reach: it gives up after timing.CommandWait.
func TestAConnectionThatNeverReportsFailsWithinCommandWait(t *testing.T) {
	path := filepath.Join(harness.ShortTempDir(t), "koko.sock")
	fakeSocket(t, path, false)
	const wait = 300 * time.Millisecond
	start := time.Now()
	r := harness.Run(t, cli.Runner{SocketPath: path, Timings: timing.Timings{CommandWait: wait}}, nil, "space", "ls", "--json")
	took := time.Since(start)
	msg := r.WantError(t, 1, "server_unreachable")
	if took < wait || took > wait+5*time.Second {
		t.Errorf("the command failed after %v, want after waiting %v for the report", took, wait)
	}
	if !strings.Contains(msg, "cannot reach the koko server on its Unix socket") {
		t.Errorf("the command says %q", msg)
	}
}

// A call cut mid-flight on the direct path, after the koko server carried
// it out, is sent again once the koko server can be reached again: the
// command waits for it, within timing.CommandWait, rather than fail at the
// first try, and the write applies once.
func TestTheRetryOfACutCallWaitsForTheKokoServer(t *testing.T) {
	h, fwd, listen, init := initThroughWith(t, timing.Timings{})
	d := harness.NewDevice(t)
	d.UseTimings(timing.Timings{ReconnectFirst: 20 * time.Millisecond, ReconnectMax: 50 * time.Millisecond})
	d.Koko("join", init.JoinCode.Code).WantSuccess(t)
	dead := harness.FreeAddr(t)
	const answered = "path=/v1/space.add status=200"
	fwd.CutWhen(func() bool {
		if !strings.Contains(h.Log(), answered) {
			return false
		}
		// The koko server is out of reach for a moment after the cut.
		fwd.To(dead)
		time.AfterFunc(300*time.Millisecond, func() { fwd.To(listen) })
		return true
	})
	before := fwd.Conns()
	r := d.Koko("space", "add", "ops", "--json")
	r.WantSuccess(t)
	if !strings.Contains(r.Stdout, `"name": "ops"`) {
		t.Errorf("printed %s, want the Space ops", r.Stdout)
	}
	if n := strings.Count(h.Log(), answered+" replayed=true"); n != 1 {
		t.Errorf("the koko server answered %d space.add call(s) with the stored response, want the retry's:\n%s", n, h.Log())
	}
	if n := fwd.Conns() - before; n < 3 {
		t.Errorf("the command made %d connection(s), want the first, the tries while the koko server was out of reach, and the one that reached it", n)
	}
}

// The Device Mux probes a quiet Device Connection, so it lives however long
// nothing is sent on it while the koko server answers; once nothing comes
// back for timing.DeviceDead it declares it dead and reconnects at once.
func TestTheDeviceMuxProbesItsDeviceConnectionAndReplacesADeadOne(t *testing.T) {
	// The probe's answer comes, so dead stays long beside it: an answer
	// slow under load must not pass for none.
	const probe, dead = 50 * time.Millisecond, time.Second
	_, fwd, d := muxDevice(t, timing.Timings{DeviceProbe: probe, DeviceDead: dead})
	waitLogs(t, d, `msg="device connection opened"`, 1)
	time.Sleep(3 * dead)
	if strings.Contains(d.MuxLog(), "device connection dropped") || fwd.Conns() != 2 {
		t.Fatalf("a quiet Device Connection whose koko server answers dropped (%d connections); the Device Mux logged:\n%s", fwd.Conns(), d.MuxLog())
	}

	fwd.Stall()
	stalled := time.Now()
	waitLogs(t, d, `msg="device connection dropped"`, 1)
	if took := time.Since(stalled); took < dead-2*probe {
		t.Errorf("the Device Connection was declared dead %v after the path stalled, want about %v", took, dead)
	}
	if !strings.Contains(d.MuxLog(), "no answer for") {
		t.Errorf("the Device Mux does not say why the Device Connection dropped:\n%s", d.MuxLog())
	}
	// Nothing waits for it, yet it reconnects at once.
	waitLogs(t, d, `msg="device connection opened"`, 2)
	d.Koko("space", "ls", "--json").WantSuccess(t)
	if n := fwd.Conns(); n != 3 {
		t.Errorf("the device made %d connections, want 3: the join's, and the Device Mux's before and after the stall", n)
	}
}

// The koko server probes a quiet Device Connection, which lives while the
// device answers, and closes it as dead once nothing comes from the device
// for timing.ServerDead.
func TestTheKokoServerProbesADeviceConnectionAndClosesADeadOne(t *testing.T) {
	// The probe's answer comes, so dead stays long beside it.
	const probe, dead = 50 * time.Millisecond, time.Second
	h, fwd, _, init := initThroughWith(t, timing.Timings{ServerProbe: probe, ServerDead: dead})
	d := harness.NewDevice(t)
	d.Koko("join", init.JoinCode.Code).WantSuccess(t)
	conn := d.Dial() // it probes nothing itself
	time.Sleep(3 * dead)
	if strings.Contains(h.Log(), "reason=dead") {
		t.Fatalf("the koko server closed a quiet Device Connection whose device answers:\n%s", h.Log())
	}
	ch, err := conn.OpenChannel(t.Context(), wire.ChannelAPI, nil)
	if err != nil {
		t.Fatalf("the quiet Device Connection opens no channel: %v", err)
	}
	_ = ch.Close()

	fwd.Stall()
	stalled := time.Now()
	waitServerLog(t, h, `msg="device connection closed" device_session=device_\S+ addr=\S+ reason=dead err="no answer for`)
	if took := time.Since(stalled); took < dead-2*probe {
		t.Errorf("the koko server declared the Device Connection dead %v after the path stalled, want about %v", took, dead)
	}
}

// When its Device Connection drops, the Device Mux tries to reconnect at
// once; with nothing waiting it stops there. While a command waits, it
// tries again after timing.ReconnectFirst, doubling the wait up to
// timing.ReconnectMax, and stops once the command has given up.
func TestTheDeviceMuxReconnectsAtOnceThenBacksOffWhileACommandWaits(t *testing.T) {
	const first, most, wait = 20 * time.Millisecond, 40 * time.Millisecond, 2 * time.Second
	// No wake or route change of the machine running the test cuts a wait
	// short.
	never := make(chan struct{})
	h, fwd, d := muxDevice(t, timing.Timings{ReconnectFirst: first, ReconnectMax: most, CommandWait: wait, Wake: never, RouteChange: never})
	waitLogs(t, d, `msg="device connection opened"`, 1)
	r := recordTries(t)
	fwd.To(r.addr())
	h.Stop()
	eventually(t, "the Device Mux tries to reconnect at once", func() bool { return len(r.times()) >= 1 })
	time.Sleep(5 * most)
	if n := len(r.times()); n != 1 {
		t.Fatalf("with nothing waiting, the Device Mux tried %d times, want once, at once", n)
	}

	d.Koko("space", "ls", "--json").WantError(t, 1, "server_unreachable")
	at := r.times()[1:]
	// Waits doubling without end (20, 40, 80, 160 ms...) leave room for at
	// most 8 tries in 2 s; held at 40 ms, for 50.
	if len(at) < 12 {
		t.Fatalf("while the command waited %v, the Device Mux tried %d times, want its waits held at %v", wait, len(at), most)
	}
	want := first
	for i := 1; i < len(at); i++ {
		if gap := at[i].Sub(at[i-1]); gap < want-5*time.Millisecond {
			t.Errorf("try %d came %v after the one before, want at least %v", i+1, gap, want)
		}
		want = min(2*want, most)
	}
	// A try begun while the command still waited may land after it gave
	// up, and no other: the loop looks for what waits before each try.
	n := len(r.times())
	time.Sleep(5 * most)
	if m := len(r.times()); m-n > 1 {
		t.Errorf("the Device Mux tried %d more times once the command had given up, want at most the one under way", m-n)
	}
}

// A wake from sleep or a change of the device's routes, injected through
// the timing hook, makes the Device Mux probe its Device Connection at
// once, long before it would have: a dead one is replaced within
// timing.DeviceDead-timing.DeviceProbe.
func TestAWakeOrARouteChangeProbesTheDeviceConnectionAtOnce(t *testing.T) {
	const probe, dead = time.Hour, time.Hour + 300*time.Millisecond
	wake, routes := make(chan struct{}), make(chan struct{})
	_, fwd, d := muxDevice(t, timing.Timings{DeviceProbe: probe, DeviceDead: dead, Wake: wake, RouteChange: routes})
	for i, signal := range []struct {
		name string
		ch   chan struct{}
	}{{"a wake", wake}, {"a route change", routes}} {
		waitLogs(t, d, `msg="device connection opened"`, i+1)
		fwd.Stall()
		signal.ch <- struct{}{}
		waitLogs(t, d, `msg="device connection dropped"`, i+1)
		waitConns(t, fwd, 3+i, d)
		if t.Failed() {
			t.Fatalf("after %s", signal.name)
		}
	}
	waitLogs(t, d, `msg="device connection opened"`, 3)
	d.Koko("space", "ls", "--json").WantSuccess(t)
}

// A wake while the Device Mux waits between tries to reconnect makes it
// try again at once.
func TestAWakeWhileTheDeviceMuxReconnectsTriesAgainAtOnce(t *testing.T) {
	wake := make(chan struct{})
	h, fwd, d := muxDevice(t, timing.Timings{ReconnectFirst: time.Hour, CommandWait: 5 * time.Second, Wake: wake})
	waitLogs(t, d, `msg="device connection opened"`, 1)
	r := recordTries(t)
	fwd.To(r.addr())
	h.Stop()
	eventually(t, "the Device Mux tries to reconnect at once", func() bool { return len(r.times()) >= 1 })

	var result harness.Result
	var command sync.WaitGroup
	command.Go(func() { result = d.Koko("space", "ls", "--json") })
	t.Cleanup(command.Wait)
	eventually(t, "the Device Mux tries at once for the command", func() bool { return len(r.times()) >= 2 })
	wake <- struct{}{}
	eventually(t, "the Device Mux tries again at once on the wake, not an hour later", func() bool { return len(r.times()) >= 3 })
	command.Wait()
	result.WantError(t, 1, "server_unreachable")
}

// silent is a listener that takes each connection and says nothing on it,
// as an address that drops what it is sent after TCP would, until the test
// ends; it counts them.
func silent(t *testing.T) (addr string, conns func() int) {
	t.Helper()
	var lc net.ListenConfig
	ln, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var held []net.Conn
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			held = append(held, c)
			mu.Unlock()
		}
	}()
	t.Cleanup(func() {
		_ = ln.Close()
		<-done
		for _, c := range held {
			_ = c.Close()
		}
	})
	return ln.Addr().String(), func() int {
		mu.Lock()
		defer mu.Unlock()
		return len(held)
	}
}

// A try to reconnect lasts at most timing.DeviceConnectAttempt, so an
// address that never answers holds up no try after it.
func TestATryToReconnectEndsSoTheNextCanFollow(t *testing.T) {
	never := make(chan struct{})
	h, fwd, d := muxDevice(t, timing.Timings{
		DeviceConnectAttempt: 200 * time.Millisecond, CommandWait: 2 * time.Second,
		ReconnectFirst: 20 * time.Millisecond, ReconnectMax: 40 * time.Millisecond, Wake: never, RouteChange: never,
	})
	waitLogs(t, d, `msg="device connection opened"`, 1)
	addr, conns := silent(t)
	fwd.To(addr)
	h.Stop()
	d.Koko("space", "ls", "--json").WantError(t, 1, "server_unreachable")
	if n := conns(); n < 4 {
		t.Errorf("the Device Mux tried %d time(s) in 2 s against an address that never answers, want a try every 200 ms or so", n)
	}
}
