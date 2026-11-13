package gateway

import (
	"context"
	"maps"
	"slices"
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
//
// A resume is refused with reason ssh.Prohibited, which the device tells
// from a channel the way to the koko server could not carry, so it gives
// up the Resume rather than try again. The refusal's message is why the
// Tunnel ended (ending.why), when the Gateway knows: it keeps that of a
// Tunnel that ended with a notice until its grace would have ended. It is
// empty when the Gateway holds no such Tunnel of the caller's, as when its
// grace ran out or the koko server restarted, so a Tunnel id is of no use
// to another Device Session.

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
	g.log.Info("ssh session detached", append(t.named(), callerAttrs(t.caller)...)...)
	t.detached = time.Now()
	t.grace = time.AfterFunc(t.holdFor, func() { t.end(errGraceOver) })
}

// stopGraceLocked stops the Tunnel's grace, if it runs. t.mu is held.
func (t *tunnel) stopGraceLocked() {
	if t.grace != nil {
		t.grace.Stop()
		t.grace = nil
		t.detached = time.Time{}
	}
}

// endedTunnel is how a Tunnel that has left the registry ended, kept for a
// resume of the caller who opened it, from the same client, until until.
type endedTunnel struct {
	key    callerKey
	client string
	ending *ending
	until  time.Time
}

// keepEnding keeps how the Tunnel t ended, when its ending has a notice,
// until until: the time its grace would have ended, from when it was
// Detached or, for a Tunnel on a channel, from now, since its close frame
// may never reach a device whose path stalled. It forgets the endings kept
// past their time. g.mu is held.
func (g *Gateway) keepEnding(t *tunnel, until time.Time) {
	now := time.Now()
	maps.DeleteFunc(g.ended, func(_ tunnelID, e endedTunnel) bool { return !now.Before(e.until) })
	if e := endingOf(context.Cause(t.ctx)); e.why != "" && now.Before(until) {
		g.ended[t.id] = endedTunnel{key: t.key(), client: t.client, ending: e, until: until}
	}
}

// resumable returns the Tunnel id names, when it is one of the caller key's
// from client that has not ended and carries a stream, or else how one of
// theirs ended, if the Gateway still knows: nil for both when it holds no
// such Tunnel. A Tunnel of another client is no such Tunnel: a Tunnel of
// the CLI's never Resumes on a WebSocket, nor a web terminal on a
// tunnel@koko channel.
func (g *Gateway) resumable(id tunnelID, key callerKey, client string) (*tunnel, *ending) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if t := g.ownLocked(id, key, client); t != nil && t.stream != nil {
		if t.ctx.Err() != nil {
			return nil, endingOf(context.Cause(t.ctx))
		}
		return t, nil
	}
	if e, ok := g.ended[id]; ok && e.key == key && e.client == client && time.Now().Before(e.until) {
		return nil, e.ending
	}
	return nil, nil
}

// ownLocked returns the Tunnel id names in the registry, when it is one of
// the caller key's from client, or nil. g.mu is held.
func (g *Gateway) ownLocked(id tunnelID, key callerKey, client string) *tunnel {
	if t := g.tunnels[id]; t != nil && t.key() == key && t.client == client {
		return t
	}
	return nil
}

// recheckResume checks the access of the SSH Session in the Tunnel t
// again before t Resumes, as recheck checks a live one after an access
// change, which ends t when a new SSH Session would now be refused or the
// read fails. It returns how t ended, or nil when t may Resume.
func (g *Gateway) recheckResume(t *tunnel) *ending {
	if t.hostID == "" {
		// Refused: its client's "none" gets the banner, and nothing more.
		return nil
	}
	g.recheck([]*tunnel{t})
	if t.ctx.Err() == nil {
		return nil
	}
	return endingOf(context.Cause(t.ctx))
}

// resume takes a tunnel@koko channel on which the connection c asks to
// Resume the Tunnel p names: one of c's caller, which has not ended, and
// whose access holds still (recheckResume). It accepts the channel, answers
// with the Gateway's received offset and moves the Tunnel's stream onto it,
// off the channel it is on, if any, and logs the Resume with the address it
// came from. Any other resume is refused, saying why the Tunnel ended when
// the Gateway knows.
func (g *Gateway) resume(c *transport.Connection, nch ssh.NewChannel, p wire.TunnelPayload) {
	id := tunnelID(p.ID)
	attrs := slices.Concat([]any{"tunnel", id, "client", clientSSH}, callerAttrs(c.Caller()), []any{"addr", c.RemoteAddr()})
	t, ended := g.resumable(id, callerKey{id: c.Caller().ID()}, clientSSH)
	if t != nil {
		ended = g.recheckResume(t)
	}
	if t == nil || ended != nil {
		reason, why := "unknown tunnel", ""
		if ended != nil {
			reason, why = ended.reason, ended.why
		}
		g.log.Info("ssh session resume refused", append(attrs, "reason", reason)...)
		_ = nch.Reject(ssh.Prohibited, why)
		return
	}
	ch, reqs, err := nch.Accept()
	if err != nil {
		g.log.Debug("accept a tunnel@koko channel", "addr", c.RemoteAddr(), "err", err)
		return
	}
	g.wg.Go(func() { ssh.DiscardRequests(reqs) })
	g.moveOnto(t, attrs, func() error { return t.stream.AcceptResume(ch, p.Received) })
}

// moveOnto Resumes the Tunnel t through attach, which moves its stream onto
// a new channel, off the one it is on, if any, and logs the outcome with
// attrs. Once on, its grace stops, and its new channel is watched for its
// end. A failed attach leaves t Detached, its grace running, unless the
// other end's offset was outside the stream's buffer, which ends the
// stream and the SSH Session.
func (g *Gateway) moveOnto(t *tunnel, attrs []any, attach func() error) {
	t.mu.Lock()
	t.resuming++
	t.mu.Unlock()
	err := attach()
	t.mu.Lock()
	t.resuming--
	if err != nil {
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
