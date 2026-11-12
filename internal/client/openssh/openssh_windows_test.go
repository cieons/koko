package openssh_test

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	osuser "os/user"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cieons/koko/internal/harness"
)

// kokoFile is the name of the koko binary built for the test.
const kokoFile = "koko.exe"

// findClients finds OpenSSH for Windows, from the project's installer in
// Program Files or the optional feature in System32, and Git for Windows'
// OpenSSH with its sftp-server, which serves the in-process Hosts' sftp for
// both. A missing one fails the test rather than skipping it: the runner
// has both.
func findClients(t *testing.T) (windows, git tools) {
	t.Helper()
	gitExe, err := exec.LookPath("git")
	if err != nil {
		t.Fatalf("the real-OpenSSH test runs Git for Windows' ssh, and finds it beside git, which is not on PATH: install Git for Windows (%v)", err)
	}
	// git.exe is in Git's cmd or bin directory.
	gitRoot := filepath.Dir(filepath.Dir(gitExe))
	git = tools{
		ssh:        filepath.Join(gitRoot, "usr", "bin", "ssh.exe"),
		scp:        filepath.Join(gitRoot, "usr", "bin", "scp.exe"),
		sftpServer: filepath.Join(gitRoot, "usr", "lib", "ssh", "sftp-server.exe"),
	}
	for _, p := range []string{git.ssh, git.scp, git.sftpServer} {
		if fi, err := os.Stat(p); err != nil || !fi.Mode().IsRegular() {
			t.Fatalf("the real-OpenSSH test runs %s of Git for Windows, which is not there: install Git for Windows (%v)", p, err)
		}
	}
	dirs := []string{
		filepath.Join(os.Getenv("ProgramFiles"), "OpenSSH"),
		filepath.Join(os.Getenv("SystemRoot"), "System32", "OpenSSH"),
	}
	for _, dir := range dirs {
		if _, err := os.Stat(filepath.Join(dir, "ssh.exe")); err != nil {
			continue
		}
		windows = tools{
			ssh:                 filepath.Join(dir, "ssh.exe"),
			scp:                 filepath.Join(dir, "scp.exe"),
			sftp:                filepath.Join(dir, "sftp.exe"),
			sftpServer:          git.sftpServer,
			knownHostsByProfile: true,
		}
		wantOpenSSH9(t, windows.ssh)
		wantOpenSSH9(t, git.ssh)
		return windows, git
	}
	t.Fatalf("the real-OpenSSH test runs OpenSSH for Windows, found in none of %q: add the OpenSSH client (Add-WindowsCapability -Online -Name OpenSSH.Client~~~~0.0.1.0)", dirs)
	return tools{}, tools{}
}

// removedAtTheEnd makes a temporary directory, short enough for the Device
// Mux's socket, removed once the test's other cleanups have run: once the
// Device Mux that koko disconnect stopped has exited, which on Windows may
// take a moment after it answered, and until then holds its log, its lock
// and koko.exe open, which Windows does not let a directory be removed
// with.
func removedAtTheEnd(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "koko-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		deadline := time.Now().Add(10 * time.Second)
		for {
			err := os.RemoveAll(dir)
			if err == nil {
				return
			}
			if time.Now().After(deadline) {
				t.Errorf("remove %s once the Device Mux stopped: %v", dir, err)
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
	})
	return dir
}

// newUser makes a user whose profile directory's name holds a space, as
// C:\Users\Alice Smith does, with LOCALAPPDATA, the Device Mux's runtime
// directory, of their own.
func newUser(t *testing.T, koko string, ts tools) *user {
	t.Helper()
	home := filepath.Join(removedAtTheEnd(t), "Alice Smith")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatal(err)
	}
	return &user{t: t, koko: koko, tools: ts, home: home, runtime: removedAtTheEnd(t), key: writeKey(t)}
}

