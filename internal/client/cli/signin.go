package cli

import (
	"cmp"
	"context"
	"fmt"
	"io"
	"reflect"
	"slices"
	"text/tabwriter"

	"github.com/cieons/koko/internal/shared/api"
)

var signinCommands = []Command{
	{
		Noun:    "signin",
		Verb:    "link",
		Summary: "Issue a Sign-in Link for a User to add a way to sign in to the web UI, which signs their browser in, valid for 15 minutes unless --valid says otherwise",
		Args:    []Arg{{Name: "user"}},
		Flags:   []Flag{validFlag("the Sign-in Link")},
		Examples: []string{
			"koko signin link alice",
			"koko signin link alice --valid 1h",
		},
		// It prints a credential (ADR 0019).
		Tier: Irreversible,
		Who:  OwnerOrAdministrator,
		Op:   api.PasswordLinkIssue,
		Run:  action(signinLink),
	},
	{
		// The name the command had before the Sign-in Link (ADR 0060),
		// kept with its --json for the scripts that call it.
		Noun:    "password",
		Verb:    "link",
		Summary: "Issue a Sign-in Link, as koko signin link does, printing it under password_link as before",
		Args:    []Arg{{Name: "user"}},
		Flags:   []Flag{validFlag("the Sign-in Link")},
		Examples: []string{
			"koko password link alice",
		},
		Tier: Irreversible,
		Who:  OwnerOrAdministrator,
		Op:   api.PasswordLinkIssue,
		Run:  action(passwordLink),
	},
	{
		Noun:    "password",
		Verb:    "rm",
		Summary: "Remove a User's Password and end the Sign-ins made with it, keeping the User, their devices and their other ways to sign in",
		Args:    []Arg{{Name: "user"}},
		Examples: []string{
			"koko password rm alice",
		},
		Tier: Irreversible,
		Who:  OwnerOrAdministrator,
		Op:   api.PasswordRemove,
		Run:  action(passwordRm),
	},
	{
		Noun:    "signin",
		Verb:    "ls",
		Summary: "List your Sign-ins in browsers, or every User's for the Owner or an Administrator",
		Flags: []Flag{
			{
				Name:    "user",
				Value:   "user",
				Summary: "list only this User's Sign-ins",
			},
			{
				Name:    "all",
				Bool:    true,
				Summary: "add the Sign-ins that have ended",
			},
		},
		Examples: []string{
			"koko signin ls",
			"koko signin ls --user alice --all",
		},
		Tier: Read,
		Who:  Anyone,
		Op:   api.SignInList,
		Run:  action(signinLs),
	},
	{
		Noun:    "signin",
		Verb:    "revoke",
		Summary: "Revoke a Sign-in, signing its browser out at once",
		Args:    []Arg{{Name: "id"}},
		Examples: []string{
			"koko signin revoke signin_3f9k2m7q",
		},
		Tier: Irreversible,
		Who:  Anyone,
		Op:   api.SignInRevoke,
		Run:  action(signinRevoke),
	},
}

var signinMappings = []mapping{
	{api: reflect.TypeFor[api.PasswordLinkResponse](), cli: reflect.TypeFor[passwordLinkJSON]()},
	{api: reflect.TypeFor[api.PasswordLink](), cli: reflect.TypeFor[passwordLinkJSON](), omitted: map[string]string{
		"id":   "the link is shown once and acts on nothing by its id",
		"link": "shown as signin_link, or as password_link by koko password link, beside its user and expires_at",
	}},
	{api: reflect.TypeFor[api.PasswordRemoveResponse](), cli: reflect.TypeFor[passwordRemoval]()},
	{api: reflect.TypeFor[api.SignIn](), cli: reflect.TypeFor[signInJSON](), omitted: map[string]string{
		"browser":    "the CLI names the browser by client, its English label made of browser and system",
		"system":     "the CLI names the browser by client, its English label made of browser and system",
		"credential": "the CLI says how the browser signed in by method; koko passkey ls lists the passkeys themselves",
	}},
	{api: reflect.TypeFor[api.SignInListResponse]()},
	{api: reflect.TypeFor[api.SignInRevokeResponse]()},
}

// signinLinkJSON is a Sign-in Link just issued, in the CLI's own --json.
type signinLinkJSON struct {
	User       string `json:"user"`
	SigninLink string `json:"signin_link"`
	ExpiresAt  string `json:"expires_at"`
}

func (l signinLinkJSON) Human(w io.Writer) error {
	return writeLinkLine(w, l.User, l.ExpiresAt, l.SigninLink)
}

// passwordLinkJSON is a Sign-in Link just issued, as koko password link
// printed a Password Link before it.
type passwordLinkJSON struct {
	User         string `json:"user"`
	PasswordLink string `json:"password_link"`
	ExpiresAt    string `json:"expires_at"`
}

func (l passwordLinkJSON) Human(w io.Writer) error {
	return writeLinkLine(w, l.User, l.ExpiresAt, l.PasswordLink)
}

// writeLinkLine writes what a person needs of a Sign-in Link: for whom and
// until when, then the link to open in their browser.
func writeLinkLine(w io.Writer, user, expiresAt, link string) error {
	_, err := fmt.Fprintf(w, "For %s to sign in, open this Sign-in Link in their browser before %s:\n%s\n", user, expiresAt, link)
	return err
}

