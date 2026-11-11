package transport_test

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/cieons/koko/internal/client/serverconn"
	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/server/op"
	"github.com/cieons/koko/internal/server/transport"
	"github.com/cieons/koko/internal/shared/build"
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
		_, _ = io.WriteString(w, "device session "+c.DeviceSession)
	}
})

// testReport is the version and operations report of the transports these
// tests start.
var testReport = wire.Report{Version: "v0.3.0", Operations: []string{"space.add", "space.list"}}

// testReporter returns testReport, for transport.New.
func testReporter() wire.Report { return testReport }

// wantTestReport fails the test unless r is testReport.
func wantTestReport(t *testing.T, r wire.Report) {
	t.Helper()
	if r.Version != testReport.Version || !slices.Equal(r.Operations, testReport.Operations) {
		t.Errorf("report = %+v, want %+v", r, testReport)
	}
}

// startSocket serves h on a fresh socket, for api@koko and web@koko
// channels alike, and returns its path.
func startSocket(t *testing.T, h http.Handler) string {
	t.Helper()
	return startTransport(t, h, h)
}

// startTransport serves api on api@koko channels and web on web@koko
// channels of a fresh socket, and returns its path.
func startTransport(t *testing.T, api, web http.Handler) string {
	t.Helper()
	path := filepath.Join(harness.ShortTempDir(t), "koko.sock")
	s := transport.New(api, web, testReporter, slog.New(slog.DiscardHandler))
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

// The caller carries the version the CLI states in its SSH version string:
// this build's from koko's own client, and v0.3.0 from a client that states
// none, as every CLI before the window did.
func TestTheCallerCarriesTheCLIsVersion(t *testing.T) {
	path := startSocket(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, _ := op.CallerFrom(r.Context())
		_, _ = io.WriteString(w, c.CLIVersion)
	}))
	conn, err := serverconn.DialSocket(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	ch, err := conn.OpenChannel(t.Context(), wire.ChannelAPI, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ch.Close() }()
	if got, want := get(t, ch, bufio.NewReader(ch)), build.Read().Version; got != want {
		t.Errorf("koko's own client is taken as %q, want this build's %q", got, want)
	}

	c, _ := dial(t, path)
	plain, reqs, err := c.OpenChannel(wire.ChannelAPI, nil)
	if err != nil {
		t.Fatal(err)
	}
	go ssh.DiscardRequests(reqs)
	defer func() { _ = plain.Close() }()
	if got := get(t, plain, bufio.NewReader(plain)); got != "v0.3.0" {
		t.Errorf("a client sending %s is taken as %q, want v0.3.0", c.ClientVersion(), got)
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

// Once a connection has authenticated, and before any channel opens, the
// koko server sends its version and operations report unasked, as a global
// request that wants no reply: the report costs the CLI no round trip.
func TestEachConnectionIsSentTheReportUnasked(t *testing.T) {
	path := startSocket(t, whoAmI)
	var d net.Dialer
	nc, err := d.DialContext(t.Context(), "unix", path)
	if err != nil {
		t.Fatal(err)
	}
	conn, chans, reqs, err := ssh.NewClientConn(nc, "unix", &ssh.ClientConfig{User: "koko", HostKeyCallback: ssh.InsecureIgnoreHostKey()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	go func() {
		for nch := range chans {
			_ = nch.Reject(ssh.Prohibited, "none")
		}
	}()
	select {
	case r := <-reqs:
		wantReport(t, r.Type, r.WantReply, r.Payload)
	case <-time.After(5 * time.Second):
		t.Fatal("no report within 5 s of authenticating")
	}
}

// wantReport fails the test unless a global request is the report of the
// transports these tests start, wanting no reply.
func wantReport(t *testing.T, kind string, wantReply bool, payload []byte) {
	t.Helper()
	if kind != wire.RequestReport || wantReply {
		t.Fatalf("got the global request %q (want reply %t), want %q wanting no reply", kind, wantReply, wire.RequestReport)
	}
	r, err := wire.ParseReport(payload)
	if err != nil {
		t.Fatalf("the report %q does not parse: %v", payload, err)
	}
	wantTestReport(t, r)
}

// Any other channel type is refused as unknown; direct-tcpip gets a message
// of its own (TestTheKokoServerRefusesDirectTCPIP in the CLI's tests).
func TestOtherChannelTypesAreRefused(t *testing.T) {
	c, _ := dial(t, startSocket(t, whoAmI))
	for _, kind := range []string{"session", "direct-streamlocal@openssh.com", "web@koko-not"} {
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
	s := transport.New(whoAmI, whoAmI, testReporter, slog.New(slog.DiscardHandler))
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
	s := transport.New(slow, slow, testReporter, slog.New(slog.DiscardHandler))
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

// whereFrom answers every request with the channel's koko web port and the
// caller, and sends the connection it came on to conns.
func whereFrom(name string, conns chan<- *transport.Connection) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, _ := op.CallerFrom(r.Context())
		port, _ := transport.WebPortFrom(r.Context())
		conn, ok := transport.ConnectionFrom(r.Context())
		if ok {
			conns <- conn
		}
		_, _ = fmt.Fprintf(w, "%s port=%d local=%t", name, port, c.Local)
	})
}

// openWeb opens a web@koko channel naming port as koko web's.
func openWeb(t *testing.T, c *ssh.Client, port int) (ssh.Channel, *bufio.Reader) {
	t.Helper()
	ch, reqs, err := c.OpenChannel(wire.ChannelWeb, wire.WebPayload(port))
	if err != nil {
		t.Fatalf("open %s: %v", wire.ChannelWeb, err)
	}
	go ssh.DiscardRequests(reqs)
	t.Cleanup(func() { _ = ch.Close() })
	return ch, bufio.NewReader(ch)
}

// A web@koko channel reaches the web front, not the API, with the Local
// caller, the port koko web named, and the SSH connection it came on, which
// the api@koko channels of that connection share.
func TestWebChannelCarriesHTTPToTheWebFront(t *testing.T) {
	conns := make(chan *transport.Connection, 4)
	c, _ := dial(t, startTransport(t, whereFrom("api", conns), whereFrom("web", conns)))
	ch, br := openWeb(t, c, 5656)
	if got := get(t, ch, br); got != "web port=5656 local=true" {
		t.Errorf("web@koko answered %q, want the web front with port 5656 and the Local caller", got)
	}
	api, reqs, err := c.OpenChannel(wire.ChannelAPI, nil)
	if err != nil {
		t.Fatal(err)
	}
	go ssh.DiscardRequests(reqs)
	defer func() { _ = api.Close() }()
	if got := get(t, api, bufio.NewReader(api)); got != "api port=0 local=true" {
		t.Errorf("api@koko answered %q, want the API, with no koko web port", got)
	}
	if web, api := <-conns, <-conns; web != api {
		t.Error("the web@koko and api@koko channels of one connection gave two connections")
	}

	other, _ := dial(t, startTransport(t, whoAmI, whereFrom("web", conns)))
	ch, br = openWeb(t, other, 5656)
	get(t, ch, br)
	first := <-conns
	select {
	case <-first.Context().Done():
		t.Fatal("the connection's context ended while it was open")
	default:
	}
	_ = other.Close()
	select {
	case <-first.Context().Done():
	case <-time.After(5 * time.Second):
		t.Error("the connection's context did not end when the connection closed")
	}
}

// A web@koko channel that does not name koko web's port is refused when it
// opens, so the web front never answers without the port its Host header
// guard needs.
func TestWebChannelWithoutItsPortIsRefused(t *testing.T) {
	c, _ := dial(t, startSocket(t, whoAmI))
	for name, payload := range map[string][]byte{
		"none":       nil,
		"short":      {0, 0, 0x16},
		"long":       append(wire.WebPayload(5656), 0),
		"port 0":     wire.WebPayload(0),
		"port 65536": {0, 1, 0, 0},
	} {
		_, _, err := c.OpenChannel(wire.ChannelWeb, payload)
		oce, ok := errors.AsType[*ssh.OpenChannelError](err)
		if !ok || oce.Reason != ssh.ConnectionFailed || !strings.Contains(oce.Message, "port") {
			t.Errorf("%s: open %s = %v, want a refusal naming the port", name, wire.ChannelWeb, err)
		}
	}
}
