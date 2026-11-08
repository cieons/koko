//go:build unix

// Package openssh_test runs the system's OpenSSH through koko, end to end
// (P0 spec: reach, "Real OpenSSH"): ssh, scp, sftp, rsync and forwards, with
// -F pointed at koko's own ssh_config under a temporary HOME, through a koko
// binary built for the test, its koko tunnel and a real Device Mux process,
// against the S1 server and in-process Hosts. It is a package of its own so
// that its build of koko and its processes count against no other package's
// time limit.
package openssh_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json/v2"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	osuser "os/user"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/harness/sshhost"
	"github.com/cieons/koko/internal/shared/timing"
)

// hostPassword is the in-process Hosts' password, which Enroll logs in with
// once.
const hostPassword = "correct horse battery staple"

// commandWait bounds each OpenSSH or koko command the test runs.
const commandWait = 30 * time.Second

// tools are the OpenSSH and rsync binaries the test runs, by name.
type tools struct {
	ssh, scp, sftp, rsync string
	// sftpServer serves the in-process Host's sftp subsystem.
	sftpServer string
}

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
	// scp speaks SFTP by default, and takes -O for its own protocol, from
	// OpenSSH 9.0 on.
	out, err := exec.CommandContext(t.Context(), ts.ssh, "-V").CombinedOutput()
	m := regexp.MustCompile(`OpenSSH_(\d+)\.(\d+)`).FindSubmatch(out)
	if err != nil || m == nil {
		t.Fatalf("ssh -V printed %q (%v), not an OpenSSH version", out, err)
	}
	if major, _ := strconv.Atoi(string(m[1])); major < 9 {
		t.Fatalf("ssh -V printed %q: the real-OpenSSH test needs OpenSSH 9.0 or later, whose scp speaks SFTP and takes -O", out)
	}
	for _, p := range sftpServers {
		if fi, err := os.Stat(p); err == nil && fi.Mode().IsRegular() {
			ts.sftpServer = p
			return ts
		}
	}
	t.Fatalf("the real-OpenSSH test serves sftp with the system's sftp-server, found at none of %q: install it", sftpServers)
	return ts
}

// buildKoko builds koko once for the test, with socket as its Unix socket
// path, so that it never reaches a koko server running on this machine,
// and with -race exactly when the test runs with it, so that the build
// takes the packages go test compiled from the cache.
func buildKoko(t *testing.T, socket string) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "koko")
	args := []string{"build", "-o", bin, "-ldflags", "-X main.socketPath=" + socket}
	if raceEnabled {
		args = append(args, "-race")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", append(args, "github.com/cieons/koko/cmd/koko")...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build koko: %v\n%s", err, out)
	}
	return bin
}

// user is one OS user's side of the test: a HOME of its own, a runtime
// directory for its Device Mux, which every command it runs gets, OpenSSH's
// included, and the koko binary built for the test. Its OpenSSH commands
// get a key of the user's own to offer and an agent to ask, so the test can
// tell that ssh does neither.
type user struct {
	t       *testing.T
	koko    string
	tools   tools
	home    string
	runtime string
	// key is the user's own private key, which koko's ssh_config must keep
	// ssh from offering.
	key string
	// agent is the user's ssh-agent socket, which koko's ssh_config must
	// keep ssh from asking.
	agent *agentSocket
}

