package gateway

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/cieons/koko/internal/server/access"
	"github.com/cieons/koko/internal/server/host"
	"github.com/cieons/koko/internal/server/op"
	"github.com/cieons/koko/internal/server/store"
	"github.com/cieons/koko/internal/shared/api"
)

// Withdraw ends each live SSH Session that the committed change c withdrew
// access from, since one goes on only while a new one would be allowed (P0
// spec: reach). Each of a Device Session c Revoked, or of a Sign-in it
// ended, ends at once; each to a Host c names, through a Relay when c names
// any Host, or of a caller it narrowed, ends once a read of the record finds
// that a new one would now be refused (recheck). The channel
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
		case t.caller.ID() != "" && slices.Contains(c.Narrowed, t.caller.ID()), slices.Contains(c.Hosts, t.hostID),
			// Any Host c names may be the Relay this SSH Session passes
			// through, or the one its Host has had since, which only a read
			// tells: check every SSH Session through a Relay.
			t.relayID != "" && len(c.Hosts) > 0:
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
// by the Host's id, so that a rename changes nothing: the Host and its
// Relay, if any, Managed. It returns nil when they would, and otherwise how
// the SSH Session ends. A Host the caller can no longer see is withdrawn
// without a word more, so the line tells nothing of where it went, and a
// Relay they cannot see is "its Relay". It fails only on an error of the
// koko server's own.
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
	if err != nil {
		return nil, err
	}
	switch h.State {
	case api.HostManaged:
		return relayWithdrawal(ctx, tx, t, who, h.Relay)
	case api.HostRetired:
		return withdrawn(t, "host retired", t.hostName+" was Retired"), nil
	}
	return withdrawn(t, "host registered", ""), nil
}

// relayWithdrawal tells whether the live SSH Session in the Tunnel t to a
// Managed Host may go on as far as Relays go: both the Relay it passes
// through, which a change of the Host's Relay since leaves as it was, and
// the Host's Relay now, current, which a new one would pass through, must
// be Managed. A Relay that is not ends it, named only to a caller who can
// see it.
func relayWithdrawal(ctx context.Context, tx store.Tx, t *tunnel, who access.Identity, current *host.Relay) (*ending, error) {
	relays := []*host.Relay{current}
	if t.relayID != "" && (current == nil || current.ID != t.relayID) {
		used, err := host.RelayByID(ctx, tx, who, t.relayID)
		if err != nil {
			return nil, err
		}
		relays = append(relays, used)
	}
	for _, r := range relays {
		if r != nil && r.State != api.HostManaged {
			return withdrawn(t, "relay "+r.State, r.Of(t.hostName)+" is no longer Managed"), nil
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
