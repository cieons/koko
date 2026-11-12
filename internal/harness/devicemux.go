package harness

import (
	"context"
	"errors"
	"io"
	"maps"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cieons/koko/internal/client/cli"
	"github.com/cieons/koko/internal/client/devicemux"
	"github.com/cieons/koko/internal/client/serverconn"
	"github.com/cieons/koko/internal/harness/skip"
	"github.com/cieons/koko/internal/shared/timing"
)

// muxes are the Device Muxes a device started in the test process.
type muxes struct {
	runtime string
	log     *syncBuffer

	mu sync.Mutex
	// running holds the stop of each Device Mux still running, by the
	// channel closed once it exits.
	running map[chan struct{}]context.CancelFunc
	wg      sync.WaitGroup
}

// NewMuxDevice returns a device that has not joined a koko server, as
// NewDevice does, whose commands reach the koko server through a Device Mux
// once it joins. Each Device Mux it starts runs in the test process, where
// koko would start one in the background (cli.Runner.StartDeviceMux), as
// `koko device-mux` with the device's environment: a temporary runtime
// directory as TMPDIR and XDG_RUNTIME_DIR, and timings as the
// timing-constants hook (the zero Timings for the product's). They log to
// MuxLog, and stop when the test ends.
func NewMuxDevice(t testing.TB, timings timing.Timings) *Device {
	t.Helper()
	skip.OnWindows(t, "no Device Mux runs on Windows until #153, so the device's commands would go direct")
	d := NewDevice(t)
	m := &muxes{runtime: ShortTempDir(t), log: &syncBuffer{}}
	d.muxes = m
	d.env = []string{"TMPDIR=" + m.runtime, "XDG_RUNTIME_DIR=" + m.runtime}
	d.runner.Timings = timings
	d.runner.StartDeviceMux = d.startMux
	t.Cleanup(func() { m.stop(t) })
	return d
}

// NewMuxDeviceBeside returns a device as NewMuxDevice does, but another
// home of other's OS user: it has a HOME of its own and shares other's
// runtime directory, and so its Device Muxes' sockets and locks.
func NewMuxDeviceBeside(t testing.TB, other *Device) *Device {
	t.Helper()
	d := NewDevice(t)
	m := &muxes{runtime: other.muxes.runtime, log: &syncBuffer{}}
	d.muxes = m
	d.env = other.env
	d.runner.Timings = other.runner.Timings
	d.runner.StartDeviceMux = d.startMux
	t.Cleanup(func() { m.stop(t) })
	return d
}

// startMux runs a Device Mux of the device in the test process until the
// test ends, or it exits, outliving the command that started it.
func (d *Device) startMux(_ context.Context, _ string, env []string) (<-chan struct{}, error) {
	ctx, stop := context.WithCancel(context.Background())
	m := d.muxes
	exited := make(chan struct{})
	m.mu.Lock()
	if m.running == nil {
		m.running = map[chan struct{}]context.CancelFunc{}
	}
	m.running[exited] = stop
	m.mu.Unlock()
	m.wg.Go(func() {
		defer close(exited)
		defer func() {
			m.mu.Lock()
			delete(m.running, exited)
			m.mu.Unlock()
		}()
		defer stop()
		d.runner.Run(ctx, cli.Invocation{
			Args:   []string{devicemux.Arg},
			Stdin:  strings.NewReader(""),
			Stdout: io.Discard,
			Stderr: m.log,
			Env:    env,
		})
	})
	return exited, nil
}

// stop stops every Device Mux still running and waits for them, at most 10
// s.
func (m *muxes) stop(t testing.TB) {
	m.mu.Lock()
	for _, stop := range m.running {
		stop()
	}
	m.mu.Unlock()
	done := make(chan struct{})
	go func() {
		m.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(waitFor):
		t.Errorf("a Device Mux did not stop within %v; it logged:\n%s", waitFor, m.log.String())
	}
}

// KillMux stops every Device Mux the device runs as a crash would, as its
// commands and the koko server see it: their connections and its Device
// Connection close at once, nothing sent on them first. It returns once
// they have exited; a command then starts a new one.
func (d *Device) KillMux() {
	d.t.Helper()
	m := d.muxes
	m.mu.Lock()
	running := maps.Clone(m.running)
	m.mu.Unlock()
	for exited, stop := range running {
		stop()
		select {
		case <-exited:
		case <-time.After(waitFor):
			d.t.Fatalf("a Device Mux did not stop within %v; it logged:\n%s", waitFor, m.log.String())
		}
	}
}

// KokoEnv runs one koko command line in process on the device, as Koko
// does, with the variables env beside the device's own, such as
// KOKO_DIRECT=1.
func (d *Device) KokoEnv(env []string, args ...string) Result {
	d.t.Helper()
	return run(d.t, d.runner, d.environ(env...), "", args...)
}

// RuntimeDir returns the runtime directory of a NewMuxDevice, which holds
// koko/, its Device Muxes' directory.
func (d *Device) RuntimeDir() string {
	return d.muxes.runtime
}

// MuxLog returns what the Device Muxes of a NewMuxDevice have logged so far.
func (d *Device) MuxLog() string {
	return d.muxes.log.String()
}

// DialMux connects to the device's Device Mux as its commands do, which
// counts as a command connected, and holds the connection until the test
// ends or the Device Mux closes it.
func (d *Device) DialMux() serverconn.Conn {
	d.t.Helper()
	socket, err := devicemux.Socket(func(key string) string { return lookup(d.environ(), key) })
	if err != nil {
		d.t.Fatalf("find the Device Mux socket: %v", err)
	}
	conn, _, err := devicemux.Dial(d.t.Context(), socket, d.home)
	if err != nil {
		d.t.Fatalf("connect to the Device Mux: %v", err)
	}
	d.t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// HoldMux connects to the device's Device Mux as DialMux does, starting one
// first when none runs, as a command does, and again should that one exit
// before it answers: one whose idle time a test shortened may idle out
// between its start and the connection, on a loaded machine. It returns
// once a Device Mux that holds its Device Connection has answered, which
// then cannot idle out until the connection closes.
func (d *Device) HoldMux() serverconn.Conn {
	d.t.Helper()
	env := d.environ()
	socket, err := devicemux.Socket(func(key string) string { return lookup(env, key) })
	if err != nil {
		d.t.Fatalf("find the Device Mux socket: %v", err)
	}
	m := d.muxes
	for deadline := time.Now().Add(waitFor); ; time.Sleep(5 * time.Millisecond) {
		conn, _, err := devicemux.Dial(d.t.Context(), socket, d.home)
		if err == nil {
			d.t.Cleanup(func() { _ = conn.Close() })
			return conn
		}
		if !errors.Is(err, devicemux.ErrNotRunning) || time.Now().After(deadline) {
			d.t.Fatalf("connect to the Device Mux: %v; it logged:\n%s", err, m.log.String())
		}
		m.mu.Lock()
		none := len(m.running) == 0
		m.mu.Unlock()
		if none {
			_, _ = d.startMux(d.t.Context(), socket, env) // it never fails
		}
	}
}

// lookup returns the value of the last key=value in env.
func lookup(env []string, key string) string {
	value := ""
	for _, kv := range env {
		if k, v, ok := strings.Cut(kv, "="); ok && k == key {
			value = v
		}
	}
	return value
}
