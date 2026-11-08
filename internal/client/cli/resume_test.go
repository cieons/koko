package cli_test

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"errors"
	"io"
	"math/rand/v2"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/harness/sshhost"
	"github.com/cieons/koko/internal/shared/timing"
	"github.com/cieons/koko/internal/shared/wire"
)

// The resumption tests shrink how long each end takes to find a stalled
// Device Connection dead: the device first, so that it Resumes on a new
// Device Connection while the koko server still holds the old one, which
// the last attach wins over; the koko server well after, so that a probe
// answered slowly under load never passes for none. The device tries again
// within 100 ms, so a Resume follows the koko server's return at once.
var (
	resumeDevice = timing.Timings{
		DeviceProbe: 50 * time.Millisecond, DeviceDead: 600 * time.Millisecond,
		ReconnectFirst: 20 * time.Millisecond, ReconnectMax: 100 * time.Millisecond,
	}
	resumeServer = timing.Timings{ServerProbe: 100 * time.Millisecond, ServerDead: 3 * time.Second}
)

// fault is a way a Device Connection fails beneath an SSH Session.
type fault struct {
	name string
	// fall makes the Device Connections fwd carries fail.
	fall func(fwd *harness.Forwarder)
	// attachWins tells that the koko server still holds the Device
	// Connection when the device Resumes, so the Resume takes the Tunnel off
	// it and the Tunnel is never Detached.
	attachWins bool
}

// faults: a cut, which both ends see at once, as when a network drops the
// connection, so the koko server Detaches the Tunnel, or a quick Resume
// comes first; and a stall, a path that died with nothing to tell either
// end, as on a move to another network, which the device finds dead first,
// so its Resume comes from a new address while the koko server holds the
// old one.
var faults = []fault{
	{name: "a cut", fall: func(fwd *harness.Forwarder) { fwd.CutWhen(func() bool { return true }) }},
	{name: "a stall", fall: func(fwd *harness.Forwarder) { fwd.Partition() }, attachWins: true},
}

// resumeSetup is a koko server reached through a forwarder, with the
// resumption tests' timings and the Host web1.
type resumeSetup struct {
	h   *harness.Harness
	fwd *harness.Forwarder
	// listen is the address the koko server listens on, behind fwd.
	listen string
	host   *sshhost.Host
	// joinCode is the Join Code of its first device.
	joinCode string
}

// resumeHost starts a resumeSetup whose Host web1 is started with opts.
func resumeHost(t *testing.T, timings timing.Timings, opts ...sshhost.Option) resumeSetup {
	t.Helper()
	h, fwd, listen, init := initThroughWith(t, timings)
	host := tunnelHost(t, h, "web1", opts...)
	return resumeSetup{h: h, fwd: fwd, listen: listen, host: host, joinCode: init.JoinCode.Code}
}

// shell starts a shell in a new session on c and returns say, which runs one
// command line in it and returns the line it prints, failing the test after
// 10 s.
func shell(t *testing.T, c *ssh.Client) (say func(command string) string) {
	t.Helper()
	s := openSession(t, c)
	stdin, err := s.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := s.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Shell(); err != nil {
		t.Fatalf("start a shell: %v", err)
	}
	lines := bufio.NewReader(stdout)
	return func(command string) string {
		t.Helper()
		if _, err := io.WriteString(stdin, command+"\n"); err != nil {
			t.Fatalf("send %q to the shell: %v", command, err)
		}
		read := make(chan string, 1)
		go func() {
			line, _ := lines.ReadString('\n')
			read <- line
		}()
		select {
		case line := <-read:
			return line
		case <-time.After(10 * time.Second):
			t.Fatalf("the shell printed nothing for %q within 10 s", command)
			return ""
		}
	}
}

// wantResumed fails the test unless the koko server logged that the SSH
// Session to web1 Resumed, from an address other than the one it started
// from, and, when attachWins, that it was never Detached: the Resume took
// it off the channel the koko server still held.
func wantResumed(t *testing.T, h *harness.Harness, attachWins bool) {
	t.Helper()
	started := waitServerLog(t, h, `msg="ssh session started" tunnel=(\S+) .* addr=(\S+)\n`)
	resumed := waitServerLog(t, h, `msg="ssh session resumed" tunnel=(\S+) device_session=\S+ addr=(\S+)\n`)
	if resumed[1] != started[1] || resumed[2] == started[2] {
		t.Errorf("the koko server logged the Tunnel %s Resumed from %s, want the Tunnel %s from a new address, not %s", resumed[1], resumed[2], started[1], started[2])
	}
	if attachWins && strings.Contains(h.Log(), `msg="ssh session detached"`) {
		t.Errorf("the koko server Detached the Tunnel, want the Resume to take it off the channel it still held:\n%s", h.Log())
	}
}

