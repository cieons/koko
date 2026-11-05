package access

import (
	"context"
	"slices"
	"sync"

	"github.com/cieons/koko/internal/server/op"
)

// Change is a committed change to the record that can withdraw access, as
// the access-change signal carries it: the Device Sessions it affects. A
// later kind of change (a Member taken out of a Space, a Host's access
// withdrawn) adds a field of its own.
type Change struct {
	// Revoked are the ids of the Device Sessions the change Revoked: one
	// revoked by itself, or every one of a User Removed. Their Device
	// Connections close.
	Revoked []string
}

// Signal is the access-change signal (P0 spec: people and Spaces): once a
// change that can withdraw access commits, it tells every consumer, such as
// the transport, which closes the Device Connections of a Revoked Device
// Session. Its zero value has no consumer.
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
