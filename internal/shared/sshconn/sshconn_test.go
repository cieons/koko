package sshconn_test

import (
	"errors"
	"io"
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
