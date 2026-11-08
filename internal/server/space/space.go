// Package space is the space area of the koko server: the Spaces, shared or
// Personal, and their Members (P0 spec: people and Spaces). Who may act in a
// Space is the access check's (package access); what other areas record in
// a Space, such as Hosts and Secrets, each area counts through a Holding.
package space

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/cieons/koko/internal/server/access"
	"github.com/cieons/koko/internal/server/op"
	"github.com/cieons/koko/internal/server/store"
	"github.com/cieons/koko/internal/shared/api"
	"github.com/cieons/koko/internal/shared/name"
)

// idPrefix starts every Space's id, as in space_3f9k2m7q.
const idPrefix = "space"

// Holding counts, into its own fields of c, what one area records in the
// Space whose id is spaceID, such as its Hosts. Each area whose records live
// in a Space gives one, through the holdings list in package server, so that
// space show counts them and space rm refuses a Space while any count is
// above 0, without the space area reading another area's records. Both read
// the same counts: records space rm must keep a Space for but space show
// should count apart, such as Retired Hosts, take a field of their own.
type Holding func(ctx context.Context, tx store.Tx, spaceID string, c *api.SpaceCounts) error

// UserFunc returns the User named userName, Removed or not, refusing a name
// no User holds with CodeUnknownUser: the user area's user.ByName, which the
// space area cannot import, since every User gets a Personal Space.
type UserFunc func(ctx context.Context, tx store.Tx, userName string) (api.User, error)

// area serves the space operations.
type area struct {
	user     UserFunc
	holdings []Holding
	changes  *access.Signal
}

// Register adds the space operations to the API; user finds the Users a
// call names, holdings count what the other areas record in a Space, and
// each User taken out of a Space is announced on changes once it commits.
func Register(a *op.API, user UserFunc, holdings []Holding, changes *access.Signal) {
	s := &area{user: user, holdings: holdings, changes: changes}
	a.Handle(api.SpaceAdd, add)
	a.Handle(api.SpaceList, list)
	a.Handle(api.SpaceShow, s.show)
	a.Handle(api.SpaceRename, rename)
	a.Handle(api.SpaceRemove, s.remove)
	a.Handle(api.SpaceMemberAdd, s.memberAdd)
	a.Handle(api.SpaceMemberRemove, s.memberRemove)
}

func add(ctx context.Context, tx store.Tx, caller op.Caller, req api.SpaceAddRequest) (api.SpaceAddResponse, error) {
	if _, err := access.OwnerOnly(ctx, tx, caller, "adds Spaces"); err != nil {
		return api.SpaceAddResponse{}, err
	}
	if err := checkFree(ctx, tx, req.Name); err != nil {
		return api.SpaceAddResponse{}, err
	}
	s := api.Space{ID: store.NewID(idPrefix), Name: req.Name}
	if _, err := tx.ExecContext(ctx, "INSERT INTO spaces (id, name) VALUES (?, ?)", s.ID, s.Name); err != nil {
		return api.SpaceAddResponse{}, fmt.Errorf("record the Space %q: %w", s.Name, err)
	}
	return api.SpaceAddResponse{Space: s}, nil
}

// checkFree refuses a new Space name that is not a Space name, or that a
// Space holds. A Space's name is checked against Spaces only; Users and
// Spaces share their names through the check a new User's name gets.
func checkFree(ctx context.Context, tx store.Tx, spaceName string) error {
	if err := name.Check(spaceName); err != nil {
		return api.CodeInvalidName.Errorf("invalid Space name: %v", err)
	}
	var taken bool
	err := tx.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM spaces WHERE name = ?)", spaceName).Scan(&taken)
	if err != nil {
		return fmt.Errorf("check whether a Space is named %q: %w", spaceName, err)
	}
	if taken {
		return api.CodeNameTaken.Errorf("a Space named %q already exists", spaceName)
	}
	return nil
}

// rename is space.rename: a shared Space takes a new name and keeps its id,
// its Members and what it holds.
func rename(ctx context.Context, tx store.Tx, caller op.Caller, req api.SpaceRenameRequest) (api.SpaceRenameResponse, error) {
	who, err := access.OwnerOnly(ctx, tx, caller, "renames Spaces")
	if err != nil {
		return api.SpaceRenameResponse{}, err
	}
	sp, err := ByName(ctx, tx, who, req.Name)
	if err != nil {
		return api.SpaceRenameResponse{}, err
	}
	if sp.Personal {
		return api.SpaceRenameResponse{}, api.CodePersonalSpace.Errorf(
			"%s is a Personal Space, which keeps its User's name while its User exists", sp.Name)
	}
	if err := checkFree(ctx, tx, req.NewName); err != nil {
		return api.SpaceRenameResponse{}, err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE spaces SET name = ? WHERE id = ?", req.NewName, sp.ID); err != nil {
		return api.SpaceRenameResponse{}, fmt.Errorf("rename the Space %s to %s: %w", sp.Name, req.NewName, err)
	}
	sp.Name = req.NewName
	return api.SpaceRenameResponse{Space: sp}, nil
}

