package kokod_test

import (
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/cieons/koko/internal/harness"
)

// kokodRunWith runs `kokod run --json` on dataDir with a new Root Key
// file, through the koko server's real start-up with opts.
func kokodRunWith(t *testing.T, dataDir string, opts ...harness.Option) harness.Result {
	t.Helper()
	keyFile := filepath.Join(t.TempDir(), "root.key")
	newKeyFile(t, keyFile)
	return harness.RunKokod(t, harness.Kokod(t, opts...), nil,
		"run", "--json", "--data-dir", dataDir, "--root-key-file", keyFile)
}

// staleSocket leaves a socket file at path that nobody answers on, as a koko
// server killed without warning leaves its own.
func staleSocket(t *testing.T, path string) {
	t.Helper()
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	ln.SetUnlinkOnClose(false)
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
}

// A socket file nobody answers on, as a koko server killed without warning
// leaves behind, is taken over, and the koko server serves on it (P1 spec:
// the container image, story 21).
func TestRunTakesOverAStaleSocket(t *testing.T) {
	t.Parallel()
	socket := filepath.Join(harness.ShortTempDir(t), "koko.sock")
	staleSocket(t, socket)
	r := kokodRunWith(t, t.TempDir(), harness.SocketPath(socket))
	if r.Status != 0 || !strings.Contains(r.Stderr, "removed a stale socket") || !strings.Contains(r.Stderr, "socket="+socket) {
		t.Fatalf("kokod run on a stale socket: exit status %d, stderr %q", r.Status, r.Stderr)
	}
	// The same start-up serves on it, as the koko server it starts would.
	staleSocket(t, socket)
	h := harness.Start(t, harness.SocketPath(socket))
	if r := h.Koko("server", "show"); r.Status != 0 {
		t.Fatalf("server show on the socket taken over: exit status %d, stderr %q", r.Status, r.Stderr)
	}
	if !strings.Contains(h.Log(), "removed a stale socket") {
		t.Errorf("the log does not say the stale socket was removed:\n%s", h.Log())
	}
}

// A second start beside a running koko server, on the same socket path,
// refuses naming the path, before it writes anything, and the running one
// keeps serving.
func TestRunRefusesToStartBesideARunningKokoServer(t *testing.T) {
	t.Parallel()
	socket := filepath.Join(harness.ShortTempDir(t), "koko.sock")
	h := harness.Start(t, harness.SocketPath(socket))
	dataDir := filepath.Join(t.TempDir(), "data")
	msg := kokodRunWith(t, dataDir, harness.SocketPath(socket)).WantError(t, 1, "port_taken")
	if !strings.Contains(msg, socket) || !strings.Contains(msg, "answers") {
		t.Errorf("message %q does not say something answers on %s", msg, socket)
	}
	if _, err := os.Stat(dataDir); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the refused start made its data directory: %v", err)
	}
	if r := h.Koko("server", "show"); r.Status != 0 {
		t.Errorf("the running koko server stopped serving: exit status %d, stderr %q", r.Status, r.Stderr)
	}
}

