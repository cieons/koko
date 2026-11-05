package transport_test

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/server/op"
	"github.com/cieons/koko/internal/server/transport"
	"github.com/cieons/koko/internal/shared/wire"
)

// whoAmI answers every request with the caller the transport attached.
var whoAmI = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	switch c, ok := op.CallerFrom(r.Context()); {
	case !ok:
		_, _ = io.WriteString(w, "nobody")
	case c.Local:
		_, _ = io.WriteString(w, "local")
	default:
		_, _ = io.WriteString(w, "not local")
	}
})

// startSocket serves h on a fresh socket and returns its path.
func startSocket(t *testing.T, h http.Handler) string {
	t.Helper()
	path := filepath.Join(harness.ShortTempDir(t), "koko.sock")
	s := transport.New(h, slog.New(slog.DiscardHandler))
	if err := s.ListenSocket(t.Context(), path); err != nil {
		t.Fatalf("listen on socket: %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("close transport: %v", err)
		}
	})
	return path
}

// dial connects like a Local caller: no authentication method beyond "none".
func dial(t *testing.T, path string) (*ssh.Client, ssh.PublicKey) {
	t.Helper()
	var d net.Dialer
	nc, err := d.DialContext(t.Context(), "unix", path)
	if err != nil {
		t.Fatal(err)
	}
	var hostKey ssh.PublicKey
	conn, chans, reqs, err := ssh.NewClientConn(nc, "unix", &ssh.ClientConfig{
		User: "anyone",
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			hostKey = key
			return nil
		},
	})
	if err != nil {
		t.Fatalf("SSH handshake with none authentication: %v", err)
	}
	c := ssh.NewClient(conn, chans, reqs)
	t.Cleanup(func() { _ = c.Close() })
	return c, hostKey
}

// get sends one HTTP/1.1 request on an open channel and returns the body.
func get(t *testing.T, ch io.ReadWriter, br *bufio.Reader) string {
	t.Helper()
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, "http://koko/v1/x", strings.NewReader("{}"))
	if err := req.Write(ch); err != nil {
		t.Fatal(err)
	}
	resp, err := http.ReadResponse(br, req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestSocketSpeaksSSHAsKoko(t *testing.T) {
	c, _ := dial(t, startSocket(t, whoAmI))
	if v := string(c.ServerVersion()); v != "SSH-2.0-koko" {
		t.Errorf("server version = %q, want SSH-2.0-koko", v)
	}
}

// Every call on the socket comes from the Local caller, and one api@koko
// channel carries any number of calls in turn.
func TestAPIChannelCarriesHTTPAsTheLocalCaller(t *testing.T) {
	c, _ := dial(t, startSocket(t, whoAmI))
	ch, reqs, err := c.OpenChannel(wire.ChannelAPI, nil)
	if err != nil {
		t.Fatalf("open %s: %v", wire.ChannelAPI, err)
	}
	go ssh.DiscardRequests(reqs)
	defer func() { _ = ch.Close() }()
	br := bufio.NewReader(ch)
	for range 2 {
		if got := get(t, ch, br); got != "local" {
			t.Errorf("caller = %q, want local", got)
		}
	}
}

func TestOtherChannelTypesAreRefused(t *testing.T) {
	c, _ := dial(t, startSocket(t, whoAmI))
	for _, kind := range []string{"session", "direct-tcpip", "web@koko-not"} {
		_, _, err := c.OpenChannel(kind, nil)
		oce, ok := errors.AsType[*ssh.OpenChannelError](err)
		if !ok || oce.Reason != ssh.UnknownChannelType {
			t.Errorf("open %q: err = %v, want an unknown channel type refusal", kind, err)
		}
	}
}

// The socket's host key is made in memory at each start.
func TestSocketHostKeyIsNewAtEachStart(t *testing.T) {
	_, first := dial(t, startSocket(t, whoAmI))
	_, second := dial(t, startSocket(t, whoAmI))
	if bytes.Equal(first.Marshal(), second.Marshal()) {
		t.Error("two starts presented the same host key")
	}
}

// Every local user may connect: the socket is mode 0666 whatever the umask.
func TestSocketIsOpenToEveryLocalUser(t *testing.T) {
	path := startSocket(t, whoAmI)
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode()&os.ModeSocket == 0 || fi.Mode().Perm() != 0o666 {
		t.Errorf("socket mode = %v, want a socket with 0666", fi.Mode())
	}
}

// Closing stops listening and removes the socket file.
func TestCloseRemovesTheSocket(t *testing.T) {
	path := filepath.Join(harness.ShortTempDir(t), "koko.sock")
	s := transport.New(whoAmI, slog.New(slog.DiscardHandler))
	if err := s.ListenSocket(t.Context(), path); err != nil {
		t.Fatal(err)
	}
	c, _ := dial(t, path)
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("socket still exists after close: %v", err)
	}
	// Wait returns only once the transport has closed the open connection.
	_ = c.Wait()
}

// Close waits for a call in flight to finish before it returns, so the store
// behind the API is never closed under a running transaction.
func TestCloseWaitsForCallsInFlight(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var finished atomic.Bool
	slow := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		<-release
		finished.Store(true)
		_, _ = io.WriteString(w, "done")
	})
	path := filepath.Join(harness.ShortTempDir(t), "koko.sock")
	s := transport.New(slow, slog.New(slog.DiscardHandler))
	if err := s.ListenSocket(t.Context(), path); err != nil {
		t.Fatal(err)
	}
	c, _ := dial(t, path)
	ch, reqs, err := c.OpenChannel(wire.ChannelAPI, nil)
	if err != nil {
		t.Fatal(err)
	}
	go ssh.DiscardRequests(reqs)
	var wg sync.WaitGroup
	wg.Go(func() {
		req, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, "http://koko/v1/x", strings.NewReader("{}"))
		_ = req.Write(ch)
		_, _ = io.Copy(io.Discard, ch)
	})
	<-started
	wg.Go(func() {
		time.Sleep(100 * time.Millisecond)
		close(release)
	})
	if err := s.Close(); err != nil {
		t.Errorf("close: %v", err)
	}
	if !finished.Load() {
		t.Error("Close returned while a call was still running")
	}
	wg.Wait()
}
