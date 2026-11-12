//go:build unix

package openssh_test

import (
	"bytes"
	"context"
	"io"
	"net"
	"os"
	"os/exec"
	osuser "os/user"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/cieons/koko/internal/harness"
)

// kokoFile is the name of the koko binary built for the test.
const kokoFile = "koko"

// sftpServers are where Linux distributions and macOS put sftp-server.
var sftpServers = []string{
	"/usr/lib/openssh/sftp-server",     // Debian, Ubuntu
	"/usr/libexec/openssh/sftp-server", // Fedora, RHEL
	"/usr/lib/ssh/sftp-server",         // Arch
	"/usr/libexec/sftp-server",         // macOS, Alpine
}

// findTools finds the system's OpenSSH client, sftp-server and rsync. A
// missing one fails the test rather than skipping it: CI installs them.
func findTools(t *testing.T) tools {
	t.Helper()
	var ts tools
	for name, path := range map[string]*string{"ssh": &ts.ssh, "scp": &ts.scp, "sftp": &ts.sftp, "rsync": &ts.rsync} {
		p, err := exec.LookPath(name)
		if err != nil {
			t.Fatalf("the real-OpenSSH test runs %s, which is not installed: install the OpenSSH client, sftp-server and rsync (%v)", name, err)
		}
		*path = p
	}
	wantOpenSSH9(t, ts.ssh)
	for _, p := range sftpServers {
		if fi, err := os.Stat(p); err == nil && fi.Mode().IsRegular() {
			ts.sftpServer = p
			return ts
		}
	}
	t.Fatalf("the real-OpenSSH test serves sftp with the system's sftp-server, found at none of %q: install it", sftpServers)
	return ts
}

// newUser makes a user with a HOME of their own, and an agent their
// commands could ask, which fails the test when it ends should any of
// them have connected to it.
func newUser(t *testing.T, koko string, ts tools) *user {
	t.Helper()
	u := &user{t: t, koko: koko, tools: ts, home: t.TempDir(), runtime: runtimeDir(t), key: writeKey(t)}
	agent := listenAgent(t, filepath.Join(u.runtime, "agent.sock"))
	t.Cleanup(func() {
		if n := agent.conns.Load(); n != 0 {
			t.Errorf("OpenSSH connected to the user's ssh-agent %d times, want never: koko's ssh_config asks no agent", n)
		}
	})
	return u
}

// agentSocket stands in for an ssh-agent: it counts the connections made to
// it and closes each at once.
type agentSocket struct {
	conns atomic.Int64
}

func listenAgent(t *testing.T, path string) *agentSocket {
	t.Helper()
	var lc net.ListenConfig
	ln, err := lc.Listen(t.Context(), "unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	a := &agentSocket{}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			a.conns.Add(1)
			_ = c.Close()
		}
	}()
	return a
}

// runtimeDir makes a runtime directory for a user's Device Mux, removed
// when the test ends, under /tmp: macOS's TMPDIR is too long a path for the
// Device Mux's socket, whose name holds the version of a build, and a
// command whose Device Mux socket would pass 103 bytes goes direct.
func runtimeDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "koko-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// env is the environment of the user's commands: HOME, the runtime
// directory as TMPDIR and XDG_RUNTIME_DIR, PATH, and the user's agent as
// SSH_AUTH_SOCK, never the agent of whoever runs the test.
func (u *user) env() []string {
	return []string{
		"HOME=" + u.home,
		"TMPDIR=" + u.runtime,
		"XDG_RUNTIME_DIR=" + u.runtime,
		"PATH=" + os.Getenv("PATH"),
		"SSH_AUTH_SOCK=" + filepath.Join(u.runtime, "agent.sock"),
	}
}

// command returns the program at path with args, run as the user: in its
// HOME, with its environment, and in a session of its own, with no
// controlling terminal, so that anything that would prompt fails instead.
// Ending ctx kills the session's whole process group.
func (u *user) command(ctx context.Context, path string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Dir = u.home
	cmd.Env = u.env()
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	// The session's whole process group, ssh's ProxyCommand, koko tunnel,
	// included, so none outlives the test.
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	// koko tunnel may hold ssh's stderr a moment after ssh exits.
	cmd.WaitDelay = 5 * time.Second
	return cmd
}

// The system's OpenSSH reaches Hosts through koko unchanged: on a joined
// device, through koko tunnel and the device's real Device Mux; and on the
// koko server's own machine over the Unix socket, once koko ssh setup has
// written koko's ssh files there. The device's part runs first: the koko
// binary built for the test finds the koko server's machine's socket at its
// path once that part starts, and goes over it.
func TestRealOpenSSH(t *testing.T) {
	t.Parallel()
	ts := findTools(t)
	socket := filepath.Join(harness.ShortTempDir(t), "koko.sock")
	koko := buildKoko(t, t.TempDir(), socket)

	t.Run("on a joined device", func(t *testing.T) { onAJoinedDevice(t, koko, ts) })
	t.Run("on the koko server's machine", func(t *testing.T) { onTheKokoServersMachine(t, koko, ts, socket) })
}