// A file at the socket's path that is not a socket is never taken for a
// stale one: start-up refuses and leaves it as it was.
func TestRunRefusesAFileThatIsNotASocketAtTheSocketPath(t *testing.T) {
	t.Parallel()
	socket := filepath.Join(harness.ShortTempDir(t), "koko.sock")
	if err := os.WriteFile(socket, []byte("not a socket\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	msg := kokodRunWith(t, t.TempDir(), harness.SocketPath(socket)).WantError(t, 1, "internal_error")
	if !strings.Contains(msg, socket) || !strings.Contains(msg, "not a socket") {
		t.Errorf("message %q does not say %s is not a socket", msg, socket)
	}
	if b, err := os.ReadFile(socket); err != nil || string(b) != "not a socket\n" {
		t.Errorf("the file at the socket path changed: %q, %v", b, err)
	}
}

// A data directory on a network filesystem, as the mount table names it, is
// refused at start before anything is written, as the installer refuses it,
// whether it exists or would be made there (P1 spec: the container image,
// story 10). One on a local filesystem in the same table starts.
func TestRunRefusesADataDirectoryOnANetworkFilesystem(t *testing.T) {
	t.Parallel()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	remote, local := filepath.Join(root, "remote"), filepath.Join(root, "local")
	for _, dir := range []string{remote, local} {
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	mountinfo := filepath.Join(t.TempDir(), "mountinfo")
	table := fmt.Sprintf("22 1 252:1 / / rw,relatime shared:1 - ext4 /dev/vda1 rw\n"+
		"40 22 0:35 / %s rw,relatime shared:20 - nfs4 10.0.0.2:/export/koko rw,vers=4.2\n", remote)
	if err := os.WriteFile(mountinfo, []byte(table), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, dataDir := range []string{remote, filepath.Join(remote, "data")} {
		t.Run(filepath.Base(dataDir), func(t *testing.T) {
			before := files(t, root)
			msg := kokodRunWith(t, dataDir, harness.Mountinfo(mountinfo)).WantError(t, 1, "unsupported_platform")
			for _, want := range []string{dataDir, "nfs4", "network filesystem"} {
				if !strings.Contains(msg, want) {
					t.Errorf("message %q does not name %q", msg, want)
				}
			}
			if after := files(t, root); !slices.Equal(after, before) {
				t.Errorf("the refused start wrote files: %v, before %v", after, before)
			}
		})
	}
	if r := kokodRunWith(t, filepath.Join(local, "data"), harness.Mountinfo(mountinfo)); r.Status != 0 {
		t.Errorf("a data directory on ext4 was refused: exit status %d, stderr %q", r.Status, r.Stderr)
	}
}

// A data directory start-up cannot write, and the parent of a missing one,
// are refused before anything is written, naming the directory and the uid
// and gid the koko server runs as (P1 spec: the container image, story 9).
func TestRunRefusesADataDirectoryItCannotWrite(t *testing.T) {
	t.Parallel()
	if os.Geteuid() == 0 {
		t.Skip("root writes a read-only directory")
	}
	root := t.TempDir()
	readOnly := filepath.Join(root, "read-only")
	if err := os.Mkdir(readOnly, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(readOnly, 0o700) })
	ids := fmt.Sprintf("uid %d and gid %d", os.Geteuid(), os.Getegid())
	for _, tc := range []struct {
		name, dataDir string
		names         []string
	}{
		{"the data directory", readOnly, []string{"the data directory " + readOnly, ids}},
		{"the parent of a missing one", filepath.Join(readOnly, "data"), []string{readOnly + ", where it would make the data directory " + filepath.Join(readOnly, "data"), ids}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := files(t, root)
			msg := kokodRunWith(t, tc.dataDir).WantError(t, 1, "internal_error")
			for _, want := range tc.names {
				if !strings.Contains(msg, want) {
					t.Errorf("message %q does not name %q", msg, want)
				}
			}
			if after := files(t, root); !slices.Equal(after, before) {
				t.Errorf("the refused start wrote files: %v, before %v", after, before)
			}
		})
	}
}

// kokod run refuses to run as root, wherever it runs, when its real,
// effective or saved uid is 0, before anything is written, naming the fix
// (ADR 0039). Started as another user, it starts as before.
func TestRunRefusesRoot(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name                   string
		real, effective, saved int
		ids                    string
	}{
		{"real uid 0", 0, 1000, 1000, "uid are 0, 1000 and 1000"},
		{"effective uid 0", 1000, 0, 1000, "uid are 1000, 0 and 1000"},
		{"saved uid 0", 1000, 1000, 0, "uid are 1000, 1000 and 0"},
		{"all three 0", 0, 0, 0, "uid are 0, 0 and 0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			socket := filepath.Join(harness.ShortTempDir(t), "koko.sock")
			before := files(t, root)
			msg := kokodRunWith(t, filepath.Join(root, "data"), harness.SocketPath(socket), harness.ProcessUIDs(tc.real, tc.effective, tc.saved)).
				WantError(t, 2, "invalid_usage")
			for _, want := range []string{"kokod run does not run as root", tc.ids, "a user who is not root", "uid 65532", "--user 0"} {
				if !strings.Contains(msg, want) {
					t.Errorf("message %q does not say %q", msg, want)
				}
			}
			if after := files(t, root); !slices.Equal(after, before) {
				t.Errorf("the refused start wrote files: %v, before %v", after, before)
			}
			if _, err := os.Lstat(socket); !errors.Is(err, fs.ErrNotExist) {
				t.Errorf("the refused start left something at its socket: %v", err)
			}
		})
	}
	// Before every other check: root is told it is root even with a Root
	// Key path that is not absolute.
	msg := harness.RunKokod(t, harness.Kokod(t, harness.ProcessUIDs(0, 0, 0)), nil,
		"run", "--json", "--data-dir", t.TempDir(), "--root-key-file", "root.key").WantError(t, 2, "invalid_usage")
	if !strings.Contains(msg, "does not run as root") {
		t.Errorf("root with a relative Root Key path: message %q does not say it does not run as root", msg)
	}
	if r := kokodRunWith(t, filepath.Join(t.TempDir(), "data"), harness.ProcessUIDs(65532, 65532, 65532)); r.Status != 0 {
		t.Errorf("started as uid 65532: exit status %d, stderr %q", r.Status, r.Stderr)
	}
}
