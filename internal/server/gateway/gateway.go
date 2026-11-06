// Package gateway is the Gateway: it ends every SSH Session and opens its
// own SSH connection to the Host, logging in with the koko key, which never
// leaves the koko server (ADR 0002; P0 spec: reach, "The Gateway").
//
// Each tunnel@koko channel is one Tunnel: a resumable stream (ADR 0021)
// beneath one SSH Session. The Gateway sends the Tunnel's id first, then runs
// an SSH server on the stream with "none" authentication, the koko server's
// sealed host key and AES-GCM ciphers only. It allows a new SSH Session only
// when the caller passes the access check for the Host, the Host is Managed
// and the caller's Device Session has fewer than 64 open, and dials the Host
// while the client's handshake runs; every refusal is a banner on the
// refused "none", the user name and the Host connector's failures among
// them. Once the client is in, each channel it opens is opened on the Host as
// asked, and the two are relayed both ways: every byte, EOF and request but
// agent and X11 forwarding (relay.go). An SSH Session that ends for a
// reason the client did not see coming, the Host's or the koko server's,
// tells it in one line on the stderr of each of its session channels.
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
	// sshSessions counts, by Device Session, the Tunnels in the registry
	// that the access check admitted, attached or Detached: from then until
	// the Tunnel ends, its dial to the Host included.
	sshSessions map[string]int
	wg          sync.WaitGroup
}

// maxSSHSessions is how many SSH Sessions one Device Session may have open
// at once, so that one runaway agent cannot exhaust the koko server.
const maxSSHSessions = 64

// New returns a Gateway that reads the record in st, opens the koko server's
// keys with rootKey, connects to Hosts as timings say (the zero Timings is
// the product's), and logs each SSH Session to log.
func New(st *store.Store, rootKey *seal.RootKey, timings timing.Timings, log *slog.Logger) *Gateway {
	return &Gateway{
		store:       st,
		rootKey:     rootKey,
		connector:   hostconn.Connector{Timings: timings},
		log:         log,
		tunnels:     map[tunnelID]*tunnel{},
		sshSessions: map[string]int{},
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
	// limited says the Tunnel counts against its Device Session's limit.
	limited bool
}

// ending is why the Gateway ends an SSH Session, as its log line's reason
// says, and the notice it gives the client, if any: one line on the stderr
// of each session channel the client has open (wire.TunnelNoticeLine), then
// the reason of the Tunnel's close frame, which koko exec shows when no
// session channel of its took the line.
type ending struct {
	reason string
	notice string
}

func (e *ending) Error() string { return e.reason }

// endingOf returns the ending a Tunnel's end cause carries, or an ending
// with no notice for any other cause.
func endingOf(cause error) *ending {
	if e, ok := errors.AsType[*ending](cause); ok {
		return e
	}
	return &ending{reason: cause.Error()}
}

// Why a Tunnel ends, besides its client or its Host closing the SSH Session.
var (
	// errLost: the Tunnel's channel failed. Until Tunnels Resume, that ends
	// the Tunnel and its SSH Session.
	errLost = &ending{reason: "device connection lost"}
	// errStopping: the koko server is stopping, as for an upgrade.
	errStopping = &ending{reason: "server stopping", notice: "the koko server is restarting; this SSH Session has ended."}
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

// Close ends every Tunnel, telling each SSH Session that the koko server is
// restarting (errStopping), refuses new ones, and waits for the Tunnels'
// goroutines. Ending a Tunnel closes its stream with the notice, which waits
// until its channel took the close or failed, so the transport's Close,
// which ends a connection too stalled to take it, must follow in time.
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

	t, limit, ok := g.register(caller, c.RemoteAddr(), resumable.New(ch), a.refused == nil)
	if !ok {
		_ = t.stream.CloseWithReason(errStopping.notice)
		return
	}
	defer g.unregister(t)
	if limit != nil {
		a.refused = limit
	}
	// Until Tunnels Resume, losing the channel ends the Tunnel. Whatever ends
	// it closes its stream, which a client in its handshake waits on, and
	// which also ends this watch.
	g.wg.Go(func() {
		<-t.stream.Detached()
		if t.stream.Err() == nil {
			t.end(errLost)
		}
	})
	// Until its SSH Session starts, the Tunnel's end closes the stream at
	// once, with the end's notice; once it has started, the relay gives the
	// notice first.
	stopEarlyClose := context.AfterFunc(t.ctx, func() { _ = t.stream.CloseWithReason(endingOf(context.Cause(t.ctx)).notice) })
	defer t.end(errEnded)

	if _, err := t.stream.Write(t.id[:]); err != nil {
		return
	}
	g.session(t, sshName, a, stopEarlyClose)
}

// register puts a new Tunnel on stream in the registry, under a new id. One
// the access check admitted counts against its Device Session's limit,
// unless the Device Session has maxSSHSessions already, when register
// returns the refusal. It fails once the Gateway is closing.
func (g *Gateway) register(caller op.Caller, from net.Addr, stream *resumable.Stream, admitted bool) (*tunnel, *refusal, bool) {
	t := &tunnel{caller: caller, from: from, stream: stream}
	_, _ = rand.Read(t.id[:]) // never fails
	t.ctx, t.end = context.WithCancelCause(context.Background())
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		t.end(errStopping)
		return t, nil, false
	}
	g.tunnels[t.id] = t
	ds := caller.DeviceSession
	switch {
	case ds == "" || !admitted:
	case g.sshSessions[ds] >= maxSSHSessions:
		return t, &refusal{
			banner: fmt.Sprintf("koko: this Device Session already has %d SSH Sessions open, the most koko allows; this SSH Session did not start.\n", maxSSHSessions),
			reason: "ssh session limit",
		}, true
	default:
		g.sshSessions[ds]++
		t.limited = true
	}
	return t, nil, true
}

