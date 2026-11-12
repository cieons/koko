// Package hostconn is the Host connector: the koko server's one way to log in
// to a Host, used by Enroll and the Gateway. It logs in as the Host Account
// with a key or a password, through at most one Relay, and checks each host
// key against its pin before sending anything to log in with.
package hostconn

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"slices"
	"strconv"
	"sync"
	"syscall"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/cieons/koko/internal/shared/liveness"
	"github.com/cieons/koko/internal/shared/sockerr"
	"github.com/cieons/koko/internal/shared/sshconn"
	"github.com/cieons/koko/internal/shared/timing"
	"github.com/cieons/koko/internal/shared/wire"
)

// Connector logs in to Hosts. Its zero value is the product.
type Connector struct {
	// Timings is the timing-constants hook, for tests only.
	Timings timing.Timings
}

// Target is one end the connector logs in to: a Host or a Relay.
type Target struct {
	// Address and Port are where its sshd listens.
	Address string
	Port    int
	// Account is the Host Account the connector logs in as.
	Account string
	// Login is how it proves it may.
	Login Login
	// Pin is the host key it must offer.
	Pin Pin
}

func (t Target) addr() string {
	return net.JoinHostPort(t.Address, strconv.Itoa(t.Port))
}

// Pin is the host key a Host must offer. The zero Pin pins nothing: the
// connector then accepts the key offered and returns it, for Enroll to pin.
type Pin struct {
	// key is nil for a pin that knows only the fingerprint.
	key         ssh.PublicKey
	fingerprint string
}

// PinKey pins key, as Enroll stored it.
func PinKey(key ssh.PublicKey) Pin {
	return Pin{key: key, fingerprint: ssh.FingerprintSHA256(key)}
}

func (p Pin) isZero() bool { return p.fingerprint == "" }

// PinFingerprint pins only a key's SHA256 fingerprint, in the "SHA256:..."
// form `--host-key` takes. Not knowing the key's type, the connector asks for
// each type the Host holds in turn, all before it logs in.
func PinFingerprint(fingerprint string) Pin {
	return Pin{fingerprint: fingerprint}
}

// Login is how the connector proves it may log in as a Host Account: a
// private key or a password. It holds the secret only inside the SSH
// authentication methods, so printing a Login or a Target never shows it.
type Login struct {
	methods []ssh.AuthMethod
	// once, when set, ends the login after its first refused method.
	once ssh.ClientAuthCallback
}

// KeyLogin logs in with a private key: the koko key, or a key supplied to
// Enroll.
func KeyLogin(key ssh.Signer) Login {
	return Login{methods: []ssh.AuthMethod{ssh.PublicKeys(key)}}
}

// PasswordLogin logs in with a password supplied to Enroll. It answers the
// "password" method and, for an sshd that asks through PAM, each no-echo
// question of "keyboard-interactive", as a person typing it into ssh would.
func PasswordLogin(password string) Login {
	answer := func(_, _ string, questions []string, echos []bool) ([]string, error) {
		answers := make([]string, len(questions))
		for i := range questions {
			if echos[i] {
				return nil, errors.New("the Host asked for more than a password")
			}
			answers[i] = password
		}
		return answers, nil
	}
	// A Host that offers both methods would otherwise get the password once
	// for each, and count two failed logins.
	once := func(c *ssh.ClientAuthContext) (ssh.AuthMethod, error) {
		if slices.Contains(c.TriedMethods, "password") || slices.Contains(c.TriedMethods, "keyboard-interactive") {
			return nil, errors.New("the Host refused the password")
		}
		return nil, nil
	}
	return Login{methods: []ssh.AuthMethod{ssh.Password(password), ssh.KeyboardInteractive(answer)}, once: once}
}

// Conn is a logged-in SSH connection to a Host, through its Relay if it has
// one. Close it with Conn.Close, which also closes the Relay's connection;
// the embedded Client's own Close does not.
//
// The connector watches each leg (package liveness): after Timings.HostProbe
// without a byte from the other end it sends a keepalive@openssh.com,
// wanting a reply, and after Timings.HostDead without any it ends the
// connection, which Broken then tells.
type Conn struct {
	*ssh.Client
	// HostKey is the host key the Host offered: the pinned one, or, with no
	// pin, the key Enroll pins.
	HostKey ssh.PublicKey
	conn    *watchedConn
	relay   *leg
}

