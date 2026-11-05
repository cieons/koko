package server

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"net"
	"sync"

	"golang.org/x/crypto/ssh"

	"github.com/cieons/koko/internal/server/transport"
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
	if err != nil {
		return fmt.Errorf("listen on %s: %w", l.addr, err)
	}
	l.ln = ln
	return nil
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
