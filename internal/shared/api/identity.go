package api

import "net/http"

// The rules an Identity Provider's settings follow (ADR 0060).
const (
	// MaxIssuerLength is the longest issuer URL, in bytes.
	MaxIssuerLength = 512
	// MaxClientIDLength is the longest client ID, in bytes.
	MaxClientIDLength = 255
	// MaxClientSecretLength is the longest client secret, in bytes.
	MaxClientSecretLength = 1024
	// MaxProviderNameLength is the longest name on the sign-in button, in
	// Unicode code points.
	MaxProviderNameLength = 40
)

// RedirectPath is where the Identity Provider sends a browser back to, after
// the Web URL: the redirect URI the Owner registers at the provider.
const RedirectPath = "/auth/oidc/callback"

// IdentityProvider is the one OpenID Connect provider the Owner configured,
// as server.oidc.show and server.oidc.set answer it. Its client secret is
// never answered.
type IdentityProvider struct {
	// Issuer is the provider's issuer URL, which its discovery document and
	// each ID token name byte for byte.
	Issuer string `json:"issuer"`
	// ClientID is the client koko is registered as at the provider.
	ClientID string `json:"client_id"`
	// Name is the provider's name on the sign-in button, such as
	// "Authentik".
	Name string `json:"name"`
	// ClientSecretSet is true: a provider is never configured without its
	// client secret, which is sealed and never answered.
	ClientSecretSet bool `json:"client_secret_set"`
	// LinkedIdentities is how many Users have linked their identity at
	// the provider: those a change of issuer, or a removal, unlinks.
	LinkedIdentities int `json:"linked_identities"`
	// SetAt is when the provider was last set, RFC 3339 in UTC.
	SetAt string `json:"set_at"`
}

// ServerOIDCShowRequest asks for the Identity Provider.
type ServerOIDCShowRequest struct{}

// ServerOIDCShowResponse is the Identity Provider, and the redirect URI to
// register at it.
type ServerOIDCShowResponse struct {
	// Provider is the Identity Provider; null while none is configured.
	Provider *IdentityProvider `json:"provider"`
	// RedirectURI is the Web URL followed by RedirectPath, which the Owner
	// registers at the provider; null while the Web URL is not set.
	RedirectURI *string `json:"redirect_uri"`
}

// ServerOIDCSetRequest configures the Identity Provider: the settings it
// names, and only those. The first set names all four.
type ServerOIDCSetRequest struct {
	// Issuer is the provider's issuer URL: https:// only, a path allowed, no
	// query or fragment, at most MaxIssuerLength bytes.
	Issuer *string `json:"issuer,omitzero"`
	// ClientID is the client koko is registered as, 1 to MaxClientIDLength
	// bytes.
	ClientID *string `json:"client_id,omitzero"`
	// ClientSecret is the client's secret, 1 to MaxClientSecretLength bytes,
	// which the koko server seals and never answers.
	ClientSecret *string `json:"client_secret,omitzero"`
	// Name is the provider's name on the sign-in button, 1 to
	// MaxProviderNameLength characters.
	Name *string `json:"name,omitzero"`
}

// ServerOIDCSetResponse is the Identity Provider as server.oidc.set left
// it.
type ServerOIDCSetResponse struct {
	Provider IdentityProvider `json:"provider"`
	// RedirectURI is the Web URL followed by RedirectPath, which the Owner
	// registers at the provider.
	RedirectURI string `json:"redirect_uri"`
	// IdentitiesUnlinked is how many Linked Identities the set unlinked: every
	// one, when it changed the issuer, and none otherwise.
	IdentitiesUnlinked int `json:"identities_unlinked"`
}

// ServerOIDCRemoveRequest removes the Identity Provider.
type ServerOIDCRemoveRequest struct{}

// ServerOIDCRemoveResponse is what removing the Identity Provider did.
type ServerOIDCRemoveResponse struct {
	// IdentitiesUnlinked is how many Linked Identities it unlinked: every
	// one.
	IdentitiesUnlinked int `json:"identities_unlinked"`
}

