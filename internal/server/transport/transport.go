// Package transport is the koko server's SSH server. Every way in is an SSH
// connection carrying the same channels (ADR 0015): an api@koko channel is
// handed to the API's HTTP handler and a web@koko channel to the web front's,
// each request with the connection's caller and the connection itself; a
// tunnel@koko channel goes to the Gateway (Tunnels), and any other channel
// type is refused. Each connection is sent the koko
// server's version and operations report once it has authenticated, before
// any channel is served (wire.RequestReport).
//
// The Unix socket speaks SSH with "none" authentication and a host key made in
// memory at each start; every connection on it is the Local caller.
//
// The TCP listener presents the koko server's sealed host key and
// authenticates each Device Connection by its device's key, its Device
// Session. A key no Device Session holds may join, in the same SSH
// authentication: once the device has proved it holds the key, the koko
// server answers with a partial success and takes a Join Code's secret as an
// SSH password (P0 spec: people and Spaces, "Joining"). A Revoked Device
// Session's key is refused once the device has signed with it, with a banner
// that names the device. Devices answers what a key is and whether a secret
// joins it.
package transport

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net"
	"net/http"
	"os"
	"slices"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/cieons/koko/internal/server/op"
	"github.com/cieons/koko/internal/shared/liveness"
	"github.com/cieons/koko/internal/shared/timing"
	"github.com/cieons/koko/internal/shared/wire"
)

// Devices answers, for the TCP listener, what a device's key is and whether
// a Join Code's secret makes it a Device Session. The device area answers;
// the transport only asks.
type Devices interface {
	// Key tells what a device's key is: a live Device Session's, a Revoked
	// one's, or none's.
	Key(ctx context.Context, key ssh.PublicKey) (DeviceKey, error)
	// Join makes key, which the device has proved it holds, a Device Session
	// through the valid Join Code whose secret the password carries
	// (wire.JoinSecret), and returns its id. It fails for any password that
	// is not such a secret, and logs why.
	Join(ctx context.Context, key ssh.PublicKey, password []byte, from net.Addr) (string, error)
	// Used records that a Device Connection of the Device Session opened a
	// channel: what makes a Device Session used.
	Used(ctx context.Context, deviceSession string) error
}

// DeviceKey is what a device's key is to the koko server. The zero
// DeviceKey is a key no Device Session holds, which may join.
type DeviceKey struct {
	// DeviceSession is the id of the Device Session that holds the key, or
	// "" for none.
	DeviceSession string
	// Device is that Device Session's device name.
	Device string
	// Revoked tells that the Device Session is Revoked: the key never
	// authenticates or joins again, and its device is told so by name.
	Revoked bool
}

// joinRefused is the banner a failed join gets, whatever the reason: the
// code was used, expired, voided or never issued, or the secret is not one,
// so trying codes teaches nothing.
const joinRefused = "koko: the Join Code is invalid or expired.\n"

// deviceSessionExt carries, in a Device Connection's ssh.Permissions, the id
// of the Device Session it authenticated as.
const deviceSessionExt = "koko-device-session"

// Tunnels takes the tunnel@koko channels: the Gateway.
type Tunnels interface {
	// Open takes one tunnel@koko channel the connection c asks for: it
	// accepts or rejects it, and returns without waiting for the Tunnel,
	// which outlives the connection when it Resumes elsewhere.
	Open(c *Connection, nch ssh.NewChannel)
}

// Server accepts SSH connections and serves their channels.
type Server struct {
	log *slog.Logger
	api *channelServer
	web *channelServer
	// report returns the version and operations report each connection is
	// sent.
	report func() wire.Report
	// tunnels takes the tunnel@koko channels; nil refuses them as an
	// unknown channel type. HandleTunnels sets it before any way in opens.
	tunnels Tunnels
	// timings are the P0 timings, resolved: how long a Device Connection
	// goes quiet before it is probed, and before it is declared dead.
	// UseTimings sets them before any way in opens.
	timings timing.Timings

	mu        sync.Mutex
	closed    bool
	listeners []net.Listener
	conns     map[*Connection]struct{}
	// revoked are the Device Sessions CloseDeviceSessions closed, whose
	// connections are refused from then on, even one that authenticated just
	// before the revocation committed.
	revoked map[string]bool
	wg      sync.WaitGroup
}

