package cli_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/cieons/koko/internal/client/devicefiles"
	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/shared/build"
	kokorelease "github.com/cieons/koko/internal/shared/release"
	"github.com/cieons/koko/internal/shared/timing"
)

// The releases the koko update tests publish: koko on the machine is
// current, an older release is back, and the latest is latest.
const (
	current = "v0.4.0"
	back    = "v0.3.1"
	latest  = "v0.5.0"
	// asset is the koko of a fresh machine's platform.
	asset = "koko-linux-amd64"
)

// updating is a machine whose koko is a release, current, at the path
// koko update replaces, with back and latest published.
type updating struct {
	m *harness.Machine
	// exe is the real path of koko's file, and was the bytes it holds.
	exe string
	was []byte
	// self is the release koko is; back and latest are the older and the
	// latest release.
	self, back, latest *harness.Releases
}

// userKoko returns a machine whose koko is the release current at
// $HOME/.local/bin/koko, run by a user other than root.
func userKoko(t *testing.T) *updating {
	t.Helper()
	m := harness.NewMachine(t)
	m.NotRoot = true
	return publish(m, filepath.Join(m.Home(), ".local", "bin", "koko"), current)
}

// rootKoko returns a machine whose koko is the release current at
// /usr/local/bin/koko, run by root.
func rootKoko(t *testing.T) *updating {
	t.Helper()
	m := harness.NewMachine(t)
	return publish(m, m.Path("/usr/local/bin/koko"), current)
}

// publish states that koko on m is the release version, running from the
// real path exe, and publishes back and then latest.
func publish(m *harness.Machine, exe, version string) *updating {
	u := &updating{m: m, exe: exe, self: m.Release(version)}
	u.was = m.KokoAt(exe)
	u.back = u.self.Publish(back)
	u.latest = u.self.Publish(latest)
	return u
}

// wantInstalled fails the test unless koko's file holds the asset of rel,
// with the mode 0755, and the directory beside it holds nothing else.
func (u *updating) wantInstalled(t *testing.T, rel *harness.Releases) {
	t.Helper()
	got, err := os.ReadFile(u.exe)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, rel.Content(asset)) {
		t.Errorf("%s holds %q, want the release's %s", u.exe, got, asset)
	}
	if b, err := u.m.Build(u.exe); err != nil || b.Path != kokorelease.KokoPackage || b.Platform != "linux/amd64" {
		t.Errorf("%s is %+v (%v), want koko for linux/amd64", u.exe, b, err)
	}
	if info, err := os.Stat(u.exe); err != nil || info.Mode().Perm() != 0o755 {
		t.Errorf("%s has the mode %v (%v), want 0755", u.exe, info.Mode().Perm(), err)
	}
	u.wantAlone(t)
}

// wantUnchanged fails the test unless koko's file holds what it held, and
// the directory beside it holds nothing else.
func (u *updating) wantUnchanged(t *testing.T) {
	t.Helper()
	if got, err := os.ReadFile(u.exe); err != nil || !bytes.Equal(got, u.was) {
		t.Errorf("koko update changed %s: it holds %q (%v)", u.exe, got, err)
	}
	u.wantAlone(t)
}

// wantAlone fails the test unless koko's directory holds its file alone: no
// .koko-update-* is left.
func (u *updating) wantAlone(t *testing.T) {
	t.Helper()
	entries, err := os.ReadDir(filepath.Dir(u.exe))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "koko" {
		t.Errorf("koko update left %v beside koko", entries)
	}
}

// wantAsked fails the test unless the fake GitHub was asked for exactly the
// paths, in order.
func (u *updating) wantAsked(t *testing.T, paths ...string) {
	t.Helper()
	if got := u.self.Requests(); !slices.Equal(got, paths) {
		t.Errorf("koko asked the fake GitHub for %q, want %q", got, paths)
	}
}

// homeless is out with the machine's HOME written $HOME, for a golden file.
func (u *updating) homeless(out string) string {
	return strings.ReplaceAll(out, u.m.Home(), "$HOME")
}

