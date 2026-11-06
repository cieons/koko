package api

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/cieons/koko/internal/shared/record"
)

// The states of a Host, as Host.State names them. Only koko sets a Host's
// state: Enroll makes it Managed, and retiring makes it Retired.
const (
	HostRegistered = "registered"
	HostManaged    = "managed"
	HostRetired    = "retired"
)

// Host is a Host as the API returns it.
type Host struct {
	ID string `json:"id"`
	// Name is the Host's SSH Name, a lowercase DNS label unique among the
	// Hosts that are not Retired, whatever Space each is in.
	Name string `json:"name"`
	// Space is the name of the Space the Host is in.
	Space   string `json:"space"`
	Address string `json:"address"`
	Port    int    `json:"port"`
	// Account is the Host Account koko logs in as.
	Account string `json:"account"`
	// State is HostRegistered, HostManaged or HostRetired.
	State string `json:"state"`
	// HostKey is the pinned host key's SHA-256 fingerprint, as OpenSSH
	// prints it ("SHA256:..."), or null for a Host with no pin.
	HostKey *string `json:"host_key"`
	// Attributes are the Host's Attributes, by key, each value in the JSON
	// type that matches it (package record): a number as a JSON number,
	// written exactly as stored, and every other value as a JSON string.
	Attributes map[string]record.Value `json:"attributes"`
	// Note is the Host's Note, left out when the Host has none. host.list
	// leaves every Note out unless with_note asks for them.
	Note string `json:"note,omitempty"`
	// CreatedAt and UpdatedAt are RFC 3339 in UTC to the second.
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// NewHost is a Host to record.
type NewHost struct {
	// Name is a lowercase DNS label no Host that is not Retired holds.
	Name string `json:"name"`
	// Space names the Space to record the Host in; left out, the Host goes
	// to the caller's Personal Space.
	Space *string `json:"space,omitzero"`
	// Address is an IP address or a DNS name (package record).
	Address string `json:"address"`
	// Port is 1 to 65535; left out, it is 22.
	Port *int `json:"port,omitzero"`
	// Account is the Host Account, which has no default.
	Account string `json:"account"`
	// Attributes are values by key, each of its key's type (package
	// record): a Preset's, or free text.
	Attributes map[string]record.Value `json:"attributes,omitempty"`
	// Note, when given, is the Host's Note: never empty.
	Note *string `json:"note,omitzero"`
}

// HostAddRequest records Hosts, all or none.
type HostAddRequest struct {
	// Hosts are the Hosts to record, at least one, in one transaction.
	Hosts []NewHost `json:"hosts"`
}

// HostAddResponse is the Hosts host.add recorded, in the order asked.
type HostAddResponse struct {
	Hosts []Host `json:"hosts"`
}

// HostListRequest asks for the Hosts the caller can see that match its
// filter, in its order.
type HostListRequest struct {
	// Filters are the terms a Host must match, every one. A term takes a
	// field of HostListFields: id, name, space, address, port (a number),
	// account, state (an enum: registered, managed, retired) and host_key;
	// or an Attribute key. A date is absolute.
	Filters []Filter `json:"filters,omitempty"`
	// Sort orders the Hosts by these keys, in turn, from the fields and
	// Attribute keys Filters takes; Hosts that tie stay by name and then id.
	Sort []SortKey `json:"sort,omitempty"`
	// WithNote asks for each Host's Note.
	WithNote bool `json:"with_note,omitzero"`
	// All adds the Retired Hosts, which the filters then take as any other
	// Host: state=retired keeps only them.
	All bool `json:"all,omitzero"`
}

// HostListFields are the fields host.list filters and sorts on, by key, with
// the types of their values; every other key is an Attribute key. The Note
// is not a key to filter on, and neither are the created and updated times:
// a timestamp is no date, and to compare one with a date would need the
// caller's time zone, which the API never sees. The CLI checks a term
// against them before any call, and the koko server on every call.
var HostListFields = map[string]record.Type{
	"id":       record.Text,
	"name":     record.Text,
	"space":    record.Text,
	"address":  record.Text,
	"port":     record.Number,
	"account":  record.Text,
	"state":    record.Enum(HostRegistered, HostManaged, HostRetired),
	"host_key": record.Text,
}

// HostListResponse holds the Hosts the caller can see that match the
// filter, by the sort keys, then by name and then id.
type HostListResponse struct {
	Hosts []Host `json:"hosts"`
}

// HostShowRequest names a Host to show, by its name or its id.
type HostShowRequest struct {
	Host string `json:"host"`
}

// HostShowResponse is the Host host.show names, with its Services.
type HostShowResponse struct {
	Host Host `json:"host"`
	// Services are the Services on the Host, by name in natural order,
	// without their Notes, as service.list answers them.
	Services []Service `json:"services"`
}

// HostSetRequest changes the fields and Attributes of a Host that it names,
// and only those.
type HostSetRequest struct {
	// Host names the Host, by its name or its id.
	Host string `json:"host"`
	// Address, Port and Account, when given, replace the Host's.
	Address *string `json:"address,omitzero"`
	Port    *int    `json:"port,omitzero"`
	Account *string `json:"account,omitzero"`
	// Attributes set these Attributes, by key, leaving the others.
	Attributes map[string]record.Value `json:"attributes,omitempty"`
	// Note, when given, replaces the Host's Note: never empty.
	Note *string `json:"note,omitzero"`
	// Unset clears these Attribute keys, the Note for "note", and sets the
	// port back to 22 for "port". A key the Host lacks is already clear.
	Unset []string `json:"unset,omitempty"`
}

// HostSetResponse is the Host as host.set left it.
type HostSetResponse struct {
	Host Host `json:"host"`
}

// HostRenameRequest gives a Host a new name, its SSH Name.
type HostRenameRequest struct {
	// Host names the Host, by its name or its id.
	Host string `json:"host"`
	// NewName is a name no Host that is not Retired holds.
	NewName string `json:"new_name"`
}

// HostRenameResponse is the Host under its new name.
type HostRenameResponse struct {
	Host Host `json:"host"`
}

// HostMoveRequest moves a Host to another Space.
type HostMoveRequest struct {
	// Host names the Host, by its name or its id.
	Host string `json:"host"`
	// Space names the Space to move it to.
	Space string `json:"space"`
}

// HostMoveResponse is the Host in its new Space.
type HostMoveResponse struct {
	Host Host `json:"host"`
}

// HostRetireRequest names a Host to retire.
type HostRetireRequest struct {
	// Host names the Host, by its name or its id.
	Host string `json:"host"`
}

// HostRetireResponse is the Host as host.retire left it: Retired, and
// named by its id alone from then on.
type HostRetireResponse struct {
	Host Host `json:"host"`
}

// HostRemoveRequest names a Host to delete.
type HostRemoveRequest struct {
	// Host names the Host, by its name or its id.
	Host string `json:"host"`
}

// HostRemoveResponse is the Host host.remove deleted, as it was.
type HostRemoveResponse struct {
	Host Host `json:"host"`
}

// The host operations. Space membership decides each of them: a Host in a
// Space the caller cannot act in answers exactly as one that does not
// exist, with CodeUnknownHost, and a Space they cannot act in as an unknown
// one, with CodeUnknownSpace.
var (
	// HostAdd records Hosts, all or none, each in the Space it names or the
	// caller's Personal Space. A name a Host that is not Retired holds is
	// refused with CodeNameTaken, whatever Space holds it, and with the
	// same message whichever one does; a value that breaks its rule with
	// CodeInvalidValue.
	HostAdd = Op[HostAddRequest, HostAddResponse]{Name: "host.add", Write: true}
	// HostList lists the Hosts the caller can see.
	HostList = Op[HostListRequest, HostListResponse]{Name: "host.list"}
	// HostShow shows a Host with its Note and its Services.
	HostShow = Op[HostShowRequest, HostShowResponse]{Name: "host.show"}
	// HostSet changes the fields, Attributes and Note it names, and only
	// those, so two callers changing different keys never undo each other.
	HostSet = Op[HostSetRequest, HostSetResponse]{Name: "host.set", Write: true}
	// HostRename gives a Host a new name, refused with CodeNameTaken as
	// host.add refuses one.
	HostRename = Op[HostRenameRequest, HostRenameResponse]{Name: "host.rename", Write: true}
	// HostMove moves a Host, with its Attributes, Note and Services, to
	// another Space; the caller must be able to act in both. Its name stays.
	HostMove = Op[HostMoveRequest, HostMoveResponse]{Name: "host.move", Write: true}
	// HostRetire makes a Registered or Managed Host Retired, which is
	// terminal. It keeps its record, Attributes, Note, Services, Space and
	// pin, leaves host.list unless all asks for it, and frees its name, so
	// that only its id names it. A Host Retired already is refused with
	// CodeHostRetired.
	HostRetire = Op[HostRetireRequest, HostRetireResponse]{Name: "host.retire", Write: true}
	// HostRemove deletes a Host, with its Attributes, Note and Services.
	HostRemove = Op[HostRemoveRequest, HostRemoveResponse]{Name: "host.remove", Write: true}
)

var hostOperations = []Operation{
	HostAdd,
	HostList,
	HostShow,
	HostSet,
	HostRename,
	HostMove,
	HostRetire,
	HostRemove,
}

// The record's codes, which Hosts and Services share.
var (
	// CodeUnknownHost: no Host the caller can see has the name or id given.
	// A Host in a Space the caller cannot act in answers exactly as one
	// that does not exist.
	CodeUnknownHost = Code{"unknown_host", http.StatusNotFound}
	// CodeInvalidValue: a field, an Attribute or the Note, or a value given
	// for one, breaks its rule; the message names which and why.
	CodeInvalidValue = Code{"invalid_value", http.StatusBadRequest}
	// CodeHostWasManaged: host.remove of a Host that has been Managed, as
	// its pinned host key shows: such a Host is kept on record, never
	// deleted.
	CodeHostWasManaged = Code{"host_was_managed", http.StatusConflict}
	// CodeInvalidBatch: a batch host.add whose Hosts at fault are at fault
	// for different reasons; the error's items give each its own code.
	CodeInvalidBatch = Code{"invalid_batch", http.StatusBadRequest}
	// CodeHostRetired: the call would do what a Retired Host does not take:
	// change its address, port or Host Account, rename it, Enroll it,
	// retire it again, or change its Services. A Retired Host takes only
	// changes to its Attributes and Note, and moves.
	CodeHostRetired = Code{"host_retired", http.StatusConflict}
)

var hostCodes = []Code{
	CodeUnknownHost,
	CodeInvalidValue,
	CodeHostWasManaged,
	CodeInvalidBatch,
	CodeHostRetired,
}

// HostBatchError is the refusal of a host.add of n Hosts, n above 1, for the
// Hosts at fault, items, at least one, in the order of the batch: none of
// the n is added.
// Its code is the one every item shares, or CodeInvalidBatch when they
// differ, and its message names each item by its index.
func HostBatchError(n int, items []ItemError) *Error {
	code := items[0].Code
	var b strings.Builder
	fmt.Fprintf(&b, "none of the %d Hosts was added, for the %d at fault:", n, len(items))
	for _, it := range items {
		if it.Code != code {
			code = CodeInvalidBatch.Name
		}
		fmt.Fprintf(&b, "\n[%d] %s", it.Index, it.Message)
	}
	return &Error{Code: code, Message: b.String(), Items: items}
}
