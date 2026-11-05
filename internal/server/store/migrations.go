package store

import (
	"context"
	"embed"
	"fmt"
)

// steps is the schema: every forward migration step, in landing order. A
// ticket adds its own file to migrations/ and one line at the end of this
// list, and never edits or reorders a step that has landed. On a rebase
// conflict, keep both lines in landing order. A step's position is its schema
// version, recorded in PRAGMA user_version.
var steps = []string{
	"spaces.sql",
}

//go:embed migrations/*.sql
var migrations embed.FS

// SchemaVersion is the schema version this binary migrates to.
func SchemaVersion() int {
	return len(steps)
}

// migrate applies every step past the database's user_version, each in its
// own IMMEDIATE transaction, which reads user_version and records the new one,
// so two processes opening the same file never apply a step twice.
func (s *Store) migrate(ctx context.Context) error {
	for {
		done := false
		err := s.Write(ctx, func(tx Tx) error {
			var version int
			if err := tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
				return fmt.Errorf("read schema version: %w", err)
			}
			if version >= len(steps) {
				done = true
				return nil
			}
			step := steps[version]
			sql, err := migrations.ReadFile("migrations/" + step)
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
