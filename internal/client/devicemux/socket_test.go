package devicemux_test

import (
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/cieons/koko/internal/shared/timing"
)

// A socket a Device Mux left behind, as one that crashed leaves it, is
// removed by the next Device Mux of its build, which listens in its place
// and carries commands.
func TestTheNextDeviceMuxReplacesASocketLeftBehind(t *testing.T) {
	dir, err := os.MkdirTemp("", "koko-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	socket := filepath.Join(dir, "mux.sock")
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: socket, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	ln.SetUnlinkOnClose(false)
	_ = ln.Close()
	if _, err := os.Lstat(socket); err != nil {
		t.Fatalf("no socket was left behind: %v", err)
	}

	srv := startReporter(t, nil)
	home, _ := runMuxAt(t, srv, timing.Timings{}, socket)
	waitReport(t, dial(t, socket, home), "v0.0.1")
}
