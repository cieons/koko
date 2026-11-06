// Package service is the service area of the koko server: what runs on each
// Host, with the ports it listens on, its Attributes and its Note (P0 spec:
// the record). A Service lives in its Host's Space, so access to a Service
// is access to its Host (package host finds it), and it never moves to
// another Host. People and agents record Services; koko discovers nothing
// itself.
package service

import (
	"cmp"
	"context"
	"database/sql"
	"encoding/json/v2"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/cieons/koko/internal/server/access"
	"github.com/cieons/koko/internal/server/host"
	"github.com/cieons/koko/internal/server/op"
	"github.com/cieons/koko/internal/server/query"
	"github.com/cieons/koko/internal/server/store"
	"github.com/cieons/koko/internal/shared/api"
	"github.com/cieons/koko/internal/shared/name"
	"github.com/cieons/koko/internal/shared/record"
)

// idPrefix starts every Service's id, as in service_7k2m9q4x.
const idPrefix = "service"

// Register adds the service operations to the API.
func Register(a *op.API) {
	a.Handle(api.ServiceAdd, add)
	a.Handle(api.ServiceList, list)
	a.Handle(api.ServiceShow, show)
	a.Handle(api.ServiceSet, set)
	a.Handle(api.ServiceRename, rename)
	a.Handle(api.ServiceRemove, remove)
}

