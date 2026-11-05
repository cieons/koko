package cli

import (
	"context"
	"fmt"
	"io"
	"reflect"

	"github.com/cieons/koko/internal/shared/api"
)

var deviceCommands = []Command{
	{
		Noun:    "device",
		Verb:    "add",
		Summary: "Issue a Join Code for a new device, valid for 15 minutes",
		Args:    []Arg{{Name: "device", Optional: true}},
		Flags: []Flag{
			{
				Name:    "user",
				Value:   "user",
				Summary: "issue it for this User's device instead of your own (the Owner only)",
			},
		},
		// It prints a credential (ADR 0019).
		Tier: Irreversible,
		Op:   api.DeviceAdd,
		Run:  action(deviceAdd),
	},
}

// joinCodeJSON is a Join Code just issued, in the CLI's own --json.
type joinCodeJSON struct {
	ID        string `json:"id"`
	Code      string `json:"code"`
	User      string `json:"user"`
	Device    string `json:"device"`
	ExpiresAt string `json:"expires_at"`
}

func joinCodeFromAPI(jc api.JoinCode) joinCodeJSON {
	return joinCodeJSON{ID: jc.ID, Code: jc.Code, User: jc.User, Device: jc.Device, ExpiresAt: jc.ExpiresAt}
}

// writeJoinLine writes what a person needs of a Join Code: for whom and
// until when, then the one line to paste on the new device.
func (jc joinCodeJSON) writeJoinLine(w io.Writer) error {
	_, err := fmt.Fprintf(w, "On %s's new device %s, before %s, run:\nkoko join %s\n", jc.User, jc.Device, jc.ExpiresAt, jc.Code)
	return err
}

var deviceMappings = []mapping{
	{api: reflect.TypeFor[api.DeviceAddResponse]()},
	{api: reflect.TypeFor[api.JoinCode](), cli: reflect.TypeFor[joinCodeJSON]()},
}

// issuedCode is what device add prints: the Join Code it issued.
type issuedCode joinCodeJSON

func (c issuedCode) Human(w io.Writer) error {
	return joinCodeJSON(c).writeJoinLine(w)
}

func deviceAdd(ctx context.Context, in *Input) (issuedCode, error) {
	resp, err := in.Call(ctx, api.DeviceAdd, api.DeviceAddRequest{Device: in.Arg("device"), User: in.Flag("user")})
	if err != nil {
		return issuedCode{}, err
	}
	return issuedCode(joinCodeFromAPI(resp.JoinCode)), nil
}
