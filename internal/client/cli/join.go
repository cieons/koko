package cli

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"

	"golang.org/x/crypto/ssh"

	"github.com/cieons/koko/internal/client/apiclient"
	"github.com/cieons/koko/internal/client/devicefiles"
	"github.com/cieons/koko/internal/client/serverconn"
	"github.com/cieons/koko/internal/shared/api"
	"github.com/cieons/koko/internal/shared/joincode"
)

var joinCommands = []Command{
	{
		Verb:    "join",
		Summary: "Join this device to a koko server with a Join Code, or with - to read the code from stdin",
		Args:    []Arg{{Name: "join-code"}},
		Flags: []Flag{{
			Name:    "no-ssh-config",
			Bool:    true,
			Summary: "leave ~/.ssh/config alone: ssh then reads koko's ssh_config only once you include it",
		}},
		// It spends a one-time Join Code (P0 spec: overview).
		Tier: Irreversible,
		// The join itself is SSH authentication, not an operation; once
		// joined, koko join asks who the device now is.
		Op:  api.UserCurrent,
		Run: action(join),
	},
}

// joined is what join prints: the koko server this device joined and the
// Device Session it became.
type joined struct {
	ServerAddress string            `json:"server_address"`
	DeviceSession deviceSessionJSON `json:"device_session"`
	// IncludeWarning is status's include_warning after the join: why ssh
	// does not read koko's ssh_config for every host, such as an Include
	// line koko could not add, and how to fix it. Null when it does, or
	// --no-ssh-config left ~/.ssh/config alone on purpose.
	IncludeWarning *string `json:"include_warning"`
}

func (j joined) Human(w io.Writer) error {
	if _, err := fmt.Fprintf(w, "Joined the koko server at %s as %s's device %s.\n",
		j.ServerAddress, j.DeviceSession.User, j.DeviceSession.Device); err != nil || j.IncludeWarning == nil {
		return err
	}
	_, err := fmt.Fprintf(w, "%s.\n", *j.IncludeWarning)
	return err
}

var joinMappings = []mapping{
	{
		api: reflect.TypeFor[api.UserCurrentResponse](),
		cli: reflect.TypeFor[joined](),
		omitted: map[string]string{
			"user":   "join prints the device's Device Session, which names its User",
			"signin": "a join calls on its new Device Session, never through a Sign-in",
		},
	},
}