// AttributeKeys lists the Attribute keys in use on the Services of the
// Hosts in the Spaces scope holds, Retired ones among them, for attr.list.
func AttributeKeys(ctx context.Context, tx store.Tx, scope access.Scope) ([]string, error) {
	hosts, err := host.Names(ctx, tx, scope)
	if err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT DISTINCT s.host_id, a.key FROM service_attributes a JOIN services s ON s.id = a.service_id`)
	if err != nil {
		return nil, fmt.Errorf("list the Attribute keys of the Services: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var keys []string
	for rows.Next() {
		var hostID, key string
		if err := rows.Scan(&hostID, &key); err != nil {
			return nil, fmt.Errorf("list the Attribute keys of the Services: %w", err)
		}
		if _, ok := hosts[hostID]; ok {
			keys = append(keys, key)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list the Attribute keys of the Services: %w", err)
	}
	return keys, nil
}

// stored is a Service as stored: the API's Service and the id of its Host.
type stored struct {
	api.Service
	hostID string
}

// selectServices reads the Services, without their Hosts' names, ports and
// Attributes, in the order scan reads them.
const selectServices = `SELECT id, host_id, name, coalesce(note, ''), created_at, updated_at FROM services`

// scan reads one Service that selectServices selected.
func scan(row interface{ Scan(...any) error }) (stored, error) {
	var s stored
	if err := row.Scan(&s.ID, &s.hostID, &s.Name, &s.Note, &s.CreatedAt, &s.UpdatedAt); err != nil {
		return stored{}, err
	}
	s.Ports = []string{}
	s.Attributes = map[string]record.Value{}
	return s, nil
}

// qualified is a Service's name as people write it, <host>/<service>.
func qualified(hostName, serviceName string) string {
	return hostName + "/" + serviceName
}

// target returns the Host hostRef names, as the caller sees it, and the
// Service named serviceName on it: the start of every operation on one
// Service.
func target(ctx context.Context, tx store.Tx, caller op.Caller, hostRef, serviceName string) (api.Host, stored, error) {
	h, err := hostOf(ctx, tx, caller, hostRef)
	if err != nil {
		return api.Host{}, stored{}, err
	}
	s, err := find(ctx, tx, h, serviceName)
	return h, s, err
}

// hostOf answers who is calling and returns the Host ref names, as host
// operations find it: one the caller cannot see is unknown_host.
func hostOf(ctx context.Context, tx store.Tx, caller op.Caller, ref string) (api.Host, error) {
	who, err := access.Who(ctx, tx, caller)
	if err != nil {
		return api.Host{}, err
	}
	return host.Find(ctx, tx, who, ref)
}

// writable refuses, with api.CodeHostRetired, a write to the Services of
// the Host h when it is Retired: they are part of the history it keeps as
// it was. Reading them stays open.
func writable(h api.Host) error {
	if h.State == api.HostRetired {
		return api.CodeHostRetired.Errorf("the Host %s (%s) is Retired, so its Services stay as they were", h.Name, h.ID)
	}
	return nil
}

// find returns the Service named serviceName on the Host h, which the
// caller can see, with its ports and Attributes.
func find(ctx context.Context, tx store.Tx, h api.Host, serviceName string) (stored, error) {
	if err := checkName(serviceName); err != nil {
		return stored{}, err
	}
	s, err := scan(tx.QueryRowContext(ctx, selectServices+" WHERE host_id = ? AND name = ?", h.ID, serviceName))
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return stored{}, api.CodeUnknownService.Errorf("the Host %s has no Service named %q", h.Name, serviceName)
	case err != nil:
		return stored{}, fmt.Errorf("read the Service %s: %w", qualified(h.Name, serviceName), err)
	}
	s.Host = host.Ref(h)
	if err := readPortsAndAttributes(ctx, tx, map[string]*api.Service{s.ID: &s.Service}); err != nil {
		return stored{}, err
	}
	return s, nil
}

// OfHost lists the Services on the Host h, by name in natural order and
// then id, with their ports and Attributes and without their Notes, as
// service.list answers them: host.show lists them (host.ServicesOf).
func OfHost(ctx context.Context, tx store.Tx, h api.Host) ([]api.Service, error) {
	rows, err := tx.QueryContext(ctx, selectServices+" WHERE host_id = ?", h.ID)
	if err != nil {
		return nil, fmt.Errorf("list the Services of %s: %w", h.Name, err)
	}
	defer func() { _ = rows.Close() }()
	services := []api.Service{}
	for rows.Next() {
		s, err := scan(rows)
		if err != nil {
			return nil, fmt.Errorf("list the Services of %s: %w", h.Name, err)
		}
		s.Host, s.Note = host.Ref(h), ""
		services = append(services, s.Service)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list the Services of %s: %w", h.Name, err)
	}
	if err := readPortsAndAttributes(ctx, tx, byID(services)); err != nil {
		return nil, err
	}
	slices.SortFunc(services, func(a, b api.Service) int {
		return cmp.Or(record.Text.Compare(a.Name, b.Name), strings.Compare(a.ID, b.ID))
	})
	return services, nil
}

// byID points at each of services by its id.
func byID(services []api.Service) map[string]*api.Service {
	m := make(map[string]*api.Service, len(services))
	for i := range services {
		m[services[i].ID] = &services[i]
	}
	return m
}

// readPortsAndAttributes reads into the Services of services, by id, their
// ports and Attributes, reading the rows of those Services alone.
func readPortsAndAttributes(ctx context.Context, tx store.Tx, services map[string]*api.Service) error {
	ids, err := json.Marshal(slices.Collect(maps.Keys(services)))
	if err != nil {
		return fmt.Errorf("list the ids of the Services: %w", err)
	}
	// One argument, a JSON array of the ids as text, holds any number of
	// them.
	const ofServices = " WHERE service_id IN (SELECT value FROM json_each(?))"
	ports, err := tx.QueryContext(ctx, "SELECT service_id, port, protocol FROM service_ports"+ofServices+" ORDER BY port, protocol", string(ids))
	if err != nil {
		return fmt.Errorf("read the ports of the Services: %w", err)
	}
	defer func() { _ = ports.Close() }()
	for ports.Next() {
		var id string
		var p record.ServicePort
		if err := ports.Scan(&id, &p.Port, &p.Protocol); err != nil {
			return fmt.Errorf("read the ports of the Services: %w", err)
		}
		if s, ok := services[id]; ok {
			s.Ports = append(s.Ports, p.String())
		}
	}
	if err := ports.Err(); err != nil {
		return fmt.Errorf("read the ports of the Services: %w", err)
	}

	attrs, err := tx.QueryContext(ctx, "SELECT service_id, key, value FROM service_attributes"+ofServices, string(ids))
	if err != nil {
		return fmt.Errorf("read the Attributes of the Services: %w", err)
	}
	defer func() { _ = attrs.Close() }()
	for attrs.Next() {
		var id, k, text string
		if err := attrs.Scan(&id, &k, &text); err != nil {
			return fmt.Errorf("read the Attributes of the Services: %w", err)
		}
		s, ok := services[id]
		if !ok {
			continue
		}
		// Every write checks its values, so a value that does not parse
		// means the record was changed behind koko's back: an error, not a
		// guess.
		if s.Attributes[k], err = record.TypeOf(k).Parse(text); err != nil {
			return fmt.Errorf("read the Attribute %s of %s: %w", k, qualified(s.Host, s.Name), err)
		}
	}
	if err := attrs.Err(); err != nil {
		return fmt.Errorf("read the Attributes of the Services: %w", err)
	}
	return nil
}

// values are the ports, Attributes and Note a write gives a Service, each
// nil or empty when the write leaves it as it is.
type values struct {
	// ports is nil when the write leaves the ports as they are.
	ports      []record.ServicePort
	attributes map[string]record.Value
	note       *string
}

// checkValues checks, for the Service named service, the ports, Attributes
// and Note a request gives, refusing a value that breaks its rule, and
// returns them in the form koko stores.
func checkValues(service string, ports []string, attributes map[string]record.Value, note *string) (values, error) {
	var v values
	if ports != nil {
		set, err := record.ParseServicePorts(ports)
		if err != nil {
			return values{}, invalid(service, err)
		}
		v.ports = set
	}
	v.attributes = make(map[string]record.Value, len(attributes))
	for _, k := range slices.Sorted(maps.Keys(attributes)) {
		if err := record.CheckKey(k); err != nil {
			return values{}, invalid(service, err)
		}
		value, err := record.TypeOf(k).Check(attributes[k])
		if err != nil {
			return values{}, invalid(service, fmt.Errorf("the Attribute %s: %w", k, err))
		}
		v.attributes[k] = value
	}
	if note != nil {
		if err := record.CheckNote(*note); err != nil {
			return values{}, invalid(service, err)
		}
		v.note = note
	}
	return v, nil
}

// invalid is the refusal of a value of the Service named service.
func invalid(service string, err error) error {
	return api.CodeInvalidValue.Errorf("the Service %s: %v", service, err)
}

// checkName refuses a Service name that is not a lowercase DNS label.
func checkName(serviceName string) error {
	if err := name.Check(serviceName); err != nil {
		return api.CodeInvalidName.Errorf("invalid Service name: %v", err)
	}
	return nil
}

// checkFree refuses a name another Service on the Host h holds, inside the
// write's transaction. Only the Services of a Host the caller can see are
// checked, so the refusal tells nothing the caller could not list.
func checkFree(ctx context.Context, tx store.Tx, h api.Host, serviceName string) error {
	var taken bool
	err := tx.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM services WHERE host_id = ? AND name = ?)", h.ID, serviceName).Scan(&taken)
	if err != nil {
		return fmt.Errorf("check whether %s has a Service named %q: %w", h.Name, serviceName, err)
	}
	if taken {
		return api.CodeNameTaken.Errorf("the Host %s already has a Service named %q", h.Name, serviceName)
	}
	return nil
}

// storeValues stores what v gives the Service whose id is serviceID: its
// ports, replacing every port it had, when v gives them, and its
// Attributes, replacing the value of a key it has.
func storeValues(ctx context.Context, tx store.Tx, serviceID string, v values) error {
	if v.ports != nil {
		if _, err := tx.ExecContext(ctx, "DELETE FROM service_ports WHERE service_id = ?", serviceID); err != nil {
			return fmt.Errorf("clear the ports of %s: %w", serviceID, err)
		}
		for _, p := range v.ports {
			_, err := tx.ExecContext(ctx, "INSERT INTO service_ports (service_id, port, protocol) VALUES (?, ?, ?)", serviceID, p.Port, p.Protocol)
			if err != nil {
				return fmt.Errorf("set the port %s of %s: %w", p, serviceID, err)
			}
		}
	}
	for k, value := range v.attributes {
		_, err := tx.ExecContext(ctx, `INSERT INTO service_attributes (service_id, key, value) VALUES (?, ?, ?)
			ON CONFLICT (service_id, key) DO UPDATE SET value = excluded.value`, serviceID, k, value.String())
		if err != nil {
			return fmt.Errorf("set the Attribute %s of %s: %w", k, serviceID, err)
		}
	}
	return nil
}

// nullNote is a Note as the services table holds it: NULL for none.
func nullNote(note string) sql.NullString {
	return sql.NullString{String: note, Valid: note != ""}
}

// add is service.add: it records a Service on a Host the caller can see
// that is not Retired (writable).
func add(ctx context.Context, tx store.Tx, caller op.Caller, req api.ServiceAddRequest) (api.ServiceAddResponse, error) {
	h, err := hostOf(ctx, tx, caller, req.Host)
	if err != nil {
		return api.ServiceAddResponse{}, err
	}
	if err := writable(h); err != nil {
		return api.ServiceAddResponse{}, err
	}
	if err := checkName(req.Name); err != nil {
		return api.ServiceAddResponse{}, err
	}
	v, err := checkValues(qualified(h.Name, req.Name), req.Ports, req.Attributes, req.Note)
	if err != nil {
		return api.ServiceAddResponse{}, err
	}
	if err := checkFree(ctx, tx, h, req.Name); err != nil {
		return api.ServiceAddResponse{}, err
	}
	var note string
	if v.note != nil {
		note = *v.note
	}
	now := op.Now(ctx).Format(time.RFC3339)
	id := store.NewID(idPrefix)
	_, err = tx.ExecContext(ctx, `INSERT INTO services (id, host_id, name, note, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
		id, h.ID, req.Name, nullNote(note), now, now)
	if err != nil {
		return api.ServiceAddResponse{}, fmt.Errorf("record the Service %s: %w", qualified(h.Name, req.Name), err)
	}
	if err := storeValues(ctx, tx, id, v); err != nil {
		return api.ServiceAddResponse{}, err
	}
	s, err := find(ctx, tx, h, req.Name)
	return api.ServiceAddResponse{Service: s.Service}, err
}

