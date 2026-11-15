package web_test

import (
	"database/sql"
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/server/store"
	"github.com/cieons/koko/internal/shared/api"
	"github.com/cieons/koko/internal/shared/timing"
)

// startIssuer starts startSignIns' koko server, whose Identity Provider
// is a new Issuer, with opts.
func startIssuer(t *testing.T, opts ...harness.Option) (*harness.Harness, *harness.Issuer) {
	t.Helper()
	iss := harness.NewIssuer(t)
	h := harness.Start(t, append([]harness.Option{harness.WebListen("127.0.0.1:0"), harness.IdentityProvider(iss)}, opts...)...)
	h.Browser() // sets the Web URL
	for _, u := range []string{"bob", "carol", "dave"} {
		h.Koko("user", "invite", u).WantSuccess(t)
	}
	h.SetIssuer(iss)
	return h, iss
}

var (
	signInThrough = api.AuthOIDCBeginRequest{Purpose: api.OIDCPurposeSignIn}
	linkToSignIn  = api.AuthOIDCBeginRequest{Purpose: api.OIDCPurposeLink}
)

// linkThrough is the request that links through the Sign-in Link link.
func linkThrough(link string) api.AuthOIDCBeginRequest {
	return api.AuthOIDCBeginRequest{Purpose: api.OIDCPurposeLink, Link: harness.LinkSecret(link)}
}

// wantSeeOther checks that a is a 303 to location.
func wantSeeOther(t *testing.T, a harness.Answer, location string) {
	t.Helper()
	if a.StatusCode != http.StatusSeeOther || a.Header.Get("Location") != location {
		t.Errorf("the callback answered %d to %q, want 303 to %q", a.StatusCode, a.Header.Get("Location"), location)
	}
}

// link links the identity iss signs in as next to user, from a Sign-in of
// theirs, and returns that browser.
func link(t *testing.T, h *harness.Harness, iss *harness.Issuer, user string) *harness.Browser {
	t.Helper()
	b := h.SignIn(user)
	wantSeeOther(t, b.ThroughProvider(iss, linkToSignIn), "/account?linked=1")
	return b
}

// signedIn is user.current's Sign-in from b, and its User's name.
type signedIn struct {
	api.CurrentSignIn
	User string
}

// currentSignIn is user.current's Sign-in from b.
func currentSignIn(t *testing.T, b *harness.Browser) signedIn {
	t.Helper()
	status, body := b.Send(api.UserCurrent, []byte(`{}`))
	var resp api.UserCurrentResponse
	if err := json.Unmarshal(body, &resp); status != http.StatusOK || err != nil || resp.SignIn == nil {
		t.Fatalf("user.current answered %d %s, want a Sign-in", status, body)
	}
	return signedIn{CurrentSignIn: *resp.SignIn, User: resp.User.Name}
}

// userOf is user from user.list over the Unix socket.
func userOf(t *testing.T, h *harness.Harness, name string) api.User {
	t.Helper()
	resp, err := h.Call(api.UserList, api.UserListRequest{})
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range resp.Users {
		if u.Name == name {
			return u
		}
	}
	t.Fatalf("user.list has no %s", name)
	return api.User{}
}

// flowCookie is the binding cookie's Set-Cookie as begin sets it.
var flowCookie = regexp.MustCompile(`^__Host-koko-oidc=[A-Za-z0-9_-]{22}; Path=/; Max-Age=600; HttpOnly; Secure; SameSite=Lax$`)

