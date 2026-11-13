package api

import (
	"net/http"
	"reflect"
)

// The rule a Password follows (NIST SP 800-63B-4, for a password that is the
// only factor): MinPasswordLength to MaxPasswordLength Unicode code points
// after NFC normalisation, any characters, and not the User's own name.
const (
	MinPasswordLength = 15
	MaxPasswordLength = 128
)

// PasswordLink is a Password Link just issued, as the API returns it once:
// its secret is never stored and never shown again.
type PasswordLink struct {
	ID string `json:"id"`
	// User is the name of the User whose Password the link sets.
	User string `json:"user"`
	// Link is the link itself: the Web URL followed by /password#<secret>.
	Link string `json:"link"`
	// ExpiresAt is when the link stops working, as long after its issue as
	// the request's valid_for chose, 15 minutes by default, as RFC 3339 in
	// UTC to the second.
	ExpiresAt string `json:"expires_at"`
}

// The reasons a Sign-in ended, as SignIn.EndReason names them.
const (
	EndSignedOut       = "signed_out"
	EndExpired         = "expired"
	EndRevoked         = "revoked"
	EndPasswordChanged = "password_changed"
	EndPasswordRemoved = "password_removed"
	EndUserRemoved     = "user_removed"
)

// SignIn is a Sign-in as the API returns it: one browser signed in with a
// User's Password. Every time is RFC 3339 in UTC to the second.
type SignIn struct {
	ID string `json:"id"`
	// User is the name of the User the browser is signed in as.
	User string `json:"user"`
	// Client is the browser's English label, made of Browser and System
	// by ClientLabel, such as "Safari on iPhone", or "Browser"; the CLI
	// prints it.
	Client string `json:"client"`
	// Browser is the browser its User-Agent named at sign-in, such as
	// "Safari"; left out when it named none koko recognises.
	Browser string `json:"browser,omitempty"`
	// System is the system its User-Agent named at sign-in, such as
	// "iPhone"; left out when it named none koko recognises.
	System string `json:"system,omitempty"`
	// Address is the client's address when the Sign-in was last used, as
	// the reverse proxy passed it on.
	Address string `json:"address"`
	// StartedAt is when the browser signed in.
	StartedAt string `json:"started_at"`
	// LastUsedAt is when the Sign-in was last used, kept to the minute.
	LastUsedAt string `json:"last_used_at"`
	// ExpiresAt is when the Sign-in ends unless it is used again first: 7
	// days after it was last used, and never later than 30 days after it
	// began.
	ExpiresAt string `json:"expires_at"`
	// EndedAt is when the Sign-in ended; empty while it is live.
	EndedAt string `json:"ended_at,omitempty"`
	// EndReason is why it ended: "signed_out", "expired", "revoked",
	// "password_changed", "password_removed" or "user_removed"; empty while
	// it is live.
	EndReason string `json:"end_reason,omitempty"`
}

// CurrentSignIn is the Sign-in a call came on, as user.current names it.
type CurrentSignIn struct {
	ID string `json:"id"`
	// Client, Browser and System name the browser, as SignIn's do.
	Client    string `json:"client"`
	Browser   string `json:"browser,omitempty"`
	System    string `json:"system,omitempty"`
	StartedAt string `json:"started_at"`
	ExpiresAt string `json:"expires_at"`
}

// ClientLabel is a Sign-in's English label of its browser and system:
// "Safari on iPhone", the browser alone without a system, or "Browser"
// without a browser koko recognises. The web UI makes the same label in
// the reader's language.
func ClientLabel(browser, system string) string {
	switch {
	case browser == "":
		return "Browser"
	case system == "":
		return browser
	}
	return browser + " on " + system
}

// PasswordLinkRequest issues a Password Link for the User Name names.
type PasswordLinkRequest struct {
	User string `json:"user"`
	// ValidFor is how long the link stays valid, in seconds, 60 to 604,800
	// (7 days); left out, 15 minutes.
	ValidFor *int `json:"valid_for,omitzero"`
}

// PasswordLinkResponse is the Password Link password.link issued.
type PasswordLinkResponse struct {
	PasswordLink PasswordLink `json:"password_link"`
}

// PasswordRemoveRequest names the User whose Password to remove.
type PasswordRemoveRequest struct {
	User string `json:"user"`
}

// PasswordRemoveResponse names the User whose Password was removed.
type PasswordRemoveResponse struct {
	User string `json:"user"`
}

// SignInListRequest lists Sign-ins.
type SignInListRequest struct {
	// User lists only the Sign-ins of the User it names.
	User string `json:"user,omitzero"`
	// All adds the Sign-ins that have ended.
	All bool `json:"all,omitzero"`
}

