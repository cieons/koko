// Package web is the web front: what answers a web@koko channel before the
// API does (P0 spec: overview, ADR 0022). It answers only a Host header that
// names koko web's own port, trades koko web's one-time token for a cookie,
// checks the cookie on every request, and then hands /v1/ to the API's
// handler, which takes only a JSON POST, and serves the embedded web UI, or
// its placeholder page, on every other path. It sends no CORS header, so
// another site's page cannot call the API. It also serves web.start, which
// koko web calls for its token.
//
// It also answers the web listener, where the Owner's reverse proxy reaches
// the web UI at the Web URL (Listener).
//
// A token and a cookie are bound to the Device Session koko web runs as: the
// cookie lets in a web@koko channel of any Device Connection of that Device
// Session, so it outlives koko web, a dropped Device Connection and a koko
// server restart, until the Device Session is Revoked. Over the Unix socket,
// where the Local caller has no Device Session, they are bound to a run of
// koko web instead, which is its SSH connection: the token is traded and the
// cookie checked only on that connection's web@koko channels, and both end
// with it.
package web

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cieons/koko/internal/server/access"
	"github.com/cieons/koko/internal/server/deployment"
	"github.com/cieons/koko/internal/server/gateway"
	"github.com/cieons/koko/internal/server/op"
	"github.com/cieons/koko/internal/server/seal"
	"github.com/cieons/koko/internal/server/signin"
	"github.com/cieons/koko/internal/server/store"
	"github.com/cieons/koko/internal/server/transport"
	"github.com/cieons/koko/internal/server/webui"
	"github.com/cieons/koko/internal/shared/api"
	"github.com/cieons/koko/internal/shared/timing"
)

// tokenParam is the query parameter that carries koko web's token.
const tokenParam = "t"

// TokenLifetime is how long a token stays usable when nobody opens koko
// web's address: long enough to copy it into a browser by hand, as with
// --no-open, and short enough that an address left in a terminal's
// scrollback is soon worth nothing.
const TokenLifetime = 5 * time.Minute

// Front is the web front.
type Front struct {
	api http.Handler
	// signins answers the web listener's /auth/ endpoints and its Sign-in
	// cookies.
	signins *signin.Service
	// gateway opens the web terminals' SSH Sessions (terminal.go).
	gateway *gateway.Gateway
	files   fs.FS
	store   *store.Store
	rootKey *seal.RootKey
	log     *slog.Logger
	// clock is the koko server's clock, which a token's expiry is read on.
	clock func() time.Time
	// timings are the P0 timings, resolved: how long a web terminal's first
	// message may take among them.
	timings timing.Timings
	runs    *runs
	// uploads are the web terminals' uploads in progress (upload.go), and
	// downloads their download tokens not yet spent (download.go).
	uploads   *uploads
	downloads *downloads

	// keyMu guards key, the cookie key once loaded or made.
	keyMu sync.Mutex
	key   []byte
}

// New returns the web front, which hands authenticated /v1/ requests to api,
// signs koko web's cookies with the cookie key kept in st under rootKey,
// signs browsers in on the web listener through signins, hands web
// terminals to gw, reads a token's expiry on clock, the koko server's, and
// waits as timings say (the zero Timings is the product's).
func New(api http.Handler, signins *signin.Service, gw *gateway.Gateway, st *store.Store, rootKey *seal.RootKey, clock func() time.Time, timings timing.Timings, log *slog.Logger) *Front {
	timings = timings.Resolve()
	return &Front{
		api: api, signins: signins, gateway: gw, files: webui.FS(), store: st, rootKey: rootKey,
		log: log, clock: clock, timings: timings, runs: newRuns(),
		uploads: newUploads(timings.UploadIdle), downloads: newDownloads(),
	}
}

// Register registers the web area's operation.
func (f *Front) Register(a *op.API) {
	a.Handle(api.WebStart, f.start)
}

// Withdraw ends the cookies and tokens of the Device Sessions c Revoked, at
// once, while their connections may still be open: a consumer of the
// access-change signal. It does not block.
func (f *Front) Withdraw(c access.Change) {
	f.runs.revoke(c.Revoked)
}

// start is web.start: it issues a token for the caller's Device Session, or,
// for the Local caller, for the run of koko web on the caller's connection,
// which it starts if none runs there yet. A Revoked Device Session gets
// none, and a Sign-in, which runs no koko web, is refused.
func (f *Front) start(ctx context.Context, tx store.Tx, caller op.Caller, _ api.WebStartRequest) (api.WebStartResponse, error) {
	who, err := access.Who(ctx, tx, caller)
	if err != nil {
		return api.WebStartResponse{}, err
	}
	if who.SignIn != nil {
		return api.WebStartResponse{}, api.CodeDeviceOnly.Errorf("web.start answers koko web on a device, and a browser's Sign-in runs none")
	}
	conn, ok := transport.ConnectionFrom(ctx)
	if !ok {
		return api.WebStartResponse{}, fmt.Errorf("start koko web: the call came without its connection")
	}
	return api.WebStartResponse{Token: f.runs.issue(conn, op.Now(ctx))}, nil
}