// koko update of a release at $HOME/.local/bin/koko, run by its user, or
// at /usr/local/bin/koko, run by root, reads the releases/latest redirect,
// downloads exactly SHA256SUMS and koko-<os>-<arch> of the latest release
// into a 0700 .koko-update-* directory beside the file, and renames the
// checked file over it, mode 0755, leaving nothing beside it.
func TestUpdateInstallsTheLatestRelease(t *testing.T) {
	t.Parallel()
	for name, setup := range map[string]func(t *testing.T) *updating{"its user's": userKoko, "root's": rootKoko} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			u := setup(t)
			content := u.latest.Content(asset)
			u.latest.Handle(asset, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				u.wantDownloadDir(t)
				_, _ = w.Write(content)
			}))
			r := u.m.Koko("update", "--json")
			r.WantSuccess(t)
			u.wantAsked(t, harness.LatestPath(), u.latest.Path("SHA256SUMS"), u.latest.Path(asset))
			u.wantInstalled(t, u.latest)
			if name == "its user's" {
				harness.Golden(t, "update.json", u.homeless(r.Stdout))
			} else if !strings.Contains(r.Stdout, `"path": "/usr/local/bin/koko"`) {
				t.Errorf("update --json printed %s, want the path /usr/local/bin/koko", r.Stdout)
			}
		})
	}
	t.Run("text", func(t *testing.T) {
		t.Parallel()
		u := userKoko(t)
		r := u.m.Koko("update")
		r.WantSuccess(t)
		u.wantInstalled(t, u.latest)
		harness.Golden(t, "update.txt", r.Stdout)
	})
}

// wantDownloadDir fails the test unless, while koko update downloads, the
// directory of koko's file holds the file and one .koko-update-* directory,
// mode 0700.
func (u *updating) wantDownloadDir(t *testing.T) {
	t.Helper()
	dir := filepath.Dir(u.exe)
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 2 || !strings.HasPrefix(entries[0].Name(), ".koko-update-") {
		t.Errorf("koko update downloads beside koko into %v, want one .koko-update-* directory: %v", entries, err)
		return
	}
	if info, err := os.Stat(filepath.Join(dir, entries[0].Name())); err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 {
		t.Errorf("koko update downloads into %s, which is %v (%v), want a directory of mode 0700", entries[0].Name(), info.Mode(), err)
	}
}

// --version installs the release it names, older too, without asking which
// is the latest; going back to a release without koko update says the
// install line is the way back up.
func TestUpdateInstallsTheReleaseVersionNames(t *testing.T) {
	t.Parallel()
	u := userKoko(t)
	r := u.m.Koko("update", "--version", back)
	r.WantSuccess(t)
	u.wantAsked(t, u.back.Path("SHA256SUMS"), u.back.Path(asset))
	u.wantInstalled(t, u.back)
	harness.Golden(t, "update-back.txt", r.Stdout)
}

// When the target is this koko's own release, koko update says so and
// exits 0, having read only the redirect; --check says what it would do,
// with --json too, and changes nothing.
func TestUpdateChangesNothingWhenItHasNothingToDo(t *testing.T) {
	t.Parallel()
	t.Run("the same release", func(t *testing.T) {
		t.Parallel()
		u := userKoko(t)
		u.self.Latest()
		r := u.m.Koko("update")
		r.WantSuccess(t)
		if want := "koko v0.4.0 is installed at ~/.local/bin/koko.\n"; r.Stdout != want {
			t.Errorf("update printed %q, want %q", r.Stdout, want)
		}
		u.wantAsked(t, harness.LatestPath())
		u.wantUnchanged(t)
	})
	t.Run("--check", func(t *testing.T) {
		t.Parallel()
		u := userKoko(t)
		r := u.m.Koko("update", "--check", "--json")
		r.WantSuccess(t)
		harness.Golden(t, "update-check.json", u.homeless(r.Stdout))
		r = u.m.Koko("update", "--check")
		r.WantSuccess(t)
		harness.Golden(t, "update-check.txt", r.Stdout)
		u.wantAsked(t, harness.LatestPath(), harness.LatestPath())
		u.wantUnchanged(t)
	})
}

