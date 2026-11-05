package api

import "net/http"

// WebStartRequest starts a run of koko web.
type WebStartRequest struct{}

// WebStartResponse is the run's one-time token.
type WebStartResponse struct {
	// Token is the one-time token koko web puts in the address it prints,
	// http://localhost:<port>/?t=<token>. The web front trades it for a
	// cookie once, on a web@koko channel of the connection that started the
	// run, within 5 minutes of its issue on the koko server's clock.
	Token string `json:"token"`
}

// The web operations.
var (
	// WebStart starts a run of koko web on the caller's connection and
	// issues its one-time token. Over the Unix socket the run is that
	// connection: the token and the cookie it is traded for work only on its
	// web@koko channels, and end with it. It answers only the Local caller
	// until a Device Session can run koko web.
	WebStart = Op[WebStartRequest, WebStartResponse]{Name: "web.start"}
)

var webOperations = []Operation{
	WebStart,
}

// The web area's codes.
var (
	// CodeCookieRequired: a request on a web@koko channel came without a
	// valid cookie of this run of koko web; open the address koko web
	// prints.
	CodeCookieRequired = Code{"cookie_required", http.StatusUnauthorized}
)

var webCodes = []Code{
	CodeCookieRequired,
}
