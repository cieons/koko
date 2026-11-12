package release

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode"

	"golang.org/x/sys/windows"
)

// winAsset is the release asset install.ps1 installs.
const winAsset = "koko-windows-amd64.exe"

// runsAsKoko makes this test binary, copied to koko.exe and started, a koko
// that runs until its stdin ends: the running koko install.ps1 replaces.
const runsAsKoko = "KOKO_TEST_RUNS_AS_KOKO"

func TestMain(m *testing.M) {
	if os.Getenv(runsAsKoko) != "" {
		_, _ = io.Copy(io.Discard, os.Stdin)
		return
	}
	m.Run()
}

// ps1Fakes is the fake machine's PowerShell half: functions defined ahead of
// install.ps1, which PowerShell finds before the cmdlets of the same names.
// Invoke-WebRequest serves the fake GitHub from the machine's web directory
// and logs every URL, refusing one that is not HTTPS; a file <url>.location
// redirects to the URL it holds. Get-Item and Set-ItemProperty are the
// registry, for the user's PATH alone, kept in registry.txt as its type and
// its value, and refuse any other path, the machine's PATH among them. The
// machine's architecture is set here, once PowerShell runs, since
// PowerShell itself may read it as it starts.
const ps1Fakes = `
$env:PROCESSOR_ARCHITECTURE = $env:KOKO_TEST_ARCH
$env:PROCESSOR_ARCHITEW6432 = $env:KOKO_TEST_ARCH6432

function Note([string]$line) {
    [IO.File]::AppendAllText((Join-Path $env:KOKO_TEST_DIR 'log.txt'), $line + [char]10)
}

function Invoke-WebRequest {
    param([string]$Uri, [string]$OutFile, [string]$Method, [switch]$UseBasicParsing)
    Note "web $Uri"
    if (-not $UseBasicParsing) {
        throw 'fake Invoke-WebRequest: without -UseBasicParsing Windows PowerShell 5.1 needs Internet Explorer'
    }
    $at = $Uri
    foreach ($hop in 1..5) {
        if (-not $at.StartsWith('https://')) {
            throw "fake Invoke-WebRequest: $at is not HTTPS"
        }
        $file = Join-Path $env:KOKO_TEST_DIR ('web\' + $at.Substring(8).Replace('/', '\'))
        if (-not (Test-Path -LiteralPath "$file.location")) {
            break
        }
        $at = [IO.File]::ReadAllText("$file.location")
    }
    if (-not (Test-Path -LiteralPath $file -PathType Leaf)) {
        throw "fake Invoke-WebRequest: 404 for $at"
    }
    if ($OutFile) {
        Copy-Item -LiteralPath $file -Destination $OutFile
        return
    }
    # Where each PowerShell keeps the address its request ended at.
    if ($PSVersionTable.PSVersion.Major -lt 6) {
        $base = [pscustomobject]@{ ResponseUri = [uri]$at }
    } else {
        $base = [pscustomobject]@{ RequestMessage = [pscustomobject]@{ RequestUri = [uri]$at } }
    }
    [pscustomobject]@{ BaseResponse = $base }
}

function Get-Item {
    param([string]$LiteralPath)
    if ($LiteralPath -cne 'HKCU:\Environment') {
        throw "fake registry: install.ps1 opened $LiteralPath"
    }
    Note "registry read $LiteralPath"
    $key = New-Object psobject
    Add-Member -InputObject $key -MemberType ScriptMethod -Name GetValue -Value {
        param($name, $default, $options)
        if ($name -ne 'Path' -or "$options" -ne 'DoNotExpandEnvironmentNames') {
            throw "fake registry: GetValue($name, $options)"
        }
        $file = Join-Path $env:KOKO_TEST_DIR 'registry.txt'
        if (-not (Test-Path -LiteralPath $file)) {
            return $default
        }
        return [IO.File]::ReadAllLines($file)[1]
    }
    Add-Member -InputObject $key -MemberType ScriptMethod -Name GetValueKind -Value {
        param($name)
        $file = Join-Path $env:KOKO_TEST_DIR 'registry.txt'
        if ($name -ne 'Path' -or -not (Test-Path -LiteralPath $file)) {
            throw "fake registry: GetValueKind($name) of a value that is not there"
        }
        return [Microsoft.Win32.RegistryValueKind]([IO.File]::ReadAllLines($file)[0])
    }
    Add-Member -InputObject $key -MemberType ScriptMethod -Name Close -Value { }
    $key
}

function Set-ItemProperty {
    param([string]$LiteralPath, [string]$Name, [string]$Value, $Type)
    if ($LiteralPath -cne 'HKCU:\Environment' -or $Name -cne 'Path') {
        throw "fake registry: install.ps1 wrote $Name of $LiteralPath"
    }
    Note "registry write $LiteralPath $Name"
    [IO.File]::WriteAllText((Join-Path $env:KOKO_TEST_DIR 'registry.txt'), "$Type" + [char]10 + $Value + [char]10)
}
`

