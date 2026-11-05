// Package gateway is the Gateway: it ends every SSH Session and opens its
// own SSH connection to the Host, logging in with the koko key, which never
// leaves the koko server (ADR 0002; P0 spec: reach, "The Gateway").
//
// Each tunnel@koko channel is one Tunnel: a resumable stream (ADR 0021)
// beneath one SSH Session. The Gateway sends the Tunnel's id first, then runs
// an SSH server on the stream with "none" authentication, the koko server's
// sealed host key and AES-GCM ciphers only. It allows a new SSH Session only
// when the caller passes the access check for the Host and the Host is
// Managed, and dials the Host while the client's handshake runs; every
// refusal is a banner on the refused "none". Once the client is in, each
// channel it opens is opened on the Host as asked, and the two are relayed
// both ways: every byte, EOF and request (relay.go).
//
// The Tunnel registry holds every Tunnel the Gateway accepted, by its id,
// for as long as it lives.
package gateway

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/cieons/koko/internal/server/access"
	"github.com/cieons/koko/internal/server/deployment"
	"github.com/cieons/koko/internal/server/host"
	"github.com/cieons/koko/internal/server/hostconn"
	"github.com/cieons/koko/internal/server/op"
	"github.com/cieons/koko/internal/server/seal"
	"github.com/cieons/koko/internal/server/store"
	"github.com/cieons/koko/internal/server/transport"
	"github.com/cieons/koko/internal/shared/api"
	"github.com/cieons/koko/internal/shared/name"
	"github.com/cieons/koko/internal/shared/resumable"
	"github.com/cieons/koko/internal/shared/timing"
	"github.com/cieons/koko/internal/shared/wire"
)

// Gateway takes the tunnel@koko channels of every connection to the koko
// server (transport.Tunnels).
type Gateway struct {
	store *store.Store
	// rootKey opens the koko server's host key and the koko key.
	rootKey   *seal.RootKey
	connector hostconn.Connector
	log       *slog.Logger

	mu     sync.Mutex
	closed bool
	// tunnels is the Tunnel registry.
	tunnels map[tunnelID]*tunnel
	wg      sync.WaitGroup
}

// New returns a Gateway that reads the record in st, opens the koko server's
// keys with rootKey, connects to Hosts as timings say (the zero Timings is
// the product's), and logs each SSH Session to log.
func New(st *store.Store, rootKey *seal.RootKey, timings timing.Timings, log *slog.Logger) *Gateway {
	return &Gateway{
		store:     st,
		rootKey:   rootKey,
		connector: hostconn.Connector{Timings: timings},
		log:       log,
		tunnels:   map[tunnelID]*tunnel{},
	}
}

// tunnelID is a Tunnel's id: 128 random bits, written in logs as 32
// lowercase hex characters.
type tunnelID [wire.TunnelIDSize]byte

func (id tunnelID) String() string { return hex.EncodeToString(id[:]) }

// tunnel is one Tunnel in the registry.
type tunnel struct {
	id     tunnelID
	caller op.Caller
	// from is the address of the connection the Tunnel came on.
	from   net.Addr
	stream *resumable.Stream
	// ctx ends with the Tunnel, end ending it with why.
	ctx context.Context
	end context.CancelCauseFunc
}

// Why a Tunnel ends, besides its client or its Host closing the SSH Session.
var (
	// errLost: the Tunnel's channel failed. Until Tunnels Resume, that ends
	// the Tunnel and its SSH Session.
	errLost = errors.New("device connection lost")
	// errStopping: the koko server is stopping.
	errStopping = errors.New("server stopping")
	// errEnded: the SSH Session has ended, or never started.
	errEnded = errors.New("the SSH Session ended")
)

// Open takes one tunnel@koko channel the connection c asks for. It refuses a
// payload it cannot read, and refuses every channel before the koko server is
// initialised, since the Gateway has no host key to present until then.
// Otherwise it accepts the Tunnel and serves it in a goroutine of its own.
func (g *Gateway) Open(c *transport.Connection, nch ssh.NewChannel) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		_ = nch.Reject(ssh.ConnectionFailed, "the koko server is stopping")
		return
	}
	g.wg.Go(func() { g.serve(c, nch) })
}

// Close ends every Tunnel, refuses new ones, and waits for the Tunnels'
// goroutines. Ending a Tunnel closes its stream, which waits until its
// channel took the close or failed, so Close runs beside the transport's
// Close, which ends a connection too stalled to take it.
func (g *Gateway) Close() {
	g.mu.Lock()
	g.closed = true
	for _, t := range g.tunnels {
		t.end(errStopping)
	}
	g.mu.Unlock()
	g.wg.Wait()
}

