package server

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"net"
	"strconv"
	"sync"
	"syscall"

	"golang.org/x/crypto/ssh"

	"github.com/cieons/koko/internal/server/transport"
	"github.com/cieons/koko/internal/shared/install"
	"github.com/cieons/koko/internal/shared/portowner"
	"github.com/cieons/koko/internal/shared/sockerr"
)

// tcpListener is the koko server's TCP listener, where devices reach it over
// Device Connections. It listens only once the koko server is initialised:
// from initialisation on, at once, and at every later start (P0 spec:
// install and run). Its port is the one a Server Address without a port
// names.
type tcpListener struct {
	// addr is the listen address, --listen.
	addr      string
	transport *transport.Server
	devices   transport.Devices

	mu sync.Mutex
	// ln is the bound listener, nil until bind; serving tells whether the
	// transport serves it yet.
	ln      net.Listener
	serving bool
}

// bind binds the listen address, unless it is bound already, so a Join
// Code can name its port (port) before the listener serves.
func (l *tcpListener) bind(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.ln != nil {
		return nil
	}
	if l.addr == "" {
		return errors.New("listen on TCP: the koko server was given no listen address")
	}
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", l.addr)
	if sockerr.Is(err, syscall.EADDRINUSE) {
		return listenAddressTaken(ctx, l.addr)
	}
	if err != nil {
		return fmt.Errorf("listen on %s: %w", l.addr, err)
	}
	l.ln = ln
	return nil
}

// listenAddressTaken is the refusal of a listen address another process
// holds. It does not say that nothing was written: at a first start, the
// Root Key file it made stays.
func listenAddressTaken(ctx context.Context, addr string) error {
	_, port, _ := net.SplitHostPort(addr)
	return &install.Refusal{Reason: install.PortTaken, Message: fmt.Sprintf(
		"port %s, where the koko server listens for devices (--listen %s), is taken by %s: stop it, or give the koko server another port with --listen (KOKO_LISTEN)",
		port, addr, portHolder(ctx, port))}
}

// portHolder names the process that holds the TCP port, for a refusal: as
// portowner tells it, and with the commands that name it when portowner
// cannot tell its pid, such as for another user's process.
func portHolder(ctx context.Context, port string) string {
	holder := "another process"
	if n, err := strconv.Atoi(port); err == nil {
		if p, err := portowner.Lookup(ctx, n); err == nil {
			if p.PID != 0 {
				return p.String()
			}
			holder = p.String()
		}
	}
	return holder + " (sudo ss -ltnp or sudo lsof -i :" + port + " names it)"
}

// serve serves Device Connections on the bound listener, presenting
// hostKey, the koko server's sealed host key.
func (l *tcpListener) serve(hostKey ed25519.PrivateKey) error {
	signer, err := ssh.NewSignerFromKey(hostKey)
	if err != nil {
		return fmt.Errorf("use the host key: %w", err)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	switch {
	case l.ln == nil:
		return errors.New("serve Device Connections: the TCP listener is not bound")
	case l.serving:
		return nil
	}
	l.serving = true
	return l.transport.ServeTCP(l.ln, signer, l.devices)
}

// unbind closes the listener when it is bound but not served yet, as when
// the initialisation that bound it is rolled back.
func (l *tcpListener) unbind() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.ln != nil && !l.serving {
		_ = l.ln.Close()
		l.ln = nil
	}
}

// port returns the bound port, or 0 before bind.
func (l *tcpListener) port() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.ln == nil {
		return 0
	}
	return l.ln.Addr().(*net.TCPAddr).Port
}

// address returns the bound address, or "" before bind.
func (l *tcpListener) address() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.ln == nil {
		return ""
	}
	return l.ln.Addr().String()
}