// SignInListResponse is every Sign-in signin.list selected, by User name
// and then by when each began.
type SignInListResponse struct {
	SignIns []SignIn `json:"signins"`
}

// SignInRevokeRequest names the Sign-in to revoke by its id.
type SignInRevokeRequest struct {
	ID string `json:"id"`
}

// SignInRevokeResponse is the Sign-in as signin.revoke left it.
type SignInRevokeResponse struct {
	SignIn SignIn `json:"signin"`
}

// The signin operations.
var (
	// PasswordLinkIssue issues a Password Link for a User who is not Removed,
	// the Owner included, voiding their earlier unused one. Only the Owner
	// or an Administrator issues one, and a Member is refused before the
	// name is read; only the Owner issues one for the Owner or another
	// Administrator (CodeOwnerOnly). It is
	// refused with CodeWebURLUnset while the Web URL is not set, and a
	// ValidFor outside 60 to 604,800 seconds with CodeInvalidValue.
	PasswordLinkIssue = Op[PasswordLinkRequest, PasswordLinkResponse]{Name: "password.link", Write: true}
	// PasswordRemove removes a User's Password, ends their Sign-ins and
	// voids their unused Password Link, keeping the User and their
	// devices. A User without a Password is refused with CodeNoPassword.
	// Only the Owner or an Administrator removes one, and only the Owner
	// removes the Owner's or another Administrator's (CodeOwnerOnly).
	PasswordRemove = Op[PasswordRemoveRequest, PasswordRemoveResponse]{Name: "password.remove", Write: true}
	// SignInList lists the caller's own Sign-ins, and everyone's for the
	// Owner or an Administrator, without the ended ones unless All asks for them.
	SignInList = Op[SignInListRequest, SignInListResponse]{Name: "signin.list"}
	// SignInRevoke ends a live Sign-in: anyone's own, anyone's for the
	// Owner, and any Member's for an Administrator. Another User's, for a
	// Member, is refused with CodeUnknownSignIn, as an id no Sign-in holds,
	// and the Owner's or another Administrator's, for an Administrator, with
	// CodeOwnerOnly.
	SignInRevoke = Op[SignInRevokeRequest, SignInRevokeResponse]{Name: "signin.revoke", Write: true}
)

var signinOperations = []Operation{
	PasswordLinkIssue,
	PasswordRemove,
	SignInList,
	SignInRevoke,
}

// The signin area's codes.
var (
	// CodeWebURLUnset: a Password Link would name no address, since the
	// Owner has not set the Web URL (koko server set web_url=).
	CodeWebURLUnset = Code{"web_url_unset", http.StatusConflict}
	// CodeInvalidPassword: a new Password breaks the rule: 15 to 128
	// characters, and not the User's name.
	CodeInvalidPassword = Code{"invalid_password", http.StatusBadRequest}
	// CodeWrongPassword: the User name or the Password given is wrong; the
	// answer is the same for both.
	CodeWrongPassword = Code{"wrong_password", http.StatusUnauthorized}
	// CodeTooManyAttempts: too many failed attempts came from this address,
	// or for this User name; the Retry-After header says when to try again.
	CodeTooManyAttempts = Code{"too_many_attempts", http.StatusTooManyRequests}
	// CodeLinkUsed: the Password Link was used already.
	CodeLinkUsed = Code{"link_used", http.StatusGone}
	// CodeLinkInvalid: the Password Link expired, was voided by a newer
	// one, is for a Removed User, or was never issued.
	CodeLinkInvalid = Code{"link_invalid", http.StatusNotFound}
	// CodeUnknownSignIn: no Sign-in the caller may see has the id given.
	CodeUnknownSignIn = Code{"unknown_signin", http.StatusNotFound}
	// CodeNoPassword: the User has no Password to remove.
	CodeNoPassword = Code{"no_password", http.StatusConflict}
)

var signinCodes = []Code{
	CodeWebURLUnset,
	CodeInvalidPassword,
	CodeWrongPassword,
	CodeTooManyAttempts,
	CodeLinkUsed,
	CodeLinkInvalid,
	CodeUnknownSignIn,
	CodeNoPassword,
}

