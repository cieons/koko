// Package liveness tells an SSH connection that went quiet from one that
// died, as both ends of a Device Connection need to (P0 spec: reach,
// "Liveness and timings"). Conn notes when bytes last arrived on the
// connection beneath SSH; Watch probes the peer with keepalive@openssh.com,
// wanting a reply, once nothing has arrived for a while, and declares the
// connection dead once nothing has arrived for longer. Any reply, a refusal
// included, is bytes, so a peer that knows no such request still answers.
// The device's end and the koko server's run it with their own timings.
package liveness

import (
	"fmt"
	"net"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/ssh"
)

// Request is the global request a probe sends, the one OpenSSH's own
// keepalives send.
const Request = "keepalive@openssh.com"

// DeadError says Watch declared a connection dead: nothing arrived on it
// for Quiet.
type DeadError struct {
	Quiet time.Duration
}

func (e *DeadError) Error() string {
	return fmt.Sprintf("no answer for %v", e.Quiet.Round(time.Millisecond))
}

// Conn is a net.Conn that notes when bytes last arrived on it, by the
// monotonic clock, for Watch.
type Conn struct {
	net.Conn
	start time.Time
	// last is when bytes last arrived, as time since start; 0 until then,
	// so a connection counts as heard from when it opened.
	last atomic.Int64
	// arrivals counts the reads that brought bytes, which tells Watch that
	// something arrived since a probe went out even when the clock, which
	// on Windows moves in steps of up to 16 ms, gives both the same time.
	arrivals atomic.Int64
}

// NewConn returns nc noting when bytes arrive on it.
func NewConn(nc net.Conn) *Conn {
	return &Conn{Conn: nc, start: time.Now()}
}

func (c *Conn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	if n > 0 {
		c.last.Store(int64(time.Since(c.start)))
		c.arrivals.Add(1)
	}
	return n, err
}

// Watch watches sc, the SSH connection over c, until done closes, and
// returns nil then. Once nothing has arrived on c for probe it sends sc a
// probe, and each time a value arrives on probeNow (nil for never) it sends
// one at once, unless one is still unanswered. It returns a *DeadError once
// nothing has arrived for dead, or for dead-probe since a probe went out,
// whichever comes first, so a probe sent at once gets the time to be
// answered that one sent after probe gets. It never closes sc: the caller
// notes why, then closes it.
func (c *Conn) Watch(sc ssh.Conn, probe, dead time.Duration, probeNow, done <-chan struct{}) error {
	timer := time.NewTimer(probe)
	defer timer.Stop()
	// probed is when the probe still unanswered went out, as time since
	// c.start; -1 while none is.
	probed := time.Duration(-1)
	// arrivedAtProbe is how many reads had brought bytes when it went out.
	var arrivedAtProbe int64
	for {
		now := false
		select {
		case <-done:
			return nil
		case <-probeNow:
			now = true
		case <-timer.C:
		}
		elapsed := time.Since(c.start)
		arrived := c.arrivals.Load()
		last := time.Duration(c.last.Load())
		if probed >= 0 && arrived != arrivedAtProbe {
			probed = -1 // answered, or something else arrived since
		}
		if probed < 0 && (now || elapsed >= last+probe) {
			probed, arrivedAtProbe = elapsed, arrived
			// A reply, or the connection ending, returns it.
			go func() { _, _, _ = sc.SendRequest(Request, true, nil) }()
		}
		deadAt := last + dead
		if probed >= 0 {
			deadAt = min(deadAt, probed+dead-probe)
		}
		if elapsed >= deadAt {
			return &DeadError{Quiet: elapsed - last}
		}
		// Next, probe once nothing has come for probe; while a probe is
		// unanswered, look again within probe, so that once it is answered
		// the next goes out probe after the answer, not at deadAt.
		next := last + probe
		if probed >= 0 {
			next = min(deadAt, elapsed+probe)
		}
		timer.Reset(next - elapsed)
	}
}
