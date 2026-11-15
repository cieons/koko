package web_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/shared/api"
)

// passkeysOf lists user's passkeys over the Unix socket.
func passkeysOf(t *testing.T, h *harness.Harness, user string) []api.Passkey {
	t.Helper()
	resp, err := h.Call(api.PasskeyList, api.PasskeyListRequest{User: user})
	if err != nil {
		t.Fatalf("passkey.list of %s: %v", user, err)
	}
	return resp.Passkeys
}

// addPasskey makes a passkey with auth from b, through link or from b's
// Sign-in, named name, as Browser.AddPasskey does, failing the test unless the koko server records it.
func addPasskey(t *testing.T, b *harness.Browser, auth *harness.Authenticator, link, name string) api.AuthPasskeyAddResponse {
	t.Helper()
	status, body := b.AddPasskey(auth, link, name)
	var resp api.AuthPasskeyAddResponse
	if err := json.Unmarshal([]byte(body), &resp); status != http.StatusOK || err != nil {
		t.Fatalf("/auth/passkey/add answered %d %s", status, body)
	}
	return resp
}

// passkeySignIn signs a new browser in with a passkey auth holds, failing
// the test unless it is signed in.
func passkeySignIn(t *testing.T, h *harness.Harness, auth *harness.Authenticator) *harness.Browser {
	t.Helper()
	b := h.Browser()
	if status, body := b.PasskeySignIn(auth); status != http.StatusOK {
		t.Fatalf("signing in with a passkey answered %d %s", status, body)
	}
	return b
}

// signInCookie is a Sign-in cookie's Set-Cookie as a passkey sets it.
var signInCookie = regexp.MustCompile(`^__Host-koko=[A-Za-z0-9_-]{43}; Path=/; Max-Age=2592000; HttpOnly; Secure; SameSite=Lax$`)

// A passkey made through a Sign-in Link spends it, signs the browser in
// with the method passkey and ends the User's other Sign-ins; it is named
// with the browser's label and added by whoever issued the link. It then
// signs a browser in, setting the Sign-in cookie, and stores its counter,
// so an answer whose counter went back is refused.
func TestAPasskeyMadeThroughALinkSignsIn(t *testing.T) {
	t.Parallel()
	h := startSignIns(t)
	before := h.SignIn("bob")
	auth := harness.NewAuthenticator(harness.WebURL)
	auth.BackupEligible, auth.BackedUp = true, true

	link := passwordLink(t, h, "bob")
	made := h.Browser()
	resp := addPasskey(t, made, auth, link, "")
	if resp.User.Name != "bob" || made.Cookie == "" {
		t.Fatalf("/auth/passkey/add answered %+v, want bob signed in", resp)
	}
	p := resp.Passkey
	if p.Name != "Firefox on Linux" || p.Host != "koko.example.com" || !p.Synced || p.AddedBy == nil || *p.AddedBy != harness.Owner || p.LastUsedAt != nil {
		t.Errorf("the passkey is %+v, want Firefox on Linux for koko.example.com, synced, added by %s, never used", p, harness.Owner)
	}
	if got := passkeysOf(t, h, "bob"); len(got) != 1 || got[0].ID != p.ID {
		t.Errorf("passkey.list answered %+v, want the passkey made", got)
	}
	wantSignedIn(t, made, "bob")
	wantSignedOut(t, before)
	for _, si := range signInsOf(t, h, "bob") {
		if si.EndedAt != "" && si.EndReason != api.EndSignInLinkUsed {
			t.Errorf("bob's Sign-in %s ended %s, want %s", si.ID, si.EndReason, api.EndSignInLinkUsed)
		}
	}
	if status, body := made.Post(api.AuthLinkPath, api.AuthLinkRequest{Link: harness.LinkSecret(link)}); status != http.StatusGone {
		t.Errorf("/auth/link of the spent link answered %d %s, want link_used", status, body)
	}

	b := h.Browser()
	c := b.BeginPasskeySignIn()
	answer, err := auth.Get(c.Options)
	if err != nil {
		t.Fatal(err)
	}
	a := b.Do(http.MethodPost, api.AuthPasskeySignInPath, fmt.Sprintf(`{"flow": %q, "credential": %s}`, c.Flow, answer))
	if a.StatusCode != http.StatusOK || !strings.Contains(a.Body, `"name":"bob"`) {
		t.Fatalf("/auth/passkey/signin answered %s %s", a.Status, a.Body)
	}
	if cookie := a.Header.Get("Set-Cookie"); !signInCookie.MatchString(cookie) {
		t.Errorf("Set-Cookie = %q, want __Host-koko, Secure, HttpOnly, SameSite=Lax, Path=/ and 30 days", cookie)
	}
	wantRefusalHeadersBut(t, a.Header)
	_, body := current(b)
	var cur api.UserCurrentResponse
	if err := json.Unmarshal([]byte(body), &cur); err != nil || cur.SignIn == nil || cur.SignIn.Method != api.MethodPasskey ||
		cur.SignIn.Credential != p.ID || cur.User.Passkeys != 1 || cur.User.PasswordSetAt == "" {
		t.Errorf("user.current answered %s, want a Sign-in made with the passkey, and bob's Password and passkey", body)
	}
	if got := passkeysOf(t, h, "bob"); len(got) != 1 || got[0].LastUsedAt == nil {
		t.Errorf("passkey.list answered %+v, want it last used now", got)
	}
	passkeySignIn(t, h, auth)

	auth.Counter = harness.CounterGoesBack
	status, body := h.Browser().PasskeySignIn(auth)
	wantCode(t, status, body, api.CodePasskeyRefused.Name)
	waitLog(t, h, `level=WARN msg="passkey counter went back" user=bob passkey=`+p.ID+` address=192.0.2.10\n`)
}