// The web front's own endpoints under /auth/ on the web listener, apart
// from the API (ADR 0043): each takes and answers JSON, at most
// MaxAuthBody bytes, from a page of the Web URL.
const (
	// AuthSignInPath signs in with an AuthSignInRequest, setting the
	// Sign-in cookie, and answers an AuthSignInResponse.
	AuthSignInPath = "/auth/signin"
	// AuthLinkPath answers, with an AuthLinkResponse, whom the unspent
	// Password Link of an AuthLinkRequest is for, spending nothing.
	AuthLinkPath = "/auth/link"
	// AuthPasswordPath sets a Password with an AuthPasswordRequest, through
	// a Password Link or by the current Password, and answers an
	// AuthSignInResponse.
	AuthPasswordPath = "/auth/password"
	// AuthSignOutPath ends the request's Sign-in and clears its cookie,
	// answering an AuthSignOutResponse.
	AuthSignOutPath = "/auth/signout"
	// MaxAuthBody is the largest body an /auth/ endpoint reads.
	MaxAuthBody = 4 << 10
)

// AuthEndpoint is one of the web front's own endpoints apart from the API,
// under /auth/ or the web terminal's under /terminal/files/ (FilesPrefix): a
// POST of a Req's JSON, answered by a Resp's, or by the error object. The
// web UI's generated types carry each one with its types, as they carry an
// Op.
type AuthEndpoint[Req, Resp any] struct {
	// Path is the endpoint's URL path, such as AuthSignInPath.
	Path string
}

// Info describes the endpoint without its types' parameters.
func (e AuthEndpoint[Req, Resp]) Info() AuthEndpointInfo {
	return AuthEndpointInfo{
		Path:     e.Path,
		Request:  reflect.TypeFor[Req](),
		Response: reflect.TypeFor[Resp](),
	}
}

// AuthEndpointInfo describes one /auth/ endpoint.
type AuthEndpointInfo struct {
	Path     string
	Request  reflect.Type
	Response reflect.Type
}

// The /auth/ endpoints, with their request and response types.
var (
	// AuthSignIn signs in with a User name and Password, setting the
	// Sign-in cookie. A wrong name or Password is refused with
	// CodeWrongPassword, and an address or name past its limit with
	// CodeTooManyAttempts.
	AuthSignIn = AuthEndpoint[AuthSignInRequest, AuthSignInResponse]{Path: AuthSignInPath}
	// AuthLink answers whom an unspent Password Link is for, spending
	// nothing. A link used already is refused with CodeLinkUsed, and one
	// that expired, was voided or was never issued with CodeLinkInvalid.
	AuthLink = AuthEndpoint[AuthLinkRequest, AuthLinkResponse]{Path: AuthLinkPath}
	// AuthPassword sets the User's Password: through a Password Link,
	// spending it and signing this browser in, or by the current Password,
	// keeping this Sign-in and ending the User's others. A Password that
	// breaks the rule is refused with CodeInvalidPassword, and a wrong
	// current one with CodeWrongPassword.
	AuthPassword = AuthEndpoint[AuthPasswordRequest, AuthSignInResponse]{Path: AuthPasswordPath}
	// AuthSignOut ends this browser's Sign-in and clears its cookie.
	AuthSignOut = AuthEndpoint[AuthSignOutRequest, AuthSignOutResponse]{Path: AuthSignOutPath}
)

// AuthEndpoints returns every /auth/ endpoint.
func AuthEndpoints() []AuthEndpointInfo {
	return []AuthEndpointInfo{AuthSignIn.Info(), AuthLink.Info(), AuthPassword.Info(), AuthSignOut.Info()}
}

// AuthSignInRequest signs in as the User Name names.
type AuthSignInRequest struct {
	Name     string `json:"name"`
	Password string `json:"password"`
}

// AuthSignInResponse is the User a browser is signed in as.
type AuthSignInResponse struct {
	User User `json:"user"`
}

// AuthLinkRequest names a Password Link by its secret, the part of the
// link after #.
type AuthLinkRequest struct {
	Link string `json:"link"`
}

// AuthLinkResponse is whom an unspent Password Link is for.
type AuthLinkResponse struct {
	// User is the name the User signs in with.
	User      string `json:"user"`
	ExpiresAt string `json:"expires_at"`
}

// AuthPasswordRequest sets the User's Password: through a Password Link,
// given Link, which signs this browser in; or, given Current with the
// Sign-in cookie, by the current Password, which keeps this Sign-in and
// ends the User's others.
type AuthPasswordRequest struct {
	Link     string `json:"link,omitzero"`
	Current  string `json:"current,omitzero"`
	Password string `json:"password"`
}

// AuthSignOutRequest signs out the request's Sign-in.
type AuthSignOutRequest struct{}

// AuthSignOutResponse answers a sign-out.
type AuthSignOutResponse struct{}
