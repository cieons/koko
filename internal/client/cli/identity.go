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

// identityCommands list and unlink Linked Identities (ADR 0060). An identity
// is linked in a browser at the Web URL, never from the CLI.
var identityCommands = []Command{
	{
		Noun:    "identity",
		Verb:    "ls",
		Summary: "List your Linked Identity at the Identity Provider, which signs you in to the web UI, or another User's for the Owner or an Administrator",
		Flags: []Flag{
			{
				Name:    "user",
				Value:   "user",
				Summary: "list this User's Linked Identity",
			},
		},
		Examples: []string{
			"koko identity ls",
			"koko identity ls --user alice",
		},
		Tier: Read,
		Who:  Anyone,
		Op:   api.IdentityList,
		Run:  action(identityLs),
	},
	{
		Noun:    "identity",
		Verb:    "unlink",
		Summary: "Unlink a User's identity at the Identity Provider, signing out the browsers signed in through it",
		Args:    []Arg{{Name: "user"}},
		Examples: []string{
			"koko identity unlink alice",
		},
		Tier: Irreversible,
		Who:  Anyone,
		Op:   api.IdentityUnlink,
		Run:  action(identityUnlink),
	},
}

var identityMappings = []mapping{
	{api: reflect.TypeFor[api.IdentityListResponse]()},
	{api: reflect.TypeFor[api.Identity](), cli: reflect.TypeFor[identityJSON]()},
	{api: reflect.TypeFor[api.IdentityUnlinkResponse](), cli: reflect.TypeFor[identityUnlinked]()},
}

// identityJSON is a Linked Identity in the CLI's own --json. Every field is
// always there: linked_by is null for one its User linked from a Sign-in
// of their own, and last_used_at until it first signs a browser in.
type identityJSON struct {
	ID         string  `json:"id"`
	User       string  `json:"user"`
	Issuer     string  `json:"issuer"`
	Subject    string  `json:"subject"`
	Label      string  `json:"label"`
	LinkedBy   *string `json:"linked_by"`
	LinkedAt   string  `json:"linked_at"`
	LastUsedAt *string `json:"last_used_at"`
}

func identityFromAPI(i api.Identity) identityJSON {
	return identityJSON{
		ID: i.ID, User: i.User, Issuer: i.Issuer, Subject: i.Subject, Label: i.Label,
		LinkedBy: i.LinkedBy, LinkedAt: i.LinkedAt, LastUsedAt: i.LastUsedAt,
	}
}

// identityList is what identity ls prints: every Linked Identity listed, as
// a bare array.
type identityList []identityJSON

func (l identityList) Human(w io.Writer) error {
	if len(l) == 0 {
		_, err := fmt.Fprintln(w, "No Linked Identity. An identity is linked in a browser at the Web URL, on Account or through a Sign-in Link.")
		return err
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	// A tabwriter reports a failed write when it flushes.
	_, _ = fmt.Fprintln(tw, "USER\tLABEL\tID\tISSUER\tSUBJECT\tLINKED BY\tLINKED\tLAST USED")
	for _, i := range l {
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", i.User, i.Label, i.ID, i.Issuer, i.Subject,
			cmp.Or(deref(i.LinkedBy), "-"), i.LinkedAt, cmp.Or(deref(i.LastUsedAt), "-"))
	}
	return tw.Flush()
}

func identityLs(ctx context.Context, in *Input) (identityList, error) {
	resp, err := in.Call(ctx, api.IdentityList, api.IdentityListRequest{User: in.Flag("user")})
	if err != nil {
		return nil, err
	}
	l := make(identityList, 0, len(resp.Identities))
	for _, i := range resp.Identities {
		l = append(l, identityFromAPI(i))
	}
	return l, nil
}

// identityUnlinked is what identity unlink prints: the Linked Identity
// unlinked, and how many Sign-ins made through it ended.
type identityUnlinked struct {
	Identity     identityJSON `json:"identity"`
	SignInsEnded int          `json:"signins_ended"`
}

func (u identityUnlinked) Human(w io.Writer) error {
	ended := "no Sign-in made through it was live"
	switch n := u.SignInsEnded; {
	case n == 1:
		ended = "ended the 1 Sign-in made through it"
	case n > 1:
		ended = fmt.Sprintf("ended the %d Sign-ins made through it", n)
	}
	_, err := fmt.Fprintf(w, "Unlinked the identity %s (%s) of %s; %s.\n", u.Identity.Label, u.Identity.ID, u.Identity.User, ended)
	return err
}

func identityUnlink(ctx context.Context, in *Input) (identityUnlinked, error) {
	resp, err := in.Call(ctx, api.IdentityUnlink, api.IdentityUnlinkRequest{User: in.Arg("user")})
	if err != nil {
		return identityUnlinked{}, err
	}
	return identityUnlinked{Identity: identityFromAPI(resp.Identity), SignInsEnded: resp.SignInsEnded}, nil
}
