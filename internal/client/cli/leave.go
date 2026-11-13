package cli

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/cieons/koko/internal/client/devicefiles"
)

var leaveCommands = []Command{
	{
		Verb:    "leave",
		Summary: "Forget the koko server this device joined, without contacting it, so that the device can join again: stop every Device Mux of this OS user and delete the device's key and files of that koko server",
		// The device's key is gone for good, and with it the Device
		// Session's only use (ADR 0019).
		Tier: Irreversible,
		Who:  Anyone,
		// It calls no operation: it never contacts the koko server, which
		// may be gone.
		Run: action(leave),
	},
}

// leftJSON is what koko leave prints: the Device Session it left behind,
// at the koko server it belongs to.
type leftJSON struct {
	// ServerAddress is the Server Address of the koko server the device
	// left.
	ServerAddress string `json:"server_address"`
	// DeviceSession is the device's Device Session there, which no device
	// can use any more, but which the koko server still lists until it is
	// revoked.
	DeviceSession deviceSessionRefJSON `json:"device_session"`
}

func (l leftJSON) Human(w io.Writer) error {
	d := l.DeviceSession
	_, err := fmt.Fprintf(w, "Left the koko server at %s. Its Device Session %s/%s (%s) stays there, but no device can use it any more, since koko deleted its key: if that koko server still runs, revoke it from another device or on the koko server's machine with koko device revoke %s/%s.\n",
		l.ServerAddress, d.User, d.Device, d.ID, d.User, d.Device)
	return err
}

// leave forgets the koko server this device joined, on the device alone,
// so that it can join again, as after the koko server was initialised
// anew: koko join refuses while the device is joined, and a koko server
// that no longer knows the device cannot revoke its Device Session. It
// stops every Device Mux first, as koko disconnect does, so none of them
// writes into the device's files afterwards, then deletes them
// (devicefiles.Leave). A Device Mux that does not stop leaves the device
// joined.
func leave(ctx context.Context, in *Input) (leftJSON, error) {
	home := in.inv.home()
	c, err := devicefiles.ReadConfig(home)
	switch {
	case errors.Is(err, devicefiles.ErrNotJoined):
		return leftJSON{}, notJoined(home, "")
	case err != nil:
		return leftJSON{}, err
	}
	// It needs each stopped, not what each ended.
	if _, _, err := stopDeviceMuxes(ctx, in, "koko leave"); err != nil {
		return leftJSON{}, fmt.Errorf("%w; this device is still joined to the koko server at %s, and koko deleted none of its files", err, c.ServerAddress)
	}
	if err := devicefiles.Leave(home); err != nil {
		return leftJSON{}, err
	}
	return leftJSON{
		ServerAddress: c.ServerAddress,
		DeviceSession: deviceSessionRefJSON{ID: c.DeviceSession, User: c.User, Device: c.Device},
	}, nil
}
