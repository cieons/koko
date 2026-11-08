package cli_test

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/cieons/koko/internal/client/serverconn"
	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/harness/sshhost"
	"github.com/cieons/koko/internal/shared/timing"
)

// sshAs runs an SSH client over the stdio of koko tunnel p as ssh does with
// the user name user, as in user@web1.koko, checking the Gateway's host key
// against pinned. It returns the client, or the banner the Gateway refused
// it with and the error.
func sshAs(t *testing.T, p *harness.Proxy, sshName, user, pinned string) (*ssh.Client, string, error) {
	t.Helper()
	var banner string
	conn, chans, reqs, err := ssh.NewClientConn(p.Conn(), sshName, &ssh.ClientConfig{
		User: user,
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			if got := ssh.FingerprintSHA256(key); got != pinned {
				return fmt.Errorf("the Gateway presented the host key %s, want the koko server's %s", got, pinned)
			}
			return nil
		},
		BannerCallback: func(message string) error {
			banner += message
			return nil
		},
	})
	if err != nil {
		return nil, banner, err
	}
	c := ssh.NewClient(conn, chans, reqs)
	t.Cleanup(func() { _ = c.Close() })
	return c, banner, nil
}

// The Gateway takes the user name koko, which koko's ssh_config sets, and
// the Host Account's own name, and refuses any other with a banner saying
// how the Host is reached.
func TestTheGatewayTakesOnlyKokoAndTheHostAccountAsTheUserName(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	tunnelHost(t, h, "web1")
	pinned := gatewayHostKey(t, h)

	for _, user := range []string{"koko", "deploy"} {
		c, banner, err := sshAs(t, h.Proxy("tunnel", "web1.koko"), "web1.koko", user, pinned)
		if err != nil {
			t.Fatalf("ssh %s@web1.koko: %v (banner %q)", user, err, banner)
		}
		if out, err := openSession(t, c).Output("echo reached"); err != nil || string(out) != "reached\n" {
			t.Errorf("ssh %s@web1.koko printed %q (%v), want it to reach web1", user, out, err)
		}
	}

	p := h.Proxy("tunnel", "web1.koko")
	_, banner, err := sshAs(t, p, "web1.koko", "root", pinned)
	if want := "koko: web1 is reached as its Host Account \"deploy\"; leave out \"root@\".\n"; err == nil || banner != want {
		t.Errorf("ssh root@web1.koko got the banner %q (%v), want %q and a refusal", banner, err, want)
	}
	waitServerLog(t, h, `level=INFO msg="ssh session refused" tunnel=[0-9a-f]{32} name=web1.koko device_session="unix socket" addr=\S+ reason="user name" ssh_user=root\n`)
}

