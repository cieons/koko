package transport_test

import (
	"bufio"
	"bytes"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/cieons/koko/internal/harness/sshhost"
	"github.com/cieons/koko/internal/server/transport"
	"github.com/cieons/koko/internal/shared/wire"
)

// slowLinkAnswer is the answer to these tests' calls: more than a loopback
// connection's receive buffer takes, so that a device reading nothing
// leaves the rest in the koko server's send buffer.
var slowLinkAnswer = bytes.Repeat([]byte("answer, "), 256<<10/8)

// The answer to a call that revokes the caller's own Device Session, and the
// revocation disconnect after it, reach a device on a slow link in full,
// though the device sends while its connection closes, as it sends window
// adjusts for what it reads: closing the connection at once would reset it
// and drop what the device had not read yet.
func TestADeviceOnASlowLinkGetsTheAnswerRevokingItsDeviceSessionAndTheRevocation(t *testing.T) {
	var s atomic.Pointer[transport.Server]
	revokeItself := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		s.Load().CloseDeviceSessions(slowLinkDeviceSession)
		_, _ = w.Write(slowLinkAnswer)
	})
	var log syncBuffer
	srv, addr, key := serveSlowLink(t, revokeItself, &log)
	s.Store(srv)

	revoked := callOnASlowLink(t, addr, key, func() {
		waitLog(t, &log, `msg="device connection closed"`)
	})
	if !revoked {
		t.Error("the device was not sent the revocation disconnect")
	}
}

// A stop sends a device on a slow link the answer to its last call in full,
// though the device sends while its connection closes, and though the stop
// closes the call's channel as it closes the connection.
func TestAStopSendsTheLastAnswerInFullToADeviceOnASlowLink(t *testing.T) {
	answered := make(chan struct{})
	answers := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(slowLinkAnswer)
		close(answered)
	})
	s, addr, key := serveSlowLink(t, answers, &syncBuffer{})

	closed := make(chan error, 1)
	var closing sync.WaitGroup
	t.Cleanup(closing.Wait)
	callOnASlowLink(t, addr, key, func() {
		<-answered
		closing.Go(func() { closed <- s.Close() })
	})
	select {
	case err := <-closed:
		if err != nil {
			t.Errorf("close the transport: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the transport did not close within 10 s")
	}
}

// slowLinkDeviceSession is the Device Session of serveSlowLink's device.
const slowLinkDeviceSession = "device_3f9k2m7q"

// serveSlowLink serves h to Device Connections on a loopback port, logging
// to log, and returns the transport, its address and the key of a device
// whose Device Session is slowLinkDeviceSession. Each connection's send
// buffer holds the part of an answer that a device reading nothing has no
// room for, as a slow link's does, rather than blocking the koko server's
// write.
func serveSlowLink(t *testing.T, h http.Handler, log io.Writer) (*transport.Server, string, ssh.Signer) {
	t.Helper()
	key := sshhost.NewKey(t, ssh.KeyAlgoED25519)
	devices := &fakeDevices{sessions: map[string]string{string(key.PublicKey().Marshal()): slowLinkDeviceSession}}
	s := transport.New(h, h, testReporter, slog.New(slog.NewTextHandler(log, nil)))
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("close transport: %v", err)
		}
	})
	var lc net.ListenConfig
	ln, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ServeTCP(sendBufferListener{ln}, sshhost.NewKey(t, ssh.KeyAlgoED25519), devices); err != nil {
		t.Fatal(err)
	}
	return s, ln.Addr().String(), key
}

// sendBufferListener gives each TCP connection it accepts a send buffer
// larger than slowLinkAnswer.
type sendBufferListener struct {
	net.Listener
}

func (l sendBufferListener) Accept() (net.Conn, error) {
	nc, err := l.Listener.Accept()
	if tc, ok := nc.(*net.TCPConn); ok {
		_ = tc.SetWriteBuffer(512 << 10)
	}
	return nc, err
}

