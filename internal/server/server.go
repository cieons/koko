// Package server is the koko server process: what `koko server run`
// dispatches into. It opens the store, registers every area's operations and
// serves them on the Unix socket.
//
// The packages under internal/server are the server-only group: no client
// package imports them (docs/subsystems/code-layout.md).
package server

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"

	"github.com/cieons/koko/internal/server/op"
	"github.com/cieons/koko/internal/server/space"
	"github.com/cieons/koko/internal/server/store"
	"github.com/cieons/koko/internal/server/transport"
	"github.com/cieons/koko/internal/shared/build"
	"github.com/cieons/koko/internal/shared/wire"
)

// areas registers each area's operations; an area's first ticket adds its
// line.
var areas = []func(*op.API){
	space.Register,
}

// Config is what the koko server starts with.
type Config struct {
	// DataDir holds koko.db.
	DataDir string
	// LogLevel is the lowest level written to Log.
	LogLevel slog.Level
	// Log receives the log, one structured line per event: stderr, which
	// journald reads under systemd.
	Log io.Writer

	// SocketPath is the socket-path hook, for tests only: empty means the
	// fixed wire.SocketPath. No flag or environment variable sets it.
	SocketPath string
}

// Server is a running koko server.
type Server struct {
	store     *store.Store
	transport *transport.Server
}

// Start opens the data directory and listens on the Unix socket; the koko
// server then serves until Close.
func Start(ctx context.Context, cfg Config) (*Server, error) {
	log := slog.New(slog.NewTextHandler(cfg.Log, &slog.HandlerOptions{Level: cfg.LogLevel}))
	st, err := store.Open(ctx, cfg.DataDir)
	if err != nil {
		return nil, err
	}
	a := op.New(st, log)
	for _, register := range areas {
		register(a)
	}
	t := transport.New(a, log)
	socket := cmp.Or(cfg.SocketPath, wire.SocketPath)
	if err := t.ListenSocket(ctx, socket); err != nil {
		return nil, errors.Join(err, t.Close(), st.Close())
	}
	log.Info("koko server started", "version", build.Read().Version, "data_dir", cfg.DataDir, "socket", socket)
	return &Server{store: st, transport: t}, nil
}

// Close stops serving, waits for calls in flight to end, and closes the store.
func (s *Server) Close() error {
	return errors.Join(s.transport.Close(), s.store.Close())
}

// Run runs the koko server until ctx ends.
func Run(ctx context.Context, cfg Config) error {
	s, err := Start(ctx, cfg)
	if err != nil {
		return fmt.Errorf("start the koko server: %w", err)
	}
	<-ctx.Done()
	return s.Close()
}
