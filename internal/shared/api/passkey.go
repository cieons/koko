package api

import "net/http"

// Passkey is a Passkey as the API returns it: one a User's device or
// password manager made for the Web URL's host, which signs them in to the
// web UI there (ADR 0060). Every time is RFC 3339 in UTC to the second.
type Passkey struct {
	ID string `json:"id"`
	// User is the name of the User whose Passkey it is.
	User string `json:"user"`
	// Name is what its User called it when it was made: on Account, the
	// name they gave it, prefilled with the browser's label; through a
	// Sign-in Link, the browser's label.
	Name string `json:"name"`
	// Host is the host it was made for, the Web URL's then; it signs in
	// only while the Web URL names that host.
	Host string `json:"host"`
	// Synced says the authenticator backs it up, as a password manager that
	// syncs it does, as of its last use.
	Synced bool `json:"synced"`
	// AddedBy is the name of the User who issued the Sign-in Link it was
	// made through, the Owner for a link the koko server's machine issued;
	// null when its User made it from a Sign-in of their own.
	AddedBy *string `json:"added_by"`
	AddedAt string  `json:"added_at"`
	// LastUsedAt is when it last signed a browser in; null until then.
	LastUsedAt *string `json:"last_used_at"`
}

// PasskeyListRequest lists Passkeys.
type PasskeyListRequest struct {
	// User lists only the Passkeys of the User it names; left out, the
	// caller's own.
	User string `json:"user,omitzero"`
}

// PasskeyListResponse is every Passkey passkey.list selected, by when each
// was made.
type PasskeyListResponse struct {
	Passkeys []Passkey `json:"passkeys"`
}

// PasskeyRemoveRequest names the Passkey to remove by its id.
type PasskeyRemoveRequest struct {
	ID string `json:"id"`
}

// PasskeyRemoveResponse is the Passkey removed, and how many Sign-ins made
// with it ended.
type PasskeyRemoveResponse struct {
	Passkey      Passkey `json:"passkey"`
	SignInsEnded int     `json:"signins_ended"`
}

// The passkey operations (ADR 0060).
var (
	// PasskeyList lists the caller's own Passkeys, or with User another
	// User's, which only the Owner and the Administrators see: a Member
	// naming another User gets none, whether or not the name is a User's.
	PasskeyList = Op[PasskeyListRequest, PasskeyListResponse]{Name: "passkey.list"}
	// PasskeyRemove removes a Passkey and ends the Sign-ins made with it,
	// and no others: anyone's own; another User's for the Owner and the
	// Administrators, as access lets them act on that User. Another User's,
	// for a Member, is refused with CodeUnknownPasskey, as an id no
	// Passkey holds, and the Owner's or another Administrator's, for an
	// Administrator, with CodeOwnerOnly.
	PasskeyRemove = Op[PasskeyRemoveRequest, PasskeyRemoveResponse]{Name: "passkey.remove", Write: true}
)

var passkeyOperations = []Operation{
	PasskeyList,
	PasskeyRemove,
}

// The passkey area's codes.
var (
	// CodePasskeyRefused: a passkey's answer did not verify, named no
	// Passkey for this host, or came for a flow that was used, expired or
	// never begun; the answer is the same for each.
	CodePasskeyRefused = Code{"passkey_refused", http.StatusUnauthorized}
	// CodeUnknownPasskey: no Passkey the caller may see has the id given.
	CodeUnknownPasskey = Code{"unknown_passkey", http.StatusNotFound}
)

var passkeyCodes = []Code{
	CodePasskeyRefused,
	CodeUnknownPasskey,
}

// The passkey endpoints under /auth/ on the web listener: the two halves
// of each ceremony, its options and then the browser's answer.
const (
	// AuthPasskeySignInBeginPath begins signing in with a passkey,
	// answering an AuthPasskeySignInBeginResponse.
	AuthPasskeySignInBeginPath = "/auth/passkey/signin/begin"
	// AuthPasskeySignInPath signs in with a passkey's answer, an
	// AuthPasskeySignInRequest, setting the Sign-in cookie, and answers an
	// AuthSignInResponse.
	AuthPasskeySignInPath = "/auth/passkey/signin"
	// AuthPasskeyAddBeginPath begins adding a passkey, through a Sign-in
	// Link or from the request's Sign-in, answering an
	// AuthPasskeyAddBeginResponse.
	AuthPasskeyAddBeginPath = "/auth/passkey/add/begin"
	// AuthPasskeyAddPath records a passkey from its answer, an
	// AuthPasskeyAddRequest, and answers an AuthPasskeyAddResponse.
	AuthPasskeyAddPath = "/auth/passkey/add"
	// MaxPasskeyBody is the largest body the two endpoints that carry a
	// passkey's answer read: an RSA key, or an attestation statement an
	// authenticator kept, takes more than MaxAuthBody.
	MaxPasskeyBody = 64 << 10
	// PasskeyTimeout is how long, in milliseconds, the browser waits for
	// the person at each ceremony, as long as its flow lasts.
	PasskeyTimeout = 300_000
)