func (g *Gateway) unregister(t *tunnel) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.tunnels, t.id)
	if ds := t.caller.DeviceSession; t.limited {
		if g.sshSessions[ds]--; g.sshSessions[ds] == 0 {
			delete(g.sshSessions, ds)
		}
	}
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
	// refused refuses the SSH Session, or is nil when it may start.
	refused *refusal
}

// refusal is why the Gateway refuses an SSH Session: the banner its client's
// "none" is refused with, and, for the log line, the reason, any further
// fields, and whether it is logged as a warning.
type refusal struct {
	banner string
	reason string
	attrs  []any
	warn   bool
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
			a.refused = &refusal{banner: unknownHost(hostName), reason: "unknown host"}
			return nil
		}
		a.who, a.host, err = host.ReachOf(ctx, tx, caller, hostName)
		if e, ok := errors.AsType[*api.Error](err); ok {
			a.refused = &refusal{banner: accessRefusal(hostName, e), reason: e.Code}
			return nil
		}
		if err != nil {
			return err
		}
		switch a.host.State {
		case api.HostManaged:
		case api.HostRegistered:
			a.refused = &refusal{
				banner: fmt.Sprintf("koko: %s is Registered: koko logs in to it only once it is Enrolled, with koko host enroll %s.\n", hostName, hostName),
				reason: "host registered",
			}
		default:
			a.refused = &refusal{banner: unknownHost(hostName), reason: "host " + a.host.State}
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

// accessRefusal is the banner for a caller the access check refused with e:
// the unknown Host's for a Host they cannot see, and otherwise the refusal
// itself, such as a Revoked Device Session's.
func accessRefusal(hostName string, e *api.Error) string {
	if e.Code == api.CodeUnknownHost.Name || e.Code == api.CodeInvalidName.Name {
		return unknownHost(hostName)
	}
	return "koko: " + e.Message + ".\n"
}

// userRefusal refuses the SSH user name user for the Host h, which the
// caller may reach: the Gateway takes koko, which koko's ssh_config and koko
// exec ask for, and the Host Account's own name, and no other, so that a
// later release may give other names a meaning. It returns nil for a name it
// takes.
func userRefusal(h host.Reach, user string) *refusal {
	if user == wire.TunnelUser || user == h.Account {
		return nil
	}
	return &refusal{
		banner: fmt.Sprintf("koko: %s is reached as its Host Account %q; leave out %q.\n", h.Name, h.Account, user+"@"),
		reason: "user name", attrs: []any{"ssh_user", user},
	}
}

// connectRefusal refuses an SSH Session to the Host h, which the Host
// connector failed to log in to with err: the Host is down after the
// connector's retries, its host key is not the pinned one, which the banner
// shows beside the one offered and says how to pin, or it refused the koko
// key. Each is logged as a warning, since the Owner may have to act; the
// connector's error, which names addresses, goes to the log only.
func connectRefusal(h host.Reach, err error) *refusal {
	if m, ok := errors.AsType[*hostconn.HostKeyMismatchError](err); ok {
		return &refusal{
			banner: fmt.Sprintf("koko: the host key of %s does not match the one pinned at Enroll (pinned %s, offered %s); koko did not log in. "+
				"If %s was rebuilt, pin its new key with koko host enroll %s --host-key <the fingerprint its provider shows>, "+
				"or with --replace-host-key, which trusts whatever key it offers.\n", h.Name, m.Pinned, m.Offered, h.Name, h.Name),
			reason: "host key mismatch", attrs: []any{"pinned", m.Pinned, "offered", m.Offered}, warn: true,
		}
	}
	r := &refusal{attrs: []any{"err", err}, warn: true}
	switch {
	case errors.Is(err, hostconn.ErrUnreachable):
		r.banner = fmt.Sprintf("koko: %s is down or unreachable: koko could not connect to it; this SSH Session did not start.\n", h.Name)
		r.reason = "host unreachable"
	case errors.Is(err, hostconn.ErrLoginRefused):
		r.banner = fmt.Sprintf("koko: %s refused the koko key for its Host Account %q; this SSH Session did not start.\n", h.Name, h.Account)
		r.reason = "login refused"
	default:
		r.banner = fmt.Sprintf("koko: koko could not connect to %s; this SSH Session did not start.\n", h.Name)
		r.reason = "host connection failed"
	}
	return r
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
// until either end closes the SSH Session or the Tunnel ends. Once the SSH
// Session starts, it calls stopEarlyClose, which stops the Tunnel's end
// from closing the stream before the relay has given its notice.
func (g *Gateway) session(t *tunnel, sshName string, a admission, stopEarlyClose func() bool) {
	var leg *hostLeg
	if a.refused == nil {
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

	// refused is why the client's "none" was refused, if it was.
	refused := a.refused
	cfg := &ssh.ServerConfig{
		Config:        ssh.Config{Ciphers: wire.TunnelCiphers},
		ServerVersion: wire.Version,
		NoClientAuth:  true,
		NoClientAuthCallback: func(md ssh.ConnMetadata) (*ssh.Permissions, error) {
			if refused == nil {
				refused = userRefusal(a.host, md.User())
			}
			if refused != nil {
				return nil, &ssh.BannerError{Err: errRefused, Message: refused.banner}
			}
			select {
			case <-leg.done:
			case <-t.ctx.Done():
				return nil, context.Cause(t.ctx)
			}
			if leg.err != nil {
				refused = connectRefusal(a.host, leg.err)
				return nil, &ssh.BannerError{Err: leg.err, Message: refused.banner}
			}
			return nil, nil
		},
	}
	cfg.AddHostKey(a.hostKey)
	counted := &countingConn{Conn: wire.TunnelConn(t.stream, wire.TunnelAddr("gateway"), t.from)}
	client, chans, reqs, err := ssh.NewServerConn(counted, cfg)
	if err != nil {
		attrs := []any{"tunnel", t.id, "name", sshName, "device_session", deviceSession(t.caller), "addr", t.from}
		if refused == nil {
			g.log.Debug("ssh session ended before it started", append(attrs, "err", err)...)
			return
		}
		level := slog.LevelInfo
		if refused.warn {
			level = slog.LevelWarn
		}
		g.log.Log(t.ctx, level, "ssh session refused", slices.Concat(attrs, []any{"reason", refused.reason}, refused.attrs)...)
		return
	}

	// The relay closes the stream from now on, after its notice. Should the
	// Tunnel have ended just before, the stream is closing with the same
	// notice, which the relay then gives in vain.
	stopEarlyClose()
	started := time.Now()
	who := []any{"tunnel", t.id, "user", a.who.User.Name, "device_session", deviceSession(t.caller), "host", a.host.Name, "host_id", a.host.ID}
	g.log.Info("ssh session started", slices.Concat(who, []any{"addr", t.from})...)
	s := &sshSession{
		host: leg.conn, hostName: a.host.Name, stream: t.stream,
		log:    g.log.With("tunnel", t.id, "host", a.host.Name),
		logged: map[string]bool{}, sessionChannels: map[ssh.Channel]struct{}{},
	}
	end := s.relay(t.ctx, client, chans, reqs)
	g.log.Info("ssh session ended", slices.Concat(who, []any{
		"duration", time.Since(started).Round(time.Millisecond),
		"bytes_from_device", counted.read.Load(), "bytes_to_device", counted.written.Load(),
	}, s.exit(), []any{"reason", end.reason})...)
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
