// Package server is the koko server process: what `koko server run`
// dispatches into. It opens the store, loads the Root Key, registers every
// area's operations and serves them on the Unix socket.
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
	"github.com/cieons/koko/internal/server/seal"
	"github.com/cieons/koko/internal/server/space"
	"github.com/cieons/koko/internal/server/store"
	"github.com/cieons/koko/internal/server/transport"
	"github.com/cieons/koko/internal/server/webui"
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
	// RootKeyPath is the Root Key's file, outside DataDir. The koko server
	// reads it at every start and never writes it.
	RootKeyPath string
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

// Start opens the data directory, loads the Root Key and listens on the Unix
// socket; the koko server then serves until Close. It refuses to start, and
// writes no key, when the Root Key file's path is not absolute, the file is
// inside the data directory, missing or does not parse, naming the file. The
// store opens before the key loads, as P0 spec: install and run orders it, so
// that a refusal can name the Root Key the sealed data needs (#34).
func Start(ctx context.Context, cfg Config) (*Server, error) {
	log := slog.New(slog.NewTextHandler(cfg.Log, &slog.HandlerOptions{Level: cfg.LogLevel}))
	// Before anything names the path: a relative one may be the key itself.
	if err := seal.CheckPath(cfg.RootKeyPath); err != nil {
		return nil, err
	}
	if err := checkRootKeyOutside(cfg.RootKeyPath, cfg.DataDir); err != nil {
		return nil, err
	}
	st, err := store.Open(ctx, cfg.DataDir)
	if err != nil {
		return nil, err
	}
	key, err := seal.Load(cfg.RootKeyPath)
	if err != nil {
		return nil, errors.Join(err, st.Close())
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
	log.Info("koko server started", "version", build.Read().Version, "data_dir", cfg.DataDir,
		"root_key", key.ID(), "socket", socket, "web_ui_built", webui.Built())
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
