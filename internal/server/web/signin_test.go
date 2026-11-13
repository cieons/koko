package web_test

import (
	"encoding/json/v2"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/shared/api"
)

// startSignIns starts a koko server with its web listener on and its Web
// URL set to harness.WebURL, and invites the Members bob, carol and dave.
func startSignIns(t *testing.T) *harness.Harness {
	t.Helper()
	h := harness.Start(t, harness.WebListen("127.0.0.1:0"))
	h.Browser() // sets the Web URL
	for _, u := range []string{"bob", "carol", "dave"} {
		h.Koko("user", "invite", u).WantSuccess(t)
	}
	return h
}

// signIn posts a sign-in as name with password from b.
func signIn(b *harness.Browser, name, password string) (int, string) {
	return b.Post(api.AuthSignInPath, api.AuthSignInRequest{Name: name, Password: password})
}

// wantCode fails the test unless status and body are the API's error
// object with code, at its status.
func wantCode(t *testing.T, status int, body, code string) {
	t.Helper()
	c, ok := api.LookupCode(code)
	if !ok {
		t.Fatalf("no code %s is registered", code)
	}
	var e api.ErrorResponse
	if err := json.Unmarshal([]byte(body), &e); err != nil || status != c.Status || e.Error.Code != code {
		t.Errorf("answered %d %s, want %d with the code %s", status, body, c.Status, code)
	}
}

// current calls user.current from b and returns the status and body.
func current(b *harness.Browser) (int, string) {
	status, body := b.Send(api.UserCurrent, []byte("{}"))
	return status, string(body)
}

// wantSignedIn fails the test unless b's calls are answered as user's.
func wantSignedIn(t *testing.T, b *harness.Browser, user string) {
	t.Helper()
	status, body := current(b)
	var resp api.UserCurrentResponse
	if err := json.Unmarshal([]byte(body), &resp); status != http.StatusOK || err != nil || resp.User.Name != user || resp.SignIn == nil {
		t.Errorf("user.current answered %d %s, want %s through a Sign-in", status, body, user)
	}
}

// wantSignedOut fails the test unless b's calls are refused signin_required.
func wantSignedOut(t *testing.T, b *harness.Browser) {
	t.Helper()
	status, body := current(b)
	wantCode(t, status, body, api.CodeSignInRequired.Name)
}

// inviteForWeb invites user with --web and returns their Password Link.
func inviteForWeb(t *testing.T, h *harness.Harness, user string, args ...string) string {
	t.Helper()
	r := h.Koko(append([]string{"user", "invite", user, "--web", "--json"}, args...)...)
	r.WantSuccess(t)
	var out struct {
		PasswordLink string `json:"password_link"`
	}
	if err := json.Unmarshal([]byte(r.Stdout), &out); err != nil {
		t.Fatalf("user invite --web printed %q: %v", r.Stdout, err)
	}
	return out.PasswordLink
}

// passwordLink issues a Password Link for user and returns it.
func passwordLink(t *testing.T, h *harness.Harness, user string) string {
	t.Helper()
	resp, err := h.Call(api.PasswordLinkIssue, api.PasswordLinkRequest{User: user})
	if err != nil {
		t.Fatalf("password.link for %s: %v", user, err)
	}
	return resp.PasswordLink.Link
}

// setPassword spends link from b, setting password.
func setPassword(b *harness.Browser, link, password string) (int, string) {
	return b.Post(api.AuthPasswordPath, api.AuthPasswordRequest{Link: harness.LinkSecret(link), Password: password})
}

// signInID returns the id of the one live Sign-in of user.
func signInID(t *testing.T, h *harness.Harness, user string) string {
	t.Helper()
	resp, err := h.Call(api.SignInList, api.SignInListRequest{User: user})
	if err != nil || len(resp.SignIns) != 1 {
		t.Fatalf("signin.list of %s: %v %+v, want one Sign-in", user, err, resp)
	}
	return resp.SignIns[0].ID
}

