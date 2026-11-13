package access

import (
	"context"
	"slices"
	"sync"

	"github.com/cieons/koko/internal/server/op"
)

// Change is a committed change to the record that can withdraw access, as
// the access-change signal carries it: the Device Sessions, the Sign-ins
// and the Hosts it affects. A kind of change that withdraws access some
// other way adds a field of its own.
type Change struct {
	// Revoked are the ids of the Device Sessions the change Revoked: one
	// revoked by itself, or every one of a User Removed. Their SSH Sessions
	// end, then their Device Connections close.
	Revoked []string
	// Removed is the id of the User the change Removed, whose Device
	// Sessions are in Revoked: every SSH Session of theirs ends, whoever
	// opened it, and their Workspace goes.
	Removed string
	// SignIns are the ids of the Sign-ins the change ended, and SignInEnd
	// why, as their records say (api.EndSignedOut, ...): their pages
	// detach, and the SSH Sessions they opened end unless they only signed
	// out or expired.
	SignIns   []string
	SignInEnd string
	// Narrowed are the ids of the Users the change took out of a Space or
	// demoted: each SSH Session of theirs is checked again, whoever opened
	// it.
	Narrowed []string
	// Hosts are the ids of the Hosts the change may have put out of a
	// caller's reach: one moved to another Space, Retired, or sent back to
	// Registered. Each SSH Session to one is checked again.
	Hosts []string
	// WebURL says the change set the Web URL to another address, or
	// cleared it: every SSH Session a Sign-in opened, through the web
	// listener, ends, and every page there detaches, since it is signed
	// out.
	WebURL bool
}

// Signal is the access-change signal (P0 spec: people and Spaces): once a
// change that can withdraw access commits, it tells every consumer, such as
// the koko server's, which has the Gateway end the SSH Sessions it withdrew
// access from and the transport close a Revoked Device Session's Device
// Connections. Its zero value has no consumer.
type Signal struct {
	mu        sync.Mutex
	consumers []func(Change)
}

// Subscribe has f called with every change once it has committed. f runs on
// the call that made the change, before that call answers, so it must not
// block.
func (s *Signal) Subscribe(f func(Change)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.consumers = append(s.consumers, f)
}

// Publish has c reach every consumer once the transaction of the call ctx
// belongs to has committed, and none when it rolls back. It panics outside
// an operation's call, which only a bug reaches.
func (s *Signal) Publish(ctx context.Context, c Change) {
	op.AfterTx(ctx, func(committed bool) {
		if !committed {
			return
		}
		s.mu.Lock()
		consumers := slices.Clone(s.consumers)
		s.mu.Unlock()
		for _, f := range consumers {
			f(c)
		}
	})
}
