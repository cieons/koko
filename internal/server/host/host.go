// Package host is the host area of the koko server: the Hosts, each in one
// Space, with their Attributes and Notes (P0 spec: the record). Space
// membership decides who sees and acts on a Host (package access): a Host
// in a Space the caller cannot act in answers exactly as one that does not
// exist. The value rules are package record's, which the CLI shares.
package host

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/cieons/koko/internal/server/access"
	"github.com/cieons/koko/internal/server/op"
	"github.com/cieons/koko/internal/server/space"
	"github.com/cieons/koko/internal/server/store"
	"github.com/cieons/koko/internal/shared/api"
	"github.com/cieons/koko/internal/shared/name"
	"github.com/cieons/koko/internal/shared/record"
)

// idPrefix starts every Host's id, as in host_7k2m9q4x. No Host name holds
// '_', so a Host id stands wherever a Host name does.
const idPrefix = "host"

// Register adds the host operations to the API.
func Register(a *op.API) {
	a.Handle(api.HostAdd, add)
	a.Handle(api.HostList, list)
	a.Handle(api.HostShow, show)
	a.Handle(api.HostSet, set)
	a.Handle(api.HostRename, rename)
	a.Handle(api.HostMove, move)
	a.Handle(api.HostRemove, remove)
}

// Holding counts a Space's Hosts for space show and space rm: those that
// are not Retired as hosts, and the Retired ones as retired_hosts.
func Holding(ctx context.Context, tx store.Tx, spaceID string, c *api.SpaceCounts) error {
	err := tx.QueryRowContext(ctx, `SELECT count(*) FILTER (WHERE state <> 'retired'), count(*) FILTER (WHERE state = 'retired')
		FROM hosts WHERE space_id = ?`, spaceID).Scan(&c.Hosts, &c.RetiredHosts)
	if err != nil {
		return fmt.Errorf("count the Hosts: %w", err)
	}
	return nil
}

// stored is a Host as stored: the API's Host, and the id of its Space.
type stored struct {
	api.Host
	spaceID string
}

// selectHosts reads the Hosts, each with the name of its Space, in the order
// scan reads them.
const selectHosts = `SELECT h.id, h.name, s.name, h.space_id, h.address, h.port, h.account, h.state, h.host_key,
	coalesce(h.note, ''), h.created_at, h.updated_at FROM hosts h JOIN spaces s ON s.id = h.space_id`

// scan reads one Host that selectHosts selected, without its Attributes.
func scan(row interface{ Scan(...any) error }) (stored, error) {
	var h stored
	var key []byte
	err := row.Scan(&h.ID, &h.Name, &h.Space, &h.spaceID, &h.Address, &h.Port, &h.Account, &h.State, &key,
		&h.Note, &h.CreatedAt, &h.UpdatedAt)
	if err != nil {
		return stored{}, err
	}
	if key != nil {
		pub, err := ssh.ParsePublicKey(key)
		if err != nil {
			return stored{}, fmt.Errorf("read the pinned host key of %s: %w", h.ID, err)
		}
		h.HostKey = new(ssh.FingerprintSHA256(pub))
	}
	h.Attributes = map[string]string{}
	return h, nil
}

// target answers who is calling and returns the Host ref names, as find
// does: the start of every operation on one Host.
func target(ctx context.Context, tx store.Tx, caller op.Caller, ref string) (access.Identity, stored, error) {
	who, err := access.Who(ctx, tx, caller)
	if err != nil {
		return access.Identity{}, stored{}, err
	}
	h, err := find(ctx, tx, who, ref)
	return who, h, err
}