// An invited person opens their Password Link: /auth/link names them
// without spending it, /auth/password sets the Password and signs the
// browser in with the __Host-koko cookie, and they sign in again with
// their name and Password. Every /auth/ answer carries the security headers
// and no-store.
func TestAPasswordLinkSetsThePasswordAndSignsIn(t *testing.T) {
	t.Parallel()
	h := startSignIns(t)
	link := inviteForWeb(t, h, "erin")
	if !regexp.MustCompile(`^https://koko\.example\.com/password#[A-Z2-7]{26}$`).MatchString(link) {
		t.Fatalf("the Password Link is %q, want the Web URL, /password and a 26-character secret after #", link)
	}
	b := h.Browser()
	b.UserAgent = "Mozilla/5.0 (iPhone; CPU iPhone OS 26_1 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/26.1 Mobile/15E148 Safari/604.1"
	for range 2 {
		status, body := b.Post(api.AuthLinkPath, api.AuthLinkRequest{Link: harness.LinkSecret(link)})
		var resp api.AuthLinkResponse
		if err := json.Unmarshal([]byte(body), &resp); status != http.StatusOK || err != nil || resp.User != "erin" || resp.ExpiresAt == "" {
			t.Fatalf("/auth/link answered %d %s, want erin, spending nothing", status, body)
		}
	}
	a := b.Do(http.MethodPost, api.AuthPasswordPath, fmt.Sprintf(`{"link": %q, "password": %q}`, harness.LinkSecret(link), harness.Password))
	if a.StatusCode != http.StatusOK || !strings.Contains(a.Body, `"name":"erin"`) {
		t.Fatalf("/auth/password answered %s %s", a.Status, a.Body)
	}
	cookie := a.Header.Get("Set-Cookie")
	want := regexp.MustCompile(`^__Host-koko=[A-Za-z0-9_-]{43}; Path=/; Max-Age=2592000; HttpOnly; Secure; SameSite=Lax$`)
	if !want.MatchString(cookie) {
		t.Errorf("Set-Cookie = %q, want __Host-koko, Secure, HttpOnly, SameSite=Lax, Path=/ and 30 days", cookie)
	}
	wantRefusalHeadersBut(t, a.Header)
	status, body := current(b)
	var cur api.UserCurrentResponse
	if err := json.Unmarshal([]byte(body), &cur); status != http.StatusOK || err != nil || cur.SignIn == nil || cur.DeviceSession != nil ||
		cur.SignIn.Client != "Safari on iPhone" || cur.SignIn.Browser != "Safari" || cur.SignIn.System != "iPhone" {
		t.Errorf("user.current answered %d %s, want erin's Sign-in from Safari on iPhone, its browser and system apart", status, body)
	}
	listed, err := h.Call(api.SignInList, api.SignInListRequest{User: "erin"})
	if err != nil || len(listed.SignIns) != 1 || listed.SignIns[0].Client != "Safari on iPhone" ||
		listed.SignIns[0].Browser != "Safari" || listed.SignIns[0].System != "iPhone" {
		t.Errorf("signin.list of erin: %v %+v, want one Sign-in from Safari on iPhone, its browser and system apart", err, listed)
	}

	again := h.Browser()
	if status, body := signIn(again, "erin", harness.Password); status != http.StatusOK || again.Cookie == "" {
		t.Errorf("signing in with the Password answered %d %s", status, body)
	}
	wantSignedIn(t, again, "erin")
	wantSignedIn(t, b, "erin")
}

// wantRefusalHeadersBut fails the test unless h, an /auth/ answer, carries
// the security headers and no-store.
func wantRefusalHeadersBut(t *testing.T, h http.Header) {
	t.Helper()
	wantSecurityHeaders(t, h)
	if cc := h.Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", cc)
	}
}

// A wrong Password, a name no User holds, a User without a Password and a
// Removed User's correct Password are answered the same bytes.
func TestAWrongNameOrPasswordIsAnsweredAlike(t *testing.T) {
	t.Parallel()
	h := startSignIns(t)
	h.SignIn("bob")
	h.SignIn("dave")
	h.Koko("user", "remove", "dave").WantSuccess(t)
	var first string
	for i, c := range []struct{ name, password string }{
		{"bob", "not bob's password at all"},
		{"nobody", harness.Password},
		{"carol", harness.Password},
		{"dave", harness.Password},
		{"Not A Name", harness.Password},
	} {
		b := h.Browser()
		b.Address = fmt.Sprintf("198.51.100.%d", i+1)
		status, body := signIn(b, c.name, c.password)
		wantCode(t, status, body, api.CodeWrongPassword.Name)
		if first == "" {
			first = body
		} else if body != first {
			t.Errorf("signing in as %s answered %q, want the same bytes as %q", c.name, body, first)
		}
		if b.Cookie != "" {
			t.Errorf("signing in as %s set a cookie", c.name)
		}
	}
}

