package store

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// PutBack puts koko.db in dataDir back as this binary left it, after a
// newer koko migrated it and never served, as when the upgrade helper puts
// the previous kokod back (ADR 0048): when koko.db is at a schema newer than
// this binary's, it moves koko.db, with its -wal and -shm, aside as
// koko.db<aside>, such as koko.db.failed-v0.5.0, or koko.db<aside>.2 and on
// when an earlier failure holds that name, and renames the snapshot the
// migration took, koko.db.before-<n>, into its place, when this binary
// opens it. It touches the data directory by rename only, and returns the
// snapshot it put back and the name it moved koko.db to, or "" and "" when
// koko.db's schema is not newer than this binary's, or there is no koko.db.
// Nothing may have koko.db open.
func PutBack(ctx context.Context, dataDir, aside string) (snapshot, moved string, err error) {
	version := 0
	err = Peek(ctx, dataDir, func(tx Tx) error {
		return tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version)
	})
	if err != nil {
		return "", "", err
	}
	latest := SchemaVersion()
	if version <= latest {
		return "", "", nil
	}
	snapshot, err = snapshotBefore(dataDir, version, latest)
	if err != nil {
		return "", "", err
	}
	moved, err = freeName(dataDir, FileName+aside)
	if err != nil {
		return "", "", err
	}
	// The -wal and -shm go first and koko.db last, so that a failure between
	// two renames leaves koko.db in place, which the previous koko refuses
	// to start on, never a data directory without one. The snapshot comes
	// once nothing of the newer database is left beside it: SQLite would
	// read a -wal it found there as its own.
	db := filepath.Join(dataDir, FileName)
	for _, suffix := range []string{"-wal", "-shm", ""} {
		err := os.Rename(db+suffix, filepath.Join(dataDir, moved+suffix))
		// koko.db must be there; its -wal and -shm need not.
		if err != nil && (suffix == "" || !errors.Is(err, fs.ErrNotExist)) {
			return "", "", fmt.Errorf("move %s%s aside: %w", FileName, suffix, err)
		}
	}
	if err := os.Rename(filepath.Join(dataDir, snapshot), db); err != nil {
		return "", "", fmt.Errorf("put the snapshot %s in place of %s: %w", snapshot, FileName, err)
	}
	if err := syncPath(dataDir); err != nil {
		return "", "", err
	}
	return snapshot, moved, nil
}

// snapshotBefore names the snapshot the migration that took koko.db to
// version took, which a koko at schema version latest opens: the migration
// names it after the version it went to, koko.db.before-<n>, n at least
// version, as one that stopped part way left koko.db short of it.
func snapshotBefore(dataDir string, version, latest int) (string, error) {
	entries, err := os.ReadDir(dataDir)
	if err != nil {
		return "", fmt.Errorf("list %s: %w", dataDir, err)
	}
	for _, e := range entries {
		target, ok := snapshotTarget(e.Name())
		if !ok || target < version {
			continue
		}
		held, err := userVersionOf(filepath.Join(dataDir, e.Name()))
		if err != nil {
			return "", fmt.Errorf("%s is at schema version %d, which this koko (schema version %d) predates, and its snapshot %s cannot be read: %w",
				FileName, version, latest, e.Name(), err)
		}
		if held >= 1 && held <= latest {
			return e.Name(), nil
		}
	}
	return "", fmt.Errorf("%s is at schema version %d, which this koko (schema version %d) predates, and %s holds no snapshot of the migration to it that this koko opens",
		FileName, version, latest, dataDir)
}

// freeName returns name, or name.2, name.3 and on, the first that neither it
// nor its -wal or -shm is in dataDir.
func freeName(dataDir, name string) (string, error) {
	for n := 1; n <= 100; n++ {
		candidate := name
		if n > 1 {
			candidate = fmt.Sprintf("%s.%d", name, n)
		}
		taken := false
		for _, suffix := range []string{"", "-wal", "-shm"} {
			switch _, err := os.Lstat(filepath.Join(dataDir, candidate+suffix)); {
			case err == nil:
				taken = true
			case !errors.Is(err, fs.ErrNotExist):
				return "", fmt.Errorf("look for %s%s: %w", candidate, suffix, err)
			}
		}
		if !taken {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("move %s aside: %s and the next 99 names after it are taken", FileName, name)
}
