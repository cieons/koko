package identity

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"

	"github.com/cieons/koko/internal/shared/api"
)

// How long a discovery is kept: fetched again once discoveryKept has passed
// since the last fetch, and after a failure no sooner than retryAfter.
const (
	discoveryKept = 24 * time.Hour
	retryAfter    = time.Minute
)

// discoveryPath is where an issuer serves its discovery document, after
// the issuer without its trailing / (OpenID Connect Discovery 1.0, 4).
const discoveryPath = "/.well-known/openid-configuration"

// discovery is what koko reads of an issuer's discovery document (OpenID
// Connect Discovery 1.0, 3).
type discovery struct {
	Issuer        string   `json:"issuer"`
	AuthURL       string   `json:"authorization_endpoint"`
	TokenURL      string   `json:"token_endpoint"`
	JWKSURL       string   `json:"jwks_uri"`
	ResponseTypes []string `json:"response_types_supported"`
	Algorithms    []string `json:"id_token_signing_alg_values_supported"`
}

// discover fetches issuer's discovery document through client and checks
// it: it names issuer byte for byte, gives https:// authorization, token and
// key set endpoints, and lists the code response type. A document that does
// not is refused with invalid_provider, saying which; a provider that does
// not answer, or answers with a server error, with provider_unreachable.
func discover(ctx context.Context, client *http.Client, issuer string) (discovery, error) {
	where := strings.TrimSuffix(issuer, "/") + discoveryPath
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, where, nil)
	if err != nil {
		return discovery{}, invalid(api.ReasonIssuerInvalid, "the issuer's discovery document cannot be asked for at %s: %v", where, err)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		if wrongAnswer(err) {
			return discovery{}, invalid(api.ReasonDiscoveryUnreadable, "the issuer's discovery document at %s cannot be read: %v", where, err)
		}
		return discovery{}, unreachable(issuer, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	switch {
	case err != nil && wrongAnswer(err):
		return discovery{}, invalid(api.ReasonDiscoveryUnreadable, "the issuer's discovery document at %s cannot be read: %v", where, err)
	case err != nil:
		return discovery{}, unreachable(issuer, err)
	case resp.StatusCode >= http.StatusInternalServerError:
		return discovery{}, unreachable(issuer, fmt.Errorf("its discovery document at %s answered %s", where, resp.Status))
	case resp.StatusCode != http.StatusOK:
		return discovery{}, invalid(api.ReasonNoDiscovery, "the issuer has no discovery document at %s, which answered %s: is the issuer right?", where, resp.Status)
	}
	var d discovery
	if err := json.Unmarshal(body, &d); err != nil {
		return discovery{}, invalid(api.ReasonDiscoveryUnreadable, "the issuer's discovery document at %s is not the JSON of one: %v", where, err)
	}
	return d, d.check(issuer, where)
}

// check refuses d, read at where, unless it names issuer byte for byte,
// gives https:// endpoints and lists the code response type.
func (d discovery) check(issuer, where string) error {
	if d.Issuer != issuer {
		return invalid(api.ReasonIssuerMismatch, "the discovery document at %s names the issuer %q, not %q: the two must match byte for byte, a trailing / included",
			where, d.Issuer, issuer)
	}
	for _, e := range []struct{ name, url string }{
		{"authorization_endpoint", d.AuthURL}, {"token_endpoint", d.TokenURL}, {"jwks_uri", d.JWKSURL},
	} {
		if u, err := url.Parse(e.url); err != nil || u.Scheme != "https" || u.Host == "" {
			return invalid(api.ReasonEndpointMissing, "the discovery document at %s gives no https:// %s", where, e.name)
		}
	}
	if !slices.Contains(d.ResponseTypes, "code") {
		return invalid(api.ReasonCodeUnsupported, "the discovery document at %s does not list code in response_types_supported, which koko's sign-in needs", where)
	}
	return nil
}

// wrongAnswer tells a failed call whose answer koko does not take, which
// says the provider is set wrongly, from one with no answer.
func wrongAnswer(err error) bool {
	return errors.Is(err, errNotHTTPS) || errors.Is(err, errTooManyRedirects) || errors.Is(err, errTooLarge)
}

// unreachable is the refusal of a call to the provider at issuer that had
// no answer.
func unreachable(issuer string, err error) error {
	return api.CodeProviderUnreachable.Errorf("the Identity Provider at %s did not answer: %v", issuer, err)
}

// discoveries keeps the discovery of the Identity Provider's issuer, and
// the provider go-oidc makes of it, which keeps the issuer's keys: from a
// set onwards, fetched again at the first flow after a restart, once
// discoveryKept has passed since the last fetch, and at the next flow after
// a failure, no sooner than retryAfter.
type discoveries struct {
	client *http.Client
	clock  func() time.Time

	mu        sync.Mutex
	issuer    string
	found     *oidc.Provider
	fetchedAt time.Time
	// failedAt is when a fetch for issuer last failed, zero once one
	// succeeds, and failure why.
	failedAt time.Time
	failure  error
}

// keep keeps d, found for issuer by a set, as fetched now.
func (k *discoveries) keep(issuer string, d discovery) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.issuer, k.found, k.fetchedAt, k.failedAt, k.failure = issuer, k.provider(d), k.clock(), time.Time{}, nil
}

// forget forgets what is kept, as removing the provider does.
func (k *discoveries) forget() {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.issuer, k.found, k.failedAt, k.failure = "", nil, time.Time{}, nil
}

// get returns go-oidc's provider of issuer's discovery: the one kept, or
// one it fetches as the rules of discoveries say, refusing as discover
// does. Within retryAfter of a failure it refuses again without a call.
func (k *discoveries) get(ctx context.Context, issuer string) (*oidc.Provider, error) {
	k.mu.Lock()
	now := k.clock()
	switch {
	case k.issuer != issuer:
	case k.found != nil && now.Sub(k.fetchedAt) < discoveryKept:
		defer k.mu.Unlock()
		return k.found, nil
	case !k.failedAt.IsZero() && now.Sub(k.failedAt) < retryAfter:
		defer k.mu.Unlock()
		return nil, k.failure
	}
	k.mu.Unlock()
	d, err := discover(ctx, k.client, issuer)
	k.mu.Lock()
	defer k.mu.Unlock()
	if err != nil {
		k.issuer, k.found, k.failedAt, k.failure = issuer, nil, k.clock(), err
		return nil, err
	}
	k.issuer, k.found, k.fetchedAt, k.failedAt, k.failure = issuer, k.provider(d), k.clock(), time.Time{}, nil
	return k.found, nil
}

// provider is go-oidc's provider of d, whose key set fetches the issuer's
// keys through k's client as tokens name them.
func (k *discoveries) provider(d discovery) *oidc.Provider {
	c := oidc.ProviderConfig{IssuerURL: d.Issuer, AuthURL: d.AuthURL, TokenURL: d.TokenURL, JWKSURL: d.JWKSURL, Algorithms: d.Algorithms}
	return c.NewProvider(oidc.ClientContext(context.Background(), k.client))
}