// env is the environment of the user's commands: the test's own, which
// Windows programs need much of, with the user's profile as USERPROFILE,
// and as HOME, from which Git for Windows' ssh takes its home; the runtime
// directory as LOCALAPPDATA; and none of the variables that name another
// home or an ssh-agent.
func (u *user) env() []string {
	drop := []string{"USERPROFILE", "HOME", "HOMEDRIVE", "HOMEPATH", "LOCALAPPDATA", "SSH_AUTH_SOCK"}
	var env []string
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		// Windows ignores case in a variable's name.
		if !slices.ContainsFunc(drop, func(d string) bool { return strings.EqualFold(d, k) }) {
			env = append(env, kv)
		}
	}
	return append(env, "USERPROFILE="+u.home, "LOCALAPPDATA="+u.runtime) // BREAK 6
}

// with returns the user running the OpenSSH client ts.
func (u *user) with(ts tools) *user {
	v := *u
	v.tools = ts
	return &v
}

// command returns the program at path with args, run as the user: in its
// profile directory, with its environment. Ending ctx kills the program;
// its koko tunnel ends once the pipes ssh gave it close.
func (u *user) command(ctx context.Context, path string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Dir = u.home
	cmd.Env = u.env()
	// koko tunnel may hold ssh's stderr a moment after ssh exits.
	cmd.WaitDelay = 5 * time.Second
	return cmd
}

// Both ssh clients on Windows reach Hosts through koko unchanged, on a
// joined device whose profile directory's name holds a space, through koko
// tunnel and the device's real Device Mux: OpenSSH for Windows with ssh,
// scp, sftp and a local forward, and Git for Windows' ssh with ssh and scp.
func TestRealOpenSSHOnWindows(t *testing.T) {
	t.Parallel()
	windows, git := findClients(t)
	socket := filepath.Join(harness.ShortTempDir(t), "koko.sock")
	koko := buildKoko(t, removedAtTheEnd(t), socket)
	d := joinDevice(t, koko, windows)

	t.Run("OpenSSH for Windows", func(t *testing.T) {
		u := d.bob.with(windows)
		t.Run("ssh", func(t *testing.T) {
			t.Parallel()
			sshRunsACommand(t, u.in(t))
		})
		t.Run("scp", func(t *testing.T) {
			t.Parallel()
			scpCopiesBothWays(t, u.in(t), d.files)
		})
		t.Run("sftp", func(t *testing.T) {
			t.Parallel()
			sftpCopiesBothWays(t, u.in(t), d.web1)
		})
		t.Run("local forward", func(t *testing.T) {
			t.Parallel()
			aLocalForwardReachesTheHost(t, u.in(t))
		})
		t.Run("known_hosts under ~", func(t *testing.T) {
			t.Parallel()
			knownHostsUnderTheProfile(t, u.in(t))
		})
	})
	t.Run("Git for Windows' ssh", func(t *testing.T) {
		u := d.bob.with(git)
		t.Run("ssh", func(t *testing.T) {
			t.Parallel()
			sshRunsACommand(t, u.in(t))
		})
		t.Run("scp", func(t *testing.T) {
			t.Parallel()
			scpCopiesBothWays(t, u.in(t), d.files)
		})
	})

	everyTunnelGoesThroughTheDeviceMux(t, d.bob.with(windows))
}

// knownHostsUnderTheProfile checks the one line the other subtests of
// OpenSSH for Windows replace: koko's ssh_config names known_hosts under
// ~, which OpenSSH for Windows expands to the profile directory Windows
// records for the user, the one it reads ~/.ssh/config from and koko
// writes under outside this test. The test's profile is a temporary
// USERPROFILE, which that ~ does not follow.
func knownHostsUnderTheProfile(t *testing.T, u *user) {
	me, err := osuser.Current()
	if err != nil {
		t.Fatal(err)
	}
	r := u.run(nil, u.tools.ssh, "-G", "-F", u.kokoSSHConfig(), "web1.koko")
	if r.status != 0 {
		t.Fatalf("ssh -G: %v", r)
	}
	var got []string
	sc := bufio.NewScanner(strings.NewReader(r.stdout))
	for sc.Scan() {
		if v, ok := strings.CutPrefix(sc.Text(), "userknownhostsfile "); ok {
			got = append(got, v)
		}
	}
	want := filepath.Join(me.HomeDir, ".ssh", "koko", "known_hosts")
	if len(got) != 1 || !strings.EqualFold(filepath.Clean(got[0]), want) {
		t.Errorf("ssh -G reads koko's ssh_config as UserKnownHostsFile %q, want %s, under the profile directory Windows records", got, want)
	}
}
