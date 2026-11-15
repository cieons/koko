package harness

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"encoding/json/v2"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"
)

// issuerPath is the path of an Issuer's issuer, ending in /, as Authentik
// names its issuers: so a test sees the trailing / kept byte for byte.
const issuerPath = "/application/o/koko/"

// Issuer is an OpenID Connect provider for tests (ADR 0060), served over
// TLS on httptest.NewTLSServer, which a koko server reaches when started
// with IdentityProvider: its discovery document; its key set, whose ES256
// key rotates; an authorization endpoint, which a test's Browser reaches
// through Authorize, answering with a code for the subject the test chose,
// or access_denied; and a token endpoint, which checks the client's
// authentication, the redirect URI, the PKCE verifier and that each code is
// used once, and answers an ID token. It logs every request, and a test
// spoils its discovery, its token endpoint or one claim at a time.
type Issuer struct {
	t   testing.TB
	srv *httptest.Server

	mu sync.Mutex
	// spoil changes the discovery document before it is served; nil
	// serves it as it is.
	spoil func(doc map[string]any)
	// status is what discovery answers in place of the document, 0 for
	// the document; body, when set, is the bytes it answers in place of
	// the document.
	status int
	body   []byte
	// redirects is how many times discovery redirects to itself before it
	// answers, and redirectTo, when set, where it redirects at once.
	redirects  int
	redirectTo string
	// held makes each discovery request wait until it is closed; nil
	// answers at once. waiting counts the requests held now.
	held    chan struct{}
	waiting int
	// keysOnly makes Hold hold only the key set's requests.
	keysOnly bool
	// requests is every request's path, in order.
	requests []string
	// key signs the issuer's ID tokens, and kid names it in the key set.
	key *ecdsa.PrivateKey
	kid int
	// clock stamps each ID token's times: the koko server's, once a
	// harness starts with the Issuer.
	clock func() time.Time
	// subject and label are the account the next authorization signs in
	// as, and deny makes it answer access_denied instead.
	subject, label string
	deny           bool
	// codes are the codes issued and not yet used.
	codes map[string]grant
	// spoilToken changes each ID token's header and claims before it is
	// signed; foreign signs it with a key the key set does not serve.
	spoilToken func(header, claims map[string]any)
	foreign    bool
	// tokenStatus is what the token endpoint answers in place of a token, 0
	// for one; tokenTo, when set, is where it redirects instead, keeping
	// the request's method and body.
	tokenStatus int
	tokenTo     string
	// secrets is every secret of a flow the Issuer saw or made: the state,
	// nonce and code challenge it was given, the codes it issued, the
	// verifiers it was given, and the tokens it issued.
	secrets []string
}

// grant is what a code was issued for.
type grant struct {
	redirectURI, challenge, nonce, subject, label string
}

// The client a koko server is registered as at an Issuer: SetIssuer sets
// the Identity Provider to it.
const (
	IssuerClientID     = "koko"
	IssuerClientSecret = "issuer-client-secret-Hq3v8Tn2Wx6"
	// IssuerName is the name on the provider's button.
	IssuerName = "Authentik"
)

// NewIssuer starts an Issuer, stopped when the test ends.
func NewIssuer(t testing.TB) *Issuer {
	t.Helper()
	i := &Issuer{t: t, clock: time.Now, subject: "subject-alice", label: "alice@idp.example.com", codes: map[string]grant{}}
	i.RotateKey()
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+issuerPath+".well-known/openid-configuration", i.discovery)
	mux.HandleFunc("GET "+issuerPath+"jwks", i.jwks)
	mux.HandleFunc("GET "+issuerPath+"authorize", i.authorize)
	mux.HandleFunc("POST "+issuerPath+"token", i.token)
	i.srv = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		i.mu.Lock()
		i.requests = append(i.requests, r.URL.Path)
		held := i.held
		if r.URL.Path == issuerPath+"authorize" || i.keysOnly && r.URL.Path != issuerPath+"jwks" {
			held = nil
		}
		if held != nil {
			i.waiting++
		}
		i.mu.Unlock()
		if held != nil {
			select {
			case <-held:
			case <-r.Context().Done():
			}
			i.mu.Lock()
			i.waiting--
			i.mu.Unlock()
		}
		mux.ServeHTTP(w, r)
	}))
	i.srv.StartTLS()
	t.Cleanup(func() {
		i.Release()
		i.srv.Close()
	})
	return i
}

// URL is the issuer, as the Owner sets it: the server's https:// URL
// followed by a path ending in /.
func (i *Issuer) URL() string {
	return i.srv.URL + issuerPath
}

// Client is a client that trusts the Issuer's certificate, which the koko
// server's calls to the provider go through.
func (i *Issuer) Client() *http.Client {
	return i.srv.Client()
}

// Spoil makes discovery serve its document changed by spoil, such as with
// another issuer or without an endpoint; nil serves it whole again.
func (i *Issuer) Spoil(spoil func(doc map[string]any)) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.spoil = spoil
}

// AnswerDiscovery makes discovery answer status, such as 404 or 503, in
// place of its document; 0 serves the document again.
func (i *Issuer) AnswerDiscovery(status int) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.status = status
}