// find returns the Host ref names, by its name, which names only a Host that
// is not Retired, or by its id. A Host in a Space the caller cannot act in
// is refused exactly as one that does not exist, with CodeUnknownHost.
func find(ctx context.Context, tx store.Tx, who access.Identity, ref string) (stored, error) {
	where, unknown := "h.id = ?", api.CodeUnknownHost.Errorf("no Host has the id %q", ref)
	if !strings.HasPrefix(ref, idPrefix+"_") {
		if err := name.Check(ref); err != nil {
			return stored{}, api.CodeInvalidName.Errorf("%q is neither a Host name nor a Host id: %v", ref, err)
		}
		where, unknown = "h.name = ? AND h.state <> 'retired'", api.CodeUnknownHost.Errorf("no Host is named %q", ref)
	}
	h, err := scan(tx.QueryRowContext(ctx, selectHosts+" WHERE "+where, ref))
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return stored{}, unknown
	case err != nil:
		return stored{}, fmt.Errorf("read the Host %q: %w", ref, err)
	}
	can, err := who.CanAct(ctx, tx, h.spaceID)
	switch {
	case err != nil:
		return stored{}, err
	case !can:
		return stored{}, unknown
	}
	rows, err := tx.QueryContext(ctx, "SELECT key, value FROM host_attributes WHERE host_id = ?", h.ID)
	if err != nil {
		return stored{}, fmt.Errorf("read the Attributes of %s: %w", h.Name, err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return stored{}, fmt.Errorf("read the Attributes of %s: %w", h.Name, err)
		}
		h.Attributes[k] = v
	}
	if err := rows.Err(); err != nil {
		return stored{}, fmt.Errorf("read the Attributes of %s: %w", h.Name, err)
	}
	return h, nil
}

// values are the fields, Attributes and Note a write gives a Host, each nil
// or empty when the write leaves it as it is.
type values struct {
	address    *string
	port       *int
	account    *string
	attributes map[string]string
	note       *string
}

// check refuses, for the Host named hostName, a value that breaks its rule,
// and puts the address in the form koko stores.
func (v *values) check(hostName string) error {
	if v.address != nil {
		address, err := record.Address(*v.address)
		if err != nil {
			return invalid(hostName, err)
		}
		v.address = &address
	}
	if v.port != nil {
		if err := record.CheckPort(*v.port); err != nil {
			return invalid(hostName, err)
		}
	}
	if v.account != nil {
		if err := record.CheckAccount(*v.account); err != nil {
			return invalid(hostName, err)
		}
	}
	for _, k := range slices.Sorted(maps.Keys(v.attributes)) {
		if err := record.CheckKey(k); err != nil {
			return invalid(hostName, err)
		}
		if err := record.CheckText(v.attributes[k]); err != nil {
			return invalid(hostName, fmt.Errorf("the Attribute %s: %w", k, err))
		}
	}
	if v.note != nil {
		if err := record.CheckNote(*v.note); err != nil {
			return invalid(hostName, err)
		}
	}
	return nil
}

// invalid is the refusal of a value of the Host named hostName.
func invalid(hostName string, err error) error {
	return api.CodeInvalidValue.Errorf("the Host %s: %v", hostName, err)
}

// add is host.add: it records the Hosts, all or none.
func add(ctx context.Context, tx store.Tx, caller op.Caller, req api.HostAddRequest) (api.HostAddResponse, error) {
	who, err := access.Who(ctx, tx, caller)
	if err != nil {
		return api.HostAddResponse{}, err
	}
	if len(req.Hosts) == 0 {
		return api.HostAddResponse{}, api.CodeInvalidRequest.Errorf("name at least one Host to add")
	}
	resp := api.HostAddResponse{Hosts: make([]api.Host, 0, len(req.Hosts))}
	for _, n := range req.Hosts {
		h, err := insert(ctx, tx, who, n)
		if err != nil {
			return api.HostAddResponse{}, err
		}
		resp.Hosts = append(resp.Hosts, h)
	}
	return resp, nil
}

