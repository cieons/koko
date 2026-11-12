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
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/cieons/koko/internal/harness/skip"
)

// repo is where install.sh downloads releases from.
const repo = "https://github.com/cieons/koko"

// machine is a fake machine for install.sh. Its bin directory comes first on
// the script's PATH: fakes for the commands that answer about the machine
// (uname, systemctl, id, sudo, ls), a fake curl that serves a fake GitHub
// from a directory and logs every URL it is asked for, fakes for the
// commands that write the CLI (mkdir, install, mv), which log each call and
// write under the machine's own root directory instead of /, and the real
// tools the script needs besides. The release binaries it serves are shell
// scripts that log how install.sh ran them, to the same log as the writing
// fakes. The user's home is the machine's home directory.
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
	// Path is PATH after the bin directory. An entry outside the machine's
	// directory, such as /usr/local/bin, is under its root directory, as
	// the writing fakes write there.
	Path []string
	// Shell is SHELL, unset when empty.
	Shell string
	// NoHome leaves HOME unset.
	NoHome bool
	// UserOwned lists the files under the root directory, as install.sh
	// names them, that are the user's rather than root's.
	UserOwned []string
	// Env is added to install.sh's environment last, such as KOKO_VERSION.
	Env []string
	// KokoStatus is the exit status of the release binaries published after
	// it is set.
	KokoStatus int

	dir string
}

