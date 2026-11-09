package api

import "net/http"

// WebStartRequest starts a run of koko web.
type WebStartRequest struct{}

// WebStartResponse is the run's one-time token.
type WebStartResponse struct {
	// Token is the one-time token koko web puts in the address it prints,
	// http://localhost:<port>/?t=<token>. The web front trades it for a
	// cookie once, within 5 minutes of its issue on the koko server's
	// clock, on a web@koko channel of the caller's Device Session, or, for
	// the Local caller, of the connection that started the run.
	Token string `json:"token"`
}

// The web operations.
var (
	// WebStart issues a one-time token for a run of koko web. A Device
	// Session's token, and the cookie it is traded for, work on a web@koko
	// channel of any of its Device Connections, and the cookie lasts until
	// the Device Session is Revoked. Over the Unix socket the run is the
	// caller's connection: the token and the cookie work only on its
	// web@koko channels, and end with it. A Sign-in, which runs no koko
	// web, is refused with CodeDeviceOnly.
	WebStart = Op[WebStartRequest, WebStartResponse]{Name: "web.start"}
)

var webOperations = []Operation{
	WebStart,
}

// The web area's codes.
var (
	// CodeCookieRequired: a request on a web@koko channel came without a
	// valid cookie of this koko web; open the address koko web prints.
	CodeCookieRequired = Code{"cookie_required", http.StatusUnauthorized}
	// CodeSignInRequired: a call on the web listener came without a valid
	// Sign-in; the web UI goes to its sign-in page.
	CodeSignInRequired = Code{"signin_required", http.StatusUnauthorized}
	// CodeBadOrigin: a POST on the web listener came without an Origin
	// header naming the Web URL, so from no page of this koko server's, and
	// was refused before anything of it was read.
	CodeBadOrigin = Code{"bad_origin", http.StatusForbidden}
	// CodeDeviceOnly: the operation answers a device alone, and a Sign-in
	// in a browser is none, as web.start, which only koko web calls.
	CodeDeviceOnly = Code{"device_only", http.StatusForbidden}
)

var webCodes = []Code{
	CodeCookieRequired,
	CodeSignInRequired,
	CodeBadOrigin,
	CodeDeviceOnly,
}
