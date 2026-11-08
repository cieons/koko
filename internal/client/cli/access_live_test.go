package cli_test

import (
	"io"
	"strings"
	"testing"
	"time"

	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/harness/sshhost"
	"github.com/cieons/koko/internal/shared/timing"
)

// managedHostIn records an in-process Host as the Host named hostName in the
// Space spaceName, as the Local caller, and Enrolls it.
func managedHostIn(t *testing.T, h *harness.Harness, hostName, spaceName string) *sshhost.Host {
	t.Helper()
	host := sshhost.Start(t, sshhost.Password(hostPassword))
	addHostAt(t, h.Koko, hostName, host, "space="+spaceName)
	enroll(t, h, hostName)
	return host
}

// liveSSH is an SSH Session through koko tunnel, as ssh runs it over its
// ProxyCommand, running cat on the Host, which ends with the SSH Session.
type liveSSH struct {
	t              *testing.T
	proxy          *harness.Proxy
	stdin          io.Writer
	stdout, stderr *firstLineWriter
	ended          chan error
}

// openLiveSSH opens an SSH Session to the Host named hostName through koko
// tunnel p, and waits until cat runs there.
func openLiveSSH(t *testing.T, h *harness.Harness, p *harness.Proxy, hostName string) *liveSSH {
	t.Helper()
	s := openSession(t, sshOver(t, p, hostName, gatewayHostKey(t, h)))
	l := &liveSSH{
		t: t, proxy: p,
		stdout: &firstLineWriter{ready: make(chan struct{})}, stderr: &firstLineWriter{ready: make(chan struct{})},
		ended: make(chan error, 1),
	}
	s.Stdout, s.Stderr = l.stdout, l.stderr
	stdin, err := s.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	l.stdin = stdin
	if err := s.Start("cat"); err != nil {
		t.Fatal(err)
	}
	go func() { l.ended <- s.Wait() }()
	l.echo("started")
	return l
}

// echo fails the test unless line, written to the SSH Session, comes back
// from cat within 10 s: the SSH Session goes on.
func (l *liveSSH) echo(line string) {
	l.t.Helper()
	if _, err := io.WriteString(l.stdin, line+"\n"); err != nil {
		l.t.Fatalf("write %q to the SSH Session: %v (its stderr: %q)", line, err, l.stderr.String())
	}
	eventually(l.t, "the SSH Session echoes "+line, func() bool { return strings.Contains(l.stdout.String(), line+"\n") })
}

// wantEnded fails the test unless the SSH Session ends within 10 s with
// line, and only line, on its stderr, and koko tunnel then exits 0 with
// nothing on its own, as the Gateway closed the Tunnel.
func (l *liveSSH) wantEnded(line string) {
	l.t.Helper()
	select {
	case err := <-l.ended:
		if err == nil || l.stderr.String() != line {
			l.t.Errorf("the SSH Session ended with %v and stderr %q; want a failure and %q", err, l.stderr.String(), line)
		}
	case <-time.After(10 * time.Second):
		l.t.Fatalf("the SSH Session is still open 10 s on; want it ended with %q", line)
	}
	if r := l.proxy.Wait(); r.Status != 0 || r.Stderr != "" {
		l.t.Errorf("koko tunnel exited %d with stderr %q; want 0 and nothing", r.Status, r.Stderr)
	}
}

// wantOpen fails the test unless the SSH Session goes on, echoing line.
func (l *liveSSH) wantOpen(line string) {
	l.t.Helper()
	select {
	case err := <-l.ended:
		l.t.Fatalf("the SSH Session ended with %v and stderr %q; want it open", err, l.stderr.String())
	default:
	}
	l.echo(line)
}

