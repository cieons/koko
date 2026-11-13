package gateway

import (
	"context"
	"errors"
	"slices"

	"github.com/cieons/koko/internal/server/access"
	"github.com/cieons/koko/internal/server/deployment"
	"github.com/cieons/koko/internal/server/op"
	"github.com/cieons/koko/internal/server/store"
	"github.com/cieons/koko/internal/shared/api"
)

// A page whose caller has ended detaches (spec #213, "What ends an SSH
// Session, and what only detaches"): its Workspace socket and its
// attachments are cut, and the SSH Sessions go on, unless the ending also
// takes back what that caller opened (Withdraw). The page finds out why as
// it reconnects, from the web front's refusal: it goes to Sign in, or shows
// the shell's words for revoked and cookie_required. Its caller ends by a
// change the access-change signal names (Withdraw), by a Sign-in's expiry
// the sweep finds (checkSignIns), by a request that finds it ended
// (wsPage.read), or, for the Local caller, by the end of its run of koko
// web (TerminalCaller.RunEnded).

// detachPages detaches every page for which ended, given its caller and
// its User's id, returns a reason: each attachment is cut and ends, its
// line in the log saying the reason, and each Workspace socket is cut. It takes
// g.mu and each Workspace's lock in turn, never one inside the other.
func (g *Gateway) detachPages(ended func(caller op.Caller, userID string) string) {
	type detaching struct {
		a      *attachment
		reason string
	}
	var atts []detaching
	g.mu.Lock()
	for _, a := range g.attachments {
		if reason := ended(a.caller, a.s.t.userID); reason != "" {
			atts = append(atts, detaching{a, reason})
		}
	}
	g.mu.Unlock()
	for _, d := range atts {
		// Each in a goroutine of its own: a cut through koko web on a
		// stalled Device Connection can take its time.
		g.wg.Go(func() { d.a.detach(d.reason) })
	}
	for _, w := range g.workspaces.all() {
		w.mu.Lock()
		for p := range w.pages {
			if reason := ended(p.c.Caller, w.userID); reason != "" {
				p.cut(reason)
			}
		}
		w.mu.Unlock()
	}
}

// detachCaller detaches every page of the caller c for reason. The Local
// caller, whose id is empty and who never ends, has none detached.
func (g *Gateway) detachCaller(c op.Caller, reason string) {
	id := c.ID()
	if id == "" {
		return
	}
	g.detachPages(func(caller op.Caller, _ string) string {
		if caller.ID() == id {
			return reason
		}
		return ""
	})
}

// detach cuts the attachment's WebSocket, then ends it for reason, which
// its line in the log says; the SSH Session goes on. Its stream, off its
// WebSocket, closes at once. An attachment whose SSH Session the same
// change ended is left to that ending, which gives the page its notice.
func (a *attachment) detach(reason string) {
	if a.s.t.ctx.Err() != nil {
		return
	}
	a.cut()
	a.end(reason, nil, "")
}

// cut cuts the page's Workspace socket, unless it is closing already, for
// reason, which the log says at debug level.
func (p *wsPage) cut(reason string) {
	p.mu.Lock()
	if p.closing {
		p.mu.Unlock()
		return
	}
	p.closing = true
	p.queue = nil
	p.mu.Unlock()
	signal(p.wake)
	g := p.w.ws.g
	g.log.Debug("workspace socket closed", append(callerAttrs(p.c.Caller), "reason", reason)...)
	g.wg.Go(p.s.Cut)
}

// isClosing tells whether the page's Workspace socket is closing, as one
// detached is: it takes no more requests.
func (p *wsPage) isClosing() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.closing
}

// endedSince tells why the caller c, read live once the access-change
// signal had been taken seen times, has ended since, as an attach or a
// Workspace socket found it once registered: Withdraw cannot have detached
// a page it had not yet seen. A page on the web listener is read again
// whatever came since, as the Web URL may have changed after the web front
// checked its Origin and before seen was taken. It returns "" for a caller
// still live and, on the web listener, at the Web URL its page has.
func (g *Gateway) endedSince(c TerminalCaller, seen uint64) string {
	g.mu.Lock()
	changed, webURL := g.withdrawals != seen, g.webURLChanged > seen
	g.mu.Unlock()
	switch {
	case c.WebURL != "" && webURL:
		return "web url changed"
	case !changed && c.WebURL == "":
		return ""
	}
	return g.callerEnd(c)
}

// callerEnd reads whether the caller c has ended, or its page's Web URL is
// no longer the koko server's, and returns why as the log's reason for its
// pages' detach, or "" while it lives. A read that fails is taken for an
// ending, as it would refuse the page anew.
func (g *Gateway) callerEnd(c TerminalCaller) string {
	ctx := g.readContext(context.Background())
	reason := ""
	err := g.store.Read(ctx, func(tx store.Tx) error {
		_, err := access.Who(ctx, tx, c.Caller)
		e, ok := errors.AsType[*api.Error](err)
		switch {
		case err == nil:
		case !ok:
			return err
		case e.Code == api.CodeRevoked.Name:
			reason = "revoked"
			return nil
		case e.Code == api.CodeSignInRequired.Name:
			found, err := access.SignIns(ctx, tx, op.Now(ctx), "s.id = ?", c.Caller.SignIn)
			if err != nil {
				return err
			}
			reason = signInReason("")
			if len(found) == 1 {
				reason = signInReason(found[0].EndReason)
			}
			return nil
		default:
			reason = "access withdrawn"
			return nil
		}
		if c.WebURL == "" {
			return nil
		}
		current, _, err := deployment.WebURL(ctx, tx)
		if err == nil && current != c.WebURL {
			reason = "web url changed"
		}
		return err
	})
	if err != nil {
		g.log.Error("read whether a web terminal's caller has ended", slices.Concat(callerAttrs(c.Caller), []any{"err", err})...)
		return "access check failed"
	}
	if reason == signInReason(api.EndExpired) {
		g.recordExpiry(c.Caller.SignIn)
	}
	return reason
}

// whenRunEnds has fn run once the run of koko web the Local caller c's page
// came through ends, unless done is closed first; nothing for any other
// caller.
func (g *Gateway) whenRunEnds(c TerminalCaller, done <-chan struct{}, fn func()) {
	if c.RunEnded == nil {
		return
	}
	g.wg.Go(func() {
		select {
		case <-c.RunEnded:
			fn()
		case <-done:
		}
	})
}
