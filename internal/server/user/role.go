package user

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/cieons/koko/internal/server/access"
	"github.com/cieons/koko/internal/server/hostlist"
	"github.com/cieons/koko/internal/server/op"
	"github.com/cieons/koko/internal/server/store"
	"github.com/cieons/koko/internal/shared/api"
)

// roles answers user.promote and user.demote (ADR 0056), announcing each
// demotion on changes, and each change of role on lists, since it changes
// the Hosts the User sees, and logging it to log, once it commits.
type roles struct {
	browsers Browsers
	changes  *access.Signal
	lists    *hostlist.Signal
	log      *slog.Logger
}

// promote is user.promote: in one transaction it makes a Member an
// Administrator and voids their unused Join Codes and Password Links issued
// by anyone but the Owner and the User themselves, so that a code an
// Administrator prepared for them never becomes an Administrator's. Once
// it commits, the host-list signal names the User, whose devices then see
// every Host by its bare name. Only
// the Owner promotes, and anyone else is refused before the name is read.
// An Administrator is answered as they are, and nothing is logged.
func (r roles) promote(ctx context.Context, tx store.Tx, caller op.Caller, req api.UserPromoteRequest) (api.UserPromoteResponse, error) {
	who, err := access.OwnerOnly(ctx, tx, caller, "promotes Users")
	if err != nil {
		return api.UserPromoteResponse{}, err
	}
	u, err := roleTarget(ctx, tx, req.Name)
	if err != nil {
		return api.UserPromoteResponse{}, err
	}
	if u.Role == api.RoleMember {
		now := op.Now(ctx).Format(time.RFC3339)
		_, err := tx.ExecContext(ctx, "INSERT INTO administrators (user_id, promoted_at, promoted_by) VALUES (?, ?, ?)",
			u.ID, now, who.Issuer())
		if err != nil {
			return api.UserPromoteResponse{}, fmt.Errorf("make %s an Administrator: %w", u.Name, err)
		}
		codes, err := voidCodes(ctx, tx, now, access.IssuedForUserByOthers, u)
		if err != nil {
			return api.UserPromoteResponse{}, err
		}
		links, err := r.browsers.VoidLinksOthersIssuedFor(ctx, tx, u)
		if err != nil {
			return api.UserPromoteResponse{}, err
		}
		afterCommit(ctx, r.log, "user promoted", "user", u.Name, "user_id", u.ID, "promoted_by", who.By(),
			"join_codes_voided", codes, "password_links_voided", links)
		r.lists.Publish(ctx, hostlist.Change{Users: []string{u.ID}})
	}
	u, err = listedOne(ctx, tx, u)
	return api.UserPromoteResponse{User: u}, err
}

// demote is user.demote: in one transaction it makes an Administrator a
// Member again and voids the unused Join Codes and Password Links they
// issued for other Users, with rights they no longer hold. Once it commits,
// the access-change signal names their live Device Sessions and Sign-ins,
// so that their SSH Sessions to Hosts outside the Spaces they still act in
// end; their memberships stay. Only the Owner demotes, and anyone else is
// refused before the name is read. A Member is answered as they are, and
// nothing is logged.
func (r roles) demote(ctx context.Context, tx store.Tx, caller op.Caller, req api.UserDemoteRequest) (api.UserDemoteResponse, error) {
	who, err := access.OwnerOnly(ctx, tx, caller, "demotes Administrators")
	if err != nil {
		return api.UserDemoteResponse{}, err
	}
	u, err := roleTarget(ctx, tx, req.Name)
	if err != nil {
		return api.UserDemoteResponse{}, err
	}
	if u.Role == api.RoleAdministrator {
		now := op.Now(ctx)
		if _, err := tx.ExecContext(ctx, "DELETE FROM administrators WHERE user_id = ?", u.ID); err != nil {
			return api.UserDemoteResponse{}, fmt.Errorf("make %s a Member: %w", u.Name, err)
		}
		codes, err := voidCodes(ctx, tx, now.Format(time.RFC3339), access.IssuedByUserForOthers, u)
		if err != nil {
			return api.UserDemoteResponse{}, err
		}
		links, err := r.browsers.VoidLinksIssuedBy(ctx, tx, u)
		if err != nil {
			return api.UserDemoteResponse{}, err
		}
		afterCommit(ctx, r.log, "user demoted", "user", u.Name, "user_id", u.ID, "demoted_by", who.By(),
			"join_codes_voided", codes, "password_links_voided", links)
		r.changes.Publish(ctx, access.Change{Narrowed: []string{u.ID}})
		r.lists.Publish(ctx, hostlist.Change{Users: []string{u.ID}})
	}
	u, err = listedOne(ctx, tx, u)
	return api.UserDemoteResponse{User: u}, err
}

// roleTarget returns the User named userName for a call that changes their
// role: one no User holds is refused with CodeUnknownUser, a Removed User
// with CodeUserRemoved, and the Owner, whose role never changes, with
// CodeOwnerNotRemovable.
func roleTarget(ctx context.Context, tx store.Tx, userName string) (api.User, error) {
	u, err := ByName(ctx, tx, userName)
	switch {
	case err != nil:
		return api.User{}, err
	case u.Role == api.RoleOwner:
		return api.User{}, api.CodeOwnerNotRemovable.Errorf("%s is the Owner, whose role never changes", u.Name)
	case u.RemovedAt != "":
		return api.User{}, api.CodeUserRemoved.Errorf("%s was Removed, at %s", u.Name, u.RemovedAt)
	}
	return u, nil
}

// listedOne returns u as user.list answers them now.
func listedOne(ctx context.Context, tx store.Tx, u api.User) (api.User, error) {
	users, err := listed(ctx, tx, "u.id = ?", u.ID)
	if err != nil {
		return api.User{}, err
	}
	if len(users) != 1 {
		return api.User{}, fmt.Errorf("read the User %s again: found %d", u.Name, len(users))
	}
	return users[0], nil
}

// voidCodes voids, at now, the valid Join Codes the SQL condition where
// selects, which reads u's id as ?2, and counts them.
func voidCodes(ctx context.Context, tx store.Tx, now, where string, u api.User) (int64, error) {
	res, err := tx.ExecContext(ctx, `UPDATE join_codes SET voided_at = ?1
		WHERE `+where+` AND used_at IS NULL AND voided_at IS NULL AND expires_at > ?1`, now, u.ID)
	if err != nil {
		return 0, fmt.Errorf("void the Join Codes for the change of role of %s: %w", u.Name, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("count the Join Codes for the change of role of %s: %w", u.Name, err)
	}
	return n, nil
}