// Revoking a Device Session ends each of its live SSH Sessions with one
// line naming the revocation, on the session's stderr through ssh and koko
// exec alike, over a direct Device Connection and the Device Mux, before
// its Device Connections close; another Device Session's SSH Session to the
// same Host goes on until its own revocation, and the Owner's goes on.
func TestRevokingADeviceSessionEndsItsSSHSessions(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	addSpaceWithMembers(t, h, "ops", "bob")
	laptop := h.Join("bob")
	managedHostIn(t, h, "web1", "ops")
	desktop := harness.NewMuxDevice(t, timing.Timings{})
	desktop.Koko("join", deviceAdd(t, h, "desktop", "--user", "bob").Code).WantSuccess(t)
	viaSSH := openLiveSSH(t, h, laptop.Proxy(nil, "tunnel", "web1"), "web1")
	viaExec := laptop.Background(nil, "exec", "web1", "echo ready; exec sleep 30")
	viaExec.WaitStdout("ready")
	viaMux := openLiveSSH(t, h, desktop.Proxy(nil, "tunnel", "web1"), "web1")
	owners := openLiveSSH(t, h, h.Proxy("tunnel", "web1"), "web1")

	const line = "koko: access to web1 was withdrawn: this device's Device Session was Revoked; this SSH Session is closed.\n"
	h.Koko("device", "revoke", "bob/"+readClientConfig(t, laptop).Device).WantSuccess(t)
	viaSSH.wantEnded(line)
	if r := viaExec.Wait(); r.Status != 255 || r.Stdout != "ready\n" || r.Stderr != line {
		t.Errorf("koko exec: exit status %d, stdout %q, stderr %q; want 255 and %q", r.Status, r.Stdout, r.Stderr, line)
	}
	viaMux.wantOpen("still here")

	h.Koko("device", "revoke", "bob/desktop").WantSuccess(t)
	viaMux.wantEnded(line)
	owners.wantOpen("still here")
	waitServerLog(t, h, `msg="ssh session ended" .* user=bob .* reason=revoked\n`)
}

// Removing a User ends each of their live SSH Sessions with one line naming
// the removal, before their Device Connections close.
func TestRemovingAUserEndsTheirSSHSessions(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	addSpaceWithMembers(t, h, "ops", "bob")
	laptop := h.Join("bob")
	managedHostIn(t, h, "web1", "ops")
	s := openLiveSSH(t, h, laptop.Proxy(nil, "tunnel", "web1"), "web1")

	h.Koko("user", "remove", "bob").WantSuccess(t)
	s.wantEnded("koko: access to web1 was withdrawn: bob was Removed; this SSH Session is closed.\n")
	waitServerLog(t, h, `msg="ssh session ended" .* user=bob .* reason="user removed"\n`)
}

// Taking a Member out of a Space ends their live SSH Sessions to its Hosts,
// and only those: one to a Host in a Space they still act in goes on, and
// so does the Owner's to the same Host.
func TestTakingAMemberOutOfASpaceEndsTheirSSHSessionsToItsHosts(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	addSpaceWithMembers(t, h, "ops", "bob")
	laptop := h.Join("bob")
	managedHostIn(t, h, "web1", "ops")
	managedHostIn(t, h, "web2", "bob")
	toWeb1 := openLiveSSH(t, h, laptop.Proxy(nil, "tunnel", "web1"), "web1")
	toWeb2 := openLiveSSH(t, h, laptop.Proxy(nil, "tunnel", "web2"), "web2")
	owners := openLiveSSH(t, h, h.Proxy("tunnel", "web1"), "web1")

	h.Koko("space", "member", "rm", "ops", "bob").WantSuccess(t)
	toWeb1.wantEnded("koko: access to web1 was withdrawn; this SSH Session is closed.\n")
	toWeb2.wantOpen("still here")
	owners.wantOpen("still here")
	waitServerLog(t, h, `msg="ssh session ended" .* user=bob .* host=web1 .* reason="access withdrawn"\n`)
}