// The passkey endpoints, with their request and response types.
var (
	// AuthPasskeySignInBegin begins signing in with a passkey: it answers
	// the options for navigator.credentials.get() and the flow its answer
	// names. It counts for no rate limit.
	AuthPasskeySignInBegin = AuthEndpoint[AuthPasskeySignInBeginRequest, AuthPasskeySignInBeginResponse]{Path: AuthPasskeySignInBeginPath}
	// AuthPasskeySignIn signs in with a passkey's answer, setting the
	// Sign-in cookie. An answer that does not verify, names no Passkey for
	// this host, or names a flow used, expired or never begun, is refused
	// with CodePasskeyRefused, the same bytes for each; an address past its
	// limit with CodeTooManyAttempts.
	AuthPasskeySignIn = AuthEndpoint[AuthPasskeySignInRequest, AuthSignInResponse]{Path: AuthPasskeySignInPath}
	// AuthPasskeyAddBegin begins adding a passkey: through a Sign-in Link,
	// given Link, or given none, from the request's Sign-in. It answers the
	// options for navigator.credentials.create() and the flow its answer
	// names, spending nothing.
	AuthPasskeyAddBegin = AuthEndpoint[AuthPasskeyAddBeginRequest, AuthPasskeyAddBeginResponse]{Path: AuthPasskeyAddBeginPath}
	// AuthPasskeyAdd records a passkey from its answer. Through a Sign-in
	// Link, it spends the link once the answer verifies, ends the User's
	// other Sign-ins and signs this browser in, setting the cookie; from a
	// Sign-in, it keeps the Sign-in. An answer that does not verify is
	// refused with CodePasskeyRefused.
	AuthPasskeyAdd = AuthEndpoint[AuthPasskeyAddRequest, AuthPasskeyAddResponse]{Path: AuthPasskeyAddPath}
)

// PasskeyEndpoints returns the passkey endpoints.
func PasskeyEndpoints() []AuthEndpointInfo {
	return []AuthEndpointInfo{AuthPasskeySignInBegin.Info(), AuthPasskeySignIn.Info(), AuthPasskeyAddBegin.Info(), AuthPasskeyAdd.Info()}
}

// AuthPasskeySignInBeginRequest begins signing in with a passkey.
type AuthPasskeySignInBeginRequest struct{}

// AuthPasskeySignInBeginResponse is a sign-in's flow and the options to
// pass navigator.credentials.get().
type AuthPasskeySignInBeginResponse struct {
	// Flow names the ceremony; the answer posts it back. It is secret to
	// the page.
	Flow    string                `json:"flow"`
	Options PasskeyRequestOptions `json:"options"`
}

// AuthPasskeySignInRequest is a passkey's answer to a sign-in's options.
type AuthPasskeySignInRequest struct {
	Flow       string           `json:"flow"`
	Credential PasskeyAssertion `json:"credential"`
}

// AuthPasskeyAddBeginRequest begins adding a passkey: through the Sign-in
// Link whose secret Link is, or, with Link left out, from the request's
// Sign-in.
type AuthPasskeyAddBeginRequest struct {
	Link string `json:"link,omitzero"`
}

// AuthPasskeyAddBeginResponse is a passkey's creation flow and the options
// to pass navigator.credentials.create().
type AuthPasskeyAddBeginResponse struct {
	Flow    string                 `json:"flow"`
	Options PasskeyCreationOptions `json:"options"`
}

