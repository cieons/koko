package identity

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"path"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/cieons/koko/internal/server/op"
	"github.com/cieons/koko/internal/server/signin"
	"github.com/cieons/koko/internal/server/store"
	"github.com/cieons/koko/internal/server/user"
	"github.com/cieons/koko/internal/shared/api"
)

// idPrefix starts every Linked Identity's id, as in identity_3f9k2m7q.
const idPrefix = "identity"

// flowLifetime is how long a flow through the provider lasts: the binding
// cookie's Max-Age.
const flowLifetime = api.OIDCFlowLifetime * time.Second

// The checks of an ID token's times that koko makes itself (ADR 0060).
const (
	// expirySkew is the clock skew an ID token's exp is allowed: it must
	// be after the koko server's clock less this.
	expirySkew = 60 * time.Second
	// notBeforeLeeway is how far ahead of the koko server's clock an ID
	// token's nbf may be, as go-oidc allows.
	notBeforeLeeway = 5 * time.Minute
)

// maxSubject is the longest subject, in bytes, and maxLabel the longest
// label kept of what the provider calls an account.
const (
	maxSubject = 255
	maxLabel   = 255
)

// scopes are what a flow asks the provider for: openid for the ID token,
// and profile and email for the label alone.
var scopes = []string{oidc.ScopeOpenID, "profile", "email"}

// The pages a flow ends on, by its purpose.
const (
	signInPage  = "/signin"
	accountPage = "/account"
	linkPage    = "/link"
)

// oidcFlow is what a flow holds from its begin to its callback.
type oidcFlow struct {
	// binding is the SHA-256 of the token the binding cookie carries.
	binding [sha256.Size]byte
	// nonce and verifier are the ID token's nonce and the PKCE verifier.
	nonce, verifier string
	// purpose is api.OIDCPurposeSignIn or api.OIDCPurposeLink; next is
	// where a sign-in ends, a path of this origin.
	purpose, next string
	// signIn is the Sign-in a link is to, and link the Sign-in Link it is
	// through, with userID the User of either: one of the two, for
	// linking.
	signIn, link, userID string
	// issuer and clientID are the Identity Provider's when the flow
	// began, and redirectURI the redirect URI it named: a callback after
	// any of them changed is refused, while a new name or client secret
	// keeps the flow.
	issuer, clientID, redirectURI string
}

// sameProvider tells whether p, the Identity Provider now, is the one f
// began with: the same issuer and client.
func (f oidcFlow) sameProvider(p *provider) bool {
	return p != nil && p.issuer == f.issuer && p.clientID == f.clientID
}

// page is where f ends, refused or, for linking, linked.
func (f oidcFlow) page() string {
	switch {
	case f.purpose == api.OIDCPurposeSignIn:
		return signInPage
	case f.link != "":
		return linkPage
	}
	return accountPage
}

// Outcome is how a callback ends: the 303 to Location and, when it signed
// this browser in, the Sign-in whose cookie to set.
type Outcome struct {
	Location string
	Started  *signin.Started
}

// refusedAt is the page at path showing the refusal code.
func refusedAt(path string, code api.Code) Outcome {
	return Outcome{Location: path + "?" + api.RefusedParam + "=" + code.Name}
}

// isBinding tells whether b is a binding token as random makes one: 22
// base64url characters, so that a browser's cookie of any other value is
// replaced rather than kept.
func isBinding(b string) bool {
	raw, err := base64.RawURLEncoding.DecodeString(b)
	return err == nil && len(raw) == 16 && len(b) == 22
}

// random is 128 random bits in base64url.
func random() string {
	var b [16]byte
	_, _ = rand.Read(b[:]) // crypto/rand.Read never fails
	return base64.RawURLEncoding.EncodeToString(b[:])
}

// now is the time of a flow's request on the koko server's clock, as
// op.Now reads it for a call.
func (s *Service) now() time.Time {
	return s.clock().UTC().Truncate(time.Second)
}