// The Identity Provider's operations (ADR 0060).
var (
	// ServerOIDCShow returns the Identity Provider, without its client
	// secret, and the redirect URI to register at it. It answers the Owner
	// and the Administrators, the Local caller included.
	ServerOIDCShow = Op[ServerOIDCShowRequest, ServerOIDCShowResponse]{Name: "server.oidc.show"}
	// ServerOIDCSet configures the Identity Provider. It is for the Owner
	// alone, the Local caller included, and refuses anyone else with
	// CodeOwnerOnly before it reads anything. It is refused with
	// CodeWebURLUnset while the Web URL is not set, with CodeInvalidRequest
	// for a first set that does not name all four settings, and with
	// CodeInvalidProvider for a setting koko does not take. A new or
	// changed issuer's discovery is checked first, outside any transaction:
	// one that is not valid is refused with CodeInvalidProvider, and a
	// provider that does not answer with CodeProviderUnreachable, changing
	// nothing. A change of issuer unlinks every Linked Identity and ends the
	// Sign-ins made through them; a change of the client or the name keeps
	// them.
	ServerOIDCSet = Op[ServerOIDCSetRequest, ServerOIDCSetResponse]{Name: "server.oidc.set", Write: true}
	// ServerOIDCRemove removes the Identity Provider, unlinking every Linked
	// Identity and ending the Sign-ins made through them. It is for the
	// Owner alone, as ServerOIDCSet is, and is refused with
	// CodeProviderUnset while none is configured.
	ServerOIDCRemove = Op[ServerOIDCRemoveRequest, ServerOIDCRemoveResponse]{Name: "server.oidc.remove", Write: true}
)

var identityOperations = []Operation{
	ServerOIDCShow,
	ServerOIDCSet,
	ServerOIDCRemove,
}

// The identity area's codes.
var (
	// CodeInvalidProvider: a setting of the Identity Provider is not one
	// koko takes, or the issuer's discovery document is not valid; the
	// message says which.
	CodeInvalidProvider = Code{"invalid_provider", http.StatusBadRequest}
	// CodeProviderUnreachable: the Identity Provider did not answer in
	// time.
	CodeProviderUnreachable = Code{"provider_unreachable", http.StatusBadGateway}
	// CodeProviderUnset: no Identity Provider is configured.
	CodeProviderUnset = Code{"provider_unset", http.StatusConflict}
)

// The reasons of an invalid_provider refusal, as Error.Reason names them.
const (
	// ReasonIssuerInvalid, ReasonClientIDInvalid,
	// ReasonClientSecretInvalid and ReasonNameInvalid: a setting breaks its
	// rule.
	ReasonIssuerInvalid       = "issuer_invalid"
	ReasonClientIDInvalid     = "client_id_invalid"
	ReasonClientSecretInvalid = "client_secret_invalid"
	ReasonNameInvalid         = "name_invalid"
	// ReasonNoDiscovery: nothing at the issuer's discovery URL answered
	// with a document (a 4xx).
	ReasonNoDiscovery = "no_discovery"
	// ReasonDiscoveryUnreadable: the answer is not a document koko reads:
	// not JSON, over 1 MiB, or redirected more than 3 times or to http://.
	ReasonDiscoveryUnreadable = "discovery_unreadable"
	// ReasonIssuerMismatch: the document names another issuer, byte for
	// byte.
	ReasonIssuerMismatch = "issuer_mismatch"
	// ReasonEndpointMissing: it gives no https:// authorization, token or
	// key set endpoint.
	ReasonEndpointMissing = "endpoint_missing"
	// ReasonCodeUnsupported: it does not list the code response type.
	ReasonCodeUnsupported = "code_unsupported"
)

