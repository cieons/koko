package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// sqliteAt writes an SQLite file at path at schema version version.
func sqliteAt(t *testing.T, path string, version int) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.ExecContext(t.Context(), fmt.Sprintf("CREATE TABLE note (body TEXT); PRAGMA user_version = %d", version)); err != nil {
		t.Fatal(err)
	}
}

// The upgrade helper's put-back moves a koko.db a newer koko migrated aside
// and renames into its place the snapshot that migration took: named for
// the version the migration went to, which koko.db reached, or not when the
// migration stopped part way. An older snapshot left beside it is not
// taken, and a name an earlier failure holds is not reused.
func TestPutBackTakesTheSnapshotOfTheMigration(t *testing.T) {
	t.Parallel()
	latest := SchemaVersion()
	for _, tc := range []struct {
		name   string
		target int
	}{
		{"a migration that ended", latest + 1},
		{"a migration that stopped part way", latest + 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			sqliteAt(t, filepath.Join(dir, FileName), latest+1)
			sqliteAt(t, filepath.Join(dir, fmt.Sprintf("%s.before-%d", FileName, latest)), 1)
			snapshot := fmt.Sprintf("%s.before-%d", FileName, tc.target)
			sqliteAt(t, filepath.Join(dir, snapshot), latest)
			if err := os.WriteFile(filepath.Join(dir, FileName+".failed-v0.5.0"), []byte("an earlier failure"), 0o600); err != nil {
				t.Fatal(err)
			}

			got, moved, err := PutBack(t.Context(), dir, ".failed-v0.5.0")
			if err != nil {
				t.Fatal(err)
			}
			if got != snapshot || moved != FileName+".failed-v0.5.0.2" {
				t.Errorf("PutBack put back %q and moved koko.db to %q, want %q and koko.db.failed-v0.5.0.2", got, moved, snapshot)
			}
			for file, want := range map[string]int{FileName: latest, moved: latest + 1} {
				if v, err := userVersionOf(filepath.Join(dir, file)); err != nil || v != want {
					t.Errorf("%s is at schema version %d (%v), want %d", file, v, err, want)
				}
			}
			if _, err := os.Stat(filepath.Join(dir, snapshot)); !os.IsNotExist(err) {
				t.Errorf("the snapshot is still there: %v", err)
			}
		})
	}
	t.Run("the schema did not move", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		sqliteAt(t, filepath.Join(dir, FileName), latest)
		if got, moved, err := PutBack(t.Context(), dir, ".failed-v0.5.0"); err != nil || got != "" || moved != "" {
			t.Errorf("PutBack = %q, %q, %v; want nothing done", got, moved, err)
		}
	})
}