// serviceLookup gives a Service's values to a query: its fields of
// api.ServiceListFields and its Attributes.
func serviceLookup(s api.Service) query.Lookup {
	return func(key string) (string, bool) {
		switch key {
		case "id":
			return s.ID, true
		case "name":
			return s.Name, true
		case "host":
			return s.Host, true
		}
		v, ok := s.Attributes[key]
		return v.String(), ok
	}
}

// list is service.list: the Services on the Hosts that are not Retired in
// the Spaces the caller can act in that match the filter, by the sort keys,
// then by Host name, name and id, with their Notes only when asked for. A
// Retired Host's Services are hidden with it.
func list(ctx context.Context, tx store.Tx, caller op.Caller, req api.ServiceListRequest) (api.ServiceListResponse, error) {
	who, err := access.Who(ctx, tx, caller)
	if err != nil {
		return api.ServiceListResponse{}, err
	}
	q, err := query.New("service.list", api.ServiceListFields, req.Filters, req.Sort)
	if err != nil {
		return api.ServiceListResponse{}, err
	}
	scope, err := who.Spaces(ctx, tx)
	if err != nil {
		return api.ServiceListResponse{}, err
	}
	hosts, err := host.ListedNames(ctx, tx, scope)
	if err != nil {
		return api.ServiceListResponse{}, err
	}
	rows, err := tx.QueryContext(ctx, selectServices)
	if err != nil {
		return api.ServiceListResponse{}, fmt.Errorf("list the Services: %w", err)
	}
	defer func() { _ = rows.Close() }()
	services := []api.Service{}
	for rows.Next() {
		s, err := scan(rows)
		if err != nil {
			return api.ServiceListResponse{}, fmt.Errorf("list the Services: %w", err)
		}
		hostName, ok := hosts[s.hostID]
		if !ok {
			continue
		}
		s.Host = hostName
		if !req.WithNote {
			s.Note = ""
		}
		services = append(services, s.Service)
	}
	if err := rows.Err(); err != nil {
		return api.ServiceListResponse{}, fmt.Errorf("list the Services: %w", err)
	}
	if err := readPortsAndAttributes(ctx, tx, byID(services)); err != nil {
		return api.ServiceListResponse{}, err
	}
	// By Host and then by name, each in natural order as --sort gives it,
	// so web2's Services come before web10's; then by id.
	slices.SortFunc(services, func(a, b api.Service) int {
		return cmp.Or(record.Text.Compare(a.Host, b.Host), record.Text.Compare(a.Name, b.Name), strings.Compare(a.ID, b.ID))
	})
	return api.ServiceListResponse{Services: q.Apply(services, serviceLookup)}, nil
}

