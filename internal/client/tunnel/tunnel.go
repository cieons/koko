// Package tunnel is the device end of a Tunnel, which koko exec and koko
// tunnel share: it opens a Tunnel to the Host an SSH Name names over a
// connection to the koko server, reads the Tunnel id the Gateway sends
// first, and then carries the SSH Session's bytes on a resumable stream
// (P0 spec: reach, "The Tunnel on the wire").
//
// When the stream's channel fails, the Tunnel Resumes, so the SSH Session
// above it only freezes (P0 spec: reach, "Resumption"): on the same
// connection while it lasts, as one through a Device Mux does, whose next
// Device Connection the resume waits for; once that connection has ended,
// on a new one, through a new Device Mux should the old one have crashed,
// or over a Device Connection of its own. It gives up only when the koko
// server refuses the resume, or when nothing it could try would reach the
// koko server again: koko disconnect, a revocation, a device no longer
// joined.
package tunnel

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/cieons/koko/internal/client/serverconn"
	"github.com/cieons/koko/internal/shared/resumable"
	"github.com/cieons/koko/internal/shared/timing"
	"github.com/cieons/koko/internal/shared/wire"
)

// Dial reaches the koko server anew, for a Tunnel whose connection has
// ended to Resume on. A failure that trying again will not change wraps
// ErrFinal.
type Dial func(ctx context.Context) (serverconn.Conn, error)

// ErrFinal marks a Dial's failure that trying again will not change, such
// as a device whose Device Session was Revoked: the Tunnel gives up its
// Resume.
var ErrFinal = errors.New("trying again would not reach the koko server")

// Tunnel is the device end of one Tunnel: a byte stream to the Gateway's SSH
// server, for an SSH client to run on.
type Tunnel struct {
	stream *resumable.Stream
	// id is the Tunnel id the Gateway picked, which a resume names.
	id      [wire.TunnelIDSize]byte
	redial  Dial
	timings timing.Timings
	// ctx ends once the Tunnel is closed, which stops a Resume; resumerDone
	// is closed once resumeAcrossDrops has returned.
	ctx         context.Context
	stop        context.CancelFunc
	resumerDone chan struct{}

	mu sync.Mutex
	// idRead tells that Open has read the id, without which there is
	// nothing to Resume.
	idRead bool
	// conn is the connection the Tunnel Resumes on next.
	conn *connection
	// lost is why the Tunnel ended, once it could not Resume.
	lost *LostError
}

// connection is a connection to the koko server a Tunnel Resumes on.
type connection struct {
	serverconn.Conn
	// ended is closed once the connection has ended, and err is why.
	ended chan struct{}
	err   error
	// own tells that the Tunnel dialled it, and so closes it.
	own bool
}

// watchConn returns c as a connection whose end the Tunnel sees.
func watchConn(c serverconn.Conn, own bool) *connection {
	cc := &connection{Conn: c, ended: make(chan struct{}), own: own}
	go func() {
		cc.err = c.Wait()
		close(cc.ended)
	}()
	return cc
}

// LostError says the Tunnel lost its channel to the koko server and could
// not Resume, which ended it.
type LostError struct {
	// At is when the device noticed the loss.
	At time.Time
	// Err is why it could not Resume: the koko server refused the resume,
	// or nothing would reach the koko server again, such as after koko
	// disconnect (serverconn.ErrDisconnected).
	Err error
}

func (e *LostError) Error() string {
	return "lost the connection to the koko server at " + e.At.Format(time.TimeOnly)
}

func (e *LostError) Unwrap() error { return e.Err }

// refusedError is a resume the koko server refused, with its message.
type refusedError struct {
	message string
}

func (e *refusedError) Error() string { return "the koko server refused the resume: " + e.message }

