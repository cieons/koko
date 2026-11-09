package cli_test

import (
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/shared/build"
)

// release is the version the download tests state koko on a machine is.
const release = "v9.9.9"

// releaseKokod is the build of a kokod of the release koko is stated to be
// on a machine.
func releaseKokod() build.Info {
	b := harness.KokodBuild()
	b.Version = release
	return b
}

// wantNoTempDir fails the test unless koko left nothing in its TMPDIR on m.
func wantNoTempDir(t *testing.T, m *harness.Machine) {
	t.Helper()
	entries, err := os.ReadDir(m.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("koko left %v in its TMPDIR", entries)
	}
}

// wantDownloadDir fails the test unless, while koko downloads asset on m,
// its TMPDIR holds one directory, 0700, holding asset alone, 0700.
func wantDownloadDir(t *testing.T, m *harness.Machine, asset string) {
	t.Helper()
	dirs, err := os.ReadDir(m.TempDir())
	if err != nil || len(dirs) != 1 {
		t.Errorf("koko downloads into %v in its TMPDIR, want one directory: %v", dirs, err)
		return
	}
	dir := filepath.Join(m.TempDir(), dirs[0].Name())
	files, err := os.ReadDir(dir)
	if err != nil || len(files) != 1 || files[0].Name() != asset {
		t.Errorf("koko downloads into %s, which holds %v, want %s alone: %v", dir, files, asset, err)
		return
	}
	for _, path := range []string{dir, filepath.Join(dir, asset)} {
		switch info, err := os.Stat(path); {
		case err != nil:
			t.Error(err)
		case info.Mode().Perm() != 0o700:
			t.Errorf("koko downloads into %s with the mode %v, want 0700", path, info.Mode().Perm())
		}
	}
}

// wantRequests fails the test unless the fake GitHub was asked for exactly
// the assets names, in order.
func wantRequests(t *testing.T, rel *harness.Releases, names ...string) {
	t.Helper()
	var want []string
	for _, name := range names {
		want = append(want, rel.Path(name))
	}
	if got := rel.Requests(); !slices.Equal(got, want) {
		t.Errorf("koko asked the fake GitHub for %q, want %q", got, want)
	}
}

// koko of a release, on a machine with no kokod, downloads exactly
// SHA256SUMS and the kokod for the machine's platform of its own release
// over HTTPS, installs and initialises the koko server with it, and removes
// the directory it downloaded into. It leaves /usr/local/bin/koko alone.
func TestServerInstallDownloadsTheKokodOfItsRelease(t *testing.T) {
	t.Parallel()
	for _, platform := range []string{"linux/amd64", "linux/arm64"} {
		t.Run(platform, func(t *testing.T) {
			t.Parallel()
			m := harness.NewMachine(t)
			m.OSArch = platform
			rel := m.Release(release)
			asset := "kokod-" + strings.ReplaceAll(platform, "/", "-")
			kokod := []byte(fileText(t, m.Kokod()))
			rel.Handle(asset, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				wantDownloadDir(t, m, asset)
				_, _ = w.Write(kokod)
			}))
			out := installAgain(t, m, "--owner", "alice")
			wantRequests(t, rel, "SHA256SUMS", asset)
			if out.Upgraded || out.JoinCode.Code == "" {
				t.Errorf("install printed %+v, want a fresh koko server and its first Join Code", out)
			}
			wantInitialised(t, m, "alice")
			if got := readFile(t, m, "/usr/local/bin/kokod"); got != fileText(t, m.Kokod()) {
				t.Errorf("/usr/local/bin/kokod holds %q, want the release's %s", got, asset)
			}
			if unit := readFile(t, m, "/etc/systemd/system/koko.service"); !strings.Contains(unit, "\nExecStart=/usr/local/bin/kokod run\n") {
				t.Errorf("the unit does not run /usr/local/bin/kokod run:\n%s", unit)
			}
			if _, err := os.Stat(m.Path("/usr/local/bin/koko")); !errors.Is(err, fs.ErrNotExist) {
				t.Errorf("install wrote /usr/local/bin/koko: %v", err)
			}
			wantNoTempDir(t, m)
		})
	}
}

