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
	"regexp"
	"strings"
	"testing"
	"time"
)

// repo is where install.sh downloads releases from.
const repo = "https://github.com/cieons/koko"

// machine is a fake machine for install.sh. Its bin directory is the
// script's whole PATH: fakes for the commands that answer about the machine
// (uname, systemctl, id, sudo), a fake curl that serves a fake GitHub from a
// directory and logs every URL it is asked for, fakes for the commands that
// write the CLI (mkdir, install, mv), which log each call and write under
// the machine's own root directory instead of /, and the real tools the
// script needs besides. The release binaries it serves are shell scripts
// that log how install.sh ran them, to the same log as the writing fakes.
type machine struct {
	t *testing.T
	// OS and Arch are what uname -s and uname -m print.
	OS, Arch string
	// Systemctl gives the machine systemctl, and Service a koko service
	// that systemctl cat koko.service shows.
	Systemctl, Service bool
	// NoSha256sum leaves sha256sum out, as on macOS, which has shasum.
	NoSha256sum bool
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

// newMachine makes a Linux amd64 machine with systemd and no koko server,
// where install.sh runs as a user with passwordless sudo, and GitHub has no
// koko release yet.
func newMachine(t *testing.T) *machine {
	t.Helper()
	m := &machine{t: t, OS: "Linux", Arch: "x86_64", Systemctl: true, Sudo: "nopasswd", dir: t.TempDir()}
	for _, d := range []string{"bin", "web", "log", "tmp", "home", "root"} {
		if err := os.Mkdir(m.path(d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return m
}

// newMac makes newMachine a Mac on Apple silicon, which has no systemctl
// and no sha256sum.
func newMac(t *testing.T) *machine {
	t.Helper()
	m := newMachine(t)
	m.OS, m.Arch, m.Systemctl, m.NoSha256sum = "Darwin", "arm64", false, true
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
// it is, the path it ran from, the uid it runs as, its arguments and what it
// reads on stdin.
func (m *machine) binary(tag, asset string) []byte {
	return fmt.Appendf(nil, `#!/bin/sh
{
	printf 'binary=%%s\n' '%s %s'
	printf 'path=%%s\n' "$0"
	printf 'uid=%%s\n' "$(id -u)"
	printf 'args=%%s\n' "$*"
	printf 'stdin=%%s\n' "$(%s)"
} >>'%s'
echo 'koko server install ran'
echo 'koko on stderr' >&2
exit %d
`, asset, tag, m.tool("cat"), m.path("log", "ran"), m.KokoStatus)
}

// release publishes tag on the fake GitHub as a release since the split:
// its page, its binaries, koko's and kokod's, and a SHA256SUMS that lists
// them.
func (m *machine) release(tag string) {
	m.publish(tag, "koko-linux-amd64", "koko-linux-arm64", "koko-darwin-arm64", "kokod-linux-amd64", "kokod-linux-arm64")
}

// releaseBeforeTheSplit publishes tag as v0.1.0 and v0.2.0 were: koko's
// binaries alone, each the whole koko, whose server install installs the
// koko server by itself.
func (m *machine) releaseBeforeTheSplit(tag string) {
	m.publish(tag, "koko-linux-amd64", "koko-linux-arm64", "koko-darwin-arm64")
}

func (m *machine) publish(tag string, assets ...string) {
	m.t.Helper()
	m.serve(repo+"/releases/tag/"+tag, []byte("<html>"))
	var sums bytes.Buffer
	for _, asset := range assets {
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
	realTools := []string{"rm", "chmod", "awk", "true"}
	if !m.NoSha256sum {
		realTools = append(realTools, "sha256sum")
	}
	for _, name := range realTools {
		if err := os.Symlink(m.tool(name), m.path("bin", name)); err != nil {
			m.t.Fatal(err)
		}
	}
	// mktemp -d makes its directory under the machine's tmp directory, as
	// GNU's does under TMPDIR, where macOS's would take the user's own.
	m.fake("mktemp", fmt.Sprintf(`[ "$*" = -d ] || { echo "fake mktemp: unexpected $*" >&2; exit 1; }
exec '%s' -d '%s/tmp.XXXXXXXXXX'
`, m.tool("mktemp"), m.path("tmp")))
	// shasum answers only as install.sh asks it, -a 256, as macOS's does.
	m.fake("shasum", fmt.Sprintf(`[ "$1 $2" = '-a 256' ] || { echo "fake shasum: unexpected $*" >&2; exit 1; }
shift 2
exec '%s' "$@"
`, m.tool("sha256sum")))
	m.fake("uname", fmt.Sprintf(`case $1 in
-s) echo '%s' ;;
-m) echo '%s' ;;
*) echo "fake uname: unexpected $*" >&2; exit 1 ;;
esac
`, m.OS, m.Arch))
	if m.Systemctl {
		// What it prints for a koko service goes to stdout, as systemctl's
		// does, so that a run that let it through would show it there.
		m.fake("systemctl", fmt.Sprintf(`[ "$*" = 'cat koko.service' ] || { echo "fake systemctl: unexpected $*" >&2; exit 1; }
if [ '%t' = true ]; then
	echo '# /etc/systemd/system/koko.service'
	echo '[Service]'
else
	echo 'No files found for koko.service.' >&2
	exit 1
fi
`, m.Service))
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
	// mkdir, install and mv log each call with the uid it runs as, then run
	// the real command with every absolute path outside the temporary
	// directories moved under the machine's root directory.
	for _, name := range []string{"mkdir", "install", "mv"} {
		m.fake(name, fmt.Sprintf(`printf '%%s %%s (uid %%s)\n' '%[1]s' "$*" "$(id -u)" >>'%[2]s'
for arg; do
	case $arg in
	'%[3]s'/*) ;;
	/*) arg='%[4]s'$arg ;;
	esac
	set -- "$@" "$arg"
	shift
done
exec '%[5]s' "$@"
`, name, m.path("log", "ran"), m.path("tmp"), m.path("root"), m.tool(name)))
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
	// Curl is every URL the script asked curl for. Ran is, in order, what
	// each release binary that ran logged and each call of mkdir, install
	// and mv, with the temporary directory install.sh made shown as <tmp>.
	Curl, Ran string
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
	tmp := regexp.MustCompile(regexp.QuoteMeta(m.path("tmp")) + `/[^/\s]+`)
	r.Curl, r.Ran = m.log("curl"), tmp.ReplaceAllString(m.log("ran"), "<tmp>")
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

// serverInstall is what the release binary asset of tag logs when
// install.sh runs its koko server install as root with args.
func serverInstall(tag, asset, args string) string {
	return "binary=" + asset + " " + tag + "\npath=<tmp>/" + asset + "\nuid=0\nargs=" + strings.TrimSpace("server install "+args) + "\nstdin=\n"
}

// writesTheCLI is what the writing fakes log when install.sh writes asset
// as root beside /usr/local/bin/koko and renames it into place.
func writesTheCLI(asset string) string {
	return "mkdir -p -m 0755 /usr/local/bin (uid 0)\n" +
		"install -m 0755 <tmp>/" + asset + " /usr/local/bin/.koko.new (uid 0)\n" +
		"mv -f /usr/local/bin/.koko.new /usr/local/bin/koko (uid 0)\n"
}

// installed checks that the machine's /usr/local/bin/koko is the release
// binary asset of tag, executable by everyone, in a /usr/local/bin made
// 0755, with nothing beside it.
func (m *machine) installed(tag, asset string) {
	t := m.t
	t.Helper()
	got, err := os.ReadFile(m.path("root", "usr", "local", "bin", "koko"))
	if err != nil {
		t.Fatalf("no CLI was installed: %v", err)
	}
	if !bytes.Equal(got, m.binary(tag, asset)) {
		t.Errorf("/usr/local/bin/koko is not %s of %s:\n%s", asset, tag, got)
	}
	for path, mode := range map[string]os.FileMode{"usr/local/bin": os.ModeDir | 0o755, "usr/local/bin/koko": 0o755} {
		info, err := os.Stat(m.path("root", path))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode() != mode {
			t.Errorf("/%s has mode %v, want %v", path, info.Mode(), mode)
		}
	}
	entries, err := os.ReadDir(m.path("root", "usr", "local", "bin"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("/usr/local/bin holds %v, want koko alone", entries)
	}
}

// A pasted line with arguments and no KOKO_VERSION installs the latest
// release's binary for the machine, checked against that release's
// SHA256SUMS: it runs its koko server install as root with the script's
// arguments, then writes it as /usr/local/bin/koko, and leaves nothing
// behind. stdout is koko's alone, so --json output stays clean. kokod is
// koko server install's to download, never install.sh's.
func TestInstallsTheLatestReleaseWithTheKokoServer(t *testing.T) {
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
	wantRan := serverInstall("v0.2.0", "koko-linux-amd64", "--owner alice --address koko.example.com") + writesTheCLI("koko-linux-amd64")
	if r.Ran != wantRan {
		t.Errorf("ran:\n%s\nwant:\n%s", r.Ran, wantRan)
	}
	m.installed("v0.2.0", "koko-linux-amd64")
	if r.Stdout != "koko server install ran\n" {
		t.Errorf("stdout = %q, want koko's alone", r.Stdout)
	}
	if !strings.Contains(r.Stderr, "koko v0.2.0 for linux/amd64") || !strings.Contains(r.Stderr, "koko on stderr\n") ||
		!strings.HasSuffix(r.Stderr, "install.sh: installed koko v0.2.0 at /usr/local/bin/koko\n") {
		t.Errorf("stderr should name the version, carry koko's, then say where koko went:\n%s", r.Stderr)
	}
	if r.Tmp != nil {
		t.Errorf("install.sh left %v in its temporary directory", r.Tmp)
	}
}

// With no arguments, on a machine with no koko server, install.sh installs
// the CLI alone: the binary for the machine's OS and architecture, written
// beside /usr/local/bin/koko and renamed into place. It runs no
// koko server install, and needs no systemd. A Mac, which has shasum and no
// sha256sum, checks the binary with shasum -a 256.
func TestInstallsTheCLIAlone(t *testing.T) {
	t.Parallel()
	linux := func(arch string) func(*testing.T) *machine {
		return func(t *testing.T) *machine {
			m := newMachine(t)
			m.Arch = arch
			return m
		}
	}
	for name, tc := range map[string]struct {
		machine func(*testing.T) *machine
		asset   string
	}{
		"Linux x86_64":  {linux("x86_64"), "koko-linux-amd64"},
		"Linux amd64":   {linux("amd64"), "koko-linux-amd64"},
		"Linux aarch64": {linux("aarch64"), "koko-linux-arm64"},
		"Linux arm64":   {linux("arm64"), "koko-linux-arm64"},
		"Linux with no systemctl": {func(t *testing.T) *machine {
			m := newMachine(t)
			m.Systemctl = false
			return m
		}, "koko-linux-amd64"},
		"Linux as root": {func(t *testing.T) *machine {
			m := newMachine(t)
			m.Root, m.Sudo = true, ""
			return m
		}, "koko-linux-amd64"},
		"macOS arm64 with shasum": {newMac, "koko-darwin-arm64"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			m := tc.machine(t)
			m.release("v0.2.0")
			m.latest("v0.2.0")

			r := m.run()

			if r.Status != 0 {
				t.Fatalf("status %d, stderr:\n%s", r.Status, r.Stderr)
			}
			wantCurl := repo + "/releases/latest\n" +
				repo + "/releases/download/v0.2.0/SHA256SUMS\n" +
				repo + "/releases/download/v0.2.0/" + tc.asset + "\n"
			if r.Curl != wantCurl {
				t.Errorf("curl was asked for:\n%s\nwant:\n%s", r.Curl, wantCurl)
			}
			if r.Ran != writesTheCLI(tc.asset) {
				t.Errorf("ran:\n%s\nwant:\n%s", r.Ran, writesTheCLI(tc.asset))
			}
			m.installed("v0.2.0", tc.asset)
			if r.Stdout != "" || !strings.HasSuffix(r.Stderr, "install.sh: installed koko v0.2.0 at /usr/local/bin/koko\n") {
				t.Errorf("stdout = %q, stderr:\n%s", r.Stdout, r.Stderr)
			}
			if r.Tmp != nil {
				t.Errorf("install.sh left %v in its temporary directory", r.Tmp)
			}
		})
	}
}

// The install line again, without arguments, on a machine whose systemd
// knows koko.service upgrades both: koko server install first, from the
// temporary directory with no arguments, and the CLI only after it.
func TestUpgradesAnInstalledKokoServerFirst(t *testing.T) {
	t.Parallel()
	m := newMachine(t)
	m.Service = true
	m.release("v0.2.0")
	m.latest("v0.2.0")

	r := m.run()

	if r.Status != 0 {
		t.Fatalf("status %d, stderr:\n%s", r.Status, r.Stderr)
	}
	wantRan := serverInstall("v0.2.0", "koko-linux-amd64", "") + writesTheCLI("koko-linux-amd64")
	if r.Ran != wantRan {
		t.Errorf("ran:\n%s\nwant:\n%s", r.Ran, wantRan)
	}
	m.installed("v0.2.0", "koko-linux-amd64")
	if r.Stdout != "koko server install ran\n" {
		t.Errorf("stdout = %q, want koko's alone", r.Stdout)
	}
}

// A release from before the split, v0.1.0 or v0.2.0, has no kokod, and its
// koko-<os>-<arch> is the whole koko, whose server install installs the
// koko server by itself. install.sh installs it by the same rules.
func TestInstallsAReleaseFromBeforeTheSplit(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		args    []string
		service bool
		want    string
	}{
		"with arguments":             {args: []string{"--owner", "alice"}, want: serverInstall("v0.1.0", "koko-linux-amd64", "--owner alice")},
		"onto its koko server":       {service: true, want: serverInstall("v0.1.0", "koko-linux-amd64", "")},
		"as the CLI, no koko server": {},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			m := newMachine(t)
			m.Service = tc.service
			m.releaseBeforeTheSplit("v0.1.0")
			m.release("v0.2.0")
			m.latest("v0.2.0")
			m.Env = []string{"KOKO_VERSION=v0.1.0"}

			r := m.run(tc.args...)

			if r.Status != 0 {
				t.Fatalf("status %d, stderr:\n%s", r.Status, r.Stderr)
			}
			if want := tc.want + writesTheCLI("koko-linux-amd64"); r.Ran != want {
				t.Errorf("ran:\n%s\nwant:\n%s", r.Ran, want)
			}
			m.installed("v0.1.0", "koko-linux-amd64")
		})
	}
}

// refused checks that a run stopped before downloading, running or writing
// anything, saying why on stderr with every word of want.
func refused(t *testing.T, r result, status int, want ...string) {
	t.Helper()
	if r.Status != status {
		t.Errorf("status %d, want %d", r.Status, status)
	}
	if r.Curl != "" || r.Ran != "" || r.Stdout != "" || r.Tmp != nil {
		t.Errorf("a refused run downloaded, ran or wrote something:\ncurl: %s\nran: %s\nstdout: %s\ntmp: %v", r.Curl, r.Ran, r.Stdout, r.Tmp)
	}
	for _, w := range want {
		if !strings.Contains(r.Stderr, w) {
			t.Errorf("stderr does not say %q:\n%s", w, r.Stderr)
		}
	}
}

// A machine a release has no koko binary for is refused before any
// download: another OS, another architecture, and a Mac on Intel.
func TestRefusesAMachineWithNoKokoBinary(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		os, arch string
		want     []string
	}{
		"Linux riscv64": {os: "Linux", arch: "riscv64", want: []string{"riscv64", "amd64", "arm64"}},
		"macOS x86_64":  {os: "Darwin", arch: "x86_64", want: []string{"x86_64", "arm64", "Apple silicon"}},
		"FreeBSD":       {os: "FreeBSD", arch: "amd64", want: []string{"FreeBSD", "Linux", "macOS"}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			m := newMachine(t)
			m.OS, m.Arch = tc.os, tc.arch
			m.release("v0.2.0")
			m.latest("v0.2.0")

			r := m.run("--owner", "alice")

			refused(t, r, 1, append(tc.want, "nothing was written")...)
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

			if r.Status != 0 || !strings.HasPrefix(r.Ran, "binary=koko-linux-amd64 v0.1.0\n") {
				t.Errorf("status %d, ran:\n%s\nstderr:\n%s", r.Status, r.Ran, r.Stderr)
			}
			m.installed("v0.1.0", "koko-linux-amd64")
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

// stopped checks that a run downloaded but ran and wrote nothing, left
// nothing behind, and said why on stderr with every word of want.
func stopped(t *testing.T, r result, want ...string) {
	t.Helper()
	if r.Status != 1 {
		t.Errorf("status %d, want 1", r.Status)
	}
	if r.Ran != "" || r.Stdout != "" || r.Tmp != nil {
		t.Errorf("a stopped run ran, wrote or left something:\nran: %s\nstdout: %s\ntmp: %v", r.Ran, r.Stdout, r.Tmp)
	}
	for _, w := range want {
		if !strings.Contains(r.Stderr, w) {
			t.Errorf("stderr does not say %q:\n%s", w, r.Stderr)
		}
	}
}

// A binary whose bytes do not match the release's SHA256SUMS, damaged or
// swapped, is never run or installed, with or without arguments.
func TestStopsWhenTheChecksumDoesNotMatch(t *testing.T) {
	t.Parallel()
	for name, args := range map[string][]string{"the CLI alone": nil, "with the koko server": {"--owner", "alice"}} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			m := newMachine(t)
			m.release("v0.2.0")
			m.latest("v0.2.0")
			m.serve(repo+"/releases/download/v0.2.0/koko-linux-amd64", append(m.binary("v0.2.0", "koko-linux-amd64"), '#'))

			r := m.run(args...)

			stopped(t, r, "koko-linux-amd64", "SHA256SUMS", "not run")
		})
	}
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

	if r.Status != 0 || !strings.HasPrefix(r.Ran, "binary=koko-linux-amd64 v0.2.0\n") {
		t.Errorf("status %d, ran:\n%s\nstderr:\n%s", r.Status, r.Ran, r.Stderr)
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

// Run as root, install.sh runs koko server install and writes the CLI
// itself, without sudo.
func TestRunsAsRootWithoutSudo(t *testing.T) {
	t.Parallel()
	m := newMachine(t)
	m.Root, m.Sudo = true, ""
	m.release("v0.2.0")
	m.latest("v0.2.0")

	r := m.run("--owner", "alice")

	if want := serverInstall("v0.2.0", "koko-linux-amd64", "--owner alice") + writesTheCLI("koko-linux-amd64"); r.Status != 0 || r.Ran != want {
		t.Errorf("status %d, ran:\n%s\nwant:\n%s\nstderr:\n%s", r.Status, r.Ran, want, r.Stderr)
	}
}

// install.sh writes /usr/local/bin and runs koko server install as root,
// and sudo that would ask for a password is never left to ask: install.sh
// stops before any download and says how to run it as root, with the same
// arguments when it was given some.
func TestRefusesAUserWhoCannotBeRootWithoutAPassword(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		sudo string
		args []string
		hint string
	}{
		"sudo asks for a password": {sudo: "password", args: []string{"--owner", "alice"}, hint: "| sudo sh -s -- <the same arguments>"},
		"no sudo":                  {args: []string{"--owner", "alice"}, hint: "| sudo sh -s -- <the same arguments>"},
		"no sudo, no arguments":    {hint: "| sudo sh;"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			m := newMachine(t)
			m.Sudo = tc.sudo
			m.release("v0.2.0")
			m.latest("v0.2.0")

			r := m.run(tc.args...)

			refused(t, r, 1, "root", tc.hint, "nothing was written")
		})
	}
}

// When koko server install fails, install.sh exits with its status, so a
// script or an agent reads koko's answer as if it ran koko itself, and
// leaves the CLI as it was: a failed upgrade of the koko server never
// leaves a CLI of another version beside it.
func TestLeavesTheCLIAsItWasWhenKokoServerInstallFails(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		args    []string
		service bool
		want    string
	}{
		"with arguments":       {args: []string{"--port", "0"}, want: "--port 0"},
		"onto its koko server": {service: true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			m := newMachine(t)
			m.Service = tc.service
			m.KokoStatus = 2
			m.release("v0.2.0")
			m.latest("v0.2.0")

			r := m.run(tc.args...)

			if r.Status != 2 {
				t.Errorf("status %d, want koko's 2", r.Status)
			}
			if want := serverInstall("v0.2.0", "koko-linux-amd64", tc.want); r.Ran != want {
				t.Errorf("ran:\n%s\nwant koko server install alone:\n%s", r.Ran, want)
			}
			if r.Stdout != "koko server install ran\n" || !strings.Contains(r.Stderr, "left as it was") {
				t.Errorf("stdout = %q, stderr should say the CLI was left as it was:\n%s", r.Stdout, r.Stderr)
			}
			if r.Tmp != nil {
				t.Errorf("install.sh left %v in its temporary directory", r.Tmp)
			}
		})
	}
}