// ServeDiscovery makes discovery answer body, 200 OK, in place of its
// document, such as one that is not JSON or is too large; nil serves the
// document again.
func (i *Issuer) ServeDiscovery(body []byte) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.body = body
}

// RedirectDiscovery makes discovery redirect n times to itself before it
// answers, and, when to is not empty, redirect to it at once instead, such
// as to an http:// URL.
func (i *Issuer) RedirectDiscovery(n int, to string) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.redirects, i.redirectTo = n, to
}

// Hold makes each request the koko server makes, to discovery, the key set
// or the token endpoint, wait, unanswered, until Release, as a provider too
// slow to answer does.
func (i *Issuer) Hold() {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.held == nil {
		i.held = make(chan struct{})
	}
}

// HoldKeys makes Hold hold only the key set's requests, as an issuer whose
// key set alone does not answer.
func (i *Issuer) HoldKeys() {
	i.mu.Lock()
	i.keysOnly = true
	i.mu.Unlock()
	i.Hold()
}

// Release answers the requests Hold holds, and the next ones at once.
func (i *Issuer) Release() {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.held != nil {
		close(i.held)
		i.held = nil
	}
	i.keysOnly = false
}

// Waiting is how many requests Hold holds now.
func (i *Issuer) Waiting() int {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.waiting
}

// Requests is the path of every request the Issuer received, in order.
func (i *Issuer) Requests() []string {
	i.mu.Lock()
	defer i.mu.Unlock()
	return append([]string(nil), i.requests...)
}

// RotateKey replaces the key the Issuer signs with by a new one, under a new
// key id, which its key set serves in place of the old.
func (i *Issuer) RotateKey() {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		i.t.Fatalf("make the issuer's key: %v", err)
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	i.key, i.kid = key, i.kid+1
}

// Document is the discovery document the Issuer serves unspoiled.
func (i *Issuer) Document() map[string]any {
	base := i.URL()
	return map[string]any{
		"issuer":                                base,
		"authorization_endpoint":                base + "authorize",
		"token_endpoint":                        base + "token",
		"jwks_uri":                              base + "jwks",
		"response_types_supported":              []string{"code", "id_token"},
		"subject_types_supported":               []string{"public"},
		"id_token_signing_alg_values_supported": []string{"ES256"},
		"scopes_supported":                      []string{"openid", "profile", "email"},
		"code_challenge_methods_supported":      []string{"S256"},
	}
}

func (i *Issuer) discovery(w http.ResponseWriter, r *http.Request) {
	i.mu.Lock()
	spoil, status, body, redirects, to := i.spoil, i.status, i.body, i.redirects, i.redirectTo
	i.mu.Unlock()
	hop, _ := strconv.Atoi(r.URL.Query().Get("hop"))
	switch {
	case to != "":
		http.Redirect(w, r, to, http.StatusFound)
		return
	case hop < redirects:
		http.Redirect(w, r, r.URL.Path+"?hop="+strconv.Itoa(hop+1), http.StatusFound)
		return
	case status != 0:
		http.Error(w, http.StatusText(status), status)
		return
	case body != nil:
		_, _ = w.Write(body)
		return
	}
	doc := i.Document()
	if spoil != nil {
		spoil(doc)
	}
	i.writeJSON(w, doc)
}

func (i *Issuer) jwks(w http.ResponseWriter, _ *http.Request) {
	i.mu.Lock()
	key, kid := i.key, i.kid
	i.mu.Unlock()
	pub, err := key.PublicKey.Bytes()
	if err != nil {
		i.t.Errorf("encode the issuer's key: %v", err)
		return
	}
	// An uncompressed P-256 point: 0x04, then x and y, 32 bytes each.
	b64 := base64.RawURLEncoding.EncodeToString
	i.writeJSON(w, map[string]any{"keys": []map[string]any{{
		"kty": "EC", "crv": "P-256", "use": "sig", "alg": "ES256",
		"kid": "key-" + strconv.Itoa(kid), "x": b64(pub[1:33]), "y": b64(pub[33:]),
	}}})
}

func (i *Issuer) writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.MarshalWrite(w, v); err != nil && !errors.Is(err, http.ErrHandlerTimeout) {
		i.t.Logf("issuer: write an answer: %v", err)
	}
}

// IdentityProvider makes the koko server's calls to the Identity Provider
// reach the Issuer, trusting its certificate. Without it, every call fails
// at once, unsent (noProvider), so no test reaches the network.
func IdentityProvider(i *Issuer) Option {
	return func(o *options) { o.issuer = i }
}

// identityClient is the client the koko server calls the provider through.
func (o *options) identityClient() *http.Client {
	if o.issuer != nil {
		return o.issuer.Client()
	}
	return noProvider
}

// noProvider is the client of a koko server a test starts without an
// Issuer: it sends nothing, so a call to any provider fails as one that did
// not answer.
var noProvider = &http.Client{Transport: refuseAll{}}

type refuseAll struct{}

func (refuseAll) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("a koko server a test starts reaches no Identity Provider but an Issuer (harness.IdentityProvider)")
}
