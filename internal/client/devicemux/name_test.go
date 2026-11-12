package devicemux

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/cieons/koko/internal/harness/skip"
	"github.com/cieons/koko/internal/shared/build"
)

// A Device Mux's name carries its build: the version, then the start of the
// VCS revision when the version does not hold it already, so builds of two
// commits, or a tag and a commit, never share a Device Mux.
func TestTheNameCarriesTheVersionAndTheRevision(t *testing.T) {
	const rev = "abcdef1234567890abcdef1234567890abcdef12"
	for _, c := range []struct {
		build build.Info
		want  string
	}{
		{build.Info{Version: "v0.3.0", Revision: rev}, "v0.3.0-abcdef123456"},
		{build.Info{Version: "v0.3.0", Revision: "0123456789ab" + rev[12:]}, "v0.3.0-0123456789ab"},
		{build.Info{Version: "v0.0.0-20261105123456-abcdef123456", Revision: rev}, "v0.0.0-20261105123456-abcdef123456"},
		{build.Info{Version: "v0.3.1-0.20261105123456-abcdef123456+dirty", Revision: rev}, "v0.3.1-0.20261105123456-abcdef123456+dirty"},
		{build.Info{Version: "(devel)"}, "_devel_"},
		{build.Info{Version: "v1/../x"}, "v1_.._x"},
	} {
		if got := Name(c.build); got != c.want {
			t.Errorf("Name(%+v) = %q, want %q", c.build, got, c.want)
		}
	}
}

// The longest name a release or a commit's build has stays, in macOS's
// $TMPDIR, under the 104 bytes a Unix socket's path may hold there.
func TestTheSocketPathFitsMacOSsLimit(t *testing.T) {
	const tmpdir = "/var/folders/ly/y_8f1w356d5684_1y7f6hr700000gn/T/"
	name := Name(build.Info{Version: "v0.3.1-0.20261105123456-abcdef123456+dirty", Revision: "abcdef1234567890abcdef1234567890abcdef12"})
	if path := filepath.Join(tmpdir, "koko", name+".sock"); len(path) > maxSocketPath {
		t.Errorf("%s is %d bytes, past %d", path, len(path), maxSocketPath)
	}
}

// Socket refuses a path too long for a Unix socket, so commands go direct.
func TestSocketRefusesAPathTooLong(t *testing.T) {
	skip.OnWindows(t, "the Device Mux has no runtime directory on Windows until #153")
	long := filepath.Join(t.TempDir(), strings.Repeat("d", 100))
	if err := os.Mkdir(long, 0o700); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{"TMPDIR": long, "XDG_RUNTIME_DIR": long}
	if _, err := Socket(func(k string) string { return env[k] }); err == nil || !strings.Contains(err.Error(), "past the 103") {
		t.Errorf("Socket in %s = %v, want it refused as too long", long, err)
	}
}

// The runtime directory is the OS's per-user one: $XDG_RUNTIME_DIR on
// Linux, $TMPDIR on macOS, and /tmp/koko-<uid> without it.
func TestTheRuntimeDirectory(t *testing.T) {
	skip.OnWindows(t, "the Device Mux has no runtime directory on Windows until #153")
	env := map[string]string{"XDG_RUNTIME_DIR": "/run/user/1000", "TMPDIR": "/var/folders/x/T/"}
	got, err := runtimeDir(func(k string) string { return env[k] })
	want := map[string]string{"linux": "/run/user/1000/koko", "darwin": "/var/folders/x/T/koko"}[runtime.GOOS]
	if err != nil || got != want {
		t.Errorf("runtimeDir = %q, %v, want %q", got, err, want)
	}
	got, err = runtimeDir(func(string) string { return "" })
	if want := fmt.Sprintf("/tmp/koko-%d", os.Getuid()); err != nil || got != want {
		t.Errorf("runtimeDir without the variable = %q, %v, want %q", got, err, want)
	}
}

// The peer-credential check reads the OS user of the process that
// connected: this one's, here.
func TestPeerUIDIsTheConnectingUser(t *testing.T) {
	skip.OnWindows(t, "Windows gives a Unix socket no peer credentials: the private directory is the Device Mux's only gate there (ADR 0051)")
	dir, err := os.MkdirTemp("", "koko-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	var lc net.ListenConfig
	ln, err := lc.Listen(t.Context(), "unix", filepath.Join(dir, "s.sock"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	var d net.Dialer
	c, err := d.DialContext(t.Context(), "unix", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	nc, err := ln.Accept()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = nc.Close() })
	if uid, err := peerUID(nc); err != nil || uid != os.Getuid() {
		t.Errorf("peerUID = %d, %v, want %d", uid, err, os.Getuid())
	}
}