// serve serves one Tunnel from its channel open until it ends.
func (g *Gateway) serve(c *transport.Connection, nch ssh.NewChannel) {
	caller := c.Caller()
	from := []any{"device_session", deviceSession(caller), "addr", c.RemoteAddr()}
	kind, sshName, err := wire.ParseTunnelPayload(nch.ExtraData())
	if err == nil && kind != wire.TunnelKindOpen {
		err = fmt.Errorf("this koko server takes no tunnel@koko %q", kind)
	}
	if err != nil {
		g.log.Info("tunnel refused", append(from, "err", err)...)
		_ = nch.Reject(ssh.ConnectionFailed, err.Error())
		return
	}
	hostName := wire.TunnelHost(sshName)
	a, err := g.admit(c.Context(), caller, hostName)
	if errors.Is(err, errNotInitialised) {
		g.log.Info("tunnel refused", append(from, "name", sshName, "err", err)...)
		_ = nch.Reject(ssh.ConnectionFailed, err.Error())
		return
	}
	if err != nil {
		g.log.Error("open a Tunnel", append(from, "name", sshName, "err", err)...)
		_ = nch.Reject(ssh.ConnectionFailed, "the koko server could not open this Tunnel; its log says why")
		return
	}
	ch, reqs, err := nch.Accept()
	if err != nil {
		g.log.Debug("accept a tunnel@koko channel", "addr", c.RemoteAddr(), "err", err)
		return
	}
	g.wg.Go(func() { ssh.DiscardRequests(reqs) })

	t, ok := g.register(caller, c.RemoteAddr(), resumable.New(ch))
	if !ok {
		_ = t.stream.Close()
		return
	}
	defer g.unregister(t)
	// Until Tunnels Resume, losing the channel ends the Tunnel. Whatever ends
	// it closes its stream, which a client in its handshake waits on, and
	// which also ends this watch.
	g.wg.Go(func() {
		<-t.stream.Detached()
		if t.stream.Err() == nil {
			t.end(errLost)
		}
	})
	context.AfterFunc(t.ctx, func() { _ = t.stream.Close() })
	defer t.end(errEnded)

	if _, err := t.stream.Write(t.id[:]); err != nil {
		return
	}
	g.session(t, sshName, a)
}

// register puts a new Tunnel on stream in the registry, under a new id. It
// fails once the Gateway is closing.
func (g *Gateway) register(caller op.Caller, from net.Addr, stream *resumable.Stream) (*tunnel, bool) {
	t := &tunnel{caller: caller, from: from, stream: stream}
	_, _ = rand.Read(t.id[:]) // never fails
	t.ctx, t.end = context.WithCancelCause(context.Background())
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		t.end(errStopping)
		return t, false
	}
	g.tunnels[t.id] = t
	return t, true
}

func (g *Gateway) unregister(t *tunnel) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.tunnels, t.id)
}

// admission is what the Gateway reads for a new Tunnel: the koko server's
// keys, who is calling, and the Host they asked for, or the banner that
// refuses them.
type admission struct {
	// hostKey is the koko server's host key, which the Gateway presents
	// inside every Tunnel; kokoKey logs in to the Host.
	hostKey ssh.Signer
	kokoKey ssh.Signer
	who     access.Identity
	host    host.Reach
	// refusal is the banner that refuses the SSH Session, or "" when it may
	// start, and reason says why in the log.
	refusal string
	reason  string
}

// errRefused is a "none" the Gateway refused with a banner.
var errRefused = errors.New("the Gateway refused the SSH Session")

// errNotInitialised refuses a Tunnel before the koko server has its keys.
var errNotInitialised = errors.New("the koko server is not initialised, so it has no host key to present yet")

// admit reads, in one read-only transaction, the koko server's keys, and
// whether the caller may start a new SSH Session to the Host named hostName:
// the access check passes and the Host is Managed. An unknown name, a Host the
// caller may not reach and a Retired Host, which holds no name, get the same
// refusal, byte for byte. It fails only on an error of the koko server's
// own.
func (g *Gateway) admit(ctx context.Context, caller op.Caller, hostName string) (admission, error) {
	var a admission
	err := g.store.Read(ctx, func(tx store.Tx) error {
		keys, err := deployment.Open(ctx, tx, g.rootKey)
		switch {
		case err != nil:
			return err
		case keys == nil:
			return errNotInitialised
		}
		if a.hostKey, err = ssh.NewSignerFromKey(keys.Host); err != nil {
			return fmt.Errorf("use the host key: %w", err)
		}
		if a.kokoKey, err = ssh.NewSignerFromKey(keys.Koko); err != nil {
			return fmt.Errorf("use the koko key: %w", err)
		}
		if name.Check(hostName) != nil {
			// Not a Host name, such as a Host's id: no SSH Name names it.
			a.refusal, a.reason = unknownHost(hostName), "unknown host"
			return nil
		}
		a.who, a.host, err = host.ReachOf(ctx, tx, caller, hostName)
		if e, ok := errors.AsType[*api.Error](err); ok {
			a.refusal, a.reason = refusal(hostName, e), e.Code
			return nil
		}
		if err != nil {
			return err
		}
		switch a.host.State {
		case api.HostManaged:
		case api.HostRegistered:
			a.refusal, a.reason = fmt.Sprintf(
				"koko: %s is Registered: koko logs in to it only once it is Enrolled, with koko host enroll %s.\n", hostName, hostName),
				"host registered"
		default:
			a.refusal, a.reason = unknownHost(hostName), "host "+a.host.State
		}
		return nil
	})
	if err != nil {
		return admission{}, fmt.Errorf("decide whether the caller may reach %q: %w", hostName, err)
	}
	return a, nil
}

