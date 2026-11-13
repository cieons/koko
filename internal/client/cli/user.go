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

var userCommands = []Command{
	{
		Noun:    "user",
		Verb:    "invite",
		Summary: "Create a Member and issue their Invite, valid for 15 minutes unless --valid says otherwise",
		Args:    []Arg{{Name: "user"}},
		Flags: []Flag{
			{
				Name:    "web",
				Bool:    true,
				Summary: "issue a Password Link, for a person who will use the web UI in a browser, instead of a Join Code",
			},
			validFlag("the Invite"),
		},
		Examples: []string{
			"koko user invite alice",
			"koko user invite alice --web --valid 1d",
		},
		Tier: Irreversible, // it prints a credential (ADR 0019)
		Who:  OwnerOrAdministrator,
		Op:   api.UserInvite,
		Run:  action(userInvite),
	},
	{
		Noun:    "user",
		Verb:    "ls",
		Summary: "List the Users",
		Flags: []Flag{
			{
				Name:    "all",
				Bool:    true,
				Summary: "add the Removed Users",
			},
		},
		Examples: []string{
			"koko user ls",
			"koko user ls --all",
		},
		Tier: Read,
		Who:  OwnerOrAdministrator,
		Op:   api.UserList,
		Run:  action(userLs),
	},
	{
		Noun:    "user",
		Verb:    "remove",
		Summary: "Delete a User who never joined, or Remove anyone else, revoking all their access",
		Args:    []Arg{{Name: "user"}},
		Examples: []string{
			"koko user remove alice",
		},
		Tier: Irreversible,
		Who:  OwnerOrAdministrator,
		Op:   api.UserRemove,
		Run:  action(userRemove),
	},
	{
		Noun:    "user",
		Verb:    "promote",
		Summary: "Make a Member an Administrator, who sees and acts in every Space and manages every Member",
		Args:    []Arg{{Name: "user"}},
		Examples: []string{
			"koko user promote alice",
		},
		// It hands over every Space and Secret, which no demotion takes
		// back (ADR 0056).
		Tier: Irreversible,
		Who:  OwnerAlone,
		Op:   api.UserPromote,
		Run:  action(userPromote),
	},
	{
		Noun:    "user",
		Verb:    "demote",
		Summary: "Make an Administrator a Member again, ending their SSH Sessions outside their own Spaces",
		Args:    []Arg{{Name: "user"}},
		Examples: []string{
			"koko user demote alice",
		},
		Tier: Reversible,
		Who:  OwnerAlone,
		Op:   api.UserDemote,
		Run:  action(userDemote),
	},
}

// userJSON is a User in the CLI's own --json. removed_at is null while the
// User belongs to the koko server, and password_set_at while they have no
// Password.
type userJSON struct {
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	Role          string  `json:"role"`
	CreatedAt     string  `json:"created_at"`
	RemovedAt     *string `json:"removed_at"`
	PasswordSetAt *string `json:"password_set_at"`
}

func userFromAPI(u api.User) userJSON {
	j := userJSON{ID: u.ID, Name: u.Name, Role: u.Role, CreatedAt: u.CreatedAt}
	if u.RemovedAt != "" {
		j.RemovedAt = new(u.RemovedAt)
	}
	if u.PasswordSetAt != "" {
		j.PasswordSetAt = new(u.PasswordSetAt)
	}
	return j
}

var userMappings = []mapping{
	{api: reflect.TypeFor[api.UserInviteResponse](), cli: reflect.TypeFor[invitation]()},
	{api: reflect.TypeFor[api.User](), cli: reflect.TypeFor[userJSON]()},
	{api: reflect.TypeFor[api.UserListResponse]()},
	{api: reflect.TypeFor[api.UserRemoveResponse](), cli: reflect.TypeFor[userRemoval]()},
	{api: reflect.TypeFor[api.UserPromoteResponse]()},
	{api: reflect.TypeFor[api.UserDemoteResponse]()},
}

// userList is what user ls prints: every User listed, as a bare array.
type userList []userJSON

func (l userList) Human(w io.Writer) error {
	removed := slices.ContainsFunc(l, func(u userJSON) bool { return u.RemovedAt != nil })
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	// A tabwriter reports a failed write when it flushes.
	_, _ = fmt.Fprint(tw, "NAME\tROLE\tID\tCREATED\tPASSWORD")
	if removed {
		_, _ = fmt.Fprint(tw, "\tREMOVED")
	}
	_, _ = fmt.Fprintln(tw)
	for _, u := range l {
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s", u.Name, u.Role, u.ID, u.CreatedAt, cmp.Or(deref(u.PasswordSetAt), "-"))
		if removed {
			_, _ = fmt.Fprintf(tw, "\t%s", cmp.Or(deref(u.RemovedAt), "-"))
		}
		_, _ = fmt.Fprintln(tw)
	}
	return tw.Flush()
}

