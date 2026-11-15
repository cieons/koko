package cli_test

import (
	"database/sql"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/server/seal"
	_ "github.com/cieons/koko/internal/server/store" // registers the sqlite driver
)

// oidcSecret is the client secret the tests set, which no output holds.
const oidcSecret = "client-secret-q7Wm2Xr9Lk4Tz8Vb"

// idpURL stands in a golden file for the Issuer's URL, whose port changes.
const idpURL = "https://idp.example.com/application/o/koko/"

// startOIDC starts a koko server with its web listener and Web URL, whose
// calls to the Identity Provider reach a new Issuer, with the Member bob
// invited.
func startOIDC(t *testing.T, opts ...harness.Option) (*harness.Harness, *harness.Issuer) {
	t.Helper()
	iss := harness.NewIssuer(t)
	h := harness.Start(t, append([]harness.Option{harness.WebListen("127.0.0.1:0"), harness.IdentityProvider(iss)}, opts...)...)
	h.Koko("server", "set", "web_url="+harness.WebURL).WantSuccess(t)
	h.Koko("user", "invite", "bob").WantSuccess(t)
	return h, iss
}

// golden compares out, with the Issuer's URL in it as idpURL, with the
// golden file name, and fails if it holds the client secret.
func goldenOIDC(t *testing.T, iss *harness.Issuer, name, out string) {
	t.Helper()
	if strings.Contains(out, oidcSecret) {
		t.Errorf("%s holds the client secret:\n%s", name, out)
	}
	harness.Golden(t, name, strings.ReplaceAll(out, iss.URL(), idpURL))
}

