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
	"github.com/cieons/koko/internal/shared/liveness"
	"github.com/cieons/koko/internal/shared/timing"
)

// fast shrinks the wait between attempts so that a test of retries takes
// milliseconds. Each attempt keeps the product's bound: a Host answers it.
var fast = timing.Timings{HostConnectWait: 20 * time.Millisecond}

// tries is how many times a test whose attempt bound is shortened, for a
// Host that never answers a step, runs before it takes the run as it came:
// the bound counts from the attempt's start, so it times the steps before,
// which a Host answers, too, and a run in which it cut one starts again.
const tries = 5

// cutBeforeLogin says whether an attempt to h ran out of time before its
// login reached h, where h answers every step but the login: each attempt
// that got that far carried one login, and none is made after one did.
func cutBeforeLogin(h *sshhost.Host) bool {
	asked := h.LoginsAsked()
	return asked == 0 || h.Conns() > asked
}

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
func connect(t *testing.T, host hostconn.Target, relays []hostconn.Target) (*hostconn.Conn, error) {
	t.Helper()
	c := hostconn.Connector{Timings: fast}
	conn, err := c.Connect(t.Context(), host, relays)
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
	key := sshhost.NewKey(t, ssh.KeyAlgoED25519)
	c := hostconn.Connector{Timings: timing.Timings{HostConnectAttempt: time.Second, HostConnectWait: fast.HostConnectWait}}
	type result struct {
		conn *hostconn.Conn
		err  error
	}
	for try := 1; ; try++ {
		// Registered before the Host starts, so it waits for Connect after
		// the Host has stopped and let go of the held connection.
		var wg sync.WaitGroup
		t.Cleanup(wg.Wait)
		h := sshhost.Start(t, sshhost.AuthorizedKey(key.PublicKey()), sshhost.StallFirst(1))
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
		if r.err == nil {
			t.Cleanup(func() { _ = r.conn.Close() })
		}
		if h.Conns() > 2 && try < tries {
			// The second attempt, which the Host answers, ran out of time
			// too (tries).
			t.Logf("try %d: the Host was connected to %d times; starting again", try, h.Conns())
			continue
		}
		if r.err != nil {
			t.Fatalf("Connect: %v", r.err)
		}
		if got := h.Conns(); got != 2 {
			t.Errorf("the Host was connected to %d times, want 2", got)
		}
		return
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

// relayChain is a Host behind n Relays, each with the koko key authorized.
type relayChain struct {
	kokoKey ssh.Signer
	// relays are in dial order: relays[0] is the one the koko server reaches.
	relays []*sshhost.Host
	host   *sshhost.Host
}

// startChain starts a Host behind n Relays. opts, when set, gives the Relay
// at each hop, and the Host at hop n, more Options.
func startChain(t *testing.T, n int, opts func(hop int) []sshhost.Option) relayChain {
	t.Helper()
	c := relayChain{kokoKey: sshhost.NewKey(t, ssh.KeyAlgoED25519)}
	start := func(hop int) *sshhost.Host {
		o := []sshhost.Option{sshhost.AuthorizedKey(c.kokoKey.PublicKey())}
		if opts != nil {
			o = append(o, opts(hop)...)
		}
		return sshhost.Start(t, o...)
	}
	for hop := range n {
		c.relays = append(c.relays, start(hop))
	}
	c.host = start(n)
	return c
}

// relayTargets are the Targets of c's Relays, in dial order, each pinned to
// its own host key.
func (c relayChain) relayTargets() []hostconn.Target {
	var targets []hostconn.Target
	for _, r := range c.relays {
		targets = append(targets, target(r, hostconn.KeyLogin(c.kokoKey), hostconn.PinKey(r.HostKey())))
	}
	return targets
}

func (c relayChain) hostTarget() hostconn.Target {
	return target(c.host, hostconn.KeyLogin(c.kokoKey), hostconn.PinKey(c.host.HostKey()))
}

// hop is the in-process Host at hop, the Host itself after the last Relay.
func (c relayChain) hop(hop int) *sshhost.Host {
	if hop == len(c.relays) {
		return c.host
	}
	return c.relays[hop]
}

func addr(h *sshhost.Host) string {
	return net.JoinHostPort(h.Address(), strconv.Itoa(h.Port()))
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

// relayHop returns the Hop of err's *RelayError, failing the test if it has
// none.
func relayHop(t *testing.T, err error) int {
	t.Helper()
	e, ok := errors.AsType[*hostconn.RelayError](err)
	if !ok {
		t.Fatalf("error = %v, want a RelayError", err)
	}
	return e.Hop
}

// Each Relay of a chain receives one login with the koko key and is asked
// for one direct-tcpip, to the next hop's address, and the Host last.
func TestLogsInThroughAChainOfRelays(t *testing.T) {
	for _, n := range []int{1, 2, 4} {
		t.Run(strconv.Itoa(n), func(t *testing.T) {
			c := startChain(t, n, nil)

			conn, err := connect(t, c.hostTarget(), c.relayTargets())
			if err != nil {
				t.Fatalf("Connect: %v", err)
			}
			if got, want := ssh.FingerprintSHA256(conn.HostKey), ssh.FingerprintSHA256(c.host.HostKey()); got != want {
				t.Errorf("HostKey = %s, want the Host's %s", got, want)
			}
			kokoLogin := []sshhost.Login{{Account: sshhost.Account, Method: "publickey", Key: ssh.FingerprintSHA256(c.kokoKey.PublicKey()), OK: true}}
			for hop, r := range c.relays {
				if got := r.Logins(); !slices.Equal(got, kokoLogin) {
					t.Errorf("the Relay at hop %d received logins %+v, want %+v", hop, got, kokoLogin)
				}
				if got, want := directTCPIPs(r), []string{addr(c.hop(hop + 1))}; !slices.Equal(got, want) {
					t.Errorf("the Relay at hop %d forwarded to %q, want %q", hop, got, want)
				}
			}
			if got := c.host.Logins(); !slices.Equal(got, kokoLogin) {
				t.Errorf("the Host received logins %+v, want %+v", got, kokoLogin)
			}
		})
	}
}

// A failure while logging in to a Relay's leg names that Relay's hop, the
// legs already made close, and nothing goes on to the hops after it.
func TestAFailureNamesItsHop(t *testing.T) {
	const relays = 3
	otherKey := sshhost.NewKey(t, ssh.KeyAlgoED25519)
	cases := []struct {
		name string
		// from is the first hop the case applies to.
		from int
		opts func(failing, hop int) []sshhost.Option
		// change changes the Relay Targets Connect is given.
		change func(c relayChain, failing int, targets []hostconn.Target)
		check  func(t *testing.T, c relayChain, failing int, err error)
	}{{
		name: "pin mismatch",
		change: func(_ relayChain, failing int, targets []hostconn.Target) {
			targets[failing].Pin = hostconn.PinKey(otherKey.PublicKey())
		},
		check: func(t *testing.T, c relayChain, failing int, err error) {
			mismatch, ok := errors.AsType[*hostconn.HostKeyMismatchError](err)
			if !ok {
				t.Fatalf("Connect error = %v, want a host key mismatch", err)
			}
			if got, want := mismatch.Offered, ssh.FingerprintSHA256(c.relays[failing].HostKey()); got != want {
				t.Errorf("Offered = %s, want the failing Relay's %s", got, want)
			}
			if got := c.relays[failing].Logins(); len(got) != 0 {
				t.Errorf("the failing Relay received logins %+v, want none", got)
			}
		},
	}, {
		name: "key refused",
		change: func(_ relayChain, failing int, targets []hostconn.Target) {
			targets[failing].Login = hostconn.KeyLogin(otherKey)
		},
		check: func(t *testing.T, c relayChain, failing int, err error) {
			if !errors.Is(err, hostconn.ErrLoginRefused) {
				t.Fatalf("Connect error = %v, want ErrLoginRefused", err)
			}
			if got := c.relays[failing].Logins(); len(got) != 1 {
				t.Errorf("the failing Relay received logins %+v, want one", got)
			}
		},
	}, {
		name: "down",
		opts: func(failing, hop int) []sshhost.Option {
			if hop == failing {
				return []sshhost.Option{sshhost.ResetFirst(3)}
			}
			return nil
		},
		check: func(t *testing.T, c relayChain, failing int, err error) {
			if !errors.Is(err, hostconn.ErrUnreachable) {
				t.Fatalf("Connect error = %v, want ErrUnreachable", err)
			}
			if got := c.relays[failing].Conns(); got != 3 {
				t.Errorf("the failing Relay was connected to %d times, want 3", got)
			}
		},
	}, {
		name: "direct-tcpip refused",
		// The first Relay is reached over TCP, through no Relay.
		from: 1,
		change: func(_ relayChain, failing int, targets []hostconn.Target) {
			targets[failing] = down(t, targets[failing].Login)
			targets[failing].Pin = hostconn.PinKey(otherKey.PublicKey())
		},
		check: func(t *testing.T, c relayChain, failing int, err error) {
			if !errors.Is(err, hostconn.ErrUnreachable) {
				t.Fatalf("Connect error = %v, want ErrUnreachable", err)
			}
			if _, ok := errors.AsType[*ssh.OpenChannelError](err); !ok {
				t.Errorf("Connect error = %v, want the refused direct-tcpip open", err)
			}
			if got := directTCPIPs(c.relays[failing-1]); len(got) != 3 {
				t.Errorf("the Relay before forwarded %d times (%q), want 3", len(got), got)
			}
		},
	}}
	for _, tc := range cases {
		for failing := tc.from; failing < relays; failing++ {
			t.Run(tc.name+"/hop "+strconv.Itoa(failing), func(t *testing.T) {
				var opts func(int) []sshhost.Option
				if tc.opts != nil {
					opts = func(hop int) []sshhost.Option { return tc.opts(failing, hop) }
				}
				c := startChain(t, relays, opts)
				targets := c.relayTargets()
				if tc.change != nil {
					tc.change(c, failing, targets)
				}

				_, err := connect(t, c.hostTarget(), targets)

				if got := relayHop(t, err); got != failing {
					t.Errorf("Hop = %d, want %d (%v)", got, failing, err)
				}
				tc.check(t, c, failing, err)
				for hop := range failing {
					if got := c.relays[hop].Logins(); len(got) != 1 || !got[0].OK {
						t.Errorf("the Relay at hop %d received logins %+v, want one accepted", hop, got)
					}
				}
				if got := c.hop(failing + 1).Conns(); got != 0 {
					t.Errorf("the hop after the failing one was connected to %d times, want never", got)
				}
				for hop := range failing + 1 {
					eventually(t, func() bool { return c.relays[hop].Open() == 0 },
						"hop "+strconv.Itoa(hop)+" still has a connection open after Connect failed")
				}
			})
		}
	}
}

// A Relay whose connection ends while the connector dials the next hop
// through it is that Relay's failure, and the next hop is not tried again
// through it.
func TestALostRelayIsThatRelaysHop(t *testing.T) {
	const relays = 3
	for lost := range relays {
		t.Run("hop "+strconv.Itoa(lost), func(t *testing.T) {
			// Registered before the Hosts start, so it waits for Connect after
			// they have stopped.
			var wg sync.WaitGroup
			t.Cleanup(wg.Wait)
			c := startChain(t, relays, func(hop int) []sshhost.Option {
				if hop == lost+1 {
					return []sshhost.Option{sshhost.StallFirst(1)}
				}
				return nil
			})
			connector := hostconn.Connector{Timings: fast}
			done := make(chan error, 1)
			wg.Go(func() {
				conn, err := connector.Connect(t.Context(), c.hostTarget(), c.relayTargets())
				if err == nil {
					_ = conn.Close()
				}
				done <- err
			})
			next := c.hop(lost + 1)
			eventually(t, func() bool { return next.Conns() > 0 }, "the connector never reached the hop after the lost one")

			c.relays[lost].DropConns()

			err := <-done
			if got := relayHop(t, err); got != lost {
				t.Errorf("Hop = %d, want %d (%v)", got, lost, err)
			}
			if got := next.Conns(); got != 1 {
				t.Errorf("the hop after the lost one was connected to %d times, want once", got)
			}
		})
	}
}

// A failure on the Host's own leg, behind a chain, is the Host's.
func TestAFailureBehindAChainOnTheHostsLegIsTheHosts(t *testing.T) {
	c := startChain(t, 2, nil)
	host := c.hostTarget()
	host.Pin = hostconn.PinKey(sshhost.NewKey(t, ssh.KeyAlgoED25519).PublicKey())

	_, err := connect(t, host, c.relayTargets())

	if _, ok := errors.AsType[*hostconn.HostKeyMismatchError](err); !ok {
		t.Fatalf("Connect error = %v, want a host key mismatch", err)
	}
	if _, ok := errors.AsType[*hostconn.RelayError](err); ok {
		t.Errorf("Connect error = %v, want the Host's leg, not a Relay's", err)
	}
}

// A Host its last Relay cannot reach is retried through the same Relay, and
// the failure is the Host's leg, not the Relay's.
func TestRetriesAHostItsRelayCannotReach(t *testing.T) {
	c := startChain(t, 1, nil)

	_, err := connect(t, down(t, hostconn.KeyLogin(c.kokoKey)), c.relayTargets())

	if !errors.Is(err, hostconn.ErrUnreachable) {
		t.Fatalf("Connect error = %v, want ErrUnreachable", err)
	}
	if _, ok := errors.AsType[*hostconn.RelayError](err); ok {
		t.Errorf("Connect error = %v, want the Host's leg, not the Relay's", err)
	}
	if got := directTCPIPs(c.relays[0]); len(got) != 3 {
		t.Errorf("the Relay forwarded %d times (%q), want 3", len(got), got)
	}
	if got := c.relays[0].Conns(); got != 1 {
		t.Errorf("the Relay was connected to %d times, want once", got)
	}
}

// An attempt that fails after a hop's host key passed has sent the login, so
// it is not retried, at a Relay of a chain as at the Host: a key reaches each
// hop at most once per Connect.
func TestALoginReachesEachHopAtMostOnce(t *testing.T) {
	const relays = 2
	for slow := range relays + 1 {
		t.Run("hop "+strconv.Itoa(slow), func(t *testing.T) {
			conn := hostconn.Connector{Timings: timing.Timings{HostConnectAttempt: time.Second, HostConnectWait: fast.HostConnectWait}}
			var c relayChain
			var err error
			for try := 1; ; try++ {
				c = startChain(t, relays, func(hop int) []sshhost.Option {
					if hop == slow {
						return []sshhost.Option{sshhost.SlowLogins(time.Minute)}
					}
					return nil
				})
				_, err = conn.Connect(t.Context(), c.hostTarget(), c.relayTargets())
				// The bound also times every step before the slow hop's
				// login, its own and each Relay's before it (tries).
				if !cutBeforeLogin(c.hop(slow)) || try == tries {
					break
				}
				t.Logf("try %d: an attempt ran out of time before the slow hop's login (%v); starting again", try, err)
			}

			if err == nil || errors.Is(err, hostconn.ErrUnreachable) || errors.Is(err, hostconn.ErrLoginRefused) {
				t.Fatalf("Connect error = %v, want the attempt's timeout, not retried", err)
			}
			if got := c.hop(slow).Conns(); got != 1 {
				t.Errorf("the slow hop was connected to %d times, want once", got)
			}
			for hop := range slow {
				if got := c.relays[hop].Logins(); len(got) != 1 {
					t.Errorf("the Relay at hop %d received logins %+v, want one", hop, got)
				}
			}
		})
	}
}

// Broken names the leg nearest the koko server whose connection ended, as a
// RelayError with its Hop, as soon as the Host's leg has ended with it.
func TestBrokenNamesTheLegNearestTheKokoServer(t *testing.T) {
	const relays = 2
	for dropped := range relays {
		t.Run("hop "+strconv.Itoa(dropped), func(t *testing.T) {
			c := startChain(t, relays, nil)
			conn, err := connect(t, c.hostTarget(), c.relayTargets())
			if err != nil {
				t.Fatalf("Connect: %v", err)
			}
			if err := conn.Broken(); err != nil {
				t.Errorf("a live chain is Broken: %v", err)
			}
			done := make(chan struct{})
			go func() {
				defer close(done)
				_ = conn.Wait()
			}()

			c.relays[dropped].DropConns()

			select {
			case <-done:
			case <-time.After(10 * time.Second):
				t.Fatal("the Host's leg did not end within 10 s of its Relay's")
			}
			broken := conn.Broken()
			if got := relayHop(t, broken); got != dropped {
				t.Errorf("Broken = %v at hop %d, want hop %d", broken, got, dropped)
			}
		})
	}
}

// A Relay that stops answering is the one Broken names, dead for HostDead,
// once the Host's leg has ended: every leg after it, whose bytes it carries,
// falls silent with it, and one found dead first, as the Host's is when the
// Host fell silent first, ends only once its close has passed through the
// Relay, which it never does, so with the Relay's own end. A Host that stops
// answering behind a chain that lives is the Host's.
func TestBrokenNamesTheRelayThatWentSilent(t *testing.T) {
	// dead is long enough that a leg still answering is never taken for
	// silent under load.
	const relays, probe, dead = 2, 20 * time.Millisecond, 1500 * time.Millisecond
	c := hostconn.Connector{Timings: timing.Timings{HostProbe: probe, HostDead: dead}}
	for frozen := range relays + 1 {
		t.Run("hop "+strconv.Itoa(frozen), func(t *testing.T) {
			t.Parallel()
			chain := startChain(t, relays, nil)
			conn, err := c.Connect(t.Context(), chain.hostTarget(), chain.relayTargets())
			if err != nil {
				t.Fatalf("Connect: %v", err)
			}
			t.Cleanup(func() { _ = conn.Close() })
			done := make(chan struct{})
			go func() {
				defer close(done)
				_ = conn.Wait()
			}()

			chain.host.Freeze()
			t.Cleanup(chain.host.Thaw)
			if frozen < relays {
				// Long enough for each leg beneath the Host's to hear its own
				// probe answered after the Host's last byte.
				time.Sleep(10 * probe)
				chain.relays[frozen].Freeze()
				t.Cleanup(chain.relays[frozen].Thaw)
			}

			select {
			case <-done:
			case <-time.After(10 * time.Second):
				t.Fatal("the Host's leg did not end within 10 s")
			}
			broken := conn.Broken()
			if d, ok := errors.AsType[*liveness.DeadError](broken); !ok || d.Quiet != dead {
				t.Errorf("Broken = %v, want no answer for %v", broken, dead)
			}
			relay, isRelay := errors.AsType[*hostconn.RelayError](broken)
			switch {
			case frozen == relays && isRelay:
				t.Errorf("a silent Host behind a live chain: Broken = %v at hop %d, want the Host's", broken, relay.Hop)
			case frozen < relays && (!isRelay || relay.Hop != frozen):
				t.Errorf("a silent Relay at hop %d: Broken = %v, want a RelayError at its hop", frozen, broken)
			}
		})
	}
}

// Closing the connection to a Host behind a chain closes every leg, and
// Broken says nothing of it.
func TestClosingAChainClosesEveryLeg(t *testing.T) {
	c := startChain(t, 2, nil)
	conn, err := (&hostconn.Connector{Timings: fast}).Connect(t.Context(), c.hostTarget(), c.relayTargets())
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}

	if err := conn.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}

	for hop := range len(c.relays) + 1 {
		eventually(t, func() bool { return c.hop(hop).Open() == 0 },
			"hop "+strconv.Itoa(hop)+" still has a connection open after Close")
	}
	if err := conn.Broken(); err != nil {
		t.Errorf("Broken after Close = %v, want nil", err)
	}
}

// The whole Connect is bounded by HostConnect, whatever each leg's own
// attempts would allow, and a leg it cuts is unreachable, even once the
// login was sent.
func TestAChainIsBoundedByHostConnect(t *testing.T) {
	const bound = 300 * time.Millisecond
	for _, tc := range []struct {
		name string
		opt  sshhost.Option
	}{
		{"stalled", sshhost.StallFirst(3)},
		{"slow login", sshhost.SlowLogins(time.Minute)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := startChain(t, 2, func(int) []sshhost.Option { return []sshhost.Option{tc.opt} })
			// Only the wait for an answer that never comes is shortened.
			conn := hostconn.Connector{Timings: timing.Timings{HostConnect: bound}}

			start := time.Now()
			_, err := conn.Connect(t.Context(), c.hostTarget(), c.relayTargets())

			if !errors.Is(err, hostconn.ErrUnreachable) || errors.Is(err, hostconn.ErrLoginRefused) {
				t.Fatalf("Connect error = %v, want ErrUnreachable", err)
			}
			if got := relayHop(t, err); got != 0 {
				t.Errorf("Hop = %d, want 0 (%v)", got, err)
			}
			if got := time.Since(start); got >= timing.HostConnectAttempt {
				t.Errorf("Connect gave up after %v, want the bound to cut it before an attempt's %v", got, timing.HostConnectAttempt)
			}
			if got := c.relays[0].Conns(); got != 1 {
				t.Errorf("the first Relay was connected to %d times, want once", got)
			}
		})
	}
}

