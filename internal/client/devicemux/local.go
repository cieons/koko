package devicemux

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/cieons/koko/internal/client/devicefiles"
	"github.com/cieons/koko/internal/client/serverconn"
)

// ErrNotRunning says no Device Mux answers at a socket: none runs, one is
// starting or stopping, or what answers is not one.
var ErrNotRunning = errors.New("no Device Mux answers")

// ErrOtherHome says the Device Mux at a socket carries the device of
// another home of the same OS user, so a command of this home opens its own
// Device Connection.
var ErrOtherHome = errors.New("the Device Mux of this build carries the device of another home")

// State is a Device Mux's state as a command finds it on connecting.
type State struct {
	// Connected tells that it holds a Device Connection, opened at Since.
	// Otherwise it has been reconnecting since Since, and Err is why its last
	// try failed: one of the errors serverconn.DialDevice fails with, or
	// devicefiles.ErrNotJoined when the device's join is gone.
	Connected bool
	Since     time.Time
	Err       error
	// Resuming counts the Tunnels through it that wait to Resume.
	Resuming int
}

// The local protocol between a command and the Device Mux is the koko
// server's own SSH, as on its Unix socket (P0 spec: reach), with the state
// in the banner:
//
//   - A command connects to the Device Mux's socket. The Device Mux admits
//     only a peer of its own OS user (peerUID), and the socket sits in a
//     directory only that user can write, so the command pins no host key.
//   - The SSH user name is the command's HOME: a device is an OS user's
//     home, and the Device Mux carries only its own. To a command of
//     another home it answers "elsewhere" at once, and that command opens
//     its own Device Connection.
//   - In the SSH authentication, which is "none", the Device Mux sends its
//     state as the banner (stateJSON), once it has a Device Connection or
//     its try to open one has failed. It accepts the authentication only
//     when it is connected.
//   - It then sends the report of its Device Connection as the koko server
//     does (wire.RequestReport), and again, on every open connection,
//     whenever it opens a new Device Connection.
//   - It carries every channel the command opens, of any type but
//     channelResuming, onto its Device Connection with the same open
//     payload, unparsed, and passes a refusal back with the koko server's
//     reason and message. Its bytes go both ways, half-closes included,
//     until either end closes. A channel open while it holds no Device
//     Connection waits for its next one.
//   - A command holds a channelResuming open on it while one of its Tunnels
//     waits to Resume (Conn.WaitingToResume). The Device Mux carries it
//     nowhere, and counts it in its state, for koko status.
//   - When the koko server revokes its Device Session, it sends each open
//     connection wire.RequestRevoked before it closes it, and when koko
//     disconnect stops it, wire.RequestDisconnected.
//
// Only commands of the same build reach a Device Mux, so this needs no
// compatibility rules, but for one exchange, which koko disconnect has with
// a Device Mux of any build and which therefore never changes: a
// connection whose SSH user name is disconnectUser, which no home is, asks
// the Device Mux to stop, and the Device Mux answers in the banner,
// {"state": "stopped", "commands": <n>}, the number of commands connected
// to it, which it ends (Stop).

// disconnectUser is the SSH user name with which koko disconnect asks a
// Device Mux to stop. A home is an absolute path, so it is never one.
const disconnectUser = "disconnect"

// channelResuming is the channel a command holds open on the Device Mux
// while one of its Tunnels waits to Resume.
const channelResuming = "resuming@koko"

// stateJSON is the banner a Device Mux sends: its state.
type stateJSON struct {
	// State is "connected", "reconnecting", "elsewhere" to a command of
	// another home, or "stopped" to koko disconnect.
	State string `json:"state"`
	// Since is when it connected, or began reconnecting, RFC 3339 in UTC.
	Since string `json:"since,omitempty"`
	// Error is why its last try failed, while it reconnects.
	Error *errorJSON `json:"error,omitzero"`
	// Home is the home whose device it carries, when it is elsewhere.
	Home string `json:"home,omitempty"`
	// Commands is how many commands were connected to it when koko
	// disconnect stopped it, which it ends.
	Commands *int `json:"commands,omitzero"`
	// Resuming counts the Tunnels through it that wait to Resume, when
	// connected or reconnecting.
	Resuming int `json:"resuming,omitzero"`
}

