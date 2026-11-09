// Package user is the user area of the koko server: the Users, of whom
// exactly one is the Owner and every other a Member, each with a Personal
// Space (P0 spec: people and Spaces).
package user

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/cieons/koko/internal/server/access"
	"github.com/cieons/koko/internal/server/op"
	"github.com/cieons/koko/internal/server/space"
	"github.com/cieons/koko/internal/server/store"
	"github.com/cieons/koko/internal/shared/api"
	"github.com/cieons/koko/internal/shared/name"
)

// idPrefix starts every User's id, as in user_3f9k2m7q.
const idPrefix = "user"

// IssueFunc issues, as caller, a Join Code for one new device, as device.add
// asks for one: the device area's device.Issuer.Issue.
type IssueFunc func(ctx context.Context, tx store.Tx, caller op.Caller, req api.DeviceAddRequest) (api.JoinCode, error)

// Browsers is the signin area's part in the user area's operations: a
// User's Password, Password Links and Sign-ins, which the user area does
// not read itself (signin.Service).
type Browsers interface {
	// IssueLink issues, as who, a Password Link for u, valid for as long as
	// validFor chooses.
	IssueLink(ctx context.Context, tx store.Tx, who access.Identity, u api.User, validFor *int) (api.PasswordLink, error)
	// Used tells whether u ever set a Password or had a Sign-in.
	Used(ctx context.Context, tx store.Tx, u api.User) (bool, error)
	// Delete deletes the Password Links of u, a User being deleted.
	Delete(ctx context.Context, tx store.Tx, u api.User) error
	// Remove ends u's Sign-ins, deletes their Password and voids their
	// unused Password Links, as by names the caller, and counts them.
	Remove(ctx context.Context, tx store.Tx, u api.User, by string) (signInsEnded, linksVoided int64, err error)
}

// Register adds the user operations to the API; issue issues an invited
// User's first Join Code, browsers their first Password Link and their
// part in removing them, and holdings count what the areas record in the
// Personal Space of a User being deleted, which must hold nothing. Each
// User Removed is announced on changes and logged to log once it commits.
func Register(a *op.API, issue IssueFunc, browsers Browsers, holdings []space.Holding, changes *access.Signal, log *slog.Logger) {
	a.Handle(api.UserInvite, invite(issue, browsers))
	a.Handle(api.UserCurrent, current)
	a.Handle(api.UserList, list)
	a.Handle(api.UserRemove, remover{browsers: browsers, holdings: holdings, changes: changes, log: log}.remove)
}

// current is user.current: who the call acts as.
func current(ctx context.Context, tx store.Tx, caller op.Caller, _ api.UserCurrentRequest) (api.UserCurrentResponse, error) {
	who, err := access.Who(ctx, tx, caller)
	if err != nil {
		return api.UserCurrentResponse{}, err
	}
	resp := api.UserCurrentResponse{User: who.User, DeviceSession: who.DeviceSession}
	if si := who.SignIn; si != nil {
		resp.SignIn = &api.CurrentSignIn{ID: si.ID, Client: si.Client, StartedAt: si.StartedAt, ExpiresAt: si.ExpiresAt}
	}
	return resp, nil
}