// New returns a Server that hands every api@koko channel to api and every
// web@koko channel to web, and sends each connection the report report
// returns, which it asks for once per connection, so that the report lists
// the operations registered after New.
func New(api, web http.Handler, report func() wire.Report, log *slog.Logger) *Server {
	s := &Server{
		log:     log,
		api:     newChannelServer(api, wire.ChannelAPI, log),
		web:     newChannelServer(web, wire.ChannelWeb, log),
		report:  report,
		timings: timing.Timings{}.Resolve(),
		conns:   map[*Connection]struct{}{},
		revoked: map[string]bool{},
	}
	for _, cs := range []*channelServer{s.api, s.web} {
		s.wg.Go(func() { _ = cs.http.Serve(cs.channels) })
	}
	return s
}

// HandleTunnels hands every tunnel@koko channel to t, the Gateway. Call it
// before ListenSocket and ServeTCP; without it, those channels are refused
// as an unknown channel type.
func (s *Server) HandleTunnels(t Tunnels) {
	s.tunnels = t
}

// UseTimings makes the transport run with t, the koko server's
// timing-constants hook, whose zero value is the product's. Call it before
// ServeTCP.
func (s *Server) UseTimings(t timing.Timings) {
	s.timings = t.Resolve()
}

// Connection is one SSH connection to the koko server, as the requests on its
// channels see it.
type Connection struct {
	ctx    context.Context
	ssh    ssh.Conn
	caller op.Caller

	mu sync.Mutex
	// calls counts the HTTP requests in flight on the connection's
	// channels: each channel's, from its first byte read until its response
	// is written.
	calls int
	// answered, once drain asks for it, is closed when no call is in
	// flight.
	answered chan struct{}
}

// Context returns a context that ends when the connection closes.
func (c *Connection) Context() context.Context {
	return c.ctx
}

// Caller returns who the connection authenticated as: the Local caller or a
// Device Session.
func (c *Connection) Caller() op.Caller {
	return c.caller
}

// RemoteAddr returns the address the connection came from.
func (c *Connection) RemoteAddr() net.Addr {
	return c.ssh.RemoteAddr()
}

// callState records that the channel cc of the connection began a call
// (active) or ended one, as net/http's ConnState reports it.
func (c *Connection) callState(cc *callerConn, active bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if cc.active == active {
		return
	}
	cc.active = active
	if active {
		c.calls++
		return
	}
	c.calls--
	if c.calls == 0 && c.answered != nil {
		select {
		case <-c.answered:
		default:
			close(c.answered)
		}
	}
}

// drain returns a channel closed once no call is in flight on the
// connection.
func (c *Connection) drain() <-chan struct{} {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.answered == nil {
		c.answered = make(chan struct{})
		if c.calls == 0 {
			close(c.answered)
		}
	}
	return c.answered
}

type connectionKey struct{}

// ConnectionFrom returns the SSH connection a request came on.
func ConnectionFrom(ctx context.Context) (*Connection, bool) {
	c, ok := ctx.Value(connectionKey{}).(*Connection)
	return c, ok
}

type webPortKey struct{}

// WebPortFrom returns the port koko web listens on, as the web@koko channel a
// request came on names it; ok is false for a request on another channel.
func WebPortFrom(ctx context.Context) (port int, ok bool) {
	port, ok = ctx.Value(webPortKey{}).(int)
	return port, ok
}

// ListenSocket listens on the Unix socket at path, mode 0666 so that every
// local user can reach it, and serves every connection as the Local caller.
// The socket's directory decides who could plant a socket there; ADR 0008.
func (s *Server) ListenSocket(ctx context.Context, path string) error {
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return fmt.Errorf("make the socket's host key: %w", err)
	}
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		return fmt.Errorf("make the socket's host key: %w", err)
	}
	cfg := &ssh.ServerConfig{NoClientAuth: true, ServerVersion: wire.Version}
	cfg.AddHostKey(signer)

	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "unix", path)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", path, err)
	}
	if err := os.Chmod(path, 0o666); err != nil {
		_ = ln.Close()
		return fmt.Errorf("open %s to every local user: %w", path, err)
	}
	if !s.track(ln) {
		_ = ln.Close()
		return errors.New("the transport is closed")
	}
	s.wg.Go(func() {
		s.accept(ln, wayIn{cfg: cfg, callerOf: func(*ssh.ServerConn) op.Caller { return op.Caller{Local: true} }})
	})
	return nil
}

