package sshconn_test

import (
	"crypto/ed25519"
	"errors"
	"io"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/cieons/koko/internal/shared/sshconn"
)

// endingConn is an SSH connection whose OpenChannel never answers, as
// x/crypto's may not for a channel opened just as its connection ends; Close
// ends the connection, and Wait then returns io.EOF.
type endingConn struct {
	ssh.Conn
	ended chan struct{}
	// asked is closed once OpenChannel is first called, and opens counts the
	// calls.
	asked chan struct{}
	opens atomic.Int32
	// release lets the unanswered OpenChannel return once the test is done.
	release chan struct{}
}

func newEndingConn(t *testing.T) *endingConn {
	c := &endingConn{ended: make(chan struct{}), asked: make(chan struct{}), release: make(chan struct{})}
	t.Cleanup(func() { close(c.release) })
	return c
}

func (c *endingConn) OpenChannel(string, []byte) (ssh.Channel, <-chan *ssh.Request, error) {
	if c.opens.Add(1) == 1 {
		close(c.asked)
	}
	<-c.release
	return nil, nil, errors.New("released at the test's end")
}

func (c *endingConn) Wait() error {
	<-c.ended
	return io.EOF
}

func (c *endingConn) Close() error {
	close(c.ended)
	return nil
}

// A channel open waiting for its answer when its connection ends fails,
// saying why the connection ended, where x/crypto's own may wait for an
// answer that never comes. It does not fail as io.EOF, which tells a caller
// such as the Device Mux that the open never went out: this one may have
// reached the other end, which may have acted on it. An open once the
// connection has ended goes nowhere, and fails as the end, io.EOF.
func TestAnOpenFailsOnceTheConnectionHasEnded(t *testing.T) {
	inner := newEndingConn(t)
	conn := sshconn.Wrap(inner)
	opened := make(chan error, 1)
	go func() {
		_, _, err := conn.OpenChannel("session", nil)
		opened <- err
	}()
	<-inner.asked
	_ = inner.Close()
	select {
	case err := <-opened:
		if err == nil || errors.Is(err, io.EOF) || !strings.Contains(err.Error(), io.EOF.Error()) {
			t.Errorf("the open waiting for its answer failed with %v, want an error that says the connection ended with EOF but is not io.EOF", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the open on a connection that has ended did not return within 5 s")
	}

	if _, _, err := conn.OpenChannel("session", nil); !errors.Is(err, io.EOF) {
		t.Errorf("an open after the connection ended failed with %v, want the connection's end, io.EOF", err)
	}
	if n := inner.opens.Load(); n != 1 {
		t.Errorf("the connection was asked for %d opens, want 1: none after it ended", n)
	}
}

// failingConn is an SSH connection whose OpenChannel fails with openErr, as
// x/crypto's does as its connection ends, and whose Wait waits for the
// test's end.
type failingConn struct {
	ssh.Conn
	openErr error
	ended   chan struct{}
}

func (c *failingConn) OpenChannel(string, []byte) (ssh.Channel, <-chan *ssh.Request, error) {
	return nil, nil, c.openErr
}

func (c *failingConn) Wait() error {
	<-c.ended
	return io.EOF
}

// x/crypto fails an open it could not send with the error its transport
// recorded, io.EOF from a peer that closed, but a *net.OpError once koko
// closed a dead connection itself, as reading or as writing found it. Such
// an open never went out, so it fails as io.EOF, saying why; one x/crypto
// sent and then lost with its connection fails otherwise, and does not.
func TestAnOpenThatNeverWentOutFailsAsEOF(t *testing.T) {
	for _, c := range []struct {
		name    string
		openErr error
		notSent bool
	}{
		{"the peer closed", io.EOF, true},
		{"reading found it closed", &net.OpError{Op: "read", Net: "tcp", Err: net.ErrClosed}, true},
		{"writing found it closed", &net.OpError{Op: "write", Net: "tcp", Err: net.ErrClosed}, true},
		{"sent and lost", errors.New("ssh: unexpected packet in response to channel open: <nil>"), false},
	} {
		t.Run(c.name, func(t *testing.T) {
			inner := &failingConn{openErr: c.openErr, ended: make(chan struct{})}
			t.Cleanup(func() { close(inner.ended) })
			_, _, err := sshconn.Wrap(inner).OpenChannel("session", nil)
			if errors.Is(err, io.EOF) != c.notSent || !strings.Contains(err.Error(), c.openErr.Error()) {
				t.Errorf("the open failed with %v; want io.EOF: %t, saying %q", err, c.notSent, c.openErr)
			}
		})
	}
}

// An open on a real x/crypto connection that koko closed beneath it, as a
// Device Connection's liveness probe closes a dead one, fails as io.EOF,
// though the connection ended with the close's error, not io.EOF.
func TestAnOpenOnAConnectionClosedBeneathItFailsAsEOF(t *testing.T) {
	var lc net.ListenConfig
	ln, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	var d net.Dialer
	client, err := d.DialContext(t.Context(), "tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	server, err := ln.Accept()
	if err != nil {
		t.Fatal(err)
	}
	_, signer, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	hostKey, err := ssh.NewSignerFromKey(signer)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &ssh.ServerConfig{NoClientAuth: true}
	cfg.AddHostKey(hostKey)
	go func() {
		if sc, chans, reqs, err := ssh.NewServerConn(server, cfg); err == nil {
			go ssh.DiscardRequests(reqs)
			go func() {
				for nch := range chans {
					_ = nch.Reject(ssh.Prohibited, "no channels")
				}
			}()
			_ = sc.Wait()
		}
	}()
	t.Cleanup(func() { _ = server.Close() })
	inner, _, reqs, err := ssh.NewClientConn(client, "pipe", &ssh.ClientConfig{HostKeyCallback: ssh.InsecureIgnoreHostKey()})
	if err != nil {
		t.Fatal(err)
	}
	go ssh.DiscardRequests(reqs)
	conn := sshconn.Wrap(inner)

	_ = client.Close()
	if end := conn.Wait(); errors.Is(end, io.EOF) {
		t.Fatalf("the connection ended with %v, want the close's error, not io.EOF", end)
	}
	if _, _, err := conn.OpenChannel("session", nil); !errors.Is(err, io.EOF) {
		t.Errorf("an open after the connection was closed beneath it failed with %v, want io.EOF", err)
	}
}