// wantDetached waits until the koko server has logged that it Detached the
// SSH Session's Tunnel.
func wantDetached(t *testing.T, h *harness.Harness) {
	t.Helper()
	started := waitServerLog(t, h, `msg="ssh session started" tunnel=(\S+) `)
	waitServerLog(t, h, `msg="ssh session detached" tunnel=`+regexp.QuoteMeta(started[1])+` `)
}

// A shell through ssh's path, koko tunnel through the Device Mux, freezes
// when the Device Connection beneath it fails, and carries on once the
// Tunnel Resumes on the next one.
func TestAShellCarriesOnAcrossADrop(t *testing.T) {
	for _, f := range faults {
		t.Run(f.name, func(t *testing.T) {
			s := resumeHost(t, resumeServer)
			d := harness.NewMuxDevice(t, resumeDevice)
			d.Koko("join", s.joinCode).WantSuccess(t)
			p := d.Proxy(nil, "tunnel", "web1.koko")
			say := shell(t, sshOver(t, p, "web1.koko", gatewayHostKey(t, s.h)))
			if got := say("echo one"); got != "one\n" {
				t.Fatalf("the shell printed %q, want one", got)
			}

			f.fall(s.fwd)
			if got := say("echo two"); got != "two\n" {
				t.Errorf("after %s the shell printed %q, want two", f.name, got)
			}
			wantResumed(t, s.h, f.attachWins)
			select {
			case <-p.Done():
				t.Errorf("koko tunnel exited across %s: %+v", f.name, p.Wait())
			default:
			}
		})
	}
}

// faultAfter returns a reader of data, which makes f fall on fwd once at
// bytes of it have been read.
func faultAfter(data []byte, at int, f fault, fwd *harness.Forwarder) io.Reader {
	return io.MultiReader(bytes.NewReader(data[:at]), readerFunc(func([]byte) (int, error) {
		f.fall(fwd)
		return 0, io.EOF
	}), bytes.NewReader(data[at:]))
}

type readerFunc func(p []byte) (int, error)

func (r readerFunc) Read(p []byte) (int, error) { return r(p) }

// randomBytes returns n bytes from a seeded generator, so a failure repeats.
func randomBytes(n int) []byte {
	b := make([]byte, n)
	r := rand.NewChaCha8([32]byte{7, 0, 70})
	_, _ = r.Read(b)
	return b
}

// bulk is more than a replay buffer holds, so that a drop falls with bytes
// in flight both in the buffer and beyond it.
const bulk = 4 << 20

// A koko exec in flight carries on across a drop with every byte intact,
// a bulk upload on its stdin and a bulk download on its stdout alike, on
// the direct path, where it Resumes over a Device Connection of its own.
func TestABulkTransferThroughKokoExecCarriesOnAcrossADrop(t *testing.T) {
	data := randomBytes(bulk)
	for _, f := range faults {
		t.Run("upload, "+f.name, func(t *testing.T) {
			s := resumeHost(t, resumeServer)
			d := harness.NewDevice(t)
			d.UseTimings(resumeDevice)
			d.Koko("join", s.joinCode).WantSuccess(t)
			p := d.Proxy(nil, "exec", "web1", "cat > up.bin")
			go func() {
				conn := p.Conn()
				_, _ = io.Copy(conn, faultAfter(data, bulk/4, f, s.fwd))
				_ = conn.Close()
			}()
			if r := p.Wait(); r.Status != 0 || r.Stderr != "" {
				t.Fatalf("koko exec exited %d with stderr %q across %s, want 0 and nothing", r.Status, r.Stderr, f.name)
			}
			got, err := os.ReadFile(filepath.Join(s.host.Home(), "up.bin"))
			if err != nil || !bytes.Equal(got, data) {
				t.Errorf("the Host received %d bytes (%v), want the %d sent, intact", len(got), err, len(data))
			}
			wantResumed(t, s.h, f.attachWins)
		})
		t.Run("download, "+f.name, func(t *testing.T) {
			s := resumeHost(t, resumeServer)
			if err := os.WriteFile(filepath.Join(s.host.Home(), "down.bin"), data, 0o600); err != nil {
				t.Fatal(err)
			}
			d := harness.NewDevice(t)
			d.UseTimings(resumeDevice)
			d.Koko("join", s.joinCode).WantSuccess(t)
			p := d.Proxy(nil, "exec", "web1", "cat down.bin")
			// The koko server's sends come in pieces of at most 32 KiB:
			// the drop falls a quarter of the way at the latest.
			var sends atomic.Int32
			s.fwd.CutWhen(func() bool {
				if sends.Add(1) == bulk/4/(32<<10) {
					f.fall(s.fwd)
				}
				return false
			})
			read := make(chan []byte, 1)
			go func() {
				got, _ := io.ReadAll(p.Conn())
				read <- got
			}()
			if r := p.Wait(); r.Status != 0 || r.Stderr != "" {
				t.Fatalf("koko exec exited %d with stderr %q across %s, want 0 and nothing", r.Status, r.Stderr, f.name)
			}
			if got := <-read; !bytes.Equal(got, data) {
				t.Errorf("koko exec printed %d bytes, want the Host's %d, intact", len(got), len(data))
			}
			wantResumed(t, s.h, f.attachWins)
		})
	}
}

