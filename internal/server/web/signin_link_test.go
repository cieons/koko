package web_test

import (
	"encoding/json/v2"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/server/store"
	"github.com/cieons/koko/internal/shared/api"
)

// linkFor asks /auth/link from b whom the link is for, failing the test
// unless it answers.
func linkFor(t *testing.T, b *harness.Browser, link string) api.AuthLinkResponse {
	t.Helper()
	status, body := b.Post(api.AuthLinkPath, api.AuthLinkRequest{Link: harness.LinkSecret(link)})
	var resp api.AuthLinkResponse
	if err := json.Unmarshal([]byte(body), &resp); status != http.StatusOK || err != nil {
		t.Fatalf("/auth/link answered %d %s", status, body)
	}
	return resp
}

// signInsOf returns every Sign-in of user, the ended ones included, by id.
func signInsOf(t *testing.T, h *harness.Harness, user string) map[string]api.SignIn {
	t.Helper()
	resp, err := h.Call(api.SignInList, api.SignInListRequest{User: user, All: true})
	if err != nil {
		t.Fatalf("signin.list of %s: %v", user, err)
	}
	found := map[string]api.SignIn{}
	for _, si := range resp.SignIns {
		found[si.ID] = si
	}
	return found
}

// A Sign-in Link is the Web URL followed by /link#: the web UI opens there,
// and at /password, where links issued before it open. /auth/link says
// what the User holds already, and spending the link sets a Password, signs
// the browser in with the method password, and ends the User's other
// Sign-ins.
func TestASignInLinkOpensAtLinkAndSetsAPassword(t *testing.T) {
	t.Parallel()
	h := startSignIns(t)
	link := inviteForWeb(t, h, "erin")
	if !regexp.MustCompile(`^https://koko\.example\.com/link#[A-Z2-7]{26}$`).MatchString(link) {
		t.Fatalf("the Sign-in Link is %q, want the Web URL, /link and a 26-character secret after #", link)
	}
	b := h.Browser()
	app := b.Do(http.MethodGet, "/", "")
	for _, path := range []string{"/link", "/password"} {
		if a := b.Do(http.MethodGet, path, ""); a.StatusCode != http.StatusOK || a.Body != app.Body {
			t.Errorf("GET %s answered %s, want the web UI as at /", path, a.Status)
		}
	}
	if got := linkFor(t, b, link); got != (api.AuthLinkResponse{User: "erin", ExpiresAt: got.ExpiresAt}) || got.ExpiresAt == "" {
		t.Errorf("/auth/link answered %+v, want erin with no Password, passkey or Linked Identity", got)
	}
	if status, body := setPassword(b, link, harness.Password); status != http.StatusOK {
		t.Fatalf("spending the link answered %d %s", status, body)
	}
	status, body := current(b)
	var cur api.UserCurrentResponse
	if err := json.Unmarshal([]byte(body), &cur); status != http.StatusOK || err != nil || cur.SignIn == nil || cur.SignIn.Method != api.MethodPassword {
		t.Errorf("user.current answered %d %s, want a Sign-in made with a Password", status, body)
	}

	reset := passwordLink(t, h, "erin")
	if got := linkFor(t, h.Browser(), reset); !got.PasswordSet || got.Passkeys != 0 || got.IdentityLinked {
		t.Errorf("/auth/link answered %+v for erin, who has a Password, want password_set alone", got)
	}
	other := h.Browser()
	if status, body := setPassword(other, reset, "the reset password of erin"); status != http.StatusOK {
		t.Fatalf("spending the reset answered %d %s", status, body)
	}
	wantSignedOut(t, b)
	wantSignedIn(t, other, "erin")
	if status, body := signIn(h.Browser(), "erin", "the reset password of erin"); status != http.StatusOK {
		t.Fatalf("signing in with the Password answered %d %s", status, body)
	}
	for _, si := range signInsOf(t, h, "erin") {
		if si.Method != api.MethodPassword {
			t.Errorf("erin's Sign-in %s has the method %q, want password", si.ID, si.Method)
		}
		if si.EndedAt != "" && si.EndReason != api.EndPasswordChanged {
			t.Errorf("erin's Sign-in %s ended %q, want password_changed", si.ID, si.EndReason)
		}
	}
}

