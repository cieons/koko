// Package serverconn reaches the koko server. All CLI traffic opens its
// channels (api@koko, web@koko and tunnel@koko) through one interface, Conn.
// DialSocket implements it over the Unix socket as the Local caller, and
// DialDevice over a direct Device Connection to the koko server a device
// joined, as its Device Session; Join opens the Device Connection that
// joins. The Device Mux (package devicemux) implements it over its own
// socket with Handshake, so commands written against Conn do not change.
package serverconn

import (
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"slices"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/cieons/koko/internal/shared/joincode"
	"github.com/cieons/koko/internal/shared/liveness"
	"github.com/cieons/koko/internal/shared/sshconn"
	"github.com/cieons/koko/internal/shared/wire"
)

// Conn is one way to the koko server.
type Conn interface {
	// Report returns the koko server's version and operations report, which
	// it sends unasked once the connection has authenticated
	// (wire.RequestReport), waiting for it if it has not arrived yet; over
	// a Device Mux, the report of the Device Connection it holds now. It
	// fails when the connection ends without one, or ctx ends first.
	Report(ctx context.Context) (wire.Report, error)
	// OpenChannel opens a channel of the given type, such as wire.ChannelAPI,
	// with its open payload, nil for none.
	OpenChannel(ctx context.Context, kind string, payload []byte) (io.ReadWriteCloser, error)
	// Wait blocks until the connection has ended, and returns why: an error
	// wrapping ErrRevoked when the koko server ended it because its Device
	// Session was Revoked, ErrDisconnected when koko disconnect stopped the
	// Device Mux it went through, and a *liveness.DeadError when nothing
	// came from the koko server for too long.
	Wait() error
	// Close ends the connection and every channel on it.
	Close() error
}

// ErrRevoked says the koko server ended a Device Connection because its
// Device Session was Revoked (wire.RequestRevoked).
var ErrRevoked = errors.New("the koko server ended the connection: this device's Device Session was revoked")

// ErrDisconnected says koko disconnect stopped the Device Mux a connection
// went through (wire.RequestDisconnected).
var ErrDisconnected = errors.New("koko disconnect stopped the Device Mux")

// user is the SSH user name koko sends; it identifies nobody.
const user = "koko"

// DialSocket connects over the koko server's Unix socket at path, as the
// Local caller. The socket pins no host key: it sits in a directory only koko
// can write (ADR 0008). When nothing is at path, the error wraps
// fs.ErrNotExist.
func DialSocket(ctx context.Context, path string) (Conn, error) {
	var d net.Dialer
	nc, err := d.DialContext(ctx, "unix", path)
	if err != nil {
		return nil, err
	}
	conn, err := Handshake(ctx, nc, path, &ssh.ClientConfig{
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // the socket's directory is its pin
	})
	if err != nil {
		return nil, fmt.Errorf("SSH handshake: %w", err)
	}
	return conn, nil
}

// Handshake runs the SSH handshake with cfg over nc, a connection to addr,
// with koko's version string, as user koko unless cfg names another (the
// Device Mux takes the command's home there), and returns the Conn on it.
// It gives up when ctx ends. On failure it closes nc.
func Handshake(ctx context.Context, nc net.Conn, addr string, cfg *ssh.ClientConfig) (Conn, error) {
	c, err := handshake(ctx, nc, addr, cfg)
	if err != nil {
		return nil, err
	}
	return c, nil
}

func handshake(ctx context.Context, nc net.Conn, addr string, cfg *ssh.ClientConfig) (*sshConn, error) {
	stop := context.AfterFunc(ctx, func() { _ = nc.Close() })
	defer stop()
	cfg.User, cfg.ClientVersion = cmp.Or(cfg.User, user), wire.Version
	conn, chans, reqs, err := ssh.NewClientConn(nc, addr, cfg)
	if err != nil {
		_ = nc.Close()
		return nil, err
	}
	return newSSHConn(conn, chans, reqs), nil
}

