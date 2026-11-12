package web

import (
	"encoding/json/v2"
	"errors"
	"io"
	"math"
	"mime"
	"net/http"
	"strconv"
	"strings"

	"github.com/cieons/koko/internal/server/signin"
	"github.com/cieons/koko/internal/shared/api"
)

// signInCookie is the Sign-in cookie's name. The __Host- prefix makes a
// browser keep it only when it is Secure, has Path=/ and names no Domain,
// so no other site under the same domain can set or read it.
const signInCookie = "__Host-koko"

// authPrefix starts the paths of the web front's own endpoints on the web
// listener, which sign a browser in, set a Password and sign out apart from
// the API (ADR 0043).
const authPrefix = "/auth/"

func isAuth(r *http.Request) bool {
	return strings.HasPrefix(r.URL.Path, authPrefix)
}

// signInToken is the token the request's Sign-in cookie carries, or "".
func signInToken(r *http.Request) string {
	c, err := r.Cookie(signInCookie)
	if err != nil {
		return ""
	}
	return c.Value
}

// attempt is who a request on the web listener came from, for the signin
// area's limits, log and labels.
func attempt(r *http.Request) signin.Attempt {
	return signin.Attempt{Address: clientAddress(r), UserAgent: r.UserAgent()}
}

// serveAuth answers one of the /auth/ endpoints: a JSON POST of at most
// api.MaxAuthBody bytes, which the Origin check has let through already.
// Every answer is JSON, which no cache keeps. Neither the body nor the
// cookie reaches the log.
func (f *Front) serveAuth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	var serve func(w http.ResponseWriter, r *http.Request, body []byte) error
	switch r.URL.Path {
	case api.AuthSignInPath:
		serve = f.authSignIn
	case api.AuthLinkPath:
		serve = f.authLink
	case api.AuthPasswordPath:
		serve = f.authPassword
	case api.AuthSignOutPath:
		serve = f.authSignOut
	default:
		f.refuseAPI(w, r, api.CodeUnknownOperation.Errorf("no endpoint is served at %s", r.URL.Path))
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		f.refuseAPI(w, r, api.CodeMethodNotAllowed.Errorf("%s is not allowed: %s takes a POST", r.Method, r.URL.Path))
		return
	}
	if mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); mt != "application/json" {
		f.refuseAPI(w, r, api.CodeUnsupportedMediaType.Errorf("the request body must be application/json"))
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, api.MaxAuthBody+1))
	if err != nil {
		f.refuseAPI(w, r, api.CodeInvalidRequest.Errorf("read the request body: %v", err))
		return
	}
	if len(body) > api.MaxAuthBody {
		f.refuseAPI(w, r, api.CodeRequestTooLarge.Errorf("the request body is over the limit of %d KiB of %s", api.MaxAuthBody>>10, r.URL.Path))
		return
	}
	if err := serve(w, r, body); err != nil {
		f.refuseAPI(w, r, err)
	}
}

func (f *Front) authSignIn(w http.ResponseWriter, r *http.Request, body []byte) error {
	req, err := decodeAuth[api.AuthSignInRequest](r, body)
	if err != nil {
		return err
	}
	started, err := f.signins.SignIn(r.Context(), req, attempt(r))
	if err != nil {
		return err
	}
	f.setSignInCookie(w, started)
	writeJSON(w, api.AuthSignInResponse{User: started.User})
	return nil
}

func (f *Front) authLink(w http.ResponseWriter, r *http.Request, body []byte) error {
	req, err := decodeAuth[api.AuthLinkRequest](r, body)
	if err != nil {
		return err
	}
	resp, err := f.signins.Link(r.Context(), req.Link, attempt(r))
	if err != nil {
		return err
	}
	writeJSON(w, resp)
	return nil
}

func (f *Front) authPassword(w http.ResponseWriter, r *http.Request, body []byte) error {
	req, err := decodeAuth[api.AuthPasswordRequest](r, body)
	if err != nil {
		return err
	}
	switch {
	case req.Link != "" && req.Current != "":
		return api.CodeInvalidRequest.Errorf("%s takes link or current, not both", r.URL.Path)
	case req.Link != "":
		started, err := f.signins.SetByLink(r.Context(), req.Link, req.Password, attempt(r))
		if err != nil {
			return err
		}
		f.setSignInCookie(w, started)
		writeJSON(w, api.AuthSignInResponse{User: started.User})
		return nil
	}
	u, err := f.signins.Change(r.Context(), signInToken(r), req, attempt(r))
	if err != nil {
		return err
	}
	writeJSON(w, api.AuthSignInResponse{User: u})
	return nil
}

func (f *Front) authSignOut(w http.ResponseWriter, r *http.Request, body []byte) error {
	if _, err := decodeAuth[api.AuthSignOutRequest](r, body); err != nil {
		return err
	}
	if err := f.signins.SignOut(r.Context(), signInToken(r), attempt(r)); err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{Name: signInCookie, Path: "/", MaxAge: -1, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode})
	writeJSON(w, api.AuthSignOutResponse{})
	return nil
}

// setSignInCookie gives the browser the cookie of the Sign-in started, for
// as long as it can last.
func (f *Front) setSignInCookie(w http.ResponseWriter, started signin.Started) {
	http.SetCookie(w, &http.Cookie{
		Name:     signInCookie,
		Value:    started.Token,
		Path:     "/",
		MaxAge:   int(started.Expires.Sub(f.clock()).Seconds()),
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

// decodeAuth reads an /auth/ endpoint's request body into its type,
// refusing a member the type does not know.
func decodeAuth[Req any](r *http.Request, body []byte) (Req, error) {
	var req Req
	err := json.Unmarshal(body, &req, json.RejectUnknownMembers(true))
	if se, ok := errors.AsType[*json.SemanticError](err); ok && errors.Is(se.Err, json.ErrUnknownName) {
		return req, api.CodeUnknownField.Errorf("%s has no request field %q", r.URL.Path, strings.TrimPrefix(string(se.JSONPointer), "/"))
	}
	if err != nil {
		return req, api.CodeInvalidRequest.Errorf("the request body is not a JSON object of %s's request", r.URL.Path)
	}
	return req, nil
}

// refuseAPI answers err as the API's error object: a refusal by the rate
// limits with Retry-After, in whole seconds; any error without a registered
// code as internal_error, whose cause the log holds.
func (f *Front) refuseAPI(w http.ResponseWriter, r *http.Request, err error) {
	if l, ok := errors.AsType[*signin.Limited](err); ok {
		w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(l.Wait.Seconds()))))
	}
	e, coded := errors.AsType[*api.Error](err)
	code, registered := api.Code{}, false
	if coded {
		code, registered = api.LookupCode(e.Code)
	}
	if !registered {
		f.log.Error("web request failed", "path", loggedPath(r), "err", err)
		code, e = api.CodeInternal, api.CodeInternal.Errorf("the koko server failed; its log says why")
	}
	writeAPIError(w, code.Status, e)
}

// writeJSON answers 200 with v as JSON.
func writeJSON(w http.ResponseWriter, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		panic(err) // the /auth/ answers hold strings alone
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}
