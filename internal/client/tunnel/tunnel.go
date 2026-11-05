// Package tunnel is the device end of a Tunnel, which koko exec and koko
// tunnel share: it opens a Tunnel to the Host an SSH Name names over a
// connection to the koko server, reads the Tunnel id the Gateway sends
// first, and then carries the SSH Session's bytes on a resumable stream
// (P0 spec: reach, "The Tunnel on the wire"). Until Tunnels Resume, a Tunnel
// whose channel fails ends.
package tunnel

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/cieons/koko/internal/client/serverconn"
	"github.com/cieons/koko/internal/shared/resumable"
	"github.com/cieons/koko/internal/shared/wire"
)

// Tunnel is the device end of one Tunnel: a byte stream to the Gateway's SSH
// server, for an SSH client to run on.
type Tunnel struct {
	stream *resumable.Stream
	// id is the Tunnel id the Gateway picked, which a resume will name.
	id [wire.TunnelIDSize]byte

	mu sync.Mutex
	// lost is when the Tunnel's channel failed, or zero while it has not.
	lost time.Time
}

// LostError says the Tunnel's channel to the koko server failed, which ended
// it.
type LostError struct {
	// At is when the device noticed.
	At time.Time
}

func (e *LostError) Error() string {
	return "lost the connection to the koko server at " + e.At.Format(time.TimeOnly)
}

// Open opens a Tunnel over conn to the Host sshName names, alone or followed
// by .koko, and reads its id. It gives up when ctx ends first.
func Open(ctx context.Context, conn serverconn.Conn, sshName string) (*Tunnel, error) {
	ch, err := conn.OpenChannel(ctx, wire.ChannelTunnel, wire.TunnelOpen(sshName))
	if err != nil {
		return nil, err
	}
	t := &Tunnel{stream: resumable.New(ch)}
	go t.watch()
	stop := context.AfterFunc(ctx, func() { _ = t.stream.Close() })
	_, err = io.ReadFull(t.stream, t.id[:])
	if !stop() {
		err = ctx.Err()
	}
	if err != nil {
		_ = t.Close()
		return nil, fmt.Errorf("read the Tunnel's id: %w", t.cause(err))
	}
	return t, nil
}

// watch ends the Tunnel once its channel fails, as nothing Resumes it yet. A
// stream that ended otherwise has its reason in its Err.
func (t *Tunnel) watch() {
	<-t.stream.Detached()
	if t.stream.Err() != nil {
		return
	}
	t.mu.Lock()
	t.lost = time.Now()
	t.mu.Unlock()
	_ = t.stream.Close()
}

// Err returns why the Tunnel ended: a *LostError, the Gateway's close
// (*resumable.PeerClosedError), or resumable.ErrClosed once this end closed
// it; nil while it lives.
func (t *Tunnel) Err() error {
	return t.cause(t.stream.Err())
}

// cause returns a *LostError in place of err when the Tunnel's channel
// failed.
func (t *Tunnel) cause(err error) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.lost.IsZero() {
		return &LostError{At: t.lost}
	}
	return err
}

// Read reads what the Gateway sends.
func (t *Tunnel) Read(p []byte) (int, error) {
	n, err := t.stream.Read(p)
	if err != nil && !errors.Is(err, io.EOF) {
		err = t.cause(err)
	}
	return n, err
}

// Write sends p to the Gateway.
func (t *Tunnel) Write(p []byte) (int, error) {
	n, err := t.stream.Write(p)
	if err != nil {
		err = t.cause(err)
	}
	return n, err
}

// Close ends the Tunnel for good, telling the Gateway.
func (t *Tunnel) Close() error {
	return t.stream.Close()
}
