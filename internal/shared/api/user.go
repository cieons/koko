package api

import "net/http"

// The roles of a User, as User.Role names them.
const (
	RoleOwner  = "owner"
	RoleMember = "member"
)

// User is a User as the API returns it.
type User struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Role is "owner" for the Owner and "member" for every other User.
	Role string `json:"role"`
	// CreatedAt is RFC 3339 in UTC to the second.
	CreatedAt string `json:"created_at"`
	// RemovedAt is when the User was Removed, RFC 3339 in UTC to the
	// second; empty while they belong to the koko server.
	RemovedAt string `json:"removed_at,omitempty"`
}

// UserInviteRequest invites a new User under a name no User or Space holds.
type UserInviteRequest struct {
	Name string `json:"name"`
}

// UserInviteResponse is the User user.invite created and their Invite: their
// first Join Code.
type UserInviteResponse struct {
	User     User     `json:"user"`
	JoinCode JoinCode `json:"join_code"`
}

// UserCurrentRequest asks who the call acts as.
type UserCurrentRequest struct{}

// UserCurrentResponse is who the call acts as: its User and the Device
// Session it came on.
type UserCurrentResponse struct {
	User User `json:"user"`
	// DeviceSession is the Device Session the call came on, left out for the
	// Local caller, which has none and acts as the Owner.
	DeviceSession *DeviceSession `json:"device_session,omitzero"`
}

// UserListRequest lists the Users.
type UserListRequest struct {
	// All adds the Removed Users.
	All bool `json:"all,omitzero"`
}

// UserListResponse is every User user.list selected, by name.
type UserListResponse struct {
	Users []User `json:"users"`
}

// UserRemoveRequest names a User to delete or Remove.
type UserRemoveRequest struct {
	Name string `json:"name"`
}

// What user.remove did, as UserRemoveResponse.Outcome names it.
const (
	// OutcomeDeleted: the User had never joined, so they are gone with
	// their Personal Space, and their name is free again.
	OutcomeDeleted = "deleted"
	// OutcomeRemoved: the User had joined, so they are Removed and kept on
	// record, and their name is never used again.
	OutcomeRemoved = "removed"
)

// UserRemoveResponse is what user.remove did to the User: deleted them, or
// Removed them.
type UserRemoveResponse struct {
	// Outcome says what the call did: "deleted" (OutcomeDeleted) or
	// "removed" (OutcomeRemoved).
	Outcome string `json:"outcome"`
	// User is the User as the call left them: as they were, when deleted,
	// and with their Removed time, when Removed.
	User User `json:"user"`
}

// The user operations.
var (
	// UserInvite creates a Member with their Personal Space and issues their
	// first Join Code, in one transaction. A name any User, Removed ones
	// included, or any Space holds is refused with CodeNameTaken.
	UserInvite = Op[UserInviteRequest, UserInviteResponse]{Name: "user.invite", Write: true}
	// UserCurrent answers who the call acts as: the User of the Device
	// Session it came on, or the Owner for the Local caller. `koko join`
	// asks it, to learn the User and device name its Join Code bound.
	UserCurrent = Op[UserCurrentRequest, UserCurrentResponse]{Name: "user.current"}
	// UserList lists the Users, without the Removed ones unless All asks
	// for them. Only the Owner lists Users.
	UserList = Op[UserListRequest, UserListResponse]{Name: "user.list"}
	// UserRemove deletes a User who never joined, with their memberships,
	// their Join Codes and their Personal Space, which must hold nothing
	// (CodeSpaceNotEmpty), and Removes any other User, in one transaction:
	// it revokes their Device Sessions, voids their valid Join Codes and
	// those their Device Sessions issued, takes them out of every Space,
	// and turns their Personal Space into an ordinary shared Space with no
	// Members, which only the Owner sees. Once it commits, their Device
	// Sessions' Device Connections close. Only the Owner removes Users, and
	// the Owner is never removed (CodeOwnerNotRemovable).
	UserRemove = Op[UserRemoveRequest, UserRemoveResponse]{Name: "user.remove", Write: true}
)

var userOperations = []Operation{
	UserInvite,
	UserCurrent,
	UserList,
	UserRemove,
}

// The user area's codes.
var (
	// CodeUnknownUser: no User has the name given.
	CodeUnknownUser = Code{"unknown_user", http.StatusNotFound}
	// CodeUserRemoved: the User named is Removed, so they are given no Join
	// Code and made a Member of no Space, and are not removed again.
	CodeUserRemoved = Code{"user_removed", http.StatusConflict}
	// CodeOwnerNotRemovable: the call would delete or Remove the Owner,
	// whom the koko server always keeps.
	CodeOwnerNotRemovable = Code{"owner_not_removable", http.StatusConflict}
)

var userCodes = []Code{
	CodeUnknownUser,
	CodeUserRemoved,
	CodeOwnerNotRemovable,
}