// newMachine makes a Linux amd64 machine with systemd and no koko server,
// where install.sh runs as a user with passwordless sudo whose PATH has
// ~/.local/bin, then /usr/local/bin, and GitHub has no koko release yet.
func newMachine(t *testing.T) *machine {
	t.Helper()
	skip.OnWindows(t, "install.sh installs koko on Linux and macOS, through /bin/sh and fake Unix commands; Windows gets install.ps1 (#156)")
	m := &machine{t: t, OS: "Linux", Arch: "x86_64", Systemctl: true, Sudo: "nopasswd", dir: t.TempDir()}
	for _, d := range []string{"bin", "web", "log", "tmp", "home", "root"} {
		if err := os.Mkdir(m.path(d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	m.Path = []string{m.userBin(), "/usr/local/bin"}
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

// userBin is the user's ~/.local/bin.
func (m *machine) userBin() string {
	return m.path("home", ".local", "bin")
}

// cliDir is where install.sh puts the CLI on this machine, as the script
// names it: /usr/local/bin as root, otherwise the user's ~/.local/bin.
func (m *machine) cliDir() string {
	if m.Root {
		return "/usr/local/bin"
	}
	return m.userBin()
}

// onDisk is where the path install.sh names is on the machine.
func (m *machine) onDisk(path string) string {
	if strings.HasPrefix(path, m.dir+"/") {
		return path
	}
	return m.path("root", path)
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
// it is, the path it ran from, the uid it runs as, the KOKO_VERSION it was
// given, its arguments and what it reads on stdin.
func (m *machine) binary(tag, asset string) []byte {
	return fmt.Appendf(nil, `#!/bin/sh
{
	printf 'binary=%%s\n' '%s %s'
	printf 'path=%%s\n' "$0"
	printf 'uid=%%s\n' "$(id -u)"
	printf 'KOKO_VERSION=%%s\n' "${KOKO_VERSION-}"
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

// put writes an executable file at path, as install.sh names it, on the
// machine.
func (m *machine) put(path, body string) {
	m.t.Helper()
	path = m.onDisk(path)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		m.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		m.t.Fatal(err)
	}
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
	realTools := []string{"rm", "chmod", "awk", "env", "true"}
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
	} else {
		// sh finds no systemctl, and never one in the real /usr/local/bin.
		m.fake("systemctl", `echo 'sh: systemctl: not found' >&2
exit 127
`)
	}
	uid := 1000
	if m.Root {
		uid = 0
	}
	m.fake("id", fmt.Sprintf(`[ "$*" = -u ] || { echo "fake id: unexpected $*" >&2; exit 1; }
if [ "${FAKE_SUDO:-}" = 1 ]; then echo 0; else echo %d; fi
`, uid))
	// sudo logs each call, and runs a command with an environment of its
	// own, as sudo's env_reset does, so that a variable reaches the command
	// only when named, as with sudo -n env KOKO_VERSION=….
	switch m.Sudo {
	case "nopasswd":
		m.fake("sudo", fmt.Sprintf(`printf '%%s\n' "$*" >>'%s'
[ "$1" = -n ] || { echo "fake sudo: would ask for a password" >&2; exit 1; }
shift
exec '%s' -i PATH="$PATH" FAKE_SUDO=1 "$@"
`, m.path("log", "sudo"), m.tool("env")))
	case "password":
		m.fake("sudo", fmt.Sprintf(`printf '%%s\n' "$*" >>'%s'
echo "sudo: a password is required" >&2
exit 1
`, m.path("log", "sudo")))
	}
	// mkdir, install and mv log each call with the uid it runs as, then run
	// the real command with every absolute path outside the machine's
	// directory moved under its root directory.
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
`, name, m.path("log", "ran"), m.dir, m.path("root"), m.tool(name)))
	}
	// ls -ldn shows a file under the root directory as root's, as / is,
	// unless UserOwned lists it, and one in the machine's directory as it is.
	m.fake("ls", fmt.Sprintf(`[ $# = 2 ] && [ "$1" = -ldn ] || { echo "fake ls: unexpected $*" >&2; exit 1; }
case $2 in
'%[1]s'/*) exec '%[3]s' -ldn "$2" ;;
esac
[ -e '%[2]s'"$2" ] || { echo "ls: $2: No such file or directory" >&2; exit 1; }
uid=0
for f in %[4]s; do [ "$f" != "$2" ] || uid=1000; done
printf '%%s 1 %%s 0 0 Jan  1 00:00 %%s\n' -rwxr-xr-x "$uid" "$2"
`, m.dir, m.path("root"), m.tool("ls"), strings.Join(m.UserOwned, " ")))
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

// commandV is a shell function put ahead of install.sh. command -v is a
// builtin that looks on the real filesystem, so on the fake machine it
// looks each PATH entry up as the writing fakes write: an entry outside the
// machine's directory under its root directory.
func (m *machine) commandV() string {
	return fmt.Sprintf(`command() {
	[ $# = 2 ] && [ "$1" = -v ] || { echo "fake command: unexpected $*" >&2; exit 1; }
	fake_ifs=$IFS
	IFS=:
	for fake_dir in $PATH; do
		IFS=$fake_ifs
		case $fake_dir in
		'%[1]s'/*) fake_file=$fake_dir/$2 ;;
		/*) fake_file='%[2]s'$fake_dir/$2 ;;
		*) continue ;;
		esac
		if [ -f "$fake_file" ] && [ -x "$fake_file" ]; then
			printf '%%s\n' "$fake_dir/$2"
			return 0
		fi
	done
	IFS=$fake_ifs
	return 1
}
`, m.dir, m.path("root"))
}

// result is what one run of install.sh did.
type result struct {
	Status         int
	Stdout, Stderr string
	// Curl is every URL the script asked curl for. Ran is, in order, what
	// each release binary that ran logged and each call of mkdir, install
	// and mv, and Sudo each call of sudo, with the temporary directory
	// install.sh made shown as <tmp>.
	Curl, Ran, Sudo string
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
	cmd.Env = []string{
		"PATH=" + strings.Join(append([]string{m.path("bin")}, m.Path...), ":"),
		"TMPDIR=" + m.path("tmp"),
	}
	if !m.NoHome {
		cmd.Env = append(cmd.Env, "HOME="+m.path("home"))
	}
	if m.Shell != "" {
		cmd.Env = append(cmd.Env, "SHELL="+m.Shell)
	}
	cmd.Env = append(cmd.Env, m.Env...)
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
	if _, err := stdin.Write(append([]byte(m.commandV()), script...)); err != nil {
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
	r.Curl = m.log("curl")
	r.Ran = tmp.ReplaceAllString(m.log("ran"), "<tmp>")
	r.Sudo = tmp.ReplaceAllString(m.log("sudo"), "<tmp>")
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

// files maps every file and directory under dir on the machine to its
// contents, "<dir>" for a directory.
func (m *machine) files(dir string) map[string]string {
	m.t.Helper()
	got := map[string]string{}
	err := filepath.WalkDir(m.path(dir), func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(m.path(dir), path)
		if err != nil || d.IsDir() {
			got[rel] = "<dir>"
			return err
		}
		b, err := os.ReadFile(path)
		got[rel] = string(b)
		return err
	})
	if err != nil {
		m.t.Fatal(err)
	}
	return got
}

// serverInstall is what the release binary asset of tag logs when
// install.sh runs its koko server install as root with args, given
// KOKO_VERSION version.
func serverInstall(tag, asset, version, args string) string {
	return "binary=" + asset + " " + tag + "\npath=<tmp>/" + asset + "\nuid=0\nKOKO_VERSION=" + version +
		"\nargs=" + strings.TrimSpace("server install "+args) + "\nstdin=\n"
}

// writesTheCLI is what the writing fakes log when install.sh writes asset
// beside koko in the machine's CLI directory, as the user who runs it, and
// renames it into place.
func (m *machine) writesTheCLI(asset string) string {
	uid, dir := "1000", m.cliDir()
	if m.Root {
		uid = "0"
	}
	return "mkdir -p -m 0755 " + dir + " (uid " + uid + ")\n" +
		"install -m 0755 <tmp>/" + asset + " " + dir + "/.koko.new (uid " + uid + ")\n" +
		"mv -f " + dir + "/.koko.new " + dir + "/koko (uid " + uid + ")\n"
}

// installed checks that the machine's CLI is the release binary asset of
// tag, executable by everyone, in a directory made 0755, with nothing
// beside it.
func (m *machine) installed(tag, asset string) {
	t := m.t
	t.Helper()
	dir := m.onDisk(m.cliDir())
	got, err := os.ReadFile(filepath.Join(dir, "koko"))
	if err != nil {
		t.Fatalf("no CLI was installed: %v", err)
	}
	if !bytes.Equal(got, m.binary(tag, asset)) {
		t.Errorf("%s/koko is not %s of %s:\n%s", m.cliDir(), asset, tag, got)
	}
	for path, mode := range map[string]os.FileMode{dir: os.ModeDir | 0o755, filepath.Join(dir, "koko"): 0o755} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode() != mode {
			t.Errorf("%s has mode %v, want %v", path, info.Mode(), mode)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("%s holds %v, want koko alone", m.cliDir(), entries)
	}
}

// A pasted line with arguments and no KOKO_VERSION installs the latest
// release's binary for the machine, checked against that release's
// SHA256SUMS: it runs its koko server install as root through sudo -n with
// the script's arguments, then writes it as the user's ~/.local/bin/koko,
// and leaves nothing behind. stdout is koko's alone, so --json output stays
// clean. kokod is koko server install's to download, never install.sh's.
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
	wantRan := serverInstall("v0.2.0", "koko-linux-amd64", "", "--owner alice --address koko.example.com") + m.writesTheCLI("koko-linux-amd64")
	if r.Ran != wantRan {
		t.Errorf("ran:\n%s\nwant:\n%s", r.Ran, wantRan)
	}
	wantSudo := "-n true\n-n env <tmp>/koko-linux-amd64 server install --owner alice --address koko.example.com\n"
	if r.Sudo != wantSudo {
		t.Errorf("sudo ran:\n%s\nwant:\n%s", r.Sudo, wantSudo)
	}
	m.installed("v0.2.0", "koko-linux-amd64")
	if r.Stdout != "koko server install ran\n" {
		t.Errorf("stdout = %q, want koko's alone", r.Stdout)
	}
	if !strings.Contains(r.Stderr, "koko v0.2.0 for linux/amd64") || !strings.Contains(r.Stderr, "koko on stderr\n") ||
		!strings.HasSuffix(r.Stderr, "install.sh: installed koko v0.2.0 at "+m.userBin()+"/koko\n") {
		t.Errorf("stderr should name the version, carry koko's, then say where koko went:\n%s", r.Stderr)
	}
	if r.Tmp != nil {
		t.Errorf("install.sh left %v in its temporary directory", r.Tmp)
	}
}

// With no arguments, on a machine with no koko server, install.sh installs
// the CLI alone: the binary for the machine's OS and architecture, written
// beside koko and renamed into place, in ~/.local/bin by the user who runs
// it, made when missing, or in /usr/local/bin by root. It runs no sudo, no
// koko server install, and needs no systemd. A Mac, which has shasum and no
// sha256sum, checks the binary with shasum -a 256. With its directory on
// PATH and no other koko before it, it says nothing more.
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
		"Linux with no sudo": {func(t *testing.T) *machine {
			m := newMachine(t)
			m.Sudo = ""
			return m
		}, "koko-linux-amd64"},
		"Linux with sudo that asks for a password": {func(t *testing.T) *machine {
			m := newMachine(t)
			m.Sudo = "password"
			return m
		}, "koko-linux-amd64"},
		"Linux as root": {func(t *testing.T) *machine {
			m := newMachine(t)
			m.Root, m.Sudo = true, ""
			return m
		}, "koko-linux-amd64"},
		"macOS arm64 with shasum": {newMac, "koko-darwin-arm64"},
		"macOS as root": {func(t *testing.T) *machine {
			m := newMac(t)
			m.Root = true
			return m
		}, "koko-darwin-arm64"},
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
			if want := m.writesTheCLI(tc.asset); r.Ran != want {
				t.Errorf("ran:\n%s\nwant:\n%s", r.Ran, want)
			}
			if r.Sudo != "" {
				t.Errorf("sudo ran:\n%s", r.Sudo)
			}
			m.installed("v0.2.0", tc.asset)
			if r.Stdout != "" || !strings.HasSuffix(r.Stderr, "install.sh: installed koko v0.2.0 at "+m.cliDir()+"/koko\n") {
				t.Errorf("stdout = %q, stderr:\n%s", r.Stdout, r.Stderr)
			}
			if r.Tmp != nil {
				t.Errorf("install.sh left %v in its temporary directory", r.Tmp)
			}
		})
	}
}

