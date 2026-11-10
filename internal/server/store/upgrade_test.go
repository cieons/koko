package store

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
)

// notes stands in for koko's own steps, so the tests can play an older and a
// newer koko: as far as its database goes, a koko is its list of steps.
var notes = fstest.MapFS{
	"migrations/notes.sql":  {Data: []byte("CREATE TABLE notes (body TEXT NOT NULL) STRICT;")},
	"migrations/tags.sql":   {Data: []byte("CREATE TABLE tags (name TEXT NOT NULL) STRICT;")},
	"migrations/pins.sql":   {Data: []byte("ALTER TABLE notes ADD COLUMN pinned INTEGER NOT NULL DEFAULT 0;")},
	"migrations/stars.sql":  {Data: []byte("ALTER TABLE notes ADD COLUMN stars INTEGER NOT NULL DEFAULT 0;")},
	"migrations/broken.sql": {Data: []byte("ALTER TABLE missing ADD COLUMN x INTEGER;")},
}

var (
	kokoV1 = schema{files: notes, steps: []string{"notes.sql"}}
	kokoV2 = schema{files: notes, steps: []string{"notes.sql", "tags.sql", "pins.sql"}}
	kokoV3 = schema{files: notes, steps: []string{"notes.sql", "tags.sql", "pins.sql", "stars.sql"}}
	// kokoV2Broken is a release whose third step fails on every database.
	kokoV2Broken = schema{files: notes, steps: []string{"notes.sql", "tags.sql", "broken.sql"}}
)

