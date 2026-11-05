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
)

var userOperations = []Operation{
	UserInvite,
	UserCurrent,
}

// The user area's codes.
var (
	// CodeUnknownUser: no User has the name given.
	CodeUnknownUser = Code{"unknown_user", http.StatusNotFound}
)

var userCodes = []Code{
	CodeUnknownUser,
}