// Begin begins a flow from a for req's purpose: signing in, linking to the
// live Sign-in whose cookie carries token, or linking through the Sign-in
// Link whose secret is req.Link. It holds the flow under its state, and
// returns the provider's authorization URL and the binding token for the
// flow's cookie: held, the browser's own, which the request's cookie
// carries, when it is one koko made, so that every flow a browser has
// begun in its lifetime finishes; a new one otherwise. It is refused with provider_unset while no provider is
// configured, and provider_unreachable when its discovery does not
// answer; a refused link counts as a failure for the client's address, and
// an address past its limit is refused with a *signin.Limited.
func (s *Service) Begin(ctx context.Context, req api.AuthOIDCBeginRequest, token, held string, a signin.Attempt) (location, binding string, err error) {
	switch {
	case req.Purpose == api.OIDCPurposeSignIn && req.Link != "":
		return "", "", api.CodeInvalidRequest.Errorf("a sign-in through the Identity Provider takes no link")
	case req.Purpose == api.OIDCPurposeLink && req.Next != "":
		return "", "", api.CodeInvalidRequest.Errorf("linking an identity takes no next: it ends on Account")
	case req.Purpose != api.OIDCPurposeSignIn && req.Purpose != api.OIDCPurposeLink:
		return "", "", api.CodeInvalidRequest.Errorf("purpose is %q, and a flow's purpose is %s or %s", req.Purpose, api.OIDCPurposeSignIn, api.OIDCPurposeLink)
	}
	end, err := s.signins.BeginAttempt(a)
	if err != nil {
		return "", "", err
	}
	var l signin.Link
	defer func() { end(req.Link != "" && s.signins.LinkRefused(l, err, a)) }()
	f := oidcFlow{purpose: req.Purpose, nonce: random(), verifier: oauth2.GenerateVerifier()}
	if f.purpose == api.OIDCPurposeSignIn {
		f.next = safeNext(req.Next)
	}
	var p *provider
	err = s.store.Read(ctx, func(tx store.Tx) (err error) {
		if p, err = readProvider(ctx, tx, s.rootKey); err != nil {
			return err
		}
		if p == nil {
			return api.CodeProviderUnset.Errorf("no Identity Provider is configured, so nobody signs in through one")
		}
		if f.redirectURI, err = redirectURI(ctx, tx); err != nil {
			return err
		}
		f.issuer, f.clientID = p.issuer, p.clientID
		switch {
		case req.Link != "":
			if l, err = s.signins.CheckLink(ctx, tx, req.Link, s.now()); err != nil {
				return err
			}
			f.link, f.userID = l.ID, l.User.ID
		case f.purpose == api.OIDCPurposeLink:
			si, err := s.signins.Live(ctx, tx, token, s.now())
			if err != nil {
				return err
			}
			u, err := user.ByName(ctx, tx, si.User)
			if err != nil {
				return err
			}
			f.signIn, f.userID = si.ID, u.ID
		}
		return nil
	})
	if err != nil {
		return "", "", err
	}
	k, err := s.discoveries.get(ctx, p.issuer)
	if err != nil {
		return "", "", err
	}
	binding = held
	if !isBinding(binding) {
		binding = random()
	}
	f.binding = sha256.Sum256([]byte(binding))
	state := s.flows.Begin(a.Address, f)
	location = p.oauth2(k, f.redirectURI).AuthCodeURL(state, oidc.Nonce(f.nonce), oauth2.S256ChallengeOption(f.verifier),
		oauth2.SetAuthURLParam("response_mode", "query"))
	return location, binding, nil
}

// oauth2 is the client p is at the provider whose discovery k is, which
// the provider sends back to redirectURI.
func (p *provider) oauth2(k *known, redirectURI string) *oauth2.Config {
	return &oauth2.Config{
		ClientID: p.clientID, ClientSecret: p.secret, RedirectURL: redirectURI, Scopes: scopes,
		Endpoint: oauth2.Endpoint{AuthURL: k.doc.AuthURL, TokenURL: k.doc.TokenURL},
	}
}