// Ten failed attempts from one address within 15 minutes, of each kind (a
// wrong sign-in, a refused link, a wrong current Password), and its next
// attempts are answered too_many_attempts with Retry-After, a correct
// Password included, until the oldest failure is 15 minutes old.
func TestTenFailuresFromOneAddressWaitFifteenMinutes(t *testing.T) {
	t.Parallel()
	h := startSignIns(t)
	b := h.SignIn("bob")
	b.Address = "198.51.100.7"
	for i := range 4 {
		status, body := signIn(b, fmt.Sprintf("name%d", i), harness.Password)
		wantCode(t, status, body, api.CodeWrongPassword.Name)
	}
	for range 3 {
		status, body := b.Post(api.AuthLinkPath, api.AuthLinkRequest{Link: "AAAAAAAAAAAAAAAAAAAAAAAAAA"})
		wantCode(t, status, body, api.CodeLinkInvalid.Name)
	}
	h.Advance(time.Minute)
	for range 3 {
		status, body := b.Post(api.AuthPasswordPath, api.AuthPasswordRequest{Current: "not the current password", Password: "a new password, long enough"})
		wantCode(t, status, body, api.CodeWrongPassword.Name)
	}
	a := b.Do(http.MethodPost, api.AuthSignInPath, fmt.Sprintf(`{"name": "bob", "password": %q}`, harness.Password))
	wantCode(t, a.StatusCode, a.Body, api.CodeTooManyAttempts.Name)
	if got := a.Header.Get("Retry-After"); got != "840" {
		t.Errorf("Retry-After = %q, want 840 seconds: until the first failure is 15 minutes old", got)
	}
	wantSignedIn(t, b, "bob")
	other := h.Browser()
	other.Address = "198.51.100.8"
	if status, body := signIn(other, "bob", harness.Password); status != http.StatusOK {
		t.Errorf("another address was refused: %d %s", status, body)
	}

	h.Advance(14 * time.Minute)
	if status, body := signIn(b, "bob", harness.Password); status != http.StatusOK {
		t.Errorf("15 minutes after the first failure, signing in answered %d %s", status, body)
	}
}

// Ten failed sign-ins in a row with one name, from ten addresses, lock it
// for 15 minutes from any address, a correct Password included, while the
// Sign-ins already open keep working. A name no User holds is counted the
// same. A successful sign-in starts the count again, and so does a Password
// Link.
func TestTenFailuresForOneNameLockIt(t *testing.T) {
	t.Parallel()
	h := startSignIns(t)
	open := h.SignIn("bob")
	fail := func(name string, n int) {
		t.Helper()
		for i := range n {
			b := h.Browser()
			b.Address = fmt.Sprintf("203.0.113.%d", i+1)
			status, body := signIn(b, name, "a wrong password for "+name)
			wantCode(t, status, body, api.CodeWrongPassword.Name)
		}
	}
	at := func(address string) *harness.Browser {
		b := h.Browser()
		b.Address = address
		return b
	}

	fail("bob", 9)
	if status, body := signIn(at("192.0.2.50"), "bob", harness.Password); status != http.StatusOK {
		t.Fatalf("after 9 failures, the correct Password answered %d %s", status, body)
	}
	fail("bob", 9)
	if status, body := signIn(at("192.0.2.51"), "bob", harness.Password); status != http.StatusOK {
		t.Fatalf("a successful sign-in did not start the count again: %d %s", status, body)
	}

	fail("bob", 10)
	status, body := signIn(at("192.0.2.52"), "bob", harness.Password)
	wantCode(t, status, body, api.CodeTooManyAttempts.Name)
	wantSignedIn(t, open, "bob")
	fail("nobody", 10)
	_, unknown := signIn(at("192.0.2.53"), "nobody", harness.Password)
	if unknown != body {
		t.Errorf("a locked name no User holds answered %q, want %q as for bob", unknown, body)
	}

	h.Advance(15 * time.Minute)
	if status, body := signIn(at("192.0.2.54"), "bob", harness.Password); status != http.StatusOK {
		t.Fatalf("15 minutes after the lockout, the correct Password answered %d %s", status, body)
	}

	fail("bob", 10)
	if status, body := setPassword(at("192.0.2.55"), passwordLink(t, h, "bob"), "the second password of bob"); status != http.StatusOK {
		t.Fatalf("the Password Link answered %d %s", status, body)
	}
	if status, body := signIn(at("192.0.2.56"), "bob", "the second password of bob"); status != http.StatusOK {
		t.Errorf("a Password Link did not end the lockout: %d %s", status, body)
	}
}

