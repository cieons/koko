// Package access is the access check: the one place that decides who a
// call acts as and what it may do (P0 spec: people and Spaces, "The access
// check"). It answers four questions: who is calling (Who), whether they are
// the Owner (Identity.IsOwner, and OwnerOnly, which refuses anyone else), whether
// they can act in a Space (Identity.CanAct), and which Spaces they can act in
// (Identity.Spaces). It answers from the current records on every call,
// inside the call's transaction, so a change, such as a membership, takes
// effect on the next call. No other package reads roles or memberships to
// decide what a caller may do.
package access

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

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
	// Local caller, which has none.
	DeviceSession *api.DeviceSession
}

// IsOwner tells whether the call acts as the Owner.
func (i Identity) IsOwner() bool {
	return i.User.Role == api.RoleOwner
}

// Who answers who is calling: it turns a Device Connection's Device Session
// into its User, and the Local caller into the Owner. A caller that is
// neither, or a Device Session no record holds, is a bug in the transport,
// and fails.
func Who(ctx context.Context, tx store.Tx, c op.Caller) (Identity, error) {
	switch {
	case c.Local:
		var u api.User
		err := tx.QueryRowContext(ctx, "SELECT id, name, role, created_at FROM users WHERE role = ?", api.RoleOwner).
			Scan(&u.ID, &u.Name, &u.Role, &u.CreatedAt)
		if err != nil {
			return Identity{}, fmt.Errorf("read the Owner, whom the Local caller acts as: %w", err)
		}
		return Identity{User: u}, nil
	case c.DeviceSession != "":
		var u api.User
		var d api.DeviceSession
		err := tx.QueryRowContext(ctx, `SELECT u.id, u.name, u.role, u.created_at, d.id, d.device, d.created_at
			FROM device_sessions d JOIN users u ON u.id = d.user_id WHERE d.id = ?`, c.DeviceSession).
			Scan(&u.ID, &u.Name, &u.Role, &u.CreatedAt, &d.ID, &d.Device, &d.CreatedAt)
		if errors.Is(err, sql.ErrNoRows) {
			return Identity{}, fmt.Errorf("no Device Session is %s", c.DeviceSession)
		}
		if err != nil {
			return Identity{}, fmt.Errorf("read the Device Session %s: %w", c.DeviceSession, err)
		}
		d.User = u.Name
		return Identity{User: u, DeviceSession: &d}, nil
	}
	return Identity{}, errors.New("the call came from neither the Unix socket nor a Device Session")
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