// ServeTCP serves Device Connections on ln, a TCP listener, presenting
// hostKey, the koko server's sealed host key. Each device authenticates by
// its key, which devices says is a Device Session's, or joins (see the
// package comment). Close closes ln.
func (s *Server) ServeTCP(ln net.Listener, hostKey ssh.Signer, devices Devices) error {
	cfg := &ssh.ServerConfig{
		ServerVersion: wire.Version,
		// Any key may try: what it is is asked only once the device has
		// signed with it, in VerifiedPublicKeyCallback.
		PublicKeyCallback: func(ssh.ConnMetadata, ssh.PublicKey) (*ssh.Permissions, error) {
			return nil, nil
		},
		VerifiedPublicKeyCallback: func(conn ssh.ConnMetadata, key ssh.PublicKey, _ *ssh.Permissions, _ string) (*ssh.Permissions, error) {
			ctx := context.Background()
			k, err := devices.Key(ctx, key)
			switch {
			case err != nil:
				s.log.Error("look up a device's key", "addr", conn.RemoteAddr(), "err", err)
				return nil, err
			case k.Revoked:
				s.log.Info("revoked device refused", "device_session", k.DeviceSession, "addr", conn.RemoteAddr())
				return nil, &ssh.BannerError{
					Err:     fmt.Errorf("the Device Session %s is revoked", k.DeviceSession),
					Message: wire.RevokedBanner(k.Device),
				}
			case k.DeviceSession != "":
				return &ssh.Permissions{Extensions: map[string]string{deviceSessionExt: k.DeviceSession}}, nil
			}
			// No Device Session holds the key: it may join, with a Join
			// Code's secret as the second step.
			return nil, &ssh.PartialSuccessError{Next: ssh.ServerAuthCallbacks{
				PasswordCallback: func(conn ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
					id, err := devices.Join(ctx, key, password, conn.RemoteAddr())
					if err != nil {
						return nil, &ssh.BannerError{Err: err, Message: joinRefused}
					}
					return &ssh.Permissions{Extensions: map[string]string{deviceSessionExt: id}}, nil
				},
			}}
		},
	}
	cfg.AddHostKey(hostKey)
	if !s.track(ln) {
		_ = ln.Close()
		return errors.New("the transport is closed")
	}
	s.wg.Go(func() {
		s.accept(ln, wayIn{
			cfg: cfg,
			callerOf: func(conn *ssh.ServerConn) op.Caller {
				return op.Caller{DeviceSession: conn.Permissions.Extensions[deviceSessionExt]}
			},
			devices: devices,
			watch:   true,
		})
	})
	return nil
}

// wayIn is one way into the koko server: how its connections authenticate,
// who each one is once it has, and for the TCP listener the device area.
type wayIn struct {
	cfg      *ssh.ServerConfig
	callerOf func(*ssh.ServerConn) op.Caller
	// devices is the device area the TCP listener asks; nil on the Unix
	// socket, which has no Device Session.
	devices Devices
	// watch probes each connection once it goes quiet, and closes it as
	// dead once it stays quiet (package liveness): a Device Connection's
	// network path can die without a word, a Unix socket's cannot.
	watch bool
}

// accept serves each connection ln accepts, the way w says.
func (s *Server) accept(ln net.Listener, w wayIn) {
	for {
		nc, err := ln.Accept()
		if err != nil {
			if !errors.Is(err, net.ErrClosed) {
				s.log.Error("accept connection", "err", err)
			}
			return
		}
		s.wg.Go(func() { s.serve(nc, w) })
	}
}

