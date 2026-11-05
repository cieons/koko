package store

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"path"
)

// steps is the schema: every forward migration step, in landing order. A
// ticket adds its own file to migrations/ and one line at the end of this
// list, and never edits or reorders a step that has landed. On a rebase
// conflict, keep both lines in landing order. A step's position is its schema
// version, recorded in PRAGMA user_version.
var steps = []string{
	"spaces.sql",
	"users.sql",
	"deployment.sql",
	"join_codes.sql",
}

//go:embed migrations/*.sql
var migrations embed.FS

// SchemaVersion is the schema version this binary migrates to.
func SchemaVersion() int {
	return len(steps)
}

// schema is a list of forward migration steps, each a file under migrations/
// in files. A step's position is its schema version.
type schema struct {
	files fs.FS
	steps []string
}

// kokoSchema is this binary's schema.
var kokoSchema = schema{files: migrations, steps: steps}

// upgrade brings the database to the schema sc, one step per IMMEDIATE
// transaction. Each transaction reads user_version and records the new one,
// so two processes opening the same file never apply a step twice.
//
//   - A database newer than sc is refused and left as it was: there are no
//     down-migrations.
//   - An older one is snapshot first. Each step's transaction makes sure of the
//     snapshot under its write lock, so two processes starting together never
//     snapshot at once, and the one that waited finds the snapshot taken (see
//     snapshot).
//   - A new one (version 0) holds nothing to go back to, so it gets no
//     snapshot.
func (s *Store) upgrade(ctx context.Context, dataDir string, sc schema) error {
	latest := len(sc.steps)
	for {
		done := false
		err := s.Write(ctx, func(tx Tx) error {
			var version int
			if err := tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
				return fmt.Errorf("read the schema version: %w", err)
			}
			switch {
			case version > latest:
				return refusal(dataDir, version, latest)
			case version == latest:
				done = true
				return nil
			case version > 0:
				// VACUUM INTO runs on another connection, which WAL lets
				// read while this transaction holds the write lock, having
				// written nothing yet.
				if err := s.snapshot(ctx, dataDir, version, latest); err != nil {
					return fmt.Errorf("snapshot before migrating: %w", err)
				}
			}
			step := sc.steps[version]
			sql, err := fs.ReadFile(sc.files, path.Join("migrations", step))
			if err != nil {
				return fmt.Errorf("read step %s: %w", step, err)
			}
			if _, err := tx.ExecContext(ctx, string(sql)); err != nil {
				return fmt.Errorf("apply step %d (%s): %w", version+1, step, err)
			}
			// PRAGMA takes no bound parameters; the version is an int.
			_, err = tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", version+1))
			return err
		})
		if err != nil || done {
			return err
		}
	}
}

// refusal is why a koko at schema version latest refuses a database at the
// newer schema version. It names the snapshot to put in place of koko.db to
// go back, when the data directory holds one this koko can open.
func refusal(dataDir string, version, latest int) error {
	msg := fmt.Sprintf("it is at schema version %d, which this koko (schema version %d) predates: "+
		"run the newer koko that migrated it", version, latest)
	if name := snapshotFor(dataDir, latest); name != "" {
		msg += fmt.Sprintf(", or go back by putting %s in place of %s", name, FileName)
	}
	return errors.New(msg)
}