// ps1Line runs install.ps1 as the install line does, its text given to
// Invoke-Expression, after the fakes, and then writes down the PATH of the
// PowerShell session it ran in.
const ps1Line = `iex ([IO.File]::ReadAllText($env:KOKO_TEST_FAKES)); ` +
	`try { [IO.File]::ReadAllText($env:KOKO_TEST_SCRIPT) | iex } ` +
	`finally { [IO.File]::WriteAllText((Join-Path $env:KOKO_TEST_DIR 'session-path.txt'), [string]$env:Path) }`

// winMachine is a fake Windows machine for install.ps1, run by one of the
// two PowerShells: a profile directory of its own, a temporary directory, a
// fake GitHub and a fake registry (ps1Fakes).
type winMachine struct {
	t *testing.T
	// shell is the PowerShell that runs install.ps1.
	shell string
	// Arch is PROCESSOR_ARCHITECTURE, and Arch6432 PROCESSOR_ARCHITEW6432,
	// which only a 32-bit PowerShell on 64-bit Windows has.
	Arch, Arch6432 string
	// Path is the session's PATH, before the system's own directory.
	Path []string
	// Env is added to install.ps1's environment last, such as KOKO_VERSION.
	Env []string

	dir string
}

// onEachPowerShell runs test on a new machine under Windows PowerShell 5.1
// and under PowerShell 7, each of which must be on the machine running the
// tests. The machine is x64, its user's PATH in the registry and the
// session's PATH both have the user's .local\bin, and GitHub has no koko
// release yet.
func onEachPowerShell(t *testing.T, test func(t *testing.T, m *winMachine)) {
	t.Helper()
	t.Parallel()
	for name, exe := range map[string]string{"Windows PowerShell 5.1": "powershell.exe", "PowerShell 7": "pwsh.exe"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			shell, err := exec.LookPath(exe)
			if err != nil {
				t.Fatalf("install.ps1's tests need %s on the machine running them: %v", exe, err)
			}
			m := &winMachine{t: t, shell: shell, Arch: "AMD64", dir: t.TempDir()}
			for _, d := range []string{"web", "tmp", "home", "appdata"} {
				if err := os.Mkdir(m.path(d), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			m.Path = []string{m.bin()}
			m.userPath("ExpandString", `%USERPROFILE%\AppData\Local\Microsoft\WindowsApps;`+m.bin())
			test(t, m)
		})
	}
}

func (m *winMachine) path(elem ...string) string {
	return filepath.Join(append([]string{m.dir}, elem...)...)
}

// bin is the user's .local\bin, and target the koko.exe in it.
func (m *winMachine) bin() string    { return m.path("home", ".local", "bin") }
func (m *winMachine) target() string { return filepath.Join(m.bin(), "koko.exe") }

func (m *winMachine) write(path string, body []byte) {
	m.t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		m.t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		m.t.Fatal(err)
	}
}

// serve makes url answer with body on the fake GitHub.
func (m *winMachine) serve(url string, body []byte) {
	m.t.Helper()
	m.write(m.path("web", filepath.FromSlash(strings.TrimPrefix(url, "https://"))), body)
}

// koko is the Windows CLI of release tag. install.ps1 never runs it.
func koko(tag string) []byte {
	return []byte("koko for Windows, " + tag)
}