// serve runs one SSH connection until it ends. Each channel a Device
// Connection opens uses its Device Session.
func (s *Server) serve(nc net.Conn, w wayIn) {
	var watched *liveness.Conn
	if w.watch {
		watched = liveness.NewConn(nc)
		nc = watched
	}
	conn, chans, reqs, err := ssh.NewServerConn(nc, w.cfg)
	if err != nil {
		s.log.Debug("ssh handshake failed", "addr", nc.RemoteAddr(), "err", err)
		_ = nc.Close()
		return
	}
	caller := w.callerOf(conn)
	ctx, ended := context.WithCancel(context.Background())
	defer ended()
	connection := &Connection{ctx: ctx, ssh: conn, caller: caller}
	if !s.trackConn(connection) {
		if s.isRevoked(caller.DeviceSession) {
			// It authenticated while its revocation committed.
			disconnectRevoked(conn)
		}
		_ = conn.Close()
		return
	}
	defer s.untrackConn(connection)
	// Before any channel is served, so the report reaches the client before
	// the answer to its first channel open.
	if _, _, err := conn.SendRequest(wire.RequestReport, false, s.report().Marshal()); err != nil {
		s.log.Debug("send the report", "addr", nc.RemoteAddr(), "err", err)
		_ = conn.Close()
		return
	}
	s.wg.Go(func() { ssh.DiscardRequests(reqs) })
	if watched != nil {
		s.wg.Go(func() { s.watch(connection, watched) })
	}
	for nch := range chans {
		cc := &callerConn{caller: caller, conn: connection}
		kind := nch.ChannelType()
		var dest *channelServer
		switch {
		case kind == wire.ChannelAPI:
			dest = s.api
		case kind == wire.ChannelWeb:
			port, err := wire.ParseWebPayload(nch.ExtraData())
			if err != nil {
				_ = nch.Reject(ssh.ConnectionFailed, err.Error())
				continue
			}
			dest, cc.webPort = s.web, port
		case kind == wire.ChannelTunnel && s.tunnels != nil:
			// The Gateway takes it, below.
		default:
			_ = nch.Reject(ssh.UnknownChannelType, fmt.Sprintf("koko serves no %q channel", kind))
			continue
		}
		if caller.DeviceSession != "" {
			// What a Device Session last did is bookkeeping: failing to
			// record it fails no channel.
			if err := w.devices.Used(ctx, caller.DeviceSession); err != nil {
				s.log.Error("record a Device Session's use", "device_session", caller.DeviceSession, "err", err)
			}
		}
		if kind == wire.ChannelTunnel {
			s.tunnels.Open(connection, nch)
			continue
		}
		ch, chReqs, err := nch.Accept()
		if err != nil {
			s.log.Debug("accept channel", "err", err)
			continue
		}
		s.wg.Go(func() { ssh.DiscardRequests(chReqs) })
		cc.Conn = wire.Conn(ch)
		dest.channels.push(cc)
	}
}

// watch probes the Device Connection c, over lc, once nothing has come from
// the device for timing.ServerProbe, and closes it as dead once nothing has
// for timing.ServerDead, until it ends.
func (s *Server) watch(c *Connection, lc *liveness.Conn) {
	err := lc.Watch(c.ssh, s.timings.ServerProbe, s.timings.ServerDead, nil, c.ctx.Done())
	if err == nil {
		return
	}
	s.log.Info("device connection closed", "device_session", c.caller.DeviceSession,
		"addr", c.ssh.RemoteAddr(), "reason", "dead", "err", err)
	_ = c.ssh.Close()
}

// shutdownGrace bounds how long Close, and the closing of a Revoked Device
// Session's connection, wait for calls in flight.
const shutdownGrace = 5 * time.Second

// CloseDeviceSessions closes the Device Connections of the Device Sessions
// named, which were Revoked, and refuses any connection of theirs from now
// on. Each connection closes once the calls in flight on it have answered,
// such as a device's call that revoked its own Device Session, or after
// shutdownGrace. It does not block.
func (s *Server) CloseDeviceSessions(ids ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	for _, id := range ids {
		s.revoked[id] = true
	}
	for c := range s.conns {
		if id := c.caller.DeviceSession; id != "" && slices.Contains(ids, id) {
			s.wg.Go(func() { s.closeRevoked(c) })
		}
	}
}

// closeRevoked closes c, a Revoked Device Session's connection, once its
// calls in flight have answered, at most shutdownGrace from now.
func (s *Server) closeRevoked(c *Connection) {
	select {
	case <-c.drain():
	case <-c.ctx.Done():
		return
	case <-time.After(shutdownGrace):
	}
	s.log.Info("device connection closed", "device_session", c.caller.DeviceSession,
		"addr", c.ssh.RemoteAddr(), "reason", "revoked")
	disconnectRevoked(c.ssh)
	_ = c.ssh.Close()
}

