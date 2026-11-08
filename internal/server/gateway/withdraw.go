package gateway

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/cieons/koko/internal/server/access"
	"github.com/cieons/koko/internal/server/host"
	"github.com/cieons/koko/internal/server/store"
	"github.com/cieons/koko/internal/shared/api"
)

// Withdraw ends each live SSH Session that the committed change c withdrew
// access from, since one goes on only while a new one would be allowed (P0
// spec: reach). Each of a Device Session c Revoked ends at once; each to a
// Host c names, or of a Device Session it narrowed, ends once a read of the
// record finds that a new one would now be refused (recheck). The channel
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
		case slices.Contains(c.Narrowed, t.caller.DeviceSession), slices.Contains(c.Hosts, t.hostID):
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
	ctx := context.Background()
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
			t.end(&ending{
				reason: "access check failed",
				notice: fmt.Sprintf("the koko server could not check access to %s, so this SSH Session is closed; its log says why.", t.hostName),
			})
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
// by the Host's id, so that a rename changes nothing. It returns nil when
// they would, and otherwise how the SSH Session ends. A Host the caller can
// no longer see is withdrawn without a word more, so the line tells nothing
// of where it went. It fails only on an error of the koko server's own.
func withdrawal(ctx context.Context, tx store.Tx, t *tunnel) (*ending, error) {
	_, h, err := host.ReachOf(ctx, tx, t.caller, t.hostID)
	if e, ok := errors.AsType[*api.Error](err); ok {
		if e.Code == api.CodeRevoked.Name {
			return revokedEnding(t), nil
		}
		return withdrawn(t, "access withdrawn", ""), nil
	}
	if err != nil {
		return nil, err
	}
	switch h.State {
	case api.HostManaged:
		return nil, nil
	case api.HostRetired:
		return withdrawn(t, "host retired", t.hostName+" was Retired"), nil
	}
	return withdrawn(t, "host registered", ""), nil
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
	notice := "access to " + t.hostName + " was withdrawn"
	if why != "" {
		notice += ": " + why
	}
	return &ending{reason: reason, notice: notice + "; this SSH Session is closed."}
}
