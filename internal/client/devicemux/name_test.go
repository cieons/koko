package devicemux

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

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
	long := filepath.Join(t.TempDir(), strings.Repeat("d", 100))
	if err := os.Mkdir(long, 0o700); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{"TMPDIR": long, "XDG_RUNTIME_DIR": long, "LOCALAPPDATA": long}
	if _, err := Socket(func(k string) string { return env[k] }); err == nil || !strings.Contains(err.Error(), "past the 103") {
		t.Errorf("Socket in %s = %v, want it refused as too long", long, err)
	}
}

// A socket path of the longest length Socket allows, 103 bytes, takes a
// listener and a connection, on Windows as on Linux and macOS.
func TestASocketPathOf103BytesListens(t *testing.T) {
	dir, err := os.MkdirTemp("", "koko-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	pad := maxSocketPath - len(dir) - len(string(filepath.Separator)) - len(".sock")
	if pad < 1 {
		t.Fatalf("the temporary directory %s is too long a path to hold a socket of %d bytes", dir, maxSocketPath)
	}
	name := strings.Repeat("s", pad) + ".sock"
	path := filepath.Join(dir, name)
	if len(path) != maxSocketPath {
		t.Fatalf("%s is %d bytes, want %d", path, len(path), maxSocketPath)
	}
	nc, _ := connect(t, path)
	if _, err := nc.Write([]byte("x")); err != nil {
		t.Errorf("write on a socket at a path of %d bytes: %v", len(path), err)
	}
}

// The runtime directory is the OS's per-user one: $XDG_RUNTIME_DIR on
// Linux, $TMPDIR on macOS, and /tmp/koko-<uid> without it; on Windows,
// %LOCALAPPDATA%\koko\run, and none without LOCALAPPDATA.
func TestTheRuntimeDirectory(t *testing.T) {
	env := map[string]string{"XDG_RUNTIME_DIR": "/run/user/1000", "TMPDIR": "/var/folders/x/T/", "LOCALAPPDATA": `C:\Users\alice\AppData\Local`}
	got, err := runtimeDir(func(k string) string { return env[k] })
	want := map[string]string{"linux": "/run/user/1000/koko", "darwin": "/var/folders/x/T/koko", "windows": `C:\Users\alice\AppData\Local\koko\run`}[runtime.GOOS]
	if err != nil || got != want {
		t.Errorf("runtimeDir = %q, %v, want %q", got, err, want)
	}
	got, err = runtimeDir(func(string) string { return "" })
	if runtime.GOOS == "windows" {
		if err == nil || !strings.Contains(err.Error(), "LOCALAPPDATA is not set") {
			t.Errorf("runtimeDir without LOCALAPPDATA = %q, %v, want it refused", got, err)
		}
		return
	}
	if want := fmt.Sprintf("/tmp/koko-%d", os.Getuid()); err != nil || got != want {
		t.Errorf("runtimeDir without the variable = %q, %v, want %q", got, err, want)
	}
}

// The same-user check admits a connection from a process of this OS user:
// on Linux and macOS by its peer credentials, and on Windows, which gives a
// Unix socket none, every connection, the runtime directory being the
// only gate there (ADR 0051).
func TestTheSameUserCheckAdmitsThisUser(t *testing.T) {
	dir, err := os.MkdirTemp("", "koko-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	_, nc := connect(t, filepath.Join(dir, "s.sock"))
	if err := sameUser(nc); err != nil {
		t.Errorf("sameUser = %v, want this OS user's connection admitted", err)
	}
}

// connect listens on a Unix socket at path and connects to it, and returns
// both ends of the connection, closed when the test ends.
func connect(t *testing.T, path string) (dialled, accepted net.Conn) {
	t.Helper()
	var lc net.ListenConfig
	ln, err := lc.Listen(t.Context(), "unix", path)
	if err != nil {
		t.Fatalf("listen on %s (%d bytes): %v", path, len(path), err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	var d net.Dialer
	dialled, err = d.DialContext(t.Context(), "unix", path)
	if err != nil {
		t.Fatalf("connect to %s (%d bytes): %v", path, len(path), err)
	}
	t.Cleanup(func() { _ = dialled.Close() })
	accepted, err = ln.Accept()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = accepted.Close() })
	return dialled, accepted
}
