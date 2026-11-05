// Package transport is the koko server's SSH server. Every way in is an SSH
// connection carrying the same channels (ADR 0015): an api@koko channel is
// handed to the API's HTTP handler and a web@koko channel to the web front's,
// each request with the connection's caller and the connection itself, and
// any other channel type is refused.
//
// The Unix socket speaks SSH with "none" authentication and a host key made in
// memory at each start; every connection on it is the Local caller.
//
// The TCP listener presents the koko server's sealed host key and
// authenticates each Device Connection by its device's key, its Device
// Session. A key no Device Session holds may join, in the same SSH
// authentication: once the device has proved it holds the key, the koko
// server answers with a partial success and takes a Join Code's secret as an
// SSH password (P0 spec: people and Spaces, "Joining"). Devices answers what
// a key is and whether a secret joins it.
package transport

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/cieons/koko/internal/server/op"
	"github.com/cieons/koko/internal/shared/wire"
)

// Devices answers, for the TCP listener, what a device's key is and whether
// a Join Code's secret makes it a Device Session. The device area answers;
// the transport only asks.
type Devices interface {
	// DeviceSession returns the id of the Device Session whose key is key,
	// or "" when no Device Session holds it.
	DeviceSession(ctx context.Context, key ssh.PublicKey) (string, error)
	// Join makes key, which the device has proved it holds, a Device Session
	// through the valid Join Code whose secret the password carries
	// (wire.JoinSecret), and returns its id. It fails for any password that
	// is not such a secret, and logs why.
	Join(ctx context.Context, key ssh.PublicKey, password []byte, from net.Addr) (string, error)
}

// joinRefused is the banner a failed join gets, whatever the reason: the
// code was used, expired, voided or never issued, or the secret is not one,
// so trying codes teaches nothing.
const joinRefused = "koko: the Join Code is invalid or expired.\n"

// deviceSessionExt carries, in a Device Connection's ssh.Permissions, the id
// of the Device Session it authenticated as.
const deviceSessionExt = "koko-device-session"

// Server accepts SSH connections and serves their channels.
type Server struct {
	log *slog.Logger
	api *channelServer
	web *channelServer

	mu        sync.Mutex
	closed    bool
	listeners []net.Listener
	conns     map[ssh.Conn]struct{}
	wg        sync.WaitGroup
}

// New returns a Server that hands every api@koko channel to api and every
// web@koko channel to web.
func New(api, web http.Handler, log *slog.Logger) *Server {
	s := &Server{
		log:   log,
		api:   newChannelServer(api, wire.ChannelAPI, log),
		web:   newChannelServer(web, wire.ChannelWeb, log),
		conns: map[ssh.Conn]struct{}{},
	}
	for _, cs := range []*channelServer{s.api, s.web} {
		s.wg.Go(func() { _ = cs.http.Serve(cs.channels) })
	}
	return s
}

// Connection is one SSH connection to the koko server, as the requests on its
// channels see it.
type Connection struct {
	ctx context.Context
}

// Context returns a context that ends when the connection closes.
func (c *Connection) Context() context.Context {
	return c.ctx
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
	s.wg.Go(func() { s.accept(ln, cfg, func(*ssh.ServerConn) op.Caller { return op.Caller{Local: true} }) })
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
			id, err := devices.DeviceSession(ctx, key)
			switch {
			case err != nil:
				s.log.Error("look up a device's key", "addr", conn.RemoteAddr(), "err", err)
				return nil, err
			case id != "":
				return &ssh.Permissions{Extensions: map[string]string{deviceSessionExt: id}}, nil
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
		s.accept(ln, cfg, func(conn *ssh.ServerConn) op.Caller {
			return op.Caller{DeviceSession: conn.Permissions.Extensions[deviceSessionExt]}
		})
	})
	return nil
}

// accept serves each connection ln accepts, as the caller callerOf names
// once it has authenticated.
func (s *Server) accept(ln net.Listener, cfg *ssh.ServerConfig, callerOf func(*ssh.ServerConn) op.Caller) {
	for {
		nc, err := ln.Accept()
		if err != nil {
			if !errors.Is(err, net.ErrClosed) {
				s.log.Error("accept connection", "err", err)
			}
			return
		}
		s.wg.Go(func() { s.serve(nc, cfg, callerOf) })
	}
}

// serve runs one SSH connection until it ends.
func (s *Server) serve(nc net.Conn, cfg *ssh.ServerConfig, callerOf func(*ssh.ServerConn) op.Caller) {
	conn, chans, reqs, err := ssh.NewServerConn(nc, cfg)
	if err != nil {
		s.log.Debug("ssh handshake failed", "addr", nc.RemoteAddr(), "err", err)
		_ = nc.Close()
		return
	}
	caller := callerOf(conn)
	if !s.trackConn(conn) {
		_ = conn.Close()
		return
	}
	defer s.untrackConn(conn)
	ctx, ended := context.WithCancel(context.Background())
	defer ended()
	connection := &Connection{ctx: ctx}
	s.wg.Go(func() { ssh.DiscardRequests(reqs) })
	for nch := range chans {
		cc := &callerConn{caller: caller, conn: connection}
		var dest *channelServer
		switch nch.ChannelType() {
		case wire.ChannelAPI:
			dest = s.api
		case wire.ChannelWeb:
			port, err := wire.ParseWebPayload(nch.ExtraData())
			if err != nil {
				_ = nch.Reject(ssh.ConnectionFailed, err.Error())
				continue
			}
			dest, cc.webPort = s.web, port
		default:
			_ = nch.Reject(ssh.UnknownChannelType, fmt.Sprintf("koko serves no %q channel", nch.ChannelType()))
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

// shutdownGrace bounds how long Close waits for calls in flight.
const shutdownGrace = 5 * time.Second

// Close stops listening and accepting channels, waits up to shutdownGrace for
// calls in flight to finish, then ends every connection and waits for its
// goroutines.
func (s *Server) Close() error {
	s.mu.Lock()
	s.closed = true
	listeners := s.listeners
	conns := make([]ssh.Conn, 0, len(s.conns))
	for c := range s.conns {
		conns = append(conns, c)
	}
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
		_ = c.Close()
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

func (s *Server) trackConn(c ssh.Conn) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}
	s.conns[c] = struct{}{}
	return true
}

func (s *Server) untrackConn(c ssh.Conn) {
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
