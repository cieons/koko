package cli

import (
	"context"
	"fmt"
	"io"
	"reflect"

	"github.com/cieons/koko/internal/shared/api"
)

var userCommands = []Command{
	{
		Noun:      "user",
		Verb:      "invite",
		Summary:   "Create a Member and issue their Invite, valid for 15 minutes",
		Args:      []Arg{{Name: "user"}},
		Tier:      Irreversible, // it prints a credential (ADR 0019)
		OwnerOnly: true,
		Op:        api.UserInvite,
		Run:       action(userInvite),
	},
}

// userJSON is a User in the CLI's own --json.
type userJSON struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Role      string `json:"role"`
	CreatedAt string `json:"created_at"`
}

func userFromAPI(u api.User) userJSON {
	return userJSON{ID: u.ID, Name: u.Name, Role: u.Role, CreatedAt: u.CreatedAt}
}

var userMappings = []mapping{
	{api: reflect.TypeFor[api.UserInviteResponse](), cli: reflect.TypeFor[invitation]()},
	{api: reflect.TypeFor[api.User](), cli: reflect.TypeFor[userJSON]()},
}

// invitation is what user invite prints: the User it created and their
// Invite.
type invitation struct {
	User     userJSON     `json:"user"`
	JoinCode joinCodeJSON `json:"join_code"`
}

func (i invitation) Human(w io.Writer) error {
	if _, err := fmt.Fprintf(w, "Invited %s (%s) as a Member.\n", i.User.Name, i.User.ID); err != nil {
		return err
	}
	return i.JoinCode.writeJoinLine(w)
}

func userInvite(ctx context.Context, in *Input) (invitation, error) {
	resp, err := in.Call(ctx, api.UserInvite, api.UserInviteRequest{Name: in.Arg("user")})
	if err != nil {
		return invitation{}, err
	}
	return invitation{User: userFromAPI(resp.User), JoinCode: joinCodeFromAPI(resp.JoinCode)}, nil
}
