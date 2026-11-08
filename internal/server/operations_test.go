package server

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/cieons/koko/internal/server/seal"
	"github.com/cieons/koko/internal/shared/api"
)

// The koko server serves exactly the operations api.Operations lists, so
// every operation it registers is in the web UI's types and has its rows in
// the access matrix, which walks that list (package access).
func TestTheServerServesExactlyTheDeclaredOperations(t *testing.T) {
	keyFile := filepath.Join(t.TempDir(), "root.key")
	if err := os.WriteFile(keyFile, seal.NewRootKey(), 0o600); err != nil {
		t.Fatal(err)
	}
	// A socket path short enough for macOS's 104 bytes.
	socketDir, err := os.MkdirTemp("", "koko-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(socketDir) })
	srv, err := Start(t.Context(), Config{
		DataDir:     t.TempDir(),
		RootKeyPath: keyFile,
		Listen:      "127.0.0.1:0",
		LogLevel:    slog.LevelError,
		Log:         io.Discard,
		Filesystems: localDisk{},
		SocketPath:  filepath.Join(socketDir, "koko.sock"),
	})
	if err != nil {
		t.Fatalf("start the koko server: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close() })

	var declared []string
	for _, o := range api.Operations() {
		declared = append(declared, o.Info().Name)
	}
	slices.Sort(declared)
	if served := srv.api.Names(); !slices.Equal(served, declared) {
		t.Errorf("the koko server serves %q, want what api.Operations lists: %q", served, declared)
	}
}

// localDisk is a filesystem.Types on which every path that exists is on
// ext4, so that a koko server a test starts reads no real /proc. It is
// harness.LocalDisk, which this package's tests cannot import, since the
// harness imports this package.
type localDisk struct{}

func (localDisk) FilesystemType(path string) (string, error) {
	if _, err := os.Stat(path); err != nil {
		return "", err
	}
	return "ext4", nil
}
