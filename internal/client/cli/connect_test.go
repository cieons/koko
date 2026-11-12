package cli_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/cieons/koko/internal/client/cli"
	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/harness/sshhost"
	"github.com/cieons/koko/internal/shared/timing"
	"github.com/cieons/koko/internal/shared/wire"
)

// With no socket at its path, and no koko server joined, a command that calls
// the koko server fails with not_joined.
func TestNoSocketIsNotJoined(t *testing.T) {
	t.Parallel()
	path := filepath.Join(harness.ShortTempDir(t), "koko.sock")
	r := harness.Run(t, cli.Runner{SocketPath: path}, nil, "space", "ls", "--json")
	msg := r.WantError(t, 1, "not_joined")
	if !strings.Contains(msg, path) {
		t.Errorf("message %q does not name the socket", msg)
	}
}

// A socket path that answers no SSH fails with server_unreachable.
func TestDeadSocketIsServerUnreachable(t *testing.T) {
	t.Parallel()
	path := filepath.Join(harness.ShortTempDir(t), "koko.sock")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	r := harness.Run(t, cli.Runner{SocketPath: path}, nil, "space", "ls", "--json")
	msg := r.WantError(t, 1, "server_unreachable")
	if !strings.Contains(msg, path) {
		t.Errorf("message %q does not name the socket", msg)
	}
}

// A socket that answers SSH but cannot carry the call (here, it refuses every
// channel) fails with server_unreachable, not internal_error.
func TestCallThatCannotBeCarriedIsServerUnreachable(t *testing.T) {
	t.Parallel()
	path := filepath.Join(harness.ShortTempDir(t), "koko.sock")
	refuseChannels(t, path)
	r := harness.Run(t, cli.Runner{SocketPath: path}, nil, "space", "ls", "--json")
	r.WantError(t, 1, "server_unreachable")
}

// refuseChannels serves SSH with none authentication on path, sends the
// report a koko server sends, and refuses every channel, until the test
// ends.
func refuseChannels(t *testing.T, path string) {
	t.Helper()
	fakeSocket(t, path, true)
}

// fakeSocket serves SSH with none authentication on path, sends the report
// a koko server sends when report is set, and refuses every channel, until
// the test ends.
func fakeSocket(t *testing.T, path string, report bool) {
	t.Helper()
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &ssh.ServerConfig{NoClientAuth: true}
	cfg.AddHostKey(signer)
	var lc net.ListenConfig
	ln, err := lc.Listen(t.Context(), "unix", path)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	t.Cleanup(func() {
		_ = ln.Close()
		<-done
	})
	go func() {
		defer close(done)
		for {
			nc, err := ln.Accept()
			if err != nil {
				return
			}
			conn, chans, reqs, err := ssh.NewServerConn(nc, cfg)
			if err != nil {
				_ = nc.Close()
				continue
			}
			go ssh.DiscardRequests(reqs)
			if report {
				r := wire.Report{Version: "v0.3.0", Operations: []string{"space.list"}}
				_, _, _ = conn.SendRequest(wire.RequestReport, false, r.Marshal())
			}
			for nch := range chans {
				_ = nch.Reject(ssh.Prohibited, "no channels here")
			}
			_ = conn.Close()
		}
	}()
}

// A Device Connection takes the koko server's host key when it is any entry
// of the pinned list, of one key or of several, and refuses it with
// host_key_mismatch, naming both, when it is none.
func TestDeviceConnectionChecksTheHostKeyAgainstThePinnedList(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	d := h.Join(harness.Owner)
	c := readClientConfig(t, d)
	other := string(ssh.MarshalAuthorizedKey(sshhost.NewKey(t, ssh.KeyAlgoED25519).PublicKey()))
	for name, keys := range map[string][]string{
		"the one from the join":        c.HostKeys,
		"another key, then the one":    {other, c.HostKeys[0]},
		"the one, then another key":    {c.HostKeys[0], other},
		"two other keys, then the one": {other, other, c.HostKeys[0]},
	} {
		c.HostKeys = keys
		writeClientConfig(t, d, c)
		if r := d.Koko("space", "ls", "--json"); r.Status != 0 {
			t.Errorf("with %s pinned, space ls = %d %s", name, r.Status, r.Stderr)
		}
	}
	c.HostKeys = []string{other}
	writeClientConfig(t, d, c)
	msg := d.Koko("space", "ls", "--json").WantError(t, 1, "host_key_mismatch")
	pinned, _, _, _, _ := ssh.ParseAuthorizedKey([]byte(other))
	if !strings.Contains(msg, ssh.FingerprintSHA256(pinned)) || !strings.Contains(msg, "SHA256:") {
		t.Errorf("message %q does not name the pinned and the presented host key", msg)
	}
}

// A key no Device Session holds gets a generic not_joined, which names no
// User and no device.
func TestDeviceConnectionWithAnUnknownKeyIsNotJoined(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	d := h.Join(harness.Owner)
	_, unknown, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKey(unknown, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(kokoDir(d), "device_key"), pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatal(err)
	}
	c := readClientConfig(t, d)
	msg := d.Koko("space", "ls", "--json").WantError(t, 1, "not_joined")
	if strings.Contains(msg, c.User) || strings.Contains(msg, c.Device) || !strings.Contains(msg, c.ServerAddress) {
		t.Errorf("message %q is not generic: it names %s or %s, or not the koko server at %s", msg, c.User, c.Device, c.ServerAddress)
	}
}

// A joined device whose koko server does not answer fails with
// server_unreachable, whose hint names koko connect <host:port>.
func TestJoinedDeviceWhoseServerIsDownIsServerUnreachable(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	d := h.Join(harness.Owner)
	d.UseTimings(timing.Timings{CommandWait: 200 * time.Millisecond})
	h.Stop()
	msg := d.Koko("space", "ls", "--json").WantError(t, 1, "server_unreachable")
	if !strings.Contains(msg, "koko connect <host:port>") || !strings.Contains(msg, readClientConfig(t, d).ServerAddress) {
		t.Errorf("message %q does not name the Server Address and koko connect <host:port>", msg)
	}
}

// The CLI uses the Unix socket when it is at its path, as the Local caller
// acting as the Owner, and otherwise the koko server the device joined.
func TestTheSocketWinsOverTheJoinedServer(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	h.Koko("user", "invite", "bob").WantSuccess(t)
	d := h.Join("bob")
	if jc := deviceAddOn(t, d); jc.User != "bob" {
		t.Errorf("without the socket, device add issued for %s, want bob", jc.User)
	}
	r := harness.Run(t, cli.Runner{SocketPath: h.Socket()}, []string{harness.HomeEnv(d.Home())}, "device", "add", "--json")
	r.WantSuccess(t)
	if !strings.Contains(r.Stdout, `"user": "alice"`) {
		t.Errorf("with the socket at its path, device add printed %s, want a code for the Owner, alice", r.Stdout)
	}
}