// A passkey added from a Sign-in keeps it, takes the name given, and is
// added by nobody; the next creation's options exclude it, so the same
// authenticator makes no second one. A name through a Sign-in Link, or one
// out of bounds, is refused.
func TestAPasskeyAddedFromASignInKeepsIt(t *testing.T) {
	t.Parallel()
	h := startSignIns(t)
	b := h.SignIn("bob")
	auth := harness.NewAuthenticator(harness.WebURL)
	resp := addPasskey(t, b, auth, "", "  Laptop  ")
	if p := resp.Passkey; p.Name != "Laptop" || p.AddedBy != nil || p.Synced {
		t.Errorf("the passkey is %+v, want Laptop, added by nobody, not synced", p)
	}
	wantSignedIn(t, b, "bob")
	if len(signInsOf(t, h, "bob")) != 1 {
		t.Error("adding a passkey from a Sign-in started another")
	}
	c := b.BeginPasskeyAdd("")
	if _, err := auth.Create(c.Options); err == nil || !strings.Contains(err.Error(), "excluded") {
		t.Errorf("the authenticator made a second passkey (%v), want the first excluded", err)
	}

	status, body := b.AddPasskey(harness.NewAuthenticator(harness.WebURL), "", strings.Repeat("n", api.MaxPasskeyNameLength+1))
	wantCode(t, status, body, api.CodeInvalidName.Name)
	status, body = h.Browser().AddPasskey(harness.NewAuthenticator(harness.WebURL), passwordLink(t, h, "carol"), "Phone")
	wantCode(t, status, body, api.CodeInvalidRequest.Name)
	status, body = h.Browser().Post(api.AuthPasskeyAddBeginPath, api.AuthPasskeyAddBeginRequest{})
	wantCode(t, status, body, api.CodeSignInRequired.Name)
}