// Open opens a Tunnel over conn to the Host sshName names, alone or followed
// by .koko, and reads its id. It gives up when ctx ends first. The Tunnel
// Resumes on conn, and once conn has ended, on the connections redial
// reaches, which it closes when it is done with them; timings is the
// timing-constants hook, the zero Timings the product's.
func Open(ctx context.Context, conn serverconn.Conn, sshName string, redial Dial, timings timing.Timings) (*Tunnel, error) {
	ch, err := conn.OpenChannel(ctx, wire.ChannelTunnel, wire.TunnelOpen(sshName))
	if err != nil {
		return nil, err
	}
	t := &Tunnel{
		stream: resumable.New(ch), redial: redial, timings: timings.Resolve(),
		conn: watchConn(conn, false), resumerDone: make(chan struct{}),
	}
	t.ctx, t.stop = context.WithCancel(context.Background())
	go t.resumeAcrossDrops()
	stop := context.AfterFunc(ctx, func() { _ = t.stream.Close() })
	_, err = io.ReadFull(t.stream, t.id[:])
	if !stop() {
		err = ctx.Err()
	}
	if err == nil {
		t.mu.Lock()
		t.idRead = true
		if t.lost != nil {
			err = t.lost
		}
		t.mu.Unlock()
	}
	if err != nil {
		_ = t.Close()
		return nil, fmt.Errorf("read the Tunnel's id: %w", t.cause(err))
	}
	return t, nil
}

// resumeAcrossDrops Resumes the Tunnel each time its stream comes off its
// channel, until the Tunnel ends: it is closed, the Gateway closes it, or
// it cannot Resume, which ends it with a *LostError. A stream whose channel
// failed before the id came cannot Resume.
func (t *Tunnel) resumeAcrossDrops() {
	defer close(t.resumerDone)
	for {
		select {
		case <-t.stream.Detached():
		case <-t.ctx.Done():
			return
		}
		if t.stream.Err() != nil {
			return
		}
		at := time.Now()
		t.mu.Lock()
		idRead := t.idRead
		t.mu.Unlock()
		err := errors.New("the Tunnel's channel failed before its id came")
		if idRead {
			err = t.resume()
		}
		if t.ctx.Err() != nil {
			return
		}
		if err != nil {
			t.mu.Lock()
			t.lost = &LostError{At: at, Err: err}
			t.mu.Unlock()
			_ = t.stream.Close()
			return
		}
	}
}

// resume Resumes the Tunnel, whose stream is off its channel, trying again
// after a failure trying again may change. Each try begins at least a wait
// after the last one began, the wait timing.ReconnectFirst at first and
// doubling up to timing.ReconnectMax, so one that took longer, as a resume
// waiting out a Device Mux's timing.CommandWait does, is followed at once.
// Meanwhile it tells the Device Mux it goes through, if any, that it waits
// to Resume.
func (t *Tunnel) resume() error {
	received := t.stream.Detach()
	var told *connection
	untell := func() {}
	defer func() { untell() }()
	var wait time.Duration
	for {
		began := time.Now()
		c, err := t.connection()
		if err == nil {
			if c != told {
				untell()
				told, untell = c, waitingToResume(t.ctx, c)
			}
			if err = t.resumeOn(c, received); err == nil {
				return nil
			}
		}
		if final(err) || t.stream.Err() != nil || t.ctx.Err() != nil {
			return err
		}
		wait = t.timings.NextReconnect(wait)
		timer := time.NewTimer(wait - time.Since(began))
		select {
		case <-timer.C:
		case <-t.ctx.Done():
			timer.Stop()
			return t.ctx.Err()
		}
	}
}

// final tells whether a try to Resume failed with err for a reason trying
// again will not change.
func final(err error) bool {
	_, refused := errors.AsType[*refusedError](err)
	return refused || errors.Is(err, ErrFinal) || errors.Is(err, serverconn.ErrDisconnected) || errors.Is(err, serverconn.ErrRevoked)
}