// newUser makes a user, and fails the test when it ends should any of the
// user's commands have connected to their agent.
func newUser(t *testing.T, koko string, ts tools) *user {
	t.Helper()
	u := &user{t: t, koko: koko, tools: ts, home: t.TempDir(), runtime: runtimeDir(t)}
	_, priv, _ := ed25519.GenerateKey(rand.Reader) // crypto/rand never fails
	block, err := ssh.MarshalPrivateKey(priv, "the user's own key")
	if err != nil {
		t.Fatal(err)
	}
	u.key = filepath.Join(t.TempDir(), "id_ed25519")
	if err := os.WriteFile(u.key, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatal(err)
	}
	u.agent = listenAgent(t, filepath.Join(u.runtime, "agent.sock"))
	t.Cleanup(func() {
		if n := u.agent.conns.Load(); n != 0 {
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

// in returns the user as seen from the subtest t, which its commands fail.
func (u *user) in(t *testing.T) *user {
	v := *u
	v.t = t
	return &v
}

// sshOptions are the options every OpenSSH command of the user's takes:
// -F pointed at koko's ssh_config under the user's HOME, since OpenSSH
// finds ~ from the password database and not from HOME; and the user's own
// key as IdentityFile, which also keeps ssh from reading the default keys
// in the real ~/.ssh.
func (u *user) sshOptions() []string {
	return []string{"-F", filepath.Join(u.home, ".ssh", "koko", "ssh_config"), "-o", "IdentityFile=" + u.key}
}

// result is what a command printed, and its exit status.
type result struct {
	stdout, stderr string
	status         int
}

func (r result) String() string {
	return fmt.Sprintf("exit status %d, stdout %q, stderr %q", r.status, r.stdout, r.stderr)
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

// run runs the program at path with args as the user, with stdin, and
// waits at most commandWait for it.
func (u *user) run(stdin io.Reader, path string, args ...string) result {
	u.t.Helper()
	ctx, cancel := context.WithTimeout(u.t.Context(), commandWait)
	defer cancel()
	cmd := u.command(ctx, path, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdin, cmd.Stdout, cmd.Stderr = stdin, &stdout, &stderr
	err := cmd.Run()
	r := result{stdout: stdout.String(), stderr: stderr.String()}
	if ee, ok := errors.AsType[*exec.ExitError](err); ok {
		r.status = ee.ExitCode()
	} else if err != nil {
		u.t.Fatalf("run %s %q: %v (stderr %q)", filepath.Base(path), args, err, r.stderr)
	}
	if ctx.Err() != nil {
		u.t.Fatalf("%s %q did not end within %v: %v", filepath.Base(path), args, commandWait, r)
	}
	return r
}

// kokoCmd runs the koko binary built for the test as the user.
func (u *user) kokoCmd(args ...string) result {
	u.t.Helper()
	return u.run(nil, u.koko, args...)
}

// ssh runs the system's ssh with the user's sshOptions.
func (u *user) ssh(stdin io.Reader, args ...string) result {
	u.t.Helper()
	return u.run(stdin, u.tools.ssh, append(u.sshOptions(), args...)...)
}

// openSSH runs the system's OpenSSH program at path, such as scp, with the
// user's sshOptions.
func (u *user) openSSH(stdin io.Reader, path string, args ...string) result {
	u.t.Helper()
	return u.run(stdin, path, append(u.sshOptions(), args...)...)
}

// wantSuccess fails the test unless r exited 0 with nothing on stderr.
func wantSuccess(t *testing.T, what string, r result) {
	t.Helper()
	if r.status != 0 || r.stderr != "" {
		t.Fatalf("%s: %v; want success with nothing on stderr", what, r)
	}
}

// stopDeviceMuxes stops the user's Device Muxes once the test is done, as
// koko disconnect does, since one outlives its commands for 10 minutes.
func (u *user) stopDeviceMuxes() {
	u.t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), commandWait)
		defer cancel()
		if out, err := u.command(ctx, u.koko, "disconnect").CombinedOutput(); err != nil {
			u.t.Errorf("koko disconnect, to stop the Device Mux: %v: %s", err, out)
		}
	})
}

// managedHost starts an in-process Host serving sftp with the system's
// sftp-server, records it as the Host named hostName in the Space space,
// as the Local caller, and Enrolls it.
func managedHost(t *testing.T, h *harness.Harness, ts tools, hostName, space string) *sshhost.Host {
	t.Helper()
	host := sshhost.Start(t, sshhost.Password(hostPassword), sshhost.Subsystem("sftp", ts.sftpServer))
	h.Koko("host", "add", hostName, "address="+host.Address(), "port="+strconv.Itoa(host.Port()),
		"account="+sshhost.Account, "space="+space).WantSuccess(t)
	h.KokoStdin(hostPassword, "host", "enroll", hostName, "--password-stdin").WantSuccess(t)
	return host
}

// randomFile writes size random bytes into a new file in dir and returns
// its path.
func randomFile(t *testing.T, dir, name string, size int) string {
	t.Helper()
	b := make([]byte, size)
	_, _ = rand.Read(b) // crypto/rand never fails
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// wantSameFile fails the test unless the files at a and b hold the same
// bytes, by their SHA-256.
func wantSameFile(t *testing.T, what, a, b string) {
	t.Helper()
	sum := func(path string) [32]byte {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s: %v", what, err)
		}
		return sha256.Sum256(data)
	}
	if sa, sb := sum(a), sum(b); sa != sb {
		t.Errorf("%s: %s has SHA-256 %x, %s has %x; want the same bytes", what, a, sa, b, sb)
	}
}

// hostCommands returns the commands of the exec requests and the names of
// the subsystem requests the Host received from from on.
func hostCommands(t *testing.T, host *sshhost.Host, from int) (execs, subsystems []string) {
	t.Helper()
	for _, r := range host.Requests()[from:] {
		var into *[]string
		switch r.Type {
		case "exec":
			into = &execs
		case "subsystem":
			into = &subsystems
		default:
			continue
		}
		var s struct{ Value string }
		if err := ssh.Unmarshal(r.Payload, &s); err != nil {
			t.Fatalf("the Host received a %s request it cannot read: %v", r.Type, err)
		}
		*into = append(*into, s.Value)
	}
	return execs, subsystems
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
	koko := buildKoko(t, socket)

	t.Run("on a joined device", func(t *testing.T) { onAJoinedDevice(t, koko, ts) })
	t.Run("on the koko server's machine", func(t *testing.T) { onTheKokoServersMachine(t, koko, ts, socket) })
}

// onAJoinedDevice joins a Member's device with the koko binary built for
// the test, so that koko's ssh_config runs it, and runs ssh, scp, sftp,
// rsync and forwards against Hosts in the Member's Space, each a subtest
// beside the others, every one through the Device Mux.
func onAJoinedDevice(t *testing.T, koko string, ts tools) {
	h := harness.Start(t, harness.Timings(timing.Timings{HostConnectWait: time.Millisecond}))
	h.Koko("space", "add", "ops").WantSuccess(t)
	invite := h.Koko("user", "invite", "bob", "--json")
	invite.WantSuccess(t)
	var invited struct {
		JoinCode struct {
			Code string `json:"code"`
		} `json:"join_code"`
	}
	if err := json.Unmarshal([]byte(invite.Stdout), &invited); err != nil {
		t.Fatalf("user invite printed %q: %v", invite.Stdout, err)
	}
	h.Koko("space", "member", "add", "ops", "bob").WantSuccess(t)
	web1 := managedHost(t, h, ts, "web1", "ops")
	files := managedHost(t, h, ts, "files", "ops") // scp's alone, which counts the requests it gets
	managedHost(t, h, ts, "hidden", harness.Owner) // in alice's Personal Space, which bob cannot see
	managedHost(t, h, ts, "down", "ops")
	_, deadPort, err := net.SplitHostPort(harness.FreeAddr(t))
	if err != nil {
		t.Fatal(err)
	}
	h.Koko("host", "set", "down", "port="+deadPort).WantSuccess(t)

	bob := newUser(t, koko, ts)
	bob.stopDeviceMuxes()
	wantSuccess(t, "koko join", bob.kokoCmd("join", invited.JoinCode.Code))
	if got, err := os.ReadFile(filepath.Join(bob.home, ".ssh", "config")); err != nil || string(got) != "Include ~/.ssh/koko/ssh_config\n" {
		t.Fatalf("after the join, ~/.ssh/config is %q (%v), want the Include line alone", got, err)
	}

	// Each tool runs beside the others.
	t.Run("tools", func(t *testing.T) {
		t.Run("ssh", func(t *testing.T) {
			t.Parallel()
			u := bob.in(t)
			r := u.ssh(strings.NewReader("from stdin\n"), "web1.koko", "cat; echo to stderr >&2; exit 3")
			if r.status != 3 || r.stdout != "from stdin\n" || r.stderr != "to stderr\n" {
				t.Errorf("ssh web1.koko: %v; want the remote command's stdin echoed, its stderr and its exit status 3", r)
			}
			r = u.ssh(nil, "web1", "echo", "bare")
			if r.status != 0 || r.stdout != "bare\n" || r.stderr != "" {
				t.Errorf("bare ssh web1: %v; want it to reach web1 as web1.koko does", r)
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
			u := bob.in(t)
			local := t.TempDir()
			for _, c := range []struct {
				name string
				flag []string
				// subsystem and exec are what the Host is asked to run: the
				// sftp subsystem, or scp's own protocol.
				subsystem, exec string
			}{
				{"scp", nil, "sftp", ""},
				{"scp -O", []string{"-O"}, "", "scp -t up-O.bin"},
			} {
				up := "up" + strings.Join(c.flag, "") + ".bin"
				down := "down" + strings.Join(c.flag, "") + ".bin"
				src := randomFile(t, local, up, 1<<20)
				remote := randomFile(t, files.Home(), down, 1<<20)
				before := len(files.Requests())
				if r := u.openSSH(nil, ts.scp, slices.Concat([]string{"-q"}, c.flag, []string{src, "files.koko:" + up})...); r.status != 0 || r.stderr != "" {
					t.Errorf("%s to files: %v", c.name, r)
				}
				wantSameFile(t, c.name+" to files", src, filepath.Join(files.Home(), up))
				if r := u.openSSH(nil, ts.scp, slices.Concat([]string{"-q"}, c.flag, []string{"files.koko:" + down, filepath.Join(local, down)})...); r.status != 0 || r.stderr != "" {
					t.Errorf("%s from files: %v", c.name, r)
				}
				wantSameFile(t, c.name+" from files", remote, filepath.Join(local, down))
				execs, subsystems := hostCommands(t, files, before)
				switch {
				case c.subsystem != "" && (len(subsystems) != 2 || subsystems[0] != c.subsystem || len(execs) != 0):
					t.Errorf("%s asked the Host to run %q and the subsystems %q, want the %s subsystem twice", c.name, execs, subsystems, c.subsystem)
				case c.exec != "" && (len(execs) != 2 || execs[0] != c.exec || len(subsystems) != 0):
					t.Errorf("%s asked the Host to run %q and the subsystems %q, want scp's own protocol, %q first", c.name, execs, subsystems, c.exec)
				}
			}
		})

		t.Run("sftp", func(t *testing.T) {
			t.Parallel()
			u := bob.in(t)
			local := t.TempDir()
			src := randomFile(t, local, "sftp-up.bin", 1<<20)
			remote := randomFile(t, web1.Home(), "sftp-down.bin", 1<<20)
			batch := fmt.Sprintf("put %s sftp-up.bin\nget sftp-down.bin %s\n", src, filepath.Join(local, "sftp-down.bin"))
			if r := u.openSSH(strings.NewReader(batch), ts.sftp, "-q", "-b", "-", "web1.koko"); r.status != 0 || r.stderr != "" {
				t.Errorf("sftp web1.koko: %v", r)
			}
			wantSameFile(t, "sftp put", src, filepath.Join(web1.Home(), "sftp-up.bin"))
			wantSameFile(t, "sftp get", remote, filepath.Join(local, "sftp-down.bin"))
		})

		t.Run("rsync", func(t *testing.T) {
			t.Parallel()
			u := bob.in(t)
			local := t.TempDir()
			src := randomFile(t, local, "rsync-up.bin", 1<<20)
			remote := randomFile(t, web1.Home(), "rsync-down.bin", 1<<20)
			// No path in it holds a space, which rsync would split -e at.
			rsh := strings.Join(append([]string{"ssh"}, u.sshOptions()...), " ")
			for _, c := range []struct{ what, from, to, a, b string }{
				{"rsync to web1", src, "web1.koko:rsync-up.bin", src, filepath.Join(web1.Home(), "rsync-up.bin")},
				{"rsync from web1", "web1.koko:rsync-down.bin", filepath.Join(local, "rsync-down.bin"), remote, filepath.Join(local, "rsync-down.bin")},
			} {
				if r := u.run(nil, ts.rsync, "-e", rsh, c.from, c.to); r.status != 0 || r.stderr != "" {
					t.Errorf("%s: %v", c.what, r)
				}
				wantSameFile(t, c.what, c.a, c.b)
			}
		})

		t.Run("forwards", func(t *testing.T) {
			t.Parallel()
			u := bob.in(t)
			// The Host is in this process, so its loopback is the test's.
			echo := listenEcho(t, "tcp", "127.0.0.1:0")
			echoSock := listenEcho(t, "unix", filepath.Join(harness.ShortTempDir(t), "echo.sock"))
			local := harness.FreeAddr(t)
			localSock := filepath.Join(harness.ShortTempDir(t), "l.sock")
			socks := harness.FreeAddr(t)
			remote := harness.FreeAddr(t)
			_, remotePort, _ := net.SplitHostPort(remote)
			u.forward(t, "-L", local+":"+echo.Addr().String())
			u.forward(t, "-L", localSock+":"+echoSock.Addr().String())
			u.forward(t, "-D", socks)
			u.forward(t, "-R", "127.0.0.1:"+remotePort+":"+echo.Addr().String())
			remoteSock := filepath.Join(harness.ShortTempDir(t), "r.sock")
			u.forward(t, "-R", remoteSock+":"+echo.Addr().String())

			wantEcho(t, "-L to a TCP port on the Host", dialWhenUp(t, "tcp", local))
			wantEcho(t, "-L to a Unix socket on the Host", dialWhenUp(t, "unix", localSock))
			wantEcho(t, "-D, SOCKS5 to a TCP port on the Host", socks5(t, dialWhenUp(t, "tcp", socks), echo.Addr().(*net.TCPAddr)))
			wantEcho(t, "-R from a TCP port on the Host", dialWhenUp(t, "tcp", remote))
			wantEcho(t, "-R from a Unix socket on the Host", dialWhenUp(t, "unix", remoteSock))
		})
	})

	// Every Tunnel goes through the Device Mux: with an SSH Session open,
	// koko disconnect finds the one Device Mux, with the session's koko
	// tunnel the one command connected to it, which a koko tunnel on the
	// direct path would not be.
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	held := bob.command(ctx, ts.ssh, append(bob.sshOptions(), "web1.koko", "cat")...)
	stdin, err := held.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	var echoed syncBuffer
	held.Stdout = &echoed
	if err := held.Start(); err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(stdin, "held\n"); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(10 * time.Second); echoed.String() != "held\n"; time.Sleep(20 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("the SSH Session held open printed %q within 10 s, want %q", echoed.String(), "held\n")
		}
	}
	r := bob.kokoCmd("disconnect", "--json")
	var disconnected struct {
		DeviceMuxes []struct {
			CommandsEnded int `json:"commands_ended"`
		} `json:"device_muxes"`
	}
	if err := json.Unmarshal([]byte(r.stdout), &disconnected); err != nil || r.status != 0 ||
		len(disconnected.DeviceMuxes) != 1 || disconnected.DeviceMuxes[0].CommandsEnded != 1 {
		t.Errorf("koko disconnect --json with an SSH Session open: %v (%v); want the one Device Mux, which the session's koko tunnel was connected to", r, err)
	}
	_ = stdin.Close()
	_ = held.Wait() // the Device Mux ended its Tunnel
}

// forward runs ssh -N with the forward flag and spec to web1 in the
// background until the test ends, failing at once should ssh exit first.
func (u *user) forward(t *testing.T, flag, spec string) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	cmd := u.command(ctx, u.tools.ssh, append(u.sshOptions(), "-N", "-o", "ExitOnForwardFailure=yes", flag, spec, "web1.koko")...)
	var stderr syncBuffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatalf("start ssh %s %s: %v", flag, spec, err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		err := cmd.Wait()
		if ctx.Err() == nil {
			t.Errorf("ssh -N %s %s exited before the test ended: %v, stderr %q", flag, spec, err, stderr.String())
		}
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
}

// syncBuffer is a buffer a process writes while the test reads it.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// listenEcho listens on network and addr until the test ends, sending back
// what each connection sends.
func listenEcho(t *testing.T, network, addr string) net.Listener {
	t.Helper()
	var lc net.ListenConfig
	ln, err := lc.Listen(t.Context(), network, addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
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
	}()
	return ln
}

// dialWhenUp connects to addr once something listens there, as ssh's
// forward does once it is set up, trying for at most 10 s. The connection
// closes when the test ends.
func dialWhenUp(t *testing.T, network, addr string) net.Conn {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	var d net.Dialer
	for {
		c, err := d.DialContext(t.Context(), network, addr)
		if err == nil {
			t.Cleanup(func() { _ = c.Close() })
			return c
		}
		if time.Now().After(deadline) {
			t.Fatalf("nothing listened at %s within 10 s: %v", addr, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// wantEcho sends a line on c and fails the test unless it comes back.
func wantEcho(t *testing.T, what string, c net.Conn) {
	t.Helper()
	const line = "through koko\n"
	_ = c.SetDeadline(time.Now().Add(10 * time.Second))
	if _, err := io.WriteString(c, line); err != nil {
		t.Errorf("%s: send: %v", what, err)
		return
	}
	got := make([]byte, len(line))
	if _, err := io.ReadFull(c, got); err != nil || string(got) != line {
		t.Errorf("%s: got %q back (%v), want %q", what, got, err, line)
	}
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
