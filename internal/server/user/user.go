// Package user is the user area of the koko server: the Users, of whom
// exactly one is the Owner and every other a Member, each with a Personal
// Space (P0 spec: people and Spaces).
package user

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/cieons/koko/internal/server/op"
	"github.com/cieons/koko/internal/server/space"
	"github.com/cieons/koko/internal/server/store"
	"github.com/cieons/koko/internal/shared/api"
	"github.com/cieons/koko/internal/shared/name"
)

// idPrefix starts every User's id, as in user_3f9k2m7q.
const idPrefix = "user"

// The roles, as the users table and the API name them.
const (
	roleOwner  = "owner"
	roleMember = "member"
)

// IssueFunc issues, as caller, a Join Code for one new device, as device.add
// asks for one: the device area's device.Issuer.Issue.
type IssueFunc func(ctx context.Context, tx store.Tx, caller op.Caller, req api.DeviceAddRequest) (api.JoinCode, error)

// Register adds the user operations to the API; issue issues an invited
// User's first Join Code.
func Register(a *op.API, issue IssueFunc) {
	a.Handle(api.UserInvite, invite(issue))
}

// invite is user.invite: in one transaction it creates the Member with their
// Personal Space, and issues their first Join Code, whose device the Invite
// does not name.
func invite(issue IssueFunc) op.Func[api.UserInviteRequest, api.UserInviteResponse] {
	return func(ctx context.Context, tx store.Tx, caller op.Caller, req api.UserInviteRequest) (api.UserInviteResponse, error) {
		u, err := add(ctx, tx, req.Name, roleMember)
		if err != nil {
			return api.UserInviteResponse{}, err
		}
		code, err := issue(ctx, tx, caller, api.DeviceAddRequest{User: u.Name})
		if err != nil {
			return api.UserInviteResponse{}, err
		}
		return api.UserInviteResponse{User: u, JoinCode: code}, nil
	}
}

// AddOwner records the Owner, named owner, with their Personal Space, which
// takes the same name. The name is checked as a User name and has no
// default, because a User's name never changes. Initialisation calls it once,
// on a koko server that holds no User and no Space yet.
func AddOwner(ctx context.Context, tx store.Tx, owner string) error {
	_, err := add(ctx, tx, owner, roleOwner)
	return err
}

// add records a new User with their Personal Space, which takes their name.
// The name must be a User name that no User, Removed ones included, and no
// Space holds, since Users and Spaces share one namespace.
func add(ctx context.Context, tx store.Tx, userName, role string) (api.User, error) {
	kind := "User"
	if role == roleOwner {
		kind = "Owner"
	}
	if err := name.Check(userName); err != nil {
		return api.User{}, api.CodeInvalidName.Errorf("invalid %s name: %v", kind, err)
	}
	var heldByUser, heldBySpace bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM users WHERE name = ?1),
		EXISTS (SELECT 1 FROM spaces WHERE name = ?1)`, userName).Scan(&heldByUser, &heldBySpace)
	switch {
	case err != nil:
		return api.User{}, fmt.Errorf("check whether the name %q is taken: %w", userName, err)
	case heldByUser:
		return api.User{}, api.CodeNameTaken.Errorf("a User named %q already exists", userName)
	case heldBySpace:
		return api.User{}, api.CodeNameTaken.Errorf("a Space named %q already exists, and Users and Spaces share their names", userName)
	}
	u := api.User{ID: store.NewID(idPrefix), Name: userName, Role: role, CreatedAt: op.Now(ctx).Format(time.RFC3339)}
	_, err = tx.ExecContext(ctx, "INSERT INTO users (id, name, role, created_at) VALUES (?, ?, ?, ?)", u.ID, u.Name, u.Role, u.CreatedAt)
	if err != nil {
		return api.User{}, fmt.Errorf("record the %s %q: %w", kind, userName, err)
	}
	if err := space.AddPersonal(ctx, tx, u.ID, u.Name); err != nil {
		return api.User{}, err
	}
	return u, nil
}

// Owner returns the Owner.
func Owner(ctx context.Context, tx store.Tx) (api.User, error) {
	u, err := scan(tx.QueryRowContext(ctx, "SELECT id, name, role, created_at FROM users WHERE role = ?", roleOwner))
	if err != nil {
		return api.User{}, fmt.Errorf("read the Owner: %w", err)
	}
	return u, nil
}

// ByName returns the User named userName. A name that is not a User name is
// refused with CodeInvalidName, and one no User holds with CodeUnknownUser.
func ByName(ctx context.Context, tx store.Tx, userName string) (api.User, error) {
	if err := name.Check(userName); err != nil {
		return api.User{}, api.CodeInvalidName.Errorf("invalid User name: %v", err)
	}
	u, err := scan(tx.QueryRowContext(ctx, "SELECT id, name, role, created_at FROM users WHERE name = ?", userName))
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return api.User{}, api.CodeUnknownUser.Errorf("no User is named %q", userName)
	case err != nil:
		return api.User{}, fmt.Errorf("read the User %q: %w", userName, err)
	}
	return u, nil
}

func scan(row *sql.Row) (api.User, error) {
	var u api.User
	err := row.Scan(&u.ID, &u.Name, &u.Role, &u.CreatedAt)
	return u, err
}