// onAJoinedDevice joins a Member's device with the koko binary built for
// the test, so that koko's ssh_config runs it, and runs ssh, scp, sftp,
// rsync and forwards against Hosts in the Member's Space, each a subtest
// beside the others, every one through the Device Mux.
func onAJoinedDevice(t *testing.T, koko string, ts tools) {
	d := joinDevice(t, koko, ts)
	bob := d.bob

	// Each tool runs beside the others.
	t.Run("tools", func(t *testing.T) {
		t.Run("ssh", func(t *testing.T) {
			t.Parallel()
			sshRunsACommand(t, bob.in(t))
		})

		// Another block after koko's Include that gives a bare name a
		// HostName and a Port changes neither the SSH Name koko tunnel is
		// given nor the Host it reaches. The test puts it in the file that
		// holds the Include; ssh takes the first value across files too,
		// so the same holds for one in /etc/ssh/ssh_config.d.
		t.Run("a bare name another block also names", func(t *testing.T) {
			t.Parallel()
			u := bob.in(t)
			config := filepath.Join(t.TempDir(), "config")
			other := "Include " + u.kokoSSHConfig() + "\n" +
				"Host web1\n  HostName 192.0.2.10\n  Port 2222\n"
			if err := os.WriteFile(config, []byte(other), 0o600); err != nil {
				t.Fatal(err)
			}
			r := u.run(nil, u.tools.ssh, "-F", config, "-o", "IdentityFile="+u.key, "web1", "echo", "through koko")
			if r.status != 0 || r.stdout != "through koko\n" || r.stderr != "" {
				t.Errorf("ssh web1 with another block giving it a HostName: %v; want it to reach web1 through koko", r)
			}
		})

		t.Run("one none request and no key", func(t *testing.T) {
			t.Parallel()
			u := bob.in(t)
			r := u.ssh(nil, "-v", "web1.koko", "true")
			if r.status != 0 {
				t.Fatalf("ssh -v web1.koko true: %v", r)
			}
			if !strings.Contains(r.stderr, `Authenticated to web1.koko (via proxy) using "none".`) {
				t.Errorf("ssh -v says nothing of authenticating with none:\n%s", r.stderr)
			}
			if me, err := osuser.Current(); err != nil {
				t.Error(err)
			} else if real := filepath.Join(me.HomeDir, ".ssh") + "/"; strings.Contains(r.stderr, real) {
				t.Errorf("ssh -v names %s, want ssh to read nothing in the real ~/.ssh:\n%s", real, r.stderr)
			}
			if m := regexp.MustCompile(`Offering public key|Trying private key|Next authentication method: \S+|Authentications that can continue`).FindString(r.stderr); m != "" {
				t.Errorf("ssh -v says %q; want ssh to send one none request and offer no key:\n%s", m, r.stderr)
			}
		})

		t.Run("banners", func(t *testing.T) {
			t.Parallel()
			u := bob.in(t)
			for _, c := range []struct{ sshName, banner string }{
				{"nope.koko", `koko: no Host is named "nope".`},
				{"hidden.koko", `koko: no Host is named "hidden".`},
				{"down.koko", "koko: down is down or unreachable: koko could not connect to it; this SSH Session did not start."},
			} {
				r := u.ssh(nil, c.sshName, "true")
				if r.status != 255 || r.stdout != "" || !strings.HasPrefix(r.stderr, c.banner+"\n") {
					t.Errorf("ssh %s: %v; want exit status 255 and stderr starting with the banner %q", c.sshName, r, c.banner)
				}
			}
		})

		t.Run("scp", func(t *testing.T) {
			t.Parallel()
			scpCopiesBothWays(t, bob.in(t), d.files)
		})

		t.Run("sftp", func(t *testing.T) {
			t.Parallel()
			sftpCopiesBothWays(t, bob.in(t), d.web1)
		})

		t.Run("rsync", func(t *testing.T) {
			t.Parallel()
			u := bob.in(t)
			local := t.TempDir()
			src := randomFile(t, local, "rsync-up.bin", 1<<20)
			remote := randomFile(t, d.web1.Home(), "rsync-down.bin", 1<<20)
			// No path in it holds a space, which rsync would split -e at.
			rsh := strings.Join(append([]string{"ssh"}, u.sshOptions()...), " ")
			for _, c := range []struct{ what, from, to, a, b string }{
				{"rsync to web1", src, "web1.koko:rsync-up.bin", src, filepath.Join(d.web1.Home(), "rsync-up.bin")},
				{"rsync from web1", "web1.koko:rsync-down.bin", filepath.Join(local, "rsync-down.bin"), remote, filepath.Join(local, "rsync-down.bin")},
			} {
				if r := u.run(nil, u.tools.rsync, "-e", rsh, c.from, c.to); r.status != 0 || r.stderr != "" {
					t.Errorf("%s: %v", c.what, r)
				}
				wantSameFile(t, c.what, c.a, c.b)
			}
		})

		t.Run("local forward", func(t *testing.T) {
			t.Parallel()
			aLocalForwardReachesTheHost(t, bob.in(t))
		})

		t.Run("other forwards", func(t *testing.T) {
			t.Parallel()
			u := bob.in(t)
			// The Host is in this process, so its loopback is the test's.
			echo := listenEcho(t, "tcp", "127.0.0.1:0")
			echoSock := listenEcho(t, "unix", filepath.Join(harness.ShortTempDir(t), "echo.sock"))
			localSock := filepath.Join(harness.ShortTempDir(t), "l.sock")
			socks := harness.FreeAddr(t)
			remote := harness.FreeAddr(t)
			_, remotePort, _ := net.SplitHostPort(remote)
			u.forward(t, "-L", localSock+":"+echoSock.Addr().String())
			u.forward(t, "-D", socks)
			u.forward(t, "-R", "127.0.0.1:"+remotePort+":"+echo.Addr().String())
			remoteSock := filepath.Join(harness.ShortTempDir(t), "r.sock")
			u.forward(t, "-R", remoteSock+":"+echo.Addr().String())

			wantEcho(t, "-L to a Unix socket on the Host", dialWhenUp(t, "unix", localSock))
			wantEcho(t, "-D, SOCKS5 to a TCP port on the Host", socks5(t, dialWhenUp(t, "tcp", socks), echo.Addr().(*net.TCPAddr)))
			wantEcho(t, "-R from a TCP port on the Host", dialWhenUp(t, "tcp", remote))
			wantEcho(t, "-R from a Unix socket on the Host", dialWhenUp(t, "unix", remoteSock))
		})
	})

	everyTunnelGoesThroughTheDeviceMux(t, bob)
}