// Every refusal of a passkey's answer is passkey_refused, the same bytes
// whether the flow was never begun, used, expired, of the other ceremony,
// or forgotten at a restart, the passkey is unknown, or the answer fails a
// check of the verifier.
func TestEveryPasskeyRefusalIsAnsweredAlike(t *testing.T) {
	t.Parallel()
	dir, keyFile := t.TempDir(), harness.NewRootKeyFile(t)
	opts := []harness.Option{harness.DataDir(dir), harness.RootKeyFile(keyFile), harness.WebListen("127.0.0.1:0")}
	h := harness.Start(t, opts...)
	h.Browser() // sets the Web URL
	h.Koko("user", "invite", "bob").WantSuccess(t)
	auth := harness.NewAuthenticator(harness.WebURL)
	addPasskey(t, h.SignIn("bob"), auth, "", "Laptop")

	var answers []string
	refuse := func(name string) func(int, string) {
		return func(status int, body string) {
			t.Helper()
			wantCode(t, status, body, api.CodePasskeyRefused.Name)
			if len(answers) > 0 && body != answers[0] {
				t.Errorf("%s answered %s, want the same bytes as %s", name, body, answers[0])
			}
			answers = append(answers, body)
		}
	}
	// Each refusal comes from an address of its own, so that the address's
	// limit refuses none of them.
	addresses := 0
	fresh := func() *harness.Browser {
		b := h.Browser()
		addresses++
		b.Address = fmt.Sprintf("192.0.2.%d", 100+addresses)
		return b
	}
	signInWith := func(c harness.Ceremony, a *harness.Authenticator) (int, string) {
		t.Helper()
		return fresh().Answer(api.AuthPasskeySignInPath, c, mustGet(t, a, c), "")
	}
	b := fresh()

	c := b.BeginPasskeySignIn()
	refuse("a flow never begun")(signInWith(harness.Ceremony{Flow: "AAAAAAAAAAAAAAAAAAAAAA", Options: c.Options}, auth))
	answer := mustGet(t, auth, c)
	if status, body := b.Answer(api.AuthPasskeySignInPath, c, answer, ""); status != http.StatusOK {
		t.Fatalf("signing in answered %d %s", status, body)
	}
	refuse("a flow replayed")(fresh().Answer(api.AuthPasskeySignInPath, c, answer, ""))
	// The first answer that names a flow takes it, verified or not: a good
	// one after it finds none.
	c = b.BeginPasskeySignIn()
	answer = mustGet(t, auth, c)
	refuse("a spoiled answer")(fresh().Answer(api.AuthPasskeySignInPath, c, []byte(`{}`), ""))
	refuse("a flow taken already")(fresh().Answer(api.AuthPasskeySignInPath, c, answer, ""))
	c = b.BeginPasskeySignIn()
	h.Advance(5 * time.Minute)
	refuse("an expired flow")(signInWith(c, auth))
	// An add flow's challenge, signed as a sign-in's, is refused for the
	// flow's purpose alone.
	add := h.SignIn("bob").BeginPasskeyAdd("")
	var made api.PasskeyCreationOptions
	if err := json.Unmarshal(add.Options, &made); err != nil {
		t.Fatal(err)
	}
	asked, err := json.Marshal(api.PasskeyRequestOptions{Challenge: made.Challenge, RPID: made.RP.ID, AllowCredentials: []api.PasskeyDescriptor{}})
	if err != nil {
		t.Fatal(err)
	}
	refuse("an add flow")(signInWith(harness.Ceremony{Flow: add.Flow, Options: asked}, auth))

	stranger := harness.NewAuthenticator(harness.WebURL)
	if _, err := stranger.Create(h.SignIn("bob").BeginPasskeyAdd("").Options); err != nil {
		t.Fatal(err)
	}
	refuse("an unknown passkey")(signInWith(b.BeginPasskeySignIn(), stranger))
	for name, spoil := range map[string]func(*harness.Authenticator){
		"no user verification": func(a *harness.Authenticator) { a.SkipUV = true },
		"the wrong type":       func(a *harness.Authenticator) { a.Type = "webauthn.create" },
		"a cross-origin call":  func(a *harness.Authenticator) { a.CrossOrigin = true },
		"another challenge": func(a *harness.Authenticator) {
			a.Tamper = func(u *harness.Unsigned) {
				u.ClientDataJSON = []byte(strings.Replace(string(u.ClientDataJSON), `"challenge":"`, `"challenge":"A`, 1))
			}
		},
	} {
		spoiled := *auth
		spoil(&spoiled)
		refuse(name)(signInWith(b.BeginPasskeySignIn(), &spoiled))
	}
	notOne := fresh().Do(http.MethodPost, api.AuthPasskeySignInPath, fmt.Sprintf(`{"flow": %q, "credential": 42}`, b.BeginPasskeySignIn().Flow))
	refuse("an answer that is not one")(notOne.StatusCode, notOne.Body)

	c = b.BeginPasskeySignIn()
	h.Stop()
	h = harness.Start(t, append(opts, harness.NoInit())...)
	b = h.Browser()
	refuse("a flow begun before a restart")(b.Answer(api.AuthPasskeySignInPath, c, mustGet(t, auth, c), ""))
	if status, body := b.PasskeySignIn(auth); status != http.StatusOK {
		t.Errorf("signing in after the restart answered %d %s", status, body)
	}
}

