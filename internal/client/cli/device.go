package cli

import (
	"cmp"
	"context"
	"fmt"
	"io"
	"reflect"
	"slices"
	"text/tabwriter"
	"time"

	"github.com/cieons/koko/internal/client/devicefiles"
	"github.com/cieons/koko/internal/shared/api"
)

var deviceCommands = []Command{
	{
		Noun:    "device",
		Verb:    "add",
		Summary: "Issue a Join Code for a new device, valid for 15 minutes unless --valid says otherwise",
		Args:    []Arg{{Name: "device", Optional: true}},
		Flags: []Flag{
			{
				Name:    "user",
				Value:   "user",
				Summary: "issue it for this User's device instead of your own (the Owner or an Administrator only)",
			},
			validFlag("the Join Code"),
		},
		Examples: []string{
			"koko device add laptop",
			"koko device add --user alice --valid 1h",
		},
		// It prints a credential (ADR 0019).
		Tier: Irreversible,
		Who:  Anyone,
		Op:   api.DeviceAdd,
		Run:  action(deviceAdd),
	},
	{
		Noun:    "device",
		Verb:    "ls",
		Summary: "List your Device Sessions, or every User's for the Owner or an Administrator",
		Flags: []Flag{
			{
				Name:    "user",
				Value:   "user",
				Summary: "list only this User's Device Sessions (the Owner or an Administrator only, for another User)",
			},
			{
				Name:    "all",
				Bool:    true,
				Summary: "add the Revoked Device Sessions",
			},
		},
		Examples: []string{
			"koko device ls",
			"koko device ls --user alice --all",
		},
		Tier: Read,
		Who:  Anyone,
		Op:   api.DeviceList,
		Run:  action(deviceLs),
	},
	{
		Noun:    "device",
		Verb:    "revoke",
		Summary: "Revoke a Device Session, cutting its device off at once",
		Args:    []Arg{{Name: "device", Qualifier: "user", Bare: true}},
		Flags: []Flag{
			{
				Name:    "last",
				Bool:    true,
				Summary: "confirm revoking the Owner's last Device Session",
			},
		},
		Examples: []string{
			"koko device revoke laptop",
			"koko device revoke alice/laptop",
		},
		Tier: Irreversible,
		Who:  Anyone,
		Op:   api.DeviceRevoke,
		Run:  action(deviceRevoke),
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

// deviceSessionJSON is a Device Session in the CLI's own --json. Every field
// is always there: added_by is null for a device the Unix socket added,
// last_used_at null before its first use, and revoked_at null while it is
// live.
type deviceSessionJSON struct {
	ID         string                `json:"id"`
	User       string                `json:"user"`
	Device     string                `json:"device"`
	AddedBy    *deviceSessionRefJSON `json:"added_by"`
	CreatedAt  string                `json:"created_at"`
	LastUsedAt *string               `json:"last_used_at"`
	RevokedAt  *string               `json:"revoked_at"`
}

// deviceSessionRefJSON names another Device Session, in the CLI's own
// --json.
type deviceSessionRefJSON struct {
	ID     string `json:"id"`
	User   string `json:"user"`
	Device string `json:"device"`
}

func deviceSessionFromAPI(d api.DeviceSession) deviceSessionJSON {
	j := deviceSessionJSON{ID: d.ID, User: d.User, Device: d.Device, CreatedAt: d.CreatedAt}
	if d.AddedBy != nil {
		j.AddedBy = &deviceSessionRefJSON{ID: d.AddedBy.ID, User: d.AddedBy.User, Device: d.AddedBy.Device}
	}
	if d.LastUsedAt != "" {
		j.LastUsedAt = new(d.LastUsedAt)
	}
	if d.RevokedAt != "" {
		j.RevokedAt = new(d.RevokedAt)
	}
	return j
}

var deviceMappings = []mapping{
	{api: reflect.TypeFor[api.DeviceAddResponse]()},
	{api: reflect.TypeFor[api.JoinCode](), cli: reflect.TypeFor[joinCodeJSON]()},
	{api: reflect.TypeFor[api.DeviceSession](), cli: reflect.TypeFor[deviceSessionJSON]()},
	{api: reflect.TypeFor[api.DeviceSessionRef](), cli: reflect.TypeFor[deviceSessionRefJSON]()},
	{api: reflect.TypeFor[api.DeviceListResponse]()},
	{api: reflect.TypeFor[api.DeviceRevokeResponse]()},
}

// issuedCode is what device add prints: the Join Code it issued.
type issuedCode joinCodeJSON

func (c issuedCode) Human(w io.Writer) error {
	return joinCodeJSON(c).writeJoinLine(w)
}

func deviceAdd(ctx context.Context, in *Input) (issuedCode, error) {
	validFor, err := in.validFor()
	if err != nil {
		return issuedCode{}, err
	}
	resp, err := in.Call(ctx, api.DeviceAdd, api.DeviceAddRequest{Device: in.Arg("device"), User: in.Flag("user"), ValidFor: validFor})
	if err != nil {
		return issuedCode{}, err
	}
	return issuedCode(joinCodeFromAPI(resp.JoinCode)), nil
}

// deviceList is what device ls prints: every Device Session listed, as a
// bare array.
type deviceList []deviceSessionJSON

func (l deviceList) Human(w io.Writer) error {
	if len(l) == 0 {
		_, err := fmt.Fprintln(w, "No Device Sessions.")
		return err
	}
	revoked := slices.ContainsFunc(l, func(d deviceSessionJSON) bool { return d.RevokedAt != nil })
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	// A tabwriter reports a failed write when it flushes.
	_, _ = fmt.Fprint(tw, "USER\tDEVICE\tID\tADDED BY\tCREATED\tLAST USED")
	if revoked {
		_, _ = fmt.Fprint(tw, "\tREVOKED")
	}
	_, _ = fmt.Fprintln(tw)
	for _, d := range l {
		addedBy := "unix socket"
		if d.AddedBy != nil {
			addedBy = d.AddedBy.User + "/" + d.AddedBy.Device
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s", d.User, d.Device, d.ID, addedBy, d.CreatedAt, cmp.Or(deref(d.LastUsedAt), "never"))
		if revoked {
			_, _ = fmt.Fprintf(tw, "\t%s", cmp.Or(deref(d.RevokedAt), "-"))
		}
		_, _ = fmt.Fprintln(tw)
	}
	return tw.Flush()
}

// deref returns what s points at, or "" for nil.
func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// revokedDevice is what device revoke prints: the Device Session it Revoked.
type revokedDevice struct {
	deviceSessionJSON
	// here is the Server Address this device reached the koko server at,
	// when the Device Session revoked was this device's own; "" otherwise.
	here string
}

func (d revokedDevice) Human(w io.Writer) error {
	if _, err := fmt.Fprintf(w, "Revoked %s's device %s (%s).\n", d.User, d.Device, d.ID); err != nil || d.here == "" {
		return err
	}
	_, err := fmt.Fprintf(w, "It was this device: koko deleted its key and client config; %s\n", rejoin(d.here))
	return err
}

// deviceRevoke revokes a Device Session. When it is the one this device
// reached the koko server with, the koko server closes the connection once
// it has answered, and the device runs the revocation cleanup at once.
func deviceRevoke(ctx context.Context, in *Input) (revokedDevice, error) {
	of, device := in.Qualified("device")
	resp, err := in.Call(ctx, api.DeviceRevoke, api.DeviceRevokeRequest{User: of, Device: device, Last: in.Bool("last")})
	if err != nil {
		return revokedDevice{}, err
	}
	d := revokedDevice{deviceSessionJSON: deviceSessionFromAPI(resp.DeviceSession)}
	if c := in.joined; c != nil && c.DeviceSession == d.ID {
		if err := devicefiles.Revoke(in.inv.home(), *c, time.Now()); err != nil {
			return revokedDevice{}, api.CodeRevoked.Errorf("revoked %s's device %s, which is this device, but koko could not finish clearing its files of it: %v", d.User, d.Device, err)
		}
		d.here = c.ServerAddress
	}
	return d, nil
}

func deviceLs(ctx context.Context, in *Input) (deviceList, error) {
	resp, err := in.Call(ctx, api.DeviceList, api.DeviceListRequest{User: in.Flag("user"), All: in.Bool("all")})
	if err != nil {
		return nil, err
	}
	l := make(deviceList, 0, len(resp.DeviceSessions))
	for _, d := range resp.DeviceSessions {
		l = append(l, deviceSessionFromAPI(d))
	}
	return l, nil
}
