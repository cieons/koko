package store

import (
	"os"
	"path/filepath"
	"testing"
)

// koko.db and the -wal and -shm files SQLite keeps beside it are their
// owner's alone, whatever the umask: a new database is made so, and an
// existing one, and those files left beside it, get the mode at the next
// open.
func TestTheDatabaseIsItsOwnersAlone(t *testing.T) {
	dir := t.TempDir()
	openForMode(t, dir)
	for _, name := range []string{"koko.db", "koko.db-wal", "koko.db-shm"} {
		wantOwnerOnly(t, filepath.Join(dir, name))
	}

	existing := t.TempDir()
	for _, name := range []string{"koko.db", "koko.db-wal"} {
		path := filepath.Join(existing, name)
		if err := os.WriteFile(path, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	openForMode(t, existing)
	for _, name := range []string{"koko.db", "koko.db-wal", "koko.db-shm"} {
		wantOwnerOnly(t, filepath.Join(existing, name))
	}
}

// openForMode opens the store in dir, closed when the test ends.
func openForMode(t *testing.T, dir string) {
	t.Helper()
	s, err := Open(t.Context(), dir)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
}

// wantOwnerOnly fails the test unless the file at path has mode 0600.
func wantOwnerOnly(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("%s has mode %o, want 0600", filepath.Base(path), mode)
	}
}
