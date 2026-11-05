// Package release holds what a koko release publishes besides the binaries,
// which make dist builds: install.sh. Its tests run install.sh as people and
// agents run it, `curl … | sh -s -- <args>`, on a fake machine.
package release

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// repo is where install.sh downloads releases from.
const repo = "https://github.com/cieons/koko"

// machine is a fake machine for install.sh. Its bin directory is the
// script's whole PATH: fakes for the commands that answer about the machine
// (uname, systemctl, id, sudo), a fake curl that serves a fake GitHub from a
// directory and logs every URL it is asked for, and the real tools the
// script needs besides. The release binaries it serves are shell scripts
// that log how install.sh ran them.
type machine struct {
	t *testing.T
	// OS and Arch are what uname -s and uname -m print.
	OS, Arch string
	// Systemd is what systemctl is-system-running prints; with "", the
	// machine has no systemctl.
	Systemd string
	// Root runs install.sh as root; otherwise as uid 1000.
	Root bool
	// Sudo is "nopasswd" for sudo that runs a command as root without a
	// password, "password" for sudo that would ask for one, or "" for no
	// sudo.
	Sudo string
	// Env is added to install.sh's environment, such as KOKO_VERSION.
	Env []string
	// KokoStatus is the exit status of the release binaries published after
	// it is set.
	KokoStatus int

	dir string
}

