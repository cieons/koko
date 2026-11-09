// Package access is the access check: the one place that decides who a
// call acts as and what it may do (P0 spec: people and Spaces, "The access
// check"). It answers four questions: who is calling (Who), whether they are
// the Owner (Identity.IsOwner, and OwnerOnly, which refuses anyone else), whether
// they can act in a Space (Identity.CanAct), and which Spaces they can act in
// (Identity.Spaces). It answers from the current records on every call,
// inside the call's transaction, so a change, such as a membership or a
// revocation, takes effect on the next call. No other package reads roles or
// memberships to decide what a caller may do. It also carries the
// access-change signal (Signal), which tells the parts of the koko server
// that hold access open, such as Device Connections, when a committed change
// withdrew it.
package access

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/cieons/koko/internal/server/op"
	"github.com/cieons/koko/internal/server/store"
	"github.com/cieons/koko/internal/shared/api"
)

// Identity is who a call acts as.
type Identity struct {
	// User is the calling User: a Device Session's own, or the Owner for
	// the Local caller (ADR 0008).
	User api.User
	// DeviceSession is the Device Session the call came on, or nil for the
	// Local caller, which has none, and for a Sign-in.
	DeviceSession *api.DeviceSession
	// SignIn is the Sign-in the call came on, from a browser on the web
	// listener, or nil for every other caller.
	SignIn *api.SignIn
}

// unixSocket stands in the log for the Unix socket where the id of the
// Device Session or the Sign-in a call came on would.
const unixSocket = "unix socket"

// By names the caller in a log line, as what issued, revoked or removed
// something: its Device Session's id, its Sign-in's id, or "unix socket".
func (i Identity) By() string {
	switch {
	case i.DeviceSession != nil:
		return i.DeviceSession.ID
	case i.SignIn != nil:
		return i.SignIn.ID
	}
	return unixSocket
}

// Issuer is the id a record keeps of the caller as what issued it: its
// Device Session's or its Sign-in's, or nil for the Unix socket.
func (i Identity) Issuer() *string {
	if i.DeviceSession == nil && i.SignIn == nil {
		return nil
	}
	return new(i.By())
}

// IsOwner tells whether the call acts as the Owner.
func (i Identity) IsOwner() bool {
	return i.User.Role == api.RoleOwner
}

// Who answers who is calling: it turns a Device Connection's Device Session,
// or a browser's Sign-in, into its User, and the Local caller into the
// Owner. A Revoked Device Session is refused with api.CodeRevoked, and so is
// a Removed User's, since Removing a User revokes each of theirs; a Sign-in
// that has ended, a Removed User's among them, with api.CodeSignInRequired.
// A caller that is none of these, or a Device Session or Sign-in no record
// holds, is a bug in the transport or the web front, and fails.
func Who(ctx context.Context, tx store.Tx, c op.Caller) (Identity, error) {
	switch {
	case c.Local:
		u, err := scanUser(tx.QueryRowContext(ctx, "SELECT id, name, role, created_at FROM users WHERE role = ?", api.RoleOwner))
		if err != nil {
			return Identity{}, fmt.Errorf("read the Owner, whom the Local caller acts as: %w", err)
		}
		return Identity{User: u}, nil
	case c.DeviceSession != "":
		found, err := DeviceSessions(ctx, tx, "d.id = ?", c.DeviceSession)
		if err != nil {
			return Identity{}, err
		}
		if len(found) == 0 {
			return Identity{}, fmt.Errorf("no Device Session is %s", c.DeviceSession)
		}
		d := found[0]
		if d.RevokedAt != "" {
			return Identity{}, api.CodeRevoked.Errorf("the Device Session %q of this device was revoked", d.Device)
		}
		u, err := scanUser(tx.QueryRowContext(ctx, `SELECT u.id, u.name, u.role, u.created_at
			FROM device_sessions d JOIN users u ON u.id = d.user_id WHERE d.id = ?`, d.ID))
		if err != nil {
			return Identity{}, fmt.Errorf("read the User of the Device Session %s: %w", d.ID, err)
		}
		return Identity{User: u, DeviceSession: &d}, nil
	case c.SignIn != "":
		found, err := SignIns(ctx, tx, op.Now(ctx), "s.id = ?", c.SignIn)
		if err != nil {
			return Identity{}, err
		}
		if len(found) == 0 {
			return Identity{}, fmt.Errorf("no Sign-in is %s", c.SignIn)
		}
		s := found[0]
		if s.EndedAt != "" {
			return Identity{}, api.CodeSignInRequired.Errorf("this browser's Sign-in has ended: sign in again")
		}
		u, err := scanUser(tx.QueryRowContext(ctx, `SELECT u.id, u.name, u.role, u.created_at
			FROM signins s JOIN users u ON u.id = s.user_id WHERE s.id = ?`, s.ID))
		if err != nil {
			return Identity{}, fmt.Errorf("read the User of the Sign-in %s: %w", s.ID, err)
		}
		return Identity{User: u, SignIn: &s}, nil
	}
	return Identity{}, errors.New("the call came from neither the Unix socket, a Device Session nor a Sign-in")
}