// A User links their identity from Account, then signs in through the
// provider: begin sets the binding cookie and answers the authorization
// URL; the callback clears the cookie, links the identity, and a later
// sign-in starts a Sign-in made through it, ending at next, its Last used
// moved, with the log lines it names.
func TestLinkingFromAccountThenSigningInThroughTheProvider(t *testing.T) {
	t.Parallel()
	h, iss := startIssuer(t)
	b := h.SignIn("bob")
	kept := currentSignIn(t, b)
	a := b.Do(http.MethodPost, api.AuthOIDCBeginPath, `{"purpose":"link"}`)
	if got := a.Header.Get("Set-Cookie"); !flowCookie.MatchString(got) {
		t.Errorf("begin set the cookie %q", got)
	}
	var begun api.AuthOIDCBeginResponse
	if err := json.Unmarshal([]byte(a.Body), &begun); a.StatusCode != http.StatusOK || err != nil {
		t.Fatalf("begin answered %d %s", a.StatusCode, a.Body)
	}
	if !strings.HasPrefix(begun.Location, iss.URL()+"authorize?") {
		t.Errorf("begin sent the browser to %s, want the issuer's authorization endpoint", begun.Location)
	}
	back := b.Callback(iss.Authorize(begun.Location))
	wantSeeOther(t, back, "/account?linked=1")
	if got := back.Header.Get("Set-Cookie"); !strings.HasPrefix(got, "__Host-koko-oidc=; Path=/; Max-Age=0;") {
		t.Errorf("the callback set %q, want the flow's cookie cleared", got)
	}
	for name, want := range map[string]string{"Cache-Control": "no-store", "Referrer-Policy": "no-referrer"} {
		if got := back.Header.Get(name); got != want {
			t.Errorf("the callback's %s is %q, want %q", name, got, want)
		}
	}
	if b.FlowCookie != "" {
		t.Error("the browser still holds the flow's cookie")
	}
	if si := currentSignIn(t, b); si.ID != kept.ID {
		t.Errorf("linking moved the browser to the Sign-in %s, want its own %s kept", si.ID, kept.ID)
	}
	if userOf(t, h, "bob").IdentityLinkedAt == "" {
		t.Error("user.list gives bob no identity_linked_at")
	}

	other := h.Browser()
	back = other.ThroughProvider(iss, api.AuthOIDCBeginRequest{Purpose: api.OIDCPurposeSignIn, Next: "/hosts?q=web"})
	wantSeeOther(t, back, "/hosts?q=web")
	if !signInCookie.MatchString(back.Header.Values("Set-Cookie")[1]) {
		t.Errorf("the callback set %q, want the Sign-in cookie", back.Header.Values("Set-Cookie"))
	}
	si := currentSignIn(t, other)
	if si.User != "bob" || si.Method != api.MethodIdentity || !strings.HasPrefix(si.Credential, "identity_") {
		t.Errorf("the provider signed in %+v, want bob through his identity", si)
	}
	if _, ok := signInsOf(t, h, "bob")[kept.ID]; !ok {
		t.Error("signing in through the provider ended bob's other Sign-in")
	}
	log := h.Log()
	for _, want := range []string{
		`msg="identity linked" user=bob identity=` + si.Credential + ` issuer=` + iss.URL() + ` subject=subject-alice linked_with=` + kept.ID + ` address=192.0.2.10`,
		`msg="signed in" user=bob signin=` + si.ID + ` method=identity identity=` + si.Credential,
	} {
		if !strings.Contains(log, want) {
			t.Errorf("the log does not say %q:\n%s", want, log)
		}
	}
}

// A User links their identity through a Sign-in Link: the callback spends
// the link, signs the browser in through the identity, ends the User's
// other Sign-ins and lands on Account. The provider's label is kept, and a
// later begin through the spent link is refused link_used.
func TestLinkingThroughASignInLink(t *testing.T) {
	t.Parallel()
	h, iss := startIssuer(t)
	before := currentSignIn(t, h.SignIn("dave"))
	l := passwordLink(t, h, "dave")
	b := h.Browser()
	iss.SignInAs("subject-dave", "dave.d")
	wantSeeOther(t, b.ThroughProvider(iss, linkThrough(l)), "/account?linked=1")
	si := currentSignIn(t, b)
	if si.User != "dave" || si.Method != api.MethodIdentity {
		t.Errorf("the link signed in %+v, want dave through his identity", si)
	}
	if got := signInsOf(t, h, "dave")[before.ID]; got.EndReason != api.EndSignInLinkUsed {
		t.Errorf("dave's other Sign-in ended as %q, want signin_link_used", got.EndReason)
	}
	status, body := h.Browser().BeginOIDC(linkThrough(l))
	wantCode(t, status, body, api.CodeLinkUsed.Name)
	if !strings.Contains(h.Log(), `msg="identity linked" user=dave identity=`+si.Credential) {
		t.Errorf("the log does not say the identity was linked:\n%s", h.Log())
	}
	if strings.Contains(h.Log(), "dave.d") {
		t.Error("the log holds the provider's label")
	}
	b2 := h.Browser()
	wantSeeOther(t, b2.ThroughProvider(iss, signInThrough), "/")
	if got := currentSignIn(t, b2).User; got != "dave" {
		t.Errorf("signing in through dave's identity signed in %s", got)
	}
}

// A refusal at the provider spends no Sign-in Link: the browser lands on
// the link page, refused, and the link still opens.
func TestARefusalAtTheProviderSpendsNoLink(t *testing.T) {
	t.Parallel()
	h, iss := startIssuer(t)
	l := passwordLink(t, h, "dave")
	iss.Deny()
	wantSeeOther(t, h.Browser().ThroughProvider(iss, linkThrough(l)), "/link?refused=provider_refused")
	if status, body := h.Browser().Post(api.AuthLinkPath, api.AuthLinkRequest{Link: harness.LinkSecret(l)}); status != http.StatusOK {
		t.Errorf("the link no longer opens after a refusal at the provider: %d %s", status, body)
	}
	if !strings.Contains(h.Log(), `msg="sign-in refused" reason=provider_refused purpose=link error=access_denied address=192.0.2.10`) {
		t.Errorf("the log does not name the provider's refusal:\n%s", h.Log())
	}
}

