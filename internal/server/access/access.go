// Package access is the access check: the one place that decides who a
// call acts as and what it may do (P0 spec: people and Spaces, "The access
// check"). It answers from the current records on every call, inside the
// call's transaction, so a change takes effect on the next call.
//
// It answers "who is calling" so far; the questions about Spaces join it with
// the Spaces and membership (#39).
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
