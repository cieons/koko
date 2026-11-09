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
	"github.com/cieons/koko/internal/shared/build"
	"github.com/cieons/koko/internal/shared/wire"
)

// drift is the line koko status adds when the koko server on its own
// machine is another build than the CLI.
const drift = "the koko server here is another build: run sudo koko server install to bring it to this CLI's"

// Over the Unix socket, on the koko server's own machine, koko status names
// a koko server of another version than the CLI, and sudo koko server
// install, in a line of its own; its JSON gains nothing. Beside a koko
// server of the CLI's version it says nothing more.
func TestStatusNamesAnotherBuildOverTheSocket(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		version string
		says    bool
	}{{"v0.1.0", true}, {build.Read().Version, false}} {
		t.Run(tc.version, func(t *testing.T) {
			t.Parallel()
			path, _ := otherServer(t, wire.Report{Version: tc.version, Operations: []string{"space.list"}})
			run := func(args ...string) harness.Result {
				return harness.Run(t, cli.Runner{SocketPath: path}, []string{"HOME=" + t.TempDir()}, args...)
			}
			r := run("status")
			r.WantSuccess(t)
			if strings.Contains(r.Stdout, drift) != tc.says {
				t.Errorf("beside a koko server of %s, status says\n%s\nwant the line %q %v", tc.version, r.Stdout, drift, tc.says)
			}
			if s, _ := status(t, run); deref(s.ServerVersion) != tc.version || deref(s.Socket) != path {
				t.Errorf("status --json = %+v, want the server_version %s over the socket", s, tc.version)
			}
		})
	}
}

// Over a Device Connection, a koko server of another version than the CLI
// is normal, and koko status says nothing of it.
func TestStatusNamesNoOtherBuildOverADeviceConnection(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	d := h.Join(harness.Owner)
	c := readClientConfig(t, d)
	addr, hostKey := otherDeviceServer(t, c.DeviceSession, wire.Report{Version: "v0.1.0", Operations: []string{"space.list"}})
	c.ServerAddress, c.HostKeys = addr, []string{string(ssh.MarshalAuthorizedKey(hostKey))}
	writeClientConfig(t, d, c)

	r := d.Koko("status")
	r.WantSuccess(t)
	if !strings.Contains(r.Stdout, "koko server:     koko v0.1.0\n") || strings.Contains(r.Stdout, drift) {
		t.Errorf("status over a Device Connection to a koko server of v0.1.0 says\n%s\nwant its version and no line %q", r.Stdout, drift)
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