// The install line again, without arguments, on a machine whose systemd
// knows koko.service upgrades both: koko server install first, as root from
// the temporary directory with no arguments, and the CLI only after it.
func TestUpgradesAnInstalledKokoServerFirst(t *testing.T) {
	t.Parallel()
	for name, root := range map[string]bool{"through sudo": false, "as root": true} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			m := newMachine(t)
			m.Service, m.Root = true, root
			m.release("v0.2.0")
			m.latest("v0.2.0")

			r := m.run()

			if r.Status != 0 {
				t.Fatalf("status %d, stderr:\n%s", r.Status, r.Stderr)
			}
			wantRan := serverInstall("v0.2.0", "koko-linux-amd64", "", "") + m.writesTheCLI("koko-linux-amd64")
			if r.Ran != wantRan {
				t.Errorf("ran:\n%s\nwant:\n%s", r.Ran, wantRan)
			}
			m.installed("v0.2.0", "koko-linux-amd64")
			if r.Stdout != "koko server install ran\n" {
				t.Errorf("stdout = %q, want koko's alone", r.Stdout)
			}
		})
	}
}

// Without arguments, on a koko server's machine, a user who is neither root
// nor able to use sudo without a password still gets the CLI. The koko
// server is left as it is, and install.sh says so, naming koko server
// upgrade and the line as root.
func TestLeavesTheKokoServerToRootAndInstallsTheCLI(t *testing.T) {
	t.Parallel()
	for name, sudo := range map[string]string{"no sudo": "", "sudo asks for a password": "password"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			m := newMachine(t)
			m.Service, m.Sudo = true, sudo
			m.release("v0.2.0")
			m.latest("v0.2.0")

			r := m.run()

			if r.Status != 0 {
				t.Fatalf("status %d, stderr:\n%s", r.Status, r.Stderr)
			}
			if want := m.writesTheCLI("koko-linux-amd64"); r.Ran != want {
				t.Errorf("ran:\n%s\nwant the CLI alone:\n%s", r.Ran, want)
			}
			m.installed("v0.2.0", "koko-linux-amd64")
			for _, want := range []string{"koko server", "left as it is", "koko server upgrade", "| sudo sh"} {
				if !strings.Contains(r.Stderr, want) {
					t.Errorf("stderr does not say %q:\n%s", want, r.Stderr)
				}
			}
			if r.Stdout != "" {
				t.Errorf("stdout = %q", r.Stdout)
			}
		})
	}
}

