package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/ssh"

	"github.com/cieons/koko/internal/client/devicefiles"
	"github.com/cieons/koko/internal/client/devicemux"
	"github.com/cieons/koko/internal/client/serverconn"
	"github.com/cieons/koko/internal/shared/serveraddr"
)

var connectCommands = []Command{
	{
		Verb:    "connect",
		Summary: "Check that the koko server this device joined answers with its pinned host key, through the Device Mux; given an address, take it as the koko server's new address once it does there",
		Args:    []Arg{{Name: "host:port", Optional: true}},
		Tier:    Reversible,
		// It calls no operation: the koko server's version comes in the
		// report every connection carries (ADR 0027).
		Run: action(connectDevice),
	},
	{
		Verb:    "disconnect",
		Summary: "Stop every Device Mux of this OS user, of every build, ending the SSH Sessions and koko web runs they carry for good",
		Tier:    Reversible,
		Run:     action(disconnect),
	},
}

// connectJSON is what koko connect prints. Every field is always there,
// null when it has no value (ADR 0016).
type connectJSON struct {
	// ServerAddress is the Server Address this device reaches the koko
	// server at, as its client config now holds it.
	ServerAddress string `json:"server_address"`
	// PreviousAddress is the Server Address koko connect <host:port>
	// replaced; null when it replaced none.
	PreviousAddress *string `json:"previous_address"`
	// HostKeys are the fingerprints of the host keys pinned for the koko
	// server, one of which it answered with.
	HostKeys []string `json:"host_keys"`
	// ServerVersion is the koko server's version, as it reported it.
	ServerVersion string `json:"server_version"`
	// DeviceMux is the Device Mux that reached the koko server, as koko
	// status shows it; null on the direct path.
	DeviceMux *deviceMuxJSON `json:"device_mux"`
}