// --version takes a release's tag alone, refused with exit 2 before
// anything is asked.
func TestUpdateRefusesAVersionThatIsNoRelease(t *testing.T) {
	t.Parallel()
	u := userKoko(t)
	for _, version := range []string{"0.5.0", "v0.5", "latest", "v0.5.0+dirty"} {
		msg := u.m.Koko("update", "--version", version, "--json").WantError(t, 2, "invalid_usage")
		if !strings.Contains(msg, version) {
			t.Errorf("message %q does not name %q", msg, version)
		}
	}
	u.wantAsked(t)
	u.wantUnchanged(t)
}

// koko update refuses a koko it did not install, naming what to do
// instead, with nothing written and nothing asked of GitHub: a development
// build, the image's, root's run by another user, and a file anywhere
// else.
func TestUpdateRefusesAKokoItDidNotInstall(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		setup func(t *testing.T) *updating
		env   []string
		says  []string
	}{
		{"a development build", func(t *testing.T) *updating {
			m := harness.NewMachine(t)
			m.NotRoot = true
			return publish(m, filepath.Join(m.Home(), ".local", "bin", "koko"), "v0.4.1-0.20261109080000-0123456789ab")
		}, nil, []string{"development build (v0.4.1-0.20261109080000-0123456789ab)", "build it again from the source"}},
		{"the image's", rootKoko, []string{"KOKO_IMAGE=ghcr.io/cieons/koko"}, []string{"the container image's", "KOKO_VERSION in its Compose file"}},
		{"root's, run by another user", func(t *testing.T) *updating {
			u := rootKoko(t)
			u.m.NotRoot = true
			return u
		}, nil, []string{"root's, at /usr/local/bin/koko", "sudo koko update", "install.sh | sh"}},
		{"another path", func(t *testing.T) *updating {
			m := harness.NewMachine(t)
			m.NotRoot = true
			return publish(m, filepath.Join(m.Home(), "Downloads", "koko"), current)
		}, nil, []string{"only the koko install.sh put in ~/.local/bin, or root's in /usr/local/bin", "Downloads/koko", "the way it was installed"}},
		{"its user's koko, run by root", func(t *testing.T) *updating {
			m := harness.NewMachine(t)
			return publish(m, filepath.Join(m.Home(), ".local", "bin", "koko"), current)
		}, nil, []string{"only the koko install.sh put in ~/.local/bin"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			u := tc.setup(t)
			fresh := u.m.Files()
			msg := u.m.KokoEnv(tc.env, "update", "--json").WantError(t, 1, "update_unsupported")
			for _, want := range append(tc.says, "nothing was changed") {
				if !strings.Contains(msg, want) {
					t.Errorf("message %q does not say %q", msg, want)
				}
			}
			u.wantAsked(t)
			u.wantUnchanged(t)
			wantNothingDone(t, u.m, fresh)
		})
	}
}