// An identity no User has linked is refused identity_not_linked; one a
// User has linked is identity_linked for that User again, and
// identity_taken for another.
func TestLinkingRefusesAnIdentityHeldAlready(t *testing.T) {
	t.Parallel()
	h, iss := startIssuer(t)
	wantSeeOther(t, h.Browser().ThroughProvider(iss, signInThrough), "/signin?refused=identity_not_linked")
	b := link(t, h, iss, "bob")
	iss.SignInAs("subject-other", "other")
	wantSeeOther(t, b.ThroughProvider(iss, linkToSignIn), "/account?refused=identity_linked")
	iss.SignInAs("subject-alice", "alice@idp.example.com")
	wantSeeOther(t, h.SignIn("carol").ThroughProvider(iss, linkToSignIn), "/account?refused=identity_taken")
	l := passwordLink(t, h, "dave")
	wantSeeOther(t, h.Browser().ThroughProvider(iss, linkThrough(l)), "/link?refused=identity_taken")
	if userOf(t, h, "carol").IdentityLinkedAt != "" || userOf(t, h, "dave").IdentityLinkedAt != "" {
		t.Error("a refused link linked an identity")
	}
	// The link the refusal came through was not spent.
	if status, body := h.Browser().Post(api.AuthLinkPath, api.AuthLinkRequest{Link: harness.LinkSecret(l)}); status != http.StatusOK {
		t.Errorf("dave's link after the refusal: %d %s", status, body)
	}
}

// A subject linked at one issuer signs nobody in from another: an identity
// is its issuer and its subject together. The product unlinks every
// identity when the issuer changes, so the test writes one at another
// issuer itself.
func TestAKnownSubjectAtAnotherIssuerIsNotLinked(t *testing.T) {
	t.Parallel()
	iss := harness.NewIssuer(t)
	dir, rootKey := t.TempDir(), harness.NewRootKeyFile(t)
	opts := []harness.Option{harness.DataDir(dir), harness.RootKeyFile(rootKey), harness.WebListen("127.0.0.1:0"), harness.IdentityProvider(iss)}
	h := harness.Start(t, opts...)
	h.Browser()
	h.Koko("user", "invite", "bob").WantSuccess(t)
	h.SetIssuer(iss)
	link(t, h, iss, "bob")
	h.Stop()
	write(t, dir, "UPDATE identities SET issuer = 'https://other.example.com/'")
	h = harness.Start(t, append(opts, harness.NoInit())...)
	wantSeeOther(t, h.Browser().ThroughProvider(iss, signInThrough), "/signin?refused=identity_not_linked")
}

// write runs query on the koko.db in dir, whose koko server has stopped.
func write(t *testing.T, dir, query string) {
	t.Helper()
	st, err := store.Open(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	err = st.Write(t.Context(), func(tx store.Tx) error {
		_, err := tx.ExecContext(t.Context(), query)
		return err
	})
	if closeErr := st.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		t.Fatal(err)
	}
}

// Every ID token that fails a check is provider_failed, logged with its
// step, and signs nobody in; a token whose exp is just within the 60 s of
// skew, or whose nbf is within go-oidc's 5 minutes, passes.
func TestEverySpoiledIDTokenFails(t *testing.T) {
	t.Parallel()
	h, iss := startIssuer(t)
	link(t, h, iss, "bob")
	now := h.Now()
	for _, c := range []struct {
		name    string
		spoil   func(header, claims map[string]any)
		foreign bool
		step    string
	}{
		{name: "another iss", spoil: func(_, c map[string]any) { c["iss"] = strings.TrimSuffix(iss.URL(), "/") }, step: "issuer"},
		{name: "another aud", spoil: func(_, c map[string]any) { c["aud"] = "other" }, step: "audience"},
		{name: "another azp", spoil: func(_, c map[string]any) { c["azp"] = "other" }, step: "audience"},
		{name: "two audiences without azp", spoil: func(_, c map[string]any) { c["aud"] = []string{harness.IssuerClientID, "other"} }, step: "audience"},
		{name: "exp past the skew", spoil: func(_, c map[string]any) { c["exp"] = now.Add(-61 * time.Second).Unix() }, step: "expiry"},
		{name: "no exp", spoil: func(_, c map[string]any) { delete(c, "exp") }, step: "expiry"},
		{name: "nbf past the leeway", spoil: func(_, c map[string]any) { c["nbf"] = now.Add(5*time.Minute + 2*time.Second).Unix() }, step: "expiry"},
		{name: "another nonce", spoil: func(_, c map[string]any) { c["nonce"] = "AAAAAAAAAAAAAAAAAAAAAA" }, step: "nonce"},
		{name: "no nonce", spoil: func(_, c map[string]any) { delete(c, "nonce") }, step: "nonce"},
		{name: "no sub", spoil: func(_, c map[string]any) { delete(c, "sub") }, step: "subject"},
		{name: "a sub of 256 bytes", spoil: func(_, c map[string]any) { c["sub"] = strings.Repeat("s", 256) }, step: "subject"},
		{name: "alg none", spoil: func(h, _ map[string]any) { h["alg"] = "none" }, step: "signature"},
		{name: "an unknown key id", spoil: func(h, _ map[string]any) { h["kid"] = "key-unknown" }, step: "signature"},
		{name: "a foreign key", foreign: true, step: "signature"},
	} {
		iss.SpoilToken(c.spoil)
		iss.SignWithForeignKey(c.foreign)
		logged := len(h.Log())
		b := h.Browser()
		b.Address = "192.0.2.40"
		wantSeeOther(t, b.ThroughProvider(iss, signInThrough), "/signin?refused=provider_failed")
		if b.Cookie != "" {
			t.Errorf("%s: the browser was signed in", c.name)
		}
		want := `msg="sign-in refused" reason=provider_failed purpose=signin step=` + c.step + ` address=192.0.2.40`
		if n := strings.Count(h.Log()[logged:], want); n != 1 {
			t.Errorf("%s: the log says %q %d times, want once", c.name, want, n)
		}
		h.Advance(15 * time.Minute) // past the address's count
		now = h.Now()
	}
	iss.SignWithForeignKey(false)
	for name, spoil := range map[string]func(_, c map[string]any){
		"exp just within the skew":   func(_, c map[string]any) { c["exp"] = h.Now().Add(-50 * time.Second).Unix() },
		"nbf just within the leeway": func(_, c map[string]any) { c["nbf"] = h.Now().Add(4 * time.Minute).Unix() },
		"azp naming the client":      func(_, c map[string]any) { c["azp"] = harness.IssuerClientID },
		"two audiences with their azp": func(_, c map[string]any) {
			c["aud"], c["azp"] = []string{"other", harness.IssuerClientID}, harness.IssuerClientID
		},
	} {
		iss.SpoilToken(spoil)
		if a := h.Browser().ThroughProvider(iss, signInThrough); a.Header.Get("Location") != "/" {
			t.Errorf("%s: the callback went to %q, want /", name, a.Header.Get("Location"))
		}
	}
}

