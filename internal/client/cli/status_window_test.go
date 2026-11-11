package cli_test

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/cieons/koko/internal/client/cli"
	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/harness/sshhost"
	"github.com/cieons/koko/internal/server/transport"
	"github.com/cieons/koko/internal/shared/compat"
	"github.com/cieons/koko/internal/shared/wire"
)

// releaseCLI returns a Runner of a koko stated as the release version,
// reaching the koko server on the socket at path.
func releaseCLI(t *testing.T, version, path string) cli.Runner {
	t.Helper()
	m := harness.NewMachine(t)
	m.Release(version)
	return cli.Runner{Kokod: m, SocketPath: path}
}

// koko status says how this CLI and the koko server work together, on the
// koko server's own machine as anywhere: a newer koko server's release
// that koko update brings this CLI to, a CLI below the window, which the
// koko server's oldest names, and a koko server below this CLI's; two
// versions on one machine are no longer drift. Its JSON says the koko
// server's oldest and, outside the window, the error every command fails
// with.
func TestStatusSaysHowTheCLIAndTheServerWorkTogether(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		report wire.Report
		// code is the server_error's code, "" for none.
		code string
	}{
		{"same", wire.Report{Version: "v0.4.0", Oldest: compat.Oldest}, ""},
		{"older-server", wire.Report{Version: "v0.3.0"}, ""},
		{"newer-server", wire.Report{Version: "v0.5.0", Oldest: compat.Oldest}, ""},
		// koko update installs releases only.
		{"newer-development-server", wire.Report{Version: "v0.5.1-0.20261112090000-0123456789ab", Oldest: compat.Oldest}, ""},
		{"cli-below-window", wire.Report{Version: "v0.6.0", Oldest: "v0.5.0"}, "cli_too_old"},
		{"server-below-window", wire.Report{Version: "v0.2.0"}, "server_too_old"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tc.report.Operations = []string{"space.list"}
			path, calls := otherServer(t, tc.report)
			run := func(args ...string) harness.Result {
				return harness.Run(t, releaseCLI(t, "v0.4.0", path), []string{"HOME=" + t.TempDir()}, args...)
			}
			r := run("status")
			r.WantSuccess(t)
			harness.Golden(t, "status-window-"+tc.name+".txt", strings.ReplaceAll(r.Stdout, path, "<socket>"))

			s, _ := status(t, run)
			if deref(s.ServerVersion) != tc.report.Version || deref(s.ServerOldest) != deref(nonEmpty(tc.report.Oldest)) {
				t.Errorf("status --json says server_version %v and server_oldest %v, want %s and %q", deref(s.ServerVersion), deref(s.ServerOldest), tc.report.Version, tc.report.Oldest)
			}
			if got := s.ServerError; (got == nil) != (tc.code == "") || got != nil && got.Code != tc.code {
				t.Errorf("status --json says server_error %+v, want the code %q", got, tc.code)
			}
			if n := calls.Load(); n != 0 {
				t.Errorf("status called the koko server's API %d time(s), want none: its report lists no server.show", n)
			}
		})
	}
}

// nonEmpty returns a pointer to s, or nil when it is empty.
func nonEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// Over a Device Connection to a koko server below the window, every
// command fails with server_too_old, naming its Server Address, and koko
// status still shows it, saying why.
func TestAServerBelowTheWindowFailsEveryCommand(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	d := h.Join(harness.Owner)
	c := readClientConfig(t, d)
	addr, hostKey := otherDeviceServer(t, c.DeviceSession, wire.Report{Version: "v0.2.0", Operations: []string{"space.list"}})
	c.ServerAddress, c.HostKeys = addr, []string{string(ssh.MarshalAuthorizedKey(hostKey))}
	writeClientConfig(t, d, c)

	msg := d.Koko("space", "ls", "--json").WantError(t, 1, "server_too_old")
	for _, want := range []string{"at " + addr, "v0.2.0", compat.Oldest, "upgrade the koko server"} {
		if !strings.Contains(msg, want) {
			t.Errorf("the message %q does not name %q", msg, want)
		}
	}
	r := d.Koko("status")
	r.WantSuccess(t)
	if want := "koko server:     koko v0.2.0 (older than the oldest this CLI works with, " + compat.Oldest + ": upgrade the koko server)\n"; !strings.Contains(r.Stdout, want) {
		t.Errorf("status says\n%s\nwant the line %q", r.Stdout, want)
	}
}

// otherDeviceServer serves Device Connections on a loopback port, as a koko
// server of another version than this CLI: the real transport, sending
// report, taking every key as the Device Session's, until the test ends. It
// returns its address and host key.
func otherDeviceServer(t *testing.T, deviceSession string, report wire.Report) (string, ssh.PublicKey) {
	t.Helper()
	h := http.NotFoundHandler()
	s := transport.New(h, h, func() wire.Report { return report }, slog.New(slog.DiscardHandler))
	var lc net.ListenConfig
	ln, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	hostKey := sshhost.NewKey(t, ssh.KeyAlgoED25519)
	served := make(chan struct{})
	go func() {
		defer close(served)
		_ = s.ServeTCP(ln, hostKey, anyKey(deviceSession))
	}()
	t.Cleanup(func() {
		_ = s.Close()
		<-served
	})
	return ln.Addr().String(), hostKey.PublicKey()
}

// anyKey takes every device's key as the Device Session's it names, and
// joins none.
type anyKey string

func (a anyKey) Key(context.Context, ssh.PublicKey) (transport.DeviceKey, error) {
	return transport.DeviceKey{DeviceSession: string(a)}, nil
}

func (anyKey) Join(context.Context, ssh.PublicKey, []byte, net.Addr) (string, error) {
	return "", errors.New("no join here")
}

func (anyKey) Used(context.Context, string) error {
	return nil
}