// start opens dir as a koko with the schema sc does when it starts. The store
// closes when the test ends, if the test has not stopped it before.
func start(t *testing.T, sc schema, dir string) *Store {
	t.Helper()
	s, err := open(t.Context(), dir, sc)
	if err != nil {
		t.Fatalf("start a koko at schema version %d: %v", len(sc.steps), err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// refused opens dir as a koko with the schema sc and returns why it refused
// to start, failing the test when it started.
func refused(t *testing.T, sc schema, dir string) error {
	t.Helper()
	s, err := open(t.Context(), dir, sc)
	if err == nil {
		_ = s.Close()
		t.Fatalf("a koko at schema version %d started on %s, want it refused", len(sc.steps), dir)
	}
	return err
}

func stop(t *testing.T, s *Store) {
	t.Helper()
	if err := s.Close(); err != nil {
		t.Fatalf("stop: %v", err)
	}
}

func addNote(t *testing.T, s *Store, body string) {
	t.Helper()
	err := s.Write(t.Context(), func(tx Tx) error {
		_, err := tx.ExecContext(t.Context(), "INSERT INTO notes (body) VALUES (?)", body)
		return err
	})
	if err != nil {
		t.Fatalf("add note %q: %v", body, err)
	}
}

func notesIn(t *testing.T, s *Store) []string {
	t.Helper()
	var bodies []string
	err := s.Read(t.Context(), func(tx Tx) error {
		rows, err := tx.QueryContext(t.Context(), "SELECT body FROM notes ORDER BY body")
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var body string
			if err := rows.Scan(&body); err != nil {
				return err
			}
			bodies = append(bodies, body)
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatalf("read notes: %v", err)
	}
	return bodies
}

func schemaVersionOf(t *testing.T, s *Store) int {
	t.Helper()
	var version int
	err := s.Read(t.Context(), func(tx Tx) error {
		return tx.QueryRowContext(t.Context(), "PRAGMA user_version").Scan(&version)
	})
	if err != nil {
		t.Fatalf("read user_version: %v", err)
	}
	return version
}

// snapshots lists the files in the data directory dir whose names begin as a
// snapshot's do.
func snapshots(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "koko.db.before-") {
			names = append(names, e.Name())
		}
	}
	return names
}

func wantSnapshots(t *testing.T, dir string, want ...string) {
	t.Helper()
	if got := snapshots(t, dir); !slices.Equal(got, want) {
		t.Fatalf("snapshots in the data directory = %q, want %q", got, want)
	}
}

// goBack puts a copy of the snapshot in place of koko.db in dir, as the Owner
// does to return to the older koko: the newer koko's -wal and -shm files go
// with its koko.db. The snapshot stays where it is.
func goBack(t *testing.T, snapshot, dir string) {
	t.Helper()
	for _, sidecar := range []string{"koko.db-wal", "koko.db-shm"} {
		if err := os.Remove(filepath.Join(dir, sidecar)); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
	}
	src, err := os.Open(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = src.Close() }()
	dst, err := os.Create(filepath.Join(dir, "koko.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(dst, src); err != nil {
		t.Fatal(err)
	}
	if err := dst.Close(); err != nil {
		t.Fatal(err)
	}
}

// openUnder starts the koko with schema sc on a copy of the snapshot in a
// data directory of its own, as the Owner does after going back to it.
func openUnder(t *testing.T, sc schema, snapshot string) *Store {
	t.Helper()
	dir := t.TempDir()
	goBack(t, snapshot, dir)
	s := start(t, sc, dir)
	wantSnapshots(t, dir) // it needed no migration, so it took no snapshot
	return s
}

// An upgrade snapshots the older database, then migrates it; the snapshot
// opens under the koko it came from and holds the record as that koko left
// it.
func TestAnOlderSchemaIsSnapshotThenMigrated(t *testing.T) {
	// The data directory may hold characters that mean something in a URI.
	dir := filepath.Join(t.TempDir(), "a b?c#d%20e")

	s := start(t, kokoV1, dir)
	wantSnapshots(t, dir) // a new database holds nothing to go back to
	addNote(t, s, "first")
	stop(t, s)

	s = start(t, kokoV2, dir)
	if got := schemaVersionOf(t, s); got != 3 {
		t.Errorf("user_version after the upgrade = %d, want 3", got)
	}
	wantSnapshots(t, dir, "koko.db.before-3")
	wantOwnerOnly(t, filepath.Join(dir, "koko.db.before-3"))
	addNote(t, s, "second")
	stop(t, s)

	// A start with nothing to migrate leaves the snapshot as it was.
	stop(t, start(t, kokoV2, dir))
	wantSnapshots(t, dir, "koko.db.before-3")

	old := openUnder(t, kokoV1, filepath.Join(dir, "koko.db.before-3"))
	if got := schemaVersionOf(t, old); got != 1 {
		t.Errorf("the snapshot's user_version = %d, want 1", got)
	}
	if got := notesIn(t, old); !slices.Equal(got, []string{"first"}) {
		t.Errorf("the snapshot holds notes %q, want only the one written before the upgrade", got)
	}
}

// Every older schema of koko's own steps gets its snapshot and migrates to
// this binary's, and the snapshot opens under the koko it came from. It covers
// each step as it lands; while koko has one step, there is no older schema.
func TestEveryOlderKokoSchemaIsSnapshotThenMigrated(t *testing.T) {
	latest := len(kokoSchema.steps)
	for k := 1; k < latest; k++ {
		t.Run(fmt.Sprintf("from schema version %d", k), func(t *testing.T) {
			older := schema{files: kokoSchema.files, steps: kokoSchema.steps[:k]}
			dir := t.TempDir()
			stop(t, start(t, older, dir))

			s := start(t, kokoSchema, dir)
			if got := schemaVersionOf(t, s); got != latest {
				t.Errorf("user_version after the upgrade = %d, want %d", got, latest)
			}
			snapshot := fmt.Sprintf("koko.db.before-%d", latest)
			wantSnapshots(t, dir, snapshot)
			if got := schemaVersionOf(t, openUnder(t, older, filepath.Join(dir, snapshot))); got != k {
				t.Errorf("the snapshot's user_version = %d, want %d", got, k)
			}
		})
	}
}

// Each upgrade replaces the snapshot of the one before: only the latest stays.
// koko removes only the files it writes, so a file of the Owner's own whose
// name begins the same way stays.
func TestOnlyTheLatestSnapshotIsKept(t *testing.T) {
	dir := t.TempDir()
	s := start(t, kokoV1, dir)
	addNote(t, s, "first")
	stop(t, s)
	for _, name := range []string{
		"koko.db.before-3.tmp", // a copy a crash cut short
		"koko.db.before-2.bak", // the Owner's own
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("cut short"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	stop(t, start(t, kokoV2, dir))
	wantSnapshots(t, dir, "koko.db.before-2.bak", "koko.db.before-3")

	stop(t, start(t, kokoV3, dir))
	wantSnapshots(t, dir, "koko.db.before-2.bak", "koko.db.before-4")
	old := openUnder(t, kokoV2, filepath.Join(dir, "koko.db.before-4"))
	if got := schemaVersionOf(t, old); got != 3 {
		t.Errorf("the latest snapshot's user_version = %d, want 3", got)
	}
}

// Two koko servers starting together on an older database (systemd's and
// one run by hand) migrate it once and leave one snapshot, of the database as
// the older koko left it.
func TestStartingTogetherSnapshotsTheOlderDatabaseOnce(t *testing.T) {
	for range 10 {
		dir := t.TempDir()
		s := start(t, kokoV1, dir)
		addNote(t, s, "first")
		stop(t, s)

		var wg sync.WaitGroup
		errs := make(chan error, 4)
		for range cap(errs) {
			wg.Go(func() {
				s, err := open(t.Context(), dir, kokoV3)
				if err == nil {
					err = s.Close()
				}
				errs <- err
			})
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatalf("a koko server starting beside another: %v", err)
			}
		}
		wantSnapshots(t, dir, "koko.db.before-4")
		old := openUnder(t, kokoV1, filepath.Join(dir, "koko.db.before-4"))
		if got := notesIn(t, old); !slices.Equal(got, []string{"first"}) {
			t.Fatalf("the snapshot holds notes %q, want %q", got, []string{"first"})
		}
	}
}

// A koko older than the database refuses it, naming the schema version it
// needs, and leaves it as it was: there are no down-migrations. The refusal
// names the snapshot to go back to when this koko can open it.
func TestANewerSchemaIsRefused(t *testing.T) {
	dir := t.TempDir()
	s := start(t, kokoV1, dir)
	addNote(t, s, "first")
	stop(t, s)
	stop(t, start(t, kokoV2, dir))

	err := refused(t, kokoV1, dir)
	for _, want := range []string{
		"schema version 3",
		"schema version 1",
		"go back by putting koko.db.before-3 in place of koko.db",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal %q does not say %q", err, want)
		}
	}
	s = start(t, kokoV2, dir)
	if got := schemaVersionOf(t, s); got != 3 {
		t.Errorf("user_version after the refusal = %d, want 3", got)
	}
	if got := notesIn(t, s); !slices.Equal(got, []string{"first"}) {
		t.Errorf("notes after the refusal = %q, want %q", got, []string{"first"})
	}
	stop(t, s)
	wantSnapshots(t, dir, "koko.db.before-3")

	// The snapshot of the next upgrade is newer than kokoV1 too.
	stop(t, start(t, kokoV3, dir))
	err = refused(t, kokoV1, dir)
	if !strings.Contains(err.Error(), "schema version 4") || strings.Contains(err.Error(), "koko.db.before-") {
		t.Errorf("refusal %q, want it to name schema version 4 and no snapshot", err)
	}
}

// Each step commits on its own, so a failing step keeps the steps before it.
// The snapshot taken before them still holds the record as the older koko
// left it, so the next start keeps it rather than snapshotting the database
// half migrated, even when that start is a fixed koko with more steps: the
// snapshot then takes the name of the migration it now stands before.
func TestAFailedStepKeepsTheStepsBeforeItAndTheSnapshot(t *testing.T) {
	dir := t.TempDir()
	s := start(t, kokoV1, dir)
	addNote(t, s, "first")
	stop(t, s)

	for range 2 { // systemd restarts a koko server that fails to start
		if err := refused(t, kokoV2Broken, dir); !strings.Contains(err.Error(), "broken.sql") {
			t.Fatalf("a failing step: open returned %v, want an error naming broken.sql", err)
		}
	}
	s = start(t, schema{files: notes, steps: kokoV2Broken.steps[:2]}, dir)
	if got := schemaVersionOf(t, s); got != 2 {
		t.Errorf("user_version after step 3 failed = %d, want 2", got)
	}
	stop(t, s)
	wantSnapshots(t, dir, "koko.db.before-3")

	s = start(t, kokoV3, dir)
	if got := schemaVersionOf(t, s); got != 4 {
		t.Errorf("user_version after the fixed upgrade = %d, want 4", got)
	}
	stop(t, s)
	wantSnapshots(t, dir, "koko.db.before-4")
	old := openUnder(t, kokoV1, filepath.Join(dir, "koko.db.before-4"))
	if got := notesIn(t, old); !slices.Equal(got, []string{"first"}) {
		t.Errorf("the snapshot holds notes %q, want %q", got, []string{"first"})
	}
}

// After going back by copying the snapshot in place, the older koko writes on;
// upgrading again snapshots the record afresh, those writes included, rather
// than keeping the copy of the record from before.
func TestUpgradingAgainAfterGoingBackSnapshotsAfresh(t *testing.T) {
	dir := t.TempDir()
	s := start(t, kokoV1, dir)
	addNote(t, s, "first")
	stop(t, s)
	stop(t, start(t, kokoV2, dir))

	goBack(t, filepath.Join(dir, "koko.db.before-3"), dir)
	s = start(t, kokoV1, dir)
	addNote(t, s, "second")
	stop(t, s)

	stop(t, start(t, kokoV2, dir))
	wantSnapshots(t, dir, "koko.db.before-3")
	old := openUnder(t, kokoV1, filepath.Join(dir, "koko.db.before-3"))
	if got, want := notesIn(t, old), []string{"first", "second"}; !slices.Equal(got, want) {
		t.Errorf("the snapshot holds notes %q, want %q", got, want)
	}
}

// A snapshot that cannot be read might be the only copy of the record as the
// older koko left it: the koko server refuses to start rather than replace it.
func TestAnUnreadableSnapshotIsLeftAlone(t *testing.T) {
	dir := t.TempDir()
	stop(t, start(t, kokoV1, dir))
	_ = refused(t, kokoV2Broken, dir) // leaves koko.db.before-3 and the database at schema version 2
	snapshot := filepath.Join(dir, "koko.db.before-3")
	damaged := []byte("not a database")
	if err := os.WriteFile(snapshot, damaged, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := refused(t, kokoV2, dir); !strings.Contains(err.Error(), snapshot) {
		t.Errorf("refusal %q does not name %s", err, snapshot)
	}
	if got, err := os.ReadFile(snapshot); err != nil || !slices.Equal(got, damaged) {
		t.Errorf("the unreadable snapshot now holds %q (%v), want it left as it was", got, err)
	}
	wantSnapshots(t, dir, "koko.db.before-3")
}
