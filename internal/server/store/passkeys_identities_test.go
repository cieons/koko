package store

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

// passkeys_identities.sql, run on a koko.db at the schema before it, keeps
// every record and reads every Sign-in recorded before as made with a
// Password; the koko before it then refuses the database, naming the
// snapshot to go back to.
func TestThePasskeysIdentitiesStepReadsEverySignInAsAPassword(t *testing.T) {
	t.Parallel()
	at := slices.Index(kokoSchema.steps, "passkeys_identities.sql")
	if at < 0 {
		t.Fatal("passkeys_identities.sql is not a step")
	}
	older := schema{files: kokoSchema.files, steps: kokoSchema.steps[:at]}
	dir := t.TempDir()
	s := start(t, older, dir)
	err := s.Write(t.Context(), func(tx Tx) error {
		_, err := tx.ExecContext(t.Context(), `INSERT INTO users (id, name, role, created_at)
			VALUES ('user_a', 'alice', 'owner', '2026-11-01T00:00:00Z'),
			       ('user_b', 'bob', 'member', '2026-11-02T00:00:00Z')`)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(t.Context(), `INSERT INTO passwords (id, user_id, sealed, set_at, set_how)
			VALUES ('password_b', 'user_b', x'00', '2026-11-03T00:00:00Z', 'link')`)
		if err != nil {
			return err
		}
		for i, user := range []string{"user_a", "user_b", "user_b"} {
			_, err := tx.ExecContext(t.Context(), `INSERT INTO signins
				(id, user_id, token_hash, browser, system, started_address, address, started_at, last_used_at, expires_at, ended_at, end_reason)
				VALUES (?1, ?2, ?3, 'Firefox', 'Linux', '192.0.2.10', '192.0.2.10', ?4, ?4, '2026-12-01T00:00:00Z', ?5, ?6)`,
				fmt.Sprintf("signin_%d", i), user, []byte{byte(i)}, fmt.Sprintf("2026-11-0%dT00:00:00Z", i+1),
				map[bool]any{true: "2026-11-09T00:00:00Z"}[i == 2], map[bool]any{true: "signed_out"}[i == 2])
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("record Users, a Password and Sign-ins before the step: %v", err)
	}
	stop(t, s)

	s = start(t, kokoSchema, dir)
	type signIn struct{ id, user, method, ended string }
	var got []signIn
	var credentials, users, passwords, others int
	err = s.Read(t.Context(), func(tx Tx) error {
		rows, err := tx.QueryContext(t.Context(), `SELECT id, user_id, method, coalesce(ended_at, '') FROM signins ORDER BY id`)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var si signIn
			if err := rows.Scan(&si.id, &si.user, &si.method, &si.ended); err != nil {
				return err
			}
			got = append(got, si)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		return tx.QueryRowContext(t.Context(), `SELECT
			(SELECT count(*) FROM signins WHERE credential IS NOT NULL),
			(SELECT count(*) FROM users), (SELECT count(*) FROM passwords),
			(SELECT count(*) FROM passkeys) + (SELECT count(*) FROM identities) + (SELECT count(*) FROM identity_provider)`).
			Scan(&credentials, &users, &passwords, &others)
	})
	if err != nil {
		t.Fatalf("read the record after the step: %v", err)
	}
	want := []signIn{
		{"signin_0", "user_a", "password", ""},
		{"signin_1", "user_b", "password", ""},
		{"signin_2", "user_b", "password", "2026-11-09T00:00:00Z"},
	}
	if !slices.Equal(got, want) {
		t.Errorf("after the step the Sign-ins are %+v, want %+v", got, want)
	}
	if credentials != 0 || users != 2 || passwords != 1 || others != 0 {
		t.Errorf("after the step %d Sign-ins name a credential, %d Users and %d Passwords are kept and the new tables hold %d rows, want 0, 2, 1 and 0",
			credentials, users, passwords, others)
	}
	stop(t, s)

	snapshot := fmt.Sprintf("koko.db.before-%d", len(kokoSchema.steps))
	if err := refused(t, older, dir); !strings.Contains(err.Error(), "go back by putting "+snapshot+" in place of koko.db") {
		t.Errorf("the koko before the step refused the database with %q, want it to name %s", err, snapshot)
	}
}