// safeNext is where a sign-in whose next is next ends: the path a browser
// lands on, its dot segments resolved and its fragment dropped, with its
// query, when it is a path of this origin outside the pages that sign in;
// / otherwise. The 303 carries exactly what was checked.
func safeNext(next string) string {
	bad := func(r rune) bool { return r == '\\' || unicode.IsControl(r) || unicode.IsSpace(r) }
	if !strings.HasPrefix(next, "/") || strings.ContainsFunc(next, bad) {
		return "/"
	}
	// //host/… parses with a host, and is refused here.
	u, err := url.Parse(next)
	if err != nil || u.Scheme != "" || u.Host != "" || u.User != nil || strings.ContainsFunc(u.Path, bad) {
		return "/"
	}
	clean := path.Clean(u.Path)
	for _, page := range []string{signInPage, linkPage, "/password", "/auth"} {
		if clean == page || strings.HasPrefix(clean, page+"/") {
			return "/"
		}
	}
	return (&url.URL{Path: clean, RawQuery: u.RawQuery}).String()
}

// Callback ends the flow the provider sent a browser back for, from a,
// with query the callback's query, binding the token of the flow's cookie
// the request carried, and token the Sign-in cookie's: in the spec's
// order, the binding, the provider's answer, the exchange, the ID token and
// the outcome. A callback that fails the binding is refused as
// signin_expired and counts for no limit, since any site can make a
// browser load it; after the binding, provider_failed, identity_not_linked
// and a refused link count for the client's address. Every outcome is a
// 303: to the flow's next, to Account once linked, or to the flow's page
// with the refusal's code.
func (s *Service) Callback(ctx context.Context, query url.Values, binding, token string, a signin.Attempt) Outcome {
	f, ok := s.flows.Take(query.Get("state"))
	if !ok || binding == "" || subtle.ConstantTimeCompare(f.binding[:], hashOf(binding)) != 1 {
		s.log.Debug("sign-in refused", "reason", api.CodeSignInExpired.Name, "flow_held", ok, "address", a.Address)
		return refusedAt(signInPage, api.CodeSignInExpired)
	}
	end, err := s.signins.BeginAttempt(a)
	if err != nil {
		return s.refuse(f, a, err, "")
	}
	var r refusal
	defer func() { end(r.counts) }()
	out, r := s.finish(ctx, f, query, token, a)
	if r.err != nil {
		return s.refuse(f, a, r.err, r.step, r.attrs...)
	}
	return out
}

// hashOf is the SHA-256 of a binding token.
func hashOf(binding string) []byte {
	h := sha256.Sum256([]byte(binding))
	return h[:]
}

// refusal is why a flow that passed its binding was refused: err, and for
// provider_failed the step that failed; counts says it counts as a failure
// for the client's address.
type refusal struct {
	err    error
	step   string
	counts bool
	attrs  []any
}

// The steps of a provider_failed refusal, as the log names them.
const (
	stepExchange  = "exchange"
	stepSignature = "signature"
	stepIssuer    = "issuer"
	stepAudience  = "audience"
	stepExpiry    = "expiry"
	stepNonce     = "nonce"
	stepSubject   = "subject"
	// stepDiscovery is a discovery that failed in a way a page does not
	// word, such as one no longer valid.
	stepDiscovery = "discovery"
)

// failed is a provider_failed refusal at step, which counts.
func failed(step string) refusal {
	return refusal{err: api.CodeProviderFailed.Errorf("the Identity Provider's answer failed a check: sign in again"), step: step, counts: true}
}

// unreachableRefusal is a provider_unreachable refusal, which counts for
// nothing, since the person did nothing wrong.
func unreachableRefusal(err error) refusal {
	return refusal{err: api.CodeProviderUnreachable.Errorf("the Identity Provider did not answer: %v", err)}
}