// A command that ends while its Tunnel is Detached gives koko exec, through
// the Device Mux, its output and its exit status once the Tunnel Resumes.
func TestACommandThatEndsWhileDetachedGivesItsStatusOnTheResume(t *testing.T) {
	s := resumeHost(t, resumeServer)
	d := harness.NewMuxDevice(t, resumeDevice)
	d.Koko("join", s.joinCode).WantSuccess(t)
	p := d.Proxy(nil, "exec", "web1", "sleep 0.3; echo done; touch ended; exit 3")
	read := make(chan []byte, 1)
	go func() {
		got, _ := io.ReadAll(p.Conn())
		read <- got
	}()
	waitServerLog(t, s.h, `msg="ssh session request" .* request=exec\n`)

	// The cut falls on what the koko server sends next, and the device
	// finds nothing to reconnect to until the command has ended.
	s.fwd.To(recordTries(t).addr())
	s.fwd.CutWhen(func() bool { return true })
	wantDetached(t, s.h)
	eventually(t, "the command on the Host ends", func() bool {
		_, err := os.Stat(filepath.Join(s.host.Home(), "ended"))
		return err == nil
	})
	// Long enough for its exit status to reach the Gateway, a hop away.
	time.Sleep(200 * time.Millisecond)
	s.fwd.To(s.listen)
	if r := p.Wait(); r.Status != 3 || r.Stderr != "" {
		t.Errorf("koko exec exited %d with stderr %q, want the command's 3 and nothing", r.Status, r.Stderr)
	}
	if got := <-read; string(got) != "done\n" {
		t.Errorf("koko exec printed %q, want the command's done", got)
	}
	wantResumed(t, s.h, false)
}

// A half-close the device sent before a drop survives it: koko tunnel's
// stdin ended after its path stalled, so the drop took the EOF, and only
// the Resume carries it to the Gateway, which then ends the SSH Session
// that never started; koko tunnel, having relayed all the Gateway sent,
// exits 0.
func TestAHalfCloseBeforeADropSurvivesIt(t *testing.T) {
	s := resumeHost(t, resumeServer)
	d := harness.NewDevice(t)
	d.UseTimings(resumeDevice)
	d.Koko("join", s.joinCode).WantSuccess(t)
	p := d.Proxy(nil, "tunnel", "web1")
	conn := p.Conn()
	if _, err := io.WriteString(conn, "SSH-2.0-OpenSSH_9.9\r\n"); err != nil {
		t.Fatal(err)
	}
	version := make([]byte, len("SSH-2.0-koko\r\n"))
	if _, err := io.ReadFull(conn, version); err != nil || string(version) != "SSH-2.0-koko\r\n" {
		t.Fatalf("koko tunnel relayed %q (%v), want the Gateway's version", version, err)
	}

	s.fwd.Stall()
	_ = conn.Close()
	read := make(chan []byte, 1)
	go func() {
		rest, _ := io.ReadAll(conn)
		read <- rest
	}()
	if r := p.Wait(); r.Status != 0 || r.Stderr != "" {
		t.Errorf("koko tunnel exited %d with stderr %q, want 0 and nothing once the Gateway took its EOF", r.Status, r.Stderr)
	}
	<-read
	waitServerLog(t, s.h, `msg="ssh session resumed"`)
	waitServerLog(t, s.h, `msg="ssh session ended before it started" .* err=EOF\n`)
}