// A rotated key is accepted after one fetch of the key set: the first
// token fetches it, the next with the same key uses it as kept, and one
// under the new key fetches it once more.
func TestARotatedKeyIsAcceptedAfterOneFetch(t *testing.T) {
	t.Parallel()
	h, iss := startIssuer(t)
	link(t, h, iss, "bob")
	fetches := func() int {
		return strings.Count(strings.Join(iss.Requests(), " "), "/jwks")
	}
	for _, want := range []int{1, 1} {
		wantSeeOther(t, h.Browser().ThroughProvider(iss, signInThrough), "/")
		if got := fetches(); got != want {
			t.Errorf("the key set was fetched %d times, want %d", got, want)
		}
	}
	iss.RotateKey()
	wantSeeOther(t, h.Browser().ThroughProvider(iss, signInThrough), "/")
	if got := fetches(); got != 2 {
		t.Errorf("the key set was fetched %d times after a rotation, want 2", got)
	}
}

// A callback that fails its binding goes to signin_expired, changing
// nothing and counting for no limit: without the cookie, with another
// browser's, replayed, after 10 minutes, and after a restart.
func TestACallbackWithoutItsBindingChangesNothing(t *testing.T) {
	t.Parallel()
	iss := harness.NewIssuer(t)
	dir, rootKey := t.TempDir(), harness.NewRootKeyFile(t)
	opts := []harness.Option{harness.DataDir(dir), harness.RootKeyFile(rootKey), harness.WebListen("127.0.0.1:0"), harness.IdentityProvider(iss)}
	h := harness.Start(t, opts...)
	h.Browser()
	h.Koko("user", "invite", "bob").WantSuccess(t)
	h.SetIssuer(iss)
	b := h.SignIn("bob")
	expired := func(what string, a harness.Answer) {
		t.Helper()
		if a.StatusCode != http.StatusSeeOther || a.Header.Get("Location") != "/signin?refused=signin_expired" {
			t.Errorf("%s: the callback answered %d to %q, want signin_expired", what, a.StatusCode, a.Header.Get("Location"))
		}
		if userOf(t, h, "bob").IdentityLinkedAt != "" {
			t.Fatalf("%s linked an identity", what)
		}
	}

	back := iss.Authorize(b.StartOIDC(linkToSignIn))
	b.FlowCookie = ""
	expired("no cookie", b.Callback(back))

	other := h.Browser()
	other.StartOIDC(signInThrough)
	back = iss.Authorize(b.StartOIDC(linkToSignIn))
	b.FlowCookie = other.FlowCookie
	expired("another browser's cookie", b.Callback(back))

	back = iss.Authorize(b.StartOIDC(linkToSignIn))
	cookie := b.FlowCookie
	wantSeeOther(t, b.Callback(back), "/account?linked=1")
	b.FlowCookie = cookie
	unlinked := userOf(t, h, "bob").IdentityLinkedAt
	if a := b.Callback(back); a.Header.Get("Location") != "/signin?refused=signin_expired" {
		t.Errorf("a replay answered %d to %q, want signin_expired", a.StatusCode, a.Header.Get("Location"))
	}
	if userOf(t, h, "bob").IdentityLinkedAt != unlinked {
		t.Error("a replay changed bob's identity")
	}

	carol := h.Browser()
	back = iss.Authorize(carol.StartOIDC(signInThrough))
	h.Advance(10 * time.Minute)
	if a := carol.Callback(back); a.Header.Get("Location") != "/signin?refused=signin_expired" || carol.Cookie != "" {
		t.Errorf("a callback after 10 minutes answered %d to %q, want signin_expired", a.StatusCode, a.Header.Get("Location"))
	}

	back = iss.Authorize(carol.StartOIDC(signInThrough))
	h.Stop()
	h = harness.Start(t, append(opts, harness.NoInit())...)
	again := h.Browser()
	again.FlowCookie = carol.FlowCookie
	if a := again.Callback(back); a.Header.Get("Location") != "/signin?refused=signin_expired" {
		t.Errorf("a callback after a restart answered %d to %q, want signin_expired", a.StatusCode, a.Header.Get("Location"))
	}
	if strings.Contains(h.Log(), "level=INFO msg=\"sign-in refused\"") {
		t.Errorf("a callback without its binding was logged at info:\n%s", h.Log())
	}
}