// Close closes the connection to the Host, then the one to its Relay.
func (c *Conn) Close() error {
	err := c.Client.Close()
	if c.relay != nil {
		err = errors.Join(err, c.relay.Close())
	}
	return err
}

// Broken returns why the connection ended, if something other than Close
// ended it: a *liveness.DeadError, its Quiet Timings.HostDead, when the
// connector ended it after its probe went unanswered, or what the network or
// the other end ended it with, such as io.EOF for a Host that closed it; a
// *RelayError wraps an end on the Relay's leg. It returns nil while the
// connection lives, and once Close has closed it. A channel on the
// connection that ends because the connection did already sees it non-nil.
func (c *Conn) Broken() error {
	if c.relay != nil {
		if err := c.relay.conn.broken(); err != nil {
			return &RelayError{Err: err}
		}
	}
	return c.conn.broken()
}

// leg is one logged-in SSH connection the connector made: to a Host, or to
// its Relay.
type leg struct {
	*ssh.Client
	conn *watchedConn
}

// watchedConn is the byte stream beneath a leg: it notes when bytes last
// arrived, for liveness.Conn.Watch, and why it ended, before the SSH client
// above it learns of the end.
type watchedConn struct {
	*liveness.Conn
	// done is closed once the connection has ended, err saying why.
	done chan struct{}
	mu   sync.Mutex
	err  error
}

// errClosed is a connection the connector itself closed.
var errClosed = errors.New("closed by koko")

func watch(nc net.Conn) *watchedConn {
	return &watchedConn{Conn: liveness.NewConn(nc), done: make(chan struct{})}
}

func (c *watchedConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	if err != nil {
		c.end(err)
	}
	return n, err
}

func (c *watchedConn) Write(p []byte) (int, error) {
	n, err := c.Conn.Write(p)
	if err != nil {
		c.end(err)
	}
	return n, err
}

func (c *watchedConn) Close() error {
	c.end(errClosed)
	return c.Conn.Close()
}

// end records err as why the connection ended, unless it had ended.
func (c *watchedConn) end(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err == nil {
		c.err = err
		close(c.done)
	}
}

// broken returns why the connection ended, or nil while it lives or once
// the connector closed it.
func (c *watchedConn) broken() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if errors.Is(c.err, errClosed) {
		return nil
	}
	return c.err
}

// keepAlive watches l, as Conn says, until its connection ends. A dead one
// is noted as dead for dead, the rule it broke, before it is closed.
func keepAlive(l *leg, probe, dead time.Duration) {
	if err := l.conn.Watch(l.Client, probe, dead, nil, l.conn.done); err != nil {
		l.conn.end(&liveness.DeadError{Quiet: dead})
		_ = l.Close()
	}
}

// ErrLoginRefused is a login the Host turned down after its host key passed:
// it rejected the key or the password, or disconnected during the login. The
// connector never retries one.
var ErrLoginRefused = errors.New("login refused")

// ErrUnreachable is a Host or Relay that could not be reached in three
// attempts, each ended by a network error before the login: refused,
// unreachable, timed out or reset. It wraps the last attempt's error.
var ErrUnreachable = errors.New("unreachable")

// HostKeyMismatchError is a Host or Relay that offered a host key other than
// the pinned one. The connector sent it nothing to log in with, and never
// retries.
type HostKeyMismatchError struct {
	// Pinned and Offered are SHA256 fingerprints, in the "SHA256:..." form.
	Pinned  string
	Offered string
	// offeredType is the offered key's type, which a fingerprint-only pin
	// asks for no more.
	offeredType string
}

func (e *HostKeyMismatchError) Error() string {
	return fmt.Sprintf("the host key does not match the pinned one (pinned %s, offered %s)", e.Pinned, e.Offered)
}

// RelayError is a failure on the Relay's leg: logging in to the Relay, or
// losing the connection to it while connecting through it. Any other error
// from Connect is on the Host's leg.
type RelayError struct {
	Err error
}

func (e *RelayError) Error() string { return e.Err.Error() }

func (e *RelayError) Unwrap() error { return e.Err }