// release publishes tag on the fake GitHub with a Windows CLI: its page,
// koko-windows-amd64.exe and a SHA256SUMS that lists it among the others.
func (m *winMachine) release(tag string) {
	m.t.Helper()
	sum := sha256.Sum256(koko(tag))
	m.publish(tag, "0000  koko-linux-amd64\n"+hex.EncodeToString(sum[:])+"  "+winAsset+"\n0000  install.ps1\n")
	m.serve(repo+"/releases/download/"+tag+"/"+winAsset, koko(tag))
}

// publish publishes tag with its page and the SHA256SUMS sums, and no
// asset.
func (m *winMachine) publish(tag, sums string) {
	m.t.Helper()
	m.serve(repo+"/releases/tag/"+tag, []byte("<html>"))
	m.serve(repo+"/releases/download/"+tag+"/SHA256SUMS", []byte(sums))
}

// latest makes tag the latest release, whose page GitHub's /releases/latest
// redirects to.
func (m *winMachine) latest(tag string) {
	m.t.Helper()
	m.serve(repo+"/releases/latest.location", []byte(repo+"/releases/tag/"+tag))
}

// userPath sets the user's PATH in the registry, with its type; noUserPath
// takes the value away.
func (m *winMachine) userPath(kind, value string) {
	m.t.Helper()
	m.write(m.path("registry.txt"), []byte(kind+"\n"+value+"\n"))
}

func (m *winMachine) noUserPath() {
	m.t.Helper()
	if err := os.Remove(m.path("registry.txt")); err != nil {
		m.t.Fatal(err)
	}
}

// registry is the user's PATH in the registry: its type, then its value,
// or "" when there is none.
func (m *winMachine) registry() string {
	return m.read(m.path("registry.txt"))
}

// read returns the file's text, "" when it is not there.
func (m *winMachine) read(path string) string {
	m.t.Helper()
	b, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		m.t.Fatal(err)
	}
	return string(b)
}

// ps1Result is what a run of install.ps1 left.
type ps1Result struct {
	Status int
	// Stderr has each line's end as \n.
	Stderr string
	// Log is what the fakes logged, in order: each URL asked for, and each
	// read and write of the registry.
	Log string
	// SessionPath is the PATH of the PowerShell session afterwards.
	SessionPath string
}

// run runs install.ps1 as `irm … | iex` does, in a PowerShell whose stdin
// stays open, so that anything that reads it, as a prompt would, waits and
// the run times out. Whatever the outcome, install.ps1 writes nothing to
// the output stream and leaves nothing in the temporary directory.
func (m *winMachine) run() ps1Result {
	t := m.t
	t.Helper()
	script, err := filepath.Abs("install.ps1")
	if err != nil {
		t.Fatal(err)
	}
	m.write(m.path("fakes.ps1"), []byte(ps1Fakes))
	if err := os.Remove(m.path("log.txt")); err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, m.shell, "-NoProfile", "-Command", ps1Line)
	// The real environment, which PowerShell needs to start, but for the
	// user's own places and the release asked for. PSModulePath is left to
	// each PowerShell: one inherited from the other names modules it
	// cannot load.
	cmd.Env = slices.DeleteFunc(os.Environ(), func(kv string) bool {
		name, _, _ := strings.Cut(kv, "=")
		return strings.EqualFold(name, "PSModulePath") || strings.EqualFold(name, "KOKO_VERSION") ||
			strings.EqualFold(name, "PROCESSOR_ARCHITEW6432")
	})
	cmd.Env = append(cmd.Env,
		"USERPROFILE="+m.path("home"),
		"TEMP="+m.path("tmp"), "TMP="+m.path("tmp"),
		"LOCALAPPDATA="+m.path("appdata"), "APPDATA="+m.path("appdata"),
		"Path="+strings.Join(append(slices.Clone(m.Path), filepath.Join(os.Getenv("SystemRoot"), "System32")), ";"),
		"POWERSHELL_TELEMETRY_OPTOUT=1", "POWERSHELL_UPDATECHECK=Off",
		"KOKO_TEST_DIR="+m.dir, "KOKO_TEST_FAKES="+m.path("fakes.ps1"), "KOKO_TEST_SCRIPT="+script,
		"KOKO_TEST_ARCH="+m.Arch, "KOKO_TEST_ARCH6432="+m.Arch6432,
	)
	cmd.Env = append(cmd.Env, m.Env...)
	cmd.WaitDelay = time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if _, err := cmd.StdinPipe(); err != nil {
		t.Fatal(err)
	}
	err = cmd.Run()
	if ctx.Err() != nil {
		t.Fatalf("install.ps1 did not end within 2 minutes: it waits on stdin or a prompt\nstderr: %s", stderr.String())
	}
	r := ps1Result{
		Stderr:      strings.ReplaceAll(stderr.String(), "\r\n", "\n"),
		Log:         m.read(m.path("log.txt")),
		SessionPath: m.read(m.path("session-path.txt")),
	}
	if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
		r.Status = exitErr.ExitCode()
	} else if err != nil {
		t.Fatalf("run install.ps1: %v", err)
	}
	if stdout.Len() != 0 {
		t.Errorf("install.ps1 wrote to the output stream:\n%s", stdout.String())
	}
	if left := m.files("tmp"); len(left) != 0 {
		t.Errorf("install.ps1 left %v in the temporary directory", left)
	}
	return r
}