// remove is space.remove: an empty shared Space goes, with its memberships.
func (s *area) remove(ctx context.Context, tx store.Tx, caller op.Caller, req api.SpaceRemoveRequest) (api.SpaceRemoveResponse, error) {
	who, err := access.OwnerOnly(ctx, tx, caller, "removes Spaces")
	if err != nil {
		return api.SpaceRemoveResponse{}, err
	}
	sp, err := ByName(ctx, tx, who, req.Name)
	if err != nil {
		return api.SpaceRemoveResponse{}, err
	}
	if sp.Personal {
		return api.SpaceRemoveResponse{}, api.CodePersonalSpace.Errorf(
			"%s is a Personal Space, which stays while its User exists", sp.Name)
	}
	c, err := count(ctx, tx, s.holdings, sp)
	if err != nil {
		return api.SpaceRemoveResponse{}, err
	}
	if c != (api.SpaceCounts{}) {
		return api.SpaceRemoveResponse{}, api.CodeSpaceNotEmpty.Errorf(
			"the Space %s still holds %s: move or remove them first", sp.Name, held(c))
	}
	// The memberships go with the Space (ON DELETE CASCADE).
	if _, err := tx.ExecContext(ctx, "DELETE FROM spaces WHERE id = ?", sp.ID); err != nil {
		return api.SpaceRemoveResponse{}, fmt.Errorf("remove the Space %s: %w", sp.Name, err)
	}
	return api.SpaceRemoveResponse{Space: sp}, nil
}

// held says for people what the counts c hold, such as "2 Hosts and 1
// Secret".
func held(c api.SpaceCounts) string {
	var parts []string
	for _, n := range []struct {
		count        int
		one, several string
	}{
		{c.Hosts, "Host", "Hosts"},
		{c.RetiredHosts, "Retired Host", "Retired Hosts"},
		{c.Secrets, "Secret", "Secrets"},
	} {
		switch {
		case n.count == 1:
			parts = append(parts, "1 "+n.one)
		case n.count > 1:
			parts = append(parts, fmt.Sprintf("%d %s", n.count, n.several))
		}
	}
	return strings.Join(parts, " and ")
}

// AddPersonal records the Personal Space of the User whose id is userID,
// named after them. The User's own rules have checked the name, which Users
// and Spaces share.
func AddPersonal(ctx context.Context, tx store.Tx, userID, name string) error {
	_, err := tx.ExecContext(ctx, "INSERT INTO spaces (id, name, user_id) VALUES (?, ?, ?)", store.NewID(idPrefix), name, userID)
	if err != nil {
		return fmt.Errorf("record the Personal Space %q: %w", name, err)
	}
	return nil
}