// encode writes sj as a banner.
func (sj stateJSON) encode() string {
	b, err := json.Marshal(sj)
	if err != nil {
		panic(fmt.Sprintf("devicemux: encode the state: %v", err)) // strings and numbers only: never fails
	}
	return string(b)
}

// elsewhere is the banner a Device Mux answers a command of another home
// with: the home whose device it carries.
func elsewhere(home string) string {
	return stateJSON{State: "elsewhere", Home: home}.encode()
}

// stopped is the banner a Device Mux answers koko disconnect with: it
// stopped, ending the n commands connected to it.
func stopped(n int) string {
	return stateJSON{State: "stopped", Commands: &n}.encode()
}

// errorJSON is an error serverconn.DialDevice fails with, or
// devicefiles.ErrNotJoined, as stateJSON carries it, by its kind.
type errorJSON struct {
	// Kind is host_key_mismatch, unknown_key, revoked, refused, not_joined
	// or failed, the last for any other error, such as a koko server it
	// cannot reach.
	Kind    string `json:"kind"`
	Message string `json:"message"`
	// Address, Presented and Pinned are a host_key_mismatch's.
	Address   string   `json:"address,omitempty"`
	Presented string   `json:"presented,omitempty"`
	Pinned    []string `json:"pinned,omitempty"`
	// Device is the Revoked Device Session's device name.
	Device string `json:"device,omitempty"`
}

// banner encodes s as the banner a Device Mux sends.
func (s State) banner() string {
	sj := stateJSON{State: "reconnecting", Since: s.Since.UTC().Format(time.RFC3339Nano), Resuming: s.Resuming}
	if s.Connected {
		sj.State = "connected"
	} else {
		sj.Error = encodeError(s.Err)
	}
	return sj.encode()
}

func encodeError(err error) *errorJSON {
	if err == nil {
		err = errors.New("the Device Mux holds no Device Connection")
	}
	e := &errorJSON{Kind: "failed", Message: err.Error()}
	if m, ok := errors.AsType[*serverconn.HostKeyMismatchError](err); ok {
		e.Kind, e.Address, e.Presented, e.Pinned = "host_key_mismatch", m.Address, m.Presented, m.Pinned
	} else if r, ok := errors.AsType[*serverconn.RevokedError](err); ok {
		e.Kind, e.Device = "revoked", r.Device
	} else if r, ok := errors.AsType[*serverconn.RefusedError](err); ok {
		e.Kind, e.Message = "refused", r.Banner
	} else if errors.Is(err, serverconn.ErrUnknownKey) {
		e.Kind = "unknown_key"
	} else if errors.Is(err, devicefiles.ErrNotJoined) {
		e.Kind = "not_joined"
	}
	return e
}

// parseState reads the banner a Device Mux sent.
func parseState(banner string) (State, error) {
	var sj stateJSON
	if err := json.Unmarshal([]byte(banner), &sj); err != nil {
		return State{}, fmt.Errorf("read the Device Mux's state: %w", err)
	}
	if sj.State == "elsewhere" {
		return State{}, fmt.Errorf("%w, %s", ErrOtherHome, sj.Home)
	}
	since, err := time.Parse(time.RFC3339Nano, sj.Since)
	if err != nil {
		return State{}, fmt.Errorf("read the Device Mux's state: %w", err)
	}
	switch sj.State {
	case "connected":
		return State{Connected: true, Since: since, Resuming: sj.Resuming}, nil
	case "reconnecting":
		if sj.Error == nil {
			return State{}, errors.New("read the Device Mux's state: it is reconnecting and says not why")
		}
		return State{Since: since, Err: sj.Error.decode(), Resuming: sj.Resuming}, nil
	}
	return State{}, fmt.Errorf("read the Device Mux's state: it is %q", sj.State)
}

