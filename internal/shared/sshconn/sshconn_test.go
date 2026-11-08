package sshconn_test

import (
	"errors"
	"io"
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
	// release lets the unanswered OpenChannel return once the test is done.
	release chan struct{}
}

func newEndingConn(t *testing.T) *endingConn {
	c := &endingConn{ended: make(chan struct{}), release: make(chan struct{})}
	t.Cleanup(func() { close(c.release) })
	return c
}

func (c *endingConn) OpenChannel(string, []byte) (ssh.Channel, <-chan *ssh.Request, error) {
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

// A channel open on a connection that ends fails, saying why the connection
// ended, where x/crypto's own may wait for an answer that never comes.
func TestAnOpenFailsOnceTheConnectionHasEnded(t *testing.T) {
	inner := newEndingConn(t)
	conn := sshconn.Wrap(inner)
	opened := make(chan error, 1)
	go func() {
		_, _, err := conn.OpenChannel("session", nil)
		opened <- err
	}()
	_ = inner.Close()
	select {
	case err := <-opened:
		if !errors.Is(err, io.EOF) {
			t.Errorf("the open failed with %v, want the connection's end, io.EOF", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the open on a connection that has ended did not return within 5 s")
	}
}