// newMachine makes a Linux amd64 machine running systemd, where install.sh
// runs as a user with passwordless sudo, and GitHub has no koko release yet.
func newMachine(t *testing.T) *machine {
	t.Helper()
	m := &machine{t: t, OS: "Linux", Arch: "x86_64", Systemd: "running", Sudo: "nopasswd", dir: t.TempDir()}
	for _, d := range []string{"bin", "web", "log", "tmp", "home"} {
		if err := os.Mkdir(m.path(d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return m
}

func (m *machine) path(elem ...string) string {
	return filepath.Join(append([]string{m.dir}, elem...)...)
}

// serve makes url answer with body on the fake GitHub.
func (m *machine) serve(url string, body []byte) {
	m.t.Helper()
	path := m.path("web", strings.TrimPrefix(url, "https://"))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		m.t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		m.t.Fatal(err)
	}
}

// binary is the release binary asset of tag: a script that logs which binary
// it is, the uid it runs as, its arguments and what it reads on stdin.
func (m *machine) binary(tag, asset string) []byte {
	return fmt.Appendf(nil, `#!/bin/sh
{
	printf 'binary=%%s\n' '%s %s'
	printf 'uid=%%s\n' "$(id -u)"
	printf 'args=%%s\n' "$*"
	printf 'stdin=%%s\n' "$(%s)"
} >>'%s'
echo 'koko server install ran'
echo 'koko on stderr' >&2
exit %d
`, asset, tag, m.tool("cat"), m.path("log", "koko"), m.KokoStatus)
}

// release publishes tag on the fake GitHub: its page, its three binaries and
// a SHA256SUMS that lists them.
func (m *machine) release(tag string) {
	m.t.Helper()
	m.serve(repo+"/releases/tag/"+tag, []byte("<html>"))
	var sums bytes.Buffer
	for _, asset := range []string{"koko-linux-amd64", "koko-linux-arm64", "koko-darwin-arm64"} {
		body := m.binary(tag, asset)
		m.serve(repo+"/releases/download/"+tag+"/"+asset, body)
		sum := sha256.Sum256(body)
		fmt.Fprintf(&sums, "%s  %s\n", hex.EncodeToString(sum[:]), asset)
	}
	m.serve(repo+"/releases/download/"+tag+"/SHA256SUMS", sums.Bytes())
}

// latest makes tag the latest release, whose page GitHub's /releases/latest
// redirects to.
func (m *machine) latest(tag string) {
	m.redirect(repo+"/releases/latest", repo+"/releases/tag/"+tag)
}

// redirect makes url redirect to target on the fake GitHub.
func (m *machine) redirect(url, target string) {
	m.serve(url+".location", []byte(target))
}

// tool is the absolute path of a real command.
func (m *machine) tool(name string) string {
	m.t.Helper()
	path, err := exec.LookPath(name)
	if err != nil {
		m.t.Fatalf("install.sh needs %s on the machine running the tests: %v", name, err)
	}
	return path
}

// fake writes the command name into the bin directory.
func (m *machine) fake(name, script string) {
	m.t.Helper()
	if err := os.WriteFile(m.path("bin", name), []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		m.t.Fatal(err)
	}
}

// fakes writes the bin directory for the machine as set, once: a machine
// runs install.sh once.
func (m *machine) fakes() {
	m.t.Helper()
	for _, name := range []string{"mktemp", "rm", "chmod", "awk", "sha256sum", "true"} {
		if err := os.Symlink(m.tool(name), m.path("bin", name)); err != nil {
			m.t.Fatal(err)
		}
	}
	m.fake("uname", fmt.Sprintf(`case $1 in
-s) echo '%s' ;;
-m) echo '%s' ;;
*) echo "fake uname: unexpected $*" >&2; exit 1 ;;
esac
`, m.OS, m.Arch))
	if m.Systemd != "" {
		m.fake("systemctl", fmt.Sprintf(`[ "$*" = is-system-running ] || { echo "fake systemctl: unexpected $*" >&2; exit 1; }
echo '%s'
[ '%[1]s' = running ]
`, m.Systemd))
	}
	uid := 1000
	if m.Root {
		uid = 0
	}
	m.fake("id", fmt.Sprintf(`[ "$*" = -u ] || { echo "fake id: unexpected $*" >&2; exit 1; }
if [ "${FAKE_SUDO:-}" = 1 ]; then echo 0; else echo %d; fi
`, uid))
	switch m.Sudo {
	case "nopasswd":
		m.fake("sudo", `[ "$1" = -n ] || { echo "fake sudo: would ask for a password" >&2; exit 1; }
shift
FAKE_SUDO=1
export FAKE_SUDO
exec "$@"
`)
	case "password":
		m.fake("sudo", `echo "sudo: a password is required" >&2
exit 1
`)
	}
	// curl serves <url> from web/<url without https://>, where <url>.location
	// holds where it redirects to, followed only with -L. As the real one
	// does, it fails on a missing file only with -f, and otherwise writes
	// the error page; -w '%{url_effective}' prints the URL it ended on. It
	// refuses a call without --proto =https, which keeps every redirect on
	// HTTPS: no fake GitHub can show a redirect to plain HTTP.
	m.fake("curl", fmt.Sprintf(`out= format= url= fail= follow= https=
while [ $# -gt 0 ]; do
	case $1 in
	-o) out=$2; shift ;;
	-w) format=$2; shift ;;
	--proto) [ "$2" = =https ] && https=1; shift ;;
	--*) ;;
	-*)
		case $1 in *f*) fail=1 ;; esac
		case $1 in *L*) follow=1 ;; esac
		;;
	*) url=$1 ;;
	esac
	shift
done
printf '%%s\n' "$url" >>'%[1]s'
[ -n "$https" ] || { echo "fake curl: no --proto =https for $url" >&2; exit 2; }
web='%[2]s'
while [ -n "$follow" ] && [ -f "$web/${url#https://}.location" ]; do
	url=$('%[3]s' "$web/${url#https://}.location")
done
path=$web/${url#https://}
[ -n "$out" ] || out=/dev/stdout
if [ -f "$path" ]; then
	'%[3]s' "$path" >"$out"
elif [ -n "$fail" ]; then
	echo "curl: (22) The requested URL returned error: 404" >&2
	exit 22
else
	echo 'Not Found' >"$out"
fi
[ "$format" != '%%{url_effective}' ] || printf '%%s' "$url"
`, m.path("log", "curl"), m.path("web"), m.tool("cat")))
}

// result is what one run of install.sh did.
type result struct {
	Status         int
	Stdout, Stderr string
	// Curl is every URL the script asked curl for, and Koko what each release
	// binary that ran logged.
	Curl, Koko string
	// Tmp lists what the script left in its temporary directory.
	Tmp []string
}

// run runs install.sh as `curl … | sh -s -- args` does: the script on sh's
// stdin, which stays open, as a pipe from curl may, so that anything that
// reads stdin, as a prompt would, waits and the run times out.
func (m *machine) run(args ...string) result {
	m.t.Helper()
	script, err := os.ReadFile("install.sh")
	if err != nil {
		m.t.Fatal(err)
	}
	m.fakes()
	ctx, cancel := context.WithTimeout(m.t.Context(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/bin/sh", append([]string{"-s", "--"}, args...)...)
	cmd.Env = append([]string{
		"PATH=" + m.path("bin"),
		"HOME=" + m.path("home"),
		"TMPDIR=" + m.path("tmp"),
	}, m.Env...)
	cmd.WaitDelay = time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		m.t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		m.t.Fatal(err)
	}
	if _, err := stdin.Write(script); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		m.t.Fatalf("hand install.sh to sh: %v", err)
	}
	err = cmd.Wait()
	if ctx.Err() != nil {
		m.t.Fatalf("install.sh did not end within 30s: it waits on stdin or a prompt\nstderr: %s", stderr.String())
	}
	r := result{Stdout: stdout.String(), Stderr: stderr.String()}
	if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
		r.Status = exitErr.ExitCode()
	} else if err != nil {
		m.t.Fatalf("run install.sh: %v", err)
	}
	r.Curl, r.Koko = m.log("curl"), m.log("koko")
	entries, err := os.ReadDir(m.path("tmp"))
	if err != nil {
		m.t.Fatal(err)
	}
	for _, e := range entries {
		r.Tmp = append(r.Tmp, e.Name())
	}
	return r
}

func (m *machine) log(name string) string {
	b, err := os.ReadFile(m.path("log", name))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		m.t.Fatal(err)
	}
	return string(b)
}

