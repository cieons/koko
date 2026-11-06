package cli_test

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/harness/sshhost"
	"github.com/cieons/koko/internal/shared/timing"
)

// sshOver runs an SSH client over the stdio of koko tunnel p, as ssh runs
// over its ProxyCommand with koko's ssh_config: "none" authentication as
// koko, and the host key checked against the koko server's own, whose
// fingerprint is pinned, as the alias koko in koko's known_hosts.
func sshOver(t *testing.T, p *harness.Proxy, sshName, pinned string) *ssh.Client {
	t.Helper()
	conn, chans, reqs, err := ssh.NewClientConn(p.Conn(), sshName, &ssh.ClientConfig{
		User: "koko",
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			if got := ssh.FingerprintSHA256(key); got != pinned {
				return fmt.Errorf("the Gateway presented the host key %s, want the koko server's %s", got, pinned)
			}
			return nil
		},
	})
	if err != nil {
		t.Fatalf("ssh through koko tunnel %s: %v (koko tunnel's stderr so far: %q)", sshName, err, p.Wait().Stderr)
	}
	c := ssh.NewClient(conn, chans, reqs)
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// gatewayHostKey returns the fingerprint of the koko server's host key, the one
// a Join Code pins and the Gateway presents inside every Tunnel.
func gatewayHostKey(t *testing.T, h *harness.Harness) string {
	t.Helper()
	return parseServer(t, h.Koko("server", "show", "--json")).HostKeyFingerprint
}

// tunnelHost records an in-process Host started with opts as the Host named
// hostName, as the Local caller, and Enrolls it, so the Gateway logs in to
// it with the koko key.
func tunnelHost(t *testing.T, h *harness.Harness, hostName string, opts ...sshhost.Option) *sshhost.Host {
	t.Helper()
	host := sshhost.Start(t, append([]sshhost.Option{sshhost.Password(hostPassword)}, opts...)...)
	addHostAt(t, h.Koko, hostName, host)
	enroll(t, h, hostName)
	return host
}