// disconnectRevoked tells a Revoked Device Session's connection why it is
// about to close (wire.RequestRevoked), so the device runs the revocation
// cleanup at once. SSH has no reason on a close, and x/crypto/ssh sends no
// disconnect message; a device that misses the request learns the same from
// the Revoked banner on its next connection.
func disconnectRevoked(conn ssh.Conn) {
	_, _, _ = conn.SendRequest(wire.RequestRevoked, false, nil)
}

// isRevoked tells whether CloseDeviceSessions closed the Device Session id.
func (s *Server) isRevoked(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return id != "" && s.revoked[id]
}

// Close stops listening and accepting channels, waits up to shutdownGrace for
// calls in flight to finish, then ends every connection and waits for its
// goroutines.
func (s *Server) Close() error {
	s.mu.Lock()
	s.closed = true
	listeners := s.listeners
	conns := slices.Collect(maps.Keys(s.conns))
	s.mu.Unlock()

	var errs []error
	for _, ln := range listeners {
		if err := ln.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			errs = append(errs, err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
	defer cancel()
	for _, cs := range []*channelServer{s.api, s.web} {
		if err := cs.http.Shutdown(ctx); err != nil {
			errs = append(errs, fmt.Errorf("%s requests still running after %v were cut: %w", cs.kind, shutdownGrace, err), cs.http.Close())
		}
	}
	for _, c := range conns {
		_ = c.ssh.Close()
	}
	s.wg.Wait()
	return errors.Join(errs...)
}

func (s *Server) track(ln net.Listener) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}
	s.listeners = append(s.listeners, ln)
	return true
}

func (s *Server) trackConn(c *Connection) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.revoked[c.caller.DeviceSession] {
		return false
	}
	s.conns[c] = struct{}{}
	return true
}

func (s *Server) untrackConn(c *Connection) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.conns, c)
}

// channelServer serves one channel type's HTTP: each channel accepted is one
// HTTP connection.
type channelServer struct {
	kind     string
	http     *http.Server
	channels *channelListener
}

func newChannelServer(h http.Handler, kind string, log *slog.Logger) *channelServer {
	return &channelServer{
		kind: kind,
		http: &http.Server{
			Handler:  h,
			ErrorLog: slog.NewLogLogger(log.Handler(), slog.LevelDebug),
			ConnContext: func(ctx context.Context, c net.Conn) context.Context {
				cc, ok := c.(*callerConn)
				if !ok {
					return ctx
				}
				ctx = context.WithValue(op.WithCaller(ctx, cc.caller), connectionKey{}, cc.conn)
				if cc.webPort != 0 {
					ctx = context.WithValue(ctx, webPortKey{}, cc.webPort)
				}
				return ctx
			},
			// A channel is active from a request's first byte until its
			// response is written, so its connection knows when its calls
			// have answered.
			ConnState: func(c net.Conn, state http.ConnState) {
				if cc, ok := c.(*callerConn); ok {
					cc.conn.callState(cc, state == http.StateActive)
				}
			},
		},
		channels: newChannelListener(kind),
	}
}

// callerConn is a channel as net/http sees it, carrying the caller and the
// SSH connection it came on, and for a web@koko channel koko web's port.
type callerConn struct {
	net.Conn
	caller  op.Caller
	conn    *Connection
	webPort int
	// active tells that a call is in flight on the channel; conn's mutex
	// guards it.
	active bool
}

// channelListener hands accepted channels of one type to its HTTP server.
type channelListener struct {
	kind  string
	conns chan net.Conn
	done  chan struct{}
	once  sync.Once
}

func newChannelListener(kind string) *channelListener {
	return &channelListener{kind: kind, conns: make(chan net.Conn), done: make(chan struct{})}
}

func (l *channelListener) push(c net.Conn) {
	select {
	case l.conns <- c:
	case <-l.done:
		_ = c.Close()
	}
}

func (l *channelListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.conns:
		return c, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}

func (l *channelListener) Close() error {
	l.once.Do(func() { close(l.done) })
	return nil
}

func (l *channelListener) Addr() net.Addr {
	return channelAddr(l.kind)
}

// channelAddr is a channel listener's address: its channel type.
type channelAddr string

func (channelAddr) Network() string  { return "ssh" }
func (a channelAddr) String() string { return string(a) }
