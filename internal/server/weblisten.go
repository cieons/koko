package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"syscall"
	"time"

	"github.com/cieons/koko/internal/shared/install"
	"github.com/cieons/koko/internal/shared/portowner"
)

// webShutdownGrace bounds how long Close waits for the web listener's calls
// in flight before it cuts them.
const webShutdownGrace = 5 * time.Second

// webReadHeaderTimeout bounds how long the web listener waits for a
// request's headers, so that a connection that sends none holds nothing
// for long.
const webReadHeaderTimeout = 10 * time.Second

// webListener is the web listener (ADR 0042): plain HTTP/1.1 on --web-listen,
// for the Owner's reverse proxy alone, which serves the web UI at the Web
// URL. It is bound before the store opens, and serves on a fresh koko
// server too.
type webListener struct {
	ln   net.Listener
	http *http.Server
	// served is closed once Serve has returned.
	served chan struct{}
}

// listenWeb binds addr, refusing an address something else holds, naming
// the process when portowner finds it. An empty addr is off: it returns
// nil.
func listenWeb(ctx context.Context, addr string) (*webListener, error) {
	if addr == "" {
		return nil, nil
	}
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", addr)
	if errors.Is(err, syscall.EADDRINUSE) {
		return nil, webAddressTaken(ctx, addr)
	}
	if err != nil {
		return nil, fmt.Errorf("listen for the web listener on %s: %w", addr, err)
	}
	return &webListener{ln: ln, served: make(chan struct{})}, nil
}

// webAddressTaken is the refusal of a web listener address another process
// holds, naming it when portowner can.
func webAddressTaken(ctx context.Context, addr string) error {
	holder := "another process"
	_, port, _ := net.SplitHostPort(addr)
	if n, err := strconv.Atoi(port); err == nil {
		if p, err := portowner.Lookup(ctx, n); err == nil {
			holder = p.String()
		}
	}
	return install.Refuse(install.PortTaken, "the web listener's address %s is taken by %s: stop it, or give the web listener another address with --web-listen (KOKO_WEB_LISTEN)", addr, holder)
}

// serve answers the listener's requests with h, until close.
func (l *webListener) serve(h http.Handler, log *slog.Logger) {
	l.http = &http.Server{
		Handler:           h,
		ReadHeaderTimeout: webReadHeaderTimeout,
		Protocols:         new(http.Protocols),
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelDebug),
	}
	l.http.Protocols.SetHTTP1(true)
	go func() {
		defer close(l.served)
		_ = l.http.Serve(l.ln)
	}()
}

// close stops listening, waits up to webShutdownGrace for the calls in
// flight, then cuts them. Before serve, it closes the bound address.
func (l *webListener) close() error {
	if l.http == nil {
		return l.ln.Close()
	}
	ctx, cancel := context.WithTimeout(context.Background(), webShutdownGrace)
	defer cancel()
	err := l.http.Shutdown(ctx)
	if err != nil {
		err = errors.Join(fmt.Errorf("web listener requests still running after %v were cut: %w", webShutdownGrace, err), l.http.Close())
	}
	<-l.served
	return err
}

// address returns the bound address, such as 127.0.0.1:7823.
func (l *webListener) address() string {
	return l.ln.Addr().String()
}

// exposed tells whether addr, a --web-listen address, listens beyond this
// machine: on every address, or on one that is not loopback. Outside a
// container, the koko server warns of it, since the listener serves no
// TLS and is meant for a reverse proxy on the same machine.
func exposed(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil || host == "localhost" {
		return false
	}
	ip, err := netip.ParseAddr(host)
	return err != nil || !ip.IsLoopback()
}