// server oidc set reads the client secret from a file, from stdin and from
// a prompt with echo off, never from the command line; each prints the
// provider without its secret, the redirect URI to register and how many
// Linked Identities it unlinked.
func TestServerOIDCSetReadsTheSecretWithoutArgv(t *testing.T) {
	t.Parallel()
	dir, keyFile := t.TempDir(), harness.NewRootKeyFile(t)
	h, iss := startOIDC(t, harness.DataDir(dir), harness.RootKeyFile(keyFile))
	// stored is the client secret the koko server keeps, opened.
	stored := func() string {
		t.Helper()
		db, err := sql.Open("sqlite", filepath.Join(dir, "koko.db"))
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = db.Close() }()
		var id string
		var sealed []byte
		if err := db.QueryRowContext(t.Context(), "SELECT id, secret FROM identity_provider").Scan(&id, &sealed); err != nil {
			t.Fatal(err)
		}
		key, err := seal.Load(keyFile)
		if err != nil {
			t.Fatal(err)
		}
		secret, err := key.Open(id, sealed)
		if err != nil {
			t.Fatal(err)
		}
		return string(secret)
	}
	first := []string{"server", "oidc", "set", "--issuer", iss.URL(), "--client-id", "koko", "--name", "Authentik"}

	file := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(file, []byte(oidcSecret+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := h.Koko(append(first, "--client-secret-file", file, "--json")...)
	r.WantSuccess(t)
	goldenOIDC(t, iss, "server-oidc-set.json", r.Stdout)
	if got := stored(); got != oidcSecret {
		t.Errorf("the koko server keeps %q from the file, want %q", got, oidcSecret)
	}

	r = h.KokoStdin(oidcSecret+"-stdin\n", "server", "oidc", "set", "--client-secret", "--name", "Keycloak")
	r.WantSuccess(t)
	goldenOIDC(t, iss, "server-oidc-set.txt", r.Stdout)
	if got := stored(); got != oidcSecret+"-stdin" {
		t.Errorf("the koko server keeps %q from stdin, want %q", got, oidcSecret+"-stdin")
	}

	term := harness.NewTerminal(oidcSecret + "-typed")
	r = h.KokoTerminal(term, "server", "oidc", "set", "--client-secret", "--json")
	if r.Status != 0 {
		t.Fatalf("server oidc set with a prompt exited %d: %s", r.Status, r.Stderr)
	}
	if !strings.Contains(r.Stderr, "Enter the client secret: ") || strings.Contains(r.Stderr+r.Stdout, oidcSecret) {
		t.Errorf("the prompt printed %q and %q, want it to ask for the client secret and echo nothing", r.Stdout, r.Stderr)
	}
	if got := stored(); got != oidcSecret+"-typed" {
		t.Errorf("the koko server keeps %q from the prompt, want %q", got, oidcSecret+"-typed")
	}

	// The secret is the one read, its line ending taken off: a set naming
	// none keeps it, and show never prints it.
	r = h.Koko("server", "oidc", "show", "--json")
	r.WantSuccess(t)
	goldenOIDC(t, iss, "server-oidc-show.json", r.Stdout)
	r = h.Koko("server", "oidc", "show")
	r.WantSuccess(t)
	goldenOIDC(t, iss, "server-oidc-show.txt", r.Stdout)
	if strings.Contains(h.Log(), oidcSecret) {
		t.Error("the koko server's log holds the client secret")
	}
}

// server oidc set refuses, with exit 2 and before any call, a command line
// that changes nothing, a secret written as an argument, and --client-secret
// with nothing to read it from; and the koko server's refusals come back as
// errors.
func TestServerOIDCSetRefusals(t *testing.T) {
	t.Parallel()
	h, iss := startOIDC(t)
	calls := h.Calls()
	for _, args := range [][]string{
		{"server", "oidc", "set"},
		{"server", "oidc", "set", "--client-secret", oidcSecret},
		{"server", "oidc", "set", "--client-secret"},
	} {
		r := h.Koko(append(args, "--json")...)
		if r.Status != 2 || strings.Contains(r.Stdout+r.Stderr, oidcSecret) {
			t.Errorf("koko %s exited %d with %q %q, want 2 without the secret", strings.Join(args, " "), r.Status, r.Stdout, r.Stderr)
		}
	}
	if got := h.Calls(); got != calls {
		t.Errorf("the refused command lines made %d calls, want none", got-calls)
	}

	r := h.KokoStdin(oidcSecret, "server", "oidc", "set", "--issuer", iss.URL(), "--client-secret", "--json")
	r.WantError(t, 1, "invalid_request")
	r = h.Join("bob").Koko("server", "oidc", "show", "--json")
	harness.Golden(t, "server-oidc-show-owner-only.json", r.Stderr)
	r.WantError(t, 1, "owner_only")
	iss.Spoil(func(doc map[string]any) { delete(doc, "jwks_uri") })
	r = h.KokoStdin(oidcSecret, "server", "oidc", "set", "--issuer", iss.URL(), "--client-id", "koko", "--name", "Authentik", "--client-secret", "--json")
	r.WantError(t, 1, "invalid_provider")
	goldenOIDC(t, iss, "server-oidc-set-invalid-provider.json", r.Stderr)
}

// server oidc show prints null, or for people the redirect URI to register,
// before a provider is set; server oidc rm removes it, saying how many
// Linked Identities it unlinked, and is refused once none is set.
func TestServerOIDCShowAndRm(t *testing.T) {
	t.Parallel()
	h, iss := startOIDC(t)
	r := h.Koko("server", "oidc", "show", "--json")
	r.WantSuccess(t)
	if strings.TrimSpace(r.Stdout) != "null" {
		t.Errorf("server oidc show printed %q before a provider was set, want null", r.Stdout)
	}
	r = h.Koko("server", "oidc", "show")
	r.WantSuccess(t)
	harness.Golden(t, "server-oidc-show-none.txt", r.Stdout)

	h.KokoStdin(oidcSecret, "server", "oidc", "set", "--issuer", iss.URL(), "--client-id", "koko", "--name", "Authentik", "--client-secret").WantSuccess(t)
	r = h.Koko("server", "oidc", "rm", "--json")
	r.WantSuccess(t)
	harness.Golden(t, "server-oidc-rm.json", r.Stdout)
	var out map[string]int
	if err := json.Unmarshal([]byte(r.Stdout), &out); err != nil || len(out) != 1 || out["identities_unlinked"] != 0 {
		t.Errorf("server oidc rm printed %q, want identities_unlinked 0 alone", r.Stdout)
	}
	h.KokoStdin(oidcSecret, "server", "oidc", "set", "--issuer", iss.URL(), "--client-id", "koko", "--name", "Authentik", "--client-secret").WantSuccess(t)
	r = h.Koko("server", "oidc", "rm")
	r.WantSuccess(t)
	harness.Golden(t, "server-oidc-rm.txt", r.Stdout)
	h.Koko("server", "oidc", "rm", "--json").WantError(t, 1, "provider_unset")
}
