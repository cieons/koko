// Package server is the koko server process: what `koko server run`
// dispatches into. It opens the store, loads the Root Key and opens the
// koko server's own keys with it, registers every area's operations and
// serves them, and the web front, on the Unix socket and, once the koko
// server is initialised, to devices on TCP. It also serves the server area's
// operations, such as initialisation, which act on the process itself.
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
	"time"

	"github.com/cieons/koko/internal/server/deployment"
	"github.com/cieons/koko/internal/server/device"
	"github.com/cieons/koko/internal/server/op"
	"github.com/cieons/koko/internal/server/seal"
	"github.com/cieons/koko/internal/server/space"
	"github.com/cieons/koko/internal/server/store"
	"github.com/cieons/koko/internal/server/transport"
	"github.com/cieons/koko/internal/server/user"
	"github.com/cieons/koko/internal/server/web"
	"github.com/cieons/koko/internal/server/webui"
	"github.com/cieons/koko/internal/shared/build"
	"github.com/cieons/koko/internal/shared/wire"
)

// areas registers each area's operations; an area's first ticket adds its
// line.
var areas = []func(*op.API){}

// holdings counts what each area records in a Space, for space show's
// counts and space rm's refusal of a Space that is not empty; an area whose
// records live in a Space, such as Hosts or Secrets, adds its line.
var holdings = []space.Holding{}

// Config is what the koko server starts with.
type Config struct {
	// DataDir holds koko.db.
	DataDir string
	// RootKeyPath is the Root Key's file, outside DataDir. The koko server
	// reads it at every start and never writes it.
	RootKeyPath string
	// RootKeySource is the file RootKeyPath is a copy of, when it is one:
	// under systemd, RootKeyPath is the credential's copy, and the source
	// is the file the koko unit's LoadCredential= names. It is the Root
	// Key's file as the Owner keeps it, which server.show names; empty
	// means RootKeyPath itself.
	RootKeySource string
	// Listen is the TCP listen address, such as :443, where devices reach
	// the koko server once it is initialised. A Server Address without a
	// port names its port.
	Listen string
	// LogLevel is the lowest level written to Log.
	LogLevel slog.Level
	// Log receives the log, one structured line per event: stderr, which
	// journald reads under systemd.
	Log io.Writer

	// SocketPath is the socket-path hook, for tests only: empty means the
	// fixed wire.SocketPath. No flag or environment variable sets it.
	SocketPath string
	// Clock is the clock hook, for tests only: nil means time.Now. Every
	// time the koko server records or compares, such as a Join Code's
	// expiry, comes from it. No flag or environment variable sets it.
	Clock func() time.Time
}

// Server is a running koko server.
type Server struct {
	store     *store.Store
	transport *transport.Server
	tcp       *tcpListener
	// api serves every operation the areas registered.
	api *op.API
}

// Start opens the data directory, loads the Root Key, opens the koko
// server's own keys with it once the koko server is initialised, and listens
// on the Unix socket and, once initialised, on TCP; the koko server then
// serves until Close. It refuses to start, and writes no key, when the Root
// Key file's path is not absolute, or the file is inside the data directory,
// missing, does not parse, or is not the Root Key the sealed data needs,
// naming the file. The store opens before the key loads, as P0 spec: install
// and run orders it, so that a refusal can name the Root Key the sealed data
// needs. An initialised koko server that cannot listen on TCP refuses to
// start.
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
	key, keys, err := loadKeys(ctx, st, cfg.RootKeyPath)
	if err != nil {
		return nil, errors.Join(err, st.Close())
	}
	clock := cfg.Clock
	if clock == nil {
		clock = time.Now
	}
	a := op.New(st, log, deployment.Initialised, clock)
	// The web area's front holds the runs of koko web, so it registers its
	// operation itself, below.
	front := web.New(a, st, key, clock, log)
	t := transport.New(a, front, log)
	tcp := &tcpListener{addr: cfg.Listen, transport: t, devices: device.NewAuthenticator(st, clock, log)}
	srv := &Server{store: st, transport: t, tcp: tcp, api: a}
	for _, register := range areas {
		register(a)
	}
	space.Register(a, user.ByName, holdings)
	// The areas that issue Join Codes need what the process loaded to name
	// the koko server in them.
	issuer := device.NewIssuer(joinTarget(key, tcp.port))
	device.Register(a, issuer)
	user.Register(a, issuer.Issue)
	registerServer(a, key, issuer, tcp, log)
	registerSettings(a, &settings{
		rootKey:     key,
		rootKeyFile: cmp.Or(cfg.RootKeySource, cfg.RootKeyPath),
		dataDir:     cfg.DataDir,
		version:     build.Read().Version,
		listenPort:  tcp.port,
	})
	front.Register(a)
	if keys != nil {
		if err := tcp.bind(ctx); err != nil {
			return nil, errors.Join(err, srv.Close())
		}
		if err := tcp.serve(keys.Host); err != nil {
			return nil, errors.Join(err, srv.Close())
		}
	}
	socket := cmp.Or(cfg.SocketPath, wire.SocketPath)
	if err := t.ListenSocket(ctx, socket); err != nil {
		return nil, errors.Join(err, srv.Close())
	}
	attrs := []any{
		"version", build.Read().Version, "data_dir", cfg.DataDir, "root_key", key.ID(),
		"socket", socket, "web_ui_built", webui.Built(), "initialised", keys != nil,
	}
	if keys != nil {
		attrs = append(attrs, "host_key", keys.HostKeyFingerprint(), "listen", tcp.address())
	}
	log.Info("koko server started", attrs...)
	return srv, nil
}

// Close stops serving, waits for calls in flight to end, and closes the store.
func (s *Server) Close() error {
	err := s.transport.Close()
	s.tcp.unbind()
	return errors.Join(err, s.store.Close())
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