// list is user.list: the Users by name, each with when they set their
// Password, the Removed ones only with req.All. Only the Owner lists Users.
func list(ctx context.Context, tx store.Tx, caller op.Caller, req api.UserListRequest) (api.UserListResponse, error) {
	if _, err := access.OwnerOnly(ctx, tx, caller, "lists Users"); err != nil {
		return api.UserListResponse{}, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT u.id, u.name, u.role, u.created_at, u.removed_at, p.set_at
		FROM users u LEFT JOIN passwords p ON p.user_id = u.id
		WHERE ? OR u.removed_at IS NULL ORDER BY u.name`, req.All)
	if err != nil {
		return api.UserListResponse{}, fmt.Errorf("list the Users: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var resp api.UserListResponse
	for rows.Next() {
		var u api.User
		var removed, passwordSet sql.NullString
		err := rows.Scan(&u.ID, &u.Name, &u.Role, &u.CreatedAt, &removed, &passwordSet)
		u.RemovedAt, u.PasswordSetAt = removed.String, passwordSet.String
		if err != nil {
			return api.UserListResponse{}, fmt.Errorf("list the Users: %w", err)
		}
		resp.Users = append(resp.Users, u)
	}
	if err := rows.Err(); err != nil {
		return api.UserListResponse{}, fmt.Errorf("list the Users: %w", err)
	}
	return resp, nil
}

// invite is user.invite: in one transaction it creates the Member with their
// Personal Space, and issues their first Join Code, whose device the Invite
// does not name, or with req.Web their first Password Link. Only the Owner
// invites, and a Member is refused before the name is checked, so the
// refusal says nothing of who holds it.
func invite(issue IssueFunc, browsers Browsers) op.Func[api.UserInviteRequest, api.UserInviteResponse] {
	return func(ctx context.Context, tx store.Tx, caller op.Caller, req api.UserInviteRequest) (api.UserInviteResponse, error) {
		who, err := access.OwnerOnly(ctx, tx, caller, "invites Users")
		if err != nil {
			return api.UserInviteResponse{}, err
		}
		u, err := add(ctx, tx, req.Name, api.RoleMember)
		if err != nil {
			return api.UserInviteResponse{}, err
		}
		if req.Web {
			l, err := browsers.IssueLink(ctx, tx, who, u, req.ValidFor)
			if err != nil {
				return api.UserInviteResponse{}, err
			}
			return api.UserInviteResponse{User: u, PasswordLink: &l}, nil
		}
		code, err := issue(ctx, tx, caller, api.DeviceAddRequest{User: u.Name, ValidFor: req.ValidFor})
		if err != nil {
			return api.UserInviteResponse{}, err
		}
		return api.UserInviteResponse{User: u, JoinCode: &code}, nil
	}
}

// AddOwner records the Owner, named owner, with their Personal Space, which
// takes the same name. The name is checked as a User name and has no
// default, because a User's name never changes. Initialisation calls it once,
// on a koko server that holds no User and no Space yet.
func AddOwner(ctx context.Context, tx store.Tx, owner string) error {
	_, err := add(ctx, tx, owner, api.RoleOwner)
	return err
}

// add records a new User with their Personal Space, which takes their name.
// The name must be a User name that no User, Removed ones included, and no
// Space holds, since Users and Spaces share one namespace.
func add(ctx context.Context, tx store.Tx, userName, role string) (api.User, error) {
	kind := "User"
	if role == api.RoleOwner {
		kind = "Owner"
	}
	if err := name.Check(userName); err != nil {
		return api.User{}, api.CodeInvalidName.Errorf("invalid %s name: %v", kind, err)
	}
	var heldByUser, heldByRemoved, heldBySpace bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM users WHERE name = ?1),
		EXISTS (SELECT 1 FROM users WHERE name = ?1 AND removed_at IS NOT NULL),
		EXISTS (SELECT 1 FROM spaces WHERE name = ?1)`, userName).Scan(&heldByUser, &heldByRemoved, &heldBySpace)
	switch {
	case err != nil:
		return api.User{}, fmt.Errorf("check whether the name %q is taken: %w", userName, err)
	case heldByRemoved:
		return api.User{}, api.CodeNameTaken.Errorf("a User named %q was Removed, and a Removed User's name is never used again", userName)
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

// remover answers user.remove, announcing each User Removed on changes and
// logging what it did to log once it commits.
type remover struct {
	browsers Browsers
	holdings []space.Holding
	changes  *access.Signal
	log      *slog.Logger
}

// remove is user.remove: it deletes a User who never used koko and Removes
// anyone else, the least permanent outcome the records allow, and says
// which. Only the Owner removes Users, and a Member is refused before the
// name is read. The Owner is never removed, and a Removed User is not
// removed again.
func (r remover) remove(ctx context.Context, tx store.Tx, caller op.Caller, req api.UserRemoveRequest) (api.UserRemoveResponse, error) {
	who, err := access.OwnerOnly(ctx, tx, caller, "removes Users")
	if err != nil {
		return api.UserRemoveResponse{}, err
	}
	u, err := ByName(ctx, tx, req.Name)
	if err != nil {
		return api.UserRemoveResponse{}, err
	}
	switch {
	case u.Role == api.RoleOwner:
		return api.UserRemoveResponse{}, api.CodeOwnerNotRemovable.Errorf("%s is the Owner, who is never deleted or Removed", u.Name)
	case u.RemovedAt != "":
		return api.UserRemoveResponse{}, api.CodeUserRemoved.Errorf("%s was Removed already, at %s", u.Name, u.RemovedAt)
	}
	// A User who never used koko has never had a Device Session, Revoked
	// ones included, and never set a Password.
	var joined bool
	err = tx.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM device_sessions WHERE user_id = ?)", u.ID).Scan(&joined)
	if err != nil {
		return api.UserRemoveResponse{}, fmt.Errorf("tell whether %s ever joined: %w", u.Name, err)
	}
	usedBrowser, err := r.browsers.Used(ctx, tx, u)
	if err != nil {
		return api.UserRemoveResponse{}, err
	}
	by := who.By()
	if !joined && !usedBrowser {
		if err := r.delete(ctx, tx, u, by); err != nil {
			return api.UserRemoveResponse{}, err
		}
		return api.UserRemoveResponse{Outcome: api.OutcomeDeleted, User: u}, nil
	}
	removed, err := r.removeJoined(ctx, tx, u, by)
	if err != nil {
		return api.UserRemoveResponse{}, err
	}
	return api.UserRemoveResponse{Outcome: api.OutcomeRemoved, User: removed}, nil
}

// removeJoined Removes u, a User who used koko, and returns them Removed.
// In the call's transaction it revokes every Device Session they hold, voids
// their valid Join Codes and those their Device Sessions issued, ends their
// Sign-ins, deletes their Password and voids their unused Password Links,
// takes them out of every Space, passes their Personal Space to the Owner,
// and marks them Removed, keeping the record so their name is never used again. Once
// it commits, each revocation and the removal are logged, by being what
// removed them, and the access-change signal names the Device Sessions it
// Revoked, as a Removed User's, so their SSH Sessions end and their Device
// Connections close.
func (r remover) removeJoined(ctx context.Context, tx store.Tx, u api.User, by string) (api.User, error) {
	now := op.Now(ctx).Format(time.RFC3339)
	revoked, err := revokeAll(ctx, tx, u, now)
	if err != nil {
		return api.User{}, err
	}
	voided, err := tx.ExecContext(ctx, `UPDATE join_codes SET voided_at = ?1
		WHERE (user_id = ?2 OR issued_by IN (SELECT id FROM device_sessions WHERE user_id = ?2))
		AND used_at IS NULL AND voided_at IS NULL AND expires_at > ?1`, now, u.ID)
	if err != nil {
		return api.User{}, fmt.Errorf("void the Join Codes of %s: %w", u.Name, err)
	}
	codes, err := voided.RowsAffected()
	if err != nil {
		return api.User{}, fmt.Errorf("count the Join Codes of %s: %w", u.Name, err)
	}
	signIns, links, err := r.browsers.Remove(ctx, tx, u, by)
	if err != nil {
		return api.User{}, err
	}
	if err := space.PassToOwner(ctx, tx, u); err != nil {
		return api.User{}, err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE users SET removed_at = ? WHERE id = ?", now, u.ID); err != nil {
		return api.User{}, fmt.Errorf("mark %s Removed: %w", u.Name, err)
	}
	ids := make([]string, 0, len(revoked))
	for _, d := range revoked {
		ids = append(ids, d.ID)
		r.afterCommit(ctx, "device revoked", "user", d.User, "device", d.Device, "device_session", d.ID, "revoked_by", by)
	}
	r.afterCommit(ctx, "user removed", "user", u.Name, "user_id", u.ID, "device_sessions_revoked", len(revoked),
		"join_codes_voided", codes, "signins_ended", signIns, "password_links_voided", links, "removed_by", by)
	r.changes.Publish(ctx, access.Change{Revoked: ids, Removed: true})
	u.RemovedAt = now
	return u, nil
}

// revokeAll revokes, at now, every live Device Session of u, and returns
// them by device name.
func revokeAll(ctx context.Context, tx store.Tx, u api.User, now string) ([]api.DeviceSessionRef, error) {
	rows, err := tx.QueryContext(ctx, `UPDATE device_sessions SET revoked_at = ?1
		WHERE user_id = ?2 AND revoked_at IS NULL RETURNING id, device`, now, u.ID)
	if err != nil {
		return nil, fmt.Errorf("revoke the Device Sessions of %s: %w", u.Name, err)
	}
	defer func() { _ = rows.Close() }()
	var revoked []api.DeviceSessionRef
	for rows.Next() {
		d := api.DeviceSessionRef{User: u.Name}
		if err := rows.Scan(&d.ID, &d.Device); err != nil {
			return nil, fmt.Errorf("revoke the Device Sessions of %s: %w", u.Name, err)
		}
		revoked = append(revoked, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("revoke the Device Sessions of %s: %w", u.Name, err)
	}
	slices.SortFunc(revoked, func(a, b api.DeviceSessionRef) int { return cmp.Compare(a.Device, b.Device) })
	return revoked, nil
}

// afterCommit logs msg with attrs once the call's transaction has committed,
// and never when it rolls back.
func (r remover) afterCommit(ctx context.Context, msg string, attrs ...any) {
	op.AfterTx(ctx, func(committed bool) {
		if committed {
			r.log.Info(msg, attrs...)
		}
	})
}

// delete deletes u, a User who never used koko, with their Personal Space,
// which must hold nothing, their Join Codes and their Password Links, which
// are all unused; their memberships go with them (ON DELETE CASCADE). Their name is free again.
// Once it commits, the deletion is logged, by being what deleted them.
func (r remover) delete(ctx context.Context, tx store.Tx, u api.User, by string) error {
	if err := space.DeletePersonal(ctx, tx, r.holdings, u); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM join_codes WHERE user_id = ?", u.ID); err != nil {
		return fmt.Errorf("delete the Join Codes of %s: %w", u.Name, err)
	}
	if err := r.browsers.Delete(ctx, tx, u); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM users WHERE id = ?", u.ID); err != nil {
		return fmt.Errorf("delete the User %s: %w", u.Name, err)
	}
	r.afterCommit(ctx, "user deleted", "user", u.Name, "user_id", u.ID, "deleted_by", by)
	return nil
}

