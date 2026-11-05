// Package host is the host area of the koko server: the Hosts, each in one
// Space, with their Attributes and Notes (P0 spec: the record). Space
// membership decides who sees and acts on a Host (package access): a Host
// in a Space the caller cannot act in answers exactly as one that does not
// exist. The value rules are package record's, which the CLI shares.
package host

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/cieons/koko/internal/server/access"
	"github.com/cieons/koko/internal/server/op"
	"github.com/cieons/koko/internal/server/query"
	"github.com/cieons/koko/internal/server/space"
	"github.com/cieons/koko/internal/server/store"
	"github.com/cieons/koko/internal/shared/api"
	"github.com/cieons/koko/internal/shared/name"
	"github.com/cieons/koko/internal/shared/record"
)

// idPrefix starts every Host's id, as in host_7k2m9q4x. No Host name holds
// '_', so a Host id stands wherever a Host name does.
const idPrefix = "host"

// ServicesOf lists the Services on the Host h, by name, with their ports and
// Attributes and without their Notes, for host.show. The service area gives
// it, since it imports this one.
type ServicesOf func(ctx context.Context, tx store.Tx, h api.Host) ([]api.Service, error)

// Register adds the host operations to the API; services lists a Host's
// Services for host.show.
func Register(a *op.API, services ServicesOf) {
	a.Handle(api.HostAdd, add)
	a.Handle(api.HostList, list)
	a.Handle(api.HostShow, func(ctx context.Context, tx store.Tx, caller op.Caller, req api.HostShowRequest) (api.HostShowResponse, error) {
		return show(ctx, tx, caller, req, services)
	})
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

// AttributeKeys lists the Attribute keys in use on the Hosts in the Spaces
// scope holds, Retired ones among them, for attr.list.
func AttributeKeys(ctx context.Context, tx store.Tx, scope access.Scope) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT DISTINCT h.space_id, a.key FROM host_attributes a JOIN hosts h ON h.id = a.host_id`)
	if err != nil {
		return nil, fmt.Errorf("list the Attribute keys of the Hosts: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var keys []string
	for rows.Next() {
		var spaceID, key string
		if err := rows.Scan(&spaceID, &key); err != nil {
			return nil, fmt.Errorf("list the Attribute keys of the Hosts: %w", err)
		}
		if scope.Has(spaceID) {
			keys = append(keys, key)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list the Attribute keys of the Hosts: %w", err)
	}
	return keys, nil
}

// Find returns the Host ref names, by its name or its id, as every host
// operation finds one: a Host in a Space the caller cannot act in is refused
// exactly as one that does not exist, with CodeUnknownHost. A Service lives
// in its Host's Space, so the service area finds a Service's Host here.
func Find(ctx context.Context, tx store.Tx, who access.Identity, ref string) (api.Host, error) {
	h, err := find(ctx, tx, who, ref)
	return h.Host, err
}

// Names returns the name of each Host in the Spaces scope holds, by id,
// Retired ones among them, for the service area's lists: a Service is seen
// by whoever sees its Host.
func Names(ctx context.Context, tx store.Tx, scope access.Scope) (map[string]string, error) {
	rows, err := tx.QueryContext(ctx, "SELECT id, name, space_id FROM hosts")
	if err != nil {
		return nil, fmt.Errorf("list the names of the Hosts: %w", err)
	}
	defer func() { _ = rows.Close() }()
	names := map[string]string{}
	for rows.Next() {
		var id, hostName, spaceID string
		if err := rows.Scan(&id, &hostName, &spaceID); err != nil {
			return nil, fmt.Errorf("list the names of the Hosts: %w", err)
		}
		if scope.Has(spaceID) {
			names[id] = hostName
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list the names of the Hosts: %w", err)
	}
	return names, nil
}

// stored is a Host as stored: the API's Host, the id of its Space, and its
// pinned host key, nil for none.
type stored struct {
	api.Host
	spaceID string
	pin     ssh.PublicKey
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
		h.pin = pub
		h.HostKey = new(ssh.FingerprintSHA256(pub))
	}
	h.Attributes = map[string]record.Value{}
	return h, nil
}

// storedValue reads the stored text of the Attribute key of the Host named
// hostName as a value of its key's type. Every write checks its values, so
// a value that does not parse means the record was changed behind koko's
// back, and is an error rather than a guess.
func storedValue(hostName, key, text string) (record.Value, error) {
	v, err := record.TypeOf(key).Parse(text)
	if err != nil {
		return record.Value{}, fmt.Errorf("read the Attribute %s of %s: %w", key, hostName, err)
	}
	return v, nil
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
		var k, text string
		if err := rows.Scan(&k, &text); err != nil {
			return stored{}, fmt.Errorf("read the Attributes of %s: %w", h.Name, err)
		}
		if h.Attributes[k], err = storedValue(h.Name, k, text); err != nil {
			return stored{}, err
		}
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
	attributes map[string]record.Value
	note       *string
}

// check refuses, for the Host named hostName, a value that breaks its rule,
// and puts the address and the Attributes in the form koko stores.
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
	attributes := make(map[string]record.Value, len(v.attributes))
	for _, k := range slices.Sorted(maps.Keys(v.attributes)) {
		if err := record.CheckKey(k); err != nil {
			return invalid(hostName, err)
		}
		value, err := record.TypeOf(k).Check(v.attributes[k])
		if err != nil {
			return invalid(hostName, fmt.Errorf("the Attribute %s: %w", k, err))
		}
		attributes[k] = value
	}
	v.attributes = attributes
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

// add is host.add: it records the Hosts, all or none. One Host at fault is
// refused as it is; in a batch of several, every Host is checked, and the
// refusal lists each one at fault (api.HostBatchError). A later Host of the
// batch under an earlier one's name is at fault, whether or not the earlier
// one is.
func add(ctx context.Context, tx store.Tx, caller op.Caller, req api.HostAddRequest) (api.HostAddResponse, error) {
	who, err := access.Who(ctx, tx, caller)
	if err != nil {
		return api.HostAddResponse{}, err
	}
	if len(req.Hosts) == 0 {
		return api.HostAddResponse{}, api.CodeInvalidRequest.Errorf("name at least one Host to add")
	}
	resp := api.HostAddResponse{Hosts: make([]api.Host, 0, len(req.Hosts))}
	var faults []api.ItemError
	first := map[string]int{}
	for i, n := range req.Hosts {
		if j, ok := first[n.Name]; ok {
			msg := fmt.Sprintf("the Host [%d] of this batch is named %q too", j, n.Name)
			faults = append(faults, api.ItemError{Index: i, Code: api.CodeNameTaken.Name, Message: msg})
			continue
		}
		first[n.Name] = i
		h, err := insert(ctx, tx, who, n)
		if e, ok := errors.AsType[*api.Error](err); ok && len(req.Hosts) > 1 {
			faults = append(faults, api.ItemError{Index: i, Code: e.Code, Message: e.Message})
			continue
		}
		if err != nil {
			return api.HostAddResponse{}, err
		}
		resp.Hosts = append(resp.Hosts, h)
	}
	if len(faults) > 0 {
		// Returning an error rolls back the Hosts already recorded.
		return api.HostAddResponse{}, api.HostBatchError(len(req.Hosts), faults)
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
		State: api.HostRegistered, Attributes: map[string]record.Value{}, CreatedAt: now, UpdatedAt: now,
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
// each checked, replacing the value of a key it has.
func setAttributes(ctx context.Context, tx store.Tx, hostID string, attrs map[string]record.Value) error {
	for k, v := range attrs {
		_, err := tx.ExecContext(ctx, `INSERT INTO host_attributes (host_id, key, value) VALUES (?, ?, ?)
			ON CONFLICT (host_id, key) DO UPDATE SET value = excluded.value`, hostID, k, v.String())
		if err != nil {
			return fmt.Errorf("set the Attribute %s of %s: %w", k, hostID, err)
		}
	}
	return nil
}

// hostLookup gives a Host's values to a query: its fields of
// api.HostListFields, a Host with no pin lacking host_key, and its
// Attributes.
func hostLookup(h api.Host) query.Lookup {
	return func(key string) (string, bool) {
		switch key {
		case "id":
			return h.ID, true
		case "name":
			return h.Name, true
		case "space":
			return h.Space, true
		case "address":
			return h.Address, true
		case "port":
			return strconv.Itoa(h.Port), true
		case "account":
			return h.Account, true
		case "state":
			return h.State, true
		case "host_key":
			if h.HostKey == nil {
				return "", false
			}
			return *h.HostKey, true
		}
		v, ok := h.Attributes[key]
		return v.String(), ok
	}
}

// list is host.list: the Hosts that are not Retired in the Spaces the caller
// can act in and match the filter, by the sort keys, then by name and id,
// with their Notes only when asked for.
func list(ctx context.Context, tx store.Tx, caller op.Caller, req api.HostListRequest) (api.HostListResponse, error) {
	who, err := access.Who(ctx, tx, caller)
	if err != nil {
		return api.HostListResponse{}, err
	}
	q, err := query.New("host.list", api.HostListFields, req.Filters, req.Sort)
	if err != nil {
		return api.HostListResponse{}, err
	}
	scope, err := who.Spaces(ctx, tx)
	if err != nil {
		return api.HostListResponse{}, err
	}
	rows, err := tx.QueryContext(ctx, selectHosts+" WHERE h.state <> 'retired'")
	if err != nil {
		return api.HostListResponse{}, fmt.Errorf("list the Hosts: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var hosts []api.Host
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
		hosts = append(hosts, h.Host)
	}
	if err := rows.Err(); err != nil {
		return api.HostListResponse{}, fmt.Errorf("list the Hosts: %w", err)
	}
	if err := readAttributes(ctx, tx, hosts); err != nil {
		return api.HostListResponse{}, err
	}
	// By name in natural order, as --sort name orders them, so web2 comes
	// before web10; then by id.
	slices.SortFunc(hosts, func(a, b api.Host) int {
		return cmp.Or(record.Text.Compare(a.Name, b.Name), strings.Compare(a.ID, b.ID))
	})
	return api.HostListResponse{Hosts: q.Apply(hosts, hostLookup)}, nil
}

// readAttributes reads the Attributes of hosts into them.
func readAttributes(ctx context.Context, tx store.Tx, hosts []api.Host) error {
	byID := map[string]*api.Host{}
	for i := range hosts {
		byID[hosts[i].ID] = &hosts[i]
	}
	rows, err := tx.QueryContext(ctx, "SELECT host_id, key, value FROM host_attributes")
	if err != nil {
		return fmt.Errorf("list the Attributes of the Hosts: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id, k, text string
		if err := rows.Scan(&id, &k, &text); err != nil {
			return fmt.Errorf("list the Attributes of the Hosts: %w", err)
		}
		if h, ok := byID[id]; ok {
			if h.Attributes[k], err = storedValue(h.Name, k, text); err != nil {
				return err
			}
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("list the Attributes of the Hosts: %w", err)
	}
	return nil
}

// show is host.show: the Host, with its Note, and its Services.
func show(ctx context.Context, tx store.Tx, caller op.Caller, req api.HostShowRequest, services ServicesOf) (api.HostShowResponse, error) {
	_, h, err := target(ctx, tx, caller, req.Host)
	if err != nil {
		return api.HostShowResponse{}, err
	}
	s, err := services(ctx, tx, h.Host)
	return api.HostShowResponse{Host: h.Host, Services: s}, err
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
	if v.account != nil && *v.account != h.Account {
		h.Account = *v.account
		// The koko key went only to the old Host Account. The pin stays,
		// so the next Enroll checks that the same Host answers.
		if h.State == api.HostManaged {
			h.State = api.HostRegistered
		}
	}
	if v.note != nil {
		h.Note = *v.note
	}

	_, err = tx.ExecContext(ctx, "UPDATE hosts SET address = ?, port = ?, account = ?, state = ?, note = ?, updated_at = ? WHERE id = ?",
		h.Address, h.Port, h.Account, h.State, sql.NullString{String: h.Note, Valid: h.Note != ""}, op.Now(ctx).Format(time.RFC3339), h.ID)
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

// remove is host.remove: the Host goes, with its Attributes, Note and
// Services. A Host that has been Managed, which its pin shows, is kept on
// record instead.
func remove(ctx context.Context, tx store.Tx, caller op.Caller, req api.HostRemoveRequest) (api.HostRemoveResponse, error) {
	_, h, err := target(ctx, tx, caller, req.Host)
	if err != nil {
		return api.HostRemoveResponse{}, err
	}
	if h.pin != nil {
		return api.HostRemoveResponse{}, api.CodeHostWasManaged.Errorf(
			"%s has been Managed, so koko keeps its record: a Host that was ever Managed is never deleted", h.Name)
	}
	// Its Attributes and Services go with it (ON DELETE CASCADE).
	if _, err := tx.ExecContext(ctx, "DELETE FROM hosts WHERE id = ?", h.ID); err != nil {
		return api.HostRemoveResponse{}, fmt.Errorf("delete the Host %s: %w", h.Name, err)
	}
	return api.HostRemoveResponse{Host: h.Host}, nil
}
