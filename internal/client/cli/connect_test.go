package cli_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/cieons/koko/internal/client/cli"
	"github.com/cieons/koko/internal/harness"
)

// With no socket at its path, and no koko server joined, a command that calls
// the koko server fails with not_joined.
func TestNoSocketIsNotJoined(t *testing.T) {
	path := filepath.Join(harness.ShortTempDir(t), "koko.sock")
	r := harness.Run(t, cli.Runner{SocketPath: path}, nil, "space", "ls", "--json")
	msg := r.WantError(t, 1, "not_joined")
	if !strings.Contains(msg, path) {
		t.Errorf("message %q does not name the socket", msg)
	}
}

// A socket path that answers no SSH fails with server_unreachable.
func TestDeadSocketIsServerUnreachable(t *testing.T) {
	path := filepath.Join(harness.ShortTempDir(t), "koko.sock")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	r := harness.Run(t, cli.Runner{SocketPath: path}, nil, "space", "ls", "--json")
	msg := r.WantError(t, 1, "server_unreachable")
	if !strings.Contains(msg, path) {
		t.Errorf("message %q does not name the socket", msg)
	}
}

// A socket that answers SSH but cannot carry the call (here, it refuses every
// channel) fails with server_unreachable, not internal_error.
func TestCallThatCannotBeCarriedIsServerUnreachable(t *testing.T) {
	path := filepath.Join(harness.ShortTempDir(t), "koko.sock")
	refuseChannels(t, path)
	r := harness.Run(t, cli.Runner{SocketPath: path}, nil, "space", "ls", "--json")
	r.WantError(t, 1, "server_unreachable")
}

// refuseChannels serves SSH with none authentication on path and refuses
// every channel, until the test ends.
func refuseChannels(t *testing.T, path string) {
	t.Helper()
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &ssh.ServerConfig{NoClientAuth: true}
	cfg.AddHostKey(signer)
	var lc net.ListenConfig
	ln, err := lc.Listen(t.Context(), "unix", path)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	t.Cleanup(func() {
		_ = ln.Close()
		<-done
	})
	go func() {
		defer close(done)
		for {
			nc, err := ln.Accept()
			if err != nil {
				return
			}
			conn, chans, reqs, err := ssh.NewServerConn(nc, cfg)
			if err != nil {
				_ = nc.Close()
				continue
			}
			go ssh.DiscardRequests(reqs)
			for nch := range chans {
				_ = nch.Reject(ssh.Prohibited, "no channels here")
			}
			_ = conn.Close()
		}
	}()
}
