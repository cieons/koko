package cli

import (
	"cmp"
	"context"
	"fmt"
	"io"
	"reflect"
	"text/tabwriter"

	"github.com/cieons/koko/internal/shared/api"
)

// passkeyCommands list and remove Passkeys (ADR 0060). A passkey is made in
// a browser at the Web URL, never from the CLI.
var passkeyCommands = []Command{
	{
		Noun:    "passkey",
		Verb:    "ls",
		Summary: "List your passkeys, which sign you in to the web UI at the Web URL, or another User's for the Owner or an Administrator",
		Flags: []Flag{
			{
				Name:    "user",
				Value:   "user",
				Summary: "list this User's passkeys",
			},
		},
		Examples: []string{
			"koko passkey ls",
			"koko passkey ls --user alice",
		},
		Tier: Read,
		Who:  Anyone,
		Op:   api.PasskeyList,
		Run:  action(passkeyLs),
	},
	{
		Noun:    "passkey",
		Verb:    "rm",
		Summary: "Remove a passkey, signing out the browsers signed in with it",
		Args:    []Arg{{Name: "id"}},
		Examples: []string{
			"koko passkey rm passkey_3f9k2m7q",
		},
		Tier: Irreversible,
		Who:  Anyone,
		Op:   api.PasskeyRemove,
		Run:  action(passkeyRm),
	},
}

var passkeyMappings = []mapping{
	{api: reflect.TypeFor[api.PasskeyListResponse]()},
	{api: reflect.TypeFor[api.Passkey](), cli: reflect.TypeFor[passkeyJSON]()},
	{api: reflect.TypeFor[api.PasskeyRemoveResponse](), cli: reflect.TypeFor[passkeyRemoval]()},
}

// passkeyJSON is a Passkey in the CLI's own --json. Every field is always
// there: added_by is null for one its User made from a Sign-in of their
// own, and last_used_at until it first signs a browser in.
type passkeyJSON struct {
	ID         string  `json:"id"`
	User       string  `json:"user"`
	Name       string  `json:"name"`
	Host       string  `json:"host"`
	Synced     bool    `json:"synced"`
	AddedBy    *string `json:"added_by"`
	AddedAt    string  `json:"added_at"`
	LastUsedAt *string `json:"last_used_at"`
}

func passkeyFromAPI(p api.Passkey) passkeyJSON {
	return passkeyJSON{
		ID: p.ID, User: p.User, Name: p.Name, Host: p.Host, Synced: p.Synced,
		AddedBy: p.AddedBy, AddedAt: p.AddedAt, LastUsedAt: p.LastUsedAt,
	}
}

// passkeyList is what passkey ls prints: every Passkey listed, as a bare
// array.
type passkeyList []passkeyJSON

func (l passkeyList) Human(w io.Writer) error {
	if len(l) == 0 {
		_, err := fmt.Fprintln(w, "No passkeys. A passkey is made in a browser at the Web URL, on Account or through a Sign-in Link.")
		return err
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	// A tabwriter reports a failed write when it flushes.
	_, _ = fmt.Fprintln(tw, "USER\tNAME\tID\tHOST\tSYNCED\tADDED BY\tADDED\tLAST USED")
	for _, p := range l {
		synced := "no"
		if p.Synced {
			synced = "yes"
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", p.User, p.Name, p.ID, p.Host, synced,
			cmp.Or(deref(p.AddedBy), "-"), p.AddedAt, cmp.Or(deref(p.LastUsedAt), "-"))
	}
	return tw.Flush()
}

func passkeyLs(ctx context.Context, in *Input) (passkeyList, error) {
	resp, err := in.Call(ctx, api.PasskeyList, api.PasskeyListRequest{User: in.Flag("user")})
	if err != nil {
		return nil, err
	}
	l := make(passkeyList, 0, len(resp.Passkeys))
	for _, p := range resp.Passkeys {
		l = append(l, passkeyFromAPI(p))
	}
	return l, nil
}

// passkeyRemoval is what passkey rm prints: the Passkey removed, and how
// many Sign-ins made with it ended.
type passkeyRemoval struct {
	Passkey      passkeyJSON `json:"passkey"`
	SignInsEnded int         `json:"signins_ended"`
}

func (r passkeyRemoval) Human(w io.Writer) error {
	ended := "no Sign-in made with it was live"
	switch n := r.SignInsEnded; {
	case n == 1:
		ended = "ended the 1 Sign-in made with it"
	case n > 1:
		ended = fmt.Sprintf("ended the %d Sign-ins made with it", n)
	}
	_, err := fmt.Fprintf(w, "Removed the passkey %s (%s) of %s; %s. The device or password manager that holds it keeps it until it is removed there too.\n",
		r.Passkey.Name, r.Passkey.ID, r.Passkey.User, ended)
	return err
}

func passkeyRm(ctx context.Context, in *Input) (passkeyRemoval, error) {
	resp, err := in.Call(ctx, api.PasskeyRemove, api.PasskeyRemoveRequest{ID: in.Arg("id")})
	if err != nil {
		return passkeyRemoval{}, err
	}
	return passkeyRemoval{Passkey: passkeyFromAPI(resp.Passkey), SignInsEnded: resp.SignInsEnded}, nil
}