// Device is what a direct Device Connection needs: where the koko server
// the device joined is, the host keys pinned for it, and the device's key.
type Device struct {
	// Address is the Server Address, host:port.
	Address string
	// HostKeys are the pinned host keys; the koko server must present one.
	HostKeys []ssh.PublicKey
	// Key is the device's private key, its Device Session's.
	Key ssh.Signer
	// Probe and Dead, when not zero, watch the connection's liveness
	// (package liveness): after Probe without a byte from the koko server it
	// probes it, and after Dead without any it closes the connection, whose
	// Wait then returns a *liveness.DeadError.
	Probe, Dead time.Duration
	// ProbeNow, when not nil, makes it probe at once each time a value
	// arrives, as on a wake or a route change.
	ProbeNow <-chan struct{}
}

// ErrUnknownKey says the koko server holds no Device Session with the
// device's key: it asked for a Join Code's secret instead.
var ErrUnknownKey = errors.New("the koko server holds no Device Session with this device's key")

// HostKeyMismatchError says the koko server at Address presented a host key
// other than the one pinned for it. Nothing was sent past the SSH key
// exchange.
type HostKeyMismatchError struct {
	Address string
	// Presented is the fingerprint of the key presented, and Pinned of the
	// key or keys pinned, as OpenSSH prints them.
	Presented string
	Pinned    []string
}

func (e *HostKeyMismatchError) Error() string {
	return fmt.Sprintf("the koko server at %s presented the host key %s, not the pinned %s",
		e.Address, e.Presented, strings.Join(e.Pinned, " or "))
}

// RefusedError says the koko server refused to authenticate the device; its
// banner, if it sent one, says why.
type RefusedError struct {
	Banner string
}

func (e *RefusedError) Error() string {
	if e.Banner == "" {
		return "the koko server refused this device"
	}
	return e.Banner
}

// RevokedError says the koko server refused the device's key as the key of
// a Revoked Device Session, which it names.
type RevokedError struct {
	// Device is the Revoked Device Session's device name.
	Device string
}

// Error is the banner's sentence without its "koko: " and its full stop,
// such as `the Device Session "laptop" of this device was revoked`.
func (e *RevokedError) Error() string {
	return strings.TrimSuffix(trimBanner(wire.RevokedBanner(e.Device)), ".")
}

// DialDevice opens a direct Device Connection to the koko server d names:
// it checks the host key against the pinned ones before it authenticates,
// then authenticates with the device's key. It fails with a
// *HostKeyMismatchError, with ErrUnknownKey, with a *RevokedError, with a
// *RefusedError, or with the error of a koko server it could not reach.
func DialDevice(ctx context.Context, d Device) (Conn, error) {
	var banner string
	var watched *liveness.Conn
	wrap := func(nc net.Conn) net.Conn {
		if d.Probe == 0 {
			return nc
		}
		watched = liveness.NewConn(nc)
		return watched
	}
	client, err := dialTCP(ctx, d.Address, wrap, &ssh.ClientConfig{
		HostKeyCallback: Pinned(d.Address, d.HostKeys),
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(d.Key)},
		AuthCallback: func(c *ssh.ClientAuthContext) (ssh.AuthMethod, error) {
			switch {
			case slices.Contains(c.PartialSuccessMethods, "publickey"):
				// A koko server that does not know the key answers its
				// signature with a partial success, asking for a Join
				// Code's secret.
				return nil, ErrUnknownKey
			case slices.Contains(c.TriedMethods, "publickey"):
				if device, ok := wire.ParseRevokedBanner(banner); ok {
					return nil, &RevokedError{Device: device}
				}
				return nil, &RefusedError{Banner: trimBanner(banner)}
			}
			return nil, nil
		},
		BannerCallback: func(message string) error {
			banner = message
			return nil
		},
	})
	if err != nil {
		return nil, err
	}
	if watched != nil {
		go client.watch(watched, d.Probe, d.Dead, d.ProbeNow)
	}
	return client, nil
}

// Permanent tells whether err, which DialDevice failed with, is one trying
// again will not change: a host key mismatch, a key the koko server does
// not know or knows as Revoked, or a refusal. Any other, such as a koko
// server it cannot reach, may pass.
func Permanent(err error) bool {
	_, mismatch := errors.AsType[*HostKeyMismatchError](err)
	_, revoked := errors.AsType[*RevokedError](err)
	_, refused := errors.AsType[*RefusedError](err)
	return mismatch || revoked || refused || errors.Is(err, ErrUnknownKey)
}

