package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strings"
	"time"

	"github.com/cieons/koko/internal/client/apiclient"
	"github.com/cieons/koko/internal/client/devicefiles"
	"github.com/cieons/koko/internal/shared/build"
	"github.com/cieons/koko/internal/shared/wire"
)

var statusCommands = []Command{
	{
		Verb:    "status",
		Summary: "Show the koko server this device reaches and as whom, both versions, any revocation, and whether ssh reads koko's ssh_config",
		Tier:    Read,
		// It calls no operation: the koko server's version comes in the
		// report every connection carries (ADR 0027).
		Run: action(status),
	},
}

// statusJSON is what status prints. Every field is always there, null when
// it has no value; later tickets add the Device Mux's state beside them.
type statusJSON struct {
	// ServerAddress and DeviceSession are this device's join, from its
	// client config: the koko server it joined, and its Device Session there
	// as device ls names one in added_by; null when it has not joined.
	ServerAddress *string               `json:"server_address"`
	DeviceSession *deviceSessionRefJSON `json:"device_session"`
	// Socket is the Unix socket commands reached the koko server on, as the
	// Local caller, on the koko server's own machine; null elsewhere.
	Socket *string `json:"socket"`
	// CLIVersion is this koko's version, as koko version prints it.
	CLIVersion string `json:"cli_version"`
	// ServerVersion is the koko server's version, as it reported it on
	// connecting; null when it was not reached.
	ServerVersion *string `json:"server_version"`
	// ServerError is why the koko server was not reached, as the error
	// object any command that calls it would fail with now; null when it
	// answered.
	ServerError *errorJSON `json:"server_error"`
	// Revocation is the record the device keeps of its Revoked Device
	// Session; null when it keeps none.
	Revocation *revocationJSON `json:"revocation"`
	// IncludeWarning says why ssh does not read koko's ssh_config for every
	// host, and how to fix it; null when it does, or the device has not
	// joined.
	IncludeWarning *string `json:"include_warning"`
}

// errorJSON is an error as the CLI prints one inside a result.
type errorJSON struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// revocationJSON is the revocation record: which Device Session of which
// koko server was Revoked, and when the device learned it.
type revocationJSON struct {
	ServerAddress string               `json:"server_address"`
	DeviceSession deviceSessionRefJSON `json:"device_session"`
	LearnedAt     string               `json:"learned_at"`
}

func (s statusJSON) Human(w io.Writer) error {
	var b strings.Builder
	line := func(label, format string, args ...any) {
		fmt.Fprintf(&b, "%-17s"+format+"\n", append([]any{label + ":"}, args...)...)
	}
	if d := s.DeviceSession; d != nil {
		line("Server Address", "%s", *s.ServerAddress)
		line("User", "%s", d.User)
		line("Device", "%s (%s)", d.Device, d.ID)
	}
	if s.Socket != nil {
		line("Unix socket", "%s, as the Local caller", *s.Socket)
	}
	if s.ServerVersion != nil {
		line("koko server", "koko %s", *s.ServerVersion)
	} else {
		line("koko server", "not reached: %s", s.ServerError.Message)
	}
	line("This CLI", "koko %s", s.CLIVersion)
	if r := s.Revocation; r != nil {
		d := r.DeviceSession
		line("Revoked", "%s's device %s (%s) at %s, learned at %s", d.User, d.Device, d.ID, r.ServerAddress, r.LearnedAt)
	}
	if s.IncludeWarning != nil {
		fmt.Fprintf(&b, "\nWarning: %s.\n", *s.IncludeWarning)
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// status shows what this device knows of its koko server. A koko server it
// cannot reach is part of what it shows, so status fails only when it
// cannot read the device's own files.
func status(ctx context.Context, in *Input) (statusJSON, error) {
	s := statusJSON{CLIVersion: build.Read().Version}
	// The koko server is reached first: refusing the device as a Revoked
	// Device Session's runs the revocation cleanup, so the files read after
	// show the device as it now is.
	report, err := in.report(ctx)
	if err != nil {
		_, e := classify(err)
		s.ServerError = &errorJSON{Code: e.Code, Message: e.Message}
	} else {
		s.ServerVersion = &report.Version
		if in.socket != "" {
			s.Socket = &in.socket
		}
	}

	home := in.inv.getenv("HOME")
	c, _, err := devicefiles.Read(home)
	switch {
	case err == nil:
		s.ServerAddress = &c.ServerAddress
		s.DeviceSession = &deviceSessionRefJSON{ID: c.DeviceSession, User: c.User, Device: c.Device}
		if err := devicefiles.CheckInclude(home); err != nil {
			s.IncludeWarning = new(err.Error())
		}
	case !errors.Is(err, devicefiles.ErrNotJoined):
		return statusJSON{}, err
	}
	r, err := devicefiles.ReadRevocation(home)
	switch {
	case err == nil:
		s.Revocation = &revocationJSON{
			ServerAddress: r.ServerAddress,
			DeviceSession: deviceSessionRefJSON{ID: r.DeviceSession, User: r.User, Device: r.Device},
			LearnedAt:     r.LearnedAt.UTC().Format(time.RFC3339),
		}
	case !errors.Is(err, fs.ErrNotExist):
		return statusJSON{}, err
	}
	return s, nil
}

// report returns the koko server's version and operations report,
// connecting as a call would; a connection that ends before the report
// arrives is server_unreachable, as it is for a call.
func (in *Input) report(ctx context.Context) (wire.Report, error) {
	conn, err := in.server(ctx)
	if err != nil {
		return wire.Report{}, err
	}
	report, err := conn.Report(ctx)
	if err != nil {
		return wire.Report{}, &cliError{code: codeServerUnreachable, message: fmt.Sprintf("%v: %v", apiclient.ErrUnreachable, err)}
	}
	return report, nil
}
