package identity_test

import (
	"bytes"
	"encoding/json/v2"
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/server/store"
	"github.com/cieons/koko/internal/shared/api"
	"github.com/cieons/koko/internal/shared/timing"
)

// secret is the client secret the tests set, which must show nowhere in
// plain.
const secret = "client-secret-q7Wm2Xr9Lk4Tz8Vb"

// redirectURI is the redirect URI of the harness's Web URL.
const redirectURI = harness.WebURL + "/auth/oidc/callback"

// start starts a koko server with its web listener and Web URL, whose
// calls to the provider reach iss, with opts.
func start(t *testing.T, iss *harness.Issuer, opts ...harness.Option) *harness.Harness {
	t.Helper()
	h := harness.Start(t, append([]harness.Option{harness.WebListen("127.0.0.1:0"), harness.IdentityProvider(iss)}, opts...)...)
	h.Browser()
	return h
}

// full is a first set of iss as the provider.
func full(iss *harness.Issuer) api.ServerOIDCSetRequest {
	return api.ServerOIDCSetRequest{Issuer: new(iss.URL()), ClientID: new("koko"), ClientSecret: new(secret), Name: new("Authentik")}
}

// code is the error code of err, failing the test unless it is an API
// error.
func code(t *testing.T, err error) string {
	t.Helper()
	e, ok := errors.AsType[*api.Error](err)
	if !ok {
		t.Fatalf("the call failed with %v, want an API error", err)
	}
	return e.Code
}

// shown is the Identity Provider as server.oidc.show answers it.
func shown(t *testing.T, h *harness.Harness) api.ServerOIDCShowResponse {
	t.Helper()
	resp, err := h.Call(api.ServerOIDCShow, api.ServerOIDCShowRequest{})
	if err != nil {
		t.Fatalf("server.oidc.show: %v", err)
	}
	return resp
}

// send makes a call of o with req's JSON from send, such as a device's or a
// browser's Send, and returns the error code, "" on success, and the body.
func send(t *testing.T, sender func(api.Operation, []byte) (int, []byte), o api.Operation, req any) (string, []byte) {
	t.Helper()
	b, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	status, body := sender(o, b)
	if status == http.StatusOK {
		return "", body
	}
	var e api.ErrorResponse
	if err := json.Unmarshal(body, &e); err != nil {
		t.Fatalf("answered %d %s, not an error object", status, body)
	}
	return e.Error.Code, body
}

