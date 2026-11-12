package web

import (
	"net"
	"net/http"
	"net/netip"
	"strings"

	"github.com/cieons/koko/internal/server/deployment"
	"github.com/cieons/koko/internal/server/op"
	"github.com/cieons/koko/internal/server/store"
	"github.com/cieons/koko/internal/shared/api"
	"github.com/cieons/koko/internal/shared/weburl"
)

// Listener returns what answers each request on the web listener, the plain
// HTTP listener the Owner's reverse proxy reaches at the Web URL (ADR
// 0042), in this order: the 503 pages before initialisation and without a
// Web URL; the Host check against the Web URL, with a 421 page; the Origin
// check on every POST, before anything of it is read; then the /auth/
// endpoints (auth.go), every /v1/ call as the Sign-in its cookie names, or
// signin_required without a live one, and on every other path the web UI
// as on web@koko. Only the /auth/ endpoints set the Sign-in cookie, and
// only they and the API calls read it.
func (f *Front) Listener() http.Handler {
	return http.HandlerFunc(f.serveListener)
}

func (f *Front) serveListener(w http.ResponseWriter, r *http.Request) {
	securityHeaders(w.Header(), true)
	var webURL string
	var initialised bool
	err := f.store.Read(r.Context(), func(tx store.Tx) (err error) {
		webURL, initialised, err = deployment.WebURL(r.Context(), tx)
		return err
	})
	switch {
	case err != nil:
		f.fail(w, r, err, listenerPage)
		return
	case !initialised:
		listenerPage(w, http.StatusServiceUnavailable, "this koko server is not initialised yet. Once it is, its Owner sets the address of these pages with koko server set web_url=https://… on the koko server's machine")
		return
	case webURL == "":
		listenerPage(w, http.StatusServiceUnavailable, "this koko server has no Web URL yet. Its Owner sets the address of these pages with koko server set web_url=https://… on the koko server's machine")
		return
	case !hostMatches(r.Host, webURL):
		f.log.Debug("web listener request refused", "reason", "host", "host", r.Host, "client", clientAddress(r))
		listenerPage(w, http.StatusMisdirectedRequest, "this koko server answers at "+webURL+" alone. Open "+webURL+"; if that is the address opened, the reverse proxy must pass the Host header on as the browser sent it")
		return
	case r.Method == http.MethodPost && r.Header.Get("Origin") != webURL:
		f.log.Debug("web listener request refused", "reason", "origin", "client", clientAddress(r))
		refuseOnListener(w, r, api.CodeBadOrigin, "this request came from no page of "+webURL+", so it was refused")
		return
	case r.URL.Path == TerminalPath:
		f.serveTerminal(w, r, f.listenerTerminal(r, webURL))
		return
	case isAuth(r):
		f.serveAuth(w, r)
		return
	case isFiles(r):
		f.serveFiles(w, r, f.listenerTerminal(r, webURL))
		return
	case isAPI(r):
		f.serveSignedIn(w, r)
		return
	}
	f.serveFile(w, r, listenerPage)
}

// serveSignedIn hands an API call to the API as the live Sign-in its cookie
// names, which the call reads again in its own transaction, or refuses it
// with signin_required.
func (f *Front) serveSignedIn(w http.ResponseWriter, r *http.Request) {
	id, err := f.signins.Check(r.Context(), signInToken(r), attempt(r))
	if err != nil {
		f.refuseAPI(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	f.api.ServeHTTP(w, r.WithContext(op.WithCaller(r.Context(), op.Caller{SignIn: id})))
}

// hostMatches tells whether a request's Host header names the Web URL's
// host and port, the default port 443 written or not. DNS names compare
// without regard to case.
func hostMatches(host, webURL string) bool {
	authority := weburl.Authority(webURL)
	if strings.EqualFold(host, authority) {
		return true
	}
	_, _, err := net.SplitHostPort(authority)
	return err != nil && strings.EqualFold(host, authority+":443")
}

// clientAddress is the address of the client a request on the web listener
// came from, which the log names: the last address in X-Forwarded-For, the
// one the reverse proxy appended, or else the TCP peer's. The listener is
// for the proxy alone, so every peer is the proxy and no list of trusted
// proxies is kept (ADR 0042). A last entry that is not an IP address is
// taken as absent.
func clientAddress(r *http.Request) string {
	if values := r.Header.Values("X-Forwarded-For"); len(values) > 0 {
		last := values[len(values)-1]
		if _, after, ok := strings.CutLast(last, ","); ok {
			last = after
		}
		if a, err := netip.ParseAddr(strings.TrimSpace(last)); err == nil {
			return a.String()
		}
	}
	if a, err := netip.ParseAddrPort(r.RemoteAddr); err == nil {
		return a.Addr().Unmap().String()
	}
	return r.RemoteAddr
}

// refuseOnListener answers a refusal with code on the web listener: to an
// API call, an /auth/ endpoint or a files endpoint, the error object the
// web UI reads, as on web@koko; to a page, the message as text a person
// reads.
func refuseOnListener(w http.ResponseWriter, r *http.Request, code api.Code, message string) {
	if isAPI(r) || isAuth(r) || isFiles(r) {
		writeAPIError(w, code.Status, code.Errorf("%s", message))
		return
	}
	listenerPage(w, code.Status, message)
}

// listenerPage answers a page's request on the web listener with status and
// a message a person reads in the browser, which no cache keeps.
func listenerPage(w http.ResponseWriter, status int, message string) {
	writePage(w, status, "koko: "+message+".\n")
}