// connection returns the connection to Resume on: the one the Tunnel holds,
// while it lasts, and once it has ended, a new one that redial reaches. One
// that koko disconnect ended, or the koko server for a revocation, ends
// the Resume instead.
func (t *Tunnel) connection() (*connection, error) {
	t.mu.Lock()
	c := t.conn
	t.mu.Unlock()
	select {
	case <-c.ended:
	default:
		return c, nil
	}
	if final(c.err) {
		return nil, c.err
	}
	conn, err := t.redial(t.ctx)
	if err != nil {
		return nil, err
	}
	next := watchConn(conn, true)
	t.mu.Lock()
	t.conn = next
	t.mu.Unlock()
	if c.own {
		_ = c.Close()
	}
	return next, nil
}

// resumeOn sends a resume, naming the Tunnel and the offset received of its
// stream, on a new channel of c, and attaches the stream to it once the koko
// server has answered, within timing.ResumeExchange. The koko server's
// refusal, which a Device Mux passes back as it is, is a *refusedError.
func (t *Tunnel) resumeOn(c *connection, received uint64) error {
	ch, err := c.OpenChannel(t.ctx, wire.ChannelTunnel, wire.TunnelResume(t.id, received))
	if e, ok := errors.AsType[*ssh.OpenChannelError](err); ok && e.Reason == ssh.Prohibited {
		return &refusedError{message: e.Message}
	}
	if err != nil {
		return err
	}
	timer := time.AfterFunc(t.timings.ResumeExchange, func() { _ = ch.Close() })
	err = t.stream.Resume(ch, received)
	if !timer.Stop() && err != nil {
		err = fmt.Errorf("no answer to the resume within %v: %w", t.timings.ResumeExchange, err)
	}
	return err
}

// resumeCounter is a connection through a Device Mux, which counts the
// Tunnels on it that wait to Resume, for koko status.
type resumeCounter interface {
	WaitingToResume(ctx context.Context) (stop func())
}

// waitingToResume tells the Device Mux that c goes through, if any, that
// the Tunnel waits to Resume, until the stop it returns is called.
func waitingToResume(ctx context.Context, c *connection) (stop func()) {
	if w, ok := c.Conn.(resumeCounter); ok {
		return w.WaitingToResume(ctx)
	}
	return func() {}
}

// Err returns why the Tunnel ended: a *LostError, the Gateway's close
// (*resumable.PeerClosedError), or resumable.ErrClosed once this end closed
// it; nil while it lives, Detached or not.
func (t *Tunnel) Err() error {
	return t.cause(t.stream.Err())
}

// cause returns the *LostError in place of err once the Tunnel could not
// Resume.
func (t *Tunnel) cause(err error) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.lost != nil {
		return t.lost
	}
	return err
}

// Read reads what the Gateway sends, waiting through a Resume.
func (t *Tunnel) Read(p []byte) (int, error) {
	n, err := t.stream.Read(p)
	if err != nil && !errors.Is(err, io.EOF) {
		err = t.cause(err)
	}
	return n, err
}

// Write sends p to the Gateway; while the Tunnel Resumes, it holds what it
// has room for in the stream's buffer.
func (t *Tunnel) Write(p []byte) (int, error) {
	n, err := t.stream.Write(p)
	if err != nil {
		err = t.cause(err)
	}
	return n, err
}

// CloseWrite half-closes the Tunnel: the Gateway reads what was sent
// before, then EOF, and the Tunnel still carries what the Gateway sends.
func (t *Tunnel) CloseWrite() error {
	err := t.stream.CloseWrite()
	if err != nil {
		err = t.cause(err)
	}
	return err
}

// Close ends the Tunnel for good, telling the Gateway when the Tunnel is on
// a channel, and closes the connections it dialled. A Tunnel closed while
// Detached is left to the Gateway's grace.
func (t *Tunnel) Close() error {
	t.stop()
	err := t.stream.Close()
	<-t.resumerDone
	t.mu.Lock()
	c := t.conn
	t.mu.Unlock()
	if c.own {
		_ = c.Close()
	}
	return err
}