// finish ends f once its binding passed: the provider's answer, the
// exchange, the ID token and the outcome.
func (s *Service) finish(ctx context.Context, f oidcFlow, query url.Values, token string, a signin.Attempt) (Outcome, refusal) {
	if e := query.Get("error"); e != "" || query.Has("error") {
		return Outcome{}, refusal{
			err:   api.CodeProviderRefused.Errorf("the Identity Provider refused the sign-in"),
			attrs: []any{"error", providerError(e)},
		}
	}
	code := query.Get("code")
	if code == "" {
		return Outcome{}, failed(stepExchange)
	}
	var p *provider
	err := s.store.Read(ctx, func(tx store.Tx) (err error) {
		p, err = readProvider(ctx, tx, s.rootKey)
		return err
	})
	switch {
	case err != nil:
		return Outcome{}, refusal{err: err}
	case !f.sameProvider(p):
		// The provider was removed, or its issuer or client changed, since
		// the flow began.
		return Outcome{}, refusal{err: expired()}
	}
	k, err := s.discoveries.get(ctx, p.issuer)
	if err != nil {
		return Outcome{}, refusal{err: err}
	}
	tok, err := p.oauth2(k, f.redirectURI).Exchange(context.WithValue(ctx, oauth2.HTTPClient, s.exchange), code, oauth2.VerifierOption(f.verifier))
	if err != nil {
		return Outcome{}, exchangeRefusal(err)
	}
	// The access and refresh tokens are discarded unread.
	raw, _ := tok.Extra("id_token").(string)
	if raw == "" {
		return Outcome{}, failed(stepExchange)
	}
	claims, r := s.verify(ctx, p, k, f, raw)
	if r.err != nil {
		return Outcome{}, r
	}
	return s.outcome(ctx, f, p.issuer, claims, token, a)
}

// expired is the refusal of a callback for a flow this browser did not
// begin, or that no longer holds.
func expired() error {
	return api.CodeSignInExpired.Errorf("this sign-in through the Identity Provider expired, or was begun in another browser: start it again")
}

// providerError is the provider's error code as the log may hold it: one
// of OAuth 2.0's error characters (RFC 6749, A.7), at most 64 of them, or
// "other".
func providerError(e string) string {
	if e == "" || len(e) > 64 || strings.ContainsFunc(e, func(r rune) bool { return r < 0x20 || r > 0x7e || r == '"' || r == '\\' }) {
		return "other"
	}
	return e
}

// exchangeRefusal is the refusal of an exchange that failed with err: one
// the provider answered, a redirect or an error, failed; one it did not
// answer, or answered with a server error, unreachable.
func exchangeRefusal(err error) refusal {
	if re, ok := errors.AsType[*oauth2.RetrieveError](err); ok {
		if re.Response != nil && re.Response.StatusCode >= 500 {
			return unreachableRefusal(fmt.Errorf("its token endpoint answered %s", re.Response.Status))
		}
		return failed(stepExchange)
	}
	if wrongAnswer(err) {
		return failed(stepExchange)
	}
	// Never err's text alone: it may hold what the provider answered.
	if ue, ok := errors.AsType[*url.Error](err); ok {
		return unreachableRefusal(ue.Err)
	}
	return failed(stepExchange)
}

// claims is what koko reads of an ID token once it verified.
type claims struct {
	subject, label string
}

// keysKey is the context key of a keySet's fetch failure.
type keysKey struct{}

// keySet is the issuer's key set, which records in the context of a
// verification a fetch of the keys that had no answer: go-oidc's verifier
// flattens its key set's errors into text, which tells it from a bad
// signature no longer.
type keySet struct{ keys *oidc.RemoteKeySet }

