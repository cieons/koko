package harness

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json/v2"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/cieons/koko/internal/shared/api"
)

// Secrets is every secret of a flow the Issuer saw or made, which no log
// line, answer or error of the koko server may hold: the states, nonces
// and code challenges of its authorizations, the codes it issued, the
// verifiers it was given, and every token it issued.
func (i *Issuer) Secrets() []string {
	i.mu.Lock()
	defer i.mu.Unlock()
	return slices.DeleteFunc(slices.Clone(i.secrets), func(s string) bool { return s == "" })
}

// SignInAs makes the next authorizations sign in as the account subject at
// the provider, which it calls label, and no longer deny.
func (i *Issuer) SignInAs(subject, label string) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.subject, i.label, i.deny = subject, label, false
}

// Deny makes the next authorizations answer access_denied, as when the
// person cancels at the provider.
func (i *Issuer) Deny() {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.deny = true
}

// SpoilToken makes the token endpoint change each ID token's header and
// claims by spoil before it signs it, such as another iss or alg none, for
// which it signs nothing; nil issues them whole again.
func (i *Issuer) SpoilToken(spoil func(header, claims map[string]any)) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.spoilToken = spoil
}

// SignWithForeignKey makes the token endpoint sign each ID token with a key
// its key set does not serve, under its key's id; false signs with its key
// again.
func (i *Issuer) SignWithForeignKey(foreign bool) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.foreign = foreign
}

// AnswerToken makes the token endpoint answer status, such as 400 or 503,
// in place of a token; 0 answers tokens again.
func (i *Issuer) AnswerToken(status int) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.tokenStatus = status
}

// RedirectToken makes the token endpoint answer a 307 to to, which keeps a
// POST's method and body, in place of a token; "" answers tokens again.
func (i *Issuer) RedirectToken(to string) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.tokenTo = to
}

// SetIssuer sets the Identity Provider of h's koko server, over the Unix
// socket, to i as the client IssuerClientID, named IssuerName. The Web URL
// must be set, as a Browser sets it.
func (h *Harness) SetIssuer(i *Issuer) {
	h.t.Helper()
	_, err := h.Call(api.ServerOIDCSet, api.ServerOIDCSetRequest{
		Issuer: new(i.URL()), ClientID: new(IssuerClientID), ClientSecret: new(IssuerClientSecret), Name: new(IssuerName),
	})
	if err != nil {
		h.t.Fatalf("set the Identity Provider: %v", err)
	}
}

func (i *Issuer) authorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	redirect := q.Get("redirect_uri")
	for name, want := range map[string]string{
		"response_type": "code", "response_mode": "query", "code_challenge_method": "S256", "client_id": IssuerClientID,
	} {
		if got := q.Get(name); got != want {
			i.t.Errorf("the authorization request's %s is %q, want %q", name, got, want)
		}
	}
	scope := strings.Fields(q.Get("scope"))
	for _, want := range []string{"openid", "profile", "email"} {
		if !slices.Contains(scope, want) {
			i.t.Errorf("the authorization request's scope %q lacks %s", q.Get("scope"), want)
		}
	}
	if redirect == "" || q.Get("state") == "" || q.Get("nonce") == "" || q.Get("code_challenge") == "" {
		i.t.Errorf("the authorization request %s lacks redirect_uri, state, nonce or code_challenge", r.URL.RawQuery)
		http.Error(w, "invalid_request", http.StatusBadRequest)
		return
	}
	back := url.Values{"state": {q.Get("state")}}
	i.mu.Lock()
	i.secrets = append(i.secrets, q.Get("state"), q.Get("nonce"), q.Get("code_challenge"))
	if i.deny {
		back.Set("error", "access_denied")
	} else {
		code := random()
		i.codes[code] = grant{redirectURI: redirect, challenge: q.Get("code_challenge"), nonce: q.Get("nonce"), subject: i.subject, label: i.label}
		back.Set("code", code)
		i.secrets = append(i.secrets, code)
	}
	i.mu.Unlock()
	http.Redirect(w, r, redirect+"?"+back.Encode(), http.StatusFound)
}

// random is 128 random bits in base64url.
func random() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return base64.RawURLEncoding.EncodeToString(b[:])
}

func (i *Issuer) token(w http.ResponseWriter, r *http.Request) {
	i.mu.Lock()
	status, to := i.tokenStatus, i.tokenTo
	i.mu.Unlock()
	switch {
	case to != "":
		http.Redirect(w, r, to, http.StatusTemporaryRedirect)
		return
	case status != 0:
		w.WriteHeader(status)
		i.writeJSON(w, map[string]string{"error": "server_error"})
		return
	}
	if err := r.ParseForm(); err != nil {
		tokenError(w, i, http.StatusBadRequest, "invalid_request")
		return
	}
	// The client authenticates before its code is spent, so x/oauth2's
	// second try, in the form, after a refused first, finds it unspent.
	id, secret, basic := r.BasicAuth()
	if basic {
		id, _ = url.QueryUnescape(id)
		secret, _ = url.QueryUnescape(secret)
	} else {
		id, secret = r.PostForm.Get("client_id"), r.PostForm.Get("client_secret")
	}
	if id != IssuerClientID || subtle.ConstantTimeCompare([]byte(secret), []byte(IssuerClientSecret)) != 1 {
		tokenError(w, i, http.StatusUnauthorized, "invalid_client")
		return
	}
	i.mu.Lock()
	i.secrets = append(i.secrets, r.PostForm.Get("code_verifier"))
	g, ok := i.codes[r.PostForm.Get("code")]
	delete(i.codes, r.PostForm.Get("code"))
	i.mu.Unlock()
	challenge := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
	switch {
	case r.PostForm.Get("grant_type") != "authorization_code", !ok,
		r.PostForm.Get("redirect_uri") != g.redirectURI,
		base64.RawURLEncoding.EncodeToString(challenge[:]) != g.challenge:
		tokenError(w, i, http.StatusBadRequest, "invalid_grant")
		return
	}
	access, refresh, idToken := random(), random(), i.idToken(g)
	i.mu.Lock()
	i.secrets = append(i.secrets, access, refresh, idToken)
	i.mu.Unlock()
	i.writeJSON(w, map[string]any{
		"access_token": access, "token_type": "Bearer", "expires_in": 300, "refresh_token": refresh, "id_token": idToken,
	})
}

