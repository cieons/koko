package api

import "net/http"

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

// The user operations.
var (
	// UserInvite creates a Member with their Personal Space and issues their
	// first Join Code, in one transaction. A name any User, Removed ones
	// included, or any Space holds is refused with CodeNameTaken.
	UserInvite = Op[UserInviteRequest, UserInviteResponse]{Name: "user.invite", Write: true}
)

var userOperations = []Operation{
	UserInvite,
}

// The user area's codes.
var (
	// CodeUnknownUser: no User has the name given.
	CodeUnknownUser = Code{"unknown_user", http.StatusNotFound}
)

var userCodes = []Code{
	CodeUnknownUser,
}