// ServeHTTP answers one request on a web@koko channel.
func (f *Front) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	securityHeaders(w.Header(), false)
	ctx := r.Context()
	port, hasPort := transport.WebPortFrom(ctx)
	conn, hasConn := transport.ConnectionFrom(ctx)
	if !hasPort || !hasConn || !hostAllowed(r.Host, port) {
		// A Host naming another site is DNS rebinding, or not koko web's
		// own traffic: it gets no answer at all.
		f.log.Debug("web request refused", "reason", "host", "host", r.Host)
		noAnswer(w)
		return
	}
	if r.URL.Path == TerminalPath {
		// Its refusals but the Origin's come in its first answer.
		f.serveTerminal(w, r, f.kokoWebTerminal(r, conn, port))
		return
	}
	if isFiles(r) {
		// Its Origin check comes before the cookie, and every refusal is
		// JSON.
		f.serveFiles(w, r, f.kokoWebTerminal(r, conn, port))
		return
	}
	sub, state := f.runs.subject(conn)
	if state == subjectRevoked {
		refuse(w, r, api.CodeRevoked, "this device's Device Session was revoked, and with it the web UI's cookie: koko web has stopped, and this device must join the koko server again")
		return
	}
	name := cookieName(port)
	cookieValid := false
	if state == subjectFound {
		var err error
		if cookieValid, err = f.checkCookie(ctx, r, name, sub); err != nil {
			f.fail(w, r, err, refusePage)
			return
		}
	}
	if token := r.URL.Query().Get(tokenParam); token != "" && r.Method == http.MethodGet && !isAPI(r) {
		if cookieValid {
			// This browser holds the cookie already, as when the address
			// is opened again: drop the token from the address bar.
			redirectWithoutToken(w, r)
			return
		}
		f.trade(w, r, sub, name, token)
		return
	}
	if !cookieValid {
		refuse(w, r, api.CodeCookieRequired, "this browser holds no cookie of this koko web: open the address koko web printed, or run koko web again for a new one")
		return
	}
	if isAPI(r) {
		// An API answer is the caller's record as it is now, and
		// secret.show's holds a Secret value: the browser keeps none in its
		// cache.
		w.Header().Set("Cache-Control", "no-store")
		f.api.ServeHTTP(w, r)
		return
	}
	f.serveFile(w, r, refusePage)
}

// trade spends the token and gives the browser the cookie of sub, the
// subject the request is let in by, then redirects to the same page without
// the token.
func (f *Front) trade(w http.ResponseWriter, r *http.Request, sub subject, name, token string) {
	key, err := f.cookieKey(r.Context())
	if err != nil {
		f.fail(w, r, err, refusePage)
		return
	}
	// As op.Now reads it for web.start, which issued the token.
	now := f.clock().UTC().Truncate(time.Second)
	switch f.runs.spend(sub, token, now) {
	case tokenAlreadySpent:
		f.log.Info("web token refused", "reason", "spent")
		refusePage(w, http.StatusForbidden, "this address of koko web was opened already, in another browser, and works only once. If that was not you, another user of this machine opened it first: stop koko web, and run it again for a new address")
		return
	case tokenUnknown:
		f.log.Info("web token refused", "reason", "expired or unknown")
		refusePage(w, http.StatusUnauthorized, "this address of koko web has expired, or comes from another run of koko web, such as another device's: run koko web again and open the address it prints")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    signCookie(key, sub),
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
	f.log.Info("web cookie issued")
	redirectWithoutToken(w, r)
}

// checkCookie tells whether the request carries a valid cookie of sub, the
// subject it is let in by.
func (f *Front) checkCookie(ctx context.Context, r *http.Request, name string, sub subject) (bool, error) {
	c, err := r.Cookie(name)
	if err != nil {
		return false, nil
	}
	key, err := f.cookieKey(ctx)
	if err != nil {
		return false, err
	}
	return verifyCookie(key, c.Value, sub), nil
}

// cookieKey returns the cookie key, which it loads, or makes on first use,
// once per process.
func (f *Front) cookieKey(ctx context.Context) ([]byte, error) {
	f.keyMu.Lock()
	defer f.keyMu.Unlock()
	if f.key != nil {
		return f.key, nil
	}
	var key []byte
	err := f.store.Write(ctx, func(tx store.Tx) (err error) {
		key, err = deployment.CookieKey(ctx, tx, f.rootKey)
		return err
	})
	if err != nil {
		return nil, err
	}
	f.key = key
	return key, nil
}

// serveFile serves a file of the web UI, and its index.html for any path
// that names none, since the web UI routes its pages itself, on either
// listener; page writes its refusals as that listener's pages read.
func (f *Front) serveFile(w http.ResponseWriter, r *http.Request, page pageWriter) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		page(w, http.StatusMethodNotAllowed, "the web UI's pages are read with GET; its API takes a POST under "+api.PathPrefix)
		return
	}
	name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	file, err := fs.ReadFile(f.files, name)
	if name == "" || err != nil {
		name = "index.html"
		file, err = fs.ReadFile(f.files, name)
	}
	if err != nil {
		f.fail(w, r, fmt.Errorf("read the web UI's %s: %w", name, err), page)
		return
	}
	if strings.HasPrefix(name, hashedFiles) {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	}
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(file))
}