// mustGet is auth's answer to c's options, failing the test without one.
func mustGet(t *testing.T, auth *harness.Authenticator, c harness.Ceremony) []byte {
	t.Helper()
	answer, err := auth.Get(c.Options)
	if err != nil {
		t.Fatal(err)
	}
	return answer
}

// A begin, a ceremony the person cancels, or an answer refused spends no
// Sign-in Link: it still makes a passkey afterwards.
func TestACancelledCeremonySpendsNoLink(t *testing.T) {
	t.Parallel()
	h := startSignIns(t)
	link := passwordLink(t, h, "bob")
	b := h.Browser()
	b.BeginPasskeyAdd(link)
	b.BeginPasskeyAdd(link)
	refused := harness.NewAuthenticator(harness.WebURL)
	refused.SkipUV = true
	status, body := b.AddPasskey(refused, link, "")
	wantCode(t, status, body, api.CodePasskeyRefused.Name)
	if got := linkFor(t, b, link); got.User != "bob" || got.Passkeys != 0 {
		t.Errorf("/auth/link answered %+v, want bob's link unspent and no passkey", got)
	}
	addPasskey(t, b, harness.NewAuthenticator(harness.WebURL), link, "")
	if got := linkFor(t, h.Browser(), passwordLink(t, h, "bob")); got.Passkeys != 1 {
		t.Errorf("/auth/link answered %+v, want one passkey", got)
	}
}

// A Sign-in Link spent, or voided, between a begin and its answer is
// refused as the link would be, and a Sign-in ended meanwhile as signed
// out; neither records a passkey.
func TestAnAddWhoseLinkOrSignInEndedMeanwhileIsRefused(t *testing.T) {
	t.Parallel()
	h := startSignIns(t)
	link := passwordLink(t, h, "bob")
	b := h.Browser()
	auth := harness.NewAuthenticator(harness.WebURL)
	c := b.BeginPasskeyAdd(link)
	if status, body := setPassword(h.Browser(), link, harness.Password); status != http.StatusOK {
		t.Fatal(status, body)
	}
	answer, err := auth.Create(c.Options)
	if err != nil {
		t.Fatal(err)
	}
	status, body := b.Answer(api.AuthPasskeyAddPath, c, answer, "")
	wantCode(t, status, body, api.CodeLinkUsed.Name)

	carol := h.SignIn("carol")
	c = carol.BeginPasskeyAdd("")
	signOut(t, carol)
	answer, err = harness.NewAuthenticator(harness.WebURL).Create(c.Options)
	if err != nil {
		t.Fatal(err)
	}
	status, body = carol.Answer(api.AuthPasskeyAddPath, c, answer, "")
	wantCode(t, status, body, api.CodeSignInRequired.Name)
	if got := passkeysOf(t, h, "bob"); len(got) != 0 {
		t.Errorf("bob has %+v, want no passkey", got)
	}
	if got := passkeysOf(t, h, "carol"); len(got) != 0 {
		t.Errorf("carol has %+v, want no passkey", got)
	}
}