// Fifty forged callbacks from one address, of any state, leave its
// sign-ins open: none counts for its limit.
func TestForgedCallbacksLockNoAddressOut(t *testing.T) {
	t.Parallel()
	h, iss := startIssuer(t)
	link(t, h, iss, "bob")
	b := h.Browser()
	b.Address = "192.0.2.50"
	for i := range 50 {
		state := url.Values{"state": {strings.Repeat("A", i%23)}, "code": {"forged"}}
		if a := b.Callback(harness.WebURL + api.AuthOIDCCallbackPath + "?" + state.Encode()); a.Header.Get("Location") != "/signin?refused=signin_expired" {
			t.Fatalf("a forged callback answered %d to %q", a.StatusCode, a.Header.Get("Location"))
		}
	}
	wantSeeOther(t, b.ThroughProvider(iss, signInThrough), "/")
	if status, body := h.Browser().Post(api.AuthSignInPath, api.AuthSignInRequest{Name: "bob", Password: harness.Password}); status != http.StatusOK {
		t.Errorf("a sign-in with a Password after the forged callbacks: %d %s", status, body)
	}
}

// An attacker who sends a victim's browser to the callback with the
// attacker's own code links nothing, whether the victim holds no flow's
// cookie or one of their own flow.
func TestAnAttackersCodeFinishedInAVictimsBrowserLinksNothing(t *testing.T) {
	t.Parallel()
	h, iss := startIssuer(t)
	attacker := h.SignIn("carol")
	iss.SignInAs("subject-mallory", "mallory")
	forged := iss.Authorize(attacker.StartOIDC(linkToSignIn))
	victim := h.SignIn("bob")
	if a := victim.Callback(forged); a.Header.Get("Location") != "/signin?refused=signin_expired" {
		t.Errorf("the attacker's callback in the victim's browser answered %q, want signin_expired", a.Header.Get("Location"))
	}
	forged = iss.Authorize(attacker.StartOIDC(linkToSignIn))
	victim.StartOIDC(linkToSignIn)
	if a := victim.Callback(forged); a.Header.Get("Location") != "/signin?refused=signin_expired" {
		t.Errorf("the attacker's callback in a victim's browser with a flow of its own answered %q, want signin_expired", a.Header.Get("Location"))
	}
	for _, u := range []string{"bob", "carol"} {
		if userOf(t, h, u).IdentityLinkedAt != "" {
			t.Errorf("%s has an identity linked", u)
		}
	}
	// The forged identity signs nobody in.
	wantSeeOther(t, h.Browser().ThroughProvider(iss, signInThrough), "/signin?refused=identity_not_linked")
}

// A link flow whose Sign-in or Sign-in Link ended while the person was at
// the provider is refused, linking nothing.
func TestALinkFlowWhoseSignInOrLinkEndedIsRefused(t *testing.T) {
	t.Parallel()
	h, iss := startIssuer(t)
	b := h.SignIn("bob")
	back := iss.Authorize(b.StartOIDC(linkToSignIn))
	if status, body := b.Post(api.AuthSignOutPath, api.AuthSignOutRequest{}); status != http.StatusOK {
		t.Fatal(status, body)
	}
	wantSeeOther(t, b.Callback(back), "/account?refused=signin_required")
	l := passwordLink(t, h, "dave")
	d := h.Browser()
	back = iss.Authorize(d.StartOIDC(linkThrough(l)))
	passwordLink(t, h, "dave") // voids l
	wantSeeOther(t, d.Callback(back), "/link?refused=link_invalid")
	for _, u := range []string{"bob", "dave"} {
		if userOf(t, h, u).IdentityLinkedAt != "" {
			t.Errorf("%s has an identity linked", u)
		}
	}
}

// A flow begun before the provider's issuer or client changed is refused;
// one begun before its name changed goes on.
func TestAFlowIsBoundToItsProvidersClient(t *testing.T) {
	t.Parallel()
	h, iss := startIssuer(t)
	b := h.SignIn("bob")
	back := iss.Authorize(b.StartOIDC(linkToSignIn))
	if _, err := h.Call(api.ServerOIDCSet, api.ServerOIDCSetRequest{Name: new("Keycloak")}); err != nil {
		t.Fatal(err)
	}
	wantSeeOther(t, b.Callback(back), "/account?linked=1")
	c := h.SignIn("carol")
	back = iss.Authorize(c.StartOIDC(linkToSignIn))
	if _, err := h.Call(api.ServerOIDCSet, api.ServerOIDCSetRequest{ClientID: new("koko-2")}); err != nil {
		t.Fatal(err)
	}
	wantSeeOther(t, c.Callback(back), "/account?refused=signin_expired")
}

