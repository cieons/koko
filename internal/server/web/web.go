// Package web is the web front: what answers a web@koko channel before the
// API does (P0 spec: overview, ADR 0022). It answers only a Host header that
// names koko web's own port, trades koko web's one-time token for a cookie,
// checks the cookie on every request, and then hands /v1/ to the API's
// handler, which takes only a JSON POST, and serves the embedded web UI, or
// its placeholder page, on every other path. It sends no CORS header, so
// another site's page cannot call the API. It also serves web.start, which
// koko web calls for its token.
//
// Over the Unix socket, where the Local caller has no Device Session, a run
// of koko web is its SSH connection: the token is traded and the cookie
// checked only on that connection's web@koko channels, and both end with it.
package web

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cieons/koko/internal/server/deployment"
	"github.com/cieons/koko/internal/server/op"
	"github.com/cieons/koko/internal/server/seal"
	"github.com/cieons/koko/internal/server/store"
	"github.com/cieons/koko/internal/server/transport"
	"github.com/cieons/koko/internal/server/webui"
	"github.com/cieons/koko/internal/shared/api"
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
	api     http.Handler
	files   fs.FS
	store   *store.Store
	rootKey *seal.RootKey
	log     *slog.Logger
	// clock is the koko server's clock, which a token's expiry is read on.
	clock func() time.Time
	runs  *runs

	// keyMu guards key, the cookie key once loaded or made.
	keyMu sync.Mutex
	key   []byte
}

// New returns the web front, which hands authenticated /v1/ requests to api,
// signs cookies with the cookie key kept in st under rootKey, and reads a
// token's expiry on clock, the koko server's.
func New(api http.Handler, st *store.Store, rootKey *seal.RootKey, clock func() time.Time, log *slog.Logger) *Front {
	return &Front{api: api, files: webui.FS(), store: st, rootKey: rootKey, log: log, clock: clock, runs: newRuns()}
}

// Register registers the web area's operation.
func (f *Front) Register(a *op.API) {
	a.Handle(api.WebStart, f.start, op.LocalOnly)
}

// start is web.start: it starts a run of koko web on the caller's
// connection, if none runs there yet, and issues a token for it.
func (f *Front) start(ctx context.Context, _ store.Tx, _ op.Caller, _ api.WebStartRequest) (api.WebStartResponse, error) {
	conn, ok := transport.ConnectionFrom(ctx)
	if !ok {
		return api.WebStartResponse{}, fmt.Errorf("start koko web: the call came without its connection")
	}
	return api.WebStartResponse{Token: f.runs.issue(conn, op.Now(ctx))}, nil
}

// ServeHTTP answers one request on a web@koko channel.
func (f *Front) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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
	name := cookieName(port)
	cookieValid, err := f.checkCookie(ctx, r, name, conn)
	if err != nil {
		f.fail(w, r, err)
		return
	}
	if token := r.URL.Query().Get(tokenParam); token != "" && r.Method == http.MethodGet && !isAPI(r) {
		if cookieValid {
			// This browser holds the cookie already, as when the address
			// is opened again: drop the token from the address bar.
			redirectWithoutToken(w, r)
			return
		}
		f.trade(w, r, conn, name, token)
		return
	}
	if !cookieValid {
		refuse(w, r, api.CodeCookieRequired, "this browser holds no cookie of this run of koko web: open the address koko web printed, or run koko web again for a new one")
		return
	}
	if isAPI(r) {
		f.api.ServeHTTP(w, r)
		return
	}
	f.serveFile(w, r)
}

// trade spends the token and gives the browser this run's cookie, then
// redirects to the same page without the token.
func (f *Front) trade(w http.ResponseWriter, r *http.Request, conn *transport.Connection, name, token string) {
	key, err := f.cookieKey(r.Context())
	if err != nil {
		f.fail(w, r, err)
		return
	}
	// As op.Now reads it for web.start, which issued the token.
	now := f.clock().UTC().Truncate(time.Second)
	id, result := f.runs.spend(conn, token, now)
	switch result {
	case tokenAlreadySpent:
		f.log.Info("web token refused", "reason", "spent")
		refusePage(w, http.StatusForbidden, "this address of koko web was opened already, in another browser, and works only once. If that was not you, another user of this machine opened it first: stop koko web, and run it again for a new address")
		return
	case tokenUnknown:
		f.log.Info("web token refused", "reason", "expired or unknown")
		refusePage(w, http.StatusUnauthorized, "this address of koko web has expired, or comes from another run of koko web: run koko web again and open the address it prints")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    signCookie(key, id),
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
	f.log.Info("web cookie issued")
	redirectWithoutToken(w, r)
}

// checkCookie tells whether the request carries a valid cookie of the run on
// conn.
func (f *Front) checkCookie(ctx context.Context, r *http.Request, name string, conn *transport.Connection) (bool, error) {
	c, err := r.Cookie(name)
	if err != nil {
		return false, nil
	}
	id, ok := f.runs.id(conn)
	if !ok {
		return false, nil
	}
	key, err := f.cookieKey(ctx)
	if err != nil {
		return false, err
	}
	return verifyCookie(key, c.Value, id), nil
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
// that names none, since the web UI routes its pages itself.
func (f *Front) serveFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		refusePage(w, http.StatusMethodNotAllowed, "the web UI's pages are read with GET; its API takes a POST under "+api.PathPrefix)
		return
	}
	name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	page, err := fs.ReadFile(f.files, name)
	if name == "" || err != nil {
		name = "index.html"
		page, err = fs.ReadFile(f.files, name)
	}
	if err != nil {
		f.fail(w, r, fmt.Errorf("read the web UI's %s: %w", name, err))
		return
	}
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(page))
}

// fail answers a failure the web front did not expect; the log holds the
// cause.
func (f *Front) fail(w http.ResponseWriter, r *http.Request, err error) {
	f.log.Error("web request failed", "path", r.URL.Path, "err", err)
	refuse(w, r, api.CodeInternal, "the koko server failed; its log says why")
}

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
	body, _ := json.Marshal(api.ErrorResponse{Error: *code.Errorf("%s", message)})
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code.Status)
	_, _ = w.Write(body)
}

// refusePage answers a page's request with status and a message a person
// reads in the browser.
func refusePage(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_, _ = fmt.Fprintf(w, "koko web: %s.\n", message)
}
