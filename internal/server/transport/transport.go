// Package transport is the koko server's SSH server. Every way in is an SSH
// connection carrying the same channels (ADR 0015): an api@koko channel is
// handed to the API's HTTP handler together with the connection's caller, and
// any other channel type is refused.
//
// The Unix socket speaks SSH with "none" authentication and a host key made in
// memory at each start; every connection on it is the Local caller.
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

// Server accepts SSH connections and serves their channels.
type Server struct {
	log  *slog.Logger
	http *http.Server
	api  *channelListener

	mu        sync.Mutex
	closed    bool
	listeners []net.Listener
	conns     map[ssh.Conn]struct{}
	wg        sync.WaitGroup
}

// New returns a Server that hands every api@koko channel to api.
func New(api http.Handler, log *slog.Logger) *Server {
	s := &Server{
		log:   log,
		api:   newChannelListener(),
		conns: map[ssh.Conn]struct{}{},
	}
	s.http = &http.Server{
		Handler:  api,
		ErrorLog: slog.NewLogLogger(log.Handler(), slog.LevelDebug),
		ConnContext: func(ctx context.Context, c net.Conn) context.Context {
			if cc, ok := c.(*callerConn); ok {
				return op.WithCaller(ctx, cc.caller)
			}
			return ctx
		},
	}
	s.wg.Go(func() { _ = s.http.Serve(s.api) })
	return s
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
	s.wg.Go(func() { s.accept(ln, cfg, op.Caller{Local: true}) })
	return nil
}

func (s *Server) accept(ln net.Listener, cfg *ssh.ServerConfig, caller op.Caller) {
	for {
		nc, err := ln.Accept()
		if err != nil {
			if !errors.Is(err, net.ErrClosed) {
				s.log.Error("accept connection", "err", err)
			}
			return
		}
		s.wg.Go(func() { s.serve(nc, cfg, caller) })
	}
}

// serve runs one SSH connection until it ends.
func (s *Server) serve(nc net.Conn, cfg *ssh.ServerConfig, caller op.Caller) {
	conn, chans, reqs, err := ssh.NewServerConn(nc, cfg)
	if err != nil {
		s.log.Debug("ssh handshake failed", "err", err)
		_ = nc.Close()
		return
	}
	if !s.trackConn(conn) {
		_ = conn.Close()
		return
	}
	defer s.untrackConn(conn)
	s.wg.Go(func() { ssh.DiscardRequests(reqs) })
	for nch := range chans {
		if nch.ChannelType() != wire.ChannelAPI {
			_ = nch.Reject(ssh.UnknownChannelType, fmt.Sprintf("koko serves no %q channel", nch.ChannelType()))
			continue
		}
		ch, chReqs, err := nch.Accept()
		if err != nil {
			s.log.Debug("accept channel", "err", err)
			continue
		}
		s.wg.Go(func() { ssh.DiscardRequests(chReqs) })
		s.api.push(&callerConn{Conn: wire.Conn(ch), caller: caller})
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
	if err := s.http.Shutdown(ctx); err != nil {
		errs = append(errs, fmt.Errorf("calls still running after %v were cut: %w", shutdownGrace, err), s.http.Close())
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

// callerConn is an api@koko channel as net/http sees it, carrying the caller
// of the connection it came on.
type callerConn struct {
	net.Conn
	caller op.Caller
}

// channelListener hands accepted api@koko channels to the HTTP server.
type channelListener struct {
	conns chan net.Conn
	done  chan struct{}
	once  sync.Once
}

func newChannelListener() *channelListener {
	return &channelListener{conns: make(chan net.Conn), done: make(chan struct{})}
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
	return channelAddr{}
}

type channelAddr struct{}

func (channelAddr) Network() string { return "ssh" }
func (channelAddr) String() string  { return wire.ChannelAPI }