// insert records one new Host, Registered, in the Space it names or the
// caller's Personal Space.
func insert(ctx context.Context, tx store.Tx, who access.Identity, n api.NewHost) (api.Host, error) {
	if err := name.Check(n.Name); err != nil {
		return api.Host{}, api.CodeInvalidName.Errorf("invalid Host name: %v", err)
	}
	sp, err := spaceFor(ctx, tx, who, n.Space)
	if err != nil {
		return api.Host{}, err
	}
	v := values{address: &n.Address, port: n.Port, account: &n.Account, attributes: n.Attributes, note: n.Note}
	if v.port == nil {
		v.port = new(record.DefaultPort)
	}
	if err := v.check(n.Name); err != nil {
		return api.Host{}, err
	}
	if err := checkFree(ctx, tx, n.Name); err != nil {
		return api.Host{}, err
	}
	now := op.Now(ctx).Format(time.RFC3339)
	h := api.Host{
		ID: store.NewID(idPrefix), Name: n.Name, Space: sp.Name, Address: *v.address, Port: *v.port, Account: *v.account,
		State: api.HostRegistered, Attributes: map[string]string{}, CreatedAt: now, UpdatedAt: now,
	}
	var note sql.NullString
	if v.note != nil {
		h.Note, note = *v.note, sql.NullString{String: *v.note, Valid: true}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO hosts (id, name, space_id, address, port, account, state, note, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, h.ID, h.Name, sp.ID, h.Address, h.Port, h.Account, h.State, note, now, now)
	if err != nil {
		return api.Host{}, fmt.Errorf("record the Host %s: %w", h.Name, err)
	}
	if err := setAttributes(ctx, tx, h.ID, v.attributes); err != nil {
		return api.Host{}, err
	}
	maps.Copy(h.Attributes, v.attributes)
	return h, nil
}

// spaceFor returns the Space a new Host goes to: the one named, which the
// caller must be able to act in, or the caller's Personal Space.
func spaceFor(ctx context.Context, tx store.Tx, who access.Identity, spaceName *string) (api.Space, error) {
	if spaceName == nil {
		return space.Personal(ctx, tx, who)
	}
	return space.ByName(ctx, tx, who, *spaceName)
}

// checkFree refuses a name that a Host that is not Retired holds, in any
// Space, inside the write's transaction. The refusal is the same whichever
// Space holds the name and tells nothing more: the one leak of ADR 0014.
func checkFree(ctx context.Context, tx store.Tx, hostName string) error {
	var taken bool
	err := tx.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM hosts WHERE name = ? AND state <> 'retired')", hostName).Scan(&taken)
	if err != nil {
		return fmt.Errorf("check whether a Host is named %q: %w", hostName, err)
	}
	if taken {
		return api.CodeNameTaken.Errorf("a Host named %q already exists", hostName)
	}
	return nil
}

// setAttributes gives the Host whose id is hostID the Attributes attrs,
// replacing the value of a key it has.
func setAttributes(ctx context.Context, tx store.Tx, hostID string, attrs map[string]string) error {
	for k, v := range attrs {
		_, err := tx.ExecContext(ctx, `INSERT INTO host_attributes (host_id, key, value) VALUES (?, ?, ?)
			ON CONFLICT (host_id, key) DO UPDATE SET value = excluded.value`, hostID, k, v)
		if err != nil {
			return fmt.Errorf("set the Attribute %s of %s: %w", k, hostID, err)
		}
	}
	return nil
}