// A Sign-in ends 7 days after it was last used, and 30 days after it began
// however often it is used; using it moves Last used at most once a minute.
func TestASignInEndsSevenDaysUnusedOrThirtyDaysOn(t *testing.T) {
	t.Parallel()
	h := startSignIns(t)
	started := h.Now().UTC().Format(time.RFC3339)
	kept := h.SignIn("bob")
	idle := h.SignIn("carol")
	lastUsed := func(user string) string {
		t.Helper()
		resp, err := h.Call(api.SignInList, api.SignInListRequest{User: user})
		if err != nil || len(resp.SignIns) != 1 {
			t.Fatalf("signin.list: %v %+v", err, resp)
		}
		return resp.SignIns[0].LastUsedAt
	}

	h.Advance(30 * time.Second)
	wantSignedIn(t, kept, "bob")
	if got := lastUsed("bob"); got != started {
		t.Errorf("30 s on, Last used is %s, want %s: it moves at most once a minute", got, started)
	}
	h.Advance(31 * time.Second)
	wantSignedIn(t, kept, "bob")
	if got, want := lastUsed("bob"), h.Now().UTC().Format(time.RFC3339); got != want {
		t.Errorf("61 s on, Last used is %s, want %s", got, want)
	}

	// 7 days after both began: carol's, unused since, has ended; bob's,
	// used 61 s after it began, has not.
	h.Advance(7*24*time.Hour - 61*time.Second)
	wantSignedOut(t, idle)
	wantSignedIn(t, kept, "bob")
	for range 3 {
		h.Advance(6 * 24 * time.Hour)
		wantSignedIn(t, kept, "bob")
	}
	// 25 days after it began, and used then: it lasts until 30 days.
	h.Advance(5*24*time.Hour - time.Second)
	wantSignedIn(t, kept, "bob")
	h.Advance(time.Second)
	wantSignedOut(t, kept)
	resp, err := h.Call(api.SignInList, api.SignInListRequest{All: true})
	if err != nil || len(resp.SignIns) != 2 {
		t.Fatalf("signin.list --all: %v %+v", err, resp)
	}
	for _, s := range resp.SignIns {
		if s.EndReason != api.EndExpired {
			t.Errorf("%s's Sign-in ended %q, want expired", s.User, s.EndReason)
		}
	}
	if !strings.Contains(h.Log(), `msg="sign-in ended" user=bob signin=`+resp.SignIns[0].ID+" reason=expired") {
		t.Errorf("the log does not say bob's Sign-in expired:\n%s", h.Log())
	}
}