// tokenError answers the token endpoint's error code with status.
func tokenError(w http.ResponseWriter, i *Issuer, status int, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	i.writeJSON(w, map[string]string{"error": code})
}

// idToken is the ID token of g, spoiled and signed as the Issuer is set
// to: ES256, by its key under its key id.
func (i *Issuer) idToken(g grant) string {
	i.mu.Lock()
	key, kid, spoil, foreign, now := i.key, i.kid, i.spoilToken, i.foreign, i.clock()
	i.mu.Unlock()
	header := map[string]any{"alg": "ES256", "typ": "JWT", "kid": "key-" + strconv.Itoa(kid)}
	claims := map[string]any{
		"iss": i.URL(), "sub": g.subject, "aud": IssuerClientID, "nonce": g.nonce,
		"iat": now.Unix(), "exp": now.Add(5 * time.Minute).Unix(), "preferred_username": g.label,
	}
	if spoil != nil {
		spoil(header, claims)
	}
	if foreign {
		var err error
		if key, err = ecdsa.GenerateKey(elliptic.P256(), rand.Reader); err != nil {
			i.t.Fatalf("make a foreign key: %v", err)
		}
	}
	b64 := base64.RawURLEncoding.EncodeToString
	encode := func(v any) string {
		b, err := json.Marshal(v)
		if err != nil {
			i.t.Fatalf("encode an ID token: %v", err)
		}
		return b64(b)
	}
	input := encode(header) + "." + encode(claims)
	if header["alg"] == "none" {
		return input + "."
	}
	digest := sha256.Sum256([]byte(input))
	r, s, err := ecdsa.Sign(rand.Reader, key, digest[:])
	if err != nil {
		i.t.Fatalf("sign an ID token: %v", err)
	}
	// A JWS ES256 signature is r and s, 32 bytes each, not ASN.1.
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])
	return input + "." + b64(sig)
}

// Authorize follows location, the authorization URL a koko server
// answered, to the Issuer, as a browser does, and returns the URL the
// Issuer sends the browser back to, failing the test unless it sends it
// back.
func (i *Issuer) Authorize(location string) string {
	i.t.Helper()
	c := *i.Client()
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	req, err := http.NewRequestWithContext(i.t.Context(), http.MethodGet, location, nil)
	if err != nil {
		i.t.Fatalf("follow the authorization URL: %v", err)
	}
	resp, err := c.Do(req)
	if err != nil {
		i.t.Fatalf("follow the authorization URL: %v", err)
	}
	_ = resp.Body.Close()
	back := resp.Header.Get("Location")
	if resp.StatusCode != http.StatusFound || back == "" {
		i.t.Fatalf("the Issuer answered the authorization URL %d, want a redirect back", resp.StatusCode)
	}
	return back
}

// BeginOIDC posts req to /auth/oidc/begin from b, and returns the status and
// body of the answer, keeping the flow's cookie it sets.
func (b *Browser) BeginOIDC(req api.AuthOIDCBeginRequest) (int, string) {
	b.t.Helper()
	return b.Post(api.AuthOIDCBeginPath, req)
}

// StartOIDC begins a flow from b for req, and returns where it sends the
// browser, failing the test unless the koko server begins one.
func (b *Browser) StartOIDC(req api.AuthOIDCBeginRequest) string {
	b.t.Helper()
	status, body := b.BeginOIDC(req)
	var resp api.AuthOIDCBeginResponse
	if err := json.Unmarshal([]byte(body), &resp); status != http.StatusOK || err != nil || resp.Location == "" {
		b.t.Fatalf("%s answered %d %s, want a location", api.AuthOIDCBeginPath, status, body)
	}
	return resp.Location
}

// Callback loads back, the URL the provider sent the browser back to, from
// b, as a browser does, and returns the koko server's answer, keeping the
// cookies it sets or clears. It follows no redirect.
func (b *Browser) Callback(back string) Answer {
	b.t.Helper()
	u, err := url.Parse(back)
	if err != nil {
		b.t.Fatal(err)
	}
	return b.Do(http.MethodGet, u.RequestURI(), "")
}

// ThroughProvider begins a flow from b for req, has i authorize it and
// sends b back, and returns the callback's answer: a 303 to the page the
// flow ends on.
func (b *Browser) ThroughProvider(i *Issuer, req api.AuthOIDCBeginRequest) Answer {
	b.t.Helper()
	return b.Callback(i.Authorize(b.StartOIDC(req)))
}