// The codes of signing in and linking through the Identity Provider.
var (
	// CodeProviderRefused: the Identity Provider answered the sign-in with
	// an error, such as the person cancelling it there.
	CodeProviderRefused = Code{"provider_refused", http.StatusForbidden}
	// CodeProviderFailed: the code exchange, or the ID token, failed a
	// check.
	CodeProviderFailed = Code{"provider_failed", http.StatusBadGateway}
	// CodeIdentityNotLinked: no User has this identity linked; the same
	// whether or not anyone ever had.
	CodeIdentityNotLinked = Code{"identity_not_linked", http.StatusForbidden}
	// CodeIdentityLinked: the User has a Linked Identity already.
	CodeIdentityLinked = Code{"identity_linked", http.StatusConflict}
	// CodeIdentityTaken: another User has this identity linked.
	CodeIdentityTaken = Code{"identity_taken", http.StatusConflict}
	// CodeSignInExpired: the provider sent back a browser with no flow it
	// began, or after the flow's 10 minutes.
	CodeSignInExpired = Code{"signin_expired", http.StatusBadRequest}
)

var identityCodes = []Code{
	CodeInvalidProvider,
	CodeProviderUnreachable,
	CodeProviderUnset,
	CodeProviderRefused,
	CodeProviderFailed,
	CodeIdentityNotLinked,
	CodeIdentityLinked,
	CodeIdentityTaken,
	CodeSignInExpired,
}

// Signing in and linking through the Identity Provider on the web listener
// (ADR 0060): a page begins a flow, which answers where to send the
// browser, and the provider sends it back to AuthOIDCCallbackPath.
const (
	// AuthOIDCBeginPath begins a flow, an AuthOIDCBeginRequest, setting
	// its binding cookie, and answers an AuthOIDCBeginResponse.
	AuthOIDCBeginPath = "/auth/oidc/begin"
	// AuthOIDCCallbackPath is where the provider sends the browser back
	// to: the one /auth/ endpoint a GET reaches, which answers a 303 to the
	// page the flow ends on.
	AuthOIDCCallbackPath = RedirectPath
	// OIDCPurposeSignIn signs a browser in; OIDCPurposeLink links the
	// identity to the User of the request's Sign-in, or of a Sign-in Link.
	OIDCPurposeSignIn = "signin"
	OIDCPurposeLink   = "link"
	// RefusedParam is the query parameter of the page a flow ends on
	// refused, which names the refusal's code; LinkedParam, set to 1, says
	// Account was reached by a link that succeeded.
	RefusedParam = "refused"
	LinkedParam  = "linked"
	// OIDCFlowLifetime is how long a flow lasts, in seconds: the binding
	// cookie's Max-Age.
	OIDCFlowLifetime = 600
)

// AuthOIDCBegin begins a flow through the Identity Provider: signing in
// with next, linking to the request's Sign-in, or linking through the
// Sign-in Link whose secret is Link. It sets the flow's binding cookie and
// answers where to send the browser. It is refused with CodeProviderUnset
// while no provider is configured, with CodeProviderUnreachable when the
// provider's discovery does not answer, and for linking with
// CodeSignInRequired without a live Sign-in, or with CodeLinkUsed or
// CodeLinkInvalid for a link no longer valid.
var AuthOIDCBegin = AuthEndpoint[AuthOIDCBeginRequest, AuthOIDCBeginResponse]{Path: AuthOIDCBeginPath}

// IdentityEndpoints returns the identity area's endpoints that take a JSON
// POST: the callback is a GET.
func IdentityEndpoints() []AuthEndpointInfo {
	return []AuthEndpointInfo{AuthOIDCBegin.Info()}
}

// AuthOIDCBeginRequest begins a flow through the Identity Provider.
type AuthOIDCBeginRequest struct {
	// Purpose is OIDCPurposeSignIn or OIDCPurposeLink.
	Purpose string `json:"purpose"`
	// Next is the path of this origin a sign-in ends on, outside /signin
	// and /link; any other is /. Only for signing in.
	Next string `json:"next,omitzero"`
	// Link is the secret of the Sign-in Link to link through; left out, a
	// link is to the request's Sign-in. Only for linking.
	Link string `json:"link,omitzero"`
}

// AuthOIDCBeginResponse is where to send the browser: the provider's
// authorization URL for the flow.
type AuthOIDCBeginResponse struct {
	Location string `json:"location"`
}