// A Sign-in ends when its browser signs out, when it is revoked by its User
// or the Owner, when the Password is changed (but for the Sign-in that
// changed it) or removed, and when the User is Removed; each is refused
// signin_required on its next call. A Member cannot revoke another's.
func TestASignInEnds(t *testing.T) {
	t.Parallel()
	h := startSignIns(t)

	out := h.SignIn("bob")
	a := out.Do(http.MethodPost, api.AuthSignOutPath, "{}")
	if a.StatusCode != http.StatusOK || !strings.Contains(a.Header.Get("Set-Cookie"), "__Host-koko=; Path=/; Max-Age=0") {
		t.Errorf("/auth/signout answered %s, Set-Cookie %q, want the cookie cleared", a.Status, a.Header.Get("Set-Cookie"))
	}
	wantSignedOut(t, out)
	if a := out.Do(http.MethodPost, api.AuthSignOutPath, "{}"); a.StatusCode != http.StatusOK {
		t.Errorf("signing out without a cookie answered %s", a.Status)
	}

	own := h.SignIn("bob")
	status, body := own.Send(api.SignInRevoke, fmt.Appendf(nil, `{"id": %q}`, signInID(t, h, "bob")))
	if status != http.StatusOK {
		t.Fatalf("bob revoking his own Sign-in: %d %s", status, body)
	}
	wantSignedOut(t, own)

	bob, carol := h.SignIn("bob"), h.SignIn("carol")
	status, body = bob.Send(api.SignInRevoke, fmt.Appendf(nil, `{"id": %q}`, signInID(t, h, "carol")))
	wantCode(t, status, string(body), api.CodeUnknownSignIn.Name)
	_, unknown := bob.Send(api.SignInRevoke, []byte(`{"id": "signin_aaaaaaaa"}`))
	if string(unknown) != strings.ReplaceAll(string(body), signInID(t, h, "carol"), "signin_aaaaaaaa") {
		t.Errorf("another's Sign-in answered %s, want it as an id no Sign-in holds: %s", body, unknown)
	}
	wantSignedIn(t, carol, "carol")
	h.Koko("signin", "revoke", signInID(t, h, "carol")).WantSuccess(t)
	wantSignedOut(t, carol)

	other := h.Browser()
	if status, body := signIn(other, "bob", harness.Password); status != http.StatusOK {
		t.Fatal(status, body)
	}
	status, text := bob.Post(api.AuthPasswordPath, api.AuthPasswordRequest{Current: harness.Password, Password: "the changed password of bob"})
	if status != http.StatusOK {
		t.Fatalf("changing the Password: %d %s", status, text)
	}
	wantSignedIn(t, bob, "bob")
	wantSignedOut(t, other)
	status, text = signIn(h.Browser(), "bob", harness.Password)
	wantCode(t, status, text, api.CodeWrongPassword.Name)

	h.Koko("password", "rm", "bob").WantSuccess(t)
	wantSignedOut(t, bob)
	status, text = signIn(h.Browser(), "bob", "the changed password of bob")
	wantCode(t, status, text, api.CodeWrongPassword.Name)
	h.Koko("password", "rm", "bob", "--json").WantError(t, 1, api.CodeNoPassword.Name)

	dave := h.SignIn("dave")
	r := h.Koko("user", "remove", "dave", "--json")
	r.WantSuccess(t)
	if !strings.Contains(r.Stdout, `"outcome": "removed"`) {
		t.Errorf("removing dave, who only set a Password, printed %s, want removed", r.Stdout)
	}
	wantSignedOut(t, dave)
	if !regexp.MustCompile(`msg="user removed" user=dave .*device_sessions_revoked=0 join_codes_voided=1 signins_ended=1 password_links_voided=0`).MatchString(h.Log()) {
		t.Errorf("the log does not count dave's Sign-in:\n%s", h.Log())
	}
}

// A Password Link: refused without a Web URL, and to a Member before the
// name is read; a newer one voids the older; it is spent once, ending
// every Sign-in of its User and signing this browser in; and an expired, a
// voided, an unknown and a Removed User's link is refused link_invalid.
// Each refusal counts as a failed attempt for the address.
func TestPasswordLinks(t *testing.T) {
	t.Parallel()
	h := harness.Start(t, harness.WebListen("127.0.0.1:0"))
	h.Koko("user", "invite", "erin", "--web", "--json").WantError(t, 1, api.CodeWebURLUnset.Name)
	h.Koko("user", "invite", "erin").WantSuccess(t)
	h.Koko("password", "link", "erin", "--json").WantError(t, 1, api.CodeWebURLUnset.Name)
	h.Browser() // sets the Web URL

	member := h.SignIn("erin")
	for _, name := range []string{"erin", "nobody", "Not A Name"} {
		status, sent := member.Send(api.PasswordLinkIssue, fmt.Appendf(nil, `{"user": %q}`, name))
		wantCode(t, status, string(sent), api.CodeOwnerOnly.Name)
	}

	first := inviteForWeb(t, h, "bob")
	second := passwordLink(t, h, "bob")
	b := h.Browser()
	status, body := setPassword(b, first, harness.Password)
	wantCode(t, status, body, api.CodeLinkInvalid.Name)
	if status, body := setPassword(b, second, harness.Password); status != http.StatusOK {
		t.Fatalf("the newer link answered %d %s", status, body)
	}
	status, body = setPassword(h.Browser(), second, "another password of bob")
	wantCode(t, status, body, api.CodeLinkUsed.Name)
	status, body = h.Browser().Post(api.AuthLinkPath, api.AuthLinkRequest{Link: harness.LinkSecret(second)})
	wantCode(t, status, body, api.CodeLinkUsed.Name)

	reset := h.Browser()
	if status, body := setPassword(reset, passwordLink(t, h, "bob"), "the reset password of bob"); status != http.StatusOK {
		t.Fatalf("a reset answered %d %s", status, body)
	}
	wantSignedOut(t, b)
	wantSignedIn(t, reset, "bob")

	expired := passwordLink(t, h, "bob")
	h.Advance(15 * time.Minute)
	status, body = setPassword(h.Browser(), expired, "a password that comes too late")
	wantCode(t, status, body, api.CodeLinkInvalid.Name)
	status, body = h.Browser().Post(api.AuthLinkPath, api.AuthLinkRequest{Link: "AAAAAAAAAAAAAAAAAAAAAAAAAA"})
	wantCode(t, status, body, api.CodeLinkInvalid.Name)
	removed := passwordLink(t, h, "erin")
	h.Koko("user", "remove", "erin").WantSuccess(t)
	status, body = setPassword(h.Browser(), removed, "the password of a removed user")
	wantCode(t, status, body, api.CodeLinkInvalid.Name)

	// Every refusal above came from the same address, as the Browsers
	// share theirs; the 3 since the clock moved 15 minutes still count,
	// and 7 more lock it out.
	for range 7 {
		h.Browser().Post(api.AuthLinkPath, api.AuthLinkRequest{Link: "AAAAAAAAAAAAAAAAAAAAAAAAAA"})
	}
	status, body = h.Browser().Post(api.AuthLinkPath, api.AuthLinkRequest{Link: harness.LinkSecret(passwordLink(t, h, "bob"))})
	wantCode(t, status, body, api.CodeTooManyAttempts.Name)
}

