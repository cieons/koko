package hostconn_test

import (
	"context"
	"errors"
	"net"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/cieons/koko/internal/harness/sshhost"
	"github.com/cieons/koko/internal/server/hostconn"
	"github.com/cieons/koko/internal/shared/timing"
)

// fast shrinks the connect timings so that a test of retries takes
// milliseconds.
var fast = timing.Timings{HostConnectAttempt: 5 * time.Second, HostConnectWait: 20 * time.Millisecond}

// target is how the connector reaches h as its Host Account.
func target(h *sshhost.Host, login hostconn.Login, pin hostconn.Pin) hostconn.Target {
	return hostconn.Target{Address: h.Address(), Port: h.Port(), Account: sshhost.Account, Login: login, Pin: pin}
}

// down is a Host on a loopback port nothing listens on.
func down(t *testing.T, login hostconn.Login) hostconn.Target {
	t.Helper()
	var lc net.ListenConfig
	ln, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
	return hostconn.Target{Address: "127.0.0.1", Port: port, Account: sshhost.Account, Login: login}
}

// eventually waits up to 5 s for cond, and fails the test with what
// otherwise.
func eventually(t *testing.T, cond func() bool, what string) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); !cond(); time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal(what)
		}
	}
}

// connect connects with the fast timings and closes the connection when the
// test ends.
func connect(t *testing.T, host hostconn.Target, relay *hostconn.Target) (*hostconn.Conn, error) {
	t.Helper()
	c := hostconn.Connector{Timings: fast}
	conn, err := c.Connect(t.Context(), host, relay)
	if err == nil {
		t.Cleanup(func() { _ = conn.Close() })
	}
	return conn, err
}

func TestLogsInWithAKey(t *testing.T) {
	key := sshhost.NewKey(t, ssh.KeyAlgoED25519)
	h := sshhost.Start(t, sshhost.AuthorizedKey(key.PublicKey()))

	conn, err := connect(t, target(h, hostconn.KeyLogin(key), hostconn.PinKey(h.HostKey())), nil)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if got := ssh.FingerprintSHA256(conn.HostKey); got != ssh.FingerprintSHA256(h.HostKey()) {
		t.Errorf("HostKey = %s, want the pinned %s", got, ssh.FingerprintSHA256(h.HostKey()))
	}
	want := []sshhost.Login{{Account: sshhost.Account, Method: "publickey", Key: ssh.FingerprintSHA256(key.PublicKey()), OK: true}}
	if got := h.Logins(); !slices.Equal(got, want) {
		t.Errorf("the Host received logins %+v, want %+v", got, want)
	}
}

func TestLogsInWithAPassword(t *testing.T) {
	h := sshhost.Start(t, sshhost.Password("correct horse"))

	if _, err := connect(t, target(h, hostconn.PasswordLogin("correct horse"), hostconn.PinKey(h.HostKey())), nil); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	want := []sshhost.Login{{Account: sshhost.Account, Method: "password", OK: true}}
	if got := h.Logins(); !slices.Equal(got, want) {
		t.Errorf("the Host received logins %+v, want %+v", got, want)
	}
}

// A Host whose sshd asks for the password through PAM, as FreeBSD's does by
// default, gets it over keyboard-interactive.
func TestLogsInWithAPasswordOverKeyboardInteractive(t *testing.T) {
	h := sshhost.Start(t, sshhost.KeyboardInteractive("correct horse"))

	if _, err := connect(t, target(h, hostconn.PasswordLogin("correct horse"), hostconn.PinKey(h.HostKey())), nil); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	want := []sshhost.Login{{Account: sshhost.Account, Method: "keyboard-interactive", OK: true}}
	if got := h.Logins(); !slices.Equal(got, want) {
		t.Errorf("the Host received logins %+v, want %+v", got, want)
	}
}