// Killing the Device Mux ends no SSH Session: koko tunnel Resumes its
// Tunnel through a new Device Mux, which it starts. With KOKO_DIRECT=1 its
// Tunnel never went through the Device Mux, and Resumes over a Device
// Connection of its own.
func TestKillingTheDeviceMuxEndsNoSSHSession(t *testing.T) {
	for _, tc := range []struct {
		name string
		env  []string
		// fall makes the Tunnel's way to the koko server fail.
		fall      func(d *harness.Device, fwd *harness.Forwarder)
		wantMuxes int
	}{
		{"through the Device Mux", nil, func(d *harness.Device, _ *harness.Forwarder) { d.KillMux() }, 2},
		{"with KOKO_DIRECT=1", []string{"KOKO_DIRECT=1"}, func(d *harness.Device, fwd *harness.Forwarder) {
			d.KillMux()
			fwd.CutWhen(func() bool { return true })
		}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := resumeHost(t, resumeServer)
			d := harness.NewMuxDevice(t, resumeDevice)
			d.Koko("join", s.joinCode).WantSuccess(t)
			waitLog(t, d, `msg="device connection opened"`)
			p := d.Proxy(tc.env, "tunnel", "web1.koko")
			say := shell(t, sshOver(t, p, "web1.koko", gatewayHostKey(t, s.h)))
			if got := say("echo one"); got != "one\n" {
				t.Fatalf("the shell printed %q, want one", got)
			}

			tc.fall(d, s.fwd)
			if got := say("echo two"); got != "two\n" {
				t.Errorf("the shell printed %q, want two", got)
			}
			wantResumed(t, s.h, false)
			if n := strings.Count(d.MuxLog(), `msg="device mux started"`); n != tc.wantMuxes {
				t.Errorf("%d Device Muxes started, want %d; they logged:\n%s", n, tc.wantMuxes, d.MuxLog())
			}
		})
	}
}

// While its Device Mux reconnects, an SSH Session waits to Resume: koko
// status counts it, and it keeps the Device Mux from idling out. Once the
// koko server answers again, it Resumes, and koko status counts none.
func TestATunnelWaitingToResumeKeepsTheDeviceMuxAndShowsInKokoStatus(t *testing.T) {
	const idle = 300 * time.Millisecond
	s := resumeHost(t, resumeServer)
	timings := resumeDevice
	timings.DeviceMuxIdle, timings.CommandWait = idle, time.Second
	d := harness.NewMuxDevice(t, timings)
	d.Koko("join", s.joinCode).WantSuccess(t)
	p := d.Proxy(nil, "tunnel", "web1.koko")
	say := shell(t, sshOver(t, p, "web1.koko", gatewayHostKey(t, s.h)))
	if got := say("echo one"); got != "one\n" {
		t.Fatalf("the shell printed %q, want one", got)
	}
	// Under load, the Device Mux koko join started may idle out before koko
	// tunnel connects, and koko tunnel then starts another: only the one it
	// holds now counts.
	stopped := strings.Count(d.MuxLog(), "device mux stopped")

	// The koko server goes out of reach: the path stalls, and a new
	// connection finds nothing there.
	s.fwd.To(recordTries(t).addr())
	s.fwd.Stall()
	waitLog(t, d, `msg="device connection dropped"`)
	eventually(t, "koko status counts the SSH Session waiting to Resume", func() bool {
		st, _ := status(t, d.Koko)
		return st.DeviceMux != nil && st.DeviceMux.State == "reconnecting" && st.DeviceMux.WaitingToResume == 1
	})
	if r := d.Koko("status"); !strings.Contains(r.Stdout, "\nResuming:        1 SSH Session waits to Resume\n") {
		t.Errorf("koko status says\n%s\nwith no line for the SSH Session waiting to Resume", r.Stdout)
	}
	time.Sleep(3 * idle)
	if n := strings.Count(d.MuxLog(), "device mux stopped"); n != stopped {
		t.Fatalf("the Device Mux stopped under a Tunnel waiting to Resume; it logged:\n%s", d.MuxLog())
	}

	s.fwd.To(s.listen)
	if got := say("echo two"); got != "two\n" {
		t.Errorf("once the koko server answered again the shell printed %q, want two", got)
	}
	eventually(t, "koko status counts no SSH Session waiting to Resume once the Tunnel Resumed", func() bool {
		st, _ := status(t, d.Koko)
		return st.DeviceMux != nil && st.DeviceMux.State == "connected" && st.DeviceMux.WaitingToResume == 0
	})
}

