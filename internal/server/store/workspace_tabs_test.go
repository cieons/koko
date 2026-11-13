package store

import (
	"slices"
	"testing"
)

// workspace_tabs.sql, run on a koko.db at the schema before it, keeps every
// record and adds an empty table, whose rows go with their User.
func TestTheWorkspaceTabsStepKeepsEverythingAndAddsAnEmptyTable(t *testing.T) {
	t.Parallel()
	at := slices.Index(kokoSchema.steps, "workspace_tabs.sql")
	if at < 0 {
		t.Fatal("workspace_tabs.sql is not a step")
	}
	dir := t.TempDir()
	s := start(t, schema{files: kokoSchema.files, steps: kokoSchema.steps[:at]}, dir)
	err := s.Write(t.Context(), func(tx Tx) error {
		_, err := tx.ExecContext(t.Context(), `INSERT INTO users (id, name, role, created_at)
			VALUES ('user_a', 'alice', 'owner', '2026-11-01T00:00:00Z'),
			       ('user_b', 'bob', 'member', '2026-11-02T00:00:00Z')`)
		return err
	})
	if err != nil {
		t.Fatalf("record Users before the step: %v", err)
	}
	stop(t, s)

	s = start(t, kokoSchema, dir)
	count := func(query string) int {
		t.Helper()
		var n int
		err := s.Read(t.Context(), func(tx Tx) error {
			return tx.QueryRowContext(t.Context(), query).Scan(&n)
		})
		if err != nil {
			t.Fatalf("%s: %v", query, err)
		}
		return n
	}
	if n := count("SELECT count(*) FROM users"); n != 2 {
		t.Errorf("after the step %d Users are recorded, want the 2 recorded before", n)
	}
	if n := count("SELECT count(*) FROM workspace_tabs"); n != 0 {
		t.Errorf("after the step workspace_tabs holds %d rows, want none", n)
	}

	err = s.Write(t.Context(), func(tx Tx) error {
		for i, user := range []string{"user_a", "user_a", "user_b"} {
			_, err := tx.ExecContext(t.Context(), `INSERT INTO workspace_tabs
				(id, user_id, position, host_id, label, opened_by, opened_at)
				VALUES (?1, ?2, ?3, 'host_1', 'web1', NULL, '2026-11-13T00:00:00Z')`,
				string(rune('a'+i)), user, i)
			if err != nil {
				return err
			}
		}
		_, err := tx.ExecContext(t.Context(), "DELETE FROM users WHERE id = 'user_a'")
		return err
	})
	if err != nil {
		t.Fatalf("record tabs and delete a User: %v", err)
	}
	if n := count("SELECT count(*) FROM workspace_tabs"); n != 1 {
		t.Errorf("after a User's delete workspace_tabs holds %d rows, want only the other User's 1", n)
	}
}
