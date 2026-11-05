// Package serverconn reaches the koko server. All CLI traffic opens its
// channels (api@koko, web@koko and tunnel@koko) through one interface, Conn.
// DialSocket implements it over the Unix socket as the Local caller, and
// DialDevice over a direct Device Connection to the koko server a device
// joined, as its Device Session; Join opens the Device Connection that
// joins. The Device Mux implements Conn later, so commands written against
// Conn do not change.
package serverconn

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"slices"
	"strings"

	"golang.org/x/crypto/ssh"

	"github.com/cieons/koko/internal/shared/joincode"
	"github.com/cieons/koko/internal/shared/wire"
)

// Conn is one way to the koko server.
type Conn interface {
	// Report returns the koko server's version and operations report, which
	// it sends unasked once the connection has authenticated
	// (wire.RequestReport), waiting for it if it has not arrived yet. It
	// fails when the connection ends without one, or ctx ends first.
	Report(ctx context.Context) (wire.Report, error)
	// OpenChannel opens a channel of the given type, such as wire.ChannelAPI,
	// with its open payload, nil for none.
	OpenChannel(ctx context.Context, kind string, payload []byte) (io.ReadWriteCloser, error)
	// Wait blocks until the connection has ended, and returns why.
	Wait() error
	// Close ends the connection and every channel on it.
	Close() error
}

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
	stop := context.AfterFunc(ctx, func() { _ = nc.Close() })
	defer stop()
	conn, chans, reqs, err := ssh.NewClientConn(nc, path, &ssh.ClientConfig{
		User:            user,
		ClientVersion:   wire.Version,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // the socket's directory is its pin
	})
	if err != nil {
		_ = nc.Close()
		return nil, fmt.Errorf("SSH handshake: %w", err)
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
	pinned := func(_ string, _ net.Addr, key ssh.PublicKey) error {
		if slices.ContainsFunc(d.HostKeys, func(k ssh.PublicKey) bool { return bytes.Equal(k.Marshal(), key.Marshal()) }) {
			return nil
		}
		e := &HostKeyMismatchError{Address: d.Address, Presented: ssh.FingerprintSHA256(key)}
		for _, k := range d.HostKeys {
			e.Pinned = append(e.Pinned, ssh.FingerprintSHA256(k))
		}
		return e
	}
	var banner string
	client, err := dialTCP(ctx, d.Address, &ssh.ClientConfig{
		HostKeyCallback: pinned,
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
	return client, nil
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
	client, err := dialTCP(ctx, address, &ssh.ClientConfig{
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
// handshake with cfg, as user koko with koko's version string, or gives up
// when ctx ends.
func dialTCP(ctx context.Context, address string, cfg *ssh.ClientConfig) (*sshConn, error) {
	var d net.Dialer
	nc, err := d.DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, err
	}
	stop := context.AfterFunc(ctx, func() { _ = nc.Close() })
	defer stop()
	cfg.User, cfg.ClientVersion = user, wire.Version
	conn, chans, reqs, err := ssh.NewClientConn(nc, address, cfg)
	if err != nil {
		_ = nc.Close()
		return nil, err
	}
	return newSSHConn(conn, chans, reqs), nil
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
	reported  chan struct{}
	report    wire.Report
	reportErr error
}

// newSSHConn returns a Conn over an SSH connection that has authenticated.
// It takes the koko server's global requests itself: it keeps the first
// report and refuses every other request, as ssh.Client does.
func newSSHConn(conn ssh.Conn, chans <-chan ssh.NewChannel, reqs <-chan *ssh.Request) *sshConn {
	none := make(chan *ssh.Request)
	close(none)
	c := &sshConn{client: ssh.NewClient(conn, chans, none), reported: make(chan struct{})}
	go c.takeRequests(reqs)
	return c
}

// takeRequests answers the koko server's global requests until the
// connection ends, keeping the first report.
func (c *sshConn) takeRequests(reqs <-chan *ssh.Request) {
	got := false
	for r := range reqs {
		if !got && r.Type == wire.RequestReport {
			c.report, c.reportErr = wire.ParseReport(r.Payload)
			got = true
			close(c.reported)
		}
		if r.WantReply {
			_ = r.Reply(false, nil)
		}
	}
	if !got {
		c.reportErr = errors.New("the connection to the koko server ended before it reported its version")
		close(c.reported)
	}
}

func (c *sshConn) Report(ctx context.Context) (wire.Report, error) {
	select {
	case <-c.reported:
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
	return c.client.Wait()
}

func (c *sshConn) Close() error {
	return c.client.Close()
}