// A host key other than the pinned one is refused with both fingerprints,
// before the Host is sent anything to log in with, and never retried.
func TestRefusesAHostKeyOtherThanThePinnedOne(t *testing.T) {
	key := sshhost.NewKey(t, ssh.KeyAlgoED25519)
	h := sshhost.Start(t, sshhost.AuthorizedKey(key.PublicKey()))
	pinned := sshhost.NewKey(t, ssh.KeyAlgoED25519).PublicKey()

	_, err := connect(t, target(h, hostconn.KeyLogin(key), hostconn.PinKey(pinned)), nil)

	mismatch, ok := errors.AsType[*hostconn.HostKeyMismatchError](err)
	if !ok {
		t.Fatalf("Connect error = %v, want a host key mismatch", err)
	}
	if got, want := mismatch.Pinned, ssh.FingerprintSHA256(pinned); got != want {
		t.Errorf("Pinned = %s, want %s", got, want)
	}
	if got, want := mismatch.Offered, ssh.FingerprintSHA256(h.HostKey()); got != want {
		t.Errorf("Offered = %s, want %s", got, want)
	}
	for _, fp := range []string{ssh.FingerprintSHA256(pinned), ssh.FingerprintSHA256(h.HostKey())} {
		if !strings.Contains(err.Error(), fp) {
			t.Errorf("error %q does not name %s", err, fp)
		}
	}
	if got := h.Logins(); len(got) != 0 {
		t.Errorf("the Host received logins %+v, want none", got)
	}
	if got := h.Conns(); got != 1 {
		t.Errorf("the Host was connected to %d times, want once", got)
	}
}

// A refused login is an authentication failure, never retried.
func TestARefusedLoginIsNotRetried(t *testing.T) {
	h := sshhost.Start(t, sshhost.Password("correct horse"))

	_, err := connect(t, target(h, hostconn.PasswordLogin("wrong"), hostconn.PinKey(h.HostKey())), nil)

	if !errors.Is(err, hostconn.ErrLoginRefused) {
		t.Fatalf("Connect error = %v, want ErrLoginRefused", err)
	}
	if got := h.Conns(); got != 1 {
		t.Errorf("the Host was connected to %d times, want once", got)
	}
}

// With no pin, the connector asks for an ed25519 host key first and returns
// the key offered, for Enroll to pin.
func TestWithNoPinReturnsTheKeyOfferedPreferringEd25519(t *testing.T) {
	key := sshhost.NewKey(t, ssh.KeyAlgoED25519)
	ed25519Key := sshhost.NewKey(t, ssh.KeyAlgoED25519)
	h := sshhost.Start(t, sshhost.AuthorizedKey(key.PublicKey()),
		sshhost.HostKeys(sshhost.NewKey(t, ssh.KeyAlgoECDSA256), ed25519Key))

	conn, err := connect(t, target(h, hostconn.KeyLogin(key), hostconn.Pin{}), nil)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if got, want := ssh.FingerprintSHA256(conn.HostKey), ssh.FingerprintSHA256(ed25519Key.PublicKey()); got != want {
		t.Errorf("HostKey = %s (%s), want the ed25519 key %s", got, conn.HostKey.Type(), want)
	}
}

// A Host holding several host keys offers the pinned one, whatever its type,
// because the connector asks for the pinned key's type first.
func TestAsksForThePinnedKeysTypeFirst(t *testing.T) {
	key := sshhost.NewKey(t, ssh.KeyAlgoED25519)
	ecdsaKey := sshhost.NewKey(t, ssh.KeyAlgoECDSA256)
	rsaKey := sshhost.NewKey(t, ssh.KeyAlgoRSA)
	h := sshhost.Start(t, sshhost.AuthorizedKey(key.PublicKey()),
		sshhost.HostKeys(sshhost.NewKey(t, ssh.KeyAlgoED25519), ecdsaKey, rsaKey))

	for _, pinned := range []ssh.PublicKey{ecdsaKey.PublicKey(), rsaKey.PublicKey()} {
		t.Run(pinned.Type(), func(t *testing.T) {
			conn, err := connect(t, target(h, hostconn.KeyLogin(key), hostconn.PinKey(pinned)), nil)
			if err != nil {
				t.Fatalf("Connect: %v", err)
			}
			if got, want := ssh.FingerprintSHA256(conn.HostKey), ssh.FingerprintSHA256(pinned); got != want {
				t.Errorf("HostKey = %s, want the pinned %s", got, want)
			}
		})
	}
}