// The Owner sets the Identity Provider from the Unix socket, a device and
// a browser Sign-in, each answered with it, without its secret, and the
// redirect URI; an Administrator and a Member are refused with owner_only,
// however wrong their call, before anything is read or any provider asked.
func TestTheOwnerSetsTheIdentityProviderFromEachOfTheirCallers(t *testing.T) {
	t.Parallel()
	iss := harness.NewIssuer(t)
	h := start(t, iss)
	resp, err := h.Call(api.ServerOIDCSet, full(iss))
	if err != nil {
		t.Fatalf("set the provider over the Unix socket: %v", err)
	}
	want := api.IdentityProvider{Issuer: iss.URL(), ClientID: "koko", Name: "Authentik", ClientSecretSet: true, LinkedIdentities: 0, SetAt: h.Now().UTC().Format(time.RFC3339)}
	if resp.Provider != want || resp.RedirectURI != redirectURI || resp.IdentitiesUnlinked != 0 {
		t.Errorf("server.oidc.set answered %+v, want %+v, the redirect URI %s and none unlinked", resp, want, redirectURI)
	}
	if got := shown(t, h); got.Provider == nil || *got.Provider != want || got.RedirectURI == nil || *got.RedirectURI != redirectURI {
		t.Errorf("server.oidc.show answered %+v, want %+v and %s", got, want, redirectURI)
	}
	device := h.Join(harness.Owner)
	if c, body := send(t, device.Send, api.ServerOIDCSet, api.ServerOIDCSetRequest{Name: new("Keycloak")}); c != "" {
		t.Errorf("the Owner's device was refused: %s", body)
	}
	browser := h.SignIn(harness.Owner)
	if c, body := send(t, browser.Send, api.ServerOIDCSet, api.ServerOIDCSetRequest{Name: new("Pocket ID")}); c != "" {
		t.Errorf("the Owner's Sign-in was refused: %s", body)
	}
	if got := shown(t, h).Provider; got == nil || got.Name != "Pocket ID" {
		t.Errorf("the provider is %+v, want the name the Owner's Sign-in set", got)
	}
	log := h.Log()
	for _, by := range []string{`set_by="unix socket"`, "set_by=device_", "set_by=signin_"} {
		if !strings.Contains(log, `msg="identity provider set" user=alice`) || !strings.Contains(log, by) {
			t.Errorf("no identity provider set line with %s in the log:\n%s", by, log)
		}
	}

	h.Koko("user", "invite", "eve").WantSuccess(t)
	h.Koko("user", "promote", "eve").WantSuccess(t)
	h.Koko("user", "invite", "bob").WantSuccess(t)
	asked := len(iss.Requests())
	wrong := api.ServerOIDCSetRequest{Issuer: new("http://nowhere"), ClientSecret: new("")}
	for _, u := range []string{"eve", "bob"} {
		d := h.Join(u)
		for _, req := range []any{full(iss), wrong, api.ServerOIDCRemoveRequest{}} {
			o := api.Operation(api.ServerOIDCSet)
			if _, ok := req.(api.ServerOIDCRemoveRequest); ok {
				o = api.ServerOIDCRemove
			}
			if c, body := send(t, d.Send, o, req); c != api.CodeOwnerOnly.Name {
				t.Errorf("%s's %s answered %s, want owner_only", u, o.Info().Name, body)
			}
		}
	}
	if got := len(iss.Requests()); got != asked {
		t.Errorf("the refused calls made %d requests to the provider, want none", got-asked)
	}
	if got := shown(t, h).Provider; got == nil || got.Name != "Pocket ID" {
		t.Errorf("the refused calls changed the provider to %+v", got)
	}
}

// server.oidc.show answers the Owner and the Administrators, and a
// provider with no Web URL has no redirect URI.
func TestShowAnswersNoneUntilAProviderIsSet(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	if got := shown(t, h); got.Provider != nil || got.RedirectURI != nil {
		t.Errorf("server.oidc.show answered %+v before any provider or Web URL, want both null", got)
	}
	h.Koko("user", "invite", "bob").WantSuccess(t)
	if c, body := send(t, h.Join("bob").Send, api.ServerOIDCShow, api.ServerOIDCShowRequest{}); c != api.CodeOwnerOnly.Name {
		t.Errorf("a Member's server.oidc.show answered %s, want owner_only", body)
	}
}