// The Password rule: 15 to 128 characters, counted after NFC, any
// characters, and not the User's name.
func TestThePasswordRule(t *testing.T) {
	t.Parallel()
	h := startSignIns(t)
	name := "abcdefghijklmnopq"
	h.Koko("user", "invite", name).WantSuccess(t)
	link := passwordLink(t, h, name)
	decomposed := strings.Repeat("é", 14) // 28 code points, 14 after NFC
	for _, refused := range []string{strings.Repeat("a", 14), strings.Repeat("a", 129), name, decomposed} {
		status, body := setPassword(h.Browser(), link, refused)
		wantCode(t, status, body, api.CodeInvalidPassword.Name)
	}
	for _, accepted := range []string{strings.Repeat("a", 15), strings.Repeat("密", 128)} {
		if status, body := setPassword(h.Browser(), passwordLink(t, h, name), accepted); status != http.StatusOK {
			t.Errorf("a Password of %d characters answered %d %s", len([]rune(accepted)), status, body)
		}
	}
	if status, body := setPassword(h.Browser(), passwordLink(t, h, name), decomposed+"é"); status != http.StatusOK {
		t.Fatalf("15 characters after NFC answered %d %s", status, body)
	}
	if status, body := signIn(h.Browser(), name, strings.Repeat("é", 15)); status != http.StatusOK {
		t.Errorf("the composed form of the Password was refused: %d %s", status, body)
	}
}

// A Sign-in does what a Device Session of its User does, but web.start,
// which only koko web calls, and server.set, which only the Local caller
// makes; it may add a device for its User.
func TestWhatASignInMayDo(t *testing.T) {
	t.Parallel()
	h := startSignIns(t)
	b := h.SignIn("bob")
	status, body := b.Send(api.WebStart, []byte("{}"))
	wantCode(t, status, string(body), api.CodeDeviceOnly.Name)
	status, body = b.Send(api.ServerSet, []byte(`{"address": "koko.test"}`))
	wantCode(t, status, string(body), api.CodeLocalOnly.Name)
	status, body = b.Send(api.DeviceAdd, []byte(`{"device": "laptop"}`))
	var added api.DeviceAddResponse
	if err := json.Unmarshal(body, &added); status != http.StatusOK || err != nil || added.JoinCode.User != "bob" {
		t.Fatalf("device.add from a Sign-in answered %d %s", status, body)
	}
}