// A Host rebuilt without the pinned key's type offers another type, and is
// refused as a mismatch with both fingerprints rather than as a failed
// negotiation.
func TestAHostWithoutThePinnedKeysTypeIsAMismatch(t *testing.T) {
	key := sshhost.NewKey(t, ssh.KeyAlgoED25519)
	h := sshhost.Start(t, sshhost.AuthorizedKey(key.PublicKey()))
	pinned := sshhost.NewKey(t, ssh.KeyAlgoECDSA256).PublicKey()

	_, err := connect(t, target(h, hostconn.KeyLogin(key), hostconn.PinKey(pinned)), nil)

	mismatch, ok := errors.AsType[*hostconn.HostKeyMismatchError](err)
	if !ok {
		t.Fatalf("Connect error = %v, want a host key mismatch", err)
	}
	if got, want := mismatch.Offered, ssh.FingerprintSHA256(h.HostKey()); got != want {
		t.Errorf("Offered = %s, want %s", got, want)
	}
}

func TestRetriesAResetConnection(t *testing.T) {
	key := sshhost.NewKey(t, ssh.KeyAlgoED25519)
	h := sshhost.Start(t, sshhost.AuthorizedKey(key.PublicKey()), sshhost.ResetFirst(2))

	if _, err := connect(t, target(h, hostconn.KeyLogin(key), hostconn.PinKey(h.HostKey())), nil); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if got := h.Conns(); got != 3 {
		t.Errorf("the Host was connected to %d times, want 3", got)
	}
}

// Three attempts fail, with a wait before the second and a doubled one before
// the third, and then the connector gives up.
func TestGivesUpAfterThreeAttempts(t *testing.T) {
	key := sshhost.NewKey(t, ssh.KeyAlgoED25519)
	h := sshhost.Start(t, sshhost.AuthorizedKey(key.PublicKey()), sshhost.ResetFirst(4))
	wait := 100 * time.Millisecond
	c := hostconn.Connector{Timings: timing.Timings{HostConnectWait: wait}}

	start := time.Now()
	_, err := c.Connect(t.Context(), target(h, hostconn.KeyLogin(key), hostconn.PinKey(h.HostKey())), nil)

	if !errors.Is(err, hostconn.ErrUnreachable) {
		t.Fatalf("Connect error = %v, want ErrUnreachable", err)
	}
	if got := h.Conns(); got != 3 {
		t.Errorf("the Host was connected to %d times, want 3", got)
	}
	if got, least := time.Since(start), wait+2*wait; got < least {
		t.Errorf("gave up after %v, want at least %v of waits (%v, then %v)", got, least, wait, 2*wait)
	}
}

// An attempt the Host never answers ends at the per-attempt timeout, and the
// next attempt is made.
func TestRetriesAnUnansweredAttempt(t *testing.T) {
	// Registered before the Host starts, so it waits for Connect after the
	// Host has stopped and let go of the held connection.
	var wg sync.WaitGroup
	t.Cleanup(wg.Wait)
	key := sshhost.NewKey(t, ssh.KeyAlgoED25519)
	h := sshhost.Start(t, sshhost.AuthorizedKey(key.PublicKey()), sshhost.StallFirst(1))
	c := hostconn.Connector{Timings: timing.Timings{HostConnectAttempt: time.Second, HostConnectWait: fast.HostConnectWait}}

	type result struct {
		conn *hostconn.Conn
		err  error
	}
	done := make(chan result, 1)
	wg.Go(func() {
		conn, err := c.Connect(t.Context(), target(h, hostconn.KeyLogin(key), hostconn.PinKey(h.HostKey())), nil)
		done <- result{conn, err}
	})

	var r result
	select {
	case r = <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Connect still waits on a Host that never answers")
	}
	if r.err != nil {
		t.Fatalf("Connect: %v", r.err)
	}
	t.Cleanup(func() { _ = r.conn.Close() })
	if got := h.Conns(); got != 2 {
		t.Errorf("the Host was connected to %d times, want 2", got)
	}
}