// files lists every file and directory under dir on the machine, by its
// path from there, but for the profile's AppData: each PowerShell keeps
// files of its own there as it starts, wherever LOCALAPPDATA and APPDATA
// point.
func (m *winMachine) files(dir string) []string {
	m.t.Helper()
	var got []string
	err := filepath.WalkDir(m.path(dir), func(path string, _ os.DirEntry, err error) error {
		if err != nil || path == m.path(dir) {
			return err
		}
		if path == m.path("home", "AppData") {
			return filepath.SkipDir
		}
		rel, err := filepath.Rel(m.path(dir), path)
		got = append(got, rel)
		return err
	})
	if err != nil {
		m.t.Fatal(err)
	}
	return got
}

// asks is what the fakes log when install.ps1 downloads the Windows CLI of
// tag, checks it and then reads the user's PATH.
func asks(tag string) string {
	return "web " + repo + "/releases/download/" + tag + "/SHA256SUMS\n" +
		"web " + repo + "/releases/download/" + tag + "/" + winAsset + "\n" +
		`registry read HKCU:\Environment` + "\n"
}

// asksLatest is what they log when it finds the latest release first.
const asksLatest = "web " + repo + "/releases/latest\n"

// installs checks that the run installed the Windows CLI of tag and said
// so, then said the lines of then and nothing else, and that the user's
// profile holds koko.exe in .local\bin and nothing else.
func (m *winMachine) installs(r ps1Result, tag string, then ...string) {
	t := m.t
	t.Helper()
	if r.Status != 0 {
		t.Fatalf("status %d, stderr:\n%s", r.Status, r.Stderr)
	}
	want := "install.ps1: downloading koko " + tag + " for windows/amd64\n" +
		"install.ps1: installed koko " + tag + " at " + m.target() + "\n"
	for _, line := range then {
		want += "install.ps1: " + line + "\n"
	}
	if r.Stderr != want {
		t.Errorf("stderr:\n%s\nwant:\n%s", r.Stderr, want)
	}
	if got := m.read(m.target()); got != string(koko(tag)) {
		t.Errorf("%s holds %q, want the Windows CLI of %s", m.target(), got, tag)
	}
	if got, want := m.files("home"), []string{".local", `.local\bin`, `.local\bin\koko.exe`}; !slices.Equal(got, want) {
		t.Errorf("the profile holds %v, want %v", got, want)
	}
}

// refuses checks that the run failed saying want, a terminating error, and
// wrote nothing in the user's profile nor to the registry. PowerShell wraps
// an error's text to the width of its window, so the text is compared
// without its white space.
func (m *winMachine) refuses(r ps1Result, want string) {
	t := m.t
	t.Helper()
	if r.Status == 0 {
		t.Errorf("install.ps1 succeeded, stderr:\n%s", r.Stderr)
	}
	if !strings.Contains(unspaced(r.Stderr), unspaced("install.ps1: "+want)) {
		t.Errorf("stderr:\n%s\nwant it to say:\ninstall.ps1: %s", r.Stderr, want)
	}
	if got := m.files("home"); len(got) != 0 {
		t.Errorf("the profile holds %v, want nothing written", got)
	}
	if strings.Contains(r.Log, "registry") {
		t.Errorf("install.ps1 went to the registry:\n%s", r.Log)
	}
}