func list(ctx context.Context, tx store.Tx, caller op.Caller, _ api.SpaceListRequest) (api.SpaceListResponse, error) {
	who, err := access.Who(ctx, tx, caller)
	if err != nil {
		return api.SpaceListResponse{}, err
	}
	scope, err := who.Spaces(ctx, tx)
	if err != nil {
		return api.SpaceListResponse{}, err
	}
	rows, err := tx.QueryContext(ctx, "SELECT id, name, user_id IS NOT NULL FROM spaces ORDER BY name")
	if err != nil {
		return api.SpaceListResponse{}, fmt.Errorf("list the Spaces: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var resp api.SpaceListResponse
	for rows.Next() {
		var s api.Space
		if err := rows.Scan(&s.ID, &s.Name, &s.Personal); err != nil {
			return api.SpaceListResponse{}, fmt.Errorf("list the Spaces: %w", err)
		}
		if scope.Has(s.ID) {
			resp.Spaces = append(resp.Spaces, s)
		}
	}
	if err := rows.Err(); err != nil {
		return api.SpaceListResponse{}, fmt.Errorf("list the Spaces: %w", err)
	}
	return resp, nil
}

func (s *area) show(ctx context.Context, tx store.Tx, caller op.Caller, req api.SpaceShowRequest) (api.SpaceShowResponse, error) {
	who, err := access.Who(ctx, tx, caller)
	if err != nil {
		return api.SpaceShowResponse{}, err
	}
	sp, err := ByName(ctx, tx, who, req.Name)
	if err != nil {
		return api.SpaceShowResponse{}, err
	}
	d, err := s.detail(ctx, tx, sp)
	if err != nil {
		return api.SpaceShowResponse{}, err
	}
	return api.SpaceShowResponse{Space: d}, nil
}

// ByName returns the Space named spaceName, as the caller who sees it: a
// Space who cannot act in is refused exactly as one that does not exist,
// with CodeUnknownSpace, so nothing outside a Member's Spaces shows through.
// A name that is not a Space name is refused with CodeInvalidName.
func ByName(ctx context.Context, tx store.Tx, who access.Identity, spaceName string) (api.Space, error) {
	if err := name.Check(spaceName); err != nil {
		return api.Space{}, api.CodeInvalidName.Errorf("invalid Space name: %v", err)
	}
	unknown := api.CodeUnknownSpace.Errorf("no Space is named %q", spaceName)
	var s api.Space
	err := tx.QueryRowContext(ctx, "SELECT id, name, user_id IS NOT NULL FROM spaces WHERE name = ?", spaceName).
		Scan(&s.ID, &s.Name, &s.Personal)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return api.Space{}, unknown
	case err != nil:
		return api.Space{}, fmt.Errorf("read the Space %q: %w", spaceName, err)
	}
	can, err := who.CanAct(ctx, tx, s.ID)
	switch {
	case err != nil:
		return api.Space{}, err
	case !can:
		return api.Space{}, unknown
	}
	return s, nil
}

// Personal returns the Personal Space of the caller's User, where what they
// record goes when they name no Space; the Owner's for the Local caller.
func Personal(ctx context.Context, tx store.Tx, who access.Identity) (api.Space, error) {
	s := api.Space{Personal: true}
	err := tx.QueryRowContext(ctx, "SELECT id, name FROM spaces WHERE user_id = ?", who.User.ID).Scan(&s.ID, &s.Name)
	if err != nil {
		return api.Space{}, fmt.Errorf("read the Personal Space of %s: %w", who.User.Name, err)
	}
	return s, nil
}

// memberAdd is space.member.add: it makes the Users Members of a shared
// Space, all or none, leaving a User who is a Member already as they are. A
// Removed User is made a Member of no Space.
func (s *area) memberAdd(ctx context.Context, tx store.Tx, caller op.Caller, req api.SpaceMemberAddRequest) (api.SpaceMemberAddResponse, error) {
	sp, users, err := s.members(ctx, tx, caller, req.Space, req.Users)
	if err != nil {
		return api.SpaceMemberAddResponse{}, err
	}
	if sp.Personal {
		return api.SpaceMemberAddResponse{}, api.CodePersonalSpace.Errorf(
			"%s is a Personal Space, which is never given Members while its User exists", sp.Name)
	}
	for _, u := range users {
		if u.RemovedAt != "" {
			return api.SpaceMemberAddResponse{}, api.CodeUserRemoved.Errorf("%s was Removed, at %s", u.Name, u.RemovedAt)
		}
	}
	for _, u := range users {
		_, err := tx.ExecContext(ctx, "INSERT INTO memberships (space_id, user_id) VALUES (?, ?) ON CONFLICT DO NOTHING", sp.ID, u.ID)
		if err != nil {
			return api.SpaceMemberAddResponse{}, fmt.Errorf("make %s a Member of %s: %w", u.Name, sp.Name, err)
		}
	}
	d, err := s.detail(ctx, tx, sp)
	return api.SpaceMemberAddResponse{Space: d}, err
}

// memberRemove is space.member.remove: it takes the Users out of a Space,
// all or none, leaving a User who is not a Member as they are, as a Removed
// User never is. Once it commits, the access-change signal names the live
// Device Sessions of each User it took out, so that their SSH Sessions to
// the Space's Hosts end.
func (s *area) memberRemove(ctx context.Context, tx store.Tx, caller op.Caller, req api.SpaceMemberRemoveRequest) (api.SpaceMemberRemoveResponse, error) {
	sp, users, err := s.members(ctx, tx, caller, req.Space, req.Users)
	if err != nil {
		return api.SpaceMemberRemoveResponse{}, err
	}
	var narrowed []string
	for _, u := range users {
		res, err := tx.ExecContext(ctx, "DELETE FROM memberships WHERE space_id = ? AND user_id = ?", sp.ID, u.ID)
		if err != nil {
			return api.SpaceMemberRemoveResponse{}, fmt.Errorf("take %s out of %s: %w", u.Name, sp.Name, err)
		}
		taken, err := res.RowsAffected()
		if err != nil {
			return api.SpaceMemberRemoveResponse{}, fmt.Errorf("take %s out of %s: %w", u.Name, sp.Name, err)
		}
		if taken == 0 {
			continue
		}
		live, err := access.DeviceSessions(ctx, tx, "u.id = ? AND d.revoked_at IS NULL", u.ID)
		if err != nil {
			return api.SpaceMemberRemoveResponse{}, err
		}
		for _, d := range live {
			narrowed = append(narrowed, d.ID)
		}
	}
	if len(narrowed) > 0 {
		s.changes.Publish(ctx, access.Change{Narrowed: narrowed})
	}
	d, err := s.detail(ctx, tx, sp)
	return api.SpaceMemberRemoveResponse{Space: d}, err
}

// members checks a call that changes the Members of the Space named
// spaceName: only the Owner makes it, and every User it names must exist,
// so that a refusal changes nothing. It returns the Space and the Users,
// each once.
func (s *area) members(ctx context.Context, tx store.Tx, caller op.Caller, spaceName string, userNames []string) (api.Space, []api.User, error) {
	who, err := access.OwnerOnly(ctx, tx, caller, "changes the Members of a Space")
	if err != nil {
		return api.Space{}, nil, err
	}
	sp, err := ByName(ctx, tx, who, spaceName)
	if err != nil {
		return api.Space{}, nil, err
	}
	if len(userNames) == 0 {
		return api.Space{}, nil, api.CodeInvalidRequest.Errorf("name at least one User")
	}
	var users []api.User
	for _, n := range userNames {
		u, err := s.user(ctx, tx, n)
		if err != nil {
			return api.Space{}, nil, err
		}
		if !slices.ContainsFunc(users, func(v api.User) bool { return v.ID == u.ID }) {
			users = append(users, u)
		}
	}
	return sp, users, nil
}

// detail returns the Space sp with its Members, by name, and what the areas
// count in it.
func (s *area) detail(ctx context.Context, tx store.Tx, sp api.Space) (api.SpaceDetail, error) {
	d := api.SpaceDetail{ID: sp.ID, Name: sp.Name, Personal: sp.Personal}
	rows, err := tx.QueryContext(ctx, `SELECT u.id, u.name FROM memberships m JOIN users u ON u.id = m.user_id
		WHERE m.space_id = ? ORDER BY u.name`, sp.ID)
	if err != nil {
		return api.SpaceDetail{}, fmt.Errorf("read the Members of %s: %w", sp.Name, err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var m api.SpaceMember
		if err := rows.Scan(&m.ID, &m.Name); err != nil {
			return api.SpaceDetail{}, fmt.Errorf("read the Members of %s: %w", sp.Name, err)
		}
		d.Members = append(d.Members, m)
	}
	if err := rows.Err(); err != nil {
		return api.SpaceDetail{}, fmt.Errorf("read the Members of %s: %w", sp.Name, err)
	}
	if d.Counts, err = count(ctx, tx, s.holdings, sp); err != nil {
		return api.SpaceDetail{}, err
	}
	return d, nil
}

// count asks each of holdings how many of its records the Space sp holds.
func count(ctx context.Context, tx store.Tx, holdings []Holding, sp api.Space) (api.SpaceCounts, error) {
	var c api.SpaceCounts
	for _, h := range holdings {
		if err := h(ctx, tx, sp.ID, &c); err != nil {
			return api.SpaceCounts{}, fmt.Errorf("count what the Space %s holds: %w", sp.Name, err)
		}
	}
	return c, nil
}

// DeletePersonal deletes the Personal Space of u, a User who never joined
// and is being deleted, refusing with CodeSpaceNotEmpty while it holds any
// record that holdings count, since deleting the User must lose nothing.
func DeletePersonal(ctx context.Context, tx store.Tx, holdings []Holding, u api.User) error {
	sp := api.Space{Name: u.Name, Personal: true}
	if err := tx.QueryRowContext(ctx, "SELECT id FROM spaces WHERE user_id = ?", u.ID).Scan(&sp.ID); err != nil {
		return fmt.Errorf("read the Personal Space of %s: %w", u.Name, err)
	}
	c, err := count(ctx, tx, holdings, sp)
	if err != nil {
		return err
	}
	if c != (api.SpaceCounts{}) {
		return api.CodeSpaceNotEmpty.Errorf(
			"%s never joined, so removing them deletes their Personal Space, which still holds %s: move or remove them first",
			u.Name, held(c))
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM spaces WHERE id = ?", sp.ID); err != nil {
		return fmt.Errorf("delete the Personal Space of %s: %w", u.Name, err)
	}
	return nil
}

// PassToOwner takes u, a User being Removed, out of every Space, and turns
// their Personal Space into an ordinary shared Space with no Members, which
// only the Owner sees: it keeps its name and what it holds, and the Owner
// can rename it, give it Members and remove it once it is empty.
func PassToOwner(ctx context.Context, tx store.Tx, u api.User) error {
	if _, err := tx.ExecContext(ctx, "DELETE FROM memberships WHERE user_id = ?", u.ID); err != nil {
		return fmt.Errorf("take %s out of every Space: %w", u.Name, err)
	}
	if _, err := tx.ExecContext(ctx, "UPDATE spaces SET user_id = NULL WHERE user_id = ?", u.ID); err != nil {
		return fmt.Errorf("pass the Personal Space of %s to the Owner: %w", u.Name, err)
	}
	return nil
}