func TestRetriesARefusedConnection(t *testing.T) {
	start := time.Now()
	_, err := connect(t, down(t, hostconn.KeyLogin(sshhost.NewKey(t, ssh.KeyAlgoED25519))), nil)

	if !errors.Is(err, hostconn.ErrUnreachable) || !errors.Is(err, syscall.ECONNREFUSED) {
		t.Fatalf("Connect error = %v, want ErrUnreachable from a refused connection", err)
	}
	if got, least := time.Since(start), fast.HostConnectWait+2*fast.HostConnectWait; got < least {
		t.Errorf("gave up after %v, want at least %v of waits between three attempts", got, least)
	}
}

// A caller that gives up stops the waits between attempts at once.
func TestStopsWhenTheCallerGivesUp(t *testing.T) {
	key := sshhost.NewKey(t, ssh.KeyAlgoED25519)
	h := sshhost.Start(t, sshhost.AuthorizedKey(key.PublicKey()), sshhost.ResetFirst(3))
	c := hostconn.Connector{Timings: timing.Timings{HostConnectWait: time.Hour}}
	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()

	_, err := c.Connect(ctx, target(h, hostconn.KeyLogin(key), hostconn.PinKey(h.HostKey())), nil)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Connect error = %v, want the caller's deadline", err)
	}
	if got := h.Conns(); got != 1 {
		t.Errorf("the Host was connected to %d times, want once", got)
	}
}

// relayedHost starts a Relay and a Host behind it, both with the koko key
// authorized.
func relayedHost(t *testing.T, hostOpts ...sshhost.Option) (kokoKey ssh.Signer, relay, host *sshhost.Host) {
	t.Helper()
	kokoKey = sshhost.NewKey(t, ssh.KeyAlgoED25519)
	relay = sshhost.Start(t, sshhost.AuthorizedKey(kokoKey.PublicKey()))
	host = sshhost.Start(t, append([]sshhost.Option{sshhost.AuthorizedKey(kokoKey.PublicKey())}, hostOpts...)...)
	return kokoKey, relay, host
}

// directTCPIPs lists where the channels a Relay was asked to open went.
func directTCPIPs(relay *sshhost.Host) []string {
	var targets []string
	for _, r := range relay.Requests() {
		if r.Channel && r.Type == "direct-tcpip" {
			targets = append(targets, r.Target)
		}
	}
	return targets
}

func TestLogsInThroughARelay(t *testing.T) {
	kokoKey, relay, host := relayedHost(t)
	r := target(relay, hostconn.KeyLogin(kokoKey), hostconn.PinKey(relay.HostKey()))

	conn, err := connect(t, target(host, hostconn.KeyLogin(kokoKey), hostconn.PinKey(host.HostKey())), &r)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if got, want := ssh.FingerprintSHA256(conn.HostKey), ssh.FingerprintSHA256(host.HostKey()); got != want {
		t.Errorf("HostKey = %s, want the Host's %s", got, want)
	}
	hostAddr := net.JoinHostPort(host.Address(), strconv.Itoa(host.Port()))
	if got := directTCPIPs(relay); !slices.Equal(got, []string{hostAddr}) {
		t.Errorf("the Relay forwarded to %q, want [%s]", got, hostAddr)
	}
	if got := host.Logins(); len(got) != 1 || !got[0].OK {
		t.Errorf("the Host received logins %+v, want one accepted", got)
	}
}

// Closing the connection to a Host closes the one to its Relay too.
func TestClosingAHostClosesItsRelay(t *testing.T) {
	kokoKey, relay, host := relayedHost(t)
	r := target(relay, hostconn.KeyLogin(kokoKey), hostconn.PinKey(relay.HostKey()))
	c := hostconn.Connector{Timings: fast}
	conn, err := c.Connect(t.Context(), target(host, hostconn.KeyLogin(kokoKey), hostconn.PinKey(host.HostKey())), &r)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}

	if err := conn.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}

	eventually(t, func() bool { return relay.Open() == 0 }, "the Relay still has a connection open after Close")
}