// Pinned checks the host key the koko server at address presents against
// the keys pinned for it, and accepts any of them; another fails with a
// *HostKeyMismatchError. A Device Connection checks it before it
// authenticates, and koko exec again inside its Tunnel, where the Gateway
// presents the same key.
func Pinned(address string, pinned []ssh.PublicKey) ssh.HostKeyCallback {
	return func(_ string, _ net.Addr, key ssh.PublicKey) error {
		if slices.ContainsFunc(pinned, func(k ssh.PublicKey) bool { return bytes.Equal(k.Marshal(), key.Marshal()) }) {
			return nil
		}
		e := &HostKeyMismatchError{Address: address, Presented: ssh.FingerprintSHA256(key)}
		for _, k := range pinned {
			e.Pinned = append(e.Pinned, ssh.FingerprintSHA256(k))
		}
		return e
	}
}

// Join opens a Device Connection to the koko server at address that makes
// key a Device Session with the Join Code whose host key fingerprint and
// secret are given: it checks the host key against fingerprint before it
// sends anything, authenticates with key, and then presents the secret
// (wire.JoinSecret). It returns the connection, now an ordinary Device
// Connection, and the host key the koko server presented. It fails with a
// *HostKeyMismatchError, with a *RefusedError once the koko server refused
// the secret, or with the error of a koko server it could not reach.
func Join(ctx context.Context, address string, fingerprint [sha256.Size]byte, key ssh.Signer, secret [joincode.SecretSize]byte) (Conn, ssh.PublicKey, error) {
	var hostKey ssh.PublicKey
	check := func(_ string, _ net.Addr, k ssh.PublicKey) error {
		if sha256.Sum256(k.Marshal()) != fingerprint {
			return &HostKeyMismatchError{
				Address:   address,
				Presented: ssh.FingerprintSHA256(k),
				Pinned:    []string{"SHA256:" + base64.RawStdEncoding.EncodeToString(fingerprint[:])},
			}
		}
		hostKey = k
		return nil
	}
	var banner string
	client, err := dialTCP(ctx, address, nil, &ssh.ClientConfig{
		HostKeyCallback: check,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(key),
			ssh.Password(wire.JoinSecret(secret)),
		},
		AuthCallback: func(c *ssh.ClientAuthContext) (ssh.AuthMethod, error) {
			// The new key is never refused but answered with a partial
			// success; a refusal of either step is the koko server's no.
			if slices.Contains(c.TriedMethods, "password") || slices.Contains(c.TriedMethods, "publickey") {
				return nil, &RefusedError{Banner: trimBanner(banner)}
			}
			return nil, nil
		},
		BannerCallback: func(message string) error {
			banner = message
			return nil
		},
	})
	if err != nil {
		return nil, nil, err
	}
	return client, hostKey, nil
}

// dialTCP connects to the koko server at address and runs the SSH
// handshake with cfg, as user koko with koko's version string, over the
// connection wrap returns for it (nil for itself), or gives up when ctx
// ends.
func dialTCP(ctx context.Context, address string, wrap func(net.Conn) net.Conn, cfg *ssh.ClientConfig) (*sshConn, error) {
	var d net.Dialer
	nc, err := d.DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, err
	}
	if wrap != nil {
		nc = wrap(nc)
	}
	return handshake(ctx, nc, address, cfg)
}

// trimBanner turns a koko server's banner, such as "koko: the Join Code is
// invalid or expired.\n", into an error message without its "koko: ".
func trimBanner(banner string) string {
	return strings.TrimPrefix(strings.TrimSpace(banner), "koko: ")
}

// sshConn is a Conn over one SSH connection.
type sshConn struct {
	client *ssh.Client
	// reported is closed once report and reportErr hold the koko server's
	// report, or why there is none.
	reported chan struct{}
	// requestsDone is closed once the connection has ended and every global
	// request it carried has been taken.
	requestsDone chan struct{}

	mu        sync.Mutex
	report    wire.Report
	reportErr error
	// revoked tells that the koko server said, before it ended the
	// connection, that its Device Session was Revoked.
	revoked bool
	// disconnected tells that the Device Mux said, before it ended the
	// connection, that koko disconnect stopped it.
	disconnected bool
	// dead is why the liveness watch ended the connection, if it did.
	dead error
}