// An Administrator through a Sign-in reads every User's Sign-ins, and
// revokes a Member's, removes their Password and issues them a Password
// Link; the same acts on the Owner or another Administrator are refused
// with owner_only and change nothing.
func TestAnAdministratorsSignInActsOnMembersOnly(t *testing.T) {
	t.Parallel()
	h := startSignIns(t)
	for _, u := range []string{"carol", "dave"} {
		h.Koko("user", "promote", u).WantSuccess(t)
	}
	admin := h.SignIn("carol")
	others := map[string]*harness.Browser{"bob": h.SignIn("bob"), harness.Owner: h.SignIn(harness.Owner), "dave": h.SignIn("dave")}
	ids := map[string]string{}
	for u := range others {
		ids[u] = signInID(t, h, u)
	}

	status, body := admin.Send(api.SignInList, []byte("{}"))
	var listed api.SignInListResponse
	if err := json.Unmarshal(body, &listed); status != http.StatusOK || err != nil || len(listed.SignIns) != 4 {
		t.Errorf("signin.list from carol's Sign-in answered %d %s, want the 4 Sign-ins of every User", status, body)
	}
	for u, whom := range map[string]string{harness.Owner: "the Owner", "dave": "another Administrator"} {
		for _, c := range []struct {
			op   api.Operation
			body string
			does string
		}{
			{api.SignInRevoke, fmt.Sprintf(`{"id": %q}`, ids[u]), "revokes the Sign-ins of"},
			{api.PasswordRemove, fmt.Sprintf(`{"user": %q}`, u), "removes the Password of"},
			{api.PasswordLinkIssue, fmt.Sprintf(`{"user": %q}`, u), "issues a Password Link for"},
		} {
			status, body := admin.Send(c.op, []byte(c.body))
			wantCode(t, status, string(body), api.CodeOwnerOnly.Name)
			if want := "only the Owner " + c.does + " " + whom; !strings.Contains(string(body), want) {
				t.Errorf("%s of %s from carol's Sign-in answered %s, want it to say %q", c.op.Info().Name, u, body, want)
			}
		}
		wantSignedIn(t, others[u], u)
	}
	for _, c := range []struct {
		op   api.Operation
		body string
	}{
		{api.PasswordLinkIssue, `{"user": "bob"}`},
		{api.SignInRevoke, fmt.Sprintf(`{"id": %q}`, ids["bob"])},
		{api.PasswordRemove, `{"user": "bob"}`},
	} {
		if status, body := admin.Send(c.op, []byte(c.body)); status != http.StatusOK {
			t.Errorf("%s from carol's Sign-in answered %d %s", c.op.Info().Name, status, body)
		}
	}
	wantSignedOut(t, others["bob"])
}

// The log names each event of signing in, without a Password, a link's
// secret or a cookie in any line.
func TestTheSignInLogLines(t *testing.T) {
	t.Parallel()
	h := harness.Start(t, harness.WebListen("127.0.0.1:0"))
	h.Browser() // sets the Web URL
	link := inviteForWeb(t, h, "bob")
	b := h.Browser()
	var answers []string
	keep := func(_ int, body string) { answers = append(answers, body) }
	status, body := setPassword(b, link, harness.Password)
	if status != http.StatusOK {
		t.Fatal(status, body)
	}
	keep(status, body)
	keep(b.Post(api.AuthPasswordPath, api.AuthPasswordRequest{Current: harness.Password, Password: "the changed password of bob"}))
	keep(b.Post(api.AuthPasswordPath, api.AuthPasswordRequest{Current: "a wrong password of bob", Password: "the changed password of bob"}))
	keep(signIn(h.Browser(), "bob", "a wrong password of bob"))
	keep(signIn(h.Browser(), "nobody", "a wrong password of nobody"))
	keep(h.Browser().Post(api.AuthLinkPath, api.AuthLinkRequest{Link: harness.LinkSecret(link)}))
	other := h.Browser()
	signIn(other, "bob", "the changed password of bob")
	h.Koko("signin", "revoke", firstLive(t, h, other)).WantSuccess(t)
	b.Post(api.AuthSignOutPath, struct{}{})
	h.SignIn("bob")
	h.Koko("password", "rm", "bob").WantSuccess(t)

	log := h.Log()
	for _, want := range []string{
		`msg="password link issued" user=bob password_link=passwordlink_`,
		`msg="password set" user=bob how=link password_link=passwordlink_`,
		`msg="signed in" user=bob signin=signin_`,
		`client="Firefox on Linux" address=192.0.2.10`,
		`msg="password set" user=bob how=change signin=signin_`,
		`msg="sign-in refused" reason=wrong_password address=192.0.2.10 user=bob signin=signin_`,
		`msg="sign-in refused" reason=wrong_password address=192.0.2.10 user=bob`,
		`msg="sign-in refused" reason=wrong_password address=192.0.2.10` + "\n",
		`msg="password link refused" reason=link_used address=192.0.2.10 user=bob`,
		`msg="sign-in revoked" user=bob signin=signin_`,
		`revoked_by="unix socket"`,
		`msg="signed out" user=bob signin=signin_`,
		`msg="sign-in ended" user=bob signin=signin_`,
		`reason=password_removed ended_by="unix socket"`,
		`msg="password removed" user=bob removed_by="unix socket"`,
	} {
		if !strings.Contains(log, want) {
			t.Errorf("the log does not say %q", want)
		}
	}
	for _, secret := range []string{harness.Password, "the changed password of bob", "a wrong password", harness.LinkSecret(link), b.Cookie, other.Cookie} {
		if secret != "" && strings.Contains(log, secret) {
			t.Errorf("the log holds %q", secret)
		}
		for _, answer := range answers {
			if secret != "" && strings.Contains(answer, secret) {
				t.Errorf("the answer %s holds %q", answer, secret)
			}
		}
	}
	if strings.Contains(log, "nobody") {
		t.Errorf("the log names a name no User holds:\n%s", log)
	}
	if t.Failed() {
		t.Log(log)
	}
}