// A pasted line with no KOKO_VERSION installs the latest release's binary
// for the machine, checked against that release's SHA256SUMS, by running its
// koko server install as root with the script's arguments, and leaves
// nothing behind. stdout is koko's alone, so --json output stays clean.
func TestInstallsTheLatestRelease(t *testing.T) {
	t.Parallel()
	m := newMachine(t)
	m.release("v0.1.0")
	m.release("v0.2.0")
	m.latest("v0.2.0")

	r := m.run("--owner", "alice", "--address", "koko.example.com")

	if r.Status != 0 {
		t.Fatalf("status %d, stderr:\n%s", r.Status, r.Stderr)
	}
	wantCurl := repo + "/releases/latest\n" +
		repo + "/releases/download/v0.2.0/SHA256SUMS\n" +
		repo + "/releases/download/v0.2.0/koko-linux-amd64\n"
	if r.Curl != wantCurl {
		t.Errorf("curl was asked for:\n%s\nwant:\n%s", r.Curl, wantCurl)
	}
	wantKoko := "binary=koko-linux-amd64 v0.2.0\nuid=0\nargs=server install --owner alice --address koko.example.com\nstdin=\n"
	if r.Koko != wantKoko {
		t.Errorf("the release binary logged:\n%s\nwant:\n%s", r.Koko, wantKoko)
	}
	if r.Stdout != "koko server install ran\n" {
		t.Errorf("stdout = %q, want koko's alone", r.Stdout)
	}
	if !strings.Contains(r.Stderr, "koko v0.2.0") || !strings.HasSuffix(r.Stderr, "koko on stderr\n") {
		t.Errorf("stderr should name the version, then carry koko's:\n%s", r.Stderr)
	}
	if r.Tmp != nil {
		t.Errorf("install.sh left %v in its temporary directory", r.Tmp)
	}
}

// install.sh picks the binary for the machine's architecture.
func TestPicksTheBinaryForTheArchitecture(t *testing.T) {
	t.Parallel()
	for arch, asset := range map[string]string{"x86_64": "koko-linux-amd64", "aarch64": "koko-linux-arm64", "arm64": "koko-linux-arm64"} {
		t.Run(arch, func(t *testing.T) {
			t.Parallel()
			m := newMachine(t)
			m.Arch = arch
			m.release("v0.2.0")
			m.latest("v0.2.0")

			r := m.run("--owner", "alice")

			if r.Status != 0 || !strings.HasPrefix(r.Koko, "binary="+asset+" v0.2.0\n") {
				t.Errorf("status %d, the release binary logged:\n%s\nstderr:\n%s", r.Status, r.Koko, r.Stderr)
			}
			if !strings.HasSuffix(r.Curl, "/releases/download/v0.2.0/"+asset+"\n") {
				t.Errorf("curl was asked for:\n%s", r.Curl)
			}
		})
	}
}

// refused checks that a run stopped before downloading or running anything,
// saying why on stderr with every word of want.
func refused(t *testing.T, r result, status int, want ...string) {
	t.Helper()
	if r.Status != status {
		t.Errorf("status %d, want %d", r.Status, status)
	}
	if r.Curl != "" || r.Koko != "" || r.Stdout != "" || r.Tmp != nil {
		t.Errorf("a refused run downloaded, ran or wrote something:\ncurl: %s\nkoko: %s\nstdout: %s\ntmp: %v", r.Curl, r.Koko, r.Stdout, r.Tmp)
	}
	for _, w := range want {
		if !strings.Contains(r.Stderr, w) {
			t.Errorf("stderr does not say %q:\n%s", w, r.Stderr)
		}
	}
}

// A machine type with no koko binary is refused before any download.
func TestRefusesAnotherArchitecture(t *testing.T) {
	t.Parallel()
	m := newMachine(t)
	m.Arch = "riscv64"
	m.release("v0.2.0")
	m.latest("v0.2.0")

	r := m.run("--owner", "alice")

	refused(t, r, 1, "riscv64", "amd64", "arm64")
}

