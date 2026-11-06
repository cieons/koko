package transport_test

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/cieons/koko/internal/client/serverconn"
	"github.com/cieons/koko/internal/harness/sshhost"
	"github.com/cieons/koko/internal/server/transport"
	"github.com/cieons/koko/internal/shared/joincode"
	"github.com/cieons/koko/internal/shared/wire"
)

// fakeDevices plays the device area: the keys its Device Sessions hold, and
// the Join Code secrets that make a key a Device Session.
type fakeDevices struct {
	mu sync.Mutex
	// sessions are the Device Sessions' ids by their key in wire format.
	sessions map[string]string
	// revoked are the names of the Revoked Device Sessions by their key in
	// wire format.
	revoked map[string]string
	// secrets are the ids of the Device Sessions each secret makes.
	secrets map[[joincode.SecretSize]byte]string
}

func (f *fakeDevices) Key(_ context.Context, key ssh.PublicKey) (transport.DeviceKey, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if device, ok := f.revoked[string(key.Marshal())]; ok {
		return transport.DeviceKey{DeviceSession: "device_r3v0k3d0", Device: device, Revoked: true}, nil
	}
	return transport.DeviceKey{DeviceSession: f.sessions[string(key.Marshal())]}, nil
}

func (f *fakeDevices) Join(_ context.Context, key ssh.PublicKey, password []byte, _ net.Addr) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	secret, ok := wire.ParseJoinSecret(password)
	id := f.secrets[secret]
	if !ok || id == "" {
		return "", errors.New("no Join Code has this secret")
	}
	delete(f.secrets, secret)
	f.sessions[string(key.Marshal())] = id
	return id, nil
}

func (f *fakeDevices) Used(context.Context, string) error {
	return nil
}

// startTCP serves h to Device Connections on a loopback port, through
// devices, and returns the address and the host key's fingerprint.
func startTCP(t *testing.T, h http.Handler, devices transport.Devices) (string, [sha256.Size]byte) {
	t.Helper()
	_, addr, fingerprint := serveTCP(t, h, devices)
	return addr, fingerprint
}

// serveTCP is startTCP that also returns the transport.
func serveTCP(t *testing.T, h http.Handler, devices transport.Devices) (*transport.Server, string, [sha256.Size]byte) {
	t.Helper()
	s := transport.New(h, h, testReporter, slog.New(slog.DiscardHandler))
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("close transport: %v", err)
		}
	})
	var lc net.ListenConfig
	ln, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	hostKey := sshhost.NewKey(t, ssh.KeyAlgoED25519)
	if err := s.ServeTCP(ln, hostKey, devices); err != nil {
		t.Fatal(err)
	}
	return s, ln.Addr().String(), sha256.Sum256(hostKey.PublicKey().Marshal())
}

// waitEnded fails the test unless conn ends within 10 s.
func waitEnded(t *testing.T, conn serverconn.Conn, what string) {
	t.Helper()
	ended := make(chan struct{})
	go func() {
		_ = conn.Wait()
		close(ended)
	}()
	select {
	case <-ended:
	case <-time.After(10 * time.Second):
		t.Fatalf("%s is still open after 10 s", what)
	}
}

// CloseDeviceSessions closes the Device Connections of the Device Sessions
// it names, and no other's. From then on it refuses their connections, even
// one that authenticated as one of them while the revocation committed:
// here through a device area that still answers the key as live.
func TestClosingADeviceSessionClosesItsConnectionsAndRefusesNew(t *testing.T) {
	revoked, other := sshhost.NewKey(t, ssh.KeyAlgoED25519), sshhost.NewKey(t, ssh.KeyAlgoED25519)
	devices := &fakeDevices{sessions: map[string]string{
		string(revoked.PublicKey().Marshal()): "device_3f9k2m7q",
		string(other.PublicKey().Marshal()):   "device_7h2k9m3q",
	}}
	s, addr, _ := serveTCP(t, whoAmI, devices)
	dial := func(key ssh.Signer) serverconn.Conn {
		t.Helper()
		conn, err := serverconn.DialDevice(t.Context(), serverconn.Device{Address: addr, HostKeys: hostKeysAt(t, addr), Key: key})
		if err != nil {
			t.Fatalf("dial the Device Connection: %v", err)
		}
		t.Cleanup(func() { _ = conn.Close() })
		return conn
	}
	before, kept := dial(revoked), dial(other)

	s.CloseDeviceSessions("device_3f9k2m7q")
	waitEnded(t, before, "the closed Device Session's connection")
	waitEnded(t, dial(revoked), "a new connection of the closed Device Session")
	if got := callerOn(t, kept); got != "device session device_7h2k9m3q" {
		t.Errorf("caller on the other Device Session's connection = %q", got)
	}
}