// openSession opens a session channel on c, closed when the test ends.
func openSession(t *testing.T, c *ssh.Client) *ssh.Session {
	t.Helper()
	s, err := c.NewSession()
	if err != nil {
		t.Fatalf("open a session through koko tunnel: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// hostRequest waits until the Host has received a request of type typ, and
// returns its payload. It fails the test after 10 s.
func hostRequest(t *testing.T, host *sshhost.Host, typ string) []byte {
	t.Helper()
	var payload []byte
	eventually(t, "the Host receives a "+typ+" request", func() bool {
		for _, r := range host.Requests() {
			if !r.Channel && r.Type == typ {
				payload = r.Payload
				return true
			}
		}
		return false
	})
	return payload
}

// firstLineWriter collects what a session prints, and closes ready once it holds
// a whole line.
type firstLineWriter struct {
	mu    sync.Mutex
	buf   bytes.Buffer
	ready chan struct{}
}

func (w *firstLineWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	had := bytes.Contains(w.buf.Bytes(), []byte("\n"))
	w.buf.Write(p)
	if !had && bytes.Contains(w.buf.Bytes(), []byte("\n")) {
		close(w.ready)
	}
	return len(p), nil
}

func (w *firstLineWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}

// Every session request ssh makes passes through koko tunnel and the
// Gateway to the Host as ssh sent it: env, a pty, a shell, a window change,
// a signal, and a subsystem such as sftp; the Host's answers come back,
// exit statuses and signals included.
func TestKokoTunnelPassesEverySessionRequestToTheHost(t *testing.T) {
	h := harness.Start(t)
	host := tunnelHost(t, h, "web1", sshhost.Subsystem("sftp", "cat"))
	c := sshOver(t, h.Proxy("tunnel", "web1"), "web1", gatewayHostKey(t, h))

	t.Run("env, a pty, a shell and a window change", func(t *testing.T) {
		s := openSession(t, c)
		if err := s.Setenv("KOKO_TEST", "passed"); err != nil {
			t.Fatalf("env: %v", err)
		}
		if err := s.RequestPty("xterm-koko", 40, 80, ssh.TerminalModes{ssh.ECHO: 0}); err != nil {
			t.Fatalf("pty-req: %v", err)
		}
		stdin, err := s.StdinPipe()
		if err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		s.Stdout = &out
		if err := s.Shell(); err != nil {
			t.Fatalf("shell: %v", err)
		}
		if err := s.WindowChange(50, 132); err != nil {
			t.Fatalf("window-change: %v", err)
		}
		var size struct{ Columns, Rows, Width, Height uint32 }
		if err := ssh.Unmarshal(hostRequest(t, host, "window-change"), &size); err != nil || size.Columns != 132 || size.Rows != 50 {
			t.Errorf("the Host received the window change %+v (%v), want 132 columns and 50 rows", size, err)
		}
		if _, err := io.WriteString(stdin, "echo $TERM $KOKO_TEST; exit 5\n"); err != nil {
			t.Fatal(err)
		}
		exit, ok := errors.AsType[*ssh.ExitError](s.Wait())
		if !ok || exit.ExitStatus() != 5 || out.String() != "xterm-koko passed\n" {
			t.Errorf("the shell printed %q and ended with %v; want the pty's TERM, the env and exit status 5", out.String(), exit)
		}
		var pty struct {
			Term                         string
			Columns, Rows, Width, Height uint32
			Modes                        string
		}
		if err := ssh.Unmarshal(hostRequest(t, host, "pty-req"), &pty); err != nil || pty.Term != "xterm-koko" || pty.Columns != 80 || pty.Rows != 40 || pty.Modes == "" {
			t.Errorf("the Host received the pty-req %+v (%v), want xterm-koko, 80 by 40, with its modes", pty, err)
		}
	})

	t.Run("a signal", func(t *testing.T) {
		s := openSession(t, c)
		out := &firstLineWriter{ready: make(chan struct{})}
		s.Stdout = out
		if err := s.Start("echo ready; exec sleep 30"); err != nil {
			t.Fatal(err)
		}
		select {
		case <-out.ready:
		case <-time.After(10 * time.Second):
			t.Fatalf("the command printed %q in 10 s, want ready", out.String())
		}
		if err := s.Signal(ssh.SIGTERM); err != nil {
			t.Fatalf("signal: %v", err)
		}
		if exit, ok := errors.AsType[*ssh.ExitError](s.Wait()); !ok || exit.Signal() != "TERM" {
			t.Errorf("the command ended with %v, want SIGTERM, which the signal request sent it", exit)
		}
	})

	t.Run("the sftp subsystem", func(t *testing.T) {
		// An sftp client talks to its subsystem over the session's pipes.
		s := openSession(t, c)
		stdin, err := s.StdinPipe()
		if err != nil {
			t.Fatal(err)
		}
		stdout, err := s.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		if err := s.RequestSubsystem("sftp"); err != nil {
			t.Fatalf("subsystem: %v", err)
		}
		if _, err := io.WriteString(stdin, "an sftp packet\n"); err != nil {
			t.Fatal(err)
		}
		_ = stdin.Close()
		if out, err := io.ReadAll(stdout); err != nil || string(out) != "an sftp packet\n" {
			t.Errorf("the subsystem printed %q (%v), want its input back", out, err)
		}
	})
}

// listenEcho listens on network at addr, as a service on the Host's side or
// on the device, and sends back what each connection sends. It stops when
// the test ends.
func listenEcho(t *testing.T, network, addr string) net.Listener {
	t.Helper()
	var lc net.ListenConfig
	ln, err := lc.Listen(t.Context(), network, addr)
	if err != nil {
		t.Fatalf("listen on %s %s: %v", network, addr, err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go serveEcho(ln)
	return ln
}

// serveEcho sends back what each connection ln accepts sends, until ln is
// closed.
func serveEcho(ln net.Listener) {
	for {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		go func() {
			defer func() { _ = c.Close() }()
			_, _ = io.Copy(c, c)
		}()
	}
}

// wantEcho fails the test unless c, a forwarded connection to an echo
// server, sends back what is written to it; it then closes c.
func wantEcho(t *testing.T, what string, c net.Conn) {
	t.Helper()
	defer func() { _ = c.Close() }()
	msg := "through " + what + "\n"
	if _, err := io.WriteString(c, msg); err != nil {
		t.Fatalf("%s: write: %v", what, err)
	}
	got := make([]byte, len(msg))
	if _, err := io.ReadFull(c, got); err != nil || string(got) != msg {
		t.Errorf("%s sent back %q (%v), want %q", what, got, err, msg)
	}
}

// dialHostSide connects to network and addr, as a program on the Host's
// side would.
func dialHostSide(t *testing.T, network, addr string) (net.Conn, error) {
	t.Helper()
	var d net.Dialer
	return d.DialContext(t.Context(), network, addr)
}

// Every forward passes through koko tunnel and the Gateway to the Host, TCP
// and Unix sockets alike: -L and -D, which open a channel to an address on
// the Host's side, and -R, which asks the Host to listen and carries each
// connection it accepts back, until ssh cancels it.
func TestKokoTunnelPassesEveryForwardToTheHost(t *testing.T) {
	h := harness.Start(t)
	tunnelHost(t, h, "web1")
	c := sshOver(t, h.Proxy("tunnel", "web1.koko"), "web1.koko", gatewayHostKey(t, h))
	sockets := harness.ShortTempDir(t)

	t.Run("-L to a TCP port", func(t *testing.T) {
		ln := listenEcho(t, "tcp", "127.0.0.1:0")
		conn, err := c.Dial("tcp", ln.Addr().String())
		if err != nil {
			t.Fatalf("direct-tcpip: %v", err)
		}
		wantEcho(t, "-L", conn)
	})

	t.Run("-D, whose SOCKS client names the Host to reach", func(t *testing.T) {
		// ssh is the SOCKS server itself, and opens one direct-tcpip
		// channel per connection, to the name its client asked for.
		ln := listenEcho(t, "tcp", "127.0.0.1:0")
		_, port, _ := net.SplitHostPort(ln.Addr().String())
		conn, err := c.Dial("tcp", net.JoinHostPort("localhost", port))
		if err != nil {
			t.Fatalf("direct-tcpip to a name: %v", err)
		}
		wantEcho(t, "-D", conn)
	})

	t.Run("-L to a Unix socket", func(t *testing.T) {
		ln := listenEcho(t, "unix", filepath.Join(sockets, "l.sock"))
		conn, err := c.Dial("unix", ln.Addr().String())
		if err != nil {
			t.Fatalf("direct-streamlocal@openssh.com: %v", err)
		}
		wantEcho(t, "-L to a Unix socket", conn)
	})

	t.Run("-R from a TCP port", func(t *testing.T) {
		ln, err := c.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("tcpip-forward: %v", err)
		}
		go serveEcho(ln)
		conn, err := dialHostSide(t, "tcp", ln.Addr().String())
		if err != nil {
			t.Fatalf("connect to the port the Host listens on for -R: %v", err)
		}
		wantEcho(t, "-R", conn)
		_ = ln.Close()
		wantNotListening(t, "tcp", ln.Addr().String())
	})

	t.Run("-R from a Unix socket", func(t *testing.T) {
		path := filepath.Join(sockets, "r.sock")
		ln, err := c.ListenUnix(path)
		if err != nil {
			t.Fatalf("streamlocal-forward@openssh.com: %v", err)
		}
		go serveEcho(ln)
		conn, err := dialHostSide(t, "unix", path)
		if err != nil {
			t.Fatalf("connect to the Unix socket the Host listens on for -R: %v", err)
		}
		wantEcho(t, "-R from a Unix socket", conn)
		_ = ln.Close()
		wantNotListening(t, "unix", path)
	})
}

// wantNotListening waits until nothing on the Host's side listens on network
// at addr, as once ssh cancels a -R forward. It fails the test after 10 s.
func wantNotListening(t *testing.T, network, addr string) {
	t.Helper()
	eventually(t, "the Host stops listening once ssh cancels -R", func() bool {
		conn, err := dialHostSide(t, network, addr)
		if err == nil {
			_ = conn.Close()
		}
		return err != nil
	})
}

// ssh reaches a Host through koko tunnel, its ProxyCommand: the SSH Session
// runs inside the Tunnel to the Gateway, which presents the koko server's
// own host key, and koko tunnel exits 0 once ssh closes the connection, as
// the Gateway closes the Tunnel.
func TestSSHReachesAHostThroughKokoTunnel(t *testing.T) {
	h := harness.Start(t)
	managedHost(t, h, "web1")

	p := h.Proxy("tunnel", "web1.koko")
	c := sshOver(t, p, "web1.koko", gatewayHostKey(t, h))
	s, err := c.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	out, err := s.Output("echo reached")
	if err != nil || string(out) != "reached\n" {
		t.Fatalf("exec through koko tunnel printed %q (%v), want it to reach web1", out, err)
	}

	_ = c.Close()
	if r := p.Wait(); r.Status != 0 || r.Stderr != "" {
		t.Errorf("koko tunnel exited %d with stderr %q once ssh closed; want 0 and nothing", r.Status, r.Stderr)
	}
	waitServerLog(t, h, `msg="ssh session ended" .* reason="client closed"\n`)
}

// On a joined device koko tunnel's Tunnel rides the Device Mux's Device
// Connection, and an open SSH Session keeps the Device Mux from idling out,
// since koko tunnel stays connected to it; once ssh is done, it idles out.
func TestAnOpenSSHSessionKeepsTheDeviceMuxRunning(t *testing.T) {
	const idle = 300 * time.Millisecond
	h, fwd, d := muxDevice(t, timing.Timings{DeviceMuxIdle: idle})
	held := d.DialMux() // through the Enroll, however long it takes
	tunnelHost(t, h, "web1")
	p := d.Proxy(nil, "tunnel", "web1.koko")
	c := sshOver(t, p, "web1.koko", gatewayHostKey(t, h))
	_ = held.Close()

	time.Sleep(3 * idle)
	if strings.Contains(d.MuxLog(), "device mux stopped") {
		t.Fatalf("the Device Mux stopped under an open SSH Session; it logged:\n%s", d.MuxLog())
	}
	if out, err := openSession(t, c).Output("echo still here"); err != nil || string(out) != "still here\n" {
		t.Errorf("after the idle time the SSH Session printed %q (%v), want it to carry on", out, err)
	}
	if n := fwd.Conns(); n != 2 {
		t.Errorf("the device made %d connections, want the 2 it had: the join's, and the Device Mux's", n)
	}

	_ = c.Close()
	p.Wait()
	waitLog(t, d, `msg="device mux stopped" reason="nothing was connected to it for a while"`)
}

// At stdin's EOF koko tunnel half-closes the Tunnel and goes on relaying
// what the Gateway sends until the Gateway closes it: ssh that sends its
// version and closes at once still gets the Gateway's, and koko tunnel then
// exits 0.
func TestKokoTunnelHalfClosesAtStdinsEOFAndKeepsRelaying(t *testing.T) {
	h := harness.Start(t)
	tunnelHost(t, h, "web1")
	p := h.Proxy("tunnel", "web1")
	conn := p.Conn()
	if _, err := io.WriteString(conn, "SSH-2.0-OpenSSH_9.9\r\n"); err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()

	read := make(chan []byte)
	go func() {
		out, _ := io.ReadAll(conn)
		read <- out
	}()
	r := p.Wait()
	if out := <-read; r.Status != 0 || r.Stderr != "" || !bytes.HasPrefix(out, []byte("SSH-2.0-koko\r\n")) {
		t.Errorf("koko tunnel exited %d with stderr %q and relayed %q; want 0 and the Gateway's version after stdin's EOF", r.Status, r.Stderr, out)
	}
	waitServerLog(t, h, `msg="ssh session ended before it started" .* err=EOF\n`)
}

// koko tunnel ignores SIGINT, which ssh also gets when Ctrl-C is pressed
// without a tty, and which ends every other koko command: the SSH Session
// carries on.
func TestKokoTunnelIgnoresSIGINT(t *testing.T) {
	h := harness.Start(t)
	tunnelHost(t, h, "web1")
	p := h.Proxy("tunnel", "web1")
	c := sshOver(t, p, "web1", gatewayHostKey(t, h))

	p.Interrupt()
	if out, err := openSession(t, c).Output("echo still here"); err != nil || string(out) != "still here\n" {
		t.Errorf("after SIGINT the SSH Session printed %q (%v), want it to carry on", out, err)
	}
	select {
	case <-p.Done():
		t.Errorf("koko tunnel exited on SIGINT: %+v", p.Wait())
	default:
	}
}

// On SIGHUP, which ssh sends its ProxyCommand when it exits, and on SIGTERM,
// koko tunnel sends a close frame and exits 0 at once: the Gateway ends the
// SSH Session as the client's close, not as a lost Device Connection, over
// the Unix socket, a direct Device Connection and the Device Mux alike.
func TestKokoTunnelSendsACloseFrameOnSIGHUPAndSIGTERM(t *testing.T) {
	for _, tc := range []struct {
		name  string
		sig   syscall.Signal
		proxy func(h *harness.Harness) *harness.Proxy
	}{
		{"SIGHUP over the Unix socket", syscall.SIGHUP, func(h *harness.Harness) *harness.Proxy {
			return h.Proxy("tunnel", "web1")
		}},
		{"SIGTERM over the Unix socket", syscall.SIGTERM, func(h *harness.Harness) *harness.Proxy {
			return h.Proxy("tunnel", "web1")
		}},
		{"SIGHUP over a direct Device Connection", syscall.SIGHUP, func(h *harness.Harness) *harness.Proxy {
			return h.Join(harness.Owner).Proxy(nil, "tunnel", "web1")
		}},
		{"SIGHUP through the Device Mux", syscall.SIGHUP, func(h *harness.Harness) *harness.Proxy {
			d := harness.NewMuxDevice(t, timing.Timings{})
			d.Koko("join", deviceAdd(t, h, "laptop").Code).WantSuccess(t)
			return d.Proxy(nil, "tunnel", "web1")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := harness.Start(t)
			tunnelHost(t, h, "web1")
			p := tc.proxy(h)
			c := sshOver(t, p, "web1", gatewayHostKey(t, h))
			// cat runs until the SSH Session ends and closes its stdin, so
			// nothing on the Host outlives the test.
			if err := openSession(t, c).Start("cat"); err != nil {
				t.Fatal(err)
			}
			waitServerLog(t, h, `msg="ssh session started"`)

			p.Signal(tc.sig)
			if r := p.Wait(); r.Status != 0 || r.Stderr != "" {
				t.Errorf("koko tunnel exited %d with stderr %q on %v; want 0 and nothing", r.Status, r.Stderr, tc.sig)
			}
			waitServerLog(t, h, `msg="ssh session ended" .* reason="client closed"\n`)
		})
	}
}

// koko tunnel takes one SSH Name and no flag, not even --json, refusing
// anything else before it reaches the koko server. koko help, koko skill and
// the completion scripts say so, and that it is the ProxyCommand of koko's
// ssh_config, not for direct use.
func TestKokoTunnelTakesOneSSHNameAndNoFlags(t *testing.T) {
	run := func(args ...string) harness.Result { return harness.Run(t, offline(t), nil, args...) }
	for _, tc := range []struct {
		args []string
		says string
	}{
		{[]string{"tunnel"}, "koko: koko tunnel needs <ssh-name>\n"},
		{[]string{"tunnel", "web1", "web2"}, "koko: unexpected argument \"web2\": write koko tunnel <ssh-name>\n"},
		{[]string{"tunnel", "web1", "--port", "22"}, "koko: unknown flag --port for koko tunnel\n"},
		{[]string{"tunnel", "--help"}, "koko: there is no --help flag: run koko help tunnel\n"},
	} {
		if r := run(tc.args...); r.Status != 2 || r.Stdout != "" || r.Stderr != tc.says {
			t.Errorf("koko %s: exit status %d, stdout %q, stderr %q; want 2 and %q", strings.Join(tc.args, " "), r.Status, r.Stdout, r.Stderr, tc.says)
		}
	}
	msg := run("tunnel", "--json", "web1").WantError(t, 2, "invalid_usage")
	if want := "koko tunnel takes no flags, not even --json: write koko tunnel <ssh-name>"; msg != want {
		t.Errorf("koko tunnel --json says %q, want %q", msg, want)
	}

	r := run("help", "tunnel")
	r.WantSuccess(t)
	if strings.Contains(r.Stdout, "Flags:") || !strings.Contains(r.Stdout, "It takes no flag, not even --json: ssh runs it as the ProxyCommand") {
		t.Errorf("koko help tunnel = %q, want no flags and what runs it", r.Stdout)
	}
	r = run("help")
	r.WantSuccess(t)
	if !strings.Contains(r.Stdout, "Every command but koko exec and koko tunnel takes --json.") {
		t.Errorf("koko help = %q, want it to leave koko tunnel out of the commands that take --json", r.Stdout)
	}
	r = run("skill")
	r.WantSuccess(t)
	if !strings.Contains(r.Stdout, "- Every command but `koko exec` and `koko tunnel` takes `--json`.") ||
		!strings.Contains(r.Stdout, "- `koko tunnel <ssh-name>` is the ProxyCommand of koko's ssh_config, for ssh") {
		t.Errorf("koko skill does not say that koko tunnel is for ssh and takes no --json:\n%s", r.Stdout)
	}
	r = run("completion", "bash")
	r.WantSuccess(t)
	for _, path := range []string{"tunnel", "exec"} {
		if strings.Contains(r.Stdout, "\t"+path+") echo --json ;;") {
			t.Errorf("koko completion bash completes --json for koko %s, which takes none", path)
		}
	}
}

// When the Device Connection beneath an open SSH Session drops, the Tunnel
// is lost, since nothing Resumes it yet: koko tunnel says so in one line and
// fails, and ssh's connection ends. The line's wording is #73's.
func TestKokoTunnelSaysWhenItLostTheKokoServer(t *testing.T) {
	h, fwd, _, init := initThrough(t)
	d := harness.NewDevice(t)
	d.Koko("join", init.JoinCode.Code).WantSuccess(t)
	tunnelHost(t, h, "web1")
	p := d.Proxy(nil, "tunnel", "web1.koko")
	c := sshOver(t, p, "web1.koko", gatewayHostKey(t, h))

	fwd.CutWhen(func() bool { return true })
	if s, err := c.NewSession(); err == nil {
		_ = s.Close()
		t.Errorf("a session opened through a Tunnel whose Device Connection dropped")
	}
	r := p.Wait()
	if !strings.HasPrefix(r.Stderr, "koko: lost the connection to the koko server") || strings.Count(r.Stderr, "\n") != 1 || r.Status == 0 {
		t.Errorf("koko tunnel exited %d with stderr %q; want a failure and one line saying it lost the koko server", r.Status, r.Stderr)
	}
}

// wantTunnelLine fails the test unless koko tunnel failed before any Tunnel
// opened: exit status 1, nothing on stdout, and one koko: line on stderr,
// which it returns.
func wantTunnelLine(t *testing.T, what string, r harness.Result) string {
	t.Helper()
	if r.Status != 1 || r.Stdout != "" || !strings.HasPrefix(r.Stderr, "koko: ") || strings.Count(r.Stderr, "\n") != 1 || !strings.HasSuffix(r.Stderr, "\n") {
		t.Errorf("%s: exit status %d, stdout %q, stderr %q; want 1 and one koko: line", what, r.Status, r.Stdout, r.Stderr)
	}
	return r.Stderr
}

// Each failure before the Device Connection is up is one koko: line on koko
// tunnel's stderr, the line any koko command prints for it, naming what to
// do; ssh then says the connection closed. No Tunnel opens.
func TestKokoTunnelSaysWhyInOneLineBeforeTheDeviceConnection(t *testing.T) {
	h := harness.Start(t)
	host := tunnelHost(t, h, "web1")

	notJoined := harness.NewDevice(t)
	line := wantTunnelLine(t, "not joined", notJoined.Koko("tunnel", "web1.koko"))
	harness.Golden(t, "tunnel-not-joined", regexp.MustCompile(`nothing at \S+\)`).ReplaceAllString(line, "nothing at <socket>)"))

	mismatch, _ := joinAs(t, deviceAdd(t, h, "desk"))
	c := readClientConfig(t, mismatch)
	c.HostKeys = []string{string(ssh.MarshalAuthorizedKey(sshhost.NewKey(t, ssh.KeyAlgoED25519).PublicKey()))}
	writeClientConfig(t, mismatch, c)
	line = wantTunnelLine(t, "host key not pinned", mismatch.Koko("tunnel", "web1.koko"))
	harness.Golden(t, "tunnel-host-key-mismatch", strings.ReplaceAll(line, c.ServerAddress, "<server address>"))

	laptop, _ := joinAs(t, deviceAdd(t, h, "laptop"))
	server := readClientConfig(t, laptop).ServerAddress
	deviceRevoke(t, h.Koko, "laptop")
	line = wantTunnelLine(t, "Revoked", laptop.Koko("tunnel", "web1.koko"))
	harness.Golden(t, "tunnel-revoked", strings.ReplaceAll(line, server, "<server address>"))
	line = wantTunnelLine(t, "the revocation record", laptop.Koko("tunnel", "web1.koko"))
	harness.Golden(t, "tunnel-revoked-record", strings.ReplaceAll(line, server, "<server address>"))

	// A short wait for the koko server: it never answers again.
	short := timing.Timings{CommandWait: 200 * time.Millisecond}
	stopped := harness.Start(t)
	away := stopped.Join(harness.Owner)
	away.UseTimings(short)
	server = readClientConfig(t, away).ServerAddress
	muxed := harness.NewMuxDevice(t, short)
	muxed.Koko("join", deviceAdd(t, stopped, "laptop").Code).WantSuccess(t)
	waitLog(t, muxed, `msg="device connection opened"`)
	stopped.Stop()
	waitLog(t, muxed, `msg="device connection dropped"`)
	for path, d := range map[string]*harness.Device{"direct": away, "through the Device Mux": muxed} {
		line = wantTunnelLine(t, "unreachable, "+path, d.Koko("tunnel", "web1.koko"))
		if !strings.HasPrefix(line, "koko: cannot reach the koko server at "+server+": ") ||
			!strings.HasSuffix(line, "; if it has moved, take its new address with koko connect <host:port>\n") {
			t.Errorf("%s, an unreachable koko server is told as %q, want which and how to give its new address", path, line)
		}
	}

	if strings.Contains(h.Log(), "ssh session") || host.Conns() != 1 {
		t.Errorf("a failure before the Device Connection opened a Tunnel or reached the Host (%d connections, the Enroll's 1 expected); the koko server logged:\n%s", host.Conns(), h.Log())
	}
}