// Moving a Host to a Space a Member cannot act in ends their live SSH
// Session to it with a line that names the Host as they reached it, and
// neither its new name nor either Space, and the Owner's goes on.
func TestMovingAHostOutOfReachEndsTheSSHSessionsThatLoseIt(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	addSpaceWithMembers(t, h, "ops", "bob")
	laptop := h.Join("bob")
	managedHostIn(t, h, "web1", "ops")
	h.Koko("space", "add", "vault").WantSuccess(t)
	members := openLiveSSH(t, h, laptop.Proxy(nil, "tunnel", "web1"), "web1")
	owners := openLiveSSH(t, h, h.Proxy("tunnel", "web1"), "web1")

	h.Koko("host", "rename", "web1", "payroll").WantSuccess(t)
	h.Koko("host", "move", "payroll", "vault").WantSuccess(t)
	members.wantEnded("koko: access to web1 was withdrawn; this SSH Session is closed.\n")
	owners.wantOpen("still here")
}

// Retiring a Host ends every live SSH Session to it, the Owner's included,
// with one line naming the retirement.
func TestRetiringAHostEndsItsSSHSessions(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	addSpaceWithMembers(t, h, "ops", "bob")
	laptop := h.Join("bob")
	managedHostIn(t, h, "web1", "ops")
	members := openLiveSSH(t, h, laptop.Proxy(nil, "tunnel", "web1"), "web1")
	owners := openLiveSSH(t, h, h.Proxy("tunnel", "web1"), "web1")

	h.Koko("host", "retire", "web1").WantSuccess(t)
	const line = "koko: access to web1 was withdrawn: web1 was Retired; this SSH Session is closed.\n"
	members.wantEnded(line)
	owners.wantEnded(line)
	waitServerLog(t, h, `msg="ssh session ended" .* reason="host retired"\n`)
}

// A change of Host Account sends a Managed Host back to Registered, which
// ends every live SSH Session to it.
func TestAHostSentBackToRegisteredEndsItsSSHSessions(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	managedHost(t, h, "web1")
	s := openLiveSSH(t, h, h.Proxy("tunnel", "web1"), "web1")

	h.Koko("host", "set", "web1", "account=ops").WantSuccess(t)
	s.wantEnded("koko: access to web1 was withdrawn; this SSH Session is closed.\n")
	waitServerLog(t, h, `msg="ssh session ended" .* reason="host registered"\n`)
}

// A change that keeps access interrupts no live SSH Session: renaming the
// Host, changing its address, port, Note and Attributes, moving it to
// another Space the Member acts in, and revoking another Device Session.
func TestAChangeThatKeepsAccessInterruptsNothing(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	addSpaceWithMembers(t, h, "ops", "bob")
	laptop := h.Join("bob")
	managedHostIn(t, h, "web1", "ops")
	h.Koko("space", "add", "dev").WantSuccess(t)
	h.Koko("space", "member", "add", "dev", "bob").WantSuccess(t)
	desktop, _ := joinAs(t, deviceAdd(t, h, "desktop", "--user", "bob"))
	s := openLiveSSH(t, h, laptop.Proxy(nil, "tunnel", "web1"), "web1")

	for _, change := range [][]string{
		{"host", "rename", "web1", "web9"},
		{"host", "set", "web9", "address=localhost", "port=2222", "provider=hetzner"},
		{"host", "set", "web9", "--note-file", "-"},
		{"host", "move", "web9", "dev"},
		{"device", "revoke", "bob/" + readClientConfig(t, desktop).Device},
	} {
		h.KokoStdin("keep it", change...).WantSuccess(t)
		s.wantOpen("after " + strings.Join(change, " "))
	}
	// The move is the one change the Gateway checks again, after it
	// commits: the check itself, not the echo, says it kept the SSH Session.
	waitServerLog(t, h, `msg="access checked again" ssh_sessions=1 ended=0\n`)
	s.wantOpen("after the check")
	if log := h.Log(); strings.Contains(log, `msg="ssh session ended"`) {
		t.Errorf("the log says an SSH Session ended:\n%s", log)
	}
}
