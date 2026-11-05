package store

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// peekNotes reads the notes of the koko.db in dir with Peek, and whether it
// found one with a schema, which is when Peek calls its function.
func peekNotes(t *testing.T, dir string) (bool, []string) {
	t.Helper()
	found := false
	var bodies []string
	err := Peek(t.Context(), dir, func(tx Tx) error {
		found = true
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
		t.Fatalf("peek: %v", err)
	}
	return found, bodies
}

// listDir lists the names in dir, or nil when it does not exist.
func listDir(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

// Peek finds nothing where there is no koko.db, and makes neither the file
// nor its directory.
func TestPeekWithoutADatabaseFindsNothing(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "koko")
	if found, _ := peekNotes(t, dir); found {
		t.Error("Peek found a database where there is none")
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("Peek made the data directory: %v", err)
	}
}

// A koko.db that no step has reached yet holds nothing.
func TestPeekOfAnEmptyDatabaseFindsNothing(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, FileName), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if found, _ := peekNotes(t, dir); found {
		t.Error("Peek found a schema in an empty koko.db")
	}
}

// Peek reads koko.db as it is, beside the koko that holds it open, and
// neither migrates it nor snapshots it.
func TestPeekReadsWithoutMigrating(t *testing.T) {
	dir := t.TempDir()
	s := start(t, kokoV1, dir)
	addNote(t, s, "left by the older koko")

	found, notes := peekNotes(t, dir)
	if !found || !slices.Equal(notes, []string{"left by the older koko"}) {
		t.Errorf("Peek found %v with notes %v, want the older koko's note", found, notes)
	}
	if v := schemaVersionOf(t, s); v != 1 {
		t.Errorf("schema version %d after Peek, want 1: Peek migrated", v)
	}
	if snaps := snapshots(t, dir); len(snaps) != 0 {
		t.Errorf("Peek wrote snapshots %v", snaps)
	}

	stop(t, s)
	before := listDir(t, dir)
	if found, notes := peekNotes(t, dir); !found || len(notes) != 1 {
		t.Errorf("Peek of a stopped koko's database found %v with notes %v", found, notes)
	}
	if after := listDir(t, dir); !slices.Equal(after, before) {
		t.Errorf("Peek left %v in the data directory, which held %v", after, before)
	}
}

// Nothing Peek runs can write.
func TestPeekCannotWrite(t *testing.T) {
	dir := t.TempDir()
	stop(t, start(t, kokoV1, dir))
	err := Peek(t.Context(), dir, func(tx Tx) error {
		_, err := tx.ExecContext(t.Context(), "INSERT INTO notes (body) VALUES ('written by Peek')")
		return err
	})
	if err == nil {
		t.Error("Peek wrote to koko.db")
	}
	s := start(t, kokoV1, dir)
	if notes := notesIn(t, s); len(notes) != 0 {
		t.Errorf("koko.db holds %v after Peek", notes)
	}
}