func (k keySet) VerifySignature(ctx context.Context, jwt string) ([]byte, error) {
	payload, err := k.keys.VerifySignature(ctx, jwt)
	if ue, ok := errors.AsType[*url.Error](err); ok && !wrongAnswer(err) {
		if fetch, ok := ctx.Value(keysKey{}).(*error); ok {
			*fetch = ue.Err
		}
	}
	return payload, err
}

// asymmetric are the algorithms an ID token may be signed with: those of
// discovery's list that sign with a private key, as go-oidc's own provider
// keeps them. An HMAC (HS256 and the rest) would be checked with a key the
// key set publishes, which anyone can read.
var asymmetric = []string{
	oidc.RS256, oidc.RS384, oidc.RS512, oidc.ES256, oidc.ES384, oidc.ES512,
	oidc.PS256, oidc.PS384, oidc.PS512, oidc.EdDSA,
}

// verify checks raw, the ID token of f's exchange with p, whose discovery
// and keys k are. go-oidc's verifier checks its signature, by a key of the
// issuer's key set, with an asymmetric algorithm its discovery lists
// (RS256 when it lists none), never an HMAC or none; koko checks each claim itself, so that a
// refusal names its step: iss byte for byte, aud and azp, exp with
// expirySkew and nbf with notBeforeLeeway on the koko server's clock, the
// nonce, and the subject, 1 to maxSubject bytes with no control character.
func (s *Service) verify(ctx context.Context, p *provider, k *known, f oidcFlow, raw string) (claims, refusal) {
	algorithms := slices.DeleteFunc(slices.Clone(k.doc.Algorithms), func(alg string) bool { return !slices.Contains(asymmetric, alg) })
	if len(algorithms) == 0 {
		algorithms = []string{oidc.RS256}
	}
	v := oidc.NewVerifier(p.issuer, keySet{k.keys}, &oidc.Config{
		SupportedSigningAlgs: algorithms,
		SkipClientIDCheck:    true, SkipExpiryCheck: true, SkipIssuerCheck: true,
	})
	var fetch error
	t, err := v.Verify(context.WithValue(ctx, keysKey{}, &fetch), raw)
	switch {
	case fetch != nil:
		return claims{}, unreachableRefusal(fetch)
	case err != nil:
		return claims{}, failed(stepSignature)
	}
	var extra struct {
		AuthorizedParty   string   `json:"azp"`
		NotBefore         *float64 `json:"nbf"`
		PreferredUsername string   `json:"preferred_username"`
		Email             string   `json:"email"`
		Name              string   `json:"name"`
	}
	if err := t.Claims(&extra); err != nil {
		return claims{}, failed(stepSignature)
	}
	now := s.clock()
	switch {
	case t.Issuer != p.issuer:
		return claims{}, failed(stepIssuer)
	case !slices.Contains(t.Audience, p.clientID),
		extra.AuthorizedParty != "" && extra.AuthorizedParty != p.clientID,
		len(t.Audience) > 1 && extra.AuthorizedParty == "":
		return claims{}, failed(stepAudience)
	case !t.Expiry.After(now.Add(-expirySkew)),
		extra.NotBefore != nil && now.Add(notBeforeLeeway).Before(time.Unix(int64(*extra.NotBefore), 0)):
		return claims{}, failed(stepExpiry)
	case subtle.ConstantTimeCompare([]byte(t.Nonce), []byte(f.nonce)) != 1:
		return claims{}, failed(stepNonce)
	case t.Subject == "" || len(t.Subject) > maxSubject || strings.ContainsFunc(t.Subject, unicode.IsControl):
		return claims{}, failed(stepSubject)
	}
	return claims{subject: t.Subject, label: label(t.Subject, extra.PreferredUsername, extra.Email, extra.Name)}, refusal{}
}