// Active returns the User named userName, as ByName does, for a call that
// acts on them, such as issuing them a Join Code: a Removed User is refused
// with CodeUserRemoved.
func Active(ctx context.Context, tx store.Tx, userName string) (api.User, error) {
	u, err := ByName(ctx, tx, userName)
	if err != nil {
		return api.User{}, err
	}
	if u.RemovedAt != "" {
		return api.User{}, api.CodeUserRemoved.Errorf("%s was Removed, at %s", u.Name, u.RemovedAt)
	}
	return u, nil
}

// ByName returns the User named userName, Removed or not. A name that is
// not a User name is refused with CodeInvalidName, and one no User holds
// with CodeUnknownUser.
func ByName(ctx context.Context, tx store.Tx, userName string) (api.User, error) {
	if err := name.Check(userName); err != nil {
		return api.User{}, api.CodeInvalidName.Errorf("invalid User name: %v", err)
	}
	u, err := scan(tx.QueryRowContext(ctx, "SELECT id, name, role, created_at, removed_at FROM users WHERE name = ?", userName))
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return api.User{}, api.CodeUnknownUser.Errorf("no User is named %q", userName)
	case err != nil:
		return api.User{}, fmt.Errorf("read the User %q: %w", userName, err)
	}
	return u, nil
}

// scanner is a *sql.Row or *sql.Rows.
type scanner interface {
	Scan(dest ...any) error
}

// scan reads a User from the columns id, name, role, created_at and
// removed_at.
func scan(row scanner) (api.User, error) {
	var u api.User
	var removed sql.NullString
	err := row.Scan(&u.ID, &u.Name, &u.Role, &u.CreatedAt, &removed)
	u.RemovedAt = removed.String
	return u, err
}
