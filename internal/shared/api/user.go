package api

import "net/http"

// The roles of a User, as User.Role names them (ADR 0056).
const (
	// RoleOwner is the one Owner's.
	RoleOwner = "owner"
	// RoleAdministrator is an Administrator's: a User the Owner has made
	// able to do everything the Owner does but act on the Owner or another
	// Administrator.
	RoleAdministrator = "administrator"
	// RoleMember is every other User's, a Removed User's included.
	RoleMember = "member"
)

// User is a User as the API returns it.
type User struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Role is "owner" for the Owner, "administrator" for an Administrator
	// and "member" for every other User (RoleOwner, ...).
	Role string `json:"role"`
	// CreatedAt is RFC 3339 in UTC to the second.
	CreatedAt string `json:"created_at"`
	// RemovedAt is when the User was Removed, RFC 3339 in UTC to the
	// second; empty while they belong to the koko server.
	RemovedAt string `json:"removed_at,omitempty"`
	// PasswordSetAt is when the User last set their Password, RFC 3339 in
	// UTC to the second, as user.list answers it; empty while they have
	// none, and in every other answer.
	PasswordSetAt string `json:"password_set_at,omitempty"`
}

// UserInviteRequest invites a new User under a name no User or Space holds.
type UserInviteRequest struct {
	Name string `json:"name"`
	// ValidFor is how long the Invite stays valid, in seconds, 60 to
	// 604,800 (7 days); left out, 15 minutes.
	ValidFor *int `json:"valid_for,omitzero"`
	// Web makes the Invite a Password Link, for a person who will use the
	// web UI in a browser, instead of a Join Code.
	Web bool `json:"web,omitzero"`
}

// UserInviteResponse is the User user.invite created and their Invite: their
// first Join Code, or with web their first Password Link.
type UserInviteResponse struct {
	User User `json:"user"`
	// JoinCode is the Invite without web; left out with it.
	JoinCode *JoinCode `json:"join_code,omitzero"`
	// PasswordLink is the Invite with web; left out without it.
	PasswordLink *PasswordLink `json:"password_link,omitzero"`
}

// UserCurrentRequest asks who the call acts as.
type UserCurrentRequest struct{}

// UserCurrentResponse is who the call acts as: its User and the Device
// Session or the Sign-in it came on.
type UserCurrentResponse struct {
	User User `json:"user"`
	// DeviceSession is the Device Session the call came on, left out for the
	// Local caller, which has none and acts as the Owner, and for a Sign-in.
	DeviceSession *DeviceSession `json:"device_session,omitzero"`
	// SignIn is the Sign-in the call came on, from a browser on the web
	// listener; left out for every other caller.
	SignIn *CurrentSignIn `json:"signin,omitzero"`
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
	// OutcomeDeleted: the User had never used koko, neither joining a
	// device nor setting a Password, so they are gone with their Personal
	// Space, and their name is free again.
	OutcomeDeleted = "deleted"
	// OutcomeRemoved: the User had used koko, so they are Removed and kept
	// on record, and their name is never used again.
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

// UserPromoteRequest names a Member to make an Administrator.
type UserPromoteRequest struct {
	Name string `json:"name"`
}

// UserPromoteResponse is the User user.promote names, as it left them.
type UserPromoteResponse struct {
	// User is in user.list's shape, its role "administrator".
	User User `json:"user"`
}

// UserDemoteRequest names an Administrator to make a Member again.
type UserDemoteRequest struct {
	Name string `json:"name"`
}

// UserDemoteResponse is the User user.demote names, as it left them.
type UserDemoteResponse struct {
	// User is in user.list's shape, its role "member".
	User User `json:"user"`
}

// The user operations.
var (
	// UserInvite creates a Member with their Personal Space and issues their
	// first Join Code, or with Web their first Password Link, in one
	// transaction. A name any User, Removed ones included, or any Space
	// holds is refused with CodeNameTaken, a ValidFor outside 60 to 604,800
	// seconds with CodeInvalidValue, and Web while the Web URL is not set
	// with CodeWebURLUnset.
	UserInvite = Op[UserInviteRequest, UserInviteResponse]{Name: "user.invite", Write: true}
	// UserCurrent answers who the call acts as: the User of the Device
	// Session or the Sign-in it came on, or the Owner for the Local caller. `koko join`
	// asks it, to learn the User and device name its Join Code bound.
	UserCurrent = Op[UserCurrentRequest, UserCurrentResponse]{Name: "user.current"}
	// UserList lists the Users, without the Removed ones unless All asks
	// for them. Only the Owner or an Administrator lists Users.
	UserList = Op[UserListRequest, UserListResponse]{Name: "user.list"}
	// UserRemove deletes a User who never used koko, with their
	// memberships, their Join Codes, their Password Links and their
	// Personal Space, which must hold nothing (CodeSpaceNotEmpty), and
	// Removes any other User, in one transaction: it revokes their Device
	// Sessions, voids their valid Join Codes and those their Device
	// Sessions issued, ends their Sign-ins, deletes their Password, voids
	// their unused Password Links, takes them out of every Space,
	// and turns their Personal Space into an ordinary shared Space with no
	// Members, which the Owner and the Administrators see; an
	// Administrator's role goes with them. Once it commits, their Device
	// Sessions' Device Connections close. Only the Owner or an
	// Administrator removes Users, only the Owner removes an Administrator
	// (CodeOwnerOnly), and the Owner is never removed
	// (CodeOwnerNotRemovable).
	UserRemove = Op[UserRemoveRequest, UserRemoveResponse]{Name: "user.remove", Write: true}
	// UserPromote makes a Member an Administrator, and in the same
	// transaction voids their unused Join Codes and Password Links issued
	// by anyone but the Owner and the User themselves. Only the Owner
	// promotes (CodeOwnerOnly); the Owner is refused with
	// CodeOwnerNotRemovable, a Removed User with CodeUserRemoved. An
	// Administrator is answered as they are.
	UserPromote = Op[UserPromoteRequest, UserPromoteResponse]{Name: "user.promote", Write: true}
	// UserDemote makes an Administrator a Member again, and in the same
	// transaction voids the unused Join Codes and Password Links they
	// issued for other Users. Once it commits, their SSH Sessions to Hosts
	// outside the Spaces they still act in end. Only the Owner demotes
	// (CodeOwnerOnly); the Owner is refused with CodeOwnerNotRemovable, a
	// Removed User with CodeUserRemoved. A Member is answered as they are.
	UserDemote = Op[UserDemoteRequest, UserDemoteResponse]{Name: "user.demote", Write: true}
)

var userOperations = []Operation{
	UserInvite,
	UserCurrent,
	UserList,
	UserRemove,
	UserPromote,
	UserDemote,
}

// The user area's codes.
var (
	// CodeUnknownUser: no User has the name given.
	CodeUnknownUser = Code{"unknown_user", http.StatusNotFound}
	// CodeUserRemoved: the User named is Removed, so they are given no Join
	// Code and made a Member of no Space, and are not removed again.
	CodeUserRemoved = Code{"user_removed", http.StatusConflict}
	// CodeOwnerNotRemovable: the call would delete, Remove or change the
	// role of the Owner, whom the koko server always keeps as they are.
	CodeOwnerNotRemovable = Code{"owner_not_removable", http.StatusConflict}
)

var userCodes = []Code{
	CodeUnknownUser,
	CodeUserRemoved,
	CodeOwnerNotRemovable,
}
