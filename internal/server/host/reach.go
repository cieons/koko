package host

import (
	"bytes"
	"context"
	"fmt"
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
}

// ReachOf answers who is calling and returns what logging in to the Host
// ref names needs, by its name or its id. A Host the caller cannot act on
// is refused exactly as one that does not exist, with api.CodeUnknownHost.
func ReachOf(ctx context.Context, tx store.Tx, caller op.Caller, ref string) (access.Identity, Reach, error) {
	who, h, err := target(ctx, tx, caller, ref)
	if err != nil {
		return access.Identity{}, Reach{}, err
	}
	return who, h.reach(), nil
}

func (h stored) reach() Reach {
	return Reach{ID: h.ID, Name: h.Name, Address: h.Address, Port: h.Port, Account: h.Account, State: h.State, Pin: h.pin}
}

// Enrolled records the Host Enroll logged in to, as ReachOf read it before
// the login, as Managed with the host key pin pinned, and returns it. Since
// the login ran outside any transaction, it refuses with
// api.CodeHostChanged a Host whose Host Account, state or pin has changed
// since: the koko key went to the Host Account it logged in as, and the pin
// it checked was the one it read. A new address or port is no matter, as
// the pin proves the Host wherever it answers.
func Enrolled(ctx context.Context, tx store.Tx, caller op.Caller, was Reach, pin ssh.PublicKey) (access.Identity, api.Host, error) {
	who, h, err := target(ctx, tx, caller, was.ID)
	if err != nil {
		return access.Identity{}, api.Host{}, err
	}
	if h.Account != was.Account || h.State != was.State || !samePin(h.pin, was.Pin) {
		return access.Identity{}, api.Host{}, api.CodeHostChanged.Errorf(
			"%s changed while koko logged in to it to Enroll it (its Host Account, its state or its pinned host key), so koko recorded nothing; the koko key may now be in the authorized_keys of %q there. Run koko host enroll %s again, or delete that line",
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