// firstLive is the id of the Sign-in b is signed in with.
func firstLive(t *testing.T, h *harness.Harness, b *harness.Browser) string {
	t.Helper()
	_, body := current(b)
	var resp api.UserCurrentResponse
	if err := json.Unmarshal([]byte(body), &resp); err != nil || resp.SignIn == nil {
		t.Fatalf("user.current answered %s", body)
	}
	return resp.SignIn.ID
}

// A User invited with --web who never set a Password is deleted with their
// link; one who set a Password is Removed.
func TestRemovingAUserInvitedForTheWeb(t *testing.T) {
	t.Parallel()
	h := startSignIns(t)
	link := inviteForWeb(t, h, "erin")
	r := h.Koko("user", "remove", "erin", "--json")
	r.WantSuccess(t)
	if !strings.Contains(r.Stdout, `"outcome": "deleted"`) {
		t.Errorf("removing erin, who never set a Password, printed %s, want deleted", r.Stdout)
	}
	inviteForWeb(t, h, "erin")
	status, body := setPassword(h.Browser(), link, harness.Password)
	wantCode(t, status, body, api.CodeLinkInvalid.Name)
}

// An /auth/ endpoint reads at most 4 KiB of body, takes only a JSON POST,
// and refuses a field it does not know.
func TestTheAuthEndpointsTakeSmallJSONPosts(t *testing.T) {
	t.Parallel()
	h := startSignIns(t)
	b := h.Browser()
	a := b.Do(http.MethodPost, api.AuthSignInPath, `{"name": "bob", "password": "`+strings.Repeat("a", 4<<10)+`"}`)
	wantCode(t, a.StatusCode, a.Body, api.CodeRequestTooLarge.Name)
	a = b.Do(http.MethodPost, api.AuthSignInPath, `{"name": "bob", "password": "x", "remember": true}`)
	wantCode(t, a.StatusCode, a.Body, api.CodeUnknownField.Name)
	a = b.Do(http.MethodGet, api.AuthSignInPath, "")
	if a.StatusCode == http.StatusOK || !strings.Contains(a.Body, api.CodeMethodNotAllowed.Name) {
		t.Errorf("a GET of /auth/signin answered %s %s, want method_not_allowed", a.Status, a.Body)
	}
	a = b.Do(http.MethodPost, "/auth/elsewhere", "{}")
	wantCode(t, a.StatusCode, a.Body, api.CodeUnknownOperation.Name)
}

// Attempts made at once count against the limits while they run: of 15
// wrong sign-ins sent together from one address, 10 are checked and 5 are
// refused too_many_attempts, however the hashes interleave.
func TestAttemptsMadeAtOnceCannotPassTheLimitsTogether(t *testing.T) {
	t.Parallel()
	h := startSignIns(t)
	statuses := make(chan int, 15)
	var wg sync.WaitGroup
	for i := range 15 {
		wg.Go(func() {
			b := h.Browser()
			b.Address = "198.51.100.9"
			status, _ := signIn(b, fmt.Sprintf("name%d", i), "a wrong password, guessed in a burst")
			statuses <- status
		})
	}
	wg.Wait()
	close(statuses)
	count := map[int]int{}
	for s := range statuses {
		count[s]++
	}
	if count[http.StatusUnauthorized] != 10 || count[http.StatusTooManyRequests] != 5 {
		t.Errorf("15 attempts at once were answered %v, want 10 wrong_password (401) and 5 too_many_attempts (429)", count)
	}
}