// Removing a passkey ends the Sign-ins made with it and no others, and
// says how many; the browser may still offer it, and it is refused.
func TestRemovingAPasskeyEndsOnlyItsSignIns(t *testing.T) {
	t.Parallel()
	h := startSignIns(t)
	withPassword := h.SignIn("bob")
	laptop, phone := harness.NewAuthenticator(harness.WebURL), harness.NewAuthenticator(harness.WebURL)
	removed := addPasskey(t, withPassword, laptop, "", "Laptop").Passkey
	addPasskey(t, withPassword, phone, "", "Phone")
	withLaptop, alsoLaptop, withPhone := passkeySignIn(t, h, laptop), passkeySignIn(t, h, laptop), passkeySignIn(t, h, phone)

	resp, err := h.Call(api.PasskeyRemove, api.PasskeyRemoveRequest{ID: removed.ID})
	if err != nil || resp.SignInsEnded != 2 || resp.Passkey.ID != removed.ID {
		t.Errorf("passkey.remove answered %+v %v, want the Laptop and 2 Sign-ins ended", resp, err)
	}
	wantSignedOut(t, withLaptop)
	wantSignedOut(t, alsoLaptop)
	wantSignedIn(t, withPassword, "bob")
	wantSignedIn(t, withPhone, "bob")
	for _, si := range signInsOf(t, h, "bob") {
		if si.EndedAt != "" && si.EndReason != api.EndPasskeyRemoved {
			t.Errorf("bob's Sign-in %s ended %s, want %s", si.ID, si.EndReason, api.EndPasskeyRemoved)
		}
	}
	status, body := h.Browser().PasskeySignIn(laptop)
	wantCode(t, status, body, api.CodePasskeyRefused.Name)
	status, sent := withPhone.Send(api.PasskeyRemove, []byte(`{"id": "`+removed.ID+`"}`))
	wantCode(t, status, string(sent), api.CodeUnknownPasskey.Name)
}

// Removing a passkey ends the SSH Sessions its Sign-ins opened, with their
// notice, and keeps those another Sign-in opened.
func TestRemovingAPasskeyEndsTheTerminalsItsSignInsOpened(t *testing.T) {
	t.Parallel()
	h := startTerminals(t)
	terminalHost(t, h, "web1", []string{"space=bob"})
	desktop := h.SignIn("bob")
	auth := harness.NewAuthenticator(harness.WebURL)
	p := addPasskey(t, desktop, auth, "", "Phone").Passkey
	phone := passkeySignIn(t, h, auth)
	phoneTab := openCat(t, phone.TerminalPage(), "web1")
	desktopTab := openCat(t, desktop.TerminalPage(), "web1")

	if _, err := h.Call(api.PasskeyRemove, api.PasskeyRemoveRequest{ID: p.ID}); err != nil {
		t.Fatal(err)
	}
	wantEnded(t, h, phoneTab, "access to web1 was withdrawn: the passkey was removed; this SSH Session is closed.", "passkey removed")
	wantKept(t, desktopTab, "still here")
}

// A passkey belongs to the host it was made for: when the Web URL names
// another host it stays listed, set says how many such there are, and it
// is refused; a change of port alone keeps it, and the old host put back
// restores it.
func TestAPasskeyBelongsToItsHost(t *testing.T) {
	t.Parallel()
	h := startSignIns(t)
	auth := harness.NewAuthenticator(harness.WebURL)
	addPasskey(t, h.SignIn("bob"), auth, "", "Laptop")
	// h.Browser sets the Web URL to harness.WebURL: these browse where the
	// test sets it after.
	moved, other := h.Browser(), h.Browser()
	moved.Origin, other.Origin = "https://koko.example.com:8443", "https://other.example.com"
	setWebURL := func(webURL string) int {
		t.Helper()
		resp, err := h.Call(api.ServerSet, api.ServerSetRequest{WebURL: new(webURL)})
		if err != nil {
			t.Fatalf("set the Web URL to %s: %v", webURL, err)
		}
		return resp.OtherHostPasskeys
	}

	if n := setWebURL("https://koko.example.com:8443"); n != 0 {
		t.Errorf("a change of port counts %d passkeys for another host, want none", n)
	}
	auth.Origin = moved.Origin
	if status, body := moved.PasskeySignIn(auth); status != http.StatusOK {
		t.Errorf("signing in after a change of port answered %d %s", status, body)
	}

	if n := setWebURL("https://other.example.com"); n != 1 {
		t.Errorf("a change of host counts %d passkeys for another host, want 1", n)
	}
	if got := passkeysOf(t, h, "bob"); len(got) != 1 || got[0].Host != "koko.example.com" {
		t.Errorf("passkey.list answered %+v, want the passkey listed for koko.example.com", got)
	}
	c := other.BeginPasskeySignIn()
	// A browser at the new host offers no passkey of the old one; a page
	// that asks for one anyway is refused.
	var options api.PasskeyRequestOptions
	if err := json.Unmarshal(c.Options, &options); err != nil || options.RPID != "other.example.com" {
		t.Fatalf("the options are %s, want the RP ID other.example.com", c.Options)
	}
	options.RPID = "koko.example.com"
	asked, err := json.Marshal(options)
	if err != nil {
		t.Fatal(err)
	}
	auth.Origin = harness.WebURL
	answer, err := auth.Get(asked)
	if err != nil {
		t.Fatal(err)
	}
	status, body := other.Answer(api.AuthPasskeySignInPath, c, answer, "")
	wantCode(t, status, body, api.CodePasskeyRefused.Name)

	if n := setWebURL(harness.WebURL); n != 0 {
		t.Errorf("the old host put back counts %d passkeys for another host, want none", n)
	}
	passkeySignIn(t, h, auth)
}