func show(ctx context.Context, tx store.Tx, caller op.Caller, req api.ServiceShowRequest) (api.ServiceShowResponse, error) {
	_, s, err := target(ctx, tx, caller, req.Host, req.Name)
	return api.ServiceShowResponse{Service: s.Service}, err
}

// set is service.set: it changes the ports, Attributes and Note the request
// names, and only those. A Service's Host never changes, and its name only
// through service.rename. A Retired Host's Services do not change
// (writable).
func set(ctx context.Context, tx store.Tx, caller op.Caller, req api.ServiceSetRequest) (api.ServiceSetResponse, error) {
	h, s, err := target(ctx, tx, caller, req.Host, req.Name)
	if err != nil {
		return api.ServiceSetResponse{}, err
	}
	if err := writable(h); err != nil {
		return api.ServiceSetResponse{}, err
	}
	service := qualified(s.Host, s.Name)
	switch {
	case req.Ports == nil && len(req.Attributes) == 0 && req.Note == nil && len(req.Unset) == 0:
		return api.ServiceSetResponse{}, api.CodeInvalidRequest.Errorf("name at least one port, Attribute or Note of %s to change", service)
	case req.Ports != nil && len(req.Ports) == 0:
		return api.ServiceSetResponse{}, invalid(service, errors.New("ports, when given, are at least one; to clear them, unset ports"))
	}
	v, err := checkValues(service, req.Ports, req.Attributes, req.Note)
	if err != nil {
		return api.ServiceSetResponse{}, err
	}
	var cleared []string
	for _, k := range req.Unset {
		_, setToo := req.Attributes[k]
		switch {
		case setToo, k == "note" && req.Note != nil, k == "ports" && req.Ports != nil:
			return api.ServiceSetResponse{}, api.CodeInvalidRequest.Errorf("%s is both set and cleared", k)
		case k == "note":
			s.Note = ""
		case k == "ports":
			v.ports = []record.ServicePort{}
		case record.Reserved(k):
			return api.ServiceSetResponse{}, invalid(service, fmt.Errorf("%q is a field service.set does not clear", k))
		default:
			if err := record.CheckKey(k); err != nil {
				return api.ServiceSetResponse{}, invalid(service, err)
			}
			cleared = append(cleared, k)
		}
	}
	if v.note != nil {
		s.Note = *v.note
	}
	_, err = tx.ExecContext(ctx, "UPDATE services SET note = ?, updated_at = ? WHERE id = ?", nullNote(s.Note), op.Now(ctx).Format(time.RFC3339), s.ID)
	if err != nil {
		return api.ServiceSetResponse{}, fmt.Errorf("change the Service %s: %w", service, err)
	}
	if err := storeValues(ctx, tx, s.ID, v); err != nil {
		return api.ServiceSetResponse{}, err
	}
	for _, k := range cleared {
		if _, err := tx.ExecContext(ctx, "DELETE FROM service_attributes WHERE service_id = ? AND key = ?", s.ID, k); err != nil {
			return api.ServiceSetResponse{}, fmt.Errorf("clear the Attribute %s of %s: %w", k, service, err)
		}
	}
	changed, err := find(ctx, tx, h, s.Name)
	return api.ServiceSetResponse{Service: changed.Service}, err
}