// When the Gateway cannot log in to a Host, the banner says why: the Host is
// down, which the Gateway tried 3 times, waiting between tries; its host key
// is not the pinned one, with both fingerprints and the two ways on, tried
// once and sent no login; or it refused the koko key, tried once. A network
// error that goes away is retried and nobody sees it.
func TestTheGatewaySaysWhyItCouldNotLogInToTheHost(t *testing.T) {
	t.Parallel()
	h := harness.Start(t, harness.Timings(timing.Timings{HostConnectWait: time.Millisecond}))
	_, deadPort, err := net.SplitHostPort(harness.FreeAddr(t))
	if err != nil {
		t.Fatal(err)
	}
	managedHost(t, h, "down")
	h.Koko("host", "set", "down", "port="+deadPort).WantSuccess(t)

	pinned := managedHost(t, h, "rebuilt")
	rebuilt := sshhost.Start(t, sshhost.Password(hostPassword))
	h.Koko("host", "set", "rebuilt", "port="+strconv.Itoa(rebuilt.Port())).WantSuccess(t)

	revoked := managedHost(t, h, "revoked")
	if err := os.WriteFile(filepath.Join(revoked.Home(), ".ssh", "authorized_keys"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	logins := len(revoked.Logins())

	flaky := managedHost(t, h, "flaky")
	flaky.ResetNext(2)
	conns := flaky.Conns()

	wantRefused(t, h.Koko("exec", "down", "true"),
		"koko: down is down or unreachable: koko could not connect to it; this SSH Session did not start.\n")
	waitServerLog(t, h, `level=WARN msg="ssh session refused" tunnel=[0-9a-f]{32} name=down device_session="unix socket" addr=\S+ reason="host unreachable" err=".*after 3 attempts`)

	wantRefused(t, h.Koko("exec", "rebuilt", "true"), fmt.Sprintf(
		"koko: the host key of rebuilt does not match the one pinned at Enroll (pinned %s, offered %s); koko did not log in. "+
			"If rebuilt was rebuilt, pin its new key with koko host enroll rebuilt --host-key <the fingerprint its provider shows>, "+
			"or with --replace-host-key, which trusts whatever key it offers.\n",
		fingerprint(pinned), fingerprint(rebuilt)))
	waitServerLog(t, h, `level=WARN msg="ssh session refused" tunnel=[0-9a-f]{32} name=rebuilt device_session="unix socket" addr=\S+ reason="host key mismatch" `+
		`pinned=`+regexp.QuoteMeta(fingerprint(pinned))+` offered=`+regexp.QuoteMeta(fingerprint(rebuilt))+`\n`)
	if rebuilt.Conns() != 1 || len(rebuilt.Logins()) != 0 {
		t.Errorf("the rebuilt Host got %d connections and the logins %+v, want 1 and none", rebuilt.Conns(), rebuilt.Logins())
	}

	wantRefused(t, h.Koko("exec", "revoked", "true"),
		"koko: revoked refused the koko key for its Host Account \"deploy\"; this SSH Session did not start.\n")
	waitServerLog(t, h, `level=WARN msg="ssh session refused" tunnel=[0-9a-f]{32} name=revoked device_session="unix socket" addr=\S+ reason="login refused" err=`)
	if got := revoked.Logins()[logins:]; len(got) != 1 {
		t.Errorf("the Host that refused the koko key got the logins %+v, want one", got)
	}

	if r := h.Koko("exec", "flaky", "echo", "reached"); r.Status != 0 || r.Stdout != "reached\n" || r.Stderr != "" {
		t.Errorf("through 2 reset connections: exit status %d, stdout %q, stderr %q; want flaky reached", r.Status, r.Stdout, r.Stderr)
	}
	if got := flaky.Conns() - conns; got != 3 {
		t.Errorf("the Host that reset its connections was connected to %d times, want 3: 2 reset, then the login", got)
	}
}

// A Device Session may have 64 SSH Sessions open at once: the 65th is
// refused with a banner, without a connection to the Host, until one of the
// 64 ends. Another Device Session of the same User starts its own.
func TestADeviceSessionHasAtMost64SSHSessions(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	host := tunnelHost(t, h, "web1")
	pinned := gatewayHostKey(t, h)
	laptop := h.Join(harness.Owner)

	open := make([]*ssh.Client, 64)
	for i := range open {
		open[i] = sshOver(t, laptop.Proxy(nil, "tunnel", "web1"), "web1", pinned)
	}
	conns := host.Conns()
	_, banner, err := sshAs(t, laptop.Proxy(nil, "tunnel", "web1"), "web1", "koko", pinned)
	if want := "koko: this Device Session already has 64 SSH Sessions open, the most koko allows; this SSH Session did not start.\n"; err == nil || banner != want {
		t.Errorf("the 65th SSH Session got the banner %q (%v), want %q", banner, err, want)
	}
	if got := host.Conns(); got != conns {
		t.Errorf("the 65th SSH Session made %d connections to the Host, want none", got-conns)
	}
	waitServerLog(t, h, `level=INFO msg="ssh session refused" tunnel=[0-9a-f]{32} name=web1 device_session=\S+ addr=\S+ reason="ssh session limit"\n`)

	if out, err := openSession(t, sshOver(t, h.Join(harness.Owner).Proxy(nil, "tunnel", "web1"), "web1", pinned)).Output("echo reached"); err != nil || string(out) != "reached\n" {
		t.Errorf("another Device Session printed %q (%v), want it to reach web1", out, err)
	}

	_ = open[0].Close()
	eventually(t, "a 65th SSH Session starts once one of the 64 ended", func() bool {
		c, _, err := sshAs(t, laptop.Proxy(nil, "tunnel", "web1"), "web1", "koko", pinned)
		if err == nil {
			_ = c.Close()
		}
		return err == nil
	})
}

// Agent and X11 forwarding are refused, each with one line on the session's
// stderr, and never reach the Host; the session carries on.
func TestTheGatewayRefusesAgentAndX11Forwarding(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	host := tunnelHost(t, h, "web1")
	c := sshOver(t, h.Proxy("tunnel", "web1"), "web1", gatewayHostKey(t, h))
	s := openSession(t, c)
	var stdout, stderr bytes.Buffer
	s.Stdout, s.Stderr = &stdout, &stderr

	if ok, err := s.SendRequest("auth-agent-req@openssh.com", true, nil); ok || err != nil {
		t.Errorf("ssh -A: the Gateway answered %v (%v), want a refusal", ok, err)
	}
	x11 := ssh.Marshal(struct {
		Single   bool
		Protocol string
		Cookie   string
		Screen   uint32
	}{false, "MIT-MAGIC-COOKIE-1", "0123456789abcdef", 0})
	if ok, err := s.SendRequest("x11-req", true, x11); ok || err != nil {
		t.Errorf("ssh -X: the Gateway answered %v (%v), want a refusal", ok, err)
	}
	if err := s.Run("echo carried on"); err != nil || stdout.String() != "carried on\n" {
		t.Errorf("after the refusals the session printed %q (%v), want it to carry on", stdout.String(), err)
	}
	if want := "koko: agent forwarding is not available through koko.\nkoko: X11 forwarding is not available through koko.\n"; stderr.String() != want {
		t.Errorf("the session's stderr is %q, want %q", stderr.String(), want)
	}
	for _, r := range host.Requests() {
		if r.Type == "auth-agent-req@openssh.com" || r.Type == "x11-req" {
			t.Errorf("the Host received a %s request", r.Type)
		}
	}
}

// A direct-tcpip channel, which ssh -J and -W would open on the koko
// server's own SSH, is refused on a Device Connection and on the Unix socket
// alike, saying that only the koko CLI opens Tunnels.
func TestTheKokoServerRefusesDirectTCPIP(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	tunnelHost(t, h, "web1")
	socket, err := serverconn.DialSocket(t.Context(), h.Socket())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = socket.Close() })
	payload := ssh.Marshal(struct {
		Host     string
		Port     uint32
		OrigHost string
		OrigPort uint32
	}{"web1.koko", 22, "127.0.0.1", 50000})

	for way, conn := range map[string]serverconn.Conn{"a Device Connection": h.Join(harness.Owner).Dial(), "the Unix socket": socket} {
		_, err := conn.OpenChannel(t.Context(), "direct-tcpip", payload)
		e, ok := errors.AsType[*ssh.OpenChannelError](err)
		if want := "only the koko CLI opens Tunnels to Hosts: use ssh <host>.koko or koko exec <host> <command>"; !ok || e.Reason != ssh.Prohibited || e.Message != want {
			t.Errorf("direct-tcpip on %s: %v, want it refused as prohibited with %q", way, err, want)
		}
	}
}