// The Relay's own pinned host key is checked as a Host's is, and a failure on
// the Relay's leg says so.
func TestChecksTheRelaysPinnedHostKey(t *testing.T) {
	kokoKey, relay, host := relayedHost(t)
	pinned := sshhost.NewKey(t, ssh.KeyAlgoED25519).PublicKey()
	r := target(relay, hostconn.KeyLogin(kokoKey), hostconn.PinKey(pinned))

	_, err := connect(t, target(host, hostconn.KeyLogin(kokoKey), hostconn.PinKey(host.HostKey())), &r)

	if _, ok := errors.AsType[*hostconn.RelayError](err); !ok {
		t.Errorf("Connect error = %v, want a RelayError", err)
	}
	mismatch, ok := errors.AsType[*hostconn.HostKeyMismatchError](err)
	if !ok {
		t.Fatalf("Connect error = %v, want a host key mismatch", err)
	}
	if got, want := mismatch.Offered, ssh.FingerprintSHA256(relay.HostKey()); got != want {
		t.Errorf("Offered = %s, want the Relay's %s", got, want)
	}
	if got := relay.Logins(); len(got) != 0 {
		t.Errorf("the Relay received logins %+v, want none", got)
	}
	if got := host.Conns(); got != 0 {
		t.Errorf("the Host was connected to %d times, want never", got)
	}
}

// A Host its Relay cannot reach is retried through the same Relay, and the
// failure is the Host's leg, not the Relay's.
func TestRetriesAHostItsRelayCannotReach(t *testing.T) {
	kokoKey, relay, _ := relayedHost(t)
	r := target(relay, hostconn.KeyLogin(kokoKey), hostconn.PinKey(relay.HostKey()))

	_, err := connect(t, down(t, hostconn.KeyLogin(kokoKey)), &r)

	if !errors.Is(err, hostconn.ErrUnreachable) {
		t.Fatalf("Connect error = %v, want ErrUnreachable", err)
	}
	if _, ok := errors.AsType[*hostconn.RelayError](err); ok {
		t.Errorf("Connect error = %v, want the Host's leg, not the Relay's", err)
	}
	if got := directTCPIPs(relay); len(got) != 3 {
		t.Errorf("the Relay forwarded %d times (%q), want 3", len(got), got)
	}
	if got := relay.Conns(); got != 1 {
		t.Errorf("the Relay was connected to %d times, want once", got)
	}
}

// A fingerprint pin, as `--host-key` gives it, finds the pinned key among the
// Host's keys whatever its type, and the login is sent once, to that key.
func TestAFingerprintPinFindsThePinnedKey(t *testing.T) {
	key := sshhost.NewKey(t, ssh.KeyAlgoED25519)
	rsaKey := sshhost.NewKey(t, ssh.KeyAlgoRSA)
	h := sshhost.Start(t, sshhost.AuthorizedKey(key.PublicKey()),
		sshhost.HostKeys(sshhost.NewKey(t, ssh.KeyAlgoED25519), sshhost.NewKey(t, ssh.KeyAlgoECDSA256), rsaKey))
	fp := ssh.FingerprintSHA256(rsaKey.PublicKey())

	conn, err := connect(t, target(h, hostconn.KeyLogin(key), hostconn.PinFingerprint(fp)), nil)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if got := ssh.FingerprintSHA256(conn.HostKey); got != fp {
		t.Errorf("HostKey = %s, want the pinned %s", got, fp)
	}
	if got := h.Logins(); len(got) != 1 || !got[0].OK {
		t.Errorf("the Host received logins %+v, want one accepted", got)
	}
}

// A fingerprint pin that matches none of the Host's keys is refused with the
// fingerprint of the key the Host offered first, before any login is sent.
func TestAFingerprintPinRefusesEveryOtherKey(t *testing.T) {
	key := sshhost.NewKey(t, ssh.KeyAlgoED25519)
	ed25519Key := sshhost.NewKey(t, ssh.KeyAlgoED25519)
	h := sshhost.Start(t, sshhost.AuthorizedKey(key.PublicKey()),
		sshhost.HostKeys(sshhost.NewKey(t, ssh.KeyAlgoECDSA256), ed25519Key))
	pinned := ssh.FingerprintSHA256(sshhost.NewKey(t, ssh.KeyAlgoED25519).PublicKey())

	_, err := connect(t, target(h, hostconn.KeyLogin(key), hostconn.PinFingerprint(pinned)), nil)

	mismatch, ok := errors.AsType[*hostconn.HostKeyMismatchError](err)
	if !ok {
		t.Fatalf("Connect error = %v, want a host key mismatch", err)
	}
	if mismatch.Pinned != pinned {
		t.Errorf("Pinned = %s, want %s", mismatch.Pinned, pinned)
	}
	if want := ssh.FingerprintSHA256(ed25519Key.PublicKey()); mismatch.Offered != want {
		t.Errorf("Offered = %s, want the ed25519 key the Host offers first, %s", mismatch.Offered, want)
	}
	if got := h.Logins(); len(got) != 0 {
		t.Errorf("the Host received logins %+v, want none", got)
	}
}