// Connect logs in to host, through relay unless it is nil. The Relay's leg
// gets the same retries and host key check as the Host's, and a Relay must
// have a pin: it is always Managed.
func (c *Connector) Connect(ctx context.Context, host Target, relay *Target) (*Conn, error) {
	dial := dialTCP
	var via *leg
	if relay != nil {
		if relay.Pin.isZero() {
			return nil, &RelayError{Err: fmt.Errorf("the Relay at %s has no pinned host key", relay.addr())}
		}
		l, _, err := c.login(ctx, *relay, dialTCP)
		if err != nil {
			return nil, &RelayError{Err: fmt.Errorf("log in to the Relay at %s as %q: %w", relay.addr(), relay.Account, err)}
		}
		via = l
		dial = func(ctx context.Context, addr string) (net.Conn, error) {
			nc, err := via.DialContext(ctx, "tcp", addr)
			if _, refused := errors.AsType[*ssh.OpenChannelError](err); err != nil && !refused && ctx.Err() == nil {
				// The Relay did not answer the channel open: its own
				// connection is gone.
				return nil, &relayLostError{err}
			}
			return nc, err
		}
	}
	l, key, err := c.login(ctx, host, dial)
	if err != nil {
		if via != nil {
			_ = via.Close()
		}
		if _, lost := errors.AsType[*relayLostError](err); lost {
			return nil, &RelayError{Err: fmt.Errorf("connect through the Relay at %s: %w", relay.addr(), err)}
		}
		return nil, fmt.Errorf("log in to %s as %q: %w", host.addr(), host.Account, err)
	}
	return &Conn{Client: l.Client, HostKey: key, conn: l.conn, relay: via}, nil
}

// relayLostError is a Relay connection that ended while the connector was
// connecting through it.
type relayLostError struct{ err error }

func (e *relayLostError) Error() string { return "lost the connection to the Relay: " + e.err.Error() }

func (e *relayLostError) Unwrap() error { return e.err }

// dialFunc opens the byte stream an attempt runs SSH over: a TCP connection,
// or a channel through a Relay.
type dialFunc func(ctx context.Context, addr string) (net.Conn, error)

func dialTCP(ctx context.Context, addr string) (net.Conn, error) {
	var d net.Dialer
	return d.DialContext(ctx, "tcp", addr)
}

// attempts is how many times the connector tries to connect to a Host or a
// Relay before giving up.
const attempts = 3

// login logs in to t, retrying an attempt that ends in a network error before
// the login, so a key or a password is offered at most once, and keeps the
// connection alive from then on.
func (c *Connector) login(ctx context.Context, t Target, dial dialFunc) (*leg, ssh.PublicKey, error) {
	timings := c.Timings.Resolve()
	wait := timings.HostConnectWait
	for n := 1; ; n++ {
		l, key, retry, err := attempt(ctx, t, dial, timings.HostConnectAttempt)
		switch {
		case err == nil:
			go keepAlive(l, timings.HostProbe, timings.HostDead)
			return l, key, nil
		case !retry || ctx.Err() != nil:
			return nil, nil, err
		case n == attempts:
			return nil, nil, fmt.Errorf("%w after %d attempts: %w", ErrUnreachable, n, err)
		}
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return nil, nil, context.Cause(ctx)
		}
		wait *= 2
	}
}

// attempt makes one attempt to log in to t within timeout, and says whether a
// failed one is worth another. Under a fingerprint-only pin, a Host that
// offers another key is asked again without that key's type, until it offers
// the pinned key or has no other.
func attempt(ctx context.Context, t Target, dial dialFunc, timeout time.Duration) (*leg, ssh.PublicKey, bool, error) {
	actx, cancel := context.WithTimeoutCause(ctx, timeout,
		fmt.Errorf("no answer within %v: %w", timeout, os.ErrDeadlineExceeded))
	defer cancel()
	algorithms := hostKeyAlgorithms(t.Pin)
	var first *HostKeyMismatchError
	for {
		client, key, retry, err := handshake(ctx, actx, t, dial, algorithms)
		mismatch, isMismatch := errors.AsType[*HostKeyMismatchError](err)
		_, noOtherKey := errors.AsType[*ssh.AlgorithmNegotiationError](err)
		switch {
		case first != nil && noOtherKey:
			// The Host holds no other type: report the key it offered
			// first.
			return nil, nil, false, first
		case !isMismatch || t.Pin.key != nil:
			return client, key, retry, err
		}
		if first == nil {
			first = mismatch
		}
		algorithms = slices.DeleteFunc(algorithms, func(a string) bool {
			return slices.Contains(algorithmsFor(mismatch.offeredType), a)
		})
		if len(algorithms) == 0 {
			return nil, nil, false, first
		}
	}
}

