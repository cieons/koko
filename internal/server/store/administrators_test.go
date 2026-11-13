package store

import (
	"slices"
	"testing"
)

// administrators.sql keeps every User, membership, Device Session and
// Sign-in recorded before it and makes nobody an Administrator; the
// one-Owner index still refuses a second Owner.
func TestTheAdministratorsStepKeepsTheRecordAndMakesNoAdministrator(t *testing.T) {
	t.Parallel()
	at := slices.Index(kokoSchema.steps, "administrators.sql")
	if at < 0 {
		t.Fatal("administrators.sql is not a step")
	}
	dir := t.TempDir()
	s := start(t, schema{files: kokoSchema.files, steps: kokoSchema.steps[:at]}, dir)
	err := s.Write(t.Context(), func(tx Tx) error {
		for _, q := range []string{
			`INSERT INTO users (id, name, role, created_at) VALUES
				('user_a', 'alice', 'owner', '2026-11-01T00:00:00Z'),
				('user_b', 'bob', 'member', '2026-11-01T00:00:00Z')`,
			`INSERT INTO spaces (id, name, user_id) VALUES ('space_o', 'ops', NULL)`,
			`INSERT INTO memberships (space_id, user_id) VALUES ('space_o', 'user_b')`,
			`INSERT INTO device_sessions (id, user_id, device, public_key, created_at)
				VALUES ('device_1', 'user_b', 'laptop', x'01', '2026-11-01T00:00:00Z')`,
			`INSERT INTO signins (id, user_id, token_hash, browser, started_address, address, started_at, last_used_at, expires_at)
				VALUES ('signin_1', 'user_b', x'02', 'Firefox', '192.0.2.10', '192.0.2.10',
				'2026-11-01T00:00:00Z', '2026-11-01T00:00:00Z', '2026-12-01T00:00:00Z')`,
		} {
			if _, err := tx.ExecContext(t.Context(), q); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("record before the step: %v", err)
	}
	stop(t, s)

	s = start(t, kokoSchema, dir)
	var users, memberships, sessions, signIns, administrators int
	err = s.Read(t.Context(), func(tx Tx) error {
		return tx.QueryRowContext(t.Context(), `SELECT (SELECT count(*) FROM users), (SELECT count(*) FROM memberships),
			(SELECT count(*) FROM device_sessions), (SELECT count(*) FROM signins), (SELECT count(*) FROM administrators)`).
			Scan(&users, &memberships, &sessions, &signIns, &administrators)
	})
	if err != nil {
		t.Fatalf("read the record after the step: %v", err)
	}
	if got, want := []int{users, memberships, sessions, signIns, administrators}, []int{2, 1, 1, 1, 0}; !slices.Equal(got, want) {
		t.Errorf("after the step: %d Users, %d memberships, %d Device Sessions, %d Sign-ins and %d Administrators; want %v",
			users, memberships, sessions, signIns, administrators, want)
	}
	err = s.Write(t.Context(), func(tx Tx) error {
		_, err := tx.ExecContext(t.Context(), `INSERT INTO users (id, name, role, created_at)
			VALUES ('user_c', 'carol', 'owner', '2026-11-02T00:00:00Z')`)
		return err
	})
	if err == nil {
		t.Error("a second owner row was recorded after the step; want the one-Owner index to refuse it")
	}
}
