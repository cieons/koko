package cli_test

import (
	"encoding/json/v2"
	"strings"
	"testing"
	"time"

	"github.com/cieons/koko/internal/harness"
)

// startWeb starts a koko server with its web listener on and its Web URL
// set, with the Member bob invited.
func startWebListener(t *testing.T) *harness.Harness {
	t.Helper()
	h := harness.Start(t, harness.WebListen("127.0.0.1:0"))
	h.Koko("server", "set", "web_url="+harness.WebURL).WantSuccess(t)
	h.Koko("user", "invite", "bob").WantSuccess(t)
	return h
}

// password link prints the link for the User's browser, valid for 15
// minutes unless --valid says otherwise; for people, the line says for whom
// and until when.
func TestPasswordLink(t *testing.T) {
	t.Parallel()
	h := startWebListener(t)
	r := h.Koko("password", "link", "bob", "--json")
	r.WantSuccess(t)
	harness.Golden(t, "password-link.json", r.Stdout)
	var out struct {
		ExpiresAt    string `json:"expires_at"`
		PasswordLink string `json:"password_link"`
	}
	if err := json.Unmarshal([]byte(r.Stdout), &out); err != nil {
		t.Fatal(err)
	}
	if want := h.Now().Add(15 * time.Minute).Format(time.RFC3339); out.ExpiresAt != want || !strings.HasPrefix(out.PasswordLink, harness.WebURL+"/password#") {
		t.Errorf("password link printed %+v, want a link under the Web URL expiring at %s", out, want)
	}

	r = h.Koko("password", "link", "bob", "--valid", "2d")
	r.WantSuccess(t)
	want := "For bob to set a Password, open this link in their browser before " + h.Now().Add(48*time.Hour).Format(time.RFC3339) + ":\n" + harness.WebURL + "/password#"
	if !strings.HasPrefix(r.Stdout, want) {
		t.Errorf("password link printed %q, want it to start %q", r.Stdout, want)
	}
}

// password link is refused without a Web URL, to a Member before the name
// is read, and for a Removed User.
func TestPasswordLinkRefusals(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	h.Koko("user", "invite", "bob").WantSuccess(t)
	r := h.Koko("password", "link", "bob", "--json")
	r.WantError(t, 1, "web_url_unset")
	harness.Golden(t, "password-link-web-url-unset.json", r.Stderr)

	h.Koko("server", "set", "web_url="+harness.WebURL).WantSuccess(t)
	member := h.Join("bob")
	for _, name := range []string{"bob", "nobody"} {
		r := member.Koko("password", "link", name, "--json")
		r.WantError(t, 1, "owner_only")
		harness.Golden(t, "password-link-owner-only.json", r.Stderr)
	}
	h.Koko("user", "remove", "bob").WantSuccess(t)
	h.Koko("password", "link", "bob", "--json").WantError(t, 1, "user_removed")
}

// user invite --web creates the Member with a Password Link instead of a
// Join Code.
func TestUserInviteWeb(t *testing.T) {
	t.Parallel()
	h := startWebListener(t)
	r := h.Koko("user", "invite", "erin", "--web", "--json")
	r.WantSuccess(t)
	harness.Golden(t, "user-invite-web.json", r.Stdout)

	r = h.Koko("user", "invite", "frank", "--web")
	r.WantSuccess(t)
	if !strings.Contains(r.Stdout, "Invited frank (user_") || !strings.Contains(r.Stdout, "For frank to set a Password, open this link in their browser before ") {
		t.Errorf("user invite --web printed %q", r.Stdout)
	}
}

// signin ls lists the Sign-ins, the ended ones with --all; signin revoke
// ends one and prints it as it left it; user ls says who has a Password;
// password rm removes it.
func TestSignInsFromTheCLI(t *testing.T) {
	t.Parallel()
	h := startWebListener(t)
	h.SignIn("bob")
	r := h.Koko("signin", "ls", "--json")
	r.WantSuccess(t)
	harness.Golden(t, "signin-ls.json", r.Stdout)
	var listed []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(r.Stdout), &listed); err != nil || len(listed) != 1 {
		t.Fatalf("signin ls printed %q: %v", r.Stdout, err)
	}
	r = h.Koko("signin", "ls")
	r.WantSuccess(t)
	if !strings.HasPrefix(r.Stdout, "USER  CLIENT            ADDRESS     ID") || !strings.Contains(r.Stdout, "bob   Firefox on Linux  192.0.2.10  "+listed[0].ID) {
		t.Errorf("signin ls printed:\n%s", r.Stdout)
	}
	r = h.Koko("user", "ls", "--json")
	r.WantSuccess(t)
	harness.Golden(t, "user-ls-password.json", r.Stdout)

	r = h.Koko("signin", "revoke", listed[0].ID, "--json")
	r.WantSuccess(t)
	harness.Golden(t, "signin-revoke.json", r.Stdout)
	if r := h.Koko("signin", "ls", "--json"); strings.TrimSpace(r.Stdout) != "[]" {
		t.Errorf("signin ls after the revoke printed %s, want none", r.Stdout)
	}
	r = h.Koko("signin", "ls", "--all", "--user", "bob", "--json")
	r.WantSuccess(t)
	harness.Golden(t, "signin-ls-all.json", r.Stdout)
	r = h.Koko("signin", "revoke", "signin_aaaaaaaa", "--json")
	r.WantError(t, 1, "unknown_signin")
	harness.Golden(t, "signin-revoke-unknown.json", r.Stderr)

	r = h.Koko("password", "rm", "bob", "--json")
	r.WantSuccess(t)
	harness.Golden(t, "password-rm.json", r.Stdout)
	r = h.Koko("password", "rm", "bob", "--json")
	r.WantError(t, 1, "no_password")
	harness.Golden(t, "password-rm-no-password.json", r.Stderr)
}

// A Member's signin ls lists their own Sign-ins alone, and another User's
// as none.
func TestAMemberListsTheirOwnSignIns(t *testing.T) {
	t.Parallel()
	h := startWebListener(t)
	h.Koko("user", "invite", "carol").WantSuccess(t)
	h.SignIn("bob")
	h.SignIn("carol")
	member := h.Join("bob")
	for args, want := range map[string]int{"": 1, "--user bob": 1, "--user carol": 0, "--user nobody": 0} {
		r := member.Koko(append([]string{"signin", "ls", "--json"}, strings.Fields(args)...)...)
		r.WantSuccess(t)
		var listed []struct {
			User string `json:"user"`
		}
		if err := json.Unmarshal([]byte(r.Stdout), &listed); err != nil || len(listed) != want {
			t.Errorf("bob's signin ls %s printed %s, want %d of his", args, r.Stdout, want)
		}
		for _, s := range listed {
			if s.User != "bob" {
				t.Errorf("bob's signin ls %s lists %s's Sign-in", args, s.User)
			}
		}
	}
}