// issueLink issues the Sign-in Link the command line asks for.
func issueLink(ctx context.Context, in *Input) (api.PasswordLink, error) {
	validFor, err := in.validFor()
	if err != nil {
		return api.PasswordLink{}, err
	}
	resp, err := in.Call(ctx, api.PasswordLinkIssue, api.PasswordLinkRequest{User: in.Arg("user"), ValidFor: validFor})
	if err != nil {
		return api.PasswordLink{}, err
	}
	return resp.PasswordLink, nil
}

func signinLink(ctx context.Context, in *Input) (signinLinkJSON, error) {
	l, err := issueLink(ctx, in)
	return signinLinkJSON{User: l.User, SigninLink: l.Link, ExpiresAt: l.ExpiresAt}, err
}

func passwordLink(ctx context.Context, in *Input) (passwordLinkJSON, error) {
	l, err := issueLink(ctx, in)
	return passwordLinkJSON{User: l.User, PasswordLink: l.Link, ExpiresAt: l.ExpiresAt}, err
}

// passwordRemoval is what password rm prints: the User whose Password it
// removed.
type passwordRemoval struct {
	User string `json:"user"`
}

func (r passwordRemoval) Human(w io.Writer) error {
	_, err := fmt.Fprintf(w, "Removed %s's Password and ended the Sign-ins made with it. A Sign-in Link (koko signin link %s) lets them set another.\n", r.User, r.User)
	return err
}

func passwordRm(ctx context.Context, in *Input) (passwordRemoval, error) {
	resp, err := in.Call(ctx, api.PasswordRemove, api.PasswordRemoveRequest{User: in.Arg("user")})
	if err != nil {
		return passwordRemoval{}, err
	}
	return passwordRemoval(resp), nil
}

// signInJSON is a Sign-in in the CLI's own --json. Every field is always
// there: ended_at and end_reason are null while it is live, and method is
// null from a koko server that does not say how the browser signed in.
type signInJSON struct {
	ID         string  `json:"id"`
	User       string  `json:"user"`
	Client     string  `json:"client"`
	Method     *string `json:"method"`
	Address    string  `json:"address"`
	StartedAt  string  `json:"started_at"`
	LastUsedAt string  `json:"last_used_at"`
	ExpiresAt  string  `json:"expires_at"`
	EndedAt    *string `json:"ended_at"`
	EndReason  *string `json:"end_reason"`
}

func signInFromAPI(s api.SignIn) signInJSON {
	j := signInJSON{
		ID: s.ID, User: s.User, Client: s.Client, Address: s.Address, StartedAt: s.StartedAt,
		LastUsedAt: s.LastUsedAt, ExpiresAt: s.ExpiresAt,
	}
	if s.Method != "" {
		j.Method = new(s.Method)
	}
	if s.EndedAt != "" {
		j.EndedAt, j.EndReason = new(s.EndedAt), new(s.EndReason)
	}
	return j
}

// signInList is what signin ls prints: every Sign-in listed, as a bare
// array.
type signInList []signInJSON

func (l signInList) Human(w io.Writer) error {
	if len(l) == 0 {
		_, err := fmt.Fprintln(w, "No Sign-ins.")
		return err
	}
	ended := slices.ContainsFunc(l, func(s signInJSON) bool { return s.EndedAt != nil })
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	// A tabwriter reports a failed write when it flushes.
	_, _ = fmt.Fprint(tw, "USER\tCLIENT\tMETHOD\tADDRESS\tID\tSTARTED\tLAST USED\tEXPIRES")
	if ended {
		_, _ = fmt.Fprint(tw, "\tENDED")
	}
	_, _ = fmt.Fprintln(tw)
	for _, s := range l {
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s", s.User, s.Client, cmp.Or(deref(s.Method), "-"), s.Address, s.ID,
			s.StartedAt, s.LastUsedAt, s.ExpiresAt)
		if ended {
			end := "-"
			if s.EndedAt != nil {
				end = *s.EndedAt + " (" + deref(s.EndReason) + ")"
			}
			_, _ = fmt.Fprintf(tw, "\t%s", end)
		}
		_, _ = fmt.Fprintln(tw)
	}
	return tw.Flush()
}

func signinLs(ctx context.Context, in *Input) (signInList, error) {
	resp, err := in.Call(ctx, api.SignInList, api.SignInListRequest{User: in.Flag("user"), All: in.Bool("all")})
	if err != nil {
		return nil, err
	}
	l := make(signInList, 0, len(resp.SignIns))
	for _, s := range resp.SignIns {
		l = append(l, signInFromAPI(s))
	}
	return l, nil
}

// revokedSignIn is what signin revoke prints: the Sign-in as it left it.
type revokedSignIn signInJSON

func (s revokedSignIn) Human(w io.Writer) error {
	if deref(s.EndReason) != api.EndRevoked || deref(s.EndedAt) == "" {
		_, err := fmt.Fprintf(w, "The Sign-in %s of %s (%s) had ended already, at %s (%s).\n", s.ID, s.User, s.Client,
			deref(s.EndedAt), cmp.Or(deref(s.EndReason), "-"))
		return err
	}
	_, err := fmt.Fprintf(w, "Revoked the Sign-in %s of %s (%s): its browser is signed out.\n", s.ID, s.User, s.Client)
	return err
}

func signinRevoke(ctx context.Context, in *Input) (revokedSignIn, error) {
	resp, err := in.Call(ctx, api.SignInRevoke, api.SignInRevokeRequest{ID: in.Arg("id")})
	if err != nil {
		return revokedSignIn{}, err
	}
	return revokedSignIn(signInFromAPI(resp.SignIn)), nil
}
