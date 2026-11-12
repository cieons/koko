package cli_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"golang.org/x/sys/windows"

	"github.com/cieons/koko/internal/client/cli"
	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/shared/api"
	"github.com/cieons/koko/internal/shared/build"
	kokorelease "github.com/cieons/koko/internal/shared/release"
)

// windowsAsset is the koko of a Windows device's platform.
const windowsAsset = "koko-windows-amd64.exe"

// runAsKoko, when set, makes this test binary stand for a running koko: it
// runs no test, and exits once its stdin ends.
const runAsKoko = "KOKO_TEST_RUN_AS_KOKO"

func TestMain(m *testing.M) {
	if os.Getenv(runAsKoko) != "" {
		_, _ = io.Copy(io.Discard, os.Stdin)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// windowsKoko is a Windows device whose koko is the release current, under
// a profile directory of its own, with a fake GitHub to update from. It is
// the cli.Kokod its koko runs on: it states the build of each file it
// makes, by the file's bytes. The fake machine of the other update tests is
// a Linux machine, so koko update on Windows has this one.
type windowsKoko struct {
	t    *testing.T
	home string
	// exe is koko's file, and was the bytes it holds.
	exe string
	was []byte
	srv *httptest.Server

	mu     sync.Mutex
	builds map[[sha256.Size]byte]build.Info
	// assets are what the fake GitHub serves, by request path, latest the
	// release its redirect names, and asked the path of each request.
	assets map[string][]byte
	latest string
	asked  []string
	// holdDownloads makes Build keep each file it reads from a
	// .koko-update-* directory open, as a virus scanner does a new file,
	// until the test ends.
	holdDownloads bool
}

var _ cli.Kokod = (*windowsKoko)(nil)

// releasesPath is where the fake GitHub serves koko's releases.
const releasesPath = "/cieons/koko/releases"

// newWindowsKoko returns a device whose koko runs from the path under its
// profile directory, such as .local\bin\koko.exe, a file holding content.
func newWindowsKoko(t *testing.T, under string, content []byte) *windowsKoko {
	t.Helper()
	k := &windowsKoko{
		t:      t,
		home:   t.TempDir(),
		was:    content,
		builds: map[[sha256.Size]byte]build.Info{},
		assets: map[string][]byte{},
	}
	k.exe = filepath.Join(k.home, under)
	k.state(content, current, "windows/amd64")
	if err := os.MkdirAll(filepath.Dir(k.exe), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(k.exe, content, 0o755); err != nil {
		t.Fatal(err)
	}
	k.srv = httptest.NewTLSServer(http.HandlerFunc(k.serve))
	t.Cleanup(k.srv.Close)
	return k
}

// state states that a file holding content is the koko of version, built
// for platform.
func (k *windowsKoko) state(content []byte, version, platform string) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.builds[sha256.Sum256(content)] = build.Info{Path: kokorelease.KokoPackage, Version: version, Platform: platform}
}

// publish adds the release version to the fake GitHub, whose
// koko-windows-amd64.exe is a koko of that version built for platform, and
// makes it the latest. It returns the asset's bytes.
func (k *windowsKoko) publish(version, platform string) []byte {
	content := []byte("koko " + version + " for " + platform + "\n")
	k.state(content, version, platform)
	base := releasesPath + "/download/" + version + "/"
	k.mu.Lock()
	defer k.mu.Unlock()
	k.assets[base+windowsAsset] = content
	k.assets[base+"SHA256SUMS"] = harness.Sums([]string{windowsAsset}, map[string][]byte{windowsAsset: content})
	k.latest = version
	return content
}

func (k *windowsKoko) serve(w http.ResponseWriter, req *http.Request) {
	k.mu.Lock()
	k.asked = append(k.asked, req.URL.Path)
	content, ok := k.assets[req.URL.Path]
	latest := k.latest
	k.mu.Unlock()
	switch {
	case req.URL.Path == releasesPath+"/latest":
		http.Redirect(w, req, k.srv.URL+releasesPath+"/tag/"+latest, http.StatusFound)
	case ok:
		_, _ = w.Write(content)
	default:
		http.NotFound(w, req)
	}
}

func (k *windowsKoko) Self() build.Info {
	return build.Info{Path: kokorelease.KokoPackage, Version: current, Platform: k.Platform()}
}

func (*windowsKoko) Platform() string { return "windows/amd64" }

func (*windowsKoko) RunsAsRoot() bool { return false }

func (*windowsKoko) Path(p string) string { return p }

func (k *windowsKoko) Build(file string) (build.Info, error) {
	content, err := os.ReadFile(file)
	if err != nil {
		return build.Info{}, err
	}
	k.mu.Lock()
	b, ok := k.builds[sha256.Sum256(content)]
	hold := k.holdDownloads && strings.HasPrefix(filepath.Base(filepath.Dir(file)), ".koko-update-")
	k.mu.Unlock()
	if !ok {
		return build.Info{}, errors.New("not a Go binary")
	}
	if hold {
		k.hold(file)
	}
	return b, nil
}

// hold opens file as a program that reads it does, letting others read and
// write it but not rename or delete it, and keeps it open until the test
// ends.
func (k *windowsKoko) hold(file string) {
	p, err := windows.UTF16PtrFromString(file)
	if err != nil {
		k.t.Error(err)
		return
	}
	h, err := windows.CreateFile(p, windows.GENERIC_READ, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil,
		windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		k.t.Errorf("hold %s open: %v", file, err)
		return
	}
	k.t.Cleanup(func() { _ = windows.CloseHandle(h) })
}

func (*windowsKoko) Run(context.Context, string, []string, io.Writer, io.Writer) (int, error) {
	return 0, errors.New("a Windows device runs no kokod")
}

// koko runs one koko command line on the device.
func (k *windowsKoko) koko(args ...string) harness.Result {
	k.t.Helper()
	r := cli.Runner{
		Kokod:      k,
		SocketPath: filepath.Join(k.home, "koko.sock"),
		Releases:   cli.Releases{URL: k.srv.URL + releasesPath, Client: k.srv.Client()},
		Executable: func() (string, error) { return k.exe, nil },
	}
	return harness.Run(k.t, r, []string{harness.HomeEnv(k.home)}, args...)
}

// wantAsked fails the test unless the fake GitHub was asked for exactly the
// paths, in order, since the last call.
func (k *windowsKoko) wantAsked(paths ...string) {
	k.t.Helper()
	k.mu.Lock()
	got := k.asked
	k.asked = nil
	k.mu.Unlock()
	if !slices.Equal(got, paths) {
		k.t.Errorf("koko asked the fake GitHub for %q, want %q", got, paths)
	}
}

// wantHolds fails the test unless koko's file holds content.
func (k *windowsKoko) wantHolds(content []byte) {
	k.t.Helper()
	if got, err := os.ReadFile(k.exe); err != nil || !bytes.Equal(got, content) {
		k.t.Errorf("%s holds %d bytes (%v), want the %d of %.40q", k.exe, len(got), err, len(content), content)
	}
}

// beside returns the names of what koko's directory holds beside its file.
func (k *windowsKoko) beside() []string {
	k.t.Helper()
	entries, err := os.ReadDir(filepath.Dir(k.exe))
	if err != nil {
		k.t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		if e.Name() != filepath.Base(k.exe) {
			names = append(names, e.Name())
		}
	}
	return names
}

// hidden tells whether the file at path has Windows' hidden attribute.
func hidden(t *testing.T, path string) bool {
	t.Helper()
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	attrs, err := windows.GetFileAttributes(p)
	if err != nil {
		t.Fatalf("read the attributes of %s: %v", path, err)
	}
	return attrs&windows.FILE_ATTRIBUTE_HIDDEN != 0
}

// koko update replaces %USERPROFILE%\.local\bin\koko.exe while a koko runs
// from it, which Windows lets no one overwrite or delete: it asks for
// koko-windows-amd64.exe, the running koko's file stays beside the new one
// as a hidden .koko.old-*.exe, from which that koko runs on to its end, and
// the next update deletes it, with the file it renames aside itself.
func TestUpdateOnWindowsReplacesTheRunningKoko(t *testing.T) {
	t.Parallel()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	program, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	k := newWindowsKoko(t, `.local\bin\koko.exe`, program)
	running := exec.CommandContext(t.Context(), k.exe)
	running.Env = append(os.Environ(), runAsKoko+"=1")
	stdin, err := running.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := running.Start(); err != nil {
		t.Fatalf("run koko.exe: %v", err)
	}
	stopped := false
	stop := func() error {
		if stopped {
			return nil
		}
		stopped = true
		_ = stdin.Close()
		return running.Wait()
	}
	t.Cleanup(func() { _ = stop() })
	next := k.publish(latest, "windows/amd64")

	r := k.koko("update")
	if r.Status != 0 || r.Stderr != "" || !strings.Contains(r.Stdout, "Updated koko from "+current+" to "+latest+` at ~\.local\bin\koko.exe.`) {
		t.Fatalf("koko update exited %d, stdout %q, stderr %q", r.Status, r.Stdout, r.Stderr)
	}
	base := releasesPath + "/download/" + latest + "/"
	k.wantAsked(releasesPath+"/latest", base+"SHA256SUMS", base+windowsAsset)
	k.wantHolds(next)
	left := k.beside()
	if len(left) != 1 || !strings.HasPrefix(left[0], ".koko.old-") || !strings.HasSuffix(left[0], ".exe") {
		t.Fatalf("koko update left %q beside koko.exe, want one .koko.old-*.exe", left)
	}
	old := filepath.Join(filepath.Dir(k.exe), left[0])
	if got, err := os.ReadFile(old); err != nil || !bytes.Equal(got, program) {
		t.Errorf("%s holds %d bytes (%v), want the running koko's %d", old, len(got), err, len(program))
	}
	if !hidden(t, old) {
		t.Errorf("%s is not hidden", old)
	}
	if err := stop(); err != nil {
		t.Errorf("the koko that ran while its file was replaced ended with %v", err)
	}

	earlier := k.publish(back, "windows/amd64")
	r = k.koko("update", "--version", back, "--json")
	r.WantSuccess(t)
	if want := fmt.Sprintf("%q", filepath.Join(k.home, ".local", "bin", "koko.exe")); !strings.Contains(r.Stdout, `"path": `+want) {
		t.Errorf("koko update --json printed %s, want the path %s", r.Stdout, want)
	}
	k.wantHolds(earlier)
	if left := k.beside(); len(left) != 0 {
		t.Errorf("the next koko update left %q beside koko.exe", left)
	}
}

// When the checked file cannot be renamed into place, here because another
// program holds it open, koko update renames the old file back and says
// that nothing was changed.
func TestUpdateOnWindowsPutsTheOldFileBackWhenTheNewOneCannotMoveIn(t *testing.T) {
	t.Parallel()
	k := newWindowsKoko(t, `.local\bin\koko.exe`, []byte("koko "+current+"\n"))
	k.publish(latest, "windows/amd64")
	k.holdDownloads = true

	msg := k.koko("update", "--json").WantError(t, 1, api.CodeInternal.Name)
	for _, want := range []string{"replace " + filepath.Join(k.home, ".local", "bin", "koko.exe"), "nothing was changed"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q does not say %q", msg, want)
		}
	}
	k.wantHolds(k.was)
	// The download's directory stays while its file is held open.
	if left := k.beside(); slices.ContainsFunc(left, func(name string) bool { return strings.HasPrefix(name, ".koko.old-") }) {
		t.Errorf("koko update left %q beside koko.exe, want no .koko.old-*.exe", left)
	}
}

// koko update refuses a koko elsewhere than %USERPROFILE%\.local\bin\koko.exe,
// naming install.ps1's directory, with nothing asked of GitHub and nothing
// written.
func TestUpdateOnWindowsRefusesAKokoElsewhere(t *testing.T) {
	t.Parallel()
	k := newWindowsKoko(t, `Downloads\koko.exe`, []byte("koko "+current+"\n"))
	k.publish(latest, "windows/amd64")

	msg := k.koko("update", "--json").WantError(t, 1, "update_unsupported")
	for _, want := range []string{`only the koko install.ps1 put in ~\.local\bin;`, `Downloads\koko.exe`, "the way it was installed", "nothing was changed"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q does not say %q", msg, want)
		}
	}
	k.wantAsked()
	k.wantHolds(k.was)
	if left := k.beside(); len(left) != 0 {
		t.Errorf("koko update left %q beside koko.exe", left)
	}
}

// koko update refuses a koko-windows-amd64.exe built for another platform
// by its build information, though the release's SHA256SUMS lists it, with
// nothing changed.
func TestUpdateOnWindowsRefusesAKokoBuiltForAnotherPlatform(t *testing.T) {
	t.Parallel()
	k := newWindowsKoko(t, `.local\bin\koko.exe`, []byte("koko "+current+"\n"))
	k.publish(latest, "linux/amd64")

	msg := k.koko("update", "--json").WantError(t, 1, "download_failed")
	base := releasesPath + "/download/" + latest + "/"
	for _, want := range []string{base + windowsAsset, "koko for linux/amd64, and this machine is windows/amd64", "nothing was changed"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q does not say %q", msg, want)
		}
	}
	k.wantAsked(releasesPath+"/latest", base+"SHA256SUMS", base+windowsAsset)
	k.wantHolds(k.was)
	if left := k.beside(); len(left) != 0 {
		t.Errorf("koko update left %q beside koko.exe", left)
	}
}