// A release from before the split, v0.1.0 or v0.2.0, has no kokod, and its
// koko-<os>-<arch> is the whole koko, whose server install installs the
// koko server by itself. install.sh installs it by the same rules, into the
// user's own directory too.
func TestInstallsAReleaseFromBeforeTheSplit(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		args    []string
		service bool
		want    string
	}{
		"with arguments":             {args: []string{"--owner", "alice"}, want: serverInstall("v0.1.0", "koko-linux-amd64", "v0.1.0", "--owner alice")},
		"onto its koko server":       {service: true, want: serverInstall("v0.1.0", "koko-linux-amd64", "v0.1.0", "")},
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
			if want := tc.want + m.writesTheCLI("koko-linux-amd64"); r.Ran != want {
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

// A user other than root installs the CLI in their home, so a HOME that is
// unset or not a directory they own is refused before any download.
func TestRefusesAUserWithNoHome(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		home   []string
		noHome bool
		// notAsRoot skips the case when the tests run as root, who owns /.
		notAsRoot bool
	}{
		"HOME unset":              {noHome: true},
		"HOME empty":              {home: []string{"HOME="}},
		"HOME missing":            {home: []string{"HOME=/nonexistent/alice"}},
		"HOME a file":             {home: []string{"HOME=/dev/null"}},
		"HOME another user's (/)": {home: []string{"HOME=/"}, notAsRoot: true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if tc.notAsRoot && os.Getuid() == 0 {
				t.Skip("the tests run as root, who owns /")
			}
			m := newMachine(t)
			m.NoHome, m.Env = tc.noHome, tc.home
			m.release("v0.2.0")
			m.latest("v0.2.0")

			r := m.run()

			refused(t, r, 1, "HOME", "nothing was written")
			if r.Sudo != "" {
				t.Errorf("sudo ran:\n%s", r.Sudo)
			}
		})
	}
}