// OwnerOnly answers who is calling, as Who does, and refuses with
// CodeOwnerOnly a caller who is not the Owner. An Owner-only operation asks
// it before it reads anything the call names, so the refusal is the same
// whatever the call names and tells a Member nothing about the records. does
// says what only the Owner does, as in "adds Spaces".
func OwnerOnly(ctx context.Context, tx store.Tx, c op.Caller, does string) (Identity, error) {
	who, err := Who(ctx, tx, c)
	if err != nil {
		return Identity{}, err
	}
	if !who.IsOwner() {
		return Identity{}, api.CodeOwnerOnly.Errorf("only the Owner %s", does)
	}
	return who, nil
}

// CanAct tells whether the caller can act in the Space whose id is spaceID,
// by the rule Spaces holds.
func (i Identity) CanAct(ctx context.Context, tx store.Tx, spaceID string) (bool, error) {
	s, err := i.Spaces(ctx, tx)
	if err != nil {
		return false, err
	}
	return s.Has(spaceID), nil
}

// Scope is the Spaces a caller can act in, for lists and counts: a record in
// a Space outside it is left out, as if it did not exist.
type Scope struct {
	// all is the Owner's scope: every Space.
	all bool
	ids map[string]bool
}

// Has tells whether the Space whose id is spaceID is in the scope.
func (s Scope) Has(spaceID string) bool {
	return s.all || s.ids[spaceID]
}

// Spaces answers which Spaces the caller can act in, by the one rule (ADR
// 0011): the Owner acts in every Space, and any other User in their own
// Personal Space and in each Space they are a Member of. Nothing else, such
// as an Attribute, grants access.
func (i Identity) Spaces(ctx context.Context, tx store.Tx) (Scope, error) {
	if i.IsOwner() {
		return Scope{all: true}, nil
	}
	rows, err := tx.QueryContext(ctx, `SELECT id FROM spaces WHERE user_id = ?1
		UNION SELECT space_id FROM memberships WHERE user_id = ?1`, i.User.ID)
	if err != nil {
		return Scope{}, fmt.Errorf("read the Spaces %s can act in: %w", i.User.Name, err)
	}
	defer func() { _ = rows.Close() }()
	s := Scope{ids: map[string]bool{}}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return Scope{}, fmt.Errorf("read the Spaces %s can act in: %w", i.User.Name, err)
		}
		s.ids[id] = true
	}
	if err := rows.Err(); err != nil {
		return Scope{}, fmt.Errorf("read the Spaces %s can act in: %w", i.User.Name, err)
	}
	return s, nil
}

func scanUser(row *sql.Row) (api.User, error) {
	var u api.User
	err := row.Scan(&u.ID, &u.Name, &u.Role, &u.CreatedAt)
	return u, err
}