// join joins this device to the koko server its Join Code names (P0 spec:
// people and Spaces, "Joining"). A failed join leaves the device as it was.
func join(ctx context.Context, in *Input) (joined, error) {
	code, err := in.joinCode(ctx)
	if err != nil {
		return joined{}, err
	}
	home := in.inv.home()
	switch c, _, err := devicefiles.Read(home); {
	case err == nil:
		return joined{}, &cliError{code: codeAlreadyJoined, message: fmt.Sprintf(
			"this device has joined the koko server at %s already, as %s's device %s: to join again, revoke this device's Device Session first (koko device revoke %s), or, if that koko server no longer knows this device, run koko leave",
			c.ServerAddress, c.User, c.Device, c.Device)}
	case !errors.Is(err, devicefiles.ErrNotJoined):
		return joined{}, fmt.Errorf("cannot tell whether this device has joined a koko server: %w", err)
	}

	_, key, _ := ed25519.GenerateKey(rand.Reader) // crypto/rand never fails
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		return joined{}, fmt.Errorf("make this device's key: %w", err)
	}
	staged, err := devicefiles.Stage(home, key)
	if err != nil {
		return joined{}, fmt.Errorf("cannot keep this device's files, so koko did not connect and the Join Code stays usable: %w", err)
	}
	// It waits for the koko server as any command does, but tries once: a
	// join cut after the koko server took the code cannot join again.
	dialing, cancel := context.WithTimeout(ctx, in.runner.Timings.Resolve().CommandWait)
	defer cancel()
	conn, hostKey, err := serverconn.Join(dialing, code.Address, code.Fingerprint, signer, code.Secret)
	if err != nil {
		staged.Abort()
		return joined{}, joinError(code.Address, err)
	}
	// The call goes on the connection that joined, which the client then
	// owns; sent again after a drop, on a Device Connection of the key the
	// join made a Device Session, which waits for the koko server as the
	// direct path does. A connection that joined but sends no report is
	// such a drop.
	joining := conn
	switch report, err := conn.Report(dialing); {
	case err != nil:
		_ = conn.Close()
		joining = nil
	default:
		// The join is done by now, in SSH authentication, but nothing
		// more is sent to a koko server this CLI does not work with.
		if err := in.window(report, "at "+code.Address); err != nil {
			_ = conn.Close()
			staged.Abort()
			_, e := classify(err)
			return joined{}, &cliError{code: e.Code, message: fmt.Sprintf(
				"the Join Code joined this device, but %s; the code is spent: ask for a new one once that is done, and revoke the Device Session this join left, which koko device ls lists", e.Message)}
		}
	}
	c := apiclient.New(func(ctx context.Context) (serverconn.Conn, error) {
		if first := joining; first != nil {
			joining = nil
			return first, nil
		}
		again, err := in.dialDirect(ctx, code.Address, devicefiles.Config{HostKeys: []ssh.PublicKey{hostKey}}, signer)
		if err != nil {
			return nil, deviceConnError(code.Address, err)
		}
		return again, nil
	})
	defer c.Close()
	who, err := c.Call(ctx, api.UserCurrent, api.UserCurrentRequest{})
	if err == nil && who.DeviceSession == nil {
		err = errors.New("the koko server named no Device Session")
	}
	if err != nil {
		staged.Abort()
		return joined{}, fmt.Errorf("the Join Code joined this device, but the koko server did not say as whom (%w); the code is spent: ask for a new one, and revoke the Device Session this join left, which koko device ls lists", err)
	}
	d := *who.DeviceSession
	err = staged.Commit(devicefiles.Config{
		ServerAddress: code.Address,
		HostKeys:      []ssh.PublicKey{hostKey},
		User:          d.User,
		Device:        d.Device,
		DeviceSession: d.ID,
	})
	if err != nil {
		return joined{}, fmt.Errorf("the Join Code joined this device as %s's device %s, but koko could not keep its files (%w); the code is spent: ask for a new one, and revoke %s/%s", d.User, d.Device, err, d.User, d.Device)
	}
	out := joined{ServerAddress: code.Address, DeviceSession: deviceSessionFromAPI(d)}
	writeBareNames(ctx, c, home, in.inv.Stderr)
	in.startDeviceMux(ctx)
	// The device has joined: ssh not reading koko's ssh_config yet changes
	// nothing about that, so the join says why and goes on. A line it finds
	// below a Host or Match line it leaves there, and warns as status does.
	if !in.Bool("no-ssh-config") {
		out.IncludeWarning = addInclude(home)
	}
	return out, nil
}

// addInclude puts the Include line first in ~/.ssh/config under home, then
// checks it as status does, for koko join and koko ssh setup, and returns
// the warning for a file it left alone or a line ssh does not read for
// every host: why, and how to fix it. It returns nil when ssh reads koko's
// ssh_config for every host.
func addInclude(home string) *string {
	err := devicefiles.AddInclude(home)
	if err == nil {
		err = devicefiles.CheckInclude(home)
	}
	if err != nil {
		return new(err.Error())
	}
	return nil
}

// joinCode reads the Join Code: the argument, or stdin for "-", which keeps
// the code out of shell history. A code that cannot be read is refused
// before any connection, with exit 2.
func (in *Input) joinCode(ctx context.Context) (joincode.Code, error) {
	text := in.Arg("join-code")
	if text == "-" {
		b, err := in.ReadSecret(ctx, SecretInput{What: "the Join Code", Stdin: true})
		if err != nil {
			return joincode.Code{}, err
		}
		text = strings.TrimSpace(string(b))
	}
	code, err := joincode.Decode(text)
	if err != nil {
		return joincode.Code{}, usagef("cannot read the Join Code: %v", err)
	}
	return code, nil
}

// joinError gives a join that failed at the koko server at address the
// CLI's code for it.
func joinError(address string, err error) error {
	if e, ok := errors.AsType[*serverconn.HostKeyMismatchError](err); ok {
		return &cliError{code: codeHostKeyMismatch, message: fmt.Sprintf(
			"the koko server at %s presented the host key %s, not the %s the Join Code names; koko sent nothing, so the code stays usable",
			address, e.Presented, e.Pinned[0])}
	}
	if e, ok := errors.AsType[*serverconn.RefusedError](err); ok {
		return &cliError{code: codeInvalidJoinCode, message: e.Error()}
	}
	return &cliError{code: codeServerUnreachable, message: fmt.Sprintf(
		"cannot reach the koko server at %s, which the Join Code names: %v; the code stays usable", address, err)}
}