// A Relay is always Managed, so a chain holding one without a pin is refused
// before the connector reaches any.
func TestEveryRelayMustBePinned(t *testing.T) {
	c := startChain(t, 2, nil)
	targets := c.relayTargets()
	targets[1].Pin = hostconn.Pin{}

	_, err := connect(t, c.hostTarget(), targets)

	if got := relayHop(t, err); got != 1 {
		t.Errorf("Hop = %d, want 1 (%v)", got, err)
	}
	if got := c.relays[0].Conns(); got != 0 {
		t.Errorf("the first Relay was connected to %d times, want never", got)
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
	c := hostconn.Connector{Timings: timing.Timings{HostConnectAttempt: time.Second, HostConnectWait: fast.HostConnectWait}}
	var h *sshhost.Host
	var err error
	for try := 1; ; try++ {
		h = sshhost.Start(t, sshhost.Password("correct horse"), sshhost.SlowLogins(time.Minute))
		_, err = c.Connect(t.Context(), target(h, hostconn.PasswordLogin("correct horse"), hostconn.PinKey(h.HostKey())), nil)
		// The bound also times the steps before the login (tries).
		if !cutBeforeLogin(h) || try == tries {
			break
		}
		t.Logf("try %d: an attempt ran out of time before the login (%v); starting again", try, err)
	}

	if err == nil || errors.Is(err, hostconn.ErrUnreachable) || errors.Is(err, hostconn.ErrLoginRefused) {
		t.Fatalf("Connect error = %v, want the attempt's timeout, not retried", err)
	}
	if got := h.Conns(); got != 1 {
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

// The connector probes a Host it hears nothing from, and ends the connection
// once the Host has sent nothing for HostDead, which Broken then says;
// Broken tells a Host that closed the connection too, and says nothing of a
// connection Close closed.
func TestBrokenSaysWhyTheConnectionEnded(t *testing.T) {
	key := sshhost.NewKey(t, ssh.KeyAlgoED25519)
	login := hostconn.KeyLogin(key)
	// Long enough that a Host still answering is never taken for silent
	// under load.
	const dead = 1500 * time.Millisecond
	c := hostconn.Connector{Timings: timing.Timings{HostProbe: 20 * time.Millisecond, HostDead: dead}}
	connect := func(h *sshhost.Host) *hostconn.Conn {
		t.Helper()
		conn, err := c.Connect(t.Context(), target(h, login, hostconn.PinKey(h.HostKey())), nil)
		if err != nil {
			t.Fatalf("Connect: %v", err)
		}
		t.Cleanup(func() { _ = conn.Close() })
		if err := conn.Broken(); err != nil {
			t.Errorf("a live connection is Broken: %v", err)
		}
		return conn
	}
	// ended waits for conn to end, failing the test after 10 s.
	ended := func(conn *hostconn.Conn, what string) {
		t.Helper()
		done := make(chan struct{})
		go func() {
			defer close(done)
			_ = conn.Wait()
		}()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Fatalf("%s: the connection did not end within 10 s", what)
		}
	}

	silent := sshhost.Start(t, sshhost.AuthorizedKey(key.PublicKey()))
	conn := connect(silent)
	eventually(t, func() bool {
		return slices.ContainsFunc(silent.Requests(), func(r sshhost.Request) bool { return r.Type == "keepalive@openssh.com" })
	}, "a quiet Host received no keepalive@openssh.com within 5 s")
	silent.Freeze()
	ended(conn, "a Host that stopped answering")
	if e, ok := errors.AsType[*liveness.DeadError](conn.Broken()); !ok || e.Quiet != dead {
		t.Errorf("a Host that stopped answering: Broken = %v, want no answer for %v", conn.Broken(), dead)
	}

	closing := sshhost.Start(t, sshhost.AuthorizedKey(key.PublicKey()))
	conn = connect(closing)
	closing.DropConns()
	ended(conn, "a Host that closed the connection")
	if err := conn.Broken(); err == nil {
		t.Error("a Host that closed the connection: Broken = nil, want why")
	}

	conn = connect(closing)
	_ = conn.Close()
	ended(conn, "a connection Close closed")
	if err := conn.Broken(); err != nil {
		t.Errorf("a connection Close closed: Broken = %v, want nil", err)
	}
}
