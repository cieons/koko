package host

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/cieons/koko/internal/server/access"
	"github.com/cieons/koko/internal/server/store"
	"github.com/cieons/koko/internal/shared/api"
)

// A Host's chain is every Relay koko passes through to reach it, in the
// order it dials them: each Host's own relay, followed from the Host (ADR
// 0055). The rules of relayFor keep every chain the record holds free of
// loops and at most MaxRelays long; one walk reads a chain for them, for
// ReachOf and for Enrolled.

// MaxRelays is the most Relays koko passes through to reach a Host.
const MaxRelays = 4

// BrokenChainError is a chain of Relays that loops, or whose last Relay
// within MaxRelays has a Relay of its own: the record holds one only if it
// was changed behind koko's back, since the rules never write one. Reading
// it fails, so it is never taken for no Relay.
type BrokenChainError struct {
	// HostID is the Host whose chain it is.
	HostID string
	// IDs are the Relays read, from the Host's own Relay on; the last of
	// them is the one that broke it.
	IDs []string
}

func (e *BrokenChainError) Error() string {
	return fmt.Sprintf("the chain of Relays of %s is broken: it loops or holds more than %d Relays (%s)",
		e.HostID, MaxRelays, strings.Join(e.IDs, ", "))
}

// walk follows the links from the Host whose id is hostID, whose own Relay's
// id is first, "" for none, by relayOf, which returns the id of the Relay of
// the Host whose id it is given, "" for none. It follows at most MaxRelays
// links and returns the ids of the Relays in dial order, the Host's own
// Relay last; a chain that loops or goes on past MaxRelays is a
// *BrokenChainError.
func walk(hostID, first string, relayOf func(id string) (string, error)) ([]string, error) {
	var ids []string
	for id := first; id != ""; {
		ids = append(ids, id)
		if len(ids) > MaxRelays || id == hostID || slices.Contains(ids[:len(ids)-1], id) {
			return nil, &BrokenChainError{HostID: hostID, IDs: ids}
		}
		next, err := relayOf(id)
		if err != nil {
			return nil, err
		}
		id = next
	}
	slices.Reverse(ids)
	return ids, nil
}

// chain reads the Relays of the Host h, as stored, in dial order.
func chain(ctx context.Context, tx store.Tx, h stored) ([]stored, error) {
	read := map[string]stored{}
	ids, err := walk(h.ID, h.relayID().String, func(id string) (string, error) {
		r, err := scan(tx.QueryRowContext(ctx, selectHosts+" WHERE h.id = ?", id))
		if err != nil {
			return "", fmt.Errorf("read the Relay %s: %w", id, err)
		}
		read[id] = r
		return r.relayID().String, nil
	})
	if err != nil {
		return nil, fmt.Errorf("read the chain of Relays of %s: %w", h.Name, err)
	}
	relays := make([]stored, len(ids))
	for i, id := range ids {
		relays[i] = read[id]
	}
	return relays, nil
}

// seeChain returns relays, a chain in dial order, as the caller who sees it:
// each Relay is Seen only when who can see it and every Relay between it
// and the Host, which is how far the links show it to them.
func seeChain(ctx context.Context, tx store.Tx, who access.Identity, relays []stored) ([]*Relay, error) {
	if len(relays) == 0 {
		return nil, nil
	}
	scope, err := who.Spaces(ctx, tx)
	if err != nil {
		return nil, err
	}
	seen := make([]*Relay, len(relays))
	follows := true
	for i, r := range slices.Backward(relays) {
		follows = follows && scope.Has(r.spaceID)
		seen[i] = &Relay{Reach: r.reach(), Seen: follows}
	}
	return seen, nil
}

// links are every Host's link to its Relay, by id, read once before a write
// that sets a Relay, so that the rules check each Host of a batch against
// the record as it was, whatever the batch's earlier Hosts wrote.
type links map[string]link

// link is a Host's link: the id of its Relay, "" for none, and whether the
// Host is Retired.
type link struct {
	relay   string
	retired bool
}

// readLinks reads the links of every Host.
func readLinks(ctx context.Context, tx store.Tx) (links, error) {
	rows, err := tx.QueryContext(ctx, "SELECT id, coalesce(relay_id, ''), state FROM hosts")
	if err != nil {
		return nil, fmt.Errorf("read the Relays of the Hosts: %w", err)
	}
	defer func() { _ = rows.Close() }()
	l := links{}
	for rows.Next() {
		var id, relay, state string
		if err := rows.Scan(&id, &relay, &state); err != nil {
			return nil, fmt.Errorf("read the Relays of the Hosts: %w", err)
		}
		l[id] = link{relay: relay, retired: state == api.HostRetired}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read the Relays of the Hosts: %w", err)
	}
	return l, nil
}

// chain returns the ids of the Relays of the Host whose id is hostID, in
// dial order.
func (l links) chain(hostID string) ([]string, error) {
	return walk(hostID, l[hostID].relay, func(id string) (string, error) { return l[id].relay, nil })
}

// deepest returns how far behind the Host whose id is hostID the deepest
// Host that is not Retired sits: 1 for one whose Relay it is, 2 for one
// behind that, and 0 for none. It leaves out the Hosts of batch, which the
// write gives another Relay, with every Host behind them, and looks no
// further than most+1, which is enough to tell more than most, so a loop
// the record should never hold cannot keep it going.
func (l links) deepest(hostID string, most int, batch []string) int {
	depth := 0
	level := []string{hostID}
	for d := 1; d <= most+1 && len(level) > 0; d++ {
		var next []string
		for id, k := range l {
			if slices.Contains(level, k.relay) && !slices.Contains(batch, id) {
				next = append(next, id)
				if !k.retired {
					depth = d
				}
			}
		}
		level = next
	}
	return depth
}
