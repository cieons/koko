// Package sshconn makes up for a flaw in x/crypto/ssh's connections: a
// channel opened just as its connection ends may never be answered, and its
// OpenChannel then never returns.
//
// As an x/crypto connection ends, it drops every channel it holds, failing
// their opens, and only then fails its writes. A channel opened in between
// is held by nothing that will fail it, and its open message is taken as
// sent, since a write that fails is recorded for the next one rather than
// returned. So the open waits for an answer the ended connection never
// brings. Every connection koko opens channels on goes through Wrap.
//
// Wrap keeps what x/crypto's errors tell a caller: an open fails as io.EOF
// only when it never went out, so a caller may send it again on another
// connection. One that went out may have reached the other end, which may
// have acted on it.
package sshconn

import (
	"fmt"

	"golang.org/x/crypto/ssh"
)

// Wrap returns conn with an OpenChannel that fails once conn has ended,
// saying why it ended, rather than waiting for an answer that cannot come.
// An open made once conn has ended fails as its end, such as io.EOF, since
// it never went out; one waiting for its answer when conn ends fails
// matching no cause, since it may have gone out.
func Wrap(conn ssh.Conn) ssh.Conn {
	c := &failOnEnd{Conn: conn, ended: make(chan struct{})}
	go func() {
		c.err = conn.Wait()
		close(c.ended)
	}()
	return c
}

// failOnEnd is an SSH connection whose channel opens fail once it has ended.
type failOnEnd struct {
	ssh.Conn
	// ended is closed once the connection has ended, and err is why.
	ended chan struct{}
	err   error
}

// OpenChannel opens a channel as x/crypto does, and fails once the
// connection has ended without an answer to the open. An open that x/crypto
// left unanswered stays in a goroutine of its own, which never returns.
func (c *failOnEnd) OpenChannel(name string, data []byte) (ssh.Channel, <-chan *ssh.Request, error) {
	select {
	case <-c.ended:
		return nil, nil, fmt.Errorf("the SSH connection ended: %w", c.err)
	default:
	}
	type opened struct {
		ch   ssh.Channel
		reqs <-chan *ssh.Request
		err  error
	}
	done := make(chan opened, 1)
	go func() {
		ch, reqs, err := c.Conn.OpenChannel(name, data)
		done <- opened{ch, reqs, err}
	}()
	select {
	case o := <-done:
		return o.ch, o.reqs, o.err
	case <-c.ended:
		select {
		case o := <-done:
			return o.ch, o.reqs, o.err
		default:
			return nil, nil, &unansweredError{end: c.err}
		}
	}
}

// unansweredError is an open whose connection ended before it had its
// answer. It does not unwrap to why the connection ended: whether the open
// went out is unknown, and io.EOF would say it never did. Now and then it
// fails one that never went out, whose x/crypto failure came too late,
// which errs toward not sending an open twice.
type unansweredError struct{ end error }

func (e *unansweredError) Error() string {
	return "the SSH connection ended before the open had its answer: " + e.end.Error()
}