// DeviceSessions returns the Device Sessions, live and Revoked, that the SQL
// condition where selects with args, as the API returns them, ordered by
// their User's name and then in the order they joined. The condition reads
// the Device Session as d and its User as u. It is the one way a Device
// Session record is read, so who is calling and the device operations
// answer alike.
func DeviceSessions(ctx context.Context, tx store.Tx, where string, args ...any) ([]api.DeviceSession, error) {
	rows, err := tx.QueryContext(ctx, `SELECT d.id, u.name, d.device, a.id, au.name, a.device,
			d.created_at, d.last_used_at, d.revoked_at
		FROM device_sessions d JOIN users u ON u.id = d.user_id
		LEFT JOIN device_sessions a ON a.id = d.added_by LEFT JOIN users au ON au.id = a.user_id
		WHERE `+where+` ORDER BY u.name, d.created_at, d.device, d.id`, args...)
	if err != nil {
		return nil, fmt.Errorf("read Device Sessions: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var list []api.DeviceSession
	for rows.Next() {
		var d api.DeviceSession
		var addedID, addedUser, addedDevice, lastUsed, revoked sql.NullString
		err := rows.Scan(&d.ID, &d.User, &d.Device, &addedID, &addedUser, &addedDevice, &d.CreatedAt, &lastUsed, &revoked)
		if err != nil {
			return nil, fmt.Errorf("read Device Sessions: %w", err)
		}
		if addedID.Valid {
			d.AddedBy = &api.DeviceSessionRef{ID: addedID.String, User: addedUser.String, Device: addedDevice.String}
		}
		d.LastUsedAt, d.RevokedAt = lastUsed.String, revoked.String
		list = append(list, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read Device Sessions: %w", err)
	}
	return list, nil
}

// How long a Sign-in lasts (ADR 0043): SignInLifetime after it began, or
// SignInIdle after it was last used, whichever comes first.
const (
	SignInLifetime = 30 * 24 * time.Hour
	SignInIdle     = 7 * 24 * time.Hour
)

// SignIns returns the Sign-ins, live and ended, that the SQL condition where
// selects with args, as the API returns them at now, ordered by their User's
// name and then in the order they began. The condition reads the Sign-in as
// s and its User as u. A Sign-in past its expiry at now is answered as
// ended then, "expired", whether or not its record says so yet. It is the
// one way a Sign-in record is read, so who is calling and the signin
// operations answer alike.
func SignIns(ctx context.Context, tx store.Tx, now time.Time, where string, args ...any) ([]api.SignIn, error) {
	rows, err := tx.QueryContext(ctx, `SELECT s.id, u.name, s.client, s.address, s.started_at, s.last_used_at,
			s.expires_at, s.ended_at, s.end_reason
		FROM signins s JOIN users u ON u.id = s.user_id
		WHERE `+where+` ORDER BY u.name, s.started_at, s.id`, args...)
	if err != nil {
		return nil, fmt.Errorf("read Sign-ins: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var list []api.SignIn
	for rows.Next() {
		var s api.SignIn
		var ended, reason sql.NullString
		err := rows.Scan(&s.ID, &s.User, &s.Client, &s.Address, &s.StartedAt, &s.LastUsedAt, &s.ExpiresAt, &ended, &reason)
		if err != nil {
			return nil, fmt.Errorf("read Sign-ins: %w", err)
		}
		s.EndedAt, s.EndReason = ended.String, reason.String
		if s.ExpiresAt, err = signInExpiry(s); err != nil {
			return nil, err
		}
		if s.EndedAt == "" && s.ExpiresAt <= now.Format(time.RFC3339) {
			s.EndedAt, s.EndReason = s.ExpiresAt, api.EndExpired
		}
		list = append(list, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read Sign-ins: %w", err)
	}
	return list, nil
}

// signInExpiry is when s ends unless it is used again: SignInIdle after it
// was last used, and never later than its record's expiry, SignInLifetime
// after it began.
func signInExpiry(s api.SignIn) (string, error) {
	last, err := time.Parse(time.RFC3339, s.LastUsedAt)
	if err != nil {
		return "", fmt.Errorf("read when the Sign-in %s was last used: %w", s.ID, err)
	}
	idle := last.Add(SignInIdle).UTC().Format(time.RFC3339)
	return min(idle, s.ExpiresAt), nil
}