// The koko server runs only on Linux with systemd, so install.sh stops
// elsewhere before it downloads or writes anything.
func TestRefusesAMachineThatIsNotLinuxWithSystemd(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		os, systemd string
		want        []string
	}{
		"macOS":                        {os: "Darwin", systemd: "running", want: []string{"Linux", "Darwin"}},
		"no systemctl":                 {os: "Linux", want: []string{"systemd"}},
		"systemd not the init (PID 1)": {os: "Linux", systemd: "offline", want: []string{"systemd"}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			m := newMachine(t)
			m.OS, m.Systemd = tc.os, tc.systemd
			m.release("v0.2.0")
			m.latest("v0.2.0")

			r := m.run("--owner", "alice")

			refused(t, r, 1, append(tc.want, "nothing was written")...)
		})
	}
}

// systemd that is still booting, or running with a failed unit, runs the
// koko service all the same.
func TestTakesSystemdThatIsBootingOrDegraded(t *testing.T) {
	t.Parallel()
	for _, state := range []string{"initializing", "starting", "degraded"} {
		t.Run(state, func(t *testing.T) {
			t.Parallel()
			m := newMachine(t)
			m.Systemd = state
			m.release("v0.2.0")
			m.latest("v0.2.0")

			r := m.run("--owner", "alice")

			if r.Status != 0 || r.Koko == "" {
				t.Errorf("status %d, the release binary logged %q, stderr:\n%s", r.Status, r.Koko, r.Stderr)
			}
		})
	}
}

// KOKO_VERSION installs the release it names instead of the latest, with or
// without its "v".
func TestKokoVersionPinsARelease(t *testing.T) {
	t.Parallel()
	for _, version := range []string{"v0.1.0", "0.1.0"} {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			m := newMachine(t)
			m.release("v0.1.0")
			m.release("v0.2.0")
			m.latest("v0.2.0")
			m.Env = []string{"KOKO_VERSION=" + version}

			r := m.run("--owner", "alice")

			if r.Status != 0 || !strings.HasPrefix(r.Koko, "binary=koko-linux-amd64 v0.1.0\n") {
				t.Errorf("status %d, the release binary logged:\n%s\nstderr:\n%s", r.Status, r.Koko, r.Stderr)
			}
			wantCurl := repo + "/releases/download/v0.1.0/SHA256SUMS\n" +
				repo + "/releases/download/v0.1.0/koko-linux-amd64\n"
			if r.Curl != wantCurl {
				t.Errorf("curl was asked for:\n%s\nwant:\n%s", r.Curl, wantCurl)
			}
		})
	}
}

// A KOKO_VERSION that is not a version is refused as given wrong (exit 2),
// before any download.
func TestRefusesAKokoVersionThatIsNotAVersion(t *testing.T) {
	t.Parallel()
	for _, version := range []string{"latest", "v0.1.0/../../evil", "v1 2"} {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			m := newMachine(t)
			m.release("v0.2.0")
			m.latest("v0.2.0")
			m.Env = []string{"KOKO_VERSION=" + version}

			r := m.run("--owner", "alice")

			refused(t, r, 2, "KOKO_VERSION", "v0.3.0")
		})
	}
}

// stopped checks that a run downloaded but ran nothing, left nothing behind,
// and said why on stderr with every word of want.
func stopped(t *testing.T, r result, want ...string) {
	t.Helper()
	if r.Status != 1 {
		t.Errorf("status %d, want 1", r.Status)
	}
	if r.Koko != "" || r.Stdout != "" || r.Tmp != nil {
		t.Errorf("a stopped run ran or left something:\nkoko: %s\nstdout: %s\ntmp: %v", r.Koko, r.Stdout, r.Tmp)
	}
	for _, w := range want {
		if !strings.Contains(r.Stderr, w) {
			t.Errorf("stderr does not say %q:\n%s", w, r.Stderr)
		}
	}
}

// A binary whose bytes do not match the release's SHA256SUMS, damaged or
// swapped, never runs, as root or at all.
func TestStopsWhenTheChecksumDoesNotMatch(t *testing.T) {
	t.Parallel()
	m := newMachine(t)
	m.release("v0.2.0")
	m.latest("v0.2.0")
	m.serve(repo+"/releases/download/v0.2.0/koko-linux-amd64", append(m.binary("v0.2.0", "koko-linux-amd64"), '#'))

	r := m.run("--owner", "alice")

	stopped(t, r, "koko-linux-amd64", "SHA256SUMS", "not run")
}

