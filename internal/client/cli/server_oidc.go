package cli

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"io"
	"reflect"
	"strings"

	"github.com/cieons/koko/internal/shared/api"
)

// serverOIDCCommands configure the Identity Provider (ADR 0060).
var serverOIDCCommands = []Command{
	{
		Noun:    "server oidc",
		Verb:    "show",
		Summary: "Show the Identity Provider browsers sign in through, without its client secret, and the redirect URI to register at it",
		Examples: []string{
			"koko server oidc show",
		},
		Tier: Read,
		Who:  OwnerOrAdministrator,
		Op:   api.ServerOIDCShow,
		Run:  action(serverOIDCShow),
	},
	{
		Noun:    "server oidc",
		Verb:    "set",
		Summary: "Set the Identity Provider browsers sign in through, checking its issuer's discovery; a change of issuer unlinks every Linked Identity",
		Flags: []Flag{
			{Name: "issuer", Value: "url", Summary: "the provider's issuer URL, https://, compared byte for byte, a trailing / included"},
			{Name: "client-id", Value: "id", Summary: "the client koko is registered as at the provider"},
			{Name: "name", Value: "name", Summary: "the provider's name on the sign-in button, such as Authentik"},
			{Name: "client-secret", Bool: true, Summary: "set the client's secret, read from --client-secret-file, stdin, or a prompt with echo off"},
			{Name: "client-secret-file", Value: "file", Summary: "read the client's secret from this file"},
		},
		ReadsSecret: true,
		Examples: []string{
			"koko server oidc set --issuer https://idp.example.com/application/o/koko/ --client-id koko --name Authentik --client-secret",
			"koko server oidc set --name Keycloak",
			"koko server oidc set --client-secret-file secret.txt",
		},
		// A change of issuer unlinks every Linked Identity (ADR 0019).
		Tier: Irreversible,
		Who:  OwnerAlone,
		Op:   api.ServerOIDCSet,
		Run:  action(serverOIDCSet),
	},
	{
		Noun:    "server oidc",
		Verb:    "rm",
		Summary: "Remove the Identity Provider, unlinking every Linked Identity and signing out the browsers signed in through them",
		Examples: []string{
			"koko server oidc rm",
		},
		Tier: Irreversible,
		Who:  OwnerAlone,
		Op:   api.ServerOIDCRemove,
		Run:  action(serverOIDCRm),
	},
}

var serverOIDCMappings = []mapping{
	{api: reflect.TypeFor[api.ServerOIDCShowResponse]()},
	{api: reflect.TypeFor[api.IdentityProvider](), cli: reflect.TypeFor[oidcJSON]()},
	{
		api:     reflect.TypeFor[api.ServerOIDCSetResponse](),
		cli:     reflect.TypeFor[oidcSetJSON](),
		omitted: map[string]string{"provider": "its fields are printed at the top level, beside redirect_uri, as koko server oidc show prints them"},
	},
	{api: reflect.TypeFor[api.ServerOIDCRemoveResponse](), cli: reflect.TypeFor[oidcRemoved]()},
}

// oidcJSON is the Identity Provider, as koko server oidc show prints it.
// Its client secret is never printed: client_secret_set says one is kept.
type oidcJSON struct {
	Issuer   string `json:"issuer"`
	ClientID string `json:"client_id"`
	Name     string `json:"name"`
	// RedirectURI is the redirect URI to register at the provider, null
	// while the Web URL is not set.
	RedirectURI     *string `json:"redirect_uri"`
	ClientSecretSet bool    `json:"client_secret_set"`
	SetAt           string  `json:"set_at"`
}

func oidcFromAPI(p api.IdentityProvider, redirect *string) oidcJSON {
	return oidcJSON{
		Issuer: p.Issuer, ClientID: p.ClientID, Name: p.Name, RedirectURI: redirect,
		ClientSecretSet: p.ClientSecretSet, SetAt: p.SetAt,
	}
}

func (p oidcJSON) Human(w io.Writer) error {
	secret := "not set"
	if p.ClientSecretSet {
		secret = "set, never shown"
	}
	_, err := fmt.Fprintf(w, `Name:           %s
Issuer:         %s
Client ID:      %s
Client secret:  %s
Redirect URI:   %s
Set at:         %s
`, p.Name, p.Issuer, p.ClientID, secret, orNone(p.RedirectURI, "none until the Web URL is set"), p.SetAt)
	return err
}

// oidcShown is what koko server oidc show prints: the Identity Provider,
// or null while none is set, which for people names the redirect URI to
// register first.
type oidcShown struct {
	provider *oidcJSON
	redirect *string
}

func (s oidcShown) MarshalJSONTo(enc *jsontext.Encoder) error {
	return json.MarshalEncode(enc, s.provider)
}

// jsonShape is what koko skill documents: the provider, or null.
func (oidcShown) jsonShape() string {
	return shape(reflect.TypeFor[*oidcJSON]())
}