func (c connectJSON) Human(w io.Writer) error {
	var b strings.Builder
	line := func(label, format string, args ...any) {
		fmt.Fprintf(&b, "%-17s"+format+"\n", append([]any{label + ":"}, args...)...)
	}
	if c.PreviousAddress != nil {
		line("Server Address", "%s (was %s)", c.ServerAddress, *c.PreviousAddress)
	} else {
		line("Server Address", "%s", c.ServerAddress)
	}
	line("Host key", "%s, pinned", strings.Join(c.HostKeys, " or "))
	line("koko server", "koko %s", c.ServerVersion)
	if m := c.DeviceMux; m != nil {
		line("Device Mux", "%s since %s (%s)", m.State, m.Since, m.Socket)
	} else {
		line("Device Mux", "none: a Device Connection of its own")
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// connectDevice reaches the koko server this device joined, as any command
// does, starting the Device Mux, and shows what answered, never over the
// Unix socket: the check is of this device's Device Connection. Given an
// address, it first tries that address directly with the device's key and
// pinned host keys, and saves it as the Server Address only once the koko
// server answered there; nothing else moves a device to another address.
// The Device Mux takes the new address at the next Device Connection it
// opens.
func connectDevice(ctx context.Context, in *Input) (connectJSON, error) {
	home := in.inv.home()
	c, key, err := devicefiles.Read(home)
	switch {
	case errors.Is(err, devicefiles.ErrNotJoined):
		return connectJSON{}, notJoined(home, "")
	case err != nil:
		return connectJSON{}, err
	}
	out := connectJSON{ServerAddress: c.ServerAddress, HostKeys: []string{}}
	for _, k := range c.HostKeys {
		out.HostKeys = append(out.HostKeys, ssh.FingerprintSHA256(k))
	}
	if address := in.Arg("host:port"); address != "" {
		if err := checkHostPort(address); err != nil {
			return connectJSON{}, err
		}
		if address != c.ServerAddress {
			if err := in.move(ctx, home, address, c, key); err != nil {
				return connectJSON{}, err
			}
			out.PreviousAddress, out.ServerAddress = &c.ServerAddress, address
		}
	}
	conn, err := in.dialJoined(ctx, "")
	if err != nil {
		return connectJSON{}, err
	}
	defer func() { _ = conn.Close() }()
	report, err := conn.Report(ctx) // here already: dialJoined waited for it
	if err != nil {
		return connectJSON{}, deviceConnError(out.ServerAddress, err)
	}
	out.ServerVersion = report.Version
	if m := in.deviceMux; m != nil {
		out.DeviceMux = m.json()
	}
	return out, nil
}

// checkHostPort refuses an address koko connect does not take: it must be
// a Server Address with its port written, since the device cannot know the
// port the koko server listens on.
func checkHostPort(address string) error {
	if err := serveraddr.Check(address); err != nil {
		return usagef("%q is not an address koko connect takes: %v", address, err)
	}
	if _, _, err := net.SplitHostPort(address); err != nil {
		return usagef("%q names no port: koko connect takes <host:port>, such as koko.example.com:7822", address)
	}
	return nil
}

// move makes address the Server Address of the device whose home is home,
// with the join c and key, once the koko server answered there with a
// pinned host key and took the device's key; otherwise the device keeps
// the address it has, and the error says why.
func (in *Input) move(ctx context.Context, home, address string, c devicefiles.Config, key ssh.Signer) error {
	conn, err := in.dialDirect(ctx, address, c, key)
	if err != nil {
		keeps := "; this device keeps the address " + c.ServerAddress
		if e, ok := errors.AsType[*serverconn.RevokedError](err); ok {
			// A pinned host key answered: it is this device's koko server.
			return revoked(home, "", c, e)
		}
		if e, ok := errors.AsType[*serverconn.HostKeyMismatchError](err); ok {
			return &cliError{code: codeHostKeyMismatch, message: e.Error() +
				": something other than this device's koko server answers there, and koko sent it nothing past the SSH key exchange" + keeps}
		}
		e := deviceConnError(address, err)
		if serverconn.Permanent(err) {
			return fmt.Errorf("%w%s", e, keeps)
		}
		return &cliError{code: codeServerUnreachable, message: fmt.Sprintf("cannot reach the koko server at %s: %v%s", address, err, keeps)}
	}
	_ = conn.Close()
	if err := devicefiles.SetServerAddress(home, address); err != nil {
		return fmt.Errorf("the koko server answered at %s, but koko could not save the address: %w", address, err)
	}
	return nil
}

// disconnectJSON is what koko disconnect prints.
type disconnectJSON struct {
	// DeviceMuxes are the Device Muxes it stopped, of every build and home
	// of this OS user, by socket.
	DeviceMuxes []stoppedDeviceMuxJSON `json:"device_muxes"`
	// CommandsEnded is how many commands they carried in all, which ended:
	// SSH Sessions, koko web runs and calls in flight.
	CommandsEnded int `json:"commands_ended"`
}

// stoppedDeviceMuxJSON is one Device Mux koko disconnect stopped.
type stoppedDeviceMuxJSON struct {
	// Socket is its socket, whose name carries its build.
	Socket string `json:"socket"`
	// CommandsEnded is how many commands were connected to it, which ended.
	CommandsEnded int `json:"commands_ended"`
}

func (d disconnectJSON) Human(w io.Writer) error {
	var b strings.Builder
	if len(d.DeviceMuxes) == 0 {
		b.WriteString("No Device Mux was running.\n")
	}
	for _, m := range d.DeviceMuxes {
		fmt.Fprintf(&b, "Stopped the Device Mux %s, ending %d command(s) connected to it: SSH Sessions, koko web runs and calls in flight.\n",
			strings.TrimSuffix(filepath.Base(m.Socket), ".sock"), m.CommandsEnded)
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// disconnect stops every Device Mux of this OS user, of every build and
// home, which ends what each carried on purpose: they tell each command
// connected that koko disconnect stopped them, so an SSH Session ends for
// good.
func disconnect(ctx context.Context, in *Input) (disconnectJSON, error) {
	out, uncounted, err := stopDeviceMuxes(ctx, in, "koko disconnect")
	if err == nil && uncounted != nil {
		// What it prints is a count, which it does not have.
		return disconnectJSON{}, fmt.Errorf("koko disconnect cannot say how many commands it ended: %w%s", uncounted, out.stopped())
	}
	return out, err
}

// stopDeviceMuxes stops every Device Mux of this OS user, of every build
// and home, for the command named, koko disconnect or koko leave, and says
// what each carried. A socket nothing answers at, such as one a crashed
// Device Mux left, is passed over. One that answers but does not stop
// fails the command, once it has asked every other, saying which it
// stopped. One that stopped but whose answer never came is in uncounted
// (devicemux.ErrUncounted): stopped, but in no count.
func stopDeviceMuxes(ctx context.Context, in *Input, command string) (out disconnectJSON, uncounted, err error) {
	sockets, err := devicemux.Sockets(in.inv.getenv)
	if err != nil {
		return disconnectJSON{}, nil, fmt.Errorf("look for the Device Muxes of this OS user: %w", err)
	}
	out = disconnectJSON{DeviceMuxes: []stoppedDeviceMuxJSON{}}
	var failed, lost []error
	for _, socket := range sockets {
		n, err := devicemux.Stop(ctx, socket, in.runner.Timings)
		switch {
		case errors.Is(err, devicemux.ErrNotRunning):
			continue
		case errors.Is(err, devicemux.ErrUncounted):
			lost = append(lost, err)
			continue
		case err != nil:
			failed = append(failed, err)
			continue
		}
		out.DeviceMuxes = append(out.DeviceMuxes, stoppedDeviceMuxJSON{Socket: socket, CommandsEnded: n})
		out.CommandsEnded += n
	}
	if failed != nil {
		return disconnectJSON{}, nil, fmt.Errorf("%s could not stop every Device Mux: %w%s", command, errors.Join(append(failed, lost...)...), out.stopped())
	}
	return out, errors.Join(lost...), nil
}

// stopped says which Device Muxes d holds were stopped, and how many
// commands each ended, after an error.
func (d disconnectJSON) stopped() string {
	var b strings.Builder
	for _, m := range d.DeviceMuxes {
		fmt.Fprintf(&b, "; it stopped the Device Mux at %s, ending %d command(s)", m.Socket, m.CommandsEnded)
	}
	return b.String()
}