// A ceremony's flow is held per client address, at most 20, a 21st
// dropping the address's oldest, and every address of an IPv6 /64 counts as
// one; another address's flows stay.
func TestAnAddressHoldsTwentyFlows(t *testing.T) {
	t.Parallel()
	h := startSignIns(t)
	auth := harness.NewAuthenticator(harness.WebURL)
	addPasskey(t, h.SignIn("bob"), auth, "", "Laptop")
	for _, tc := range []struct{ name, first, other string }{
		{"IPv4", "192.0.2.20", "192.0.2.21"},
		{"IPv6 /64", "2001:db8:1:2::%x", "2001:db8:1:3::1"},
	} {
		other := h.Browser()
		other.Address = tc.other
		kept := other.BeginPasskeySignIn()
		var flows []harness.Ceremony
		for i := range 21 {
			b := h.Browser()
			b.Address = tc.first
			if strings.Contains(tc.first, "%") {
				b.Address = fmt.Sprintf(tc.first, i+1)
			}
			flows = append(flows, b.BeginPasskeySignIn())
		}
		b := h.Browser()
		status, body := b.Answer(api.AuthPasskeySignInPath, flows[0], mustGet(t, auth, flows[0]), "")
		wantCode(t, status, body, api.CodePasskeyRefused.Name)
		for _, c := range []harness.Ceremony{flows[1], kept} {
			if status, body := h.Browser().Answer(api.AuthPasskeySignInPath, c, mustGet(t, auth, c), ""); status != http.StatusOK {
				t.Errorf("%s: a kept flow answered %d %s", tc.name, status, body)
			}
		}
	}
}

// passkey_refused counts as a failure for the client's address: past the
// limit, the answers are refused too_many_attempts, and so is a begin of
// adding a passkey, while a sign-in's begin, which counts nothing, is not.
func TestARefusedPasskeyCountsForTheAddress(t *testing.T) {
	t.Parallel()
	h := startSignIns(t)
	b := h.Browser()
	b.Address = "192.0.2.30"
	for range 10 {
		status, body := b.Answer(api.AuthPasskeySignInPath, harness.Ceremony{Flow: "unknown"}, []byte(`{}`), "")
		wantCode(t, status, body, api.CodePasskeyRefused.Name)
	}
	status, body := b.Answer(api.AuthPasskeySignInPath, harness.Ceremony{Flow: "unknown"}, []byte(`{}`), "")
	wantCode(t, status, body, api.CodeTooManyAttempts.Name)
	status, body = b.Post(api.AuthPasskeyAddBeginPath, api.AuthPasskeyAddBeginRequest{Link: harness.LinkSecret(passwordLink(t, h, "bob"))})
	wantCode(t, status, body, api.CodeTooManyAttempts.Name)
	b.BeginPasskeySignIn()
	if status, body := h.Browser().Post(api.AuthPasskeyAddBeginPath, api.AuthPasskeyAddBeginRequest{Link: "AAAAAAAAAAAAAAAAAAAAAAAAAA"}); status != http.StatusNotFound {
		t.Errorf("another address's begin answered %d %s, want link_invalid", status, body)
	}
}