// koko runs a downloaded kokod only when the release's SHA256SUMS lists it
// with the SHA-256 it has, and it is koko's build; a download that fails,
// a 404 or a redirect to plain HTTP among them, refuses too. Each refusal
// names the URL and --kokod, runs nothing, writes nothing, and leaves no
// temporary directory.
func TestServerInstallRefusesADownloadItCannotTrust(t *testing.T) {
	t.Parallel()
	const asset = "kokod-linux-amd64"
	other := releaseKokod()
	other.Revision = "0123456789abcdef0123456789abcdef01234567"
	for _, tc := range []struct {
		name string
		// serve changes the release rel serves on m.
		serve func(t *testing.T, m *harness.Machine, rel *harness.Releases)
		// asked are the assets koko asks for.
		asked []string
		says  string
	}{
		{
			"not in SHA256SUMS", func(_ *testing.T, _ *harness.Machine, rel *harness.Releases) {
				rel.Put("SHA256SUMS", harness.Sums([]string{"koko-linux-amd64"}, map[string][]byte{"koko-linux-amd64": []byte("koko")}))
			},
			[]string{"SHA256SUMS"},
			"SHA256SUMS lists no kokod-linux-amd64",
		},
		{
			"another SHA-256", func(t *testing.T, m *harness.Machine, rel *harness.Releases) {
				rel.Put(asset, []byte(fileText(t, m.NewKokod(releaseKokod()))))
			},
			[]string{"SHA256SUMS", asset},
			"kokod-linux-amd64 has the SHA-256 ",
		},
		{
			"another build", func(t *testing.T, m *harness.Machine, rel *harness.Releases) {
				kokod := []byte(fileText(t, m.NewKokod(other)))
				rel.Put(asset, kokod)
				rel.Put("SHA256SUMS", harness.Sums([]string{asset}, map[string][]byte{asset: kokod}))
			},
			[]string{"SHA256SUMS", asset},
			"koko runs only the kokod of its own build",
		},
		{
			"no SHA256SUMS", func(_ *testing.T, _ *harness.Machine, rel *harness.Releases) {
				rel.Handle("SHA256SUMS", http.NotFoundHandler())
			},
			[]string{"SHA256SUMS"},
			"SHA256SUMS: the answer is 404 Not Found",
		},
		{
			"no kokod", func(_ *testing.T, _ *harness.Machine, rel *harness.Releases) {
				rel.Handle(asset, http.NotFoundHandler())
			},
			[]string{"SHA256SUMS", asset},
			"kokod-linux-amd64: the answer is 404 Not Found",
		},
		{
			"a redirect to HTTP", func(t *testing.T, _ *harness.Machine, rel *harness.Releases) {
				var asked atomic.Int32
				plain := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { asked.Add(1) }))
				t.Cleanup(func() {
					plain.Close()
					if n := asked.Load(); n != 0 {
						t.Errorf("koko followed the redirect to plain HTTP %d time(s)", n)
					}
				})
				rel.Handle(asset, http.RedirectHandler(plain.URL+"/"+asset, http.StatusFound))
			},
			[]string{"SHA256SUMS", asset},
			"is not HTTPS",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := harness.NewMachine(t)
			rel := m.Release(release)
			tc.serve(t, m, rel)
			fresh := m.Files()
			msg := m.Koko("server", "install", "--owner", "alice", "--json").WantError(t, 1, "download_failed")
			for _, want := range []string{tc.says, rel.URL(tc.asked[len(tc.asked)-1]), "give kokod-linux-amd64 of v9.9.9 with --kokod <file>", "nothing was written"} {
				if !strings.Contains(msg, want) {
					t.Errorf("message %q does not say %q", msg, want)
				}
			}
			wantRequests(t, rel, tc.asked...)
			wantNothingDone(t, m, fresh)
			wantNoTempDir(t, m)
		})
	}
}

// A machine that cannot run the koko server is refused before koko asks
// GitHub anything: not Linux, not root, and systemd not running it.
func TestServerInstallChecksTheMachineBeforeADownload(t *testing.T) {
	t.Parallel()
	for name, setup := range map[string]func(t *testing.T, m *harness.Machine){
		"macOS":    func(_ *testing.T, m *harness.Machine) { m.OSArch = "darwin/arm64" },
		"not root": func(_ *testing.T, m *harness.Machine) { m.NotRoot = true },
		"no systemd": func(t *testing.T, m *harness.Machine) {
			if err := os.Remove(m.Path("/run/systemd/system")); err != nil {
				t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			m := harness.NewMachine(t)
			rel := m.Release(release)
			setup(t, m)
			m.Koko("server", "install", "--owner", "alice", "--json").WantError(t, 1, "unsupported_platform")
			wantRequests(t, rel)
		})
	}
}

// A kokod given with --kokod, or one installed of koko's build, is run
// without a download: install, the upgrade and uninstall ask GitHub
// nothing.
func TestServerInstallWithAKokodOfItsBuildDownloadsNothing(t *testing.T) {
	t.Parallel()
	m := harness.NewMachine(t)
	rel := m.Release(release)
	install(t, m, "--owner", "alice")
	wantInitialised(t, m, "alice")
	upgrade(t, m)
	m.Koko("server", "uninstall", "--json").WantSuccess(t)
	wantRequests(t, rel)
}

// koko of a build that is no release, such as one from a commit no tag
// names, or with changes, downloads nothing: without --kokod or a kokod of
// its build installed, it is refused with exit 2, naming --kokod bin/kokod.
func TestServerInstallOfADevelopmentBuildDownloadsNothing(t *testing.T) {
	t.Parallel()
	for _, version := range []string{"(devel)", "v9.9.10-0.20261109080000-0123456789ab", "v9.9.9+dirty"} {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			m := harness.NewMachine(t)
			rel := m.Release(version)
			fresh := m.Files()
			msg := m.Koko("server", "install", "--owner", "alice", "--json").WantError(t, 2, "invalid_usage")
			for _, want := range []string{version, "no release to download it from", "--kokod bin/kokod", "nothing was written"} {
				if !strings.Contains(msg, want) {
					t.Errorf("message %q does not say %q", msg, want)
				}
			}
			wantRequests(t, rel)
			wantNothingDone(t, m, fresh)
		})
	}
}