// AuthPasskeyAddRequest is a passkey's answer to a creation's options.
type AuthPasskeyAddRequest struct {
	Flow       string              `json:"flow"`
	Credential PasskeyRegistration `json:"credential"`
	// Name is the Passkey's name, 1 to MaxPasskeyNameLength characters,
	// taken only from a Sign-in; left out, or through a Sign-in Link, the
	// browser's label.
	Name string `json:"name,omitzero"`
}

// AuthPasskeyAddResponse is the User signed in and the Passkey made.
type AuthPasskeyAddResponse struct {
	User    User    `json:"user"`
	Passkey Passkey `json:"passkey"`
}

// MaxPasskeyNameLength is the longest name of a Passkey, in Unicode code
// points.
const MaxPasskeyNameLength = 64

// PasskeyRequestOptions is what koko asks of an authenticator to sign in,
// as WebAuthn Level 3's PublicKeyCredentialRequestOptionsJSON: binary
// members in base64url.
type PasskeyRequestOptions struct {
	Challenge string `json:"challenge"`
	// RPID is the Web URL's host.
	RPID             string `json:"rpId"`
	Timeout          int    `json:"timeout"`
	UserVerification string `json:"userVerification"`
	// AllowCredentials is empty: the authenticator offers its discoverable
	// passkeys for the host.
	AllowCredentials []PasskeyDescriptor `json:"allowCredentials"`
}

// PasskeyCreationOptions is what koko asks of an authenticator to make a
// passkey, as WebAuthn Level 3's PublicKeyCredentialCreationOptionsJSON:
// binary members in base64url.
type PasskeyCreationOptions struct {
	RP PasskeyRP `json:"rp"`
	// User names the User and nothing else about the person.
	User             PasskeyUser    `json:"user"`
	Challenge        string         `json:"challenge"`
	PubKeyCredParams []PasskeyParam `json:"pubKeyCredParams"`
	Timeout          int            `json:"timeout"`
	// ExcludeCredentials are the User's Passkeys for this host, which the
	// authenticator does not make again.
	ExcludeCredentials     []PasskeyDescriptor `json:"excludeCredentials"`
	AuthenticatorSelection PasskeySelection    `json:"authenticatorSelection"`
	Attestation            string              `json:"attestation"`
}

// PasskeyRP is the relying party a passkey is made for: the Web URL's host.
type PasskeyRP struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// PasskeyUser is the User a passkey is made for: their id, in base64url,
// and their name.
type PasskeyUser struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
}

// PasskeyParam is one algorithm koko takes, by its COSE number.
type PasskeyParam struct {
	Type string `json:"type"`
	Alg  int    `json:"alg"`
}

// PasskeyDescriptor names one passkey by its credential id, in base64url.
type PasskeyDescriptor struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

// PasskeySelection is what koko asks of the authenticator itself: a
// discoverable passkey, and that it checks who is there.
type PasskeySelection struct {
	ResidentKey        string `json:"residentKey"`
	RequireResidentKey bool   `json:"requireResidentKey"`
	UserVerification   string `json:"userVerification"`
}

// PasskeyRegistration is a browser's answer to
// navigator.credentials.create(), as PublicKeyCredential.toJSON() gives
// it: binary members in base64url. Members koko does not read are taken
// and left unread.
type PasskeyRegistration struct {
	ID       string                      `json:"id"`
	RawID    string                      `json:"rawId"`
	Type     string                      `json:"type"`
	Response PasskeyRegistrationResponse `json:"response"`
}

// PasskeyRegistrationResponse is a creation's AuthenticatorAttestationResponse.
type PasskeyRegistrationResponse struct {
	ClientDataJSON    string `json:"clientDataJSON"`
	AttestationObject string `json:"attestationObject"`
}

// PasskeyAssertion is a browser's answer to navigator.credentials.get(),
// as PublicKeyCredential.toJSON() gives it: binary members in base64url.
// Members koko does not read are taken and left unread.
type PasskeyAssertion struct {
	ID       string                   `json:"id"`
	RawID    string                   `json:"rawId"`
	Type     string                   `json:"type"`
	Response PasskeyAssertionResponse `json:"response"`
}

// PasskeyAssertionResponse is a sign-in's AuthenticatorAssertionResponse.
type PasskeyAssertionResponse struct {
	ClientDataJSON    string `json:"clientDataJSON"`
	AuthenticatorData string `json:"authenticatorData"`
	Signature         string `json:"signature"`
	UserHandle        string `json:"userHandle"`
}