// The two endpoints that carry a passkey's answer take up to 64 KiB, and
// the rest of the passkey endpoints 4 KiB.
func TestThePasskeyAnswersTakeLargerBodies(t *testing.T) {
	t.Parallel()
	h := startSignIns(t)
	b := h.Browser()
	pad := func(n int) string {
		return fmt.Sprintf(`{"flow": "unknown", "credential": {"id": %q}}`, strings.Repeat("a", n))
	}
	for _, path := range []string{api.AuthPasskeySignInPath, api.AuthPasskeyAddPath} {
		a := b.Do(http.MethodPost, path, pad(32<<10))
		wantCode(t, a.StatusCode, a.Body, api.CodePasskeyRefused.Name)
		a = b.Do(http.MethodPost, path, pad(api.MaxPasskeyBody))
		wantCode(t, a.StatusCode, a.Body, api.CodeRequestTooLarge.Name)
	}
	a := b.Do(http.MethodPost, api.AuthPasskeyAddBeginPath, fmt.Sprintf(`{"link": %q}`, strings.Repeat("a", 8<<10)))
	wantCode(t, a.StatusCode, a.Body, api.CodeRequestTooLarge.Name)
}

// /auth/methods names the Identity Provider's button, null while none is
// configured, to any page.
func TestAuthMethodsNamesTheProvidersButton(t *testing.T) {
	t.Parallel()
	h := startSignIns(t)
	status, body := h.Browser().Post(api.AuthMethodsPath, api.AuthMethodsRequest{})
	if status != http.StatusOK || body != `{"provider":null}` {
		t.Errorf("/auth/methods answered %d %s, want no provider", status, body)
	}
}

// Removing a User deletes their passkeys, and user.list counts each User's.
func TestRemovingAUserDeletesTheirPasskeys(t *testing.T) {
	t.Parallel()
	h := startSignIns(t)
	b := h.SignIn("bob")
	addPasskey(t, b, harness.NewAuthenticator(harness.WebURL), "", "Laptop")
	addPasskey(t, b, harness.NewAuthenticator(harness.WebURL), "", "Phone")
	resp, err := h.Call(api.UserList, api.UserListRequest{})
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range resp.Users {
		if want := map[string]int{"bob": 2}[u.Name]; u.Passkeys != want || u.IdentityLinkedAt != "" {
			t.Errorf("user.list answered %s with %d passkeys and identity %q, want %d and none", u.Name, u.Passkeys, u.IdentityLinkedAt, want)
		}
	}
	h.Koko("user", "remove", "bob").WantSuccess(t)
	if got := passkeysOf(t, h, "bob"); len(got) != 0 {
		t.Errorf("Removed bob has %+v, want no passkey", got)
	}
}

// The passkey log lines name the User, the passkey and the address, and no
// line, answer or error holds a challenge, a flow, client data, an
// attestation, an assertion or a signature.
func TestThePasskeyLogLines(t *testing.T) {
	t.Parallel()
	h := startSignIns(t)
	var secrets, answers []string
	keepOptions := func(c harness.Ceremony) {
		var o struct {
			Challenge string `json:"challenge"`
		}
		if err := json.Unmarshal(c.Options, &o); err != nil {
			t.Fatal(err)
		}
		secrets = append(secrets, c.Flow, o.Challenge)
	}
	keepAnswer := func(answer []byte) {
		var a struct {
			Response map[string]jsontext.Value `json:"response"`
		}
		if err := json.Unmarshal(answer, &a); err != nil {
			t.Fatal(err)
		}
		for name, v := range a.Response {
			if name != "transports" && name != "userHandle" {
				secrets = append(secrets, strings.Trim(string(v), `"`))
			}
		}
	}
	auth := harness.NewAuthenticator(harness.WebURL)
	link := passwordLink(t, h, "bob")
	b := h.Browser()
	c := b.BeginPasskeyAdd(link)
	keepOptions(c)
	answer, err := auth.Create(c.Options)
	if err != nil {
		t.Fatal(err)
	}
	keepAnswer(answer)
	status, body := b.Answer(api.AuthPasskeyAddPath, c, answer, "")
	answers = append(answers, body)
	var added api.AuthPasskeyAddResponse
	if err := json.Unmarshal([]byte(body), &added); status != http.StatusOK || err != nil {
		t.Fatal(status, body)
	}
	for _, spoil := range []bool{false, true} {
		auth.SkipUV = spoil
		c := b.BeginPasskeySignIn()
		keepOptions(c)
		answer := mustGet(t, auth, c)
		keepAnswer(answer)
		_, body := b.Answer(api.AuthPasskeySignInPath, c, answer, "")
		answers = append(answers, body)
	}
	if _, err := h.Call(api.PasskeyRemove, api.PasskeyRemoveRequest{ID: added.Passkey.ID}); err != nil {
		t.Fatal(err)
	}

	log := h.Log()
	id := added.Passkey.ID
	for _, want := range []string{
		`msg="passkey added" user=bob passkey=` + id + ` name="Firefox on Linux" host=koko.example.com added_with=passwordlink_`,
		`msg="signed in" user=bob signin=signin_`,
		` method=passkey passkey=` + id + ` client="Firefox on Linux" address=192.0.2.10`,
		`msg="sign-in refused" reason=passkey_refused ceremony=signin address=192.0.2.10 user=bob`,
		`msg="sign-in ended" user=bob signin=signin_`,
		` reason=passkey_removed ended_by="unix socket"`,
		`msg="passkey removed" user=bob passkey=` + id + ` name="Firefox on Linux" removed_by="unix socket" signins_ended=2`,
	} {
		if !strings.Contains(log, want) {
			t.Errorf("the log does not say %q", want)
		}
	}
	for _, secret := range append(secrets, harness.LinkSecret(link), b.Cookie) {
		if secret == "" {
			continue
		}
		if strings.Contains(log, secret) {
			t.Errorf("the log holds %q", secret)
		}
		for _, answer := range answers {
			if strings.Contains(answer, secret) {
				t.Errorf("the answer %s holds %q", answer, secret)
			}
		}
	}
	if t.Failed() {
		t.Log(log)
	}
}