// newSSHConn returns a Conn over an SSH connection that has authenticated.
// It takes the koko server's global requests itself: it keeps the latest
// report, notes a revocation, and refuses every other request, as
// ssh.Client does.
func newSSHConn(conn ssh.Conn, chans <-chan ssh.NewChannel, reqs <-chan *ssh.Request) *sshConn {
	none := make(chan *ssh.Request)
	close(none)
	c := &sshConn{
		client:       ssh.NewClient(sshconn.Wrap(conn), chans, none),
		reported:     make(chan struct{}),
		requestsDone: make(chan struct{}),
	}
	go c.takeRequests(reqs)
	return c
}

// takeRequests answers the koko server's global requests until the
// connection ends. A koko server reports once; a Device Mux reports again
// for each Device Connection it opens, and the latest report holds: one
// that does not parse leaves the report unknown, not the one before.
func (c *sshConn) takeRequests(reqs <-chan *ssh.Request) {
	defer close(c.requestsDone)
	got := false
	for r := range reqs {
		switch r.Type {
		case wire.RequestReport:
			report, err := wire.ParseReport(r.Payload)
			c.mu.Lock()
			c.report, c.reportErr = report, err
			c.mu.Unlock()
			if !got {
				got = true
				close(c.reported)
			}
		case wire.RequestRevoked:
			c.mu.Lock()
			c.revoked = true
			c.mu.Unlock()
		case wire.RequestDisconnected:
			c.mu.Lock()
			c.disconnected = true
			c.mu.Unlock()
		}
		if r.WantReply {
			_ = r.Reply(false, nil)
		}
	}
	if !got {
		c.mu.Lock()
		c.reportErr = errors.New("the connection to the koko server ended before it reported its version")
		c.mu.Unlock()
		close(c.reported)
	}
}

func (c *sshConn) Report(ctx context.Context) (wire.Report, error) {
	select {
	case <-c.reported:
		c.mu.Lock()
		defer c.mu.Unlock()
		return c.report, c.reportErr
	case <-ctx.Done():
		return wire.Report{}, fmt.Errorf("wait for the koko server's report: %w", ctx.Err())
	}
}

// OpenChannel opens a channel, or gives up when ctx ends; a channel the koko
// server grants after that is closed at once.
func (c *sshConn) OpenChannel(ctx context.Context, kind string, payload []byte) (io.ReadWriteCloser, error) {
	type opened struct {
		ch   ssh.Channel
		reqs <-chan *ssh.Request
		err  error
	}
	done := make(chan opened, 1)
	go func() {
		ch, reqs, err := c.client.OpenChannel(kind, payload)
		done <- opened{ch, reqs, err}
	}()
	select {
	case o := <-done:
		if o.err != nil {
			return nil, fmt.Errorf("open %s channel: %w", kind, o.err)
		}
		go ssh.DiscardRequests(o.reqs)
		return o.ch, nil
	case <-ctx.Done():
		go func() {
			if o := <-done; o.err == nil {
				go ssh.DiscardRequests(o.reqs)
				_ = o.ch.Close()
			}
		}()
		return nil, fmt.Errorf("open %s channel: %w", kind, ctx.Err())
	}
}

func (c *sshConn) Wait() error {
	err := c.client.Wait()
	// The requests the connection carried before it ended are all taken
	// once this closes, a revocation among them.
	<-c.requestsDone
	c.mu.Lock()
	defer c.mu.Unlock()
	switch {
	case c.revoked:
		return ErrRevoked
	case c.disconnected:
		return ErrDisconnected
	case c.dead != nil:
		return c.dead
	}
	return err
}

// watch watches the connection's liveness over lc until it ends, and closes
// it, noting why, when nothing has come from the koko server for too long.
func (c *sshConn) watch(lc *liveness.Conn, probe, dead time.Duration, probeNow <-chan struct{}) {
	err := lc.Watch(c.client, probe, dead, probeNow, c.requestsDone)
	if err == nil {
		return
	}
	c.mu.Lock()
	c.dead = err
	c.mu.Unlock()
	_ = c.client.Close()
}

func (c *sshConn) Close() error {
	return c.client.Close()
}
