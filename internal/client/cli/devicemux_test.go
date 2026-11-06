package cli_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/cieons/koko/internal/client/devicemux"
	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/shared/build"
	"github.com/cieons/koko/internal/shared/timing"
	"github.com/cieons/koko/internal/shared/wire"
)

// muxDevice joins a device whose commands go through a Device Mux, with the
// timings given, to a koko server reached through a forwarder, which counts
// the Device Connections the device opens. It returns once the Device Mux
// koko join started holds its Device Connection, the second after the join's
// own.
func muxDevice(t *testing.T, timings timing.Timings) (*harness.Harness, *harness.Forwarder, *harness.Device) {
	t.Helper()
	h, fwd, _, init := initThrough(t)
	d := harness.NewMuxDevice(t, timings)
	d.Koko("join", init.JoinCode.Code).WantSuccess(t)
	waitConns(t, fwd, 2, d)
	return h, fwd, d
}

// waitConns waits until the forwarder has carried n connections, at most 10
// s.
func waitConns(t *testing.T, fwd *harness.Forwarder, n int, d *harness.Device) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for fwd.Conns() < n {
		if time.Now().After(deadline) {
			t.Fatalf("%d connection(s) reached the koko server within 10 s, want %d; the Device Mux logged:\n%s", fwd.Conns(), n, d.MuxLog())
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// koko join starts the Device Mux, which opens the device's Device
// Connection at once; every command after shares it, and opens none of its
// own. KOKO_DIRECT=1 makes a command open its own.
func TestCommandsShareTheDeviceMuxsDeviceConnection(t *testing.T) {
	_, fwd, d := muxDevice(t, timing.Timings{})
	for range 3 {
		d.Koko("space", "ls", "--json").WantSuccess(t)
	}
	d.Koko("space", "add", "ops", "--json").WantSuccess(t)
	if n := fwd.Conns(); n != 2 {
		t.Errorf("the device opened %d connections, want 2: the join's, and the Device Mux's", n)
	}

	d.KokoEnv([]string{"KOKO_DIRECT=1"}, "space", "ls", "--json").WantSuccess(t)
	if n := fwd.Conns(); n != 3 {
		t.Errorf("with KOKO_DIRECT=1 the device opened %d connections in all, want 3: one more of the command's own", n)
	}
}

// Commands that find no Device Mux running and start one at the same time
// race for its lock: one Device Mux wins, the others' exit, and every
// command waits for the winner's socket and goes through it, so the device
// opens one Device Connection. koko join with KOKO_DIRECT=1 starts none.
func TestOneDeviceMuxPerBuildWhenCommandsRaceToStartIt(t *testing.T) {
	h, fwd, _, _ := initThrough(t)
	d := harness.NewMuxDevice(t, timing.Timings{})
	d.KokoEnv([]string{"KOKO_DIRECT=1"}, "join", deviceAdd(t, h, "laptop").Code).WantSuccess(t)
	if n := fwd.Conns(); n != 1 {
		t.Fatalf("koko join with KOKO_DIRECT=1 left %d connections, want its own: it starts no Device Mux", n)
	}

	const commands = 6
	results := make(chan harness.Result, commands)
	var wg sync.WaitGroup
	for range commands {
		wg.Go(func() { results <- d.Koko("space", "ls", "--json") })
	}
	wg.Wait()
	close(results)
	for r := range results {
		r.WantSuccess(t)
	}
	if n := fwd.Conns(); n != 2 {
		t.Errorf("%d commands racing to start the Device Mux made %d connections in all, want 2: the join's, and one Device Mux's; it logged:\n%s", commands, n, d.MuxLog())
	}
	if n := strings.Count(d.MuxLog(), "device mux started"); n != 1 {
		t.Errorf("%d Device Muxes started, want 1; they logged:\n%s", n, d.MuxLog())
	}
}

// While a Device Mux of this build runs, another one started (koko
// device-mux, as a command that saw none would start it) finds the lock
// taken, waits for it as long as a start may take, and exits, leaving the
// first one serving: it opens no Device Connection and takes no socket.
func TestASecondDeviceMuxOfTheBuildExitsAtItsLock(t *testing.T) {
	const start = 200 * time.Millisecond
	_, fwd, d := muxDevice(t, timing.Timings{DeviceMuxStart: start})
	ran := make(chan harness.Result, 1)
	t.Cleanup(func() { <-ran }) // the test's context, which ends first, stops it
	began := time.Now()
	go func() { ran <- d.KokoEnv([]string{"KOKO_LOG_LEVEL=debug"}, devicemux.Arg) }()
	var r harness.Result
	select {
	case r = <-ran:
		ran <- r
	case <-time.After(10 * time.Second):
		t.Fatalf("a second Device Mux ran on beside the first; the first logged:\n%s", d.MuxLog())
	}
	if r.Status != 0 || r.Stdout != "" || strings.Contains(r.Stderr, "device mux started") || !strings.Contains(r.Stderr, "another Device Mux of this build runs") {
		t.Errorf("the second Device Mux exited %d, stdout %q, and logged %q; want 0, having found the lock taken", r.Status, r.Stdout, r.Stderr)
	}
	if took := time.Since(began); took < start {
		t.Errorf("the second Device Mux exited after %v, before waiting %v for the lock", took, start)
	}
	d.Koko("space", "ls", "--json").WantSuccess(t)
	if n := fwd.Conns(); n != 2 {
		t.Errorf("the device made %d connections, want the 2 it had", n)
	}
}

// A device is an OS user's home: another home of the same OS user, joined
// as another User, finds the Device Mux of its build carrying the first
// home's device, and opens its own Device Connections, saying why at debug
// level; it acts as its own User throughout.
func TestAnotherHomeGoesDirectBesideTheDeviceMux(t *testing.T) {
	h, fwd, _, _ := initThrough(t)
	h.Koko("user", "invite", "bob").WantSuccess(t)
	alices := harness.NewMuxDevice(t, timing.Timings{DeviceMuxStart: 200 * time.Millisecond})
	alices.Koko("join", deviceAdd(t, h, "laptop").Code).WantSuccess(t)
	waitConns(t, fwd, 2, alices)

	bobs := harness.NewMuxDeviceBeside(t, alices)
	bobs.Koko("join", deviceAdd(t, h, "phone", "--user", "bob").Code).WantSuccess(t)
	conns := fwd.Conns()
	r := bobs.KokoEnv([]string{"KOKO_LOG_LEVEL=debug"}, "device", "ls", "--json")
	wantDirect(t, r, "carries the device of another home, "+alices.Home())
	if strings.Contains(r.Stdout, `"device": "laptop"`) || !strings.Contains(r.Stdout, `"device": "phone"`) {
		t.Errorf("device ls in bob's home printed %s, want bob's devices only", r.Stdout)
	}
	if n := fwd.Conns() - conns; n != 1 {
		t.Errorf("a command in bob's home made %d connections, want its own one", n)
	}
	alices.Koko("space", "ls", "--json").WantSuccess(t)
	if n := fwd.Conns() - conns; n != 1 {
		t.Errorf("a command in alice's home made a connection, want it through her Device Mux")
	}
}

// A Device Mux that starts and exits at once, as in a sandbox that lets it
// start but not listen, sends the command direct at once, not after the
// whole wait for its socket.
func TestADeviceMuxThatExitsAtOnceSendsTheCommandDirect(t *testing.T) {
	const start = 5 * time.Second
	h, fwd, _, _ := initThrough(t)
	d := harness.NewMuxDevice(t, timing.Timings{DeviceMuxStart: start})
	// A directory at its socket's path, which it cannot remove, stops every
	// Device Mux at its start.
	socket := filepath.Join(d.RuntimeDir(), "koko", devicemux.Name(build.Read())+".sock")
	mustDo(t, os.MkdirAll(filepath.Join(socket, "in-the-way"), 0o700))
	d.Koko("join", deviceAdd(t, h, "laptop").Code).WantSuccess(t)

	began := time.Now()
	wantDirect(t, d.KokoEnv([]string{"KOKO_LOG_LEVEL=debug"}, "space", "ls", "--json"), "the Device Mux it started exited")
	if took := time.Since(began); took > start/2 {
		t.Errorf("the command went direct after %v, want at once", took)
	}
	if n := fwd.Conns(); n != 2 {
		t.Errorf("the device made %d connections, want 2: the join's, and the command's own", n)
	}
}

// KOKO_LOG_LEVEL takes what koko server run's --log-level takes; any other
// value refuses the command line, as there.
func TestAnUnknownLogLevelIsRefused(t *testing.T) {
	d := harness.NewDevice(t)
	msg := d.KokoEnv([]string{"KOKO_LOG_LEVEL=trace"}, "version", "--json").WantError(t, 2, "invalid_usage")
	if !strings.Contains(msg, `KOKO_LOG_LEVEL is "trace"`) {
		t.Errorf("the refusal says %q, without naming the value", msg)
	}
	d.KokoEnv([]string{"KOKO_LOG_LEVEL=warn"}, "version", "--json").WantSuccess(t)
}

// The Device Mux exits once nothing has been connected to it for its idle
// time, and removes its socket; the next command starts a new one. A
// command connected keeps it running, however long it holds no channel
// open, as koko web between browser connections does.
func TestTheDeviceMuxIdlesOutWithNothingConnected(t *testing.T) {
	const idle = 300 * time.Millisecond
	_, fwd, d := muxDevice(t, timing.Timings{DeviceMuxIdle: idle})
	d.Koko("space", "ls", "--json").WantSuccess(t)
	held := d.DialMux()
	time.Sleep(3 * idle)
	if strings.Contains(d.MuxLog(), "device mux stopped") {
		t.Fatalf("the Device Mux stopped with a command connected; it logged:\n%s", d.MuxLog())
	}
	ch, err := held.OpenChannel(t.Context(), wire.ChannelAPI, nil)
	if err != nil {
		t.Fatalf("the command held through the idle time opens no channel: %v", err)
	}
	_ = ch.Close()
	if n := fwd.Conns(); n != 2 {
		t.Errorf("the device made %d connections, want the 2 it had", n)
	}

	_ = held.Close()
	waitLog(t, d, `msg="device mux stopped" reason="nothing was connected to it for a while"`)
	socket := filepath.Join(d.RuntimeDir(), "koko", devicemux.Name(build.Read())+".sock")
	if _, err := os.Lstat(socket); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the Device Mux left its socket %s behind (%v)", socket, err)
	}
	d.Koko("space", "ls", "--json").WantSuccess(t)
	if n := fwd.Conns(); n != 3 {
		t.Errorf("the device made %d connections in all, want 3: the next command's Device Mux opened one more", n)
	}
}

// When the koko server revokes the Device Session of a device whose Device
// Mux holds its Device Connection, the Device Mux learns it from the
// revocation disconnect at once, with no command running: it deletes the
// device's key, client config and known_hosts, writes the revocation record,
// and exits. Later commands explain the revocation from the record, and
// connect to nothing.
func TestTheDeviceMuxRunsTheRevocationCleanupAndExits(t *testing.T) {
	h, fwd, _, _ := initThrough(t)
	h.Koko("user", "invite", "bob").WantSuccess(t)
	d := harness.NewMuxDevice(t, timing.Timings{})
	d.Koko("join", deviceAdd(t, h, "laptop", "--user", "bob").Code).WantSuccess(t)
	waitConns(t, fwd, 2, d)
	id := readClientConfig(t, d).DeviceSession

	deviceRevoke(t, h.Koko, "bob/laptop")
	waitLog(t, d, `msg="device mux stopped" reason="the koko server revoked this device's Device Session"`)
	wantGone(t, d, "device_key", "client.json", "known_hosts")
	if rec := readRevocation(t, d); rec.DeviceSession != id || rec.Device != "laptop" || rec.User != "bob" {
		t.Errorf("the revocation record is %+v, want bob's laptop %s", rec, id)
	}
	conns := fwd.Conns()
	msg := d.Koko("space", "ls", "--json").WantError(t, 1, "revoked")
	if !strings.Contains(msg, `the Device Session "laptop" of this device was revoked`) {
		t.Errorf("a command after the revocation says %q, without naming the device", msg)
	}
	if n := fwd.Conns() - conns; n != 0 {
		t.Errorf("a command after the revocation made %d connection(s), want none", n)
	}
}

// A device revoking its own Device Session through its Device Mux gets the
// answer, says it was this device, and is cleaned up; its Device Mux exits.
func TestRevokingTheDeviceInUseThroughTheDeviceMux(t *testing.T) {
	h, fwd, _, _ := initThrough(t)
	h.Koko("user", "invite", "bob").WantSuccess(t)
	d := harness.NewMuxDevice(t, timing.Timings{})
	d.Koko("join", deviceAdd(t, h, "laptop", "--user", "bob").Code).WantSuccess(t)
	waitConns(t, fwd, 2, d)

	r := d.Koko("device", "revoke", "laptop")
	r.WantSuccess(t)
	if !strings.Contains(r.Stdout, "It was this device: koko deleted its key and client config") {
		t.Errorf("device revoke of the device in use printed %q", r.Stdout)
	}
	wantGone(t, d, "device_key", "client.json", "known_hosts")
	waitLog(t, d, `msg="device mux stopped" reason="the koko server revoked this device's Device Session"`)
	d.Koko("space", "ls", "--json").WantError(t, 1, "revoked")
}

// A runtime directory koko cannot trust (another mode than 0700, a symlink,
// a file) keeps the Device Mux from starting: koko join still joins, and
// each command opens its own Device Connection, saying why in one debug
// line when KOKO_LOG_LEVEL=debug asks for it. KOKO_DIRECT=1 does the same.
func TestTheDirectPath(t *testing.T) {
	for name, c := range map[string]struct {
		spoil func(t *testing.T, dir string)
		why   string
	}{
		"mode 0755": {func(t *testing.T, dir string) {
			mustDo(t, os.Mkdir(dir, 0o700), os.Chmod(dir, 0o755))
		}, "has mode 0755, not 0700"},
		"a symlink": {func(t *testing.T, dir string) {
			mustDo(t, os.Symlink(t.TempDir(), dir))
		}, "is not a directory"},
		"a file": {func(t *testing.T, dir string) {
			mustDo(t, os.WriteFile(dir, nil, 0o600))
		}, "is not a directory"},
	} {
		t.Run(name, func(t *testing.T) {
			h, fwd, _, _ := initThrough(t)
			d := harness.NewMuxDevice(t, timing.Timings{})
			c.spoil(t, filepath.Join(d.RuntimeDir(), "koko"))
			d.Koko("join", deviceAdd(t, h, "laptop").Code).WantSuccess(t)
			d.Koko("space", "ls", "--json").WantSuccess(t)
			wantDirect(t, d.KokoEnv([]string{"KOKO_LOG_LEVEL=debug"}, "space", "ls", "--json"), c.why)
			if n := fwd.Conns(); n != 3 {
				t.Errorf("the device made %d connections, want 3: the join's, then one per command", n)
			}
		})
	}

	t.Run("KOKO_DIRECT=1", func(t *testing.T) {
		_, fwd, d := muxDevice(t, timing.Timings{})
		wantDirect(t, d.KokoEnv([]string{"KOKO_DIRECT=1", "KOKO_LOG_LEVEL=debug"}, "space", "ls", "--json"), "KOKO_DIRECT=1")
		if n := fwd.Conns(); n != 3 {
			t.Errorf("the device made %d connections, want 3: the join's, the Device Mux's and the command's own", n)
		}
	})
}

// wantDirect fails the test unless the command succeeded and logged one
// debug line on stderr, saying it opened a direct Device Connection and why.
func wantDirect(t *testing.T, r harness.Result, why string) {
	t.Helper()
	if r.Status != 0 || r.Stdout == "" {
		t.Fatalf("exit status %d, stdout %q, stderr %q, want success", r.Status, r.Stdout, r.Stderr)
	}
	lines := strings.Split(strings.TrimSuffix(r.Stderr, "\n"), "\n")
	if len(lines) != 1 || !strings.Contains(lines[0], `level=DEBUG msg="opening a direct Device Connection"`) || !strings.Contains(lines[0], why) {
		t.Errorf("stderr is %q, want one debug line saying the command went direct because %s", r.Stderr, why)
	}
}

func mustDo(t *testing.T, errs ...error) {
	t.Helper()
	if err := errors.Join(errs...); err != nil {
		t.Fatal(err)
	}
}

// A call cut mid-flight on the Device Mux's Device Connection, after the
// koko server has carried it out, fails in the command as a drop; the
// command sends it again through the Device Mux, which opens a new Device
// Connection, and the write applies once.
func TestACallCutUnderTheDeviceMuxIsRetriedAndAppliesOnce(t *testing.T) {
	h, fwd, d := muxDevice(t, timing.Timings{})
	const answered = "path=/v1/space.add status=200"
	fwd.CutWhen(func() bool { return strings.Contains(h.Log(), answered) })
	r := d.Koko("space", "add", "ops", "--json")
	r.WantSuccess(t)
	if !strings.Contains(r.Stdout, `"name": "ops"`) {
		t.Errorf("printed %s, want the Space ops", r.Stdout)
	}
	if n := fwd.Conns(); n != 3 {
		t.Errorf("the device made %d connections, want 3: the join's, and the Device Mux's before and after the cut", n)
	}
	if n := strings.Count(h.Log(), "path=/v1/space.add status=200 replayed=true"); n != 1 {
		t.Errorf("the koko server answered %d space.add call(s) with the stored response, want the retry's:\n%s", n, h.Log())
	}
}

// A command stays connected to the Device Mux through a dropped Device
// Connection. An open the drop cut after it reached the koko server fails,
// since the koko server may have acted on it, and is not sent again on the
// Device Connection the Device Mux opens at once after the drop; the next
// open goes on that one. A channel the koko server refuses comes back
// refused with the koko server's own message, and the report is the koko
// server's.
func TestTheDeviceMuxCarriesChannelsAcrossADrop(t *testing.T) {
	_, fwd, d := muxDevice(t, timing.Timings{})
	conn := d.DialMux()
	report, err := conn.Report(t.Context())
	if err != nil || report.Version != build.Read().Version || !slices.Contains(report.Operations, "space.list") {
		t.Errorf("the report through the Device Mux is %+v (%v), want the koko server's", report, err)
	}
	_, err = conn.OpenChannel(t.Context(), "nope@koko", nil)
	if e, ok := errors.AsType[*ssh.OpenChannelError](err); !ok || e.Reason != ssh.UnknownChannelType || e.Message != `koko serves no "nope@koko" channel` {
		t.Errorf("opening a channel the koko server serves not: %v, want its refusal as it is", err)
	}

	fwd.CutWhen(func() bool { return true })
	if ch, err := conn.OpenChannel(t.Context(), wire.ChannelAPI, nil); err == nil {
		_ = ch.Close()
		t.Fatalf("a channel whose open the drop cut opened, want it refused: it was sent again")
	}
	waitLogs(t, d, `msg="device connection opened"`, 2)
	ch, err := conn.OpenChannel(t.Context(), wire.ChannelAPI, nil)
	if err != nil {
		t.Fatalf("a channel opened after the drop: %v; the Device Mux logged:\n%s", err, d.MuxLog())
	}
	_ = ch.Close()
	if n := fwd.Conns(); n != 3 {
		t.Errorf("the device made %d connections, want 3: the join's, and the Device Mux's before and after the drop", n)
	}
}

// waitLog waits until the device's Device Muxes have logged substr, at most
// 10 s.
func waitLog(t *testing.T, d *harness.Device, substr string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(d.MuxLog(), substr) {
		if time.Now().After(deadline) {
			t.Fatalf("the Device Mux did not log %q within 10 s; it logged:\n%s", substr, d.MuxLog())
		}
		time.Sleep(5 * time.Millisecond)
	}
}