// The Gateway probes a Host it has heard nothing from for a while with a
// keepalive@openssh.com, so a quiet SSH Session, and a Host that stops
// answering for less than the end's wait, pass unnoticed.
func theGatewayProbesAQuietHost(t *testing.T) {
	h := harness.Start(t, harness.Timings(timing.Timings{HostProbe: 50 * time.Millisecond}))
	host := managedHost(t, h, "web1")
	p := h.Background(nil, "exec", "web1", "echo ready; sleep 0.5; echo after the blip")
	p.WaitStdout("ready")

	host.Freeze()
	time.Sleep(200 * time.Millisecond)
	host.Thaw()
	eventually(t, "the Host receives a keepalive@openssh.com", func() bool {
		return slices.ContainsFunc(host.Requests(), func(r sshhost.Request) bool { return !r.Channel && r.Type == "keepalive@openssh.com" })
	})
	if r := p.Wait(); r.Status != 0 || r.Stdout != "ready\nafter the blip\n" || r.Stderr != "" {
		t.Errorf("exit status %d, stdout %q, stderr %q; want the command to carry on after the blip", r.Status, r.Stdout, r.Stderr)
	}
}

// A Host that sends nothing for the end's wait, the probes unanswered, ends
// its SSH Session: the Gateway writes one line naming the Host on the
// session's stderr, and koko exec, which shows it, exits 255 at once and
// adds nothing.
func theGatewayEndsTheSSHSessionOfAHostThatStopsAnswering(t *testing.T) {
	// Long enough that a Host still answering, through the Enroll and the
	// command's start, is never taken for silent under load.
	h := harness.Start(t, harness.Timings(timing.Timings{HostProbe: 50 * time.Millisecond, HostDead: 1500 * time.Millisecond}))
	host := managedHost(t, h, "web1")
	p := h.Background(nil, "exec", "web1", "echo ready; exec sleep 30")
	p.WaitStdout("ready")

	host.Freeze()
	if r := p.Wait(); r.Status != 255 || r.Stdout != "ready\n" || r.Stderr != "koko: lost the connection to web1 (no answer for 1.5 s); this SSH Session has ended.\n" {
		t.Errorf("exit status %d, stdout %q, stderr %q; want 255 and the line naming web1", r.Status, r.Stdout, r.Stderr)
	}
	waitServerLog(t, h, `msg="ssh session ended" .* reason="Host leg lost"\n`)
}

