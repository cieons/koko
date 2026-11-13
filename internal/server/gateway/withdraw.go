package gateway

import (
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
// spec: reach). Each of a Device Session c Revoked, or of a Sign-in it
// ended, ends at once, and so does each of a Sign-in when c changed the
// Web URL; each, when c names any Host, or each of a caller it narrowed,
// ends once a read of the record finds that a new one would now be refused
// (recheck). The channel
// it returns is closed once the Revoked Device Sessions' SSH Sessions have
// given their notice and ended, so that their Device Connections close only
// after. It never blocks, since the access-change signal calls it before
// the call that made the change answers.
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
		switch {
		case t.hostID == "":
			// Refused: its client's "none" gets the banner.
		case slices.Contains(c.Revoked, t.caller.DeviceSession):
			if c.Removed {
				t.end(withdrawn(t, "user removed", t.user+" was Removed"))
			} else {
				t.end(revokedEnding(t))
			}
			revoked = append(revoked, t.done)
		case t.caller.SignIn != "" && slices.Contains(c.SignIns, t.caller.SignIn):
			t.end(signInEnding(t, c.SignInEnd))
		case t.caller.SignIn != "" && c.WebURL:
			t.end(errWebURLChanged)
		case t.caller.ID() != "" && slices.Contains(c.Narrowed, t.caller.ID()),
			// Any Host c names may be the Relay this SSH Session passes
			// through, or the one its Host has had since, even one that
			// started with none, which only a read tells: check every SSH
			// Session.
			len(c.Hosts) > 0:
			recheck = append(recheck, t)
		}
	}
	if len(recheck) > 0 {
		g.wg.Go(func() { g.recheck(recheck) })
	}
	g.wg.Go(func() {
		for _, done := range revoked {
			<-done
		}
		close(told)
	})
	return told
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
// Relay of its chain Managed. It returns nil when they would, and otherwise
// how the SSH Session ends. A Host the caller can no longer see is withdrawn
// without a word more, so the line tells nothing of where it went, and a
// Relay they cannot follow the links to is "its Relay". A chain the record
// holds broken ends it as a check that failed, with the ids read in the
// log. It fails only on an error of the koko server's own.
func withdrawal(ctx context.Context, tx store.Tx, t *tunnel) (*ending, error) {
	who, h, err := host.ReachOf(ctx, tx, t.caller, t.hostID)
	if e, ok := errors.AsType[*api.Error](err); ok {
		switch e.Code {
		case api.CodeRevoked.Name:
			return revokedEnding(t), nil
		case api.CodeSignInRequired.Name:
			return signInWithdrawal(ctx, tx, t)
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

// signInWithdrawal is how the SSH Session in the Tunnel t ends once its
// Sign-in has ended: as the Sign-in's record says it ended, read at the
// time of ctx, so that a check that finds it ended, after an access change
// or before a resume, ends it as the signal would have.
func signInWithdrawal(ctx context.Context, tx store.Tx, t *tunnel) (*ending, error) {
	found, err := access.SignIns(ctx, tx, op.Now(ctx), "s.id = ?", t.caller.SignIn)
	if err != nil {
		return nil, err
	}
	reason := ""
	if len(found) == 1 {
		reason = found[0].EndReason
	}
	return signInEnding(t, reason), nil
}

// signInEnding is how the SSH Session in the Tunnel t ends once its
// Sign-in has ended for reason, as the Sign-in's record names it
// (api.EndSignedOut, ...): each with the notice and the log reason of its
// own, and any other reason as access withdrawn.
func signInEnding(t *tunnel, reason string) *ending {
	switch reason {
	case api.EndSignedOut:
		return &ending{reason: "signed out", notice: "this browser signed out; this SSH Session is closed.", why: "this browser signed out"}
	case api.EndRevoked:
		return withdrawn(t, "signin revoked", "this browser's Sign-in was revoked")
	case api.EndPasswordChanged:
		return withdrawn(t, "password changed", "the Password was changed")
	case api.EndPasswordRemoved:
		return withdrawn(t, "password removed", "the Password was removed")
	case api.EndUserRemoved:
		return withdrawn(t, "user removed", t.user+" was Removed")
	case api.EndExpired:
		return withdrawn(t, "signin expired", "this browser's Sign-in has expired")
	}
	return withdrawn(t, "access withdrawn", "")
}

// errWebURLChanged ends the SSH Session of a Sign-in, which came through the
// web listener, once the Web URL has changed: its page is signed out, its
// cookie the old address's.
var errWebURLChanged = &ending{
	reason: "web url changed",
	notice: "the Web URL changed; this SSH Session is closed. Sign in at the new address.",
	why:    "the Web URL changed",
}

// sweepSignIns checks the Sign-ins of the web terminals every
// timing.SignInCheck until the Gateway closes (checkSignIns): a Sign-in's
// expiry, 30 days from its start or 7 from its last use, is no change any
// call publishes.
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

// checkSignIns reads the Sign-in of each web terminal in the registry, in
// one read-only transaction on the koko server's clock, and ends the SSH
// Sessions of each that has ended, as its record says it ended
// (signInEnding): one that expired, or ended by a change the Gateway
// somehow missed. A read that fails ends nothing, since it tells nothing,
// and is logged; the next sweep reads again.
func (g *Gateway) checkSignIns() {
	g.mu.Lock()
	of := map[string][]*tunnel{}
	for _, t := range g.tunnels {
		if t.caller.SignIn != "" && t.hostID != "" {
			of[t.caller.SignIn] = append(of[t.caller.SignIn], t)
		}
	}
	g.mu.Unlock()
	if len(of) == 0 {
		return
	}
	ids, err := json.Marshal(slices.Collect(maps.Keys(of)))
	if err != nil {
		g.log.Error("check the Sign-ins of the web terminals", "err", err)
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
		g.log.Error("check the Sign-ins of the web terminals", "err", err)
		return
	}
	for _, si := range found {
		if si.EndedAt == "" {
			continue
		}
		for _, t := range of[si.ID] {
			t.end(signInEnding(t, si.EndReason))
		}
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