func unspaced(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, s)
}

// The install line installs the latest release's Windows CLI, checked
// against that release's SHA256SUMS, as koko.exe in a .local\bin it makes
// in the user's profile, reading the latest release once. The directory is
// in the user's PATH already, so the registry is read and left alone.
func TestPS1InstallsTheLatestRelease(t *testing.T) {
	onEachPowerShell(t, func(t *testing.T, m *winMachine) {
		m.release("v0.6.0")
		m.release("v0.7.0")
		m.latest("v0.7.0")
		before := m.registry()

		r := m.run()

		m.installs(r, "v0.7.0")
		if want := asksLatest + asks("v0.7.0"); r.Log != want {
			t.Errorf("install.ps1 asked:\n%s\nwant:\n%s", r.Log, want)
		}
		if got := m.registry(); got != before {
			t.Errorf("the user's PATH became %q, want it left as %q", got, before)
		}
	})
}

// KOKO_VERSION names the release, with or without its v, and the latest
// release is never asked for.
func TestPS1InstallsTheReleaseKOKOVERSIONNames(t *testing.T) {
	for _, version := range []string{"v0.6.0", "0.6.0"} {
		t.Run(version, func(t *testing.T) {
			onEachPowerShell(t, func(t *testing.T, m *winMachine) {
				m.release("v0.6.0")
				m.release("v0.7.0")
				m.latest("v0.7.0")
				m.Env = []string{"KOKO_VERSION=" + version}

				r := m.run()

				m.installs(r, "v0.6.0")
				if want := asks("v0.6.0"); r.Log != want {
					t.Errorf("install.ps1 asked:\n%s\nwant:\n%s", r.Log, want)
				}
			})
		})
	}
}

// A KOKO_VERSION that is no version is refused before anything is asked of
// GitHub.
func TestPS1RefusesAKOKOVERSIONThatIsNoVersion(t *testing.T) {
	for _, version := range []string{"latest", "v0.6.0/../v0.7.0", "v", "0.6.0 "} {
		t.Run(version, func(t *testing.T) {
			onEachPowerShell(t, func(t *testing.T, m *winMachine) {
				m.release("v0.6.0")
				m.latest("v0.6.0")
				m.Env = []string{"KOKO_VERSION=" + version}

				r := m.run()

				m.refuses(r, "KOKO_VERSION="+version+" is not a koko version, such as v0.6.0")
				if r.Log != "" {
					t.Errorf("install.ps1 asked:\n%s\nwant nothing", r.Log)
				}
			})
		})
	}
}

// A Windows CLI whose SHA-256 is not the one SHA256SUMS lists is not
// installed.
func TestPS1RefusesAChecksumThatDoesNotMatch(t *testing.T) {
	onEachPowerShell(t, func(t *testing.T, m *winMachine) {
		m.release("v0.6.0")
		m.latest("v0.6.0")
		m.serve(repo+"/releases/download/v0.6.0/"+winAsset, []byte("another file"))

		r := m.run()

		m.refuses(r, winAsset+" does not match the SHA256SUMS of koko v0.6.0: the download is damaged or was swapped, so it was not installed")
	})
}

// A release since the first with a Windows CLI whose SHA256SUMS does not
// list it is refused as such, with the file never asked for.
func TestPS1RefusesAFileSHA256SUMSDoesNotList(t *testing.T) {
	onEachPowerShell(t, func(t *testing.T, m *winMachine) {
		m.publish("v0.7.0", "0000  koko-linux-amd64\n0000  not-"+winAsset+"\n0000  "+winAsset+".sig\n")
		m.latest("v0.7.0")

		r := m.run()

		m.refuses(r, "the SHA256SUMS of koko v0.7.0 lists no "+winAsset+", so it was not installed")
		if want := asksLatest + "web " + repo + "/releases/download/v0.7.0/SHA256SUMS\n"; r.Log != want {
			t.Errorf("install.ps1 asked:\n%s\nwant:\n%s", r.Log, want)
		}
	})
}