// socks5 asks the SOCKS5 proxy at c, with no authentication, to connect to
// to, and returns c once it has.
func socks5(t *testing.T, c net.Conn, to *net.TCPAddr) net.Conn {
	t.Helper()
	_ = c.SetDeadline(time.Now().Add(10 * time.Second))
	req := []byte{5, 1, 0, 5, 1, 0, 1}
	req = append(req, to.IP.To4()...)
	req = append(req, byte(to.Port>>8), byte(to.Port))
	if _, err := c.Write(req); err != nil {
		t.Fatalf("SOCKS5 request: %v", err)
	}
	reply := make([]byte, 2+10)
	if _, err := io.ReadFull(c, reply); err != nil || !bytes.Equal(reply[:2], []byte{5, 0}) || reply[3] != 0 {
		t.Fatalf("SOCKS5 replied %v (%v), want no authentication and a connection", reply, err)
	}
	return c
}

// onTheKokoServersMachine runs ssh on the koko server's own machine: as
// whoever runs it, over the Unix socket, as the Local caller, once koko ssh
// setup has written koko's ssh files for that OS user.
func onTheKokoServersMachine(t *testing.T, koko string, ts tools, socket string) {
	h := harness.Start(t, harness.SocketPath(socket))
	managedHost(t, h, ts, "web1", harness.Owner)

	owner := newUser(t, koko, ts)
	r := owner.kokoCmd("ssh", "setup")
	wantSuccess(t, "koko ssh setup", r)
	if got, err := os.ReadFile(filepath.Join(owner.home, ".ssh", "config")); err != nil || string(got) != "Include ~/.ssh/koko/ssh_config\n" {
		t.Errorf("after koko ssh setup, ~/.ssh/config is %q (%v), want the Include line alone", got, err)
	}
	for _, sshName := range []string{"web1.koko", "web1"} {
		r := owner.ssh(nil, sshName, "echo", "reached")
		if r.status != 0 || r.stdout != "reached\n" || r.stderr != "" {
			t.Errorf("ssh %s on the koko server's machine: %v; want it to reach web1", sshName, r)
		}
	}
	if !regexp.MustCompile(`msg="ssh session started" .*device_session="unix socket" host=web1 `).MatchString(h.Log()) {
		t.Errorf("the koko server logged no SSH Session to web1 over the Unix socket:\n%s", h.Log())
	}
	if _, err := os.Stat(filepath.Join(owner.runtime, "koko")); err == nil {
		t.Errorf("ssh over the Unix socket started a Device Mux; want none on the koko server's machine")
	}
}
