package signin

import (
	"container/list"
	"sync"
	"time"
)

// The rate limits on signing in (ADR 0043).
const (
	// maxFailures is how many failed attempts an address may make within
	// failureWindow, and how many failed sign-ins in a row lock a name.
	maxFailures = 10
	// failureWindow is how long an address's failure counts, and how long
	// a name stays locked.
	failureWindow = 15 * time.Minute
	// limitCapacity bounds how many addresses, and how many names, the
	// limits remember: the most recent ones.
	limitCapacity = 10_000
)

// limiter holds the rate limits in memory, forgotten at restart: for each
// client address, its failed attempts (a wrong sign-in, a refused Password
// Link, a wrong current Password) in the last failureWindow; for each name
// signed in with, whether or not a User holds it, its failed sign-ins in a
// row and, after maxFailures of them, until when it is locked. A lockout
// stops new sign-ins only; Sign-ins already open keep working. An attempt
// counts as a failure while it runs, from its check to its outcome, so that
// attempts made at once cannot pass the limits together.
type limiter struct {
	mu    sync.Mutex
	addrs *recent[[]time.Time]
	names *recent[nameCount]
	// running counts the attempts running from each address, and for each
	// name; an entry goes once its last attempt ends, so it holds no more
	// than the attempts waiting to hash.
	runningAddrs map[string]int
	runningNames map[string]int
}

// nameCount is one name's failed sign-ins in a row, and until when it is
// locked once they reach maxFailures.
type nameCount struct {
	failures int
	locked   time.Time
}

func newLimiter() *limiter {
	return &limiter{
		addrs: newRecent[[]time.Time](limitCapacity), names: newRecent[nameCount](limitCapacity),
		runningAddrs: map[string]int{}, runningNames: map[string]int{},
	}
}

// runningWait is how long an attempt waits that the limits refuse only for
// the attempts running beside it: until they have ended, about the time a
// hash takes.
const runningWait = time.Second

// begin starts, at now, an attempt from addr and, unless name is empty, with
// name, if the limits allow it; else it returns how long the client waits.
// An address is past its limit once its failures in the last failureWindow
// and its attempts running reach maxFailures, and waits until the oldest
// failure is failureWindow old; a name, while it is locked, or once its
// failures in a row and its attempts running reach maxFailures. The caller
// calls end once, with whether the attempt failed, which counts it for addr
// and for name.
func (l *limiter) begin(addr, name string, now time.Time) (wait time.Duration, end func(failed bool)) {
	l.mu.Lock()
	defer l.mu.Unlock()
	failures := live(l.addrs.get(addr), now)
	if len(failures)+l.runningAddrs[addr] >= maxFailures {
		if len(failures) < maxFailures {
			return runningWait, nil
		}
		return failures[0].Add(failureWindow).Sub(now), nil
	}
	if name != "" {
		c := l.names.get(name)
		if now.Before(c.locked) {
			return c.locked.Sub(now), nil
		}
		if c.failures+l.runningNames[name] >= maxFailures {
			return runningWait, nil
		}
		l.runningNames[name]++
	}
	l.runningAddrs[addr]++
	return 0, func(failed bool) {
		l.mu.Lock()
		defer l.mu.Unlock()
		decrement(l.runningAddrs, addr)
		if failed {
			l.failAddress(addr, now)
		}
		if name != "" {
			decrement(l.runningNames, name)
			if failed {
				l.failName(name, now)
			}
		}
	}
}

// decrement takes one from m[key], dropping the entry at 0.
func decrement(m map[string]int, key string) {
	if m[key]--; m[key] <= 0 {
		delete(m, key)
	}
}

// failAddress counts a failed attempt from addr at now; l.mu is held.
func (l *limiter) failAddress(addr string, now time.Time) {
	failures := append(live(l.addrs.get(addr), now), now)
	if len(failures) > maxFailures {
		failures = failures[len(failures)-maxFailures:]
	}
	l.addrs.put(addr, failures)
}

// countNameFailure counts a failure for name at now, outside an attempt
// begin started for it: a wrong current Password, which the name's lockout
// does not stop.
func (l *limiter) countNameFailure(name string, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.failName(name, now)
}

// failName counts a failed sign-in with name at now, locking the name for
// failureWindow once it has failed maxFailures times in a row; l.mu is
// held.
func (l *limiter) failName(name string, now time.Time) {
	c := l.names.get(name)
	c.failures++
	if c.failures >= maxFailures {
		c = nameCount{locked: now.Add(failureWindow)}
	}
	l.names.put(name, c)
}

// resetName starts name's count again, and ends its lockout: after a
// successful sign-in, and once its User sets their Password.
func (l *limiter) resetName(name string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.names.remove(name)
}

// live returns the failures still within failureWindow at now.
func live(failures []time.Time, now time.Time) []time.Time {
	for len(failures) > 0 && !now.Before(failures[0].Add(failureWindow)) {
		failures = failures[1:]
	}
	return failures
}

// recent maps keys to values, keeping only the capacity most recently put.
type recent[V any] struct {
	capacity int
	order    *list.List // of *entry[V], most recent first
	byKey    map[string]*list.Element
}

type entry[V any] struct {
	key   string
	value V
}

func newRecent[V any](capacity int) *recent[V] {
	return &recent[V]{capacity: capacity, order: list.New(), byKey: map[string]*list.Element{}}
}

func (r *recent[V]) get(key string) V {
	if e, ok := r.byKey[key]; ok {
		return e.Value.(*entry[V]).value
	}
	var zero V
	return zero
}

func (r *recent[V]) put(key string, value V) {
	if e, ok := r.byKey[key]; ok {
		e.Value.(*entry[V]).value = value
		r.order.MoveToFront(e)
		return
	}
	r.byKey[key] = r.order.PushFront(&entry[V]{key: key, value: value})
	if r.order.Len() > r.capacity {
		oldest := r.order.Back()
		r.order.Remove(oldest)
		delete(r.byKey, oldest.Value.(*entry[V]).key)
	}
}

func (r *recent[V]) remove(key string) {
	if e, ok := r.byKey[key]; ok {
		r.order.Remove(e)
		delete(r.byKey, key)
	}
}
