// Package sshconn makes up for two flaws in x/crypto/ssh's connections: a
// channel opened just as its connection ends may never be answered, and its
// OpenChannel then never returns; and an open that never went out fails as
// io.EOF only when the peer closed the connection.
//
// As an x/crypto connection ends, it drops every channel it holds, failing
// their opens, and only then fails its writes. A channel opened in between
// is held by nothing that will fail it, and its open message is taken as
// sent, since a write that fails is recorded for the next one rather than
// returned. So the open waits for an answer the ended connection never
// brings. Every connection koko opens channels on goes through Wrap.
//
// Once reading or writing fails, x/crypto records the error for every later
// write, and fails an open it could not send with it: io.EOF when the peer
// closed the connection, but a *net.OpError when koko closed a dead one
// itself, as the liveness probe does after a stall.
//
// Wrap makes what its errors tell a caller hold for every end: an open fails
// as io.EOF only when it never went out, so a caller may send it again on
// another connection. One that went out may have reached the other end,
// which may have acted on it.
package sshconn

import (
	"errors"
	"io"
	"net"

	"golang.org/x/crypto/ssh"
)

// Wrap returns conn with an OpenChannel that fails once conn has ended,
// saying why it ended, rather than waiting for an answer that cannot come.
// An open made once conn has ended, such as after its Wait has returned, or
// one x/crypto could not send, never went out, and fails as io.EOF; one
// waiting for its answer when conn ends fails matching no cause, since it
// may have gone out.
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

// Wait waits for the connection to end, as x/crypto's does, and returns once
// every open made after it fails as io.EOF.
func (c *failOnEnd) Wait() error {
	<-c.ended
	return c.err
}

// OpenChannel opens a channel as x/crypto does, and fails once the
// connection has ended without an answer to the open. An open that x/crypto
// left unanswered stays in a goroutine of its own, which never returns.
func (c *failOnEnd) OpenChannel(name string, data []byte) (ssh.Channel, <-chan *ssh.Request, error) {
	select {
	case <-c.ended:
		return nil, nil, &notSentError{end: c.err}
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
	var o opened
	select {
	case o = <-done:
	case <-c.ended:
		select {
		case o = <-done:
		default:
			return nil, nil, &unansweredError{end: c.err}
		}
	}
	if notSent(o.err) {
		return nil, nil, &notSentError{end: o.err}
	}
	return o.ch, o.reqs, o.err
}

// notSent tells whether x/crypto failed an open with err because it could
// not send it: with the error its transport recorded, the network's or
// io.EOF. One it sent and then lost with its connection fails with an error
// of its own, and a refusal as *ssh.OpenChannelError.
func notSent(err error) bool {
	_, network := errors.AsType[*net.OpError](err)
	return network || errors.Is(err, io.EOF)
}

// notSentError is an open that never went out, since its connection had
// ended: it is io.EOF, as x/crypto's own is when the peer closed, and it is
// why the connection ended.
type notSentError struct{ end error }

func (e *notSentError) Error() string {
	return "the SSH connection ended before the open went out: " + e.end.Error()
}

func (e *notSentError) Unwrap() []error { return []error{io.EOF, e.end} }

// unansweredError is an open whose connection ended before it had its
// answer. It does not unwrap to why the connection ended: whether the open
// went out is unknown, and io.EOF would say it never did. Now and then it
// fails one that never went out, whose x/crypto failure came too late,
// which errs toward not sending an open twice.
type unansweredError struct{ end error }

func (e *unansweredError) Error() string {
	return "the SSH connection ended before the open had its answer: " + e.end.Error()
}
