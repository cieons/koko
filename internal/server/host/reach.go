package host

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/cieons/koko/internal/server/access"
	"github.com/cieons/koko/internal/server/op"
	"github.com/cieons/koko/internal/server/store"
	"github.com/cieons/koko/internal/shared/api"
)

// What the record offers reach (P0 spec: the record): a read of what
// logging in to a Host needs, and the Registered to Managed transition that
// Enroll makes.

// Reach is what logging in to a Host needs, as the record holds it.
type Reach struct {
	ID   string
	Name string
	// Address and Port are where its sshd listens.
	Address string
	Port    int
	// Account is the Host Account.
	Account string
	// State is api.HostRegistered, api.HostManaged or api.HostRetired.
	State string
	// Pin is the pinned host key, or nil for a Host with no pin.
	Pin ssh.PublicKey
	// Relays are its chain: the Relays koko passes through to reach it, in
	// the order it dials them, its own Relay last; none for a Host without
	// one.
	Relays []*Relay
}

// Relay is what passing through a Relay of a Host's chain needs: what
// logging in to the Relay needs, its own Relays left out, and whether the
// caller can follow the links to it, seeing it and every Relay between it
// and the Host, since only then may a message name it; otherwise it is "its
// Relay". The caller need not see it to pass through it (ADR 0055).
type Relay struct {
	Reach
	Seen bool
}

// ReachOf answers who is calling and returns what logging in to the Host
// ref names needs, by its name or its id, with its chain's, whose Relays
// the caller need not see. A Host the caller cannot act on is refused
// exactly as one that does not exist, with api.CodeUnknownHost. A chain the
// record holds broken fails with a *BrokenChainError, returned with who
// and the Host without its chain, so the refusal can name it.
func ReachOf(ctx context.Context, tx store.Tx, caller op.Caller, ref string) (access.Identity, Reach, error) {
	who, h, err := target(ctx, tx, caller, ref)
	if err != nil {
		return access.Identity{}, Reach{}, err
	}
	r := h.reach()
	relays, err := chain(ctx, tx, h)
	if _, broken := errors.AsType[*BrokenChainError](err); broken {
		return who, r, err
	}
	if err != nil {
		return access.Identity{}, Reach{}, err
	}
	if r.Relays, err = seeChain(ctx, tx, who, relays); err != nil {
		return access.Identity{}, Reach{}, err
	}
	return who, r, nil
}

// RelaysByID returns what passing through the Hosts whose ids are ids, a
// chain in dial order, needs, whatever their states, and how far who can
// follow its links. The Gateway reads by it the chain a live SSH Session
// passes through, which may no longer be its Host's.
func RelaysByID(ctx context.Context, tx store.Tx, who access.Identity, ids []string) ([]*Relay, error) {
	relays := make([]stored, len(ids))
	for i, id := range ids {
		var err error
		if relays[i], err = scan(tx.QueryRowContext(ctx, selectHosts+" WHERE h.id = ?", id)); err != nil {
			return nil, fmt.Errorf("read the Relay %s: %w", id, err)
		}
	}
	return seeChain(ctx, tx, who, relays)
}

func (h stored) reach() Reach {
	return Reach{ID: h.ID, Name: h.Name, Address: h.Address, Port: h.Port, Account: h.Account, State: h.State, Pin: h.pin}
}

// Of is how a message to the caller names r as the Relay of the Host named
// hostName: "web1's Relay web2", or "web1's Relay" for a Relay they cannot
// see, so the message tells nothing of a Host outside their Spaces.
func (r *Relay) Of(hostName string) string {
	if !r.Seen {
		return hostName + "'s Relay"
	}
	return hostName + "'s Relay " + r.Name
}

// IDs are the ids of the Relays of a chain, in its order.
func IDs(relays []*Relay) []string {
	ids := make([]string, len(relays))
	for i, r := range relays {
		ids[i] = r.ID
	}
	return ids
}

// Enrolled records the Host Enroll logged in to, as ReachOf read it before
// the login, as Managed with the host key pin pinned, and returns it. Since
// the login ran outside any transaction, it refuses with
// api.CodeHostChanged a Host whose Host Account, state, pin or chain of
// Relays has changed since: the koko key went to the Host Account it logged
// in as, the pin it checked was the one it read, and the Host it reached
// was the one behind that chain, compared by ids in order; a chain broken
// since is one that changed. A new address or port is no matter, as the pin
// proves the Host wherever it answers.
func Enrolled(ctx context.Context, tx store.Tx, caller op.Caller, was Reach, pin ssh.PublicKey) (access.Identity, api.Host, error) {
	who, h, err := target(ctx, tx, caller, was.ID)
	if err != nil {
		return access.Identity{}, api.Host{}, err
	}
	relays, err := chain(ctx, tx, h)
	_, broken := errors.AsType[*BrokenChainError](err)
	if err != nil && !broken {
		return access.Identity{}, api.Host{}, err
	}
	now := make([]string, len(relays))
	for i, r := range relays {
		now[i] = r.ID
	}
	if broken || h.Account != was.Account || h.State != was.State || !samePin(h.pin, was.Pin) || !slices.Equal(now, IDs(was.Relays)) {
		return access.Identity{}, api.Host{}, api.CodeHostChanged.Errorf(
			"%s changed while koko logged in to it to Enroll it (its Host Account, its state, its pinned host key or its Relay), so koko recorded nothing; the koko key may now be in the authorized_keys of %q there. Run koko host enroll %s again, or delete that line",
			h.Name, was.Account, h.Name)
	}
	_, err = tx.ExecContext(ctx, "UPDATE hosts SET state = ?, host_key = ?, updated_at = ? WHERE id = ?",
		api.HostManaged, pin.Marshal(), op.Now(ctx).Format(time.RFC3339), h.ID)
	if err != nil {
		return access.Identity{}, api.Host{}, fmt.Errorf("record %s as Managed: %w", h.Name, err)
	}
	enrolled, err := find(ctx, tx, who, h.ID)
	return who, enrolled.Host, err
}

// samePin tells whether a and b pin the same host key, or both none.
func samePin(a, b ssh.PublicKey) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return bytes.Equal(a.Marshal(), b.Marshal())
}