// list is host.list: the Hosts that are not Retired in the Spaces the caller
// can act in, by name, with their Notes only when asked for.
func list(ctx context.Context, tx store.Tx, caller op.Caller, req api.HostListRequest) (api.HostListResponse, error) {
	who, err := access.Who(ctx, tx, caller)
	if err != nil {
		return api.HostListResponse{}, err
	}
	scope, err := who.Spaces(ctx, tx)
	if err != nil {
		return api.HostListResponse{}, err
	}
	rows, err := tx.QueryContext(ctx, selectHosts+" WHERE h.state <> 'retired' ORDER BY h.name, h.id")
	if err != nil {
		return api.HostListResponse{}, fmt.Errorf("list the Hosts: %w", err)
	}
	defer func() { _ = rows.Close() }()
	resp := api.HostListResponse{Hosts: []api.Host{}}
	for rows.Next() {
		h, err := scan(rows)
		if err != nil {
			return api.HostListResponse{}, fmt.Errorf("list the Hosts: %w", err)
		}
		if !scope.Has(h.spaceID) {
			continue
		}
		if !req.WithNote {
			h.Note = ""
		}
		resp.Hosts = append(resp.Hosts, h.Host)
	}
	if err := rows.Err(); err != nil {
		return api.HostListResponse{}, fmt.Errorf("list the Hosts: %w", err)
	}
	byID := map[string]*api.Host{}
	for i := range resp.Hosts {
		byID[resp.Hosts[i].ID] = &resp.Hosts[i]
	}
	attrs, err := tx.QueryContext(ctx, "SELECT host_id, key, value FROM host_attributes")
	if err != nil {
		return api.HostListResponse{}, fmt.Errorf("list the Attributes of the Hosts: %w", err)
	}
	defer func() { _ = attrs.Close() }()
	for attrs.Next() {
		var id, k, v string
		if err := attrs.Scan(&id, &k, &v); err != nil {
			return api.HostListResponse{}, fmt.Errorf("list the Attributes of the Hosts: %w", err)
		}
		if h, ok := byID[id]; ok {
			h.Attributes[k] = v
		}
	}
	if err := attrs.Err(); err != nil {
		return api.HostListResponse{}, fmt.Errorf("list the Attributes of the Hosts: %w", err)
	}
	return resp, nil
}

func show(ctx context.Context, tx store.Tx, caller op.Caller, req api.HostShowRequest) (api.HostShowResponse, error) {
	_, h, err := target(ctx, tx, caller, req.Host)
	return api.HostShowResponse{Host: h.Host}, err
}

// set is host.set: it changes the fields, Attributes and Note the request
// names, and only those.
func set(ctx context.Context, tx store.Tx, caller op.Caller, req api.HostSetRequest) (api.HostSetResponse, error) {
	who, h, err := target(ctx, tx, caller, req.Host)
	if err != nil {
		return api.HostSetResponse{}, err
	}
	v := values{address: req.Address, port: req.Port, account: req.Account, attributes: req.Attributes, note: req.Note}
	if v.address == nil && v.port == nil && v.account == nil && len(v.attributes) == 0 && v.note == nil && len(req.Unset) == 0 {
		return api.HostSetResponse{}, api.CodeInvalidRequest.Errorf("name at least one field, Attribute or Note of %s to change", h.Name)
	}
	if err := v.check(h.Name); err != nil {
		return api.HostSetResponse{}, err
	}
	var cleared []string
	for _, k := range req.Unset {
		_, setToo := req.Attributes[k]
		switch {
		case setToo, k == "note" && req.Note != nil, k == "port" && req.Port != nil:
			return api.HostSetResponse{}, api.CodeInvalidRequest.Errorf("%s is both set and cleared", k)
		case k == "note":
			h.Note = ""
		case k == "port":
			v.port = new(record.DefaultPort)
		case slices.Contains(record.Required, k):
			return api.HostSetResponse{}, invalid(h.Name, fmt.Errorf("the %s cannot be cleared: a Host always has one", k))
		case record.Reserved(k):
			return api.HostSetResponse{}, invalid(h.Name, fmt.Errorf("%q is a field host.set does not clear", k))
		default:
			if err := record.CheckKey(k); err != nil {
				return api.HostSetResponse{}, invalid(h.Name, err)
			}
			cleared = append(cleared, k)
		}
	}
	if v.address != nil {
		h.Address = *v.address
	}
	if v.port != nil {
		h.Port = *v.port
	}
	if v.account != nil {
		h.Account = *v.account
	}
	if v.note != nil {
		h.Note = *v.note
	}

	_, err = tx.ExecContext(ctx, "UPDATE hosts SET address = ?, port = ?, account = ?, note = ?, updated_at = ? WHERE id = ?",
		h.Address, h.Port, h.Account, sql.NullString{String: h.Note, Valid: h.Note != ""}, op.Now(ctx).Format(time.RFC3339), h.ID)
	if err != nil {
		return api.HostSetResponse{}, fmt.Errorf("change the Host %s: %w", h.Name, err)
	}
	if err := setAttributes(ctx, tx, h.ID, v.attributes); err != nil {
		return api.HostSetResponse{}, err
	}
	for _, k := range cleared {
		if _, err := tx.ExecContext(ctx, "DELETE FROM host_attributes WHERE host_id = ? AND key = ?", h.ID, k); err != nil {
			return api.HostSetResponse{}, fmt.Errorf("clear the Attribute %s of %s: %w", k, h.Name, err)
		}
	}
	changed, err := find(ctx, tx, who, h.ID)
	return api.HostSetResponse{Host: changed.Host}, err
}