func userLs(ctx context.Context, in *Input) (userList, error) {
	resp, err := in.Call(ctx, api.UserList, api.UserListRequest{All: in.Bool("all")})
	if err != nil {
		return nil, err
	}
	l := make(userList, 0, len(resp.Users))
	for _, u := range resp.Users {
		l = append(l, userFromAPI(u))
	}
	return l, nil
}

// invitation is what user invite prints: the User it created and their
// Invite, a Join Code, or with --web a Password Link and its expiry.
type invitation struct {
	User         userJSON      `json:"user"`
	JoinCode     *joinCodeJSON `json:"join_code,omitzero"`
	PasswordLink string        `json:"password_link,omitzero"`
	ExpiresAt    string        `json:"expires_at,omitzero"`
}

func (i invitation) Human(w io.Writer) error {
	if _, err := fmt.Fprintf(w, "Invited %s (%s) as a Member.\n", i.User.Name, i.User.ID); err != nil {
		return err
	}
	if i.JoinCode == nil {
		return writeLinkLine(w, i.User.Name, i.ExpiresAt, i.PasswordLink)
	}
	return i.JoinCode.writeJoinLine(w)
}

func userInvite(ctx context.Context, in *Input) (invitation, error) {
	validFor, err := in.validFor()
	if err != nil {
		return invitation{}, err
	}
	resp, err := in.Call(ctx, api.UserInvite, api.UserInviteRequest{Name: in.Arg("user"), ValidFor: validFor, Web: in.Bool("web")})
	if err != nil {
		return invitation{}, err
	}
	i := invitation{User: userFromAPI(resp.User)}
	switch {
	case resp.PasswordLink != nil:
		i.PasswordLink, i.ExpiresAt = resp.PasswordLink.Link, resp.PasswordLink.ExpiresAt
	case resp.JoinCode != nil:
		i.JoinCode = new(joinCodeFromAPI(*resp.JoinCode))
	}
	return i, nil
}

// userRemoval is what user remove prints: what it did, "deleted" or
// "removed", and the User as it left them.
type userRemoval struct {
	Outcome string   `json:"outcome"`
	User    userJSON `json:"user"`
}

func (r userRemoval) Human(w io.Writer) error {
	u := r.User
	if r.Outcome == api.OutcomeDeleted {
		_, err := fmt.Fprintf(w, "Deleted %s (%s), who never joined; the name %s is free again.\n", u.Name, u.ID, u.Name)
		return err
	}
	_, err := fmt.Fprintf(w, "Removed %s (%s): their Device Sessions are revoked and they are in no Space. "+
		"Their Personal Space %s is now a Space with no Members that only the Owner and the Administrators see.\n", u.Name, u.ID, u.Name)
	return err
}

func userRemove(ctx context.Context, in *Input) (userRemoval, error) {
	resp, err := in.Call(ctx, api.UserRemove, api.UserRemoveRequest{Name: in.Arg("user")})
	if err != nil {
		return userRemoval{}, err
	}
	return userRemoval{Outcome: resp.Outcome, User: userFromAPI(resp.User)}, nil
}

// roleChange is what user promote and user demote print: the User as the
// call left them, in user ls's shape.
type roleChange userJSON

func (r roleChange) Human(w io.Writer) error {
	if r.Role == api.RoleAdministrator {
		_, err := fmt.Fprintf(w, "%s (%s) is an Administrator: they see and act in every Space, read every Secret and manage every Member.\n", r.Name, r.ID)
		return err
	}
	_, err := fmt.Fprintf(w, "%s (%s) is a Member: they act in their Personal Space and the Spaces they are a Member of.\n", r.Name, r.ID)
	return err
}

func userPromote(ctx context.Context, in *Input) (roleChange, error) {
	resp, err := in.Call(ctx, api.UserPromote, api.UserPromoteRequest{Name: in.Arg("user")})
	if err != nil {
		return roleChange{}, err
	}
	return roleChange(userFromAPI(resp.User)), nil
}

func userDemote(ctx context.Context, in *Input) (roleChange, error) {
	resp, err := in.Call(ctx, api.UserDemote, api.UserDemoteRequest{Name: in.Arg("user")})
	if err != nil {
		return roleChange{}, err
	}
	return roleChange(userFromAPI(resp.User)), nil
}
