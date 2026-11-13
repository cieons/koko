package gateway

import (
	"cmp"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/cieons/koko/internal/server/access"
	"github.com/cieons/koko/internal/server/host"
	"github.com/cieons/koko/internal/server/op"
	"github.com/cieons/koko/internal/server/store"
	"github.com/cieons/koko/internal/shared/api"
)

// Withdraw ends each live SSH Session that the committed change c withdrew
// access from, since one goes on only while a new one would be allowed (P0
// spec: reach), and detaches the pages of each caller c ended (detachPages).
// Each SSH Session of a User c Removed ends at once, and so does each a
// Device Session c Revoked opened, each a Sign-in c took back opened
// (takesBack), and when c changed the Web URL, each a Sign-in opened;
// each, when c names any Host, or each of a User it narrowed, ends once a
// read of the record finds that a new one would now be refused (recheck).
// A Sign-in that only signed out or expired takes back nothing it opened
// (spec #213, "What ends an SSH Session, and what only detaches"). The
// channel it returns is closed once the SSH Sessions the Revoked Device
// Sessions opened have given their notice and ended, so that their Device
// Connections close only after. It never blocks, since the access-change
// signal calls it before the call that made the change answers.
func (g *Gateway) Withdraw(c access.Change) <-chan struct{} {
	g.mu.Lock()
	defer g.mu.Unlock()
	told := make(chan struct{})
	if g.closed {
		// Close ends every Tunnel with its own notice.
		close(told)
		return told
	}
	g.withdrawals++
	if c.WebURL {
		g.webURLChanged = g.withdrawals
	}
	var revoked []chan struct{}
	var recheck []*tunnel
	for _, t := range g.tunnels {
		if t.hostID == "" {
			// Refused: its client's "none" gets the banner.
			continue
		}
		switch {
		case c.Removed != "" && t.userID == c.Removed:
			t.end(userRemoved(t))
		case slices.Contains(c.Revoked, t.caller.DeviceSession):
			t.end(revokedEnding(t))
		case slices.Contains(c.SignIns, t.caller.SignIn) && takesBack(c.SignInEnd):
			t.end(signInEnding(t, c.SignInEnd))
		case t.caller.SignIn != "" && c.WebURL:
			t.end(errWebURLChanged)
		case slices.Contains(c.Narrowed, t.userID),
			// Any Host c names may be the Relay this SSH Session passes
			// through, or the one its Host has had since, even one that
			// started with none, which only a read tells: check every SSH
			// Session.
			len(c.Hosts) > 0:
			recheck = append(recheck, t)
		}
		if slices.Contains(c.Revoked, t.caller.DeviceSession) {
			revoked = append(revoked, t.done)
		}
	}
	if len(recheck) > 0 {
		g.wg.Go(func() { g.recheck(recheck) })
	}
	if ended := pagesEnded(c); ended != nil {
		g.wg.Go(func() {
			g.detachPages(ended)
			if c.Removed != "" {
				g.workspaces.drop(c.Removed)
			}
		})
	}
	g.wg.Go(func() {
		for _, done := range revoked {
			<-done
		}
		close(told)
	})
	return told
}

// pagesEnded is which pages the change c ends the caller of, as
// detachPages takes it, or nil for none: those of a User c Removed, of a Device
// Session it Revoked, of a Sign-in it ended, and when it changed the Web
// URL, every page on the web listener, a Sign-in's.
func pagesEnded(c access.Change) func(caller op.Caller, userID string) string {
	if c.Removed == "" && len(c.Revoked) == 0 && len(c.SignIns) == 0 && !c.WebURL {
		return nil
	}
	return func(caller op.Caller, userID string) string {
		switch {
		case c.Removed != "" && userID == c.Removed:
			return "user removed"
		case slices.Contains(c.Revoked, caller.DeviceSession):
			return "revoked"
		case slices.Contains(c.SignIns, caller.SignIn):
			return signInReason(c.SignInEnd)
		case caller.SignIn != "" && c.WebURL:
			return "web url changed"
		}
		return ""
	}
}

