package api

import (
	"net/http"

	"github.com/cieons/koko/internal/shared/record"
)

// Service is a Service as the API returns it: what runs on one Host, with
// the ports it listens on. It lives in its Host's Space and never moves to
// another Host.
type Service struct {
	ID string `json:"id"`
	// Name is a lowercase DNS label, unique on its Host.
	Name string `json:"name"`
	// Host is the name of the Host the Service runs on, or its id once the
	// Host is Retired and holds its name no more, so that <host>/<service>
	// always names the Service.
	Host string `json:"host"`
	// Ports are the ports the Service listens on, each written
	// <port>/<protocol>, such as 443/tcp or 51820/udp, by port and then
	// protocol; empty for a Service that listens on none.
	Ports []string `json:"ports"`
	// Attributes are the Service's Attributes, by key, typed as a Host's.
	Attributes map[string]record.Value `json:"attributes"`
	// Note is the Service's Note, left out when it has none. service.list
	// leaves every Note out unless with_note asks for them.
	Note string `json:"note,omitempty"`
	// CreatedAt and UpdatedAt are RFC 3339 in UTC to the second.
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// ServiceAddRequest records a Service on a Host.
type ServiceAddRequest struct {
	// Host names the Host, by its name or its id.
	Host string `json:"host"`
	// Name is a lowercase DNS label no other Service on the Host holds.
	Name string `json:"name"`
	// Ports are the ports it listens on, each <port>/<protocol> or a bare
	// <port> for tcp, none given twice; left out, it listens on none.
	// Other Services' ports are not checked.
	Ports []string `json:"ports,omitempty"`
	// Attributes are values by key, each of its key's type (package
	// record): a Preset's, or free text.
	Attributes map[string]record.Value `json:"attributes,omitempty"`
	// Note, when given, is the Service's Note: never empty.
	Note *string `json:"note,omitzero"`
}

// ServiceAddResponse is the Service service.add recorded.
type ServiceAddResponse struct {
	Service Service `json:"service"`
}

// ServiceListRequest asks for the Services on the Hosts the caller can see
// that match its filter, in its order.
type ServiceListRequest struct {
	// Filters are the terms a Service must match, every one. A term takes a
	// field of ServiceListFields: id, name and host, the name of its Host;
	// or an Attribute key of the Service's. A date is absolute.
	Filters []Filter `json:"filters,omitempty"`
	// Sort orders the Services by these keys, in turn, from the fields and
	// Attribute keys Filters takes; Services that tie stay by Host, then by
	// name and then id.
	Sort []SortKey `json:"sort,omitempty"`
	// WithNote asks for each Service's Note.
	WithNote bool `json:"with_note,omitzero"`
}

// ServiceListFields are the fields service.list filters and sorts on, by
// key, with the types of their values; every other key is an Attribute key
// of the Service's own. The ports, a set, have no comparison, and the Note
// and the times are no keys, as on host.list.
var ServiceListFields = map[string]record.Type{
	"id":   record.Text,
	"name": record.Text,
	"host": record.Text,
}

// ServiceListResponse holds the Services the caller can see that match the
// filter, by the sort keys, then by Host name, name and id.
type ServiceListResponse struct {
	Services []Service `json:"services"`
}

// ServiceShowRequest names a Service to show.
type ServiceShowRequest struct {
	// Host names its Host, by its name or its id.
	Host string `json:"host"`
	// Name is the Service's name on its Host.
	Name string `json:"name"`
}

// ServiceShowResponse is the Service service.show names.
type ServiceShowResponse struct {
	Service Service `json:"service"`
}

// ServiceSetRequest changes the ports, Attributes and Note of a Service that
// it names, and only those.
type ServiceSetRequest struct {
	// Host names its Host, by its name or its id.
	Host string `json:"host"`
	// Name is the Service's name on its Host.
	Name string `json:"name"`
	// Ports, when given, replace the Service's ports: at least one, written
	// as on service.add. Unset clears them.
	Ports []string `json:"ports,omitempty"`
	// Attributes set these Attributes, by key, leaving the others.
	Attributes map[string]record.Value `json:"attributes,omitempty"`
	// Note, when given, replaces the Service's Note: never empty.
	Note *string `json:"note,omitzero"`
	// Unset clears these Attribute keys, the Note for "note", and every
	// port for "ports". A key the Service lacks is already clear.
	Unset []string `json:"unset,omitempty"`
}

// ServiceSetResponse is the Service as service.set left it.
type ServiceSetResponse struct {
	Service Service `json:"service"`
}

// ServiceRenameRequest gives a Service a new name on its Host.
type ServiceRenameRequest struct {
	// Host names its Host, by its name or its id.
	Host string `json:"host"`
	// Name is the Service's name on its Host.
	Name string `json:"name"`
	// NewName is a name no other Service on the Host holds.
	NewName string `json:"new_name"`
}

// ServiceRenameResponse is the Service under its new name.
type ServiceRenameResponse struct {
	Service Service `json:"service"`
}

// ServiceRemoveRequest names a Service to delete.
type ServiceRemoveRequest struct {
	// Host names its Host, by its name or its id.
	Host string `json:"host"`
	// Name is the Service's name on its Host.
	Name string `json:"name"`
}

// ServiceRemoveResponse is the Service service.remove deleted, as it was.
type ServiceRemoveResponse struct {
	Service Service `json:"service"`
}

// The service operations. A Service lives in its Host's Space, so access to
// it is access to its Host: a Host the caller cannot see answers as one
// that does not exist, with CodeUnknownHost, and a Service its Host does not
// have with CodeUnknownService.
var (
	// ServiceAdd records a Service on a Host. A name another Service on the
	// Host holds is refused with CodeNameTaken; a value that breaks its
	// rule with CodeInvalidValue.
	ServiceAdd = Op[ServiceAddRequest, ServiceAddResponse]{Name: "service.add", Write: true}
	// ServiceList lists the Services on every Host the caller can see.
	ServiceList = Op[ServiceListRequest, ServiceListResponse]{Name: "service.list"}
	// ServiceShow shows a Service with its Note.
	ServiceShow = Op[ServiceShowRequest, ServiceShowResponse]{Name: "service.show"}
	// ServiceSet changes the ports, Attributes and Note it names, and only
	// those.
	ServiceSet = Op[ServiceSetRequest, ServiceSetResponse]{Name: "service.set", Write: true}
	// ServiceRename gives a Service a new name on its Host, refused with
	// CodeNameTaken as service.add refuses one.
	ServiceRename = Op[ServiceRenameRequest, ServiceRenameResponse]{Name: "service.rename", Write: true}
	// ServiceRemove deletes a Service, with its Attributes and Note.
	ServiceRemove = Op[ServiceRemoveRequest, ServiceRemoveResponse]{Name: "service.remove", Write: true}
)

var serviceOperations = []Operation{
	ServiceAdd,
	ServiceList,
	ServiceShow,
	ServiceSet,
	ServiceRename,
	ServiceRemove,
}

// CodeUnknownService: the Host, which the caller can see, has no Service of
// the name given.
var CodeUnknownService = Code{"unknown_service", http.StatusNotFound}

var serviceCodes = []Code{
	CodeUnknownService,
}