// Begin refuses a flow it cannot begin: with no provider, with a purpose it
// does not know, to link without a Sign-in or through a link no longer
// valid.
func TestBeginRefusesWhatItCannotBegin(t *testing.T) {
	t.Parallel()
	h := startSignIns(t)
	status, body := h.Browser().BeginOIDC(signInThrough)
	wantCode(t, status, body, api.CodeProviderUnset.Name)
	h, _ = startIssuer(t)
	for _, c := range []struct {
		req  api.AuthOIDCBeginRequest
		code api.Code
	}{
		{api.AuthOIDCBeginRequest{Purpose: "other"}, api.CodeInvalidRequest},
		{api.AuthOIDCBeginRequest{Purpose: api.OIDCPurposeSignIn, Link: "AAAA"}, api.CodeInvalidRequest},
		{api.AuthOIDCBeginRequest{Purpose: api.OIDCPurposeLink, Next: "/"}, api.CodeInvalidRequest},
		{linkToSignIn, api.CodeSignInRequired},
		{api.AuthOIDCBeginRequest{Purpose: api.OIDCPurposeLink, Link: "AAAAAAAAAAAAAAAAAAAAAAAAAA"}, api.CodeLinkInvalid},
	} {
		b := h.Browser()
		status, body := b.BeginOIDC(c.req)
		wantCode(t, status, body, c.code.Name)
		if b.FlowCookie != "" {
			t.Errorf("a refused begin %+v set the flow's cookie", c.req)
		}
	}
}

// next keeps only a path of this origin outside the pages that sign in.
func TestASignInEndsOnlyOnAPathOfThisOrigin(t *testing.T) {
	t.Parallel()
	h, iss := startIssuer(t)
	link(t, h, iss, "bob")
	for next, want := range map[string]string{
		"/spaces/ops":          "/spaces/ops",
		"":                     "/",
		"https://evil.example": "/",
		"//evil.example/":      "/",
		"/\\evil.example":      "/",
		"/signin":              "/",
		"/link?x=1":            "/",
		"/password":            "/",
		"/auth/oidc/callback":  "/",
		"spaces":               "/",
		"/a\tb":                "/",
	} {
		a := h.Browser().ThroughProvider(iss, api.AuthOIDCBeginRequest{Purpose: api.OIDCPurposeSignIn, Next: next})
		if got := a.Header.Get("Location"); got != want {
			t.Errorf("next %q ended on %q, want %q", next, got, want)
		}
	}
}

// Ten failures after a passed binding lock the address out of the flows:
// its next callback is refused too_many_attempts, and so is its next
// begin. A refusal at the provider counts for nothing.
func TestFailedCallbacksCountForTheAddress(t *testing.T) {
	t.Parallel()
	h, iss := startIssuer(t)
	b := h.Browser()
	b.Address = "192.0.2.60"
	iss.Deny()
	for range 10 {
		wantSeeOther(t, b.ThroughProvider(iss, signInThrough), "/signin?refused=provider_refused")
	}
	iss.SignInAs("subject-nobody", "nobody")
	// Begun while the address may still begin one.
	late := h.Browser()
	late.Address = b.Address
	back := iss.Authorize(late.StartOIDC(signInThrough))
	for range 10 {
		wantSeeOther(t, b.ThroughProvider(iss, signInThrough), "/signin?refused=identity_not_linked")
	}
	wantSeeOther(t, late.Callback(back), "/signin?refused=too_many_attempts")
	h.Advance(time.Minute)
	status, body := b.BeginOIDC(signInThrough)
	wantCode(t, status, body, api.CodeTooManyAttempts.Name)
}

// The exchange follows no redirect: a token endpoint that redirects is
// provider_failed, and its body, which holds the client secret, the code
// and the verifier, goes nowhere else.
func TestTheExchangeFollowsNoRedirect(t *testing.T) {
	t.Parallel()
	h, iss := startIssuer(t)
	link(t, h, iss, "bob")
	var mu sync.Mutex
	reached := 0
	elsewhere := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		reached++
	}))
	t.Cleanup(elsewhere.Close)
	iss.RedirectToken(elsewhere.URL + "/token")
	wantSeeOther(t, h.Browser().ThroughProvider(iss, signInThrough), "/signin?refused=provider_failed")
	mu.Lock()
	defer mu.Unlock()
	if reached != 0 {
		t.Errorf("the exchange reached the redirect's target %d times", reached)
	}
	if !strings.Contains(h.Log(), `reason=provider_failed purpose=signin step=exchange`) {
		t.Errorf("the log does not name the exchange:\n%s", h.Log())
	}
}