// A Host that closes its connection ends its SSH Sessions with one line on
// each session's stderr, through koko exec and ssh alike, after what the
// Host sent before.
func TestTheGatewaySaysWhenTheHostClosedTheConnection(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	host := managedHost(t, h, "web1")
	p := h.Background(nil, "exec", "web1", "echo ready; exec sleep 30")
	p.WaitStdout("ready")
	tunnel := h.Proxy("tunnel", "web1")
	s := openSession(t, sshOver(t, tunnel, "web1", gatewayHostKey(t, h)))
	stdout := &firstLineWriter{ready: make(chan struct{})}
	var stderr bytes.Buffer
	s.Stdout, s.Stderr = stdout, &stderr
	if err := s.Start("echo ready; exec sleep 30"); err != nil {
		t.Fatal(err)
	}
	<-stdout.ready

	host.DropConns()
	const line = "koko: web1 closed the connection.\n"
	if r := p.Wait(); r.Status != 255 || r.Stdout != "ready\n" || r.Stderr != line {
		t.Errorf("koko exec: exit status %d, stdout %q, stderr %q; want 255 and %q", r.Status, r.Stdout, r.Stderr, line)
	}
	if err := s.Wait(); err == nil || stdout.String() != "ready\n" || stderr.String() != line {
		t.Errorf("ssh: the session ended with %v, stdout %q, stderr %q; want a failure and %q", err, stdout.String(), stderr.String(), line)
	}
	if r := tunnel.Wait(); r.Status != 0 || r.Stderr != "" {
		t.Errorf("koko tunnel exited %d with stderr %q; want 0 and nothing, as the Gateway closed the Tunnel", r.Status, r.Stderr)
	}
	waitServerLog(t, h, `msg="ssh session ended" .* reason="Host closed"\n`)
}

// A koko server stop writes the restart notice on the stderr of each open
// session of every SSH Session, then closes the Tunnels with it before the
// Device Connections close, so koko tunnel exits 0 at once, over the Unix
// socket, a direct Device Connection and the Device Mux alike, as the
// Gateway closed the Tunnel.
func TestAKokoServerStopTellsEachSSHSessionItIsRestarting(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	tunnelHost(t, h, "web1")
	pinned := gatewayHostKey(t, h)
	muxed := harness.NewMuxDevice(t, timing.Timings{})
	muxed.Koko("join", deviceAdd(t, h, "laptop").Code).WantSuccess(t)
	proxies := map[string]*harness.Proxy{
		"the Unix socket":            h.Proxy("tunnel", "web1"),
		"a direct Device Connection": h.Join(harness.Owner).Proxy(nil, "tunnel", "web1"),
		"the Device Mux":             muxed.Proxy(nil, "tunnel", "web1"),
	}
	stderrs := map[string]*bytes.Buffer{}
	sessions := map[string]*ssh.Session{}
	for way, p := range proxies {
		s := openSession(t, sshOver(t, p, "web1", pinned))
		stderrs[way] = &bytes.Buffer{}
		s.Stderr = stderrs[way]
		if err := s.Start("exec sleep 30"); err != nil {
			t.Fatal(err)
		}
		sessions[way] = s
	}
	eventually(t, "3 SSH Sessions start", func() bool { return strings.Count(h.Log(), `msg="ssh session started"`) == 3 })

	h.Stop()
	const notice = "koko: the koko server is restarting; this SSH Session has ended.\n"
	for way, p := range proxies {
		if err := sessions[way].Wait(); err == nil || stderrs[way].String() != notice {
			t.Errorf("over %s the session ended with %v and stderr %q; want a failure and %q", way, err, stderrs[way].String(), notice)
		}
		if r := p.Wait(); r.Status != 0 || r.Stderr != "" {
			t.Errorf("over %s koko tunnel exited %d with stderr %q; want 0 and nothing", way, r.Status, r.Stderr)
		}
	}
}

