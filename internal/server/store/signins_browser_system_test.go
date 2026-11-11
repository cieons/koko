package store

import (
	"fmt"
	"slices"
	"testing"
)

// A Sign-in recorded before signins_browser_system.sql keeps the browser and the
// system its English client label named, and "Browser" names neither.
func TestASignInRecordedBeforeKeepsItsBrowserAndSystem(t *testing.T) {
	t.Parallel()
	at := slices.Index(kokoSchema.steps, "signins_browser_system.sql")
	if at < 0 {
		t.Fatal("signins_browser_system.sql is not a step")
	}
	dir := t.TempDir()
	s := start(t, schema{files: kokoSchema.files, steps: kokoSchema.steps[:at]}, dir)
	labels := []string{"Safari on iPhone", "Firefox", "Browser"}
	err := s.Write(t.Context(), func(tx Tx) error {
		_, err := tx.ExecContext(t.Context(), `INSERT INTO users (id, name, role, created_at)
			VALUES ('user_a', 'alice', 'owner', '2026-11-01T00:00:00Z')`)
		if err != nil {
			return err
		}
		for i, label := range labels {
			_, err := tx.ExecContext(t.Context(), `INSERT INTO signins
				(id, user_id, token_hash, client, started_address, address, started_at, last_used_at, expires_at)
				VALUES (?1, 'user_a', ?2, ?3, '192.0.2.10', '192.0.2.10', ?4, ?4, '2026-12-01T00:00:00Z')`,
				fmt.Sprintf("signin_%d", i), []byte(label), label, fmt.Sprintf("2026-11-0%dT00:00:00Z", i+1))
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("record Sign-ins before the step: %v", err)
	}
	stop(t, s)

	s = start(t, kokoSchema, dir)
	var got [][2]string
	err = s.Read(t.Context(), func(tx Tx) error {
		rows, err := tx.QueryContext(t.Context(), "SELECT browser, system FROM signins ORDER BY started_at")
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var b, sys string
			if err := rows.Scan(&b, &sys); err != nil {
				return err
			}
			got = append(got, [2]string{b, sys})
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatalf("read the Sign-ins after the step: %v", err)
	}
	want := [][2]string{{"Safari", "iPhone"}, {"Firefox", ""}, {"", ""}}
	if !slices.Equal(got, want) {
		t.Errorf("the Sign-ins labelled %q have browser and system %q, want %q", labels, got, want)
	}
}