// unknownHost is the banner for a name no Host the caller can reach holds:
// the same for a Host in a Space they cannot act in, so the Gateway tells
// nothing of Hosts outside their Spaces.
func unknownHost(hostName string) string {
	return fmt.Sprintf("koko: no Host is named %q.\n", hostName)
}

// refusal is the banner for a caller the access check refused with e: the
// unknown Host's for a Host they cannot see, and otherwise the refusal
// itself, such as a Revoked Device Session's.
func refusal(hostName string, e *api.Error) string {
	if e.Code == api.CodeUnknownHost.Name || e.Code == api.CodeInvalidName.Name {
		return unknownHost(hostName)
	}
	return "koko: " + e.Message + ".\n"
}

// hostLeg is the Gateway's SSH connection to the Host, dialled while the
// client's handshake runs: done is closed once conn or err holds the
// outcome.
type hostLeg struct {
	done chan struct{}
	conn *hostconn.Conn
	err  error
}

// session runs the SSH Session inside the Tunnel t to the Host a admitted:
// it dials the Host while the client's handshake runs, answers the client's
// "none" with the outcome, then relays the client's channels to the Host
// until either end closes the SSH Session or the Tunnel ends.
func (g *Gateway) session(t *tunnel, sshName string, a admission) {
	var leg *hostLeg
	if a.refusal == "" {
		leg = &hostLeg{done: make(chan struct{})}
		target := hostconn.Target{
			Address: a.host.Address,
			Port:    a.host.Port,
			Account: a.host.Account,
			Login:   hostconn.KeyLogin(a.kokoKey),
			Pin:     hostconn.PinKey(a.host.Pin),
		}
		g.wg.Go(func() {
			defer close(leg.done)
			leg.conn, leg.err = g.connector.Connect(t.ctx, target, nil)
		})
		defer func() {
			<-leg.done
			if leg.conn != nil {
				_ = leg.conn.Close()
			}
		}()
	}
	// Before the wait for the Host above: a client that gives up during the
	// dial stops it.
	defer t.end(errEnded)

	// hostErr is why the Host could not be reached, once the client's "none"
	// has waited for it.
	var hostErr error
	cfg := &ssh.ServerConfig{
		Config:        ssh.Config{Ciphers: wire.TunnelCiphers},
		ServerVersion: wire.Version,
		NoClientAuth:  true,
		NoClientAuthCallback: func(ssh.ConnMetadata) (*ssh.Permissions, error) {
			if a.refusal != "" {
				return nil, &ssh.BannerError{Err: errRefused, Message: a.refusal}
			}
			select {
			case <-leg.done:
			case <-t.ctx.Done():
				return nil, context.Cause(t.ctx)
			}
			if hostErr = leg.err; hostErr != nil {
				return nil, &ssh.BannerError{
					Err:     hostErr,
					Message: fmt.Sprintf("koko: koko could not connect to %s; this SSH Session did not start.\n", a.host.Name),
				}
			}
			return nil, nil
		},
	}
	cfg.AddHostKey(a.hostKey)
	counted := &countingConn{Conn: wire.TunnelConn(t.stream, wire.TunnelAddr("gateway"), t.from)}
	client, chans, reqs, err := ssh.NewServerConn(counted, cfg)
	if err != nil {
		attrs := []any{"tunnel", t.id, "name", sshName, "device_session", deviceSession(t.caller), "addr", t.from}
		switch {
		case a.refusal != "":
			g.log.Info("ssh session refused", append(attrs, "reason", a.reason)...)
		case hostErr != nil:
			g.log.Warn("ssh session refused", append(attrs, "reason", "host connection failed", "err", hostErr)...)
		default:
			g.log.Debug("ssh session ended before it started", append(attrs, "err", err)...)
		}
		return
	}

	started := time.Now()
	who := []any{"tunnel", t.id, "user", a.who.User.Name, "device_session", deviceSession(t.caller), "host", a.host.Name, "host_id", a.host.ID}
	g.log.Info("ssh session started", slices.Concat(who, []any{"addr", t.from})...)
	s := &sshSession{host: leg.conn}
	why := s.relay(t.ctx, client, chans, reqs)
	g.log.Info("ssh session ended", slices.Concat(who, []any{
		"duration", time.Since(started).Round(time.Millisecond),
		"bytes_from_device", counted.read.Load(), "bytes_to_device", counted.written.Load(),
	}, s.exit(), []any{"reason", why})...)
}

// deviceSession names who a Tunnel came from in a log line: its Device
// Session's id, or the Unix socket, as other lines name who acted.
func deviceSession(c op.Caller) string {
	if c.DeviceSession == "" {
		return "unix socket"
	}
	return c.DeviceSession
}

// countingConn counts the bytes of a Tunnel each way.
type countingConn struct {
	net.Conn
	read, written atomic.Int64
}

func (c *countingConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	c.read.Add(int64(n))
	return n, err
}

func (c *countingConn) Write(p []byte) (int, error) {
	n, err := c.Conn.Write(p)
	c.written.Add(int64(n))
	return n, err
}
