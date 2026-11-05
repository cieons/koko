package device

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/cieons/koko/internal/server/store"
	"github.com/cieons/koko/internal/server/transport"
	"github.com/cieons/koko/internal/shared/wire"
)

// sessionIDPrefix starts every Device Session's id, as in device_3f9k2m7q.
const sessionIDPrefix = "device"

// Authenticator answers, for the TCP listener (transport.Devices), what a
// device's key is and whether a Join Code's secret makes it a Device
// Session: the join step of Device Connection authentication. It runs outside
// any API call, in transactions of its own, and reads the time from the koko
// server's clock.
type Authenticator struct {
	store *store.Store
	clock func() time.Time
	log   *slog.Logger

	mu sync.Mutex
	// recorded is the last used time Used last wrote for each Device
	// Session, so a use within usedGranularity of it skips the database.
	recorded map[string]time.Time
}

// NewAuthenticator returns the Authenticator of the koko server whose
// records st holds, whose clock is clock, logging each join and failed join
// to log.
func NewAuthenticator(st *store.Store, clock func() time.Time, log *slog.Logger) *Authenticator {
	return &Authenticator{store: st, clock: clock, log: log, recorded: map[string]time.Time{}}
}

// Key tells what key is: the key of a live Device Session, of a Revoked
// one, which a public key stays for ever, or of none.
func (s *Authenticator) Key(ctx context.Context, key ssh.PublicKey) (transport.DeviceKey, error) {
	var k transport.DeviceKey
	err := s.store.Read(ctx, func(tx store.Tx) error {
		err := tx.QueryRowContext(ctx, "SELECT id, device, revoked_at IS NOT NULL FROM device_sessions WHERE public_key = ?",
			key.Marshal()).Scan(&k.DeviceSession, &k.Device, &k.Revoked)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	})
	if err != nil {
		return transport.DeviceKey{}, fmt.Errorf("look up a device's key: %w", err)
	}
	return k, nil
}

// usedGranularity is how finely a Device Session's last used time is kept: a
// use within it of the time recorded changes nothing, so a device running
// many commands writes its row once a minute.
const usedGranularity = time.Minute

// Used records that a Device Connection of the live Device Session whose id
// is deviceSession opened a channel now: its last used time, kept to
// usedGranularity. A long-lived Device Connection is used each time it opens
// a channel (a command, an SSH Session, a browser connection), never by
// merely staying open. A use within usedGranularity of the time this koko
// server last wrote costs no transaction.
func (s *Authenticator) Used(ctx context.Context, deviceSession string) error {
	now := s.clock().UTC().Truncate(time.Second)
	s.mu.Lock()
	last, ok := s.recorded[deviceSession]
	s.mu.Unlock()
	if ok && now.Sub(last) < usedGranularity {
		return nil
	}
	var written bool
	err := s.store.Write(ctx, func(tx store.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE device_sessions SET last_used_at = ?1
			WHERE id = ?2 AND revoked_at IS NULL AND (last_used_at IS NULL OR last_used_at <= ?3)`,
			now.Format(time.RFC3339), deviceSession, now.Add(-usedGranularity).Format(time.RFC3339))
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		written = n == 1
		return err
	})
	if err != nil {
		return fmt.Errorf("record the use of the Device Session %s: %w", deviceSession, err)
	}
	if written {
		s.mu.Lock()
		s.recorded[deviceSession] = now
		s.mu.Unlock()
	}
	return nil
}

// noCodeError is why a join fails when the secret is no valid Join Code's.
// The device is told only that the code is invalid or expired; the log
// says which.
type noCodeError struct {
	reason string
	// code is the Join Code's id, when the secret is one's.
	code string
}

func (e *noCodeError) Error() string {
	return e.reason
}

// Join makes key, which the device has proved it holds, a Device Session
// through the Join Code whose secret password carries (wire.JoinSecret): in
// one transaction it records the Device Session with the code's User and
// device name and the code's issuer as "added by", and marks the code used.
// It refuses a password that carries no secret, and a secret of no Join
// Code, or of one used, voided or expired. It logs the join or why it
// failed, never the code or its secret.
func (s *Authenticator) Join(ctx context.Context, key ssh.PublicKey, password []byte, from net.Addr) (string, error) {
	j, err := s.join(ctx, key, password)
	if err != nil {
		attrs := []any{"addr", from, "reason", err.Error()}
		if e, ok := errors.AsType[*noCodeError](err); ok && e.code != "" {
			attrs = append(attrs, "join_code", e.code)
		}
		s.log.Info("join failed", attrs...)
		return "", err
	}
	s.log.Info("device joined", "user", j.user, "device", j.device, "device_session", j.id,
		"added_by", j.addedBy, "join_code", j.code, "addr", from)
	return j.id, nil
}

// unixSocket stands in the log for the Unix socket where a Device Session's
// id would: as the issuer of a Join Code, or what revoked a Device Session.
const unixSocket = "unix socket"

// joined is a Device Session a join made.
type joined struct {
	id, user, device, code string
	// addedBy is the issuing Device Session's id, or unixSocket.
	addedBy string
}

func (s *Authenticator) join(ctx context.Context, key ssh.PublicKey, password []byte) (joined, error) {
	secret, ok := wire.ParseJoinSecret(password)
	if !ok {
		return joined{}, &noCodeError{reason: "the password carries no Join Code secret"}
	}
	now := s.clock().UTC().Truncate(time.Second)
	var j joined
	err := s.store.Write(ctx, func(tx store.Tx) error {
		var userID, expiresAt string
		var issuedBy, usedAt, voidedAt sql.NullString
		err := tx.QueryRowContext(ctx, `SELECT j.id, j.user_id, u.name, j.device, j.issued_by, j.expires_at, j.used_at, j.voided_at
			FROM join_codes j JOIN users u ON u.id = j.user_id WHERE j.secret_hash = ?`, secretHash(secret)).
			Scan(&j.code, &userID, &j.user, &j.device, &issuedBy, &expiresAt, &usedAt, &voidedAt)
		if errors.Is(err, sql.ErrNoRows) {
			return &noCodeError{reason: "no Join Code has this secret"}
		}
		if err != nil {
			return fmt.Errorf("read the Join Code: %w", err)
		}
		expires, err := time.Parse(time.RFC3339, expiresAt)
		if err != nil {
			return fmt.Errorf("read the Join Code %s's expiry: %w", j.code, err)
		}
		switch {
		case usedAt.Valid:
			return &noCodeError{reason: "the Join Code was used", code: j.code}
		case voidedAt.Valid:
			return &noCodeError{reason: "the Join Code was voided", code: j.code}
		case !now.Before(expires):
			return &noCodeError{reason: "the Join Code expired", code: j.code}
		}
		j.id = store.NewID(sessionIDPrefix)
		j.addedBy = unixSocket
		if issuedBy.Valid {
			j.addedBy = issuedBy.String
		}
		stamp := now.Format(time.RFC3339)
		_, err = tx.ExecContext(ctx, `INSERT INTO device_sessions (id, user_id, device, public_key, added_by, created_at)
			VALUES (?, ?, ?, ?, ?, ?)`, j.id, userID, j.device, key.Marshal(), issuedBy, stamp)
		if err != nil {
			return fmt.Errorf("record %s's Device Session %q: %w", j.user, j.device, err)
		}
		if _, err := tx.ExecContext(ctx, "UPDATE join_codes SET used_at = ? WHERE id = ?", stamp, j.code); err != nil {
			return fmt.Errorf("mark the Join Code %s used: %w", j.code, err)
		}
		return nil
	})
	return j, err
}