// password.remove ends only the Sign-ins made with the Password, leaving
// those made another way live, and from such a live Sign-in a User with no
// Password sets a first one, ending no Sign-in and voiding no Sign-in Link.
// One with a Password is still asked for the current one. /auth/link counts the passkeys and the Linked
// Identity of the User it is for.
func TestRemovingAPasswordEndsOnlyItsSignIns(t *testing.T) {
	t.Parallel()
	dir, keyFile := t.TempDir(), harness.NewRootKeyFile(t)
	opts := []harness.Option{harness.DataDir(dir), harness.RootKeyFile(keyFile), harness.WebListen("127.0.0.1:0")}
	h := harness.Start(t, opts...)
	h.Koko("user", "invite", "bob").WantSuccess(t)
	withPassword, withPasskey, alsoPasskey := h.SignIn("bob"), h.Browser(), h.Browser()
	for _, b := range []*harness.Browser{withPasskey, alsoPasskey} {
		if status, body := signIn(b, "bob", harness.Password); status != http.StatusOK {
			t.Fatalf("signing in again answered %d %s", status, body)
		}
	}
	passkeySignIn, alsoPasskeySignIn := firstLive(t, h, withPasskey), firstLive(t, h, alsoPasskey)
	h.Stop()

	// The Sign-ins withPasskey and alsoPasskey hold read as ones a passkey
	// made, as the passkey's ticket will record them.
	st, err := store.Open(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	err = st.Write(t.Context(), func(tx store.Tx) error {
		_, err := tx.ExecContext(t.Context(), `UPDATE signins SET method = 'passkey', credential = 'passkey_a' WHERE id IN (?, ?)`,
			passkeySignIn, alsoPasskeySignIn)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(t.Context(), `INSERT INTO passkeys
			(id, user_id, credential_id, public_key, algorithm, rp_id, name, sign_count, backup_eligible, backed_up, added_with, added_at)
			SELECT 'passkey_a', id, x'01', x'02', -7, 'koko.example.com', 'Laptop', 0, 1, 1, ?, '2026-11-15T00:00:00Z' FROM users WHERE name = 'bob'`,
			passkeySignIn)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(t.Context(), `INSERT INTO identities (id, user_id, issuer, subject, label, linked_with, linked_at)
			SELECT 'identity_a', id, 'https://idp.example.com', 'subject-of-bob', 'bob', ?, '2026-11-15T00:00:00Z' FROM users WHERE name = 'bob'`,
			passkeySignIn)
		return err
	})
	if closeErr := st.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		t.Fatalf("record bob's passkey: %v", err)
	}

	h = harness.Start(t, append(opts, harness.NoInit())...)
	again := func(b *harness.Browser) *harness.Browser {
		moved := h.Browser()
		moved.Cookie = b.Cookie
		return moved
	}
	withPassword, withPasskey, alsoPasskey = again(withPassword), again(withPasskey), again(alsoPasskey)
	wantSignedIn(t, withPassword, "bob")
	h.Koko("password", "rm", "bob").WantSuccess(t)
	wantSignedOut(t, withPassword)
	wantSignedIn(t, withPasskey, "bob")
	for id, si := range signInsOf(t, h, "bob") {
		switch {
		case (id == passkeySignIn || id == alsoPasskeySignIn) && (si.Method != api.MethodPasskey || si.EndedAt != ""):
			t.Errorf("bob's passkey Sign-in is %+v, want it live with the method passkey", si)
		case id != passkeySignIn && id != alsoPasskeySignIn && (si.Method != api.MethodPassword || si.EndReason != api.EndPasswordRemoved):
			t.Errorf("bob's Password Sign-in is %+v, want it ended password_removed", si)
		}
	}

	link := passwordLink(t, h, "bob")
	status, body := withPasskey.Post(api.AuthPasswordPath, map[string]string{"password": "the first password of bob"})
	if status != http.StatusOK || !strings.Contains(body, `"name":"bob"`) {
		t.Fatalf("a first Password from the passkey's Sign-in answered %d %s", status, body)
	}
	wantSignedIn(t, withPasskey, "bob")
	wantSignedIn(t, alsoPasskey, "bob")
	signedIn := h.Browser()
	if status, body := signIn(signedIn, "bob", "the first password of bob"); status != http.StatusOK {
		t.Fatalf("signing in with the first Password answered %d %s", status, body)
	}
	wantSignedIn(t, withPasskey, "bob")
	for _, b := range []*harness.Browser{withPasskey, signedIn} {
		status, body = b.Post(api.AuthPasswordPath, map[string]string{"password": "a password without the current one"})
		wantCode(t, status, body, api.CodeWrongPassword.Name)
	}
	wantSignedIn(t, signedIn, "bob")
	if !strings.Contains(h.Log(), `msg="password set" user=bob how=signin signin=`+passkeySignIn) {
		t.Errorf("the log does not say bob set a first Password from %s:\n%s", passkeySignIn, h.Log())
	}

	if got := linkFor(t, h.Browser(), link); !got.PasswordSet || got.Passkeys != 1 || !got.IdentityLinked {
		t.Errorf("/auth/link answered %+v for bob, want his Password, his 1 passkey and his Linked Identity", got)
	}
}