// koko update installs only a koko that the release's SHA256SUMS lists
// with the SHA-256 it has, of koko's main package, the release's version
// and this platform; a download that fails, a 404 or a redirect to plain
// HTTP among them, refuses too. Each refusal names the URL, replaces
// nothing and leaves no directory behind.
func TestUpdateRefusesADownloadItCannotTrust(t *testing.T) {
	t.Parallel()
	// koko puts a file the machine states the build b of in the latest
	// release as its asset, with SHA256SUMS listing it.
	koko := func(t *testing.T, m *harness.Machine, rel *harness.Releases, b build.Info) {
		content, err := os.ReadFile(m.NewKokod(b))
		if err != nil {
			t.Fatal(err)
		}
		rel.Put(asset, content)
		rel.Put("SHA256SUMS", harness.Sums([]string{asset}, map[string][]byte{asset: content}))
	}
	good := build.Info{Path: kokorelease.KokoPackage, Version: latest, Platform: "linux/amd64"}
	for _, tc := range []struct {
		name string
		// serve changes the latest release rel serves on m.
		serve func(t *testing.T, m *harness.Machine, rel *harness.Releases)
		// asked are the latest release's assets koko asks for.
		asked []string
		says  string
	}{
		{
			"not in SHA256SUMS", func(_ *testing.T, _ *harness.Machine, rel *harness.Releases) {
				rel.Put("SHA256SUMS", harness.Sums([]string{"koko-darwin-arm64"}, map[string][]byte{"koko-darwin-arm64": []byte("koko")}))
			},
			[]string{"SHA256SUMS"},
			"SHA256SUMS lists no koko-linux-amd64",
		},
		{
			"another SHA-256", func(_ *testing.T, _ *harness.Machine, rel *harness.Releases) {
				rel.Put(asset, rel.Content("koko-linux-arm64"))
			},
			[]string{"SHA256SUMS", asset},
			"koko-linux-amd64 has the SHA-256 ",
		},
		{
			"kokod", func(t *testing.T, m *harness.Machine, rel *harness.Releases) {
				b := good
				b.Path = "github.com/cieons/koko/cmd/kokod"
				koko(t, m, rel, b)
			},
			[]string{"SHA256SUMS", asset},
			"which is github.com/cieons/koko/cmd/kokod, not koko (github.com/cieons/koko/cmd/koko)",
		},
		{
			"another version", func(t *testing.T, m *harness.Machine, rel *harness.Releases) {
				b := good
				b.Version = "v0.4.9"
				koko(t, m, rel, b)
			},
			[]string{"SHA256SUMS", asset},
			"it is koko v0.4.9 (",
		},
		{
			"another platform", func(t *testing.T, m *harness.Machine, rel *harness.Releases) {
				b := good
				b.Platform = "linux/arm64"
				koko(t, m, rel, b)
			},
			[]string{"SHA256SUMS", asset},
			"koko for linux/arm64, and this machine is linux/amd64",
		},
		{
			"no koko", func(_ *testing.T, _ *harness.Machine, rel *harness.Releases) {
				rel.Handle(asset, http.NotFoundHandler())
			},
			[]string{"SHA256SUMS", asset},
			"koko-linux-amd64: the answer is 404 Not Found",
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
			u := userKoko(t)
			tc.serve(t, u.m, u.latest)
			fresh := u.m.Files()
			msg := u.m.Koko("update", "--json").WantError(t, 1, "download_failed")
			for _, want := range []string{tc.says, u.latest.URL(tc.asked[len(tc.asked)-1]), "nothing was changed"} {
				if !strings.Contains(msg, want) {
					t.Errorf("message %q does not say %q", msg, want)
				}
			}
			var asked []string
			for _, name := range tc.asked {
				asked = append(asked, u.latest.Path(name))
			}
			u.wantAsked(t, append([]string{harness.LatestPath()}, asked...)...)
			u.wantUnchanged(t)
			wantNothingDone(t, u.m, fresh)
		})
	}
}

// With no latest release to read, koko update fails naming the redirect,
// having asked for nothing else and changed nothing.
func TestUpdateFailsWithoutALatestRelease(t *testing.T) {
	t.Parallel()
	m := harness.NewMachine(t)
	m.NotRoot = true
	u := &updating{m: m, exe: filepath.Join(m.Home(), ".local", "bin", "koko"), self: m.Release(current)}
	u.was = m.KokoAt(u.exe)
	msg := m.Koko("update", "--json").WantError(t, 1, "download_failed")
	for _, want := range []string{"could not find its latest release", harness.LatestPath(), "404 Not Found", "nothing was changed"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q does not say %q", msg, want)
		}
	}
	u.wantAsked(t, harness.LatestPath())
	u.wantUnchanged(t)
}