// hashedFiles is the directory of the web UI's build whose files carry a
// hash of their content in their names (Vite's assets/), so a name never
// comes to hold other content, and a browser keeps each for a year without
// asking again. index.html and every other file change under one name, and
// get no caching header.
const hashedFiles = "assets/"

// fail answers a failure the web front did not expect, to a page as page
// writes it; the log holds the cause.
func (f *Front) fail(w http.ResponseWriter, r *http.Request, err error, page pageWriter) {
	f.log.Error("web request failed", "path", loggedPath(r), "err", err)
	const message = "the koko server failed; its log says why"
	if isAPI(r) {
		refuse(w, r, api.CodeInternal, message)
		return
	}
	page(w, api.CodeInternal.Status, message)
}

// pageWriter answers a page's request with status and a message a person
// reads in the browser, as one listener's pages read: refusePage on
// web@koko, listenerPage on the web listener.
type pageWriter func(w http.ResponseWriter, status int, message string)

// hostAllowed tells whether a Host header names koko web's own port on
// localhost or 127.0.0.1, as a browser opening koko web's address sends it:
// without the port when it is HTTP's own, 80.
func hostAllowed(host string, port int) bool {
	name, p, hasPort := strings.Cut(host, ":")
	if name != "localhost" && name != "127.0.0.1" {
		return false
	}
	if !hasPort {
		return port == 80
	}
	return p == strconv.Itoa(port)
}

// cookieName is the cookie's name for koko web's port. Browsers send a
// localhost cookie to every port, so each port's koko web keeps its own.
func cookieName(port int) string {
	return "koko_" + strconv.Itoa(port)
}

func isAPI(r *http.Request) bool {
	return strings.HasPrefix(r.URL.Path, api.PathPrefix)
}

// noAnswer ends the connection without a response.
func noAnswer(w http.ResponseWriter) {
	conn, _, err := http.NewResponseController(w).Hijack()
	if err != nil {
		panic(http.ErrAbortHandler) // ends the connection all the same
	}
	_ = conn.Close()
}

// redirectWithoutToken sends the browser to the same page without the token.
func redirectWithoutToken(w http.ResponseWriter, r *http.Request) {
	u := *r.URL
	q := u.Query()
	q.Del(tokenParam)
	u.RawQuery = q.Encode()
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, u.RequestURI(), http.StatusSeeOther)
}

// refuse answers a refusal with code: to an API call, the error object the
// web UI reads; to a page, the message as text a person reads.
func refuse(w http.ResponseWriter, r *http.Request, code api.Code, message string) {
	if !isAPI(r) {
		refusePage(w, code.Status, message)
		return
	}
	writeAPIError(w, code.Status, code.Errorf("%s", message))
}

// writeAPIError answers with status and e as the API's error object, which
// no cache keeps.
func writeAPIError(w http.ResponseWriter, status int, e *api.Error) {
	body, _ := json.Marshal(api.ErrorResponse{Error: *e}) // strings and ints: never fails
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// refusePage answers a page's request on web@koko with status and a message
// a person reads in the browser.
func refusePage(w http.ResponseWriter, status int, message string) {
	writePage(w, status, "koko web: "+message+".\n")
}

// writePage answers a page's request with status and text, which no cache
// keeps.
func writePage(w http.ResponseWriter, status int, text string) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, text)
}

// contentSecurityPolicy is the policy every answer of the web front
// carries: the web UI loads nothing from another site, runs no inline
// script and applies no inline style sheet but one, and no other site
// frames a page. The one is the <style> element React Aria adds on iOS
// Safari while an overlay is open, @layer { * { overscroll-behavior:
// contain } }, allowed by its hash (ADR 0046); the web UI's static checks
// fail when the pinned React Aria adds another. script-src adds to 'self'
// only the compiling of WebAssembly, which the web terminal's emulator
// needs (ADR 0054): no inline script, eval or other source.
const contentSecurityPolicy = "default-src 'self'; script-src 'self' 'wasm-unsafe-eval'; " +
	"style-src 'self' 'sha256-gYiS/BvZvRcK27JIXTuwhZ3hs2+VJ1X+2gUlE+farlg='; " +
	"frame-ancestors 'none'; base-uri 'none'; form-action 'self'"

// securityHeaders sets the headers every answer of the web front carries,
// on either listener, so that both serve the web UI alike: the Content
// Security Policy, no MIME sniffing, and no Referer. hsts adds
// Strict-Transport-Security, for the web listener alone, whose answers
// reach the browser over the reverse proxy's HTTPS; koko web serves
// http://localhost.
func securityHeaders(h http.Header, hsts bool) {
	h.Set("Content-Security-Policy", contentSecurityPolicy)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "no-referrer")
	if hsts {
		h.Set("Strict-Transport-Security", "max-age=31536000")
	}
}
