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
// Services for host.show, and each write that may put a Host out of a
// caller's reach is announced on changes once it commits.
func Register(a *op.API, services ServicesOf, changes *access.Signal) {
	a.Handle(api.HostAdd, add)
	a.Handle(api.HostList, list)
	a.Handle(api.HostShow, func(ctx context.Context, tx store.Tx, caller op.Caller, req api.HostShowRequest) (api.HostShowResponse, error) {
		return show(ctx, tx, caller, req, services)
	})
	a.Handle(api.HostSet, func(ctx context.Context, tx store.Tx, caller op.Caller, req api.HostSetRequest) (api.HostSetResponse, error) {
		return set(ctx, tx, caller, req, changes)
	})
	a.Handle(api.HostRename, rename)
	a.Handle(api.HostMove, func(ctx context.Context, tx store.Tx, caller op.Caller, req api.HostMoveRequest) (api.HostMoveResponse, error) {
		return move(ctx, tx, caller, req, changes)
	})
	a.Handle(api.HostRetire, func(ctx context.Context, tx store.Tx, caller op.Caller, req api.HostRetireRequest) (api.HostRetireResponse, error) {
		return retire(ctx, tx, caller, req, changes)
	})
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

// Ref returns what names the Host h in a request: its name, or its id once
// it is Retired and holds its name no more.
func Ref(h api.Host) string {
	if h.State == api.HostRetired {
		return h.ID
	}
	return h.Name
}

// Names returns the name of each Host in the Spaces scope holds, by id,
// Retired ones among them, for the service area: a Service is seen by
// whoever sees its Host.
func Names(ctx context.Context, tx store.Tx, scope access.Scope) (map[string]string, error) {
	return names(ctx, tx, scope, "SELECT id, name, space_id FROM hosts")
}

// ListedNames returns Names without the Retired Hosts, for the service
// area's lists, which leave a Retired Host's Services out with it.
func ListedNames(ctx context.Context, tx store.Tx, scope access.Scope) (map[string]string, error) {
	return names(ctx, tx, scope, "SELECT id, name, space_id FROM hosts WHERE state <> 'retired'")
}

// names returns the name of each Host that stmt selects, as id, name and
// the id of its Space, in the Spaces scope holds, by id.
func names(ctx context.Context, tx store.Tx, scope access.Scope, stmt string) (map[string]string, error) {
	rows, err := tx.QueryContext(ctx, stmt)
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

// stored is a Host as stored: the API's Host, the id of its Space, its
// pinned host key, nil for none, and its Relay, nil for none. The API's Host
// holds no Relay until seeRelay says how the caller sees it.
type stored struct {
	api.Host
	spaceID string
	pin     ssh.PublicKey
	relay   *relayLink
}

// relayLink is a Host's Relay as stored: its id, its name and the id of its
// Space, which decides whether a caller sees it.
type relayLink struct {
	id, name, spaceID string
}

// selectHosts reads the Hosts, each with the name of its Space and its
// Relay, in the order scan reads them.
const selectHosts = `SELECT h.id, h.name, s.name, h.space_id, h.address, h.port, h.account, h.state, h.host_key,
	coalesce(h.note, ''), h.created_at, h.updated_at, h.relay_id, r.name, r.space_id
	FROM hosts h JOIN spaces s ON s.id = h.space_id LEFT JOIN hosts r ON r.id = h.relay_id`

// scan reads one Host that selectHosts selected, without its Attributes.
func scan(row interface{ Scan(...any) error }) (stored, error) {
	var h stored
	var key []byte
	var relayID, relayName, relaySpaceID sql.NullString
	err := row.Scan(&h.ID, &h.Name, &h.Space, &h.spaceID, &h.Address, &h.Port, &h.Account, &h.State, &key,
		&h.Note, &h.CreatedAt, &h.UpdatedAt, &relayID, &relayName, &relaySpaceID)
	if err != nil {
		return stored{}, err
	}
	if relayID.Valid {
		h.relay = &relayLink{id: relayID.String, name: relayName.String, spaceID: relaySpaceID.String}
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

// seeRelay shows the Host's Relay, if it has one, as a caller who can act in
// the Spaces scope holds sees it: by its id and name, or, in a Space they
// cannot act in, present but unnamed (ADR 0035).
func (h *stored) seeRelay(scope access.Scope) {
	switch {
	case h.relay == nil:
		h.Relay = nil
	case scope.Has(h.relay.spaceID):
		h.Relay = &api.HostRelay{ID: new(h.relay.id), Name: new(h.relay.name)}
	default:
		h.Relay = &api.HostRelay{}
	}
}

// relayID is the id of the Host's Relay, as the column relay_id holds it.
func (h stored) relayID() sql.NullString {
	if h.relay == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: h.relay.id, Valid: true}
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
	scope, err := who.Spaces(ctx, tx)
	switch {
	case err != nil:
		return stored{}, err
	case !scope.Has(h.spaceID):
		return stored{}, unknown
	}
	h.seeRelay(scope)
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
	relay      *string
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
// refusal lists each one at fault (api.BatchError). A later Host of the
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
		return api.HostAddResponse{}, api.BatchError(len(req.Hosts), "Hosts", "added", faults)
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
	v := values{address: &n.Address, port: n.Port, account: &n.Account, relay: n.Relay, attributes: n.Attributes, note: n.Note}
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
	var relayID sql.NullString
	if v.relay != nil {
		r, err := relayFor(ctx, tx, who, h.ID, h.Name, *v.relay)
		if err != nil {
			return api.Host{}, err
		}
		// Whoever sets a Relay can see it.
		h.Relay, relayID = &api.HostRelay{ID: new(r.ID), Name: new(r.Name)}, sql.NullString{String: r.ID, Valid: true}
	}
	var note sql.NullString
	if v.note != nil {
		h.Note, note = *v.note, sql.NullString{String: *v.note, Valid: true}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO hosts (id, name, space_id, address, port, account, state, note, created_at, updated_at, relay_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, h.ID, h.Name, sp.ID, h.Address, h.Port, h.Account, h.State, note, now, now, relayID)
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

// relayFor returns the Host ref names, by its name or its id, as the Relay
// of the Host named hostName whose id is hostID, refusing one that cannot
// be (P0 spec: the record, "Relay"): a Host the caller cannot see, as one
// that does not exist; the Host itself; a Host that is not Managed, a
// Retired one among them; one that has a Relay itself; and any Relay at all
// for a Host that is the Relay of a Host that is not Retired, since a Relay
// is exactly one hop. A refusal names no Host the caller cannot see.
func relayFor(ctx context.Context, tx store.Tx, who access.Identity, hostID, hostName, ref string) (stored, error) {
	r, err := find(ctx, tx, who, ref)
	if e, ok := errors.AsType[*api.Error](err); ok {
		return stored{}, &api.Error{Code: e.Code, Message: fmt.Sprintf("the Relay of %s: %s", hostName, e.Message)}
	}
	if err != nil {
		return stored{}, err
	}
	switch {
	case r.ID == hostID:
		return stored{}, api.CodeInvalidRelay.Errorf("%s cannot be its own Relay", hostName)
	case r.State == api.HostRetired:
		return stored{}, api.CodeInvalidRelay.Errorf("%s (%s) is Retired, and a Retired Host is never a Relay", r.Name, r.ID)
	case r.State != api.HostManaged:
		return stored{}, api.CodeInvalidRelay.Errorf(
			"%s is Registered, and a Relay must be Managed: Enroll it first, with koko host enroll %s", r.Name, r.Name)
	case r.relay != nil:
		return stored{}, api.CodeInvalidRelay.Errorf(
			"%s has a Relay itself, and a Relay is exactly one hop: koko does not pass through two", r.Name)
	}
	used, err := usedAsRelay(ctx, tx, hostID, nil)
	switch {
	case err != nil:
		return stored{}, err
	case used:
		return stored{}, api.CodeInvalidRelay.Errorf(
			"%s is the Relay of a Host that is not Retired, and a Relay is exactly one hop, so it takes no Relay itself; koko host ls relay=%s lists those you can see",
			hostName, hostName)
	}
	return r, nil
}

// usedAsRelay tells whether a Host that is not Retired, and whose id is not
// among except, has the Host whose id is hostID as its Relay.
func usedAsRelay(ctx context.Context, tx store.Tx, hostID string, except []string) (bool, error) {
	rows, err := tx.QueryContext(ctx, "SELECT id FROM hosts WHERE relay_id = ? AND state <> 'retired'", hostID)
	if err != nil {
		return false, fmt.Errorf("check whether a Host uses %s as its Relay: %w", hostID, err)
	}
	defer func() { _ = rows.Close() }()
	used := false
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return false, fmt.Errorf("check whether a Host uses %s as its Relay: %w", hostID, err)
		}
		used = used || !slices.Contains(except, id)
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("check whether a Host uses %s as its Relay: %w", hostID, err)
	}
	return used, nil
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
// Attributes. A Host without a Relay lacks relay; one whose Relay the caller
// cannot see has it, with an empty name, so relay.has matches it, relay=
// never does, and no term compares a name the caller may not see (ADR 0035).
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
		case "relay":
			switch {
			case h.Relay == nil:
				return "", false
			case h.Relay.Name == nil:
				return "", true
			}
			return *h.Relay.Name, true
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

// list is host.list: the Hosts in the Spaces the caller can act in that
// match the filter, by the sort keys, then by name and id, with the Retired
// ones and the Notes only when asked for.
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
	stmt := selectHosts
	if !req.All {
		stmt += " WHERE h.state <> 'retired'"
	}
	rows, err := tx.QueryContext(ctx, stmt)
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
		h.seeRelay(scope)
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

// retiredTakes ends each refusal of a change to a Retired Host, saying what
// it does take.
const retiredTakes = "a Retired Host takes only changes to its Attributes and Note, and moves"

// set is host.set: it changes the fields, Attributes and Note the request
// names, and only those, of the Host it names or of each Host of a batch.
// A request that names nothing to change is refused once; a Host that holds
// the values already is not at fault.
func set(ctx context.Context, tx store.Tx, caller op.Caller, req api.HostSetRequest, changes *access.Signal) (api.HostSetResponse, error) {
	who, err := access.Who(ctx, tx, caller)
	if err != nil {
		return api.HostSetResponse{}, err
	}
	n, err := findNamed(ctx, tx, who, req.Host, req.Hosts)
	if err != nil {
		return api.HostSetResponse{}, err
	}
	if req.Address == nil && req.Port == nil && req.Account == nil && req.Relay == nil && len(req.Attributes) == 0 && req.Note == nil && len(req.Unset) == 0 {
		of := fmt.Sprintf("the %d Hosts", len(n.hosts))
		if !n.several {
			of = n.hosts[0].Name
		}
		return api.HostSetResponse{}, api.CodeInvalidRequest.Errorf("name at least one field, Attribute or Note of %s to change", of)
	}
	hosts, err := n.each("changed", func(h stored) (api.Host, error) {
		return setOne(ctx, tx, who, h, req, changes)
	})
	switch {
	case err != nil:
		return api.HostSetResponse{}, err
	case n.several:
		return api.HostSetResponse{Hosts: hosts}, nil
	}
	return api.HostSetResponse{Host: hosts[0]}, nil
}

// setOne changes the Host h as host.set's req says, and answers it as the
// change left it. A Retired Host keeps its address, port and Host Account
// as they were. A Managed Host whose Host Account changes goes back to
// Registered, which it announces on changes, so its SSH Sessions end.
func setOne(ctx context.Context, tx store.Tx, who access.Identity, h stored, req api.HostSetRequest, changes *access.Signal) (api.Host, error) {
	v := values{address: req.Address, port: req.Port, account: req.Account, relay: req.Relay, attributes: req.Attributes, note: req.Note}
	if h.State == api.HostRetired && (v.address != nil || v.port != nil || v.account != nil || v.relay != nil ||
		slices.Contains(req.Unset, "port") || slices.Contains(req.Unset, "relay")) {
		return api.Host{}, api.CodeHostRetired.Errorf(
			"%s (%s) is Retired, so its address, port, Host Account and Relay stay as they were: %s", h.Name, h.ID, retiredTakes)
	}
	if err := v.check(h.Name); err != nil {
		return api.Host{}, err
	}
	var cleared []string
	for _, k := range req.Unset {
		_, setToo := req.Attributes[k]
		switch {
		case setToo, k == "note" && req.Note != nil, k == "port" && req.Port != nil, k == "relay" && req.Relay != nil:
			return api.Host{}, api.CodeInvalidRequest.Errorf("%s is both set and cleared", k)
		case k == "note":
			h.Note = ""
		case k == "relay":
			h.relay = nil
		case k == "port":
			v.port = new(record.DefaultPort)
		case slices.Contains(record.Required, k):
			return api.Host{}, invalid(h.Name, fmt.Errorf("the %s cannot be cleared: a Host always has one", k))
		case record.Reserved(k):
			return api.Host{}, invalid(h.Name, fmt.Errorf("%q is a field host.set does not clear", k))
		default:
			if err := record.CheckKey(k); err != nil {
				return api.Host{}, invalid(h.Name, err)
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
			changes.Publish(ctx, access.Change{Hosts: []string{h.ID}})
		}
	}
	if v.note != nil {
		h.Note = *v.note
	}
	// A new Relay keeps access, so it publishes nothing: a live SSH Session
	// goes on through the Relay it started through.
	if v.relay != nil {
		r, err := relayFor(ctx, tx, who, h.ID, h.Name, *v.relay)
		if err != nil {
			return api.Host{}, err
		}
		h.relay = &relayLink{id: r.ID}
	}

	_, err := tx.ExecContext(ctx, "UPDATE hosts SET address = ?, port = ?, account = ?, relay_id = ?, state = ?, note = ?, updated_at = ? WHERE id = ?",
		h.Address, h.Port, h.Account, h.relayID(), h.State, sql.NullString{String: h.Note, Valid: h.Note != ""}, op.Now(ctx).Format(time.RFC3339), h.ID)
	if err != nil {
		return api.Host{}, fmt.Errorf("change the Host %s: %w", h.Name, err)
	}
	if err := setAttributes(ctx, tx, h.ID, v.attributes); err != nil {
		return api.Host{}, err
	}
	for _, k := range cleared {
		if _, err := tx.ExecContext(ctx, "DELETE FROM host_attributes WHERE host_id = ? AND key = ?", h.ID, k); err != nil {
			return api.Host{}, fmt.Errorf("clear the Attribute %s of %s: %w", k, h.Name, err)
		}
	}
	changed, err := find(ctx, tx, who, h.ID)
	return changed.Host, err
}

// rename is host.rename: the Host takes a new name, its SSH Name, and keeps
// its id and everything else. A Retired Host holds no name to change.
func rename(ctx context.Context, tx store.Tx, caller op.Caller, req api.HostRenameRequest) (api.HostRenameResponse, error) {
	who, h, err := target(ctx, tx, caller, req.Host)
	if err != nil {
		return api.HostRenameResponse{}, err
	}
	if h.State == api.HostRetired {
		return api.HostRenameResponse{}, api.CodeHostRetired.Errorf(
			"%s (%s) is Retired and holds its name no more, so it is not renamed: %s", h.Name, h.ID, retiredTakes)
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

// move is host.move: the Host, or each Host of a batch, goes to another
// Space with its Attributes and Note, keeping its name. The caller must be
// able to act in both Spaces. It announces each move on changes, so each
// SSH Session to a moved Host of a caller who cannot act in the new Space
// ends once the call commits.
func move(ctx context.Context, tx store.Tx, caller op.Caller, req api.HostMoveRequest, changes *access.Signal) (api.HostMoveResponse, error) {
	who, err := access.Who(ctx, tx, caller)
	if err != nil {
		return api.HostMoveResponse{}, err
	}
	n, err := findNamed(ctx, tx, who, req.Host, req.Hosts)
	if err != nil {
		return api.HostMoveResponse{}, err
	}
	to, err := space.ByName(ctx, tx, who, req.Space)
	if err != nil {
		return api.HostMoveResponse{}, err
	}
	hosts, err := n.each("moved", func(h stored) (api.Host, error) {
		if to.ID != h.spaceID {
			_, err := tx.ExecContext(ctx, "UPDATE hosts SET space_id = ?, updated_at = ? WHERE id = ?", to.ID, op.Now(ctx).Format(time.RFC3339), h.ID)
			if err != nil {
				return api.Host{}, fmt.Errorf("move the Host %s to %s: %w", h.Name, to.Name, err)
			}
			changes.Publish(ctx, access.Change{Hosts: []string{h.ID}})
		}
		moved, err := find(ctx, tx, who, h.ID)
		return moved.Host, err
	})
	switch {
	case err != nil:
		return api.HostMoveResponse{}, err
	case n.several:
		return api.HostMoveResponse{Hosts: hosts}, nil
	}
	return api.HostMoveResponse{Host: hosts[0]}, nil
}

// retire is host.retire: the Host, or each Host of a batch, becomes Retired,
// which is terminal, and keeps its record, Attributes, Note, Services,
// Space and pin. Its name is free from then on, since only a Host that is
// not Retired holds one (checkFree, and the index hosts_live_name), so only
// its id names it. A Relay is refused while a Host outside the batch that
// is not Retired uses it, so a Relay retires with the Hosts behind it. It
// announces each retirement on changes, so the Host's SSH Sessions end.
func retire(ctx context.Context, tx store.Tx, caller op.Caller, req api.HostRetireRequest, changes *access.Signal) (api.HostRetireResponse, error) {
	who, err := access.Who(ctx, tx, caller)
	if err != nil {
		return api.HostRetireResponse{}, err
	}
	n, err := findNamed(ctx, tx, who, req.Host, req.Hosts)
	if err != nil {
		return api.HostRetireResponse{}, err
	}
	batch := n.ids()
	hosts, err := n.each("retired", func(h stored) (api.Host, error) {
		if h.State == api.HostRetired {
			return api.Host{}, api.CodeHostRetired.Errorf("%s (%s) is Retired already", h.Name, h.ID)
		}
		// The refusal shows through to a caller who cannot see the Host
		// that uses it, telling only that one exists (ADR 0014's accepted
		// cost).
		used, err := usedAsRelay(ctx, tx, h.ID, batch)
		switch {
		case err != nil:
			return api.Host{}, err
		case used:
			return api.Host{}, api.CodeRelayInUse.Errorf(
				"%s is the Relay of a Host that is not Retired, so it is not Retired: give that Host another Relay, with koko host set <host> relay=<relay>, or none, with --unset relay; koko host ls relay=%s lists those you can see",
				h.Name, h.Name)
		}
		_, err = tx.ExecContext(ctx, "UPDATE hosts SET state = ?, updated_at = ? WHERE id = ?", api.HostRetired, op.Now(ctx).Format(time.RFC3339), h.ID)
		if err != nil {
			return api.Host{}, fmt.Errorf("retire the Host %s: %w", h.Name, err)
		}
		changes.Publish(ctx, access.Change{Hosts: []string{h.ID}})
		retired, err := find(ctx, tx, who, h.ID)
		return retired.Host, err
	})
	switch {
	case err != nil:
		return api.HostRetireResponse{}, err
	case n.several:
		return api.HostRetireResponse{Hosts: hosts}, nil
	}
	return api.HostRetireResponse{Host: hosts[0]}, nil
}

// remove is host.remove: the Host, or each Host of a batch, goes, with its
// Attributes, Note and Services, and is answered as it was. A Host that has
// been Managed, which its pin shows, is kept on record instead, and
// retiring it is the way on unless it is Retired already.
func remove(ctx context.Context, tx store.Tx, caller op.Caller, req api.HostRemoveRequest) (api.HostRemoveResponse, error) {
	who, err := access.Who(ctx, tx, caller)
	if err != nil {
		return api.HostRemoveResponse{}, err
	}
	n, err := findNamed(ctx, tx, who, req.Host, req.Hosts)
	if err != nil {
		return api.HostRemoveResponse{}, err
	}
	hosts, err := n.each("removed", func(h stored) (api.Host, error) {
		if h.pin != nil {
			var wayOn string
			if h.State != api.HostRetired {
				wayOn = fmt.Sprintf(". Retire it instead, with koko host retire %s, which frees its name", h.Name)
			}
			return api.Host{}, api.CodeHostWasManaged.Errorf(
				"%s has been Managed, so koko keeps its record: a Host that was ever Managed is never deleted%s", h.Name, wayOn)
		}
		// Its Attributes and Services go with it (ON DELETE CASCADE).
		if _, err := tx.ExecContext(ctx, "DELETE FROM hosts WHERE id = ?", h.ID); err != nil {
			return api.Host{}, fmt.Errorf("delete the Host %s: %w", h.Name, err)
		}
		return h.Host, nil
	})
	switch {
	case err != nil:
		return api.HostRemoveResponse{}, err
	case n.several:
		return api.HostRemoveResponse{Hosts: hosts}, nil
	}
	return api.HostRemoveResponse{Host: hosts[0]}, nil
}