// proxyConfig writes koko's ssh_config into home with the bare name web1,
// running koko from koko, as a koko there would have written it, and
// returns it.
func proxyConfig(t *testing.T, home, koko string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(home, ".ssh", "koko"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := devicefiles.WriteSSHConfig(home, []string{"web1"}); err != nil {
		t.Fatal(err)
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return replaceConfig(t, home, self, koko)
}

// replaceConfig rewrites the koko binary koko's ssh_config in home runs from
// was to koko, by hand, and returns the file.
func replaceConfig(t *testing.T, home, was, koko string) string {
	t.Helper()
	path := devicefiles.SSHConfigPath(home)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	b = bytes.ReplaceAll(b, []byte(`ProxyCommand "`+was+`"`), []byte(`ProxyCommand "`+koko+`"`))
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// wantConfig fails the test unless koko's ssh_config in home is want.
func wantConfig(t *testing.T, home, want string) {
	t.Helper()
	got, err := os.ReadFile(devicefiles.SSHConfigPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Errorf("koko's ssh_config is:\n%s\nwant:\n%s", got, want)
	}
}

// A ProxyCommand that runs /usr/local/bin/koko, as a v0.3.x device's does,
// is pointed at the koko koko update replaces, by koko update once it has
// replaced it, and by any command run from the user's shell by that koko,
// with every other byte, the bare names among them, as it was. A koko
// update that changes nothing and a development build leave it, and the
// Device Mux's refresh still keeps whatever path is there.
func TestCommandsPointSSHAtTheKokoTheyRunFrom(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{{"update"}, {"version"}} {
		t.Run(args[0], func(t *testing.T) {
			t.Parallel()
			u := userKoko(t)
			home := u.m.Home()
			old := u.m.Path("/usr/local/bin/koko")
			was := proxyConfig(t, home, old)
			koko := filepath.Join(home, ".local", "bin", "koko")
			u.m.Koko(args...).WantSuccess(t)
			want := strings.ReplaceAll(was, `ProxyCommand "`+old+`"`, `ProxyCommand "`+koko+`"`)
			if want == was || !strings.Contains(want, "Host *.koko web1\n") {
				t.Fatalf("koko's ssh_config as written is not one this test reads:\n%s", was)
			}
			wantConfig(t, home, want)

			// The Device Mux's refresh keeps the path there, though this
			// test's own binary refreshes it.
			was = replaceConfig(t, home, koko, old)
			if _, err := devicefiles.RefreshSSHConfig(home, []string{"web1"}); err != nil {
				t.Fatal(err)
			}
			wantConfig(t, home, was)
		})
	}
	t.Run("a koko update that changes nothing", func(t *testing.T) {
		t.Parallel()
		u := userKoko(t)
		was := proxyConfig(t, u.m.Home(), u.m.Path("/usr/local/bin/koko"))
		u.m.Koko("update", "--check").WantSuccess(t)
		u.m.Koko("update", "--version", "latest", "--json").WantError(t, 2, "invalid_usage")
		wantConfig(t, u.m.Home(), was)
	})
	t.Run("a development build", func(t *testing.T) {
		t.Parallel()
		m := harness.NewMachine(t)
		m.NotRoot = true
		m.KokoAt(filepath.Join(m.Home(), ".local", "bin", "koko"))
		was := proxyConfig(t, m.Home(), m.Path("/usr/local/bin/koko"))
		m.Koko("version").WantSuccess(t)
		wantConfig(t, m.Home(), was)
	})
}

// A command of an old build whose file koko update has replaced, which
// must start a Device Mux, would start the new build's, which never
// answers on the old build's socket: it goes direct at once, starting
// none.
func TestADeviceMuxSpawnOfAnotherBuildGoesDirectAtOnce(t *testing.T) {
	t.Parallel()
	h, fwd, _, _ := initThrough(t)
	d := harness.NewMuxDevice(t, timing.Timings{})
	m := harness.NewMachine(t)
	newer := m.Self()
	newer.Version = latest
	d.RunsFrom(m, m.NewKokod(newer))
	d.Koko("join", deviceAdd(t, h, "laptop").Code).WantSuccess(t)
	wantDirect(t, d.KokoEnv([]string{"KOKO_LOG_LEVEL=debug"}, "space", "ls", "--json"), "is now koko "+latest)
	if n := fwd.Conns(); n != 2 {
		t.Errorf("the device made %d connections, want 2: the join's, and the command's own", n)
	}
	if log := d.MuxLog(); log != "" {
		t.Errorf("a Device Mux started; it logged:\n%s", log)
	}
}
