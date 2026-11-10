package store

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// snapshotPrefix begins the name of every snapshot in the data directory:
// koko.db.before-<n> holds the database as it was before a migration to
// schema version n, so the older koko that left it so can open it again.
const snapshotPrefix = FileName + ".before-"

// snapshot makes sure the data directory holds koko.db.before-<to>, the
// snapshot that lets the Owner go back to the older koko, before a migration
// step takes the database from schema version from towards version to, and no
// other snapshot: only the latest stays.
//
// A migration that stopped part way (a step failed, then systemd restarted the
// koko server) left the database between two schemas, which no koko left it
// at. The snapshot it took holds a schema older than from and names a target
// beyond from; that snapshot still holds the record as the older koko left
// it, so it is kept, renamed for the migration it now stands before, rather
// than replaced by a copy of the database half migrated. Every step of a
// migration finds its own snapshot so, and keeps it. Any other snapshot is
// replaced.
func (s *Store) snapshot(ctx context.Context, dataDir string, from, to int) error {
	entries, err := os.ReadDir(dataDir)
	if err != nil {
		return fmt.Errorf("list %s: %w", dataDir, err)
	}
	keep := fmt.Sprintf("%s%d", snapshotPrefix, to)
	kept := ""
	for _, e := range entries {
		target, ok := snapshotTarget(e.Name())
		if !ok || target <= from {
			continue
		}
		held, err := userVersionOf(filepath.Join(dataDir, e.Name()))
		if err != nil {
			return fmt.Errorf("read a snapshot: %w; move it out of the data directory to let koko take a new one", err)
		}
		if held < from {
			kept = e.Name()
			break
		}
	}
	switch kept {
	case keep: // an earlier step of this migration took it
	case "":
		if err := s.vacuumInto(ctx, filepath.Join(dataDir, keep)); err != nil {
			return err
		}
	default:
		if err := os.Rename(filepath.Join(dataDir, kept), filepath.Join(dataDir, keep)); err != nil {
			return fmt.Errorf("rename the snapshot %s: %w", kept, err)
		}
		if err := syncPath(dataDir); err != nil {
			return err
		}
	}
	for _, e := range entries {
		if e.Name() != keep && writtenBySnapshot(e.Name()) {
			if err := removeIfPresent(filepath.Join(dataDir, e.Name())); err != nil {
				return fmt.Errorf("remove the older snapshot: %w", err)
			}
		}
	}
	return nil
}

// snapshotFor names the snapshot in the data directory that a koko at schema
// version latest can open, or returns "" when there is none, or when the
// directory or a snapshot cannot be read.
func snapshotFor(dataDir string, latest int) string {
	entries, err := os.ReadDir(dataDir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if _, ok := snapshotTarget(e.Name()); !ok {
			continue
		}
		if held, err := userVersionOf(filepath.Join(dataDir, e.Name())); err == nil && held > 0 && held <= latest {
			return e.Name()
		}
	}
	return ""
}

// snapshotTarget reads the schema version a snapshot's name says its
// migration went to; ok is false for any other file.
func snapshotTarget(name string) (target int, ok bool) {
	rest, ok := strings.CutPrefix(name, snapshotPrefix)
	if !ok {
		return 0, false
	}
	target, err := strconv.Atoi(rest)
	return target, err == nil
}

// writtenBySnapshot tells the files snapshot writes, a snapshot and the copy
// it renames into place, from any other file, such as one of the Owner's own.
func writtenBySnapshot(name string) bool {
	_, ok := snapshotTarget(strings.TrimSuffix(name, ".tmp"))
	return ok
}

// userVersionOf reads the schema version (PRAGMA user_version) from the
// header of the SQLite file at path, without opening it as a database, so no
// -wal or -shm file appears beside it.
func userVersionOf(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer func() { _ = f.Close() }()
	var header [100]byte
	if _, err := io.ReadFull(f, header[:]); err != nil {
		return 0, fmt.Errorf("read the header of %s: %w", path, err)
	}
	if string(header[:16]) != "SQLite format 3\x00" {
		return 0, fmt.Errorf("%s is not an SQLite database", path)
	}
	return int(int32(binary.BigEndian.Uint32(header[60:64]))), nil
}

// vacuumInto writes a copy of the database to path, durably. VACUUM INTO
// syncs nothing it writes, so the copy is written beside path, synced, renamed
// into place and its directory synced, before any migration step commits.
func (s *Store) vacuumInto(ctx context.Context, path string) error {
	tmp := path + ".tmp"
	// A copy a crash cut short would make VACUUM INTO refuse to start.
	if err := removeIfPresent(tmp); err != nil {
		return err
	}
	if _, err := s.write.ExecContext(ctx, "VACUUM INTO ?", tmp); err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := os.Chmod(tmp, fileMode); err != nil {
		return fmt.Errorf("set the mode of %s to %o: %w", tmp, fileMode, err)
	}
	if err := syncPath(tmp); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename %s: %w", tmp, err)
	}
	return syncPath(filepath.Dir(path))
}

// syncPath flushes the file or directory at path to disk.
func syncPath(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("sync: %w", err)
	}
	if err := errors.Join(f.Sync(), f.Close()); err != nil {
		return fmt.Errorf("sync: %w", err)
	}
	return nil
}

// removeIfPresent removes the file at path; one already gone is no error.
func removeIfPresent(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}