// A release from before the Windows CLI is refused, naming the first
// release that has one.
func TestPS1RefusesAReleaseFromBeforeTheWindowsCLI(t *testing.T) {
	for _, tag := range []string{"v0.5.0", "v0.4.3", "v0.6.0-rc.1"} {
		t.Run(tag, func(t *testing.T) {
			onEachPowerShell(t, func(t *testing.T, m *winMachine) {
				m.publish(tag, "0000  koko-linux-amd64\n0000  install.sh\n")
				m.Env = []string{"KOKO_VERSION=" + tag}

				r := m.run()

				want := "koko " + tag + " has no Windows CLI; the first release with one is v0.6.0"
				if tag == "v0.6.0-rc.1" {
					// Not before it: a pre-release of the first.
					want = "the SHA256SUMS of koko " + tag + " lists no " + winAsset + ", so it was not installed"
				}
				m.refuses(r, want)
			})
		})
	}
}

// A download that fails, of SHA256SUMS or of the Windows CLI, is named.
func TestPS1RefusesAFailedDownload(t *testing.T) {
	for _, file := range []string{"SHA256SUMS", winAsset} {
		t.Run(file, func(t *testing.T) {
			onEachPowerShell(t, func(t *testing.T, m *winMachine) {
				m.release("v0.6.0")
				m.latest("v0.6.0")
				url := repo + "/releases/download/v0.6.0/" + file
				if err := os.Remove(m.path("web", filepath.FromSlash(strings.TrimPrefix(url, "https://")))); err != nil {
					t.Fatal(err)
				}

				r := m.run()

				m.refuses(r, "could not download "+url)
			})
		})
	}
}

// With no release at all, or a /releases/latest that leads to no release's
// page, install.ps1 says so.
func TestPS1RefusesWhenThereIsNoRelease(t *testing.T) {
	t.Run("none", func(t *testing.T) {
		onEachPowerShell(t, func(t *testing.T, m *winMachine) {
			r := m.run()

			m.refuses(r, "could not find koko's latest release at "+repo+"/releases/latest")
		})
	})
	t.Run("another page", func(t *testing.T) {
		onEachPowerShell(t, func(t *testing.T, m *winMachine) {
			m.serve("https://github.com/login", []byte("<html>"))
			m.serve(repo+"/releases/latest.location", []byte("https://github.com/login"))

			r := m.run()

			m.refuses(r, "found no koko release at "+repo+"/releases/latest")
		})
	})
}

// When the user's PATH in the registry lacks .local\bin, install.ps1
// appends it, keeps the value's type, adds it to the session's PATH and
// says so. Telling running programs is the real call, which changes
// nothing: had it failed, install.ps1 would have said so.
func TestPS1AddsItsDirectoryToTheUsersPATH(t *testing.T) {
	const apps = `%USERPROFILE%\AppData\Local\Microsoft\WindowsApps`
	for name, c := range map[string]struct{ kind, value, wantKind, wantBefore string }{
		"a value with variables":    {"ExpandString", apps, "ExpandString", apps + ";"},
		"a plain value":             {"String", `C:\Tools;`, "String", `C:\Tools;`},
		"no value":                  {"", "", "ExpandString", ""},
		"an empty value":            {"String", "", "String", ""},
		"a directory of that start": {"ExpandString", `%USERPROFILE%\.local\bin2`, "ExpandString", `%USERPROFILE%\.local\bin2;`},
	} {
		t.Run(name, func(t *testing.T) {
			onEachPowerShell(t, func(t *testing.T, m *winMachine) {
				m.release("v0.6.0")
				m.latest("v0.6.0")
				if c.kind == "" {
					m.noUserPath()
				} else {
					m.userPath(c.kind, c.value)
				}
				m.Path = []string{`C:\Tools`}

				r := m.run()

				m.installs(r, "v0.6.0", "added "+m.bin()+" to this user's PATH; a terminal that is already open must be reopened to find koko")
				if want := asksLatest + asks("v0.6.0") + `registry write HKCU:\Environment Path` + "\n"; r.Log != want {
					t.Errorf("install.ps1 asked:\n%s\nwant:\n%s", r.Log, want)
				}
				if got, want := m.registry(), c.wantKind+"\n"+c.wantBefore+m.bin()+"\n"; got != want {
					t.Errorf("the user's PATH is %q, want %q", got, want)
				}
				if !strings.HasSuffix(r.SessionPath, ";"+m.bin()) {
					t.Errorf("the session's PATH is %q, want it to end with %s", r.SessionPath, m.bin())
				}
			})
		})
	}
}

