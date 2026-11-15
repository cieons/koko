package cli_test

import (
	"encoding/json/v2"
	"net/http"
	"strings"
	"testing"

	"github.com/cieons/koko/internal/client/cli"
	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/shared/api"
)

// identity ls lists a User's Linked Identity, with its issuer, subject and
// label, who issued the link it came through and when it was last used;
// identity unlink unlinks one and says how many Sign-ins it ended; user ls
// says identity for a User who has one.
func TestIdentitiesFromTheCLI(t *testing.T) {
	t.Parallel()
	iss := harness.NewIssuer(t)
	h := harness.Start(t, harness.WebListen("127.0.0.1:0"), harness.IdentityProvider(iss))
	h.Browser() // sets the Web URL
	for _, u := range []string{"bob", "carol"} {
		h.Koko("user", "invite", u).WantSuccess(t)
	}
	h.SetIssuer(iss)
	link := h.Koko("signin", "link", "bob", "--json")
	link.WantSuccess(t)
	var issued struct {
		SigninLink string `json:"signin_link"`
	}
	if err := json.Unmarshal([]byte(link.Stdout), &issued); err != nil {
		t.Fatal(err)
	}
	iss.SignInAs("subject-bob", "bob@idp.example.com")
	through := func(b *harness.Browser, req api.AuthOIDCBeginRequest, want string) {
		t.Helper()
		if a := b.ThroughProvider(iss, req); a.StatusCode != http.StatusSeeOther || a.Header.Get("Location") != want {
			t.Fatalf("the callback answered %d to %q, want %s", a.StatusCode, a.Header.Get("Location"), want)
		}
	}
	through(h.Browser(), api.AuthOIDCBeginRequest{Purpose: api.OIDCPurposeLink, Link: harness.LinkSecret(issued.SigninLink)}, "/account?linked=1")
	through(h.Browser(), api.AuthOIDCBeginRequest{Purpose: api.OIDCPurposeSignIn}, "/")
	iss.SignInAs("subject-carol", "carol")
	through(h.SignIn("carol"), api.AuthOIDCBeginRequest{Purpose: api.OIDCPurposeLink}, "/account?linked=1")
	// The Issuer's URL holds its random port.
	issuer := func(out string) string {
		return strings.ReplaceAll(out, iss.URL(), "https://idp.example.com:12345/application/o/koko/")
	}

	r := h.Koko("identity", "ls", "--user", "bob", "--json")
	r.WantSuccess(t)
	harness.Golden(t, "identity-ls.json", issuer(r.Stdout))
	r = h.Koko("identity", "ls", "--user", "bob")
	r.WantSuccess(t)
	harness.Golden(t, "identity-ls.txt", issuer(bareIDs(r.Stdout)))
	r = h.Koko("identity", "ls")
	r.WantSuccess(t)
	harness.Golden(t, "identity-ls-none.txt", r.Stdout)

	r = h.Koko("user", "ls", "--json")
	r.WantSuccess(t)
	harness.Golden(t, "user-ls-identity.json", r.Stdout)
	r = h.Koko("user", "ls")
	r.WantSuccess(t)
	harness.Golden(t, "user-ls-identity.txt", bareIDs(r.Stdout))

	r = h.Koko("identity", "unlink", "bob", "--json")
	r.WantSuccess(t)
	harness.Golden(t, "identity-unlink.json", issuer(r.Stdout))
	r = h.Koko("identity", "unlink", "bob", "--json")
	r.WantError(t, 1, "no_identity")
	r = h.Koko("identity", "unlink", "carol")
	r.WantSuccess(t)
	harness.Golden(t, "identity-unlink.txt", bareIDs(r.Stdout))
}

// Against a koko server from before Linked Identities, identity ls and
// unlink fail with server_too_old, sending nothing.
func TestIdentitiesAgainstAServerBeforeThem(t *testing.T) {
	t.Parallel()
	path := serverBeforePasskeys(t)
	for _, args := range [][]string{{"identity", "ls", "--json"}, {"identity", "unlink", "bob", "--json"}} {
		r := harness.Run(t, cli.Runner{SocketPath: path}, nil, args...)
		r.WantError(t, 1, "server_too_old")
	}
}