func (s oidcShown) Human(w io.Writer) error {
	if s.provider != nil {
		return s.provider.Human(w)
	}
	if s.redirect == nil {
		_, err := fmt.Fprintln(w, "No Identity Provider is set. Its redirect URI is under the Web URL, which is not set: set it first with koko server set web_url=https://… on the koko server's machine.")
		return err
	}
	_, err := fmt.Fprintf(w, `No Identity Provider is set. Register koko at the provider as a confidential client, with authorization code and PKCE, the scopes openid profile email and this redirect URI:
%s
Then set it with koko server oidc set --issuer <url> --client-id <id> --name <name> --client-secret.
`, *s.redirect)
	return err
}

func serverOIDCShow(ctx context.Context, in *Input) (oidcShown, error) {
	resp, err := in.Call(ctx, api.ServerOIDCShow, api.ServerOIDCShowRequest{})
	if err != nil {
		return oidcShown{}, err
	}
	s := oidcShown{redirect: resp.RedirectURI}
	if resp.Provider != nil {
		s.provider = new(oidcFromAPI(*resp.Provider, resp.RedirectURI))
	}
	return s, nil
}

// oidcSetJSON is what koko server oidc set prints: the Identity Provider as
// it left it, and how many Linked Identities it unlinked.
type oidcSetJSON struct {
	Issuer             string  `json:"issuer"`
	ClientID           string  `json:"client_id"`
	Name               string  `json:"name"`
	RedirectURI        *string `json:"redirect_uri"`
	ClientSecretSet    bool    `json:"client_secret_set"`
	SetAt              string  `json:"set_at"`
	IdentitiesUnlinked int     `json:"identities_unlinked"`
}

func (s oidcSetJSON) Human(w io.Writer) error {
	var b strings.Builder
	fmt.Fprintf(&b, "Set the Identity Provider %s, at %s.\n", s.Name, s.Issuer)
	if s.RedirectURI != nil {
		fmt.Fprintf(&b, "Register this redirect URI at the provider:\n%s\n", *s.RedirectURI)
	}
	b.WriteString("The first sign-in through it checks the client ID and the client secret.\n")
	b.WriteString(unlinkedLine(s.IdentitiesUnlinked))
	_, err := io.WriteString(w, b.String())
	return err
}

// unlinkedLine says how many Linked Identities a change of the Identity
// Provider unlinked.
func unlinkedLine(n int) string {
	switch n {
	case 0:
		return "No Linked Identity was unlinked.\n"
	case 1:
		return "Unlinked 1 Linked Identity, ending the Sign-ins made through it; its User links it again from Account.\n"
	}
	return fmt.Sprintf("Unlinked %d Linked Identities, ending the Sign-ins made through them; each User links theirs again from Account.\n", n)
}

// clientSecretInput is the client secret of koko server oidc set.
var clientSecretInput = SecretInput{
	What:     "the client secret",
	FileFlag: "client-secret-file",
	Stdin:    true,
	// The line ending a file or echo adds, which is taken off.
	Max: api.MaxClientSecretLength + len("\r\n"),
}

func serverOIDCSet(ctx context.Context, in *Input) (oidcSetJSON, error) {
	var req api.ServerOIDCSetRequest
	for flag, field := range map[string]**string{"issuer": &req.Issuer, "client-id": &req.ClientID, "name": &req.Name} {
		if v := in.Flag(flag); v != "" {
			*field = new(v)
		}
	}
	if in.Bool("client-secret") || in.Flag("client-secret-file") != "" {
		secret, err := in.ReadSecret(ctx, clientSecretInput)
		if err != nil {
			return oidcSetJSON{}, err
		}
		secret = bytes.TrimSuffix(bytes.TrimSuffix(secret, []byte("\n")), []byte("\r"))
		if len(secret) == 0 {
			return oidcSetJSON{}, usagef("the client secret cannot be empty: it holds only a line ending")
		}
		req.ClientSecret = new(string(secret))
	}
	if req == (api.ServerOIDCSetRequest{}) {
		return oidcSetJSON{}, usagef("koko server oidc set changes nothing: name --issuer, --client-id, --name or --client-secret")
	}
	resp, err := in.Call(ctx, api.ServerOIDCSet, req)
	if err != nil {
		return oidcSetJSON{}, err
	}
	p := resp.Provider
	return oidcSetJSON{
		Issuer: p.Issuer, ClientID: p.ClientID, Name: p.Name, RedirectURI: &resp.RedirectURI,
		ClientSecretSet: p.ClientSecretSet, SetAt: p.SetAt, IdentitiesUnlinked: resp.IdentitiesUnlinked,
	}, nil
}

// oidcRemoved is what koko server oidc rm prints: how many Linked
// Identities removing the Identity Provider unlinked.
type oidcRemoved struct {
	IdentitiesUnlinked int `json:"identities_unlinked"`
}

func (r oidcRemoved) Human(w io.Writer) error {
	_, err := io.WriteString(w, "Removed the Identity Provider.\n"+unlinkedLine(r.IdentitiesUnlinked))
	return err
}

func serverOIDCRm(ctx context.Context, in *Input) (oidcRemoved, error) {
	resp, err := in.Call(ctx, api.ServerOIDCRemove, api.ServerOIDCRemoveRequest{})
	if err != nil {
		return oidcRemoved{}, err
	}
	return oidcRemoved(resp), nil
}