// An attempt that fails after the host key passed has sent the login, so it
// is not retried: a key or a password reaches a Host at most once per
// Connect.
func TestAnAttemptThatSentTheLoginIsNotRetried(t *testing.T) {
	h := sshhost.Start(t, sshhost.Password("correct horse"), sshhost.SlowLogins(time.Minute))
	c := hostconn.Connector{Timings: timing.Timings{HostConnectAttempt: time.Second, HostConnectWait: fast.HostConnectWait}}

	_, err := c.Connect(t.Context(), target(h, hostconn.PasswordLogin("correct horse"), hostconn.PinKey(h.HostKey())), nil)

	if err == nil || errors.Is(err, hostconn.ErrUnreachable) || errors.Is(err, hostconn.ErrLoginRefused) {
		t.Fatalf("Connect error = %v, want the attempt's timeout, not retried", err)
	}
	if got := h.Conns(); got != 1 {
		t.Errorf("the Host was connected to %d times, want once", got)
	}
}

// A Relay whose connection breaks while the connector is reaching the Host
// through it is named as the broken leg, and the Host is not tried again
// through it.
func TestALostRelayIsTheRelaysLeg(t *testing.T) {
	// Registered before the Hosts start, so it waits for Connect after they
	// have stopped.
	var wg sync.WaitGroup
	t.Cleanup(wg.Wait)
	kokoKey, relay, host := relayedHost(t, sshhost.StallFirst(1))
	r := target(relay, hostconn.KeyLogin(kokoKey), hostconn.PinKey(relay.HostKey()))
	c := hostconn.Connector{Timings: fast}
	done := make(chan error, 1)
	wg.Go(func() {
		conn, err := c.Connect(t.Context(), target(host, hostconn.KeyLogin(kokoKey), hostconn.PinKey(host.HostKey())), &r)
		if err == nil {
			_ = conn.Close()
		}
		done <- err
	})
	eventually(t, func() bool { return host.Conns() > 0 }, "the connector never reached the Host through the Relay")

	relay.DropConns()

	err := <-done
	if _, ok := errors.AsType[*hostconn.RelayError](err); !ok {
		t.Errorf("Connect error = %v, want a RelayError", err)
	}
	if got := host.Conns(); got != 1 {
		t.Errorf("the Host was connected to %d times, want once", got)
	}
}

// A Host that takes a password both ways gets a wrong one once, not once for
// each method, so it counts one failed login.
func TestAPasswordIsSentOnce(t *testing.T) {
	h := sshhost.Start(t, sshhost.Password("correct horse"), sshhost.KeyboardInteractive("correct horse"))

	_, err := connect(t, target(h, hostconn.PasswordLogin("wrong"), hostconn.PinKey(h.HostKey())), nil)

	if !errors.Is(err, hostconn.ErrLoginRefused) {
		t.Fatalf("Connect error = %v, want ErrLoginRefused", err)
	}
	if got := h.Logins(); len(got) != 1 {
		t.Errorf("the Host received logins %+v, want one", got)
	}
}

// A Relay is always Managed, so one without a pin is refused before the
// connector reaches it.
func TestARelayMustBePinned(t *testing.T) {
	kokoKey, relay, host := relayedHost(t)
	r := target(relay, hostconn.KeyLogin(kokoKey), hostconn.Pin{})

	_, err := connect(t, target(host, hostconn.KeyLogin(kokoKey), hostconn.PinKey(host.HostKey())), &r)

	if _, ok := errors.AsType[*hostconn.RelayError](err); !ok {
		t.Errorf("Connect error = %v, want a RelayError", err)
	}
	if got := relay.Conns(); got != 0 {
		t.Errorf("the Relay was connected to %d times, want never", got)
	}
}
