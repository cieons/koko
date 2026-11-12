// Package openssh_test runs the system's OpenSSH through koko, end to end
// (P0 spec: reach, "Real OpenSSH"; P1 spec: the koko CLI on Windows, "The
// real OpenSSH test on Windows"): ssh, scp, sftp and forwards, and on
// Linux and macOS rsync, with -F pointed at koko's own ssh_config under a
// temporary home, through a koko binary built for the test, its koko tunnel
// and a real Device Mux process, against the S1 server and in-process
// Hosts. On Windows it runs OpenSSH for Windows and Git for Windows' ssh.
// It is a package of its own so that its build of koko and its processes
// count against no other package's time limit.
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
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
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

// tools are the binaries of one OpenSSH client the test runs, by path.
type tools struct {
	ssh, scp, sftp string
	// sftpServer serves the in-process Host's sftp subsystem.
	sftpServer string
	// knownHostsByProfile says the client expands ~ from the user's
	// profile as Windows records it, not from the environment, as OpenSSH
	// for Windows does: under the test's temporary profile its commands
	// then name koko's known_hosts themselves.
	knownHostsByProfile bool
}

// wantOpenSSH9 fails the test unless the ssh at path is OpenSSH 9.0 or
// later, whose scp speaks SFTP by default and takes -O for its own
// protocol.
func wantOpenSSH9(t *testing.T, path string) {
	t.Helper()
	out, err := exec.CommandContext(t.Context(), path, "-V").CombinedOutput()
	m := regexp.MustCompile(`OpenSSH_(?:for_Windows_)?(\d+)\.(\d+)`).FindSubmatch(out)
	if err != nil || m == nil {
		t.Fatalf("%s -V printed %q (%v), not an OpenSSH version", path, out, err)
	}
	if major, _ := strconv.Atoi(string(m[1])); major < 9 {
		t.Fatalf("%s -V printed %q: the real-OpenSSH test needs OpenSSH 9.0 or later, whose scp speaks SFTP and takes -O", path, out)
	}
}