// koko disconnect ends an SSH Session for good: koko tunnel says that koko
// disconnect ended it, and the Tunnel does not Resume, so the SSH Session
// ends once its grace runs out.
func TestKokoDisconnectEndsAnSSHSessionWithoutAResume(t *testing.T) {
	timings := resumeServer
	timings.Grace = 300 * time.Millisecond
	s := resumeHost(t, timings)
	d := harness.NewMuxDevice(t, resumeDevice)
	d.Koko("join", s.joinCode).WantSuccess(t)
	p := d.Proxy(nil, "tunnel", "web1.koko")
	say := shell(t, sshOver(t, p, "web1.koko", gatewayHostKey(t, s.h)))
	if got := say("echo one"); got != "one\n" {
		t.Fatalf("the shell printed %q, want one", got)
	}

	if out, _ := kokoDisconnect(t, d); out.CommandsEnded != 1 {
		t.Errorf("koko disconnect ended %d commands, want koko tunnel", out.CommandsEnded)
	}
	if r := p.Wait(); r.Status != 1 || r.Stderr != "koko: koko disconnect ended the SSH Session to web1\n" {
		t.Errorf("koko tunnel exited %d with stderr %q, want 1 and that koko disconnect ended it", r.Status, r.Stderr)
	}
	wantDetached(t, s.h)
	waitServerLog(t, s.h, `msg="ssh session ended" .* reason="grace over"\n`)
	if strings.Contains(s.h.Log(), "ssh session resume") {
		t.Errorf("a Tunnel koko disconnect ended tried to Resume:\n%s", s.h.Log())
	}
}

// A Host whose sshd sets ClientAliveInterval keeps a Detached SSH Session:
// the Gateway answers its keepalives on the session channel itself, which
// the Detached client could not, so the Host never ends the SSH Session
// koko Resumes.
func TestAHostWithClientAliveIntervalKeepsADetachedSSHSession(t *testing.T) {
	// The Host gives up on a client that leaves 3 probes 50 ms apart
	// unanswered: well within the time the Tunnel stays Detached.
	const interval = 50 * time.Millisecond
	s := resumeHost(t, resumeServer, sshhost.ClientAlive(interval, 3))
	d := harness.NewDevice(t)
	d.UseTimings(resumeDevice)
	d.Koko("join", s.joinCode).WantSuccess(t)
	p := d.Proxy(nil, "tunnel", "web1")
	say := shell(t, sshOver(t, p, "web1", gatewayHostKey(t, s.h)))
	if got := say("echo one"); got != "one\n" {
		t.Fatalf("the shell printed %q, want one", got)
	}

	// The cut falls on the koko server's next bytes, such as its probe of
	// the quiet device, and the device finds nothing to reconnect to.
	s.fwd.To(recordTries(t).addr())
	s.fwd.CutWhen(func() bool { return true })
	wantDetached(t, s.h)
	// Long enough for the Host to give up on a client that leaves its
	// probes unanswered, twice over.
	time.Sleep(10 * interval)
	s.fwd.To(s.listen)
	if got := say("echo two"); got != "two\n" {
		t.Errorf("after the Host's probes the shell printed %q, want two", got)
	}
	if strings.Contains(s.h.Log(), `msg="ssh session ended"`) {
		t.Errorf("the SSH Session ended while Detached:\n%s", s.h.Log())
	}
}

// A resume names the Tunnel by its id, but a Tunnel Resumes only on a
// Device Connection of the Device Session that opened it: another Device
// Session's resume is refused as for a Tunnel the koko server does not
// hold, and the SSH Session carries on.
func TestAResumeFromAnotherDeviceSessionIsRefused(t *testing.T) {
	s := resumeHost(t, resumeServer)
	d := harness.NewDevice(t)
	d.Koko("join", s.joinCode).WantSuccess(t)
	p := d.Proxy(nil, "tunnel", "web1")
	say := shell(t, sshOver(t, p, "web1", gatewayHostKey(t, s.h)))
	started := waitServerLog(t, s.h, `msg="ssh session started" tunnel=([0-9a-f]{32}) `)
	var id [wire.TunnelIDSize]byte
	if _, err := hex.Decode(id[:], []byte(started[1])); err != nil {
		t.Fatal(err)
	}

	other, _ := joinAs(t, deviceAdd(t, s.h, "thief"))
	_, err := other.Dial().OpenChannel(t.Context(), wire.ChannelTunnel, wire.TunnelResume(id, 0))
	if e, ok := errors.AsType[*ssh.OpenChannelError](err); !ok || e.Reason != ssh.Prohibited || e.Message != "" {
		t.Errorf("another Device Session's resume: %v, want it refused as a Tunnel the koko server does not hold", err)
	}
	waitServerLog(t, s.h, `msg="ssh session resume refused" tunnel=`+started[1]+` .* reason="unknown tunnel"`)
	if got := say("echo still here"); got != "still here\n" {
		t.Errorf("after the refused resume the shell printed %q, want it to carry on", got)
	}
}
