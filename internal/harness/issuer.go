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
)

// issuerPath is the path of an Issuer's issuer, ending in /, as Authentik
// names its issuers: so a test sees the trailing / kept byte for byte.
const issuerPath = "/application/o/koko/"

// Issuer is an OpenID Connect provider for tests (ADR 0060): its discovery
// document and its key set, served over TLS on httptest.NewTLSServer, which
// a koko server reaches when started with IdentityProvider. It logs every
// request, and a test spoils its discovery one way at a time.
type Issuer struct {
	t   testing.TB
	srv *httptest.Server

	mu sync.Mutex
	// spoil changes the discovery document before it is served; nil
	// serves it as it is.
	spoil func(doc map[string]any)
	// status is what discovery answers in place of the document, 0 for
	// the document.
	status int
	// held makes each discovery request wait until it is closed; nil
	// answers at once. waiting counts the requests held now.
	held    chan struct{}
	waiting int
	// requests is every request's path, in order.
	requests []string
	// key signs the issuer's ID tokens, and kid names it in the key set.
	key *ecdsa.PrivateKey
	kid int
}

// NewIssuer starts an Issuer, stopped when the test ends.
func NewIssuer(t testing.TB) *Issuer {
	t.Helper()
	i := &Issuer{t: t}
	i.RotateKey()
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+issuerPath+".well-known/openid-configuration", i.discovery)
	mux.HandleFunc("GET "+issuerPath+"jwks", i.jwks)
	i.srv = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		i.mu.Lock()
		i.requests = append(i.requests, r.URL.Path)
		i.mu.Unlock()
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

// Hold makes each discovery request wait, unanswered, until Release, as a
// provider too slow to answer does.
func (i *Issuer) Hold() {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.held == nil {
		i.held = make(chan struct{})
	}
}

// Release answers the discovery requests Hold holds, and the next ones at
// once.
func (i *Issuer) Release() {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.held != nil {
		close(i.held)
		i.held = nil
	}
}

// Waiting is how many discovery requests Hold holds now.
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
	held, spoil, status := i.held, i.spoil, i.status
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
	if status != 0 {
		http.Error(w, http.StatusText(status), status)
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