func (e *errorJSON) decode() error {
	switch e.Kind {
	case "host_key_mismatch":
		return &serverconn.HostKeyMismatchError{Address: e.Address, Presented: e.Presented, Pinned: e.Pinned}
	case "revoked":
		return &serverconn.RevokedError{Device: e.Device}
	case "refused":
		return &serverconn.RefusedError{Banner: e.Message}
	case "unknown_key":
		return serverconn.ErrUnknownKey
	case "not_joined":
		return devicefiles.ErrNotJoined
	}
	return errors.New(e.Message)
}

// Dial connects to the Device Mux at socket for a command of the device
// whose home is home, as every command on a joined device does, and returns
// the connection, which reaches the koko server over the Device Mux's
// Device Connection, and the state it found the Device Mux in. A Device Mux
// that holds no Device Connection and failed to open one answers with its
// state and why (State.Err), which Dial returns as its error. One that
// carries the device of another home answers with an error wrapping
// ErrOtherHome. When no Device Mux answers at socket, the error wraps
// ErrNotRunning.
func Dial(ctx context.Context, socket, home string) (serverconn.Conn, State, error) {
	var d net.Dialer
	nc, err := d.DialContext(ctx, "unix", socket)
	if err != nil {
		return nil, State{}, fmt.Errorf("%w at %s: %w", ErrNotRunning, socket, err)
	}
	var banner string
	conn, err := serverconn.Handshake(ctx, nc, socket, &ssh.ClientConfig{
		User:            home,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // its private directory is its pin
		BannerCallback: func(message string) error {
			banner = message
			return nil
		},
	})
	st, stErr := parseState(banner)
	switch {
	case errors.Is(stErr, ErrOtherHome):
		return nil, State{}, stErr
	case stErr != nil:
		// It sent no state: it stopped meanwhile, or is not a Device Mux.
		if err == nil {
			_ = conn.Close()
			err = stErr
		}
		return nil, State{}, fmt.Errorf("%w at %s: %w", ErrNotRunning, socket, err)
	case !st.Connected:
		return nil, st, st.Err
	case err != nil:
		return nil, State{}, fmt.Errorf("%w at %s: %w", ErrNotRunning, socket, err)
	}
	return Conn{conn}, st, nil
}

// Conn is a command's connection through the Device Mux, as Dial returns it.
type Conn struct {
	serverconn.Conn
}

// WaitingToResume tells the Device Mux that a Tunnel on the connection
// waits to Resume, until stop is called or the connection ends, so that
// koko status counts it.
func (c Conn) WaitingToResume(ctx context.Context) (stop func()) {
	ch, err := c.OpenChannel(ctx, channelResuming, nil)
	if err != nil {
		return func() {}
	}
	return func() { _ = ch.Close() }
}

// Stop asks the Device Mux at socket, of any build, to stop, as koko
// disconnect does, and returns how many commands it carried, which it
// ends: SSH Sessions, koko web runs and calls in flight. When none
// answers at socket, such as one that is stopping already, or the socket a
// crashed one left, the error wraps ErrNotRunning.
func Stop(ctx context.Context, socket string) (int, error) {
	var d net.Dialer
	nc, err := d.DialContext(ctx, "unix", socket)
	if err != nil {
		return 0, fmt.Errorf("%w at %s: %w", ErrNotRunning, socket, err)
	}
	var banner string
	conn, err := serverconn.Handshake(ctx, nc, socket, &ssh.ClientConfig{
		User:            disconnectUser,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // its private directory is its pin
		BannerCallback: func(message string) error {
			banner = message
			return nil
		},
	})
	if err == nil {
		// A Device Mux never lets koko disconnect in.
		_ = conn.Close()
		err = errors.New("it let koko disconnect in")
	}
	if banner == "" {
		return 0, fmt.Errorf("%w at %s: %w", ErrNotRunning, socket, err)
	}
	var sj stateJSON
	if err := json.Unmarshal([]byte(banner), &sj); err != nil || sj.State != "stopped" || sj.Commands == nil {
		return 0, fmt.Errorf("the Device Mux at %s did not stop: it answered %q", socket, banner)
	}
	return *sj.Commands, nil
}