// The first set names all four settings, and no set is made without a Web
// URL, which the redirect URI is part of; neither changes anything, and
// neither asks the provider.
func TestTheFirstSetNamesEverySettingAndNeedsTheWebURL(t *testing.T) {
	t.Parallel()
	iss := harness.NewIssuer(t)
	h := start(t, iss)
	_, err := h.Call(api.ServerOIDCSet, api.ServerOIDCSetRequest{Issuer: new(iss.URL()), Name: new("Authentik")})
	if c := code(t, err); c != api.CodeInvalidRequest.Name || !strings.Contains(err.Error(), "client_id, client_secret missing") {
		t.Errorf("a first set without the client answered %s %v, want invalid_request naming client_id and client_secret", c, err)
	}
	if _, err := h.Call(api.ServerSet, api.ServerSetRequest{Unset: []string{"web_url"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Call(api.ServerOIDCSet, full(iss)); code(t, err) != api.CodeWebURLUnset.Name {
		t.Errorf("a set without a Web URL answered %v, want web_url_unset", err)
	}
	if got := shown(t, h).Provider; got != nil {
		t.Errorf("the refused sets left the provider %+v, want none", got)
	}
	if got := iss.Requests(); len(got) != 0 {
		t.Errorf("the refused sets asked the provider %v, want nothing", got)
	}
}

// A setting koko does not take is refused with invalid_provider before the
// provider is asked, changing nothing.
func TestASettingKokoDoesNotTakeIsRefused(t *testing.T) {
	t.Parallel()
	iss := harness.NewIssuer(t)
	h := start(t, iss)
	base := full(iss)
	with := func(change func(*api.ServerOIDCSetRequest)) api.ServerOIDCSetRequest {
		req := base
		change(&req)
		return req
	}
	for name, req := range map[string]api.ServerOIDCSetRequest{
		"an http:// issuer":      with(func(r *api.ServerOIDCSetRequest) { r.Issuer = new("http://idp.example.com/") }),
		"an issuer with a query": with(func(r *api.ServerOIDCSetRequest) { r.Issuer = new(iss.URL() + "?a=b") }),
		"an issuer with a hash":  with(func(r *api.ServerOIDCSetRequest) { r.Issuer = new(iss.URL() + "#x") }),
		"an issuer with a user":  with(func(r *api.ServerOIDCSetRequest) { r.Issuer = new("https://me@idp.example.com/") }),
		"an issuer with a space": with(func(r *api.ServerOIDCSetRequest) { r.Issuer = new("https://idp.example.com/a b") }),
		"an issuer with no host": with(func(r *api.ServerOIDCSetRequest) { r.Issuer = new("https:///path") }),
		"an empty issuer":        with(func(r *api.ServerOIDCSetRequest) { r.Issuer = new("") }),
		"an issuer of 513 bytes": with(func(r *api.ServerOIDCSetRequest) {
			r.Issuer = new("https://idp.example.com/" + strings.Repeat("a", 489))
		}),
		"an empty client ID":       with(func(r *api.ServerOIDCSetRequest) { r.ClientID = new("") }),
		"a client ID of 256 bytes": with(func(r *api.ServerOIDCSetRequest) { r.ClientID = new(strings.Repeat("c", 256)) }),
		"an empty secret":          with(func(r *api.ServerOIDCSetRequest) { r.ClientSecret = new("") }),
		"a secret of 1,025 bytes":  with(func(r *api.ServerOIDCSetRequest) { r.ClientSecret = new(strings.Repeat("s", 1025)) }),
		"a secret ending a line":   with(func(r *api.ServerOIDCSetRequest) { r.ClientSecret = new(secret + "\n") }),
		"an empty name":            with(func(r *api.ServerOIDCSetRequest) { r.Name = new("") }),
		"a name of 41 characters":  with(func(r *api.ServerOIDCSetRequest) { r.Name = new(strings.Repeat("é", 41)) }),
		"a name ending in a space": with(func(r *api.ServerOIDCSetRequest) { r.Name = new("Authentik ") }),
	} {
		_, err := h.Call(api.ServerOIDCSet, req)
		if code(t, err) != api.CodeInvalidProvider.Name {
			t.Errorf("%s answered %v, want invalid_provider", name, err)
		}
		if strings.Contains(err.Error(), secret) {
			t.Errorf("the refusal of %s quotes the client secret: %v", name, err)
		}
	}
	for _, edge := range []api.ServerOIDCSetRequest{
		with(func(r *api.ServerOIDCSetRequest) { r.ClientID = new(strings.Repeat("c", 255)) }),
		with(func(r *api.ServerOIDCSetRequest) { r.ClientSecret = new(strings.Repeat("s", 1024)) }),
		with(func(r *api.ServerOIDCSetRequest) { r.Name = new(strings.Repeat("é", 40)) }),
	} {
		if _, err := h.Call(api.ServerOIDCSet, edge); err != nil {
			t.Errorf("a set at the limit was refused: %v", err)
		}
	}
	if got := iss.Requests(); len(got) != 1 {
		t.Errorf("the provider was asked %v, want its discovery once, by the first set taken", got)
	}
}

// A new issuer's discovery that is not valid is refused with
// invalid_provider, saying which, and one that does not answer with
// provider_unreachable; neither changes anything.
func TestADiscoveryKokoDoesNotTakeIsRefused(t *testing.T) {
	t.Parallel()
	iss := harness.NewIssuer(t)
	h := start(t, iss)
	for _, c := range []struct {
		name   string
		spoil  func(doc map[string]any)
		status int
		want   api.Code
		says   string
	}{
		{name: "another issuer", spoil: func(d map[string]any) { d["issuer"] = strings.TrimSuffix(iss.URL(), "/") }, want: api.CodeInvalidProvider, says: "byte for byte"},
		{name: "no token endpoint", spoil: func(d map[string]any) { delete(d, "token_endpoint") }, want: api.CodeInvalidProvider, says: "token_endpoint"},
		{name: "an http:// authorization endpoint", spoil: func(d map[string]any) { d["authorization_endpoint"] = "http://idp.example.com/authorize" }, want: api.CodeInvalidProvider, says: "authorization_endpoint"},
		{name: "no key set", spoil: func(d map[string]any) { delete(d, "jwks_uri") }, want: api.CodeInvalidProvider, says: "jwks_uri"},
		{name: "no code response type", spoil: func(d map[string]any) { d["response_types_supported"] = []string{"id_token"} }, want: api.CodeInvalidProvider, says: "response_types_supported"},
		{name: "no document", status: http.StatusNotFound, want: api.CodeInvalidProvider, says: "404"},
		{name: "a server error", status: http.StatusServiceUnavailable, want: api.CodeProviderUnreachable, says: "503"},
	} {
		iss.Spoil(c.spoil)
		iss.AnswerDiscovery(c.status)
		_, err := h.Call(api.ServerOIDCSet, full(iss))
		if code(t, err) != c.want.Name || !strings.Contains(err.Error(), c.says) {
			t.Errorf("a discovery with %s answered %v, want %s saying %s", c.name, err, c.want.Name, c.says)
		}
	}
	iss.Spoil(nil)
	iss.AnswerDiscovery(0)
	down := full(iss)
	down.Issuer = new("https://127.0.0.1:1/")
	if _, err := h.Call(api.ServerOIDCSet, down); code(t, err) != api.CodeProviderUnreachable.Name {
		t.Errorf("an issuer nothing answers at answered %v, want provider_unreachable", err)
	}
	if got := shown(t, h).Provider; got != nil {
		t.Errorf("the refused sets left the provider %+v, want none", got)
	}
	if !strings.Contains(h.Log(), `msg="the identity provider could not be reached"`) {
		t.Errorf("the log has no line for the provider that did not answer:\n%s", h.Log())
	}

	// A change to an issuer whose discovery is refused keeps the provider
	// as it was.
	if _, err := h.Call(api.ServerOIDCSet, full(iss)); err != nil {
		t.Fatal(err)
	}
	before := shown(t, h)
	if _, err := h.Call(api.ServerOIDCSet, api.ServerOIDCSetRequest{Issuer: down.Issuer, Name: new("Other")}); code(t, err) != api.CodeProviderUnreachable.Name {
		t.Errorf("a change to an issuer nothing answers at answered %v, want provider_unreachable", err)
	}
	if after := shown(t, h); *after.Provider != *before.Provider {
		t.Errorf("a refused change left the provider %+v, want %+v", *after.Provider, *before.Provider)
	}
}

// The client secret is sealed: it is in no file of the data directory, and
// in no answer, error or log line, while a set that does not name it keeps
// it.
func TestTheClientSecretIsSealed(t *testing.T) {
	t.Parallel()
	iss := harness.NewIssuer(t)
	dir := t.TempDir()
	h := start(t, iss, harness.DataDir(dir))
	resp, err := h.Call(api.ServerOIDCSet, full(iss))
	if err != nil {
		t.Fatal(err)
	}
	refused := full(iss)
	refused.Name = new("")
	_, refusal := h.Call(api.ServerOIDCSet, refused)
	_, show := send(t, h.Send, api.ServerOIDCShow, api.ServerOIDCShowRequest{})
	_, kept := send(t, h.Send, api.ServerOIDCSet, api.ServerOIDCSetRequest{Name: new("Keycloak")})
	for what, text := range map[string]string{
		"server.oidc.set's answer": string(must(json.Marshal(resp))), "a refusal": refusal.Error(),
		"server.oidc.show's answer": string(show), "a set naming no secret": string(kept), "the log": h.Log(),
	} {
		if strings.Contains(text, secret) {
			t.Errorf("%s holds the client secret: %s", what, text)
		}
	}
	h.Stop()
	err = filepath.WalkDir(dir, func(path string, e fs.DirEntry, err error) error {
		if err != nil || e.IsDir() {
			return err
		}
		b, err := os.ReadFile(path)
		if bytes.Contains(b, []byte(secret)) {
			t.Errorf("%s holds the client secret in plain", path)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := readSecret(t, dir); got == nil {
		t.Error("the provider's record holds no sealed secret")
	}
}

// must is v, failing nothing: for a value whose error cannot happen.
func must[T any](v T, _ error) T { return v }

// readSecret is the sealed secret the provider's record in dir holds.
func readSecret(t *testing.T, dir string) []byte {
	t.Helper()
	st, err := store.Open(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	var sealed []byte
	err = st.Read(t.Context(), func(tx store.Tx) error {
		return tx.QueryRowContext(t.Context(), "SELECT secret FROM identity_provider").Scan(&sealed)
	})
	if err != nil {
		t.Fatal(err)
	}
	return sealed
}

// linkedFixture is a koko server whose provider is iss, where bob and
// carol each have a Linked Identity at it, written by the test, with one
// Sign-in made through it and one with a Password, all live.
type linkedFixture struct {
	h   *harness.Harness
	dir string
	// throughIdentity and withPassword are each User's Sign-ins by id.
	throughIdentity, withPassword map[string]string
}

func newLinkedFixture(t *testing.T, iss *harness.Issuer) linkedFixture {
	t.Helper()
	f := linkedFixture{dir: t.TempDir(), throughIdentity: map[string]string{}, withPassword: map[string]string{}}
	rootKey := harness.NewRootKeyFile(t)
	opts := []harness.Option{harness.DataDir(f.dir), harness.RootKeyFile(rootKey)}
	h := start(t, iss, opts...)
	if _, err := h.Call(api.ServerOIDCSet, full(iss)); err != nil {
		t.Fatal(err)
	}
	for _, u := range []string{"bob", "carol"} {
		h.Koko("user", "invite", u).WantSuccess(t)
		f.throughIdentity[u] = signInID(t, h.SignIn(u))
		b := h.Browser()
		if status, body := b.Post(api.AuthSignInPath, api.AuthSignInRequest{Name: u, Password: harness.Password}); status != http.StatusOK {
			t.Fatalf("sign %s in with their Password: %d %s", u, status, body)
		}
		f.withPassword[u] = signInID(t, b)
	}
	h.Stop()
	st, err := store.Open(t.Context(), f.dir)
	if err != nil {
		t.Fatal(err)
	}
	err = st.Write(t.Context(), func(tx store.Tx) error {
		for u, si := range f.throughIdentity {
			_, err := tx.ExecContext(t.Context(), `INSERT INTO identities (id, user_id, issuer, subject, label, linked_with, linked_at)
				SELECT ?1, id, ?2, ?3, ?3, ?4, '2026-11-15T00:00:00Z' FROM users WHERE name = ?3`, "identity_"+u, iss.URL(), u, si)
			if err != nil {
				return err
			}
			_, err = tx.ExecContext(t.Context(), `UPDATE signins SET method = 'identity', credential = ? WHERE id = ?`, "identity_"+u, si)
			if err != nil {
				return err
			}
		}
		return nil
	})
	if closeErr := st.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		t.Fatal(err)
	}
	f.h = harness.Start(t, append(opts, harness.NoInit(), harness.WebListen("127.0.0.1:0"), harness.IdentityProvider(iss))...)
	return f
}

// signInID is the id of the Sign-in b holds.
func signInID(t *testing.T, b *harness.Browser) string {
	t.Helper()
	status, body := b.Send(api.UserCurrent, []byte(`{}`))
	var resp api.UserCurrentResponse
	if err := json.Unmarshal(body, &resp); status != http.StatusOK || err != nil || resp.SignIn == nil {
		t.Fatalf("user.current from a browser signed in: %d %s", status, body)
	}
	return resp.SignIn.ID
}

// identities is how many Linked Identities f's koko server holds, read
// once it has stopped.
func (f linkedFixture) identities(t *testing.T) int {
	t.Helper()
	f.h.Stop()
	st, err := store.Open(t.Context(), f.dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	var n int
	err = st.Read(t.Context(), func(tx store.Tx) error {
		return tx.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM identities").Scan(&n)
	})
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// wantEnded checks that each User's Sign-in through their identity ended
// as identity_unlinked, or is live when !ended, and that the one made with
// their Password is live.
func (f linkedFixture) wantEnded(t *testing.T, ended bool) {
	t.Helper()
	for u, through := range f.throughIdentity {
		resp, err := f.h.Call(api.SignInList, api.SignInListRequest{User: u, All: true})
		if err != nil {
			t.Fatal(err)
		}
		for _, si := range resp.SignIns {
			switch {
			case si.ID == through && ended && si.EndReason != api.EndIdentityUnlinked:
				t.Errorf("%s's Sign-in through their identity ended as %q, want identity_unlinked", u, si.EndReason)
			case si.ID == through && !ended && si.EndedAt != "":
				t.Errorf("%s's Sign-in through their identity ended as %q, want it live", u, si.EndReason)
			case si.ID == f.withPassword[u] && si.EndedAt != "":
				t.Errorf("%s's Sign-in with their Password ended as %q, want it live", u, si.EndReason)
			}
		}
	}
}

// A change of the client or the name keeps every Linked Identity and the
// Sign-ins made through them.
func TestAChangeOfClientOrNameKeepsIdentities(t *testing.T) {
	t.Parallel()
	iss := harness.NewIssuer(t)
	f := newLinkedFixture(t, iss)
	if got := shown(t, f.h).Provider; got == nil || got.LinkedIdentities != 2 {
		t.Errorf("server.oidc.show answered %+v, want 2 Linked Identities", got)
	}
	resp, err := f.h.Call(api.ServerOIDCSet, api.ServerOIDCSetRequest{ClientID: new("koko-2"), ClientSecret: new(secret + "-2"), Name: new("Keycloak")})
	if err != nil || resp.IdentitiesUnlinked != 0 || resp.Provider.LinkedIdentities != 2 {
		t.Fatalf("a change of the client and the name answered %+v %v, want none unlinked and 2 linked", resp, err)
	}
	f.wantEnded(t, false)
	if !strings.Contains(f.h.Log(), `client_id=koko-2 name=Keycloak secret_changed=true set_by="unix socket" identities_unlinked=0`) {
		t.Errorf("the log does not say the secret changed and nothing was unlinked:\n%s", f.h.Log())
	}
	if n := f.identities(t); n != 2 {
		t.Errorf("%d Linked Identities are left, want both", n)
	}
}

// A change of issuer unlinks every Linked Identity, ending the Sign-ins
// made through them and no others, and logs each.
func TestAChangeOfIssuerUnlinksEveryIdentity(t *testing.T) {
	t.Parallel()
	iss := harness.NewIssuer(t)
	f := newLinkedFixture(t, iss)
	other := harness.NewIssuer(t)
	resp, err := f.h.Call(api.ServerOIDCSet, api.ServerOIDCSetRequest{Issuer: new(other.URL())})
	if err != nil || resp.IdentitiesUnlinked != 2 || resp.Provider.Issuer != other.URL() || resp.Provider.LinkedIdentities != 0 {
		t.Fatalf("a change of issuer answered %+v %v, want the new issuer and 2 unlinked", resp, err)
	}
	f.wantEnded(t, true)
	log := f.h.Log()
	for _, line := range []string{
		`msg="identity unlinked" user=bob identity=identity_bob unlinked_by="unix socket" signins_ended=1`,
		`msg="identity unlinked" user=carol identity=identity_carol unlinked_by="unix socket" signins_ended=1`,
		`reason=identity_unlinked`,
		"secret_changed=false",
		"identities_unlinked=2",
	} {
		if !strings.Contains(log, line) {
			t.Errorf("the log has no %s:\n%s", line, log)
		}
	}
	if n := f.identities(t); n != 0 {
		t.Errorf("%d Linked Identities are left, want none", n)
	}
}

// Removing the provider unlinks every Linked Identity, ending the Sign-ins
// made through them; with none set, it is refused with provider_unset.
func TestRemovingTheProviderUnlinksEveryIdentity(t *testing.T) {
	t.Parallel()
	iss := harness.NewIssuer(t)
	f := newLinkedFixture(t, iss)
	resp, err := f.h.Call(api.ServerOIDCRemove, api.ServerOIDCRemoveRequest{})
	if err != nil || resp.IdentitiesUnlinked != 2 {
		t.Fatalf("server.oidc.remove answered %+v %v, want 2 unlinked", resp, err)
	}
	f.wantEnded(t, true)
	if got := shown(t, f.h).Provider; got != nil {
		t.Errorf("the provider is %+v after its removal", got)
	}
	if !strings.Contains(f.h.Log(), `msg="identity provider removed" user=alice issuer=`+iss.URL()+` removed_by="unix socket" identities_unlinked=2`) {
		t.Errorf("the log has no identity provider removed line:\n%s", f.h.Log())
	}
	if _, err := f.h.Call(api.ServerOIDCRemove, api.ServerOIDCRemoveRequest{}); code(t, err) != api.CodeProviderUnset.Name {
		t.Errorf("a second removal answered %v, want provider_unset", err)
	}
	if n := f.identities(t); n != 0 {
		t.Errorf("%d Linked Identities are left, want none", n)
	}
}

// At most 4 calls to the provider are in flight: a fifth waits its turn,
// then fails with provider_unreachable, logged once for the run, while no
// transaction is held, so other writes go on.
func TestTheCallsInFlightAreBounded(t *testing.T) {
	t.Parallel()
	iss := harness.NewIssuer(t)
	h := start(t, iss, harness.Timings(timing.Timings{ProviderTurn: 200 * time.Millisecond}))
	iss.Hold()
	var held sync.WaitGroup
	errs := make(chan error, 4)
	for range 4 {
		held.Go(func() {
			_, err := h.Call(api.ServerOIDCSet, full(iss))
			errs <- err
		})
	}
	deadline := time.Now().Add(10 * time.Second)
	for iss.Waiting() < 4 {
		if time.Now().After(deadline) {
			t.Fatalf("%d calls reached the provider, want 4", iss.Waiting())
		}
		time.Sleep(10 * time.Millisecond)
	}
	// While they wait, other writes go on.
	h.Koko("space", "add", "ops").WantSuccess(t)
	for range 2 {
		if _, err := h.Call(api.ServerOIDCSet, full(iss)); code(t, err) != api.CodeProviderUnreachable.Name {
			t.Errorf("a fifth call answered %v, want provider_unreachable", err)
		}
	}
	if n := strings.Count(h.Log(), `msg="the identity provider could not be reached"`); n != 1 {
		t.Errorf("the log has %d lines for the run of calls without an answer, want 1:\n%s", n, h.Log())
	}
	if got := iss.Waiting(); got != 4 {
		t.Errorf("%d calls reached the provider, want the 4 in flight", got)
	}
	iss.Release()
	held.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("a call held in flight failed once answered: %v", err)
		}
	}
}