// When the CLI's directory is not on PATH, install.sh gives the line that
// puts it there for the user's shell, read from SHELL, and the full path to
// run koko by until then. It edits no file outside the CLI's directory: the
// shell's files are the user's.
func TestSaysHowToPutTheCLIOnPATH(t *testing.T) {
	t.Parallel()
	const userLine = `export PATH="$HOME/.local/bin:$PATH"`
	for name, tc := range map[string]struct {
		machine func(*testing.T) *machine
		shell   string
		want    string
	}{
		"zsh":           {newMachine, "/bin/zsh", "run:\n  echo '" + userLine + "' >> ~/.zprofile\n"},
		"bash on Linux": {newMachine, "/bin/bash", "run:\n  echo '" + userLine + "' >> ~/.bashrc\n"},
		"bash on macOS": {newMac, "/bin/bash", "run:\n  echo '" + userLine + "' >> ~/.bash_profile\n"},
		"zsh on macOS":  {newMac, "/bin/zsh", "run:\n  echo '" + userLine + "' >> ~/.zprofile\n"},
		"fish":          {newMachine, "/usr/bin/fish", "run:\n  fish_add_path ~/.local/bin\n"},
		"another shell": {newMachine, "/bin/ksh", "add this line to the shell's profile:\n  " + userLine + "\n"},
		"root": {func(t *testing.T) *machine {
			m := newMachine(t)
			m.Root, m.Sudo = true, ""
			return m
		}, "/bin/zsh", "run:\n  echo 'export PATH=\"/usr/local/bin:$PATH\"' >> ~/.zprofile\n"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			m := tc.machine(t)
			m.Path, m.Shell = nil, tc.shell
			for path, body := range map[string]string{
				".zprofile": "# zsh\n", ".bashrc": "# bash\n", ".bash_profile": "# bash on macOS\n",
				".profile": "# sh\n", ".config/fish/config.fish": "# fish\n",
			} {
				m.put(m.path("home", path), body)
			}
			homeBefore, rootBefore := m.files("home"), m.files("root")
			m.release("v0.2.0")
			m.latest("v0.2.0")

			r := m.run()

			if r.Status != 0 {
				t.Fatalf("status %d, stderr:\n%s", r.Status, r.Stderr)
			}
			asset := "koko-linux-amd64"
			if m.OS == "Darwin" {
				asset = "koko-darwin-arm64"
			}
			m.installed("v0.2.0", asset)
			wantStderr := "install.sh: installed koko v0.2.0 at " + m.cliDir() + "/koko\n" +
				"install.sh: " + m.cliDir() + " is not on PATH; to put it there for new shells, " + tc.want +
				"install.sh: until a new shell starts, run koko as " + m.cliDir() + "/koko\n"
			if !strings.HasSuffix(r.Stderr, wantStderr) {
				t.Errorf("stderr:\n%s\nwant it to end with:\n%s", r.Stderr, wantStderr)
			}
			homeAfter, rootAfter := m.files("home"), m.files("root")
			if m.Root {
				maps.DeleteFunc(rootAfter, func(path, _ string) bool { return strings.HasPrefix(path, "usr") })
			} else {
				maps.DeleteFunc(homeAfter, func(path, _ string) bool { return strings.HasPrefix(path, ".local") })
			}
			if !maps.Equal(homeAfter, homeBefore) || !maps.Equal(rootAfter, rootBefore) {
				t.Errorf("install.sh changed files outside the CLI's directory:\nhome %v, was %v\nroot %v, was %v", homeAfter, homeBefore, rootAfter, rootBefore)
			}
		})
	}
}