// handshake opens a stream with dial and runs the SSH handshake over it,
// login included, asking for the host key algorithms given. It ends when
// actx does.
func handshake(ctx, actx context.Context, t Target, dial dialFunc, algorithms []string) (*leg, ssh.PublicKey, bool, error) {
	// failed says why the handshake ended: the caller gave up, the attempt
	// ran out of time, or err.
	failed := func(err error) error {
		switch {
		case ctx.Err() != nil:
			return context.Cause(ctx)
		case actx.Err() != nil:
			return context.Cause(actx)
		default:
			return err
		}
	}

	dialled, err := dial(actx, t.addr())
	if err != nil {
		err = failed(err)
		return nil, nil, retryable(err), err
	}
	nc := watch(dialled)
	// The handshake takes no context: closing the connection ends it.
	stop := context.AfterFunc(actx, func() { _ = nc.Close() })
	var offered ssh.PublicKey
	conn, chans, reqs, err := ssh.NewClientConn(nc, t.addr(), &ssh.ClientConfig{
		User:              t.Account,
		Auth:              t.Login.methods,
		AuthCallback:      t.Login.once,
		ClientVersion:     wire.Version,
		HostKeyAlgorithms: algorithms,
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			if fp := ssh.FingerprintSHA256(key); !t.Pin.isZero() && fp != t.Pin.fingerprint {
				return &HostKeyMismatchError{Pinned: t.Pin.fingerprint, Offered: fp, offeredType: key.Type()}
			}
			offered = key
			return nil
		},
	})
	if !stop() {
		// The attempt's time ran out, or the caller gave up, and closed nc.
		if err == nil {
			_ = conn.Close()
		}
		err = failed(err)
	}
	switch {
	case err == nil:
		return &leg{Client: ssh.NewClient(sshconn.Wrap(conn), chans, reqs), conn: nc}, offered, false, nil
	case offered == nil:
		return nil, nil, retryable(err), err
	case retryable(err):
		// The host key passed, so the login was sent: never send it again.
		return nil, nil, false, fmt.Errorf("lost the connection during the login: %w", err)
	default:
		return nil, nil, false, fmt.Errorf("%w: %w", ErrLoginRefused, err)
	}
}

// retryable says whether err is a network error worth another attempt:
// refused, unreachable, timed out, or reset (or closed) by the other end,
// whether met by the koko server or by a Relay.
// Anything else, an authentication failure or a host key mismatch among
// them, would fail the same way again.
func retryable(err error) bool {
	if _, lost := errors.AsType[*relayLostError](err); lost {
		return false // the Host's leg cannot recover without the Relay's
	}
	if sockerr.Is(err,
		syscall.ECONNREFUSED,
		syscall.ENETUNREACH, syscall.EHOSTUNREACH,
		syscall.ETIMEDOUT,
		syscall.ECONNRESET, syscall.ECONNABORTED, syscall.EPIPE,
	) {
		return true
	}
	if ne, ok := errors.AsType[net.Error](err); ok && ne.Timeout() {
		return true
	}
	// A Relay reports that it could not connect to the Host this way, for
	// each of the reasons above.
	if oe, ok := errors.AsType[*ssh.OpenChannelError](err); ok && oe.Reason == ssh.ConnectionFailed {
		return true
	}
	return errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF)
}

// preferredHostKeyAlgorithms are the host key algorithms the connector asks a
// Host for, in OpenSSH's order: ed25519, then ECDSA, then RSA with SHA-2.
// Certificates are left out, since koko pins keys, and so are DSA and SHA-1
// RSA, which OpenSSH no longer accepts by default.
var preferredHostKeyAlgorithms = []string{
	ssh.KeyAlgoED25519,
	ssh.KeyAlgoECDSA256, ssh.KeyAlgoECDSA384, ssh.KeyAlgoECDSA521,
	ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSASHA256,
}

// hostKeyAlgorithms puts a pinned key's own algorithms first, so a Host that
// still holds that key offers it, and one that no longer does offers
// another, which is then refused as a mismatch with both fingerprints.
func hostKeyAlgorithms(pin Pin) []string {
	if pin.key == nil {
		return slices.Clone(preferredHostKeyAlgorithms)
	}
	first := algorithmsFor(pin.key.Type())
	rest := slices.DeleteFunc(slices.Clone(preferredHostKeyAlgorithms), func(a string) bool {
		return slices.Contains(first, a)
	})
	return slices.Concat(first, rest)
}

// algorithmsFor lists the host key algorithms that sign with a key of
// keyType.
func algorithmsFor(keyType string) []string {
	if keyType == ssh.KeyAlgoRSA {
		return []string{ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSASHA256}
	}
	return []string{keyType}
}
