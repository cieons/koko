// Package hostlist carries the host-list signal: once a change that may
// change the Hosts a User sees commits, it tells the koko server, which
// tells the Device Connections of the Users it concerns
// (wire.RequestHostsChanged), so that each Device Mux rewrites the bare
// names in koko's ssh_config at once (docs/subsystems/device-mux.md, "The
// bare-name refresh"). It names Spaces and Users, not callers: who acts in
// a Space is the access check's to answer, after the change.
package hostlist

import (
	"context"
	"slices"
	"sync"

	"github.com/cieons/koko/internal/server/op"
)

// Change is a committed change to the Hosts some Users see.
type Change struct {
	// Spaces are the ids of the Spaces in which a Host was added, renamed,
	// Retired or deleted, or which a Host left or joined: every User who
	// acts in one of them may see another list.
	Spaces []string
	// Users are the ids of the Users added to a Space or taken out of one.
	Users []string
}

// Signal is the host-list signal. Its zero value has no consumer.
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