// A user's PATH that has the directory, however it is spelled, is left
// alone, and nothing is said about it.
func TestPS1LeavesAUsersPATHThatHasItsDirectory(t *testing.T) {
	for name, entry := range map[string]string{
		"with a variable":      `%USERPROFILE%\.local\bin`,
		"with a \\ at its end": `%USERPROFILE%\.local\bin\`,
		"in another case":      `%userprofile%\.LOCAL\Bin`,
		"in quotes":            `"%USERPROFILE%\.local\bin"`,
	} {
		t.Run(name, func(t *testing.T) {
			onEachPowerShell(t, func(t *testing.T, m *winMachine) {
				m.release("v0.6.0")
				m.latest("v0.6.0")
				m.userPath("ExpandString", `C:\Tools;`+entry+`;C:\More`)
				before := m.registry()

				r := m.run()

				m.installs(r, "v0.6.0")
				if got := m.registry(); got != before {
					t.Errorf("the user's PATH became %q, want it left as %q", got, before)
				}
				if strings.Contains(r.Log, "registry write") {
					t.Errorf("install.ps1 wrote the registry:\n%s", r.Log)
				}
			})
		})
	}
}

// A koko.exe that is in place and running is replaced: it is renamed aside,
// where it runs on, hidden, and the new one takes its name. One renamed
// aside earlier that no longer runs, as koko update leaves it, is deleted,
// and so is this one by the next run, once it has stopped.
func TestPS1ReplacesAKokoThatIsRunning(t *testing.T) {
	onEachPowerShell(t, func(t *testing.T, m *winMachine) {
		m.release("v0.6.0")
		m.release("v0.7.0")
		m.latest("v0.6.0")
		self, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		running, err := os.ReadFile(self)
		if err != nil {
			t.Fatal(err)
		}
		m.write(m.target(), running)
		m.write(filepath.Join(m.bin(), ".koko.old-left.exe"), []byte("an older koko"))
		old := exec.CommandContext(t.Context(), m.target())
		old.Env = append(os.Environ(), runsAsKoko+"=1")
		stdin, err := old.StdinPipe()
		if err != nil {
			t.Fatal(err)
		}
		if err := old.Start(); err != nil {
			t.Fatal(err)
		}
		stop := func() {
			_ = stdin.Close()
			_ = old.Wait()
		}
		t.Cleanup(stop)

		r := m.run()

		if r.Status != 0 {
			t.Fatalf("status %d, stderr:\n%s", r.Status, r.Stderr)
		}
		if got := m.read(m.target()); got != string(koko("v0.6.0")) {
			t.Errorf("koko.exe is not the Windows CLI of v0.6.0")
		}
		aside := m.aside()
		if len(aside) != 1 || aside[0] == ".koko.old-left.exe" {
			t.Fatalf(".local\\bin holds %v renamed aside, want the running koko alone", aside)
		}
		file := filepath.Join(m.bin(), aside[0])
		if got := m.read(file); got != string(running) {
			t.Errorf("%s is not the koko that was running", aside[0])
		}
		p, err := windows.UTF16PtrFromString(file)
		if err != nil {
			t.Fatal(err)
		}
		attrs, err := windows.GetFileAttributes(p)
		if err != nil {
			t.Fatal(err)
		}
		if attrs&windows.FILE_ATTRIBUTE_HIDDEN == 0 {
			t.Errorf("%s is not hidden", aside[0])
		}

		stop()
		m.latest("v0.7.0")
		r = m.run()

		m.installs(r, "v0.7.0")
	})
}

// aside lists the koko files renamed aside in the user's .local\bin.
func (m *winMachine) aside() []string {
	m.t.Helper()
	entries, err := os.ReadDir(m.bin())
	if err != nil {
		m.t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".koko.old-") {
			names = append(names, e.Name())
		}
	}
	return names
}

// Another koko that comes first on PATH is named, and left where it is.
func TestPS1NamesAnotherKokoFirstOnPATH(t *testing.T) {
	onEachPowerShell(t, func(t *testing.T, m *winMachine) {
		m.release("v0.6.0")
		m.latest("v0.6.0")
		other := m.path("tools", "koko.exe")
		m.write(other, []byte("another koko"))
		m.Path = []string{m.path("tools"), m.bin()}

		r := m.run()

		m.installs(r, "v0.6.0", "koko on PATH is "+other+", not the new "+m.target())
		if got := m.read(other); got != "another koko" {
			t.Errorf("%s was changed: %q", other, got)
		}
	})
}

// 32-bit Windows is refused before anything is asked of GitHub. Windows on
// Arm64 gets the amd64 file, which it runs under emulation, and so does a
// 32-bit PowerShell on 64-bit Windows.
func TestPS1ChecksTheMachinesArchitecture(t *testing.T) {
	t.Run("32-bit Windows", func(t *testing.T) {
		onEachPowerShell(t, func(t *testing.T, m *winMachine) {
			m.release("v0.6.0")
			m.latest("v0.6.0")
			m.Arch = "x86"

			r := m.run()

			m.refuses(r, "koko runs on 64-bit Windows, on x64 and Arm64, and this machine is x86; nothing was written")
			if r.Log != "" {
				t.Errorf("install.ps1 asked:\n%s\nwant nothing", r.Log)
			}
		})
	})
	for name, arch := range map[string][2]string{
		"Arm64":                        {"ARM64", ""},
		"a 32-bit PowerShell on x64":   {"x86", "AMD64"},
		"a 32-bit PowerShell on Arm64": {"x86", "ARM64"},
		"an x64 PowerShell on Arm64":   {"AMD64", "ARM64"},
	} {
		t.Run(name, func(t *testing.T) {
			onEachPowerShell(t, func(t *testing.T, m *winMachine) {
				m.release("v0.6.0")
				m.latest("v0.6.0")
				m.Arch, m.Arch6432 = arch[0], arch[1]

				r := m.run()

				m.installs(r, "v0.6.0")
				if want := asksLatest + asks("v0.6.0"); r.Log != want {
					t.Errorf("install.ps1 asked:\n%s\nwant:\n%s", r.Log, want)
				}
			})
		})
	}
}

// A USERPROFILE that is not set, or names no directory, is refused before
// anything is asked of GitHub.
func TestPS1RefusesWithoutAProfileDirectory(t *testing.T) {
	t.Run("not set", func(t *testing.T) {
		onEachPowerShell(t, func(t *testing.T, m *winMachine) {
			m.release("v0.6.0")
			m.latest("v0.6.0")
			m.Env = []string{"USERPROFILE="}

			r := m.run()

			m.refuses(r, `install.ps1 installs koko for this user in %USERPROFILE%\.local\bin, and USERPROFILE is not set; nothing was written`)
			if r.Log != "" {
				t.Errorf("install.ps1 asked:\n%s\nwant nothing", r.Log)
			}
		})
	})
	t.Run("a file", func(t *testing.T) {
		onEachPowerShell(t, func(t *testing.T, m *winMachine) {
			m.release("v0.6.0")
			m.latest("v0.6.0")
			file := m.path("a-file")
			m.write(file, nil)
			m.Env = []string{"USERPROFILE=" + file}

			r := m.run()

			m.refuses(r, fmt.Sprintf(`install.ps1 installs koko for this user in %%USERPROFILE%%\.local\bin, and USERPROFILE=%s is not a directory; nothing was written`, file))
			if r.Log != "" {
				t.Errorf("install.ps1 asked:\n%s\nwant nothing", r.Log)
			}
		})
	})
}