// A binary the release's SHA256SUMS does not list is never run.
func TestStopsWhenSHA256SUMSDoesNotListTheBinary(t *testing.T) {
	t.Parallel()
	m := newMachine(t)
	m.release("v0.2.0")
	m.latest("v0.2.0")
	m.serve(repo+"/releases/download/v0.2.0/SHA256SUMS", []byte("0000  koko-linux-arm64\n"))

	r := m.run("--owner", "alice")

	stopped(t, r, "SHA256SUMS", "koko-linux-amd64", "not run")
}

// A download that fails stops the run, naming what could not be downloaded.
func TestStopsWhenADownloadFails(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		// published publishes v0.1.0, all but the asset at failing.
		published bool
		failing   string
	}{
		"the binary":              {published: true, failing: repo + "/releases/download/v0.1.0/koko-linux-amd64"},
		"SHA256SUMS":              {published: true, failing: repo + "/releases/download/v0.1.0/SHA256SUMS"},
		"a release not published": {failing: repo + "/releases/download/v0.1.0/SHA256SUMS"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			m := newMachine(t)
			if tc.published {
				m.release("v0.1.0")
				if err := os.Remove(m.path("web", strings.TrimPrefix(tc.failing, "https://"))); err != nil {
					t.Fatal(err)
				}
			}
			m.Env = []string{"KOKO_VERSION=v0.1.0"}

			r := m.run("--owner", "alice")

			stopped(t, r, tc.failing)
		})
	}
}

// Once the repository has moved, GitHub redirects /releases/latest through
// its new path, and install.sh follows it to the latest release.
func TestFindsTheLatestReleaseOfAMovedRepository(t *testing.T) {
	t.Parallel()
	m := newMachine(t)
	m.release("v0.2.0")
	moved := "https://github.com/koko-dev/koko"
	m.redirect(repo+"/releases/latest", moved+"/releases/latest")
	m.redirect(moved+"/releases/latest", moved+"/releases/tag/v0.2.0")
	m.serve(moved+"/releases/tag/v0.2.0", []byte("<html>"))

	r := m.run("--owner", "alice")

	if r.Status != 0 || !strings.HasPrefix(r.Koko, "binary=koko-linux-amd64 v0.2.0\n") {
		t.Errorf("status %d, the release binary logged:\n%s\nstderr:\n%s", r.Status, r.Koko, r.Stderr)
	}
}

// With no release published, there is no latest to install.
func TestStopsWhenThereIsNoRelease(t *testing.T) {
	t.Parallel()
	m := newMachine(t)

	r := m.run("--owner", "alice")

	stopped(t, r, repo+"/releases/latest")
	if r.Curl != repo+"/releases/latest\n" {
		t.Errorf("curl was asked for:\n%s", r.Curl)
	}
}

// Run as root, install.sh runs koko server install itself, without sudo.
func TestRunsAsRootWithoutSudo(t *testing.T) {
	t.Parallel()
	m := newMachine(t)
	m.Root, m.Sudo = true, ""
	m.release("v0.2.0")
	m.latest("v0.2.0")

	r := m.run("--owner", "alice")

	if r.Status != 0 || !strings.Contains(r.Koko, "uid=0\n") {
		t.Errorf("status %d, the release binary logged:\n%s\nstderr:\n%s", r.Status, r.Koko, r.Stderr)
	}
}

// koko server install runs as root, and sudo that would ask for a password
// is never left to ask: install.sh stops before any download and says how to
// run it as root.
func TestRefusesAUserWhoCannotBeRootWithoutAPassword(t *testing.T) {
	t.Parallel()
	for _, sudo := range []string{"password", ""} {
		t.Run("sudo="+sudo, func(t *testing.T) {
			t.Parallel()
			m := newMachine(t)
			m.Sudo = sudo
			m.release("v0.2.0")
			m.latest("v0.2.0")

			r := m.run("--owner", "alice")

			refused(t, r, 1, "root", "| sudo sh -s --", "nothing was written")
		})
	}
}

// install.sh exits with koko server install's status, so a script or an
// agent reads koko's answer as if it ran koko itself.
func TestExitsWithKokosStatus(t *testing.T) {
	t.Parallel()
	m := newMachine(t)
	m.KokoStatus = 2
	m.release("v0.2.0")
	m.latest("v0.2.0")

	r := m.run("--port", "0")

	if r.Status != 2 || r.Koko == "" || r.Tmp != nil {
		t.Errorf("status %d, the release binary logged %q, tmp %v; want koko's 2, and nothing left", r.Status, r.Koko, r.Tmp)
	}
}