// When a new shell would run another koko than the one just installed,
// install.sh names it. A root-owned /usr/local/bin/koko, which an earlier
// install line put there as root, it names with the line that removes it,
// and leaves where it is: other users of the machine may run it. On a koko
// server's machine, that file is the koko server's CLI, so install.sh says
// to keep it, and gives no line that removes it.
func TestNamesAnotherKokoFirstOnPATH(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		service bool
		advice  string
	}{
		"root's /usr/local/bin/koko": {advice: "install.sh: an earlier install line put /usr/local/bin/koko there as root; once no other user of this machine runs it, and a koko command has run with the new koko, remove it with:\n" +
			"  sudo rm /usr/local/bin/koko\n"},
		"root's /usr/local/bin/koko on a koko server's machine": {service: true, advice: "install.sh: /usr/local/bin/koko is the CLI of this machine's koko server: keep it, and upgrade both with " +
			"curl -fsSL " + repo + "/releases/latest/download/install.sh | sudo sh\n"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			m := newMachine(t)
			m.Service = tc.service
			m.Path = []string{"/usr/local/bin", m.userBin()}
			m.put("/usr/local/bin/koko", "#!/bin/sh\n# koko v0.3.0\n")
			m.release("v0.4.0")
			m.latest("v0.4.0")

			r := m.run()

			if r.Status != 0 {
				t.Fatalf("status %d, stderr:\n%s", r.Status, r.Stderr)
			}
			m.installed("v0.4.0", "koko-linux-amd64")
			want := "install.sh: installed koko v0.4.0 at " + m.userBin() + "/koko\n" +
				"install.sh: koko on PATH is /usr/local/bin/koko, not the new " + m.userBin() + "/koko\n" +
				tc.advice
			if !strings.HasSuffix(r.Stderr, want) {
				t.Errorf("stderr:\n%s\nwant it to end with:\n%s", r.Stderr, want)
			}
			if got, err := os.ReadFile(m.path("root", "usr", "local", "bin", "koko")); err != nil || string(got) != "#!/bin/sh\n# koko v0.3.0\n" {
				t.Errorf("/usr/local/bin/koko was changed: %q, %v", got, err)
			}
		})
	}
	// The new koko found through a PATH entry with a trailing /, which
	// dash's command -v gives as dir//koko, is no other koko.
	t.Run("the new koko through dir/", func(t *testing.T) {
		t.Parallel()
		m := newMachine(t)
		m.Path = []string{m.userBin() + "/", "/usr/local/bin"}
		m.put("/usr/local/bin/koko", "#!/bin/sh\n")
		m.release("v0.4.0")
		m.latest("v0.4.0")

		r := m.run()

		if want := "install.sh: installed koko v0.4.0 at " + m.userBin() + "/koko\n"; r.Status != 0 || !strings.HasSuffix(r.Stderr, want) {
			t.Errorf("status %d, stderr:\n%s\nwant it to end with:\n%s", r.Status, r.Stderr, want)
		}
	})
	// Any other koko is named alone: install.sh does not know who put it
	// there or who else runs it.
	for name, tc := range map[string]struct {
		// dir is where the other koko is, in the machine's directory when
		// relative.
		dir       string
		userOwned bool
	}{
		"in another directory":           {dir: "opt/bin"},
		"the user's /usr/local/bin/koko": {dir: "/usr/local/bin", userOwned: true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			m := newMachine(t)
			dir := tc.dir
			if !filepath.IsAbs(dir) {
				dir = m.path(dir)
			}
			m.Path = []string{dir, m.userBin()}
			m.put(dir+"/koko", "#!/bin/sh\n")
			if tc.userOwned {
				m.UserOwned = []string{dir + "/koko"}
			}
			m.release("v0.4.0")
			m.latest("v0.4.0")

			r := m.run()

			if r.Status != 0 {
				t.Fatalf("status %d, stderr:\n%s", r.Status, r.Stderr)
			}
			want := "install.sh: installed koko v0.4.0 at " + m.userBin() + "/koko\n" +
				"install.sh: koko on PATH is " + dir + "/koko, not the new " + m.userBin() + "/koko\n"
			if !strings.HasSuffix(r.Stderr, want) {
				t.Errorf("stderr:\n%s\nwant it to end with:\n%s", r.Stderr, want)
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

// sudo drops KOKO_VERSION from the environment, so install.sh passes it on
// to koko server install by name, as the tag it installs, which reads it as
// a going back asked for; as root too.
func TestPassesKokoVersionOnToKokoServerInstall(t *testing.T) {
	t.Parallel()
	for name, root := range map[string]bool{"through sudo": false, "as root": true} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			m := newMachine(t)
			m.Root = root
			m.release("v0.1.0")
			m.Env = []string{"KOKO_VERSION=0.1.0"}

			r := m.run("--owner", "alice")

			if want := serverInstall("v0.1.0", "koko-linux-amd64", "v0.1.0", "--owner alice") + m.writesTheCLI("koko-linux-amd64"); r.Status != 0 || r.Ran != want {
				t.Errorf("status %d, ran:\n%s\nwant:\n%s\nstderr:\n%s", r.Status, r.Ran, want, r.Stderr)
			}
			if !root && !strings.Contains(r.Sudo, "-n env KOKO_VERSION=v0.1.0 <tmp>/koko-linux-amd64 server install --owner alice\n") {
				t.Errorf("sudo ran:\n%s", r.Sudo)
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

// Run as root, install.sh runs koko server install and writes the CLI in
// /usr/local/bin itself, without sudo.
func TestRunsAsRootWithoutSudo(t *testing.T) {
	t.Parallel()
	m := newMachine(t)
	m.Root, m.Sudo = true, ""
	m.release("v0.2.0")
	m.latest("v0.2.0")

	r := m.run("--owner", "alice")

	if want := serverInstall("v0.2.0", "koko-linux-amd64", "", "--owner alice") + m.writesTheCLI("koko-linux-amd64"); r.Status != 0 || r.Ran != want {
		t.Errorf("status %d, ran:\n%s\nwant:\n%s\nstderr:\n%s", r.Status, r.Ran, want, r.Stderr)
	}
	m.installed("v0.2.0", "koko-linux-amd64")
}

// With arguments, install.sh runs koko server install as root, and sudo
// that would ask for a password is never left to ask: install.sh stops
// before any download and says how to run it as root with the same
// arguments.
func TestRefusesTheKokoServerToAUserWhoCannotBeRoot(t *testing.T) {
	t.Parallel()
	for name, sudo := range map[string]string{"sudo asks for a password": "password", "no sudo": ""} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			m := newMachine(t)
			m.Sudo = sudo
			m.release("v0.2.0")
			m.latest("v0.2.0")

			r := m.run("--owner", "alice")

			refused(t, r, 1, "root", "| sudo sh -s -- <the same arguments>", "nothing was written")
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
			if want := serverInstall("v0.2.0", "koko-linux-amd64", "", tc.want); r.Ran != want {
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