// callOnASlowLink makes one call on a new api@koko channel of a Device
// Connection to addr with key, over a link that holds back what the koko
// server sends, from before the call until 100 ms after closing returns,
// which it does once the koko server begins to close the connection. The
// device sends meanwhile, as it would send window adjusts. It fails the
// test unless the answer is slowLinkAnswer, and returns whether the
// revocation disconnect came.
func callOnASlowLink(t *testing.T, addr string, key ssh.Signer, closing func()) (revoked bool) {
	t.Helper()
	var d net.Dialer
	nc, err := d.DialContext(t.Context(), "tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	link := &heldConn{Conn: nc}
	conn, chans, reqs, err := ssh.NewClientConn(link, addr, &ssh.ClientConfig{
		User:            "koko",
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(key)},
		HostKeyCallback: ssh.FixedHostKey(hostKeysAt(t, addr)[0]),
	})
	if err != nil {
		t.Fatalf("dial the Device Connection: %v", err)
	}
	var wg sync.WaitGroup
	t.Cleanup(func() {
		_ = conn.Close()
		wg.Wait()
	})
	// Before the close, so that a test failing while reads are held does
	// not leave the connection's reader waiting.
	t.Cleanup(link.resume)
	wg.Go(func() {
		for nch := range chans {
			_ = nch.Reject(ssh.Prohibited, "a device opens no channel to the koko server")
		}
	})
	// requests ends once the connection has, with whether the revocation
	// disconnect came.
	requests := make(chan bool, 1)
	wg.Go(func() {
		revoked := false
		for r := range reqs {
			revoked = revoked || r.Type == wire.RequestRevoked
		}
		requests <- revoked
	})
	ch, chReqs, err := conn.OpenChannel(wire.ChannelAPI, nil)
	if err != nil {
		t.Fatalf("open %s: %v", wire.ChannelAPI, err)
	}
	wg.Go(func() { ssh.DiscardRequests(chReqs) })

	link.hold()
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, "http://koko/v1/x", strings.NewReader("{}"))
	if err := req.Write(ch); err != nil {
		t.Fatal(err)
	}
	closing()
	for until := time.Now().Add(100 * time.Millisecond); time.Now().Before(until); time.Sleep(2 * time.Millisecond) {
		if _, _, err := conn.SendRequest("ping@test", false, nil); err != nil {
			break // reset
		}
	}
	link.resume()

	var body []byte
	if resp, err := http.ReadResponse(bufio.NewReader(ch), req); err == nil {
		body, _ = io.ReadAll(resp.Body)
		_ = resp.Body.Close()
	}
	if !bytes.Equal(body, slowLinkAnswer) {
		t.Errorf("the device got %d bytes of the koko server's %d-byte answer", len(body), len(slowLinkAnswer))
	}
	select {
	case revoked = <-requests:
	case <-time.After(10 * time.Second):
		t.Fatal("the Device Connection is still open 10 s after the koko server closed it")
	}
	return revoked
}

// heldConn is a device's end of a link whose reads the test can hold back:
// what the koko server sends meanwhile waits in the buffers beneath.
type heldConn struct {
	net.Conn
	mu sync.Mutex
	// resumed, while reads are held, closes when they resume.
	resumed chan struct{}
}

func (c *heldConn) hold() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.resumed = make(chan struct{})
}

// resume lets reads go on; it does nothing while they are not held.
func (c *heldConn) resume() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.resumed != nil {
		close(c.resumed)
		c.resumed = nil
	}
}

func (c *heldConn) Read(p []byte) (int, error) {
	c.mu.Lock()
	resumed := c.resumed
	c.mu.Unlock()
	if resumed != nil {
		<-resumed
	}
	return c.Conn.Read(p)
}

// waitLog fails the test unless log holds substr within 10 s.
func waitLog(t *testing.T, log *syncBuffer, substr string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(log.String(), substr) {
		if time.Now().After(deadline) {
			t.Fatalf("the transport did not log %q within 10 s; it logged:\n%s", substr, log)
		}
		time.Sleep(time.Millisecond)
	}
}

// syncBuffer holds what a transport logs, for the test to read while it
// logs.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
