package api

import "net/http"

// Space is a Space as the API returns it.
type Space struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Personal is set for a User's Personal Space, named after them, which
	// cannot be renamed, removed or given Members while its User exists.
	Personal bool `json:"personal"`
}

// SpaceDetail is a Space with its Members and how many records it holds.
type SpaceDetail struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Personal bool   `json:"personal"`
	// Members are the Users who are Members of the Space, by name. A
	// Personal Space has none: it belongs to its User, which is not a
	// membership.
	Members []SpaceMember `json:"members"`
	// Counts are how many records of each kind the Space holds.
	Counts SpaceCounts `json:"counts"`
}

// SpaceMember is a User who is a Member of a Space.
type SpaceMember struct {
	// ID is the User's id.
	ID string `json:"id"`
	// Name is the User's name.
	Name string `json:"name"`
}

// SpaceCounts are how many records of each kind a Space holds. A Space that
// holds any cannot be removed.
type SpaceCounts struct {
	// Hosts counts the Hosts that are not Retired, which ordinary lists
	// show.
	Hosts int `json:"hosts"`
	// RetiredHosts counts the Retired Hosts, which a Space keeps as history.
	RetiredHosts int `json:"retired_hosts"`
	Secrets      int `json:"secrets"`
}

// SpaceAddRequest records a shared Space under a new name.
type SpaceAddRequest struct {
	Name string `json:"name"`
}

// SpaceAddResponse is the Space space.add recorded.
type SpaceAddResponse struct {
	Space Space `json:"space"`
}

// SpaceListRequest asks for every Space the caller can act in.
type SpaceListRequest struct{}

// SpaceListResponse holds every Space the caller can act in, by name.
type SpaceListResponse struct {
	Spaces []Space `json:"spaces"`
}

// SpaceShowRequest names a Space to show.
type SpaceShowRequest struct {
	Name string `json:"name"`
}

// SpaceShowResponse is the Space space.show names, with its Members and
// counts.
type SpaceShowResponse struct {
	Space SpaceDetail `json:"space"`
}

// SpaceRenameRequest gives a shared Space a new name.
type SpaceRenameRequest struct {
	Name string `json:"name"`
	// NewName is a name no Space holds.
	NewName string `json:"new_name"`
}

// SpaceRenameResponse is the Space under its new name.
type SpaceRenameResponse struct {
	Space Space `json:"space"`
}

// SpaceRemoveRequest names an empty shared Space to remove.
type SpaceRemoveRequest struct {
	Name string `json:"name"`
}

// SpaceRemoveResponse is the Space space.remove removed, as it was.
type SpaceRemoveResponse struct {
	Space Space `json:"space"`
}

// SpaceMemberAddRequest makes Users Members of a shared Space.
type SpaceMemberAddRequest struct {
	// Space names the Space.
	Space string `json:"space"`
	// Users names the Users, at least one. A User who is a Member already
	// stays one.
	Users []string `json:"users"`
}

// SpaceMemberAddResponse is the Space with its Members after the call.
type SpaceMemberAddResponse struct {
	Space SpaceDetail `json:"space"`
}

// SpaceMemberRemoveRequest takes Users out of a Space.
type SpaceMemberRemoveRequest struct {
	// Space names the Space.
	Space string `json:"space"`
	// Users names the Users, at least one. A User who is not a Member stays
	// out.
	Users []string `json:"users"`
}

// SpaceMemberRemoveResponse is the Space with its Members after the call.
type SpaceMemberRemoveResponse struct {
	Space SpaceDetail `json:"space"`
}

// The space operations.
var (
	// SpaceAdd records a shared Space. Only the Owner adds one; a name
	// another Space holds is refused with CodeNameTaken.
	SpaceAdd = Op[SpaceAddRequest, SpaceAddResponse]{Name: "space.add", Write: true}
	// SpaceList lists the Spaces the caller can act in.
	SpaceList = Op[SpaceListRequest, SpaceListResponse]{Name: "space.list"}
	// SpaceShow shows a Space the caller can act in, with its Members and
	// counts. A Space the caller cannot act in is refused exactly as one
	// that does not exist, with CodeUnknownSpace.
	SpaceShow = Op[SpaceShowRequest, SpaceShowResponse]{Name: "space.show"}
	// SpaceRename gives a shared Space a new name, which no Space may hold
	// (CodeNameTaken); its id, Members and records stay. Only the Owner
	// renames, and a Personal Space is refused with CodePersonalSpace.
	SpaceRename = Op[SpaceRenameRequest, SpaceRenameResponse]{Name: "space.rename", Write: true}
	// SpaceRemove removes a shared Space with its memberships. Only the
	// Owner removes; a Space that holds any record is refused with
	// CodeSpaceNotEmpty, and a Personal Space with CodePersonalSpace.
	SpaceRemove = Op[SpaceRemoveRequest, SpaceRemoveResponse]{Name: "space.remove", Write: true}
	// SpaceMemberAdd makes Users Members of a Space, all or none: a name no
	// User holds is refused with CodeUnknownUser and adds nobody. Only the
	// Owner changes Members, and a Personal Space is refused with
	// CodePersonalSpace.
	SpaceMemberAdd = Op[SpaceMemberAddRequest, SpaceMemberAddResponse]{Name: "space.member.add", Write: true}
	// SpaceMemberRemove takes Users out of a Space, all or none, as
	// SpaceMemberAdd adds them. Only the Owner changes Members.
	SpaceMemberRemove = Op[SpaceMemberRemoveRequest, SpaceMemberRemoveResponse]{Name: "space.member.remove", Write: true}
)

var spaceOperations = []Operation{
	SpaceAdd,
	SpaceList,
	SpaceShow,
	SpaceRename,
	SpaceRemove,
	SpaceMemberAdd,
	SpaceMemberRemove,
}

// The space area's codes.
var (
	// CodeUnknownSpace: no Space the caller can act in has the name given.
	// A Space the caller cannot act in answers exactly as one that does not
	// exist.
	CodeUnknownSpace = Code{"unknown_space", http.StatusNotFound}
	// CodePersonalSpace: a Personal Space cannot be renamed, removed or
	// given Members while its User exists.
	CodePersonalSpace = Code{"personal_space", http.StatusConflict}
	// CodeSpaceNotEmpty: a Space that still holds a record, such as a Host,
	// Retired ones included, or a Secret, cannot be removed; move or remove
	// what it holds first.
	CodeSpaceNotEmpty = Code{"space_not_empty", http.StatusConflict}
)

var spaceCodes = []Code{
	CodeUnknownSpace,
	CodePersonalSpace,
	CodeSpaceNotEmpty,
}