// rename is host.rename: the Host takes a new name, its SSH Name, and keeps
// its id and everything else.
func rename(ctx context.Context, tx store.Tx, caller op.Caller, req api.HostRenameRequest) (api.HostRenameResponse, error) {
	who, h, err := target(ctx, tx, caller, req.Host)
	if err != nil {
		return api.HostRenameResponse{}, err
	}
	if err := name.Check(req.NewName); err != nil {
		return api.HostRenameResponse{}, api.CodeInvalidName.Errorf("invalid Host name: %v", err)
	}
	if err := checkFree(ctx, tx, req.NewName); err != nil {
		return api.HostRenameResponse{}, err
	}
	_, err = tx.ExecContext(ctx, "UPDATE hosts SET name = ?, updated_at = ? WHERE id = ?", req.NewName, op.Now(ctx).Format(time.RFC3339), h.ID)
	if err != nil {
		return api.HostRenameResponse{}, fmt.Errorf("rename the Host %s to %s: %w", h.Name, req.NewName, err)
	}
	renamed, err := find(ctx, tx, who, h.ID)
	return api.HostRenameResponse{Host: renamed.Host}, err
}

// move is host.move: the Host goes to another Space with its Attributes and
// Note, keeping its name. The caller must be able to act in both Spaces.
func move(ctx context.Context, tx store.Tx, caller op.Caller, req api.HostMoveRequest) (api.HostMoveResponse, error) {
	who, h, err := target(ctx, tx, caller, req.Host)
	if err != nil {
		return api.HostMoveResponse{}, err
	}
	to, err := space.ByName(ctx, tx, who, req.Space)
	if err != nil {
		return api.HostMoveResponse{}, err
	}
	if to.ID != h.spaceID {
		_, err := tx.ExecContext(ctx, "UPDATE hosts SET space_id = ?, updated_at = ? WHERE id = ?", to.ID, op.Now(ctx).Format(time.RFC3339), h.ID)
		if err != nil {
			return api.HostMoveResponse{}, fmt.Errorf("move the Host %s to %s: %w", h.Name, to.Name, err)
		}
	}
	moved, err := find(ctx, tx, who, h.ID)
	return api.HostMoveResponse{Host: moved.Host}, err
}

// remove is host.remove: the Host goes, with its Attributes and Note.
func remove(ctx context.Context, tx store.Tx, caller op.Caller, req api.HostRemoveRequest) (api.HostRemoveResponse, error) {
	_, h, err := target(ctx, tx, caller, req.Host)
	if err != nil {
		return api.HostRemoveResponse{}, err
	}
	// Its Attributes go with it (ON DELETE CASCADE).
	if _, err := tx.ExecContext(ctx, "DELETE FROM hosts WHERE id = ?", h.ID); err != nil {
		return api.HostRemoveResponse{}, fmt.Errorf("delete the Host %s: %w", h.Name, err)
	}
	return api.HostRemoveResponse{Host: h.Host}, nil
}