// label is what the provider calls an account, as koko shows it: the first
// of the candidates that holds anything once control characters are
// dropped, cut to maxLabel bytes, else the subject.
func label(subject string, candidates ...string) string {
	for _, c := range candidates {
		c = strings.TrimSpace(strings.Map(func(r rune) rune {
			if unicode.IsControl(r) || r == utf8.RuneError {
				return -1
			}
			return r
		}, c))
		for len(c) > maxLabel {
			_, size := utf8.DecodeLastRuneInString(c)
			c = c[:len(c)-size]
		}
		if c != "" {
			return c
		}
	}
	return subject
}

// outcome is f's outcome for the identity (issuer, the claims' subject),
// in one write transaction: a Sign-in through it, or the identity linked
// to the flow's User.
func (s *Service) outcome(ctx context.Context, f oidcFlow, issuer string, c claims, token string, a signin.Attempt) (Outcome, refusal) {
	var out Outcome
	var r refusal
	var l signin.Link
	err := s.api.Do(ctx, func(ctx context.Context, tx store.Tx) error {
		// The provider or the Web URL may have changed while the provider
		// was called.
		p, err := readProvider(ctx, tx, s.rootKey)
		if err != nil {
			return err
		}
		redirect, err := redirectURI(ctx, tx)
		if !f.sameProvider(p) || err != nil || redirect != f.redirectURI {
			return expired()
		}
		if f.purpose == api.OIDCPurposeSignIn {
			out, err = s.signIn(ctx, tx, f, issuer, c.subject, a)
			return err
		}
		id := store.NewID(idPrefix)
		linkedWith := f.signIn
		if f.link != "" {
			// The link is checked first, and spent once the identity is
			// written; any refusal after rolls the spending back.
			var started signin.Started
			if l, started, err = s.signins.SpendLink(ctx, tx, f.link, a, api.MethodIdentity, id); err != nil {
				return err
			}
			out.Started, linkedWith = &started, f.link
		} else {
			si, err := s.signins.Live(ctx, tx, token, op.Now(ctx))
			if err != nil || si.ID != f.signIn {
				return api.CodeSignInRequired.Errorf("the Sign-in this link began from has ended: sign in again, and link from Account")
			}
		}
		if err := s.link(ctx, tx, f.userID, id, issuer, c, linkedWith, a); err != nil {
			return err
		}
		out.Location = accountPage + "?" + api.LinkedParam + "=1"
		return nil
	})
	if err != nil {
		e, coded := errors.AsType[*api.Error](err)
		r = refusal{err: err, counts: coded && e.Code == api.CodeIdentityNotLinked.Name}
		if f.link != "" && s.signins.LinkRefused(l, err, a) {
			r.counts = true
		}
		return Outcome{}, r
	}
	return out, refusal{}
}

// signIn starts a Sign-in through the Linked Identity (issuer, subject) of
// a User not Removed, moving its Last used, or refuses it with
// identity_not_linked, the same whether or not anyone ever had it.
func (s *Service) signIn(ctx context.Context, tx store.Tx, f oidcFlow, issuer, subject string, a signin.Attempt) (Outcome, error) {
	var id, name string
	err := tx.QueryRowContext(ctx, `SELECT i.id, u.name FROM identities i JOIN users u ON u.id = i.user_id
		WHERE i.issuer = ? AND i.subject = ? AND u.removed_at IS NULL`, issuer, subject).Scan(&id, &name)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return Outcome{}, api.CodeIdentityNotLinked.Errorf("no User has this identity at the Identity Provider linked: link it from Account first, or sign in another way")
	case err != nil:
		return Outcome{}, fmt.Errorf("read the Linked Identity: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "UPDATE identities SET last_used_at = ? WHERE id = ?", op.Now(ctx).Format(time.RFC3339), id); err != nil {
		return Outcome{}, fmt.Errorf("record the use of the Linked Identity %s: %w", id, err)
	}
	u, err := user.ByName(ctx, tx, name)
	if err != nil {
		return Outcome{}, err
	}
	started, err := s.signins.Start(ctx, tx, u, a, api.MethodIdentity, id)
	if err != nil {
		return Outcome{}, err
	}
	return Outcome{Location: f.next, Started: &started}, nil
}

