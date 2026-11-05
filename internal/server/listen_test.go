package server_test

import (
	"errors"
	"net"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/shared/api"
)

// tcpHostKey connects to the koko server's TCP listener at addr and returns
// the fingerprint of the host key it presents, without authenticating.
func tcpHostKey(t *testing.T, addr string) (string, error) {
	t.Helper()
	var d net.Dialer
	nc, err := d.DialContext(t.Context(), "tcp", addr)
	if err != nil {
		return "", err
	}
	defer func() { _ = nc.Close() }()
	// A port bound but not served accepts the connection and never answers.
	if err := nc.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	var presented string
	_, _, _, err = ssh.NewClientConn(nc, addr, &ssh.ClientConfig{
		User: "koko",
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			presented = ssh.FingerprintSHA256(key)
			return nil
		},
	})
	if presented == "" {
		t.Fatalf("no SSH host key from %s: %v", addr, err)
	}
	return presented, nil
}

// Before initialisation nothing listens on TCP. Initialisation starts the
// listener at once, presenting the host key it made, and every later start
// listens at once with the same sealed key.
func TestTCPListensFromInitialisationOn(t *testing.T) {
	addr := harness.FreeAddr(t)
	dataDir, rootKey := t.TempDir(), harness.NewRootKeyFile(t)
	h := harness.Start(t, harness.NoInit(), harness.DataDir(dataDir), harness.RootKeyFile(rootKey), harness.Listen(addr))
	if _, err := tcpHostKey(t, addr); !errors.Is(err, syscall.ECONNREFUSED) {
		t.Fatalf("before initialisation, connecting to %s = %v, want connection refused", addr, err)
	}

	init, err := h.Call(api.ServerInit, api.ServerInitRequest{Owner: harness.Owner, Address: harness.Address})
	if err != nil {
		t.Fatalf("server.init: %v", err)
	}
	if got, err := tcpHostKey(t, addr); err != nil || got != init.HostKeyFingerprint {
		t.Fatalf("after initialisation, %s presents %s (%v), want the host key %s", addr, got, err, init.HostKeyFingerprint)
	}

	h.Stop()
	harness.Start(t, harness.NoInit(), harness.DataDir(dataDir), harness.RootKeyFile(rootKey), harness.Listen(addr))
	if got, err := tcpHostKey(t, addr); err != nil || got != init.HostKeyFingerprint {
		t.Errorf("after a restart, %s presents %s (%v), want the same host key %s", addr, got, err, init.HostKeyFingerprint)
	}
}

// An initialisation refused after the listener started, here at its last
// step, issuing the first Join Code, leaves nothing listening on TCP, so it
// can be tried again on the same port.
func TestARefusedInitialisationLeavesNothingListening(t *testing.T) {
	addr := harness.FreeAddr(t)
	h := harness.Start(t, harness.NoInit(), harness.Listen(addr))
	long := strings.Repeat(strings.Repeat("a", 62)+".", 3) + strings.Repeat("a", 61) // 250 characters
	_, err := h.Call(api.ServerInit, api.ServerInitRequest{Owner: harness.Owner, Address: long + ":18443"})
	wantCode(t, err, api.CodeInvalidAddress)
	if _, err := tcpHostKey(t, addr); !errors.Is(err, syscall.ECONNREFUSED) {
		t.Fatalf("after a refused initialisation, connecting to %s = %v, want connection refused", addr, err)
	}
	if _, err := h.Call(api.ServerInit, api.ServerInitRequest{Owner: harness.Owner, Address: harness.Address}); err != nil {
		t.Fatalf("server.init after a refused one: %v", err)
	}
	if _, err := tcpHostKey(t, addr); err != nil {
		t.Errorf("after initialisation, connecting to %s: %v", addr, err)
	}
}