// A token endpoint that refuses the exchange is provider_failed; one that
// answers a server error is provider_unreachable, which counts for
// nothing.
func TestAnExchangeTheProviderRefusesFails(t *testing.T) {
	t.Parallel()
	h, iss := startIssuer(t)
	link(t, h, iss, "bob")
	iss.AnswerToken(http.StatusBadRequest)
	wantSeeOther(t, h.Browser().ThroughProvider(iss, signInThrough), "/signin?refused=provider_failed")
	iss.AnswerToken(http.StatusServiceUnavailable)
	wantSeeOther(t, h.Browser().ThroughProvider(iss, signInThrough), "/signin?refused=provider_unreachable")
}

// With the provider holding every call, at most 4 are in flight, and a
// callback past them is provider_unreachable once its turn does not come;
// meanwhile no transaction is held, so other writes go on and a checkpoint
// completes. Once the provider answers, the 4 sign in.
func TestCallbacksBoundTheCallsInFlight(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// The held calls' own bound is far beyond the test's steps.
	h, iss := startIssuer(t, harness.DataDir(dir), harness.Timings(timing.Timings{ProviderTurn: 200 * time.Millisecond, ProviderCall: time.Minute}))
	link(t, h, iss, "bob")
	type pending struct {
		b    *harness.Browser
		back string
	}
	var flows []pending
	for range 5 {
		b := h.Browser()
		flows = append(flows, pending{b, iss.Authorize(b.StartOIDC(signInThrough))})
	}
	iss.Hold()
	var held sync.WaitGroup
	ended := make(chan string, 4)
	for _, f := range flows[:4] {
		held.Go(func() { ended <- f.b.Callback(f.back).Header.Get("Location") })
	}
	deadline := time.Now().Add(10 * time.Second)
	for iss.Waiting() < 4 {
		if time.Now().After(deadline) {
			t.Fatalf("%d calls reached the provider, want 4", iss.Waiting())
		}
		time.Sleep(10 * time.Millisecond)
	}
	h.Koko("space", "add", "ops").WantSuccess(t)
	var busy, frames, done int
	db, err := sql.Open("sqlite", filepath.Join(dir, "koko.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if err := db.QueryRowContext(t.Context(), "PRAGMA wal_checkpoint(TRUNCATE)").Scan(&busy, &frames, &done); err != nil {
		t.Fatal(err)
	}
	if busy != 0 {
		t.Errorf("a checkpoint while the calls wait was blocked (busy %d, %d of %d frames): a transaction is held", busy, done, frames)
	}
	wantSeeOther(t, flows[4].b.Callback(flows[4].back), "/signin?refused=provider_unreachable")
	if got := iss.Waiting(); got != 4 {
		t.Errorf("%d calls reached the provider, want the 4 in flight", got)
	}
	iss.Release()
	held.Wait()
	close(ended)
	for location := range ended {
		if location != "/" {
			t.Errorf("a callback held in flight ended on %q once answered, want /", location)
		}
	}
}

// No log line, and no answer but begin's, which sends the browser to the
// provider, holds a secret of a flow: the client secret, the state, nonce,
// verifier, code, binding token or any token, the link's secret, a cookie
// or the provider's label for an account; through a link, a sign-in, a
// refusal at each step and a callback without its binding.
func TestNoLogLineOrAnswerHoldsAFlowsSecret(t *testing.T) {
	t.Parallel()
	h, iss := startIssuer(t)
	const label = "label-q8Zt3Kx"
	iss.SignInAs("subject-bob", label)
	var secrets, answers []string
	through := func(b *harness.Browser, req api.AuthOIDCBeginRequest) {
		t.Helper()
		back := iss.Authorize(b.StartOIDC(req))
		secrets = append(secrets, b.FlowCookie, back)
		a := b.Callback(back)
		answers = append(answers, a.Header.Get("Location"), a.Body)
		secrets = append(secrets, b.Cookie)
	}
	l := passwordLink(t, h, "bob")
	through(h.Browser(), linkThrough(l))
	through(h.Browser(), signInThrough)
	iss.Deny()
	through(h.Browser(), signInThrough)
	iss.SignInAs("subject-bob", label)
	iss.SpoilToken(func(_, c map[string]any) { c["nonce"] = "spoiled" })
	through(h.Browser(), signInThrough)
	iss.SpoilToken(nil)
	iss.AnswerToken(http.StatusBadRequest)
	through(h.Browser(), signInThrough)
	iss.AnswerToken(0)
	iss.SignInAs("subject-nobody", label)
	through(h.Browser(), signInThrough)
	b := h.Browser()
	back := iss.Authorize(b.StartOIDC(signInThrough))
	secrets = append(secrets, b.FlowCookie)
	b.FlowCookie = "AAAAAAAAAAAAAAAAAAAAAA"
	a := b.Callback(back)
	answers = append(answers, a.Header.Get("Location"), a.Body)

	shown, err := h.Call(api.ServerOIDCShow, api.ServerOIDCShowRequest{})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(shown)
	answers = append(answers, string(body))
	log := h.Log()
	for _, secret := range append(append(secrets, iss.Secrets()...), harness.IssuerClientSecret, harness.LinkSecret(l), label) {
		if secret == "" {
			continue
		}
		if strings.Contains(log, secret) {
			t.Errorf("the log holds %q", secret)
		}
		for _, answer := range answers {
			if strings.Contains(answer, secret) {
				t.Errorf("the answer %q holds %q", answer, secret)
			}
		}
	}
	for _, want := range []string{"step=nonce", "error=access_denied", "step=exchange", "reason=identity_not_linked", "reason=signin_expired"} {
		if !strings.Contains(log, want) {
			t.Errorf("the log does not say %q, so a step went untried", want)
		}
	}
	if t.Failed() {
		t.Log(log)
	}
}

// Removing a User deletes their Linked Identity: it signs nobody in, and
// no row of it is left.
func TestRemovingAUserDeletesTheirIdentity(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h, iss := startIssuer(t, harness.DataDir(dir))
	link(t, h, iss, "bob")
	h.Koko("user", "remove", "bob").WantSuccess(t)
	wantSeeOther(t, h.Browser().ThroughProvider(iss, signInThrough), "/signin?refused=identity_not_linked")
	h.Stop()
	st, err := store.Open(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	var n int
	if err := st.Read(t.Context(), func(tx store.Tx) error {
		return tx.QueryRowContext(t.Context(), "SELECT count(*) FROM identities").Scan(&n)
	}); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("%d Linked Identities are left after removing their User", n)
	}
}

// Removing the provider ends the Sign-ins made through each identity, and
// only those.
func TestRemovingTheProviderEndsTheSignInsThroughIt(t *testing.T) {
	t.Parallel()
	h, iss := startIssuer(t)
	kept := currentSignIn(t, link(t, h, iss, "bob"))
	b := h.Browser()
	wantSeeOther(t, b.ThroughProvider(iss, signInThrough), "/")
	through := currentSignIn(t, b)
	if _, err := h.Call(api.ServerOIDCRemove, api.ServerOIDCRemoveRequest{}); err != nil {
		t.Fatal(err)
	}
	found := signInsOf(t, h, "bob")
	if got := found[through.ID]; got.EndReason != api.EndIdentityUnlinked {
		t.Errorf("the Sign-in through the identity ended as %q, want identity_unlinked", got.EndReason)
	}
	if got := found[kept.ID]; got.EndedAt != "" {
		t.Errorf("the Sign-in with a Password ended as %q", got.EndReason)
	}
}

// A key set that does not answer, when a token names a key it lacks, is
// provider_unreachable, which counts for nothing, not a bad signature.
func TestAKeySetThatDoesNotAnswerIsUnreachable(t *testing.T) {
	t.Parallel()
	h, iss := startIssuer(t, harness.Timings(timing.Timings{ProviderCall: 200 * time.Millisecond}))
	link(t, h, iss, "bob")
	iss.RotateKey()
	iss.HoldKeys()
	wantSeeOther(t, h.Browser().ThroughProvider(iss, signInThrough), "/signin?refused=provider_unreachable")
	if strings.Contains(h.Log(), "reason=provider_failed") {
		t.Errorf("the key set's silence was taken for a bad signature:\n%s", h.Log())
	}
	iss.Release()
	wantSeeOther(t, h.Browser().ThroughProvider(iss, signInThrough), "/")
}

// Unlinking one's identity from a browser ends the Sign-ins made through
// it, and no others, and the identity signs nobody in after; a Member
// lists no other User's identity.
func TestUnlinkingEndsOnlyTheSignInsThroughIt(t *testing.T) {
	t.Parallel()
	h, iss := startIssuer(t)
	b := link(t, h, iss, "bob")
	kept := currentSignIn(t, b)
	other := h.Browser()
	wantSeeOther(t, other.ThroughProvider(iss, signInThrough), "/")
	through := currentSignIn(t, other)
	status, body := h.SignIn("carol").Send(api.IdentityList, []byte(`{"user":"bob"}`))
	if status != http.StatusOK || string(body) != `{"identities":[]}` {
		t.Errorf("a Member listed bob's identity: %d %s", status, body)
	}
	status, body = b.Send(api.IdentityUnlink, []byte(`{}`))
	var resp api.IdentityUnlinkResponse
	if err := json.Unmarshal(body, &resp); status != http.StatusOK || err != nil || resp.SignInsEnded != 1 || resp.Identity.ID != through.Credential {
		t.Fatalf("identity.unlink answered %d %s", status, body)
	}
	found := signInsOf(t, h, "bob")
	if got := found[through.ID]; got.EndReason != api.EndIdentityUnlinked {
		t.Errorf("the Sign-in through the identity ended as %q, want identity_unlinked", got.EndReason)
	}
	if got := found[kept.ID]; got.EndedAt != "" {
		t.Errorf("the Sign-in with a Password ended as %q", got.EndReason)
	}
	wantSeeOther(t, h.Browser().ThroughProvider(iss, signInThrough), "/signin?refused=identity_not_linked")
	if !strings.Contains(h.Log(), `msg="identity unlinked" user=bob identity=`+through.Credential+` unlinked_by=`+kept.ID+` signins_ended=1`) {
		t.Errorf("the log does not say the identity was unlinked:\n%s", h.Log())
	}
}
