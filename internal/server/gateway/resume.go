package gateway

import (
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/cieons/koko/internal/server/transport"
	"github.com/cieons/koko/internal/shared/wire"
)

// A Tunnel whose channel fails, as when the koko server declares its Device
// Connection dead, is Detached: the Gateway keeps its SSH Session and the
// Host connection for the grace (timing.Grace), while the device Resumes the
// Tunnel on another channel, from any address, so long as it comes from the
// Device Session that opened it (P0 spec: reach, "Resumption"). The last
// attach wins: a resume also takes the Tunnel off a channel the koko server
// has not yet found dead.

// unknownTunnel is the message a resume is refused with: no Tunnel of the
// caller's has the id it names. The refusal's reason is ssh.Prohibited,
// which the device tells from a channel the way to the koko server could
// not carry, so it gives up the Resume rather than try again.
const unknownTunnel = "the koko server holds no such Tunnel of this device: its grace ran out, or the koko server restarted"

// watch watches the channel the Tunnel t is on now for its end, unless it
// is watched already.
func (g *Gateway) watch(t *tunnel) {
	t.mu.Lock()
	defer t.mu.Unlock()
	attached := t.stream.Detached()
	if attached == t.watched {
		return
	}
	t.watched = attached
	g.wg.Go(func() { g.detach(t, attached) })
}

// detach waits for the stream of the Tunnel t to come off the channel whose
// end attached tells, then holds the Tunnel Detached (holdLocked).
func (g *Gateway) detach(t *tunnel, attached <-chan struct{}) {
	select {
	case <-attached:
	case <-t.ctx.Done():
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	g.holdLocked(t)
}

// holdLocked holds the Tunnel t Detached: the grace starts, and ends the
// Tunnel when it runs out. It does neither while t is on a channel, while a
// resume moves it onto one, which takes it off the last channel first and
// holds it itself should it fail, once t has ended, whatever ended it saying
// why, or while its grace runs already. t.mu is held.
func (g *Gateway) holdLocked(t *tunnel) {
	if t.resuming > 0 || t.grace != nil || t.ctx.Err() != nil || t.stream.Err() != nil {
		return
	}
	select {
	case <-t.stream.Detached():
	default:
		return
	}
	g.log.Info("ssh session detached", "tunnel", t.id, "device_session", deviceSession(t.caller))
	t.grace = time.AfterFunc(g.timings.Grace, func() { t.end(errGraceOver) })
}

// stopGrace stops the Tunnel's grace, if it runs.
func (t *tunnel) stopGrace() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.stopGraceLocked()
}

func (t *tunnel) stopGraceLocked() {
	if t.grace != nil {
		t.grace.Stop()
		t.grace = nil
	}
}

// resume takes a tunnel@koko channel on which the connection c asks to
// Resume the Tunnel p names: one of c's caller, which has not ended. It
// accepts the channel, answers with the Gateway's received offset and moves
// the Tunnel's stream onto it, off the channel it is on, if any, and logs
// the Resume with the address it came from. Any other resume is refused
// with unknownTunnel, so a Tunnel id is of no use to another Device
// Session.
func (g *Gateway) resume(c *transport.Connection, nch ssh.NewChannel, p wire.TunnelPayload) {
	id := tunnelID(p.ID)
	attrs := []any{"tunnel", id, "device_session", deviceSession(c.Caller()), "addr", c.RemoteAddr()}
	g.mu.Lock()
	t := g.tunnels[id]
	g.mu.Unlock()
	if t == nil || t.caller.DeviceSession != c.Caller().DeviceSession || t.ctx.Err() != nil {
		g.log.Info("ssh session resume refused", append(attrs, "reason", "unknown tunnel")...)
		_ = nch.Reject(ssh.Prohibited, unknownTunnel)
		return
	}
	ch, reqs, err := nch.Accept()
	if err != nil {
		g.log.Debug("accept a tunnel@koko channel", "addr", c.RemoteAddr(), "err", err)
		return
	}
	g.wg.Go(func() { ssh.DiscardRequests(reqs) })
	t.mu.Lock()
	t.resuming++
	t.mu.Unlock()
	err = t.stream.AcceptResume(ch, p.Received)
	t.mu.Lock()
	t.resuming--
	if err != nil {
		// The stream stays Detached, unless the device's offset was outside
		// its buffer, which ends it and its SSH Session.
		g.holdLocked(t)
	} else {
		t.stopGraceLocked()
	}
	t.mu.Unlock()
	if err != nil {
		g.log.Info("ssh session resume failed", append(attrs, "err", err)...)
		return
	}
	g.log.Info("ssh session resumed", attrs...)
	g.watch(t)
}