// A new passkey whose credential id a passkey holds already, another
// User's here, is refused, and nothing is recorded: an answer that claims
// someone else's passkey takes nothing over.
func TestACredentialIDAPasskeyHoldsIsRefused(t *testing.T) {
	t.Parallel()
	h := startSignIns(t)
	bob := h.SignIn("bob")
	c := bob.BeginPasskeyAdd("")
	answer, err := harness.NewAuthenticator(harness.WebURL).Create(c.Options)
	if err != nil {
		t.Fatal(err)
	}
	if status, body := bob.Answer(api.AuthPasskeyAddPath, c, answer, "Laptop"); status != http.StatusOK {
		t.Fatal(status, body)
	}
	var held struct {
		RawID string `json:"rawId"`
	}
	if err := json.Unmarshal(answer, &held); err != nil {
		t.Fatal(err)
	}

	carol := h.SignIn("carol")
	c = carol.BeginPasskeyAdd("")
	answer, err = harness.NewAuthenticator(harness.WebURL).Create(c.Options)
	if err != nil {
		t.Fatal(err)
	}
	status, body := carol.Answer(api.AuthPasskeyAddPath, c, claim(t, answer, held.RawID), "Laptop")
	wantCode(t, status, body, api.CodePasskeyRefused.Name)
	if got := passkeysOf(t, h, "carol"); len(got) != 0 {
		t.Errorf("carol has %+v, want no passkey", got)
	}
}

// claim rewrites a creation's answer to carry the credential id rawID, of
// the same length, in place of its own: in its id, its rawId and its
// authenticator data, which a `none` attestation signs nothing over.
func claim(t *testing.T, answer []byte, rawID string) []byte {
	t.Helper()
	var a map[string]any
	if err := json.Unmarshal(answer, &a); err != nil {
		t.Fatal(err)
	}
	b64 := base64.RawURLEncoding
	own, err := b64.DecodeString(a["rawId"].(string))
	if err != nil {
		t.Fatal(err)
	}
	other, err := b64.DecodeString(rawID)
	if err != nil || len(other) != len(own) {
		t.Fatalf("the credential ids differ in length: %d and %d", len(own), len(other))
	}
	response := a["response"].(map[string]any)
	obj, err := b64.DecodeString(response["attestationObject"].(string))
	if err != nil {
		t.Fatal(err)
	}
	response["attestationObject"] = b64.EncodeToString(bytes.Replace(obj, own, other, 1))
	a["id"], a["rawId"] = rawID, rawID
	claimed, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	return claimed
}