// buildKoko builds koko once for the test into dir, with socket as its
// Unix socket path, so that it never reaches a koko server running on this
// machine, and with -race exactly when the test runs with it, so that the
// build takes the packages go test compiled from the cache.
func buildKoko(t *testing.T, dir, socket string) string {
	t.Helper()
	bin := filepath.Join(dir, kokoFile)
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

// user is one OS user's side of the test: a home of its own, a runtime
// directory for its Device Mux, which every command it runs gets, OpenSSH's
// included, the koko binary built for the test, and the OpenSSH client it
// runs. Its OpenSSH commands get a key of the user's own to offer, so the
// test can tell that ssh offers none.
type user struct {
	t       *testing.T
	koko    string
	tools   tools
	home    string
	runtime string
	// key is the user's own private key, which koko's ssh_config must keep
	// ssh from offering.
	key string
}

// writeKey writes a new private key of the user's own into a new
// directory, and returns its path.
func writeKey(t *testing.T) string {
	t.Helper()
	_, priv, _ := ed25519.GenerateKey(rand.Reader) // crypto/rand never fails
	block, err := ssh.MarshalPrivateKey(priv, "the user's own key")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "id_ed25519")
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// in returns the user as seen from the subtest t, which its commands fail.
func (u *user) in(t *testing.T) *user {
	v := *u
	v.t = t
	return &v
}

// kokoSSHConfig is the path of koko's ssh_config under the user's home.
func (u *user) kokoSSHConfig() string {
	return filepath.Join(u.home, ".ssh", "koko", "ssh_config")
}

// sshOptions are the options every OpenSSH command of the user's takes:
// -F pointed at koko's ssh_config under the user's home, since OpenSSH
// does not find ~ from the environment; the user's own key as
// IdentityFile, which also keeps ssh from reading the default keys in the
// real ~/.ssh; and, for a client that finds ~ from the profile Windows
// records, koko's known_hosts under the user's home.
func (u *user) sshOptions() []string {
	opts := []string{"-F", u.kokoSSHConfig(), "-o", "IdentityFile=" + u.key}
	if u.tools.knownHostsByProfile {
		known := filepath.ToSlash(filepath.Join(u.home, ".ssh", "koko", "known_hosts"))
		opts = append(opts, "-o", `UserKnownHostsFile="`+known+`"`)
	}
	return opts
}

// result is what a command printed, and its exit status.
type result struct {
	stdout, stderr string
	status         int
}

func (r result) String() string {
	return fmt.Sprintf("exit status %d, stdout %q, stderr %q", r.status, r.stdout, r.stderr)
}

// run runs the program at path with args as the user, in its home, with
// stdin, and waits at most commandWait for it.
func (u *user) run(stdin io.Reader, path string, args ...string) result {
	u.t.Helper()
	return u.runIn(u.home, stdin, path, args...)
}

// runIn runs the program at path with args as the user, in the directory
// dir, with stdin, and waits at most commandWait for it.
func (u *user) runIn(dir string, stdin io.Reader, path string, args ...string) result {
	u.t.Helper()
	ctx, cancel := context.WithTimeout(u.t.Context(), commandWait)
	defer cancel()
	cmd := u.command(ctx, path, args...)
	cmd.Dir = dir
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

// ssh runs the user's ssh with the user's sshOptions.
func (u *user) ssh(stdin io.Reader, args ...string) result {
	u.t.Helper()
	return u.run(stdin, u.tools.ssh, append(u.sshOptions(), args...)...)
}

// openSSHIn runs the user's OpenSSH program at path, such as scp, with the
// user's sshOptions, in the directory dir.
func (u *user) openSSHIn(dir string, stdin io.Reader, path string, args ...string) result {
	u.t.Helper()
	return u.runIn(dir, stdin, path, append(u.sshOptions(), args...)...)
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

// managedHost starts an in-process Host serving sftp with the client's
// sftp-server, records it as the Host named hostName in the Space space,
// as the Local caller, and Enrolls it.
func managedHost(t *testing.T, h *harness.Harness, ts tools, hostName, space string) *sshhost.Host {
	t.Helper()
	// The Host runs it through its shell; no path the test finds holds a
	// single quote.
	sftp := "'" + filepath.ToSlash(ts.sftpServer) + "'"
	host := sshhost.Start(t, sshhost.Password(hostPassword), sshhost.Subsystem("sftp", sftp))
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

// device is a Member's joined device and the Hosts in the Member's Space:
// web1 and files, which scp alone uses, since it counts the requests it
// gets; hidden, in the Owner's Personal Space, which the Member cannot
// see; and down, whose port nothing listens on.
type device struct {
	h           *harness.Harness
	bob         *user
	web1, files *sshhost.Host
}

// joinDevice joins bob's device with the koko binary built for the test,
// so that koko's ssh_config runs it, with ts as bob's OpenSSH client.
func joinDevice(t *testing.T, koko string, ts tools) device {
	t.Helper()
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
	d := device{h: h}
	d.web1 = managedHost(t, h, ts, "web1", "ops")
	d.files = managedHost(t, h, ts, "files", "ops")
	managedHost(t, h, ts, "hidden", harness.Owner)
	managedHost(t, h, ts, "down", "ops")
	_, deadPort, err := net.SplitHostPort(harness.FreeAddr(t))
	if err != nil {
		t.Fatal(err)
	}
	h.Koko("host", "set", "down", "port="+deadPort).WantSuccess(t)

	d.bob = newUser(t, koko, ts)
	d.bob.stopDeviceMuxes()
	wantSuccess(t, "koko join", d.bob.kokoCmd("join", invited.JoinCode.Code))
	if got, err := os.ReadFile(filepath.Join(d.bob.home, ".ssh", "config")); err != nil || string(got) != "Include ~/.ssh/koko/ssh_config\n" {
		t.Fatalf("after the join, ~/.ssh/config is %q (%v), want the Include line alone", got, err)
	}
	return d
}

// sshRunsACommand runs a command on web1 through koko: its stdin and
// stdout pass byte for byte, bytes a text mode would change among them,
// and its stderr and exit status come back; and the bare name reaches web1
// as the .koko name does.
func sshRunsACommand(t *testing.T, u *user) {
	// CR LF, a lone LF, NUL and Ctrl-Z, which a text mode on Windows would
	// change or stop at, then random bytes.
	payload := []byte("line\r\nnext\n\x00\x1aend\r")
	random := make([]byte, 64<<10)
	_, _ = rand.Read(random) // crypto/rand never fails
	payload = append(payload, random...)
	r := u.ssh(bytes.NewReader(payload), "web1.koko", "cat; echo to stderr >&2; exit 3")
	if r.status != 3 || r.stderr != "to stderr\n" {
		t.Errorf("ssh web1.koko: exit status %d, stderr %q; want the remote command's stderr and its exit status 3", r.status, r.stderr)
	}
	if r.stdout != string(payload) {
		t.Errorf("ssh web1.koko echoed %d bytes, want the %d bytes of its stdin as they were", len(r.stdout), len(payload))
	}
	r = u.ssh(nil, "web1", "echo", "bare")
	if r.status != 0 || r.stdout != "bare\n" || r.stderr != "" {
		t.Errorf("bare ssh web1: %v; want it to reach web1 as web1.koko does", r)
	}
}

// scpCopiesBothWays copies a file to the Host files and one back, with
// scp's SFTP and with its own protocol (-O). It names local files relative
// to the directory it runs in: Git for Windows' scp reads C: as a host.
func scpCopiesBothWays(t *testing.T, u *user, files *sshhost.Host) {
	local := t.TempDir()
	for _, c := range []struct {
		name string
		flag []string
		// subsystem and exec are what the Host is asked to run: the sftp
		// subsystem, or scp's own protocol.
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
		if r := u.openSSHIn(local, nil, u.tools.scp, slices.Concat([]string{"-q"}, c.flag, []string{up, "files.koko:" + up})...); r.status != 0 || r.stderr != "" {
			t.Errorf("%s to files: %v", c.name, r)
		}
		wantSameFile(t, c.name+" to files", src, filepath.Join(files.Home(), up))
		if r := u.openSSHIn(local, nil, u.tools.scp, slices.Concat([]string{"-q"}, c.flag, []string{"files.koko:" + down, down})...); r.status != 0 || r.stderr != "" {
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
}

// sftpCopiesBothWays puts a file on web1 and gets one back with sftp, its
// local files named relative to the directory it runs in.
func sftpCopiesBothWays(t *testing.T, u *user, web1 *sshhost.Host) {
	local := t.TempDir()
	src := randomFile(t, local, "sftp-up.bin", 1<<20)
	remote := randomFile(t, web1.Home(), "sftp-down.bin", 1<<20)
	batch := "put sftp-up.bin sftp-up.bin\nget sftp-down.bin sftp-down.bin\n"
	if r := u.openSSHIn(local, strings.NewReader(batch), u.tools.sftp, "-q", "-b", "-", "web1.koko"); r.status != 0 || r.stderr != "" {
		t.Errorf("sftp web1.koko: %v", r)
	}
	wantSameFile(t, "sftp put", src, filepath.Join(web1.Home(), "sftp-up.bin"))
	wantSameFile(t, "sftp get", remote, filepath.Join(local, "sftp-down.bin"))
}

// aLocalForwardReachesTheHost forwards a local TCP port to a TCP port on
// web1 with ssh -L.
func aLocalForwardReachesTheHost(t *testing.T, u *user) {
	// The Host is in this process, so its loopback is the test's.
	echo := listenEcho(t, "tcp", "127.0.0.1:0")
	local := harness.FreeAddr(t)
	u.forward(t, "-L", local+":"+echo.Addr().String())
	wantEcho(t, "-L to a TCP port on the Host", dialWhenUp(t, "tcp", local))
}

// everyTunnelGoesThroughTheDeviceMux checks that ssh's Tunnels ride the
// device's Device Mux: with an SSH Session open, koko disconnect finds the
// one Device Mux, with the session's koko tunnel the one command connected
// to it, which a koko tunnel on the direct path would not be.
func everyTunnelGoesThroughTheDeviceMux(t *testing.T, u *user) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	held := u.command(ctx, u.tools.ssh, append(u.sshOptions(), "web1.koko", "cat")...)
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
	r := u.kokoCmd("disconnect", "--json")
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