// The koko server logs each kind of request an SSH Session makes once, by
// the Tunnel's id: a shell, an exec, each subsystem by name, and each
// forward's target, -L and -R, TCP and Unix sockets; never a command's text.
func TestTheGatewayLogsEachKindOfRequestOnce(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	tunnelHost(t, h, "web1", sshhost.Subsystem("sftp", "cat"))
	c := sshOver(t, h.Proxy("tunnel", "web1"), "web1", gatewayHostKey(t, h))
	sockets := harness.ShortTempDir(t)

	const marker = "not-for-the-log-5e1d"
	for range 2 {
		if err := openSession(t, c).Run("echo " + marker); err != nil {
			t.Fatal(err)
		}
	}
	shell := openSession(t, c)
	shell.Stdin = strings.NewReader("exit 0\n")
	if err := shell.Shell(); err != nil {
		t.Fatal(err)
	}
	if err := shell.Wait(); err != nil {
		t.Fatal(err)
	}
	if err := openSession(t, c).RequestSubsystem("sftp"); err != nil {
		t.Fatal(err)
	}
	ln := listenEcho(t, "tcp", "127.0.0.1:0")
	for range 2 {
		conn, err := c.Dial("tcp", ln.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		wantEcho(t, "-L", conn)
	}
	unix := listenEcho(t, "unix", filepath.Join(sockets, "l.sock"))
	conn, err := c.Dial("unix", unix.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	wantEcho(t, "-L to a Unix socket", conn)
	remote, err := c.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_ = remote.Close()
	remoteUnix, err := c.ListenUnix(filepath.Join(sockets, "r.sock"))
	if err != nil {
		t.Fatal(err)
	}
	_ = remoteUnix.Close()

	id := waitServerLog(t, h, `msg="ssh session started" tunnel=([0-9a-f]{32}) `)[1]
	for _, line := range []string{
		`request=exec`,
		`request=shell`,
		`request=subsystem subsystem=sftp`,
		`request=-L target=` + regexp.QuoteMeta(ln.Addr().String()),
		`request=-L target=` + regexp.QuoteMeta(unix.Addr().String()),
		`request=-R target=127\.0\.0\.1:0`,
		`request=-R target=` + regexp.QuoteMeta(filepath.Join(sockets, "r.sock")),
	} {
		want := `level=INFO msg="ssh session request" tunnel=` + id + ` host=web1 ` + line + `\n`
		waitServerLog(t, h, want)
		if n := len(regexp.MustCompile(want).FindAllString(h.Log(), -1)); n != 1 {
			t.Errorf("the log holds %d lines matching %s, want 1", n, want)
		}
	}
	if strings.Contains(h.Log(), marker) {
		t.Errorf("the log holds a command's text:\n%s", h.Log())
	}
}

// A Host the caller cannot see gets the unknown name's banner whatever user
// name they ask for, so the user-name banner never names the Host Account
// of a Host outside their Spaces.
func TestTheUserNameBannerTellsNothingOfAnUnseenHost(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	tunnelHost(t, h, "web1")
	h.Koko("user", "invite", "bob").WantSuccess(t)
	bob := h.Join("bob")

	_, banner, err := sshAs(t, bob.Proxy(nil, "tunnel", "web1.koko"), "web1.koko", "root", gatewayHostKey(t, h))
	if want := "koko: no Host is named \"web1\".\n"; err == nil || banner != want {
		t.Errorf("root@ an unseen Host got the banner %q (%v), want %q", banner, err, want)
	}
}