// recheck checks the access of the live SSH Sessions ts again, in one read
// of the record, and ends each that a new SSH Session would now be refused
// (withdrawal). A read that fails ends them all, as it would refuse a new
// one, saying the log tells why.
func (g *Gateway) recheck(ts []*tunnel) {
	ctx := g.readContext(context.Background())
	ends := make([]*ending, len(ts))
	err := g.store.Read(ctx, func(tx store.Tx) error {
		for i, t := range ts {
			var err error
			if ends[i], err = withdrawal(ctx, tx, t); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		g.log.Error("check access during an SSH Session", "err", err)
	}
	ended := 0
	for i, t := range ts {
		switch {
		case err != nil:
			t.end(checkFailed(t))
			ended++
		case ends[i] != nil:
			t.end(ends[i])
			ended++
		}
	}
	g.log.Debug("access checked again", "ssh_sessions", len(ts), "ended", ended)
}

// withdrawal tells whether the live SSH Session in the Tunnel t may go on:
// whether its caller would be allowed a new one to its Host, which it reads
// by the Host's id, so that a rename changes nothing: the Host and every
// Relay of its chain Managed. A Workspace's SSH Session is checked as its
// User's, whoever opened it, once its opener is found to have taken
// nothing back (workspaceWho). It returns nil when they would, and
// otherwise how the SSH Session ends. A Host the caller can no longer see
// is withdrawn without a word more, so the line tells nothing of where it
// went, and a Relay they cannot follow the links to is "its Relay". A chain
// the record holds broken ends it as a check that failed, with the ids read
// in the log. It fails only on an error of the koko server's own.
func withdrawal(ctx context.Context, tx store.Tx, t *tunnel) (*ending, error) {
	var who access.Identity
	var h host.Reach
	var err error
	if t.session != nil {
		var end *ending
		if who, end, err = workspaceWho(ctx, tx, t); end != nil || err != nil {
			return end, err
		}
		h, err = host.ReachFor(ctx, tx, who, t.hostID)
	} else {
		who, h, err = host.ReachOf(ctx, tx, t.caller, t.hostID)
	}
	if e, ok := errors.AsType[*api.Error](err); ok {
		if e.Code == api.CodeRevoked.Name {
			return revokedEnding(t), nil
		}
		return withdrawn(t, "access withdrawn", ""), nil
	}
	broken, isBroken := errors.AsType[*host.BrokenChainError](err)
	if err != nil && !isBroken {
		return nil, err
	}
	switch h.State {
	case api.HostManaged:
		if isBroken {
			e := checkFailed(t)
			e.reason, e.attrs = "relay chain broken", []any{"read_relay_ids", strings.Join(broken.IDs, ",")}
			return e, nil
		}
		return relayWithdrawal(ctx, tx, t, who, h.Relays)
	case api.HostRetired:
		return withdrawn(t, "host retired", t.hostName+" was Retired"), nil
	}
	return withdrawn(t, "host registered", ""), nil
}

// workspaceWho is who the Workspace's SSH Session in the Tunnel t acts as,
// its User, or how it ends: its User Removed, or its opener taken back, a
// Device Session Revoked or a Sign-in ended in a way that takes back what
// it opened (takesBack), as when it was admitted across that change. An
// opener that signed out or expired takes back nothing.
func workspaceWho(ctx context.Context, tx store.Tx, t *tunnel) (access.Identity, *ending, error) {
	who, err := access.OfUser(ctx, tx, t.userID)
	switch {
	case err != nil:
		return access.Identity{}, nil, err
	case who.User.RemovedAt != "":
		return access.Identity{}, userRemoved(t), nil
	}
	switch c := t.caller; {
	case c.DeviceSession != "":
		found, err := access.DeviceSessions(ctx, tx, "d.id = ?", c.DeviceSession)
		if err != nil {
			return access.Identity{}, nil, err
		}
		if len(found) == 1 && found[0].RevokedAt != "" {
			return access.Identity{}, revokedEnding(t), nil
		}
	case c.SignIn != "":
		found, err := access.SignIns(ctx, tx, op.Now(ctx), "s.id = ?", c.SignIn)
		if err != nil {
			return access.Identity{}, nil, err
		}
		if len(found) == 1 && found[0].EndedAt != "" && takesBack(found[0].EndReason) {
			return access.Identity{}, signInEnding(t, found[0].EndReason), nil
		}
	}
	return who, nil, nil
}

// relayWithdrawal tells whether the live SSH Session in the Tunnel t to a
// Managed Host may go on as far as Relays go: every Relay of the chain it
// passes through, which a change of a Relay since leaves as it was, and of
// the Host's chain now, current, which a new one would pass through, must
// be Managed. The first that is not, in the chain it started through first
// and nearest the koko server first in each, ends it, named only to a
// caller who can follow the links of its chain to it.
func relayWithdrawal(ctx context.Context, tx store.Tx, t *tunnel, who access.Identity, current []*host.Relay) (*ending, error) {
	chains := [][]*host.Relay{current}
	if len(t.relayIDs) > 0 && !slices.Equal(t.relayIDs, host.IDs(current)) {
		started, err := host.RelaysByID(ctx, tx, who, t.relayIDs)
		if err != nil {
			return nil, err
		}
		chains = [][]*host.Relay{started, current}
	}
	for _, chain := range chains {
		for hop, r := range chain {
			if r.State != api.HostManaged {
				e := withdrawn(t, "relay "+r.State, r.Of(t.hostName)+" is no longer Managed")
				e.attrs = relayAttrs(r, hop)
				return e, nil
			}
		}
	}
	return nil, nil
}

// signInEnds are what each way a Sign-in ends, as its record names it
// (api.EndSignedOut, ...), means for the web terminal: reason is the log's,
// for its pages' detach and, when it takes back the SSH Sessions it opened,
// for their end, whose notice says why.
var signInEnds = map[string]struct{ reason, why string }{
	api.EndSignedOut:       {reason: "signed out"},
	api.EndExpired:         {reason: "signin expired"},
	api.EndRevoked:         {reason: "signin revoked", why: "this browser's Sign-in was revoked"},
	api.EndPasswordChanged: {reason: "password changed", why: "the Password was changed"},
	api.EndPasswordRemoved: {reason: "password removed", why: "the Password was removed"},
	api.EndUserRemoved:     {reason: "user removed"},
}

// takesBack tells whether a Sign-in that ended for reason takes back the
// SSH Sessions it opened: every way but signing out and expiring, which
// leave them to its User, so that a way added later takes back until it is
// told otherwise.
func takesBack(reason string) bool {
	return reason != api.EndSignedOut && reason != api.EndExpired
}

// signInReason is the log's reason for a Sign-in that ended for reason.
func signInReason(reason string) string {
	return cmp.Or(signInEnds[reason].reason, "access withdrawn")
}

// signInEnding is how the SSH Session in the Tunnel t ends once the
// Sign-in that opened it has ended for reason, one that takes it back: with
// the notice and the log reason of its own, or as access withdrawn for a
// reason it does not know.
func signInEnding(t *tunnel, reason string) *ending {
	if reason == api.EndUserRemoved {
		return userRemoved(t)
	}
	if e, ok := signInEnds[reason]; ok && e.why != "" {
		return withdrawn(t, e.reason, e.why)
	}
	return withdrawn(t, "access withdrawn", "")
}

// userRemoved ends the SSH Session in the Tunnel t once its User was
// Removed.
func userRemoved(t *tunnel) *ending {
	return withdrawn(t, "user removed", t.user+" was Removed")
}

// errWebURLChanged ends the SSH Session of a Sign-in, which came through the
// web listener, once the Web URL has changed: its page is signed out, its
// cookie the old address's.
var errWebURLChanged = &ending{
	reason: "web url changed",
	notice: "the Web URL changed; this SSH Session is closed. Sign in at the new address.",
	why:    "the Web URL changed",
}

// sweepSignIns checks the Sign-ins of the web terminal's pages every
// timing.SignInCheck until the Gateway closes (checkSignIns): a Sign-in's
// expiry, 30 days from its start or 7 from its last use, is no change any
// call publishes until its cookie comes back.
func (g *Gateway) sweepSignIns() {
	tick := time.NewTicker(g.timings.SignInCheck)
	defer tick.Stop()
	for {
		select {
		case <-g.stopped:
			return
		case <-tick.C:
		}
		g.checkSignIns()
	}
}

// checkSignIns reads the Sign-in of each page attached or on its
// Workspace socket, in one read-only transaction on the koko server's
// clock, and detaches the pages of each that has ended, as its record says
// it ended (signInReason). An expired one is recorded (recordExpiry), so
// that the access-change signal names it: a Sign-in's expiry is no change
// any call makes. It ends no SSH Session, since an expiry takes back
// nothing; every other ending ends what it takes back as the signal names
// it. A read that fails detaches nothing, since it tells nothing, and is
// logged; the next sweep reads again.
func (g *Gateway) checkSignIns() {
	of := map[string]bool{}
	g.mu.Lock()
	for _, a := range g.attachments {
		if a.caller.SignIn != "" {
			of[a.caller.SignIn] = true
		}
	}
	g.mu.Unlock()
	for _, w := range g.workspaces.all() {
		w.mu.Lock()
		for p := range w.pages {
			if p.c.Caller.SignIn != "" {
				of[p.c.Caller.SignIn] = true
			}
		}
		w.mu.Unlock()
	}
	if len(of) == 0 {
		return
	}
	ids, err := json.Marshal(slices.Collect(maps.Keys(of)))
	if err != nil {
		g.log.Error("check the Sign-ins of the web terminal's pages", "err", err)
		return
	}
	ctx := g.readContext(context.Background())
	var found []api.SignIn
	err = g.store.Read(ctx, func(tx store.Tx) (err error) {
		// One argument, a JSON array of the ids as text, holds any number
		// of them.
		found, err = access.SignIns(ctx, tx, op.Now(ctx), "s.id IN (SELECT value FROM json_each(?))", string(ids))
		return err
	})
	if err != nil {
		g.log.Error("check the Sign-ins of the web terminal's pages", "err", err)
		return
	}
	ended := map[string]string{}
	for _, si := range found {
		if si.EndedAt != "" {
			ended[si.ID] = signInReason(si.EndReason)
		}
		if si.EndReason == api.EndExpired {
			g.recordExpiry(si.ID)
		}
	}
	if len(ended) > 0 {
		g.detachPages(func(c op.Caller, _ string) string { return ended[c.SignIn] })
	}
}

// revokedEnding ends the SSH Session in the Tunnel t of a Revoked Device
// Session.
func revokedEnding(t *tunnel) *ending {
	return withdrawn(t, "revoked", "this device's Device Session was Revoked")
}

// withdrawn is how the SSH Session in the Tunnel t ends once its access was
// withdrawn: reason goes to the log, and why, when not empty, into the
// notice. The notice names the Host by the name the SSH Session was opened
// with, so it tells of no later name.
func withdrawn(t *tunnel, reason, why string) *ending {
	what := "access to " + t.hostName + " was withdrawn"
	if why != "" {
		what += ": " + why
	}
	return &ending{reason: reason, notice: what + "; this SSH Session is closed.", why: what}
}

// checkFailed ends the SSH Session in the Tunnel t once the koko server
// could not read the record to check its access again.
func checkFailed(t *tunnel) *ending {
	return &ending{
		reason: "access check failed",
		notice: fmt.Sprintf("the koko server could not check access to %s, so this SSH Session is closed; its log says why.", t.hostName),
		why:    fmt.Sprintf("the koko server could not check access to %s; its log says why", t.hostName),
	}
}