// rename is service.rename: the Service takes a new name on its Host and
// keeps its id and everything else, unless the Host is Retired (writable).
func rename(ctx context.Context, tx store.Tx, caller op.Caller, req api.ServiceRenameRequest) (api.ServiceRenameResponse, error) {
	h, s, err := target(ctx, tx, caller, req.Host, req.Name)
	if err != nil {
		return api.ServiceRenameResponse{}, err
	}
	if err := writable(h); err != nil {
		return api.ServiceRenameResponse{}, err
	}
	if err := checkName(req.NewName); err != nil {
		return api.ServiceRenameResponse{}, err
	}
	if err := checkFree(ctx, tx, h, req.NewName); err != nil {
		return api.ServiceRenameResponse{}, err
	}
	_, err = tx.ExecContext(ctx, "UPDATE services SET name = ?, updated_at = ? WHERE id = ?", req.NewName, op.Now(ctx).Format(time.RFC3339), s.ID)
	if err != nil {
		return api.ServiceRenameResponse{}, fmt.Errorf("rename the Service %s to %s: %w", qualified(s.Host, s.Name), req.NewName, err)
	}
	renamed, err := find(ctx, tx, h, req.NewName)
	return api.ServiceRenameResponse{Service: renamed.Service}, err
}

// remove is service.remove: the Service goes, with its ports, Attributes and
// Note, and is answered as it was, unless its Host is Retired (writable).
func remove(ctx context.Context, tx store.Tx, caller op.Caller, req api.ServiceRemoveRequest) (api.ServiceRemoveResponse, error) {
	h, s, err := target(ctx, tx, caller, req.Host, req.Name)
	if err != nil {
		return api.ServiceRemoveResponse{}, err
	}
	if err := writable(h); err != nil {
		return api.ServiceRemoveResponse{}, err
	}
	// Its ports and Attributes go with it (ON DELETE CASCADE).
	if _, err := tx.ExecContext(ctx, "DELETE FROM services WHERE id = ?", s.ID); err != nil {
		return api.ServiceRemoveResponse{}, fmt.Errorf("delete the Service %s: %w", qualified(s.Host, s.Name), err)
	}
	return api.ServiceRemoveResponse{Service: s.Service}, nil
}