// callerOn makes one call on a new api@koko channel of conn and returns the
// caller whoAmI saw.
func callerOn(t *testing.T, conn serverconn.Conn) string {
	t.Helper()
	ch, err := conn.OpenChannel(t.Context(), wire.ChannelAPI, nil)
	if err != nil {
		t.Fatalf("open %s: %v", wire.ChannelAPI, err)
	}
	defer func() { _ = ch.Close() }()
	return get(t, ch, bufio.NewReader(ch))
}

// A Device Connection authenticates by its device's key: every call on it
// comes from that key's Device Session.
func TestDeviceConnectionAuthenticatesByTheDeviceKey(t *testing.T) {
	key := sshhost.NewKey(t, ssh.KeyAlgoED25519)
	devices := &fakeDevices{sessions: map[string]string{string(key.PublicKey().Marshal()): "device_3f9k2m7q"}}
	addr, _ := startTCP(t, whoAmI, devices)
	conn, err := serverconn.DialDevice(t.Context(), serverconn.Device{Address: addr, HostKeys: hostKeysAt(t, addr), Key: key})
	if err != nil {
		t.Fatalf("dial the Device Connection: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if got := callerOn(t, conn); got != "device session device_3f9k2m7q" {
		t.Errorf("caller = %q, want the Device Session device_3f9k2m7q", got)
	}
}

// A key no Device Session holds is answered, once the device has signed
// with it, with a request for a Join Code's secret: a Device Connection
// that has none gets ErrUnknownKey; one with a valid secret joins on that
// connection, and its key then authenticates alone.
func TestAnUnknownKeyJoinsWithAJoinCodesSecret(t *testing.T) {
	secret := [joincode.SecretSize]byte{1, 2, 3}
	devices := &fakeDevices{sessions: map[string]string{}, secrets: map[[joincode.SecretSize]byte]string{secret: "device_7h2k9m3q"}}
	addr, fingerprint := startTCP(t, whoAmI, devices)
	key := sshhost.NewKey(t, ssh.KeyAlgoED25519)
	device := serverconn.Device{Address: addr, HostKeys: hostKeysAt(t, addr), Key: key}
	if _, err := serverconn.DialDevice(t.Context(), device); !errors.Is(err, serverconn.ErrUnknownKey) {
		t.Fatalf("a Device Connection with an unknown key = %v, want ErrUnknownKey", err)
	}

	wrong := [joincode.SecretSize]byte{9}
	_, _, err := serverconn.Join(t.Context(), addr, fingerprint, key, wrong)
	if e, ok := errors.AsType[*serverconn.RefusedError](err); !ok || e.Banner != "the Join Code is invalid or expired." {
		t.Fatalf("a join with a wrong secret = %v, want refused with the invalid-or-expired banner", err)
	}

	conn, hostKey, err := serverconn.Join(t.Context(), addr, fingerprint, key, secret)
	if err != nil {
		t.Fatalf("a join with the secret: %v", err)
	}
	if sha256.Sum256(hostKey.Marshal()) != fingerprint {
		t.Error("Join returned a host key other than the one the fingerprint names")
	}
	if got := callerOn(t, conn); got != "device session device_7h2k9m3q" {
		t.Errorf("caller on the joining connection = %q, want the new Device Session", got)
	}
	_ = conn.Close()
	again, err := serverconn.DialDevice(t.Context(), device)
	if err != nil {
		t.Fatalf("after the join, a Device Connection with the key: %v", err)
	}
	defer func() { _ = again.Close() }()
	if got := callerOn(t, again); got != "device session device_7h2k9m3q" {
		t.Errorf("caller = %q, want the Device Session the join made", got)
	}
}

// forgedSigner offers a public key without its private half: its
// signatures are garbage.
type forgedSigner struct {
	key ssh.PublicKey
}

func (f forgedSigner) PublicKey() ssh.PublicKey {
	return f.key
}

func (f forgedSigner) Sign(io.Reader, []byte) (*ssh.Signature, error) {
	return &ssh.Signature{Format: f.key.Type(), Blob: make([]byte, ed25519.SignatureSize)}, nil
}

// A Revoked Device Session's key is refused with a banner naming its
// device, and only once the device has signed with it: offering the key
// without its private half learns nothing of the Device Session.
func TestARevokedKeyIsToldByNameOnlyAfterItsSignature(t *testing.T) {
	key := sshhost.NewKey(t, ssh.KeyAlgoED25519)
	devices := &fakeDevices{revoked: map[string]string{string(key.PublicKey().Marshal()): "laptop"}}
	addr, _ := startTCP(t, whoAmI, devices)
	device := serverconn.Device{Address: addr, HostKeys: hostKeysAt(t, addr), Key: key}
	_, err := serverconn.DialDevice(t.Context(), device)
	if e, ok := errors.AsType[*serverconn.RevokedError](err); !ok || e.Device != "laptop" {
		t.Errorf("a Device Connection with a Revoked key = %v, want it refused as the Revoked laptop's", err)
	}
	device.Key = forgedSigner{key.PublicKey()}
	_, err = serverconn.DialDevice(t.Context(), device)
	if e, ok := errors.AsType[*serverconn.RefusedError](err); !ok || e.Banner != "" {
		t.Errorf("a Device Connection offering the Revoked key without signing = %v, want a refusal with no banner", err)
	}
}

// A Device Connection gets the version and operations report too, as soon
// as it has authenticated, and the CLI's connection reads it.
func TestDeviceConnectionIsSentTheReport(t *testing.T) {
	key := sshhost.NewKey(t, ssh.KeyAlgoED25519)
	devices := &fakeDevices{sessions: map[string]string{string(key.PublicKey().Marshal()): "device_3f9k2m7q"}}
	addr, _ := startTCP(t, whoAmI, devices)
	conn, err := serverconn.DialDevice(t.Context(), serverconn.Device{Address: addr, HostKeys: hostKeysAt(t, addr), Key: key})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	r, err := conn.Report(ctx)
	if err != nil {
		t.Fatalf("read the report: %v", err)
	}
	wantTestReport(t, r)
}

// A Device Connection carries the same channels as the socket: api@koko,
// and nothing else yet.
func TestDeviceConnectionRefusesOtherChannelTypes(t *testing.T) {
	key := sshhost.NewKey(t, ssh.KeyAlgoED25519)
	devices := &fakeDevices{sessions: map[string]string{string(key.PublicKey().Marshal()): "device_3f9k2m7q"}}
	addr, _ := startTCP(t, whoAmI, devices)
	conn, err := serverconn.DialDevice(t.Context(), serverconn.Device{Address: addr, HostKeys: hostKeysAt(t, addr), Key: key})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	for _, kind := range []string{"session", "direct-streamlocal@openssh.com"} {
		_, err := conn.OpenChannel(t.Context(), kind, nil)
		if oce, ok := errors.AsType[*ssh.OpenChannelError](err); !ok || oce.Reason != ssh.UnknownChannelType {
			t.Errorf("open %q: err = %v, want an unknown channel type refusal", kind, err)
		}
	}
}

// hostKeysAt returns the host key the koko server at addr presents, as a
// pinned list of one.
func hostKeysAt(t *testing.T, addr string) []ssh.PublicKey {
	t.Helper()
	var d net.Dialer
	nc, err := d.DialContext(t.Context(), "tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = nc.Close() }()
	var key ssh.PublicKey
	_, _, _, _ = ssh.NewClientConn(nc, addr, &ssh.ClientConfig{
		User: "koko",
		HostKeyCallback: func(_ string, _ net.Addr, k ssh.PublicKey) error {
			key = k
			return errors.New("only the host key is wanted")
		},
	})
	if key == nil {
		t.Fatal("no host key presented")
	}
	return []ssh.PublicKey{key}
}
