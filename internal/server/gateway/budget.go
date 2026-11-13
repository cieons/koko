package gateway

import (
	"context"
	"sync"
)

// vtBudget bounds the memory the emulators of every web terminal's SSH
// Session hold together, across all Users: each User's are bounded by the
// 16 tabs of their Workspace, and a few Users at that worst could
// otherwise have the koko server killed for running out of memory, which
// would end every User's shells. Once a call passes the budget, the
// largest emulator fails, and its SSH Session goes on with a new one
// (termSession.failEmulator), rather than any tab being refused or any
// shell ended.
type vtBudget struct {
	// limit is the budget in bytes; 0 means none.
	limit int

	mu    sync.Mutex
	held  map[*termSession]budgetEntry
	total int
}

// budgetEntry is what one SSH Session's emulator held when last measured,
// and how to fail that emulator.
type budgetEntry struct {
	n    int
	fail context.CancelCauseFunc
}

// budgetFloor is the least an emulator must hold to be failed for the
// budget: one below it holds little more than the new one that would take
// its place, about 3 MiB, so failing it frees nothing.
const budgetFloor = 8 << 20

func newBudget(limit int) *vtBudget {
	return &vtBudget{limit: limit, held: map[*termSession]budgetEntry{}}
}

// note records that the emulator of s, which fail fails, holds n bytes
// now, and returns how to fail the emulator to fail when the emulators
// together hold more than the budget: the one that held most when last
// measured, which is then counted as holding nothing, so no other call
// fails it again. It returns nil while they hold less, or when none holds
// budgetFloor.
func (b *vtBudget) note(s *termSession, n int, fail context.CancelCauseFunc) context.CancelCauseFunc {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.total += n - b.held[s].n
	b.held[s] = budgetEntry{n: n, fail: fail}
	if b.limit <= 0 || b.total <= b.limit {
		return nil
	}
	var largest *termSession
	for t, e := range b.held {
		if e.n >= budgetFloor && (largest == nil || e.n > b.held[largest].n) {
			largest = t
		}
	}
	if largest == nil {
		return nil
	}
	e := b.held[largest]
	b.total -= e.n
	b.held[largest] = budgetEntry{}
	return e.fail
}

// forget drops what the emulator of s held, once it is closed.
func (b *vtBudget) forget(s *termSession) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.total -= b.held[s].n
	delete(b.held, s)
}
