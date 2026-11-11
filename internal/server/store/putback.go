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
// koko.db<aside>, such as koko.db.failed-v0.5.0, and renames the snapshot
// the migration took, koko.db.before-<n>, into its place, when this binary
// opens it. It touches the data directory by rename
// only, and returns the snapshot it put back, or "" when koko.db's schema is
// not newer than this binary's, or there is no koko.db. Nothing may have
// koko.db open.
func PutBack(ctx context.Context, dataDir, aside string) (string, error) {
	version := 0
	err := Peek(ctx, dataDir, func(tx Tx) error {
		return tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version)
	})
	if err != nil {
		return "", err
	}
	latest := SchemaVersion()
	if version <= latest {
		return "", nil
	}
	// The migration to version took this snapshot, before its first step.
	snapshot := fmt.Sprintf("%s%d", snapshotPrefix, version)
	held, err := userVersionOf(filepath.Join(dataDir, snapshot))
	if err != nil {
		return "", fmt.Errorf("%s is at schema version %d, which this koko (schema version %d) predates, and its snapshot cannot be read: %w",
			FileName, version, latest, err)
	}
	if held < 1 || held > latest {
		return "", fmt.Errorf("%s is at schema version %d, which this koko (schema version %d) predates, and its snapshot %s is at schema version %d, which this koko does not open",
			FileName, version, latest, snapshot, held)
	}
	db := filepath.Join(dataDir, FileName)
	moved := db + aside
	for _, suffix := range []string{"-wal", "-shm"} {
		if _, err := os.Lstat(moved + suffix); err == nil {
			return "", fmt.Errorf("move %s aside: %s%s is there already", FileName, filepath.Base(moved), suffix)
		}
	}
	if _, err := os.Lstat(moved); err == nil {
		return "", fmt.Errorf("move %s aside: %s is there already", FileName, filepath.Base(moved))
	}
	// The snapshot comes last, once nothing of the newer database is left
	// beside it: SQLite would read a -wal it found there as its own.
	for _, suffix := range []string{"", "-wal", "-shm"} {
		err := os.Rename(db+suffix, moved+suffix)
		// koko.db must be there; its -wal and -shm need not.
		if err != nil && (suffix == "" || !errors.Is(err, fs.ErrNotExist)) {
			return "", fmt.Errorf("move %s%s aside: %w", FileName, suffix, err)
		}
	}
	if err := os.Rename(filepath.Join(dataDir, snapshot), db); err != nil {
		return "", fmt.Errorf("put the snapshot %s in place of %s: %w", snapshot, FileName, err)
	}
	if err := syncPath(dataDir); err != nil {
		return "", err
	}
	return snapshot, nil
}