// link records the identity (issuer, the claims' subject) under id as the
// Linked Identity of the User userID, linked through linkedWith, the
// Sign-in or the Sign-in Link, once neither that User nor another holds
// it, and logs it once the call commits.
func (s *Service) link(ctx context.Context, tx store.Tx, userID, id, issuer string, c claims, linkedWith string, a signin.Attempt) error {
	var name string
	var holds, taken bool
	err := tx.QueryRowContext(ctx, `SELECT name,
			EXISTS (SELECT 1 FROM identities WHERE user_id = ?1),
			EXISTS (SELECT 1 FROM identities WHERE issuer = ?2 AND subject = ?3)
		FROM users WHERE id = ?1 AND removed_at IS NULL`, userID, issuer, c.subject).Scan(&name, &holds, &taken)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return expired()
	case err != nil:
		return fmt.Errorf("read the Linked Identities: %w", err)
	case holds:
		return api.CodeIdentityLinked.Errorf("%s has a Linked Identity already: unlink it first to link another", name)
	case taken:
		return api.CodeIdentityTaken.Errorf("another User has this identity at the Identity Provider linked")
	}
	now := op.Now(ctx).Format(time.RFC3339)
	_, err = tx.ExecContext(ctx, `INSERT INTO identities (id, user_id, issuer, subject, label, linked_with, linked_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, id, userID, issuer, c.subject, c.label, linkedWith, now)
	if err != nil {
		return fmt.Errorf("record the Linked Identity of %s: %w", name, err)
	}
	op.AfterTx(ctx, func(committed bool) {
		if committed {
			s.log.Info("identity linked", "user", name, "identity", id, "issuer", issuer, "subject", c.subject,
				"linked_with", linkedWith, "address", a.Address)
		}
	})
	return nil
}

// callbackCodes are the codes a callback's refusal sends a page, each of
// which the page words: any other is provider_failed at the step
// discovery.
var callbackCodes = []api.Code{
	api.CodeSignInExpired, api.CodeProviderRefused, api.CodeProviderFailed, api.CodeProviderUnreachable,
	api.CodeIdentityNotLinked, api.CodeIdentityLinked, api.CodeIdentityTaken, api.CodeSignInRequired,
	api.CodeLinkUsed, api.CodeLinkInvalid, api.CodeTooManyAttempts, api.CodeInternal,
}

// refuse is the outcome of f refused with err from a, at step for
// provider_failed, logging it with attrs: a coded refusal at f's page; any
// other failure, which the log holds, as internal_error there.
func (s *Service) refuse(f oidcFlow, a signin.Attempt, err error, step string, attrs ...any) Outcome {
	e, coded := errors.AsType[*api.Error](err)
	code, registered := api.Code{}, false
	if coded {
		code, registered = api.LookupCode(e.Code)
	}
	if !registered {
		s.log.Error("sign-in through the identity provider failed", "purpose", f.purpose, "address", a.Address, "err", err)
		return refusedAt(f.page(), api.CodeInternal)
	}
	if !slices.Contains(callbackCodes, code) {
		// Such as invalid_provider, from a discovery fetched again since
		// the flow began: a page words only the callback's own codes.
		attrs = append(attrs, "cause", code.Name)
		code, step = api.CodeProviderFailed, stepDiscovery
	}
	line := append([]any{"reason", code.Name, "purpose", f.purpose}, attrs...)
	if step != "" {
		line = append(line, "step", step)
	}
	line = append(line, "address", a.Address)
	switch code {
	case api.CodeSignInExpired:
		s.log.Debug("sign-in refused", line...)
	case api.CodeLinkUsed, api.CodeLinkInvalid, api.CodeTooManyAttempts:
		// LinkRefused, or the limits, logged it.
	default:
		s.log.Info("sign-in refused", line...)
	}
	return refusedAt(f.page(), code)
}
