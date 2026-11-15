// Package flow holds the short-lived flows of signing in that a browser
// begins and answers in two requests, such as a passkey's ceremony (#228,
// "Challenges"): each in memory under a random 128-bit id the page posts
// back, taken once, expiring after its lifetime, and bounded per client
// address and in all, so that no one host can evict everyone's flows. A
// restart forgets them all, and the page starts again.
package flow

import (
	"container/list"
	"crypto/rand"
	"encoding/base64"
	"net/netip"
	"slices"
	"sync"
	"time"
)

// The bounds on the flows held.
const (
	// PerAddress is how many flows one client address holds at most; one
	// more drops that address's oldest. Every address of one IPv6 /64
	// counts as one, since one host commonly holds the /64 whole.
	PerAddress = 20
	// Total is how many flows are held at most in all; one more drops the
	// oldest.
	Total = 10_000
)

// Store holds flows of type V: what a flow's answer is checked against,
// such as its purpose and challenge.
type Store[V any] struct {
	lifetime time.Duration
	clock    func() time.Time

	mu sync.Mutex
	// order holds every flow, the oldest at the front; since every flow
	// has the same lifetime, the oldest expires first.
	order *list.List // of *entry[V]
	byID  map[string]*list.Element
	// byAddress holds the ids of each address's flows, the oldest first.
	byAddress map[string][]string
}

type entry[V any] struct {
	id, address string
	began       time.Time
	value       V
}

// New returns a store of flows that expire lifetime after they begin, on
// clock, the koko server's.
func New[V any](lifetime time.Duration, clock func() time.Time) *Store[V] {
	return &Store[V]{lifetime: lifetime, clock: clock, order: list.New(), byID: map[string]*list.Element{}, byAddress: map[string][]string{}}
}

// Begin holds v as a new flow begun from the client address, and returns
// its id, which is secret to the page that began it: 128 random bits in
// base64url.
func (s *Store[V]) Begin(address string, v V) string {
	var b [16]byte
	_, _ = rand.Read(b[:]) // crypto/rand.Read never fails
	id := base64.RawURLEncoding.EncodeToString(b[:])
	key := addressKey(address)
	now := s.clock()

	s.mu.Lock()
	defer s.mu.Unlock()
	s.expire(now)
	if held := s.byAddress[key]; len(held) >= PerAddress {
		s.drop(s.byID[held[0]])
	}
	if s.order.Len() >= Total {
		s.drop(s.order.Front())
	}
	s.byID[id] = s.order.PushBack(&entry[V]{id: id, address: key, began: now, value: v})
	s.byAddress[key] = append(s.byAddress[key], id)
	return id
}

// Take returns the flow id names and forgets it, so that no later answer
// finds it; a flow expired, dropped or never begun is none.
func (s *Store[V]) Take(id string) (V, bool) {
	now := s.clock()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expire(now)
	el, ok := s.byID[id]
	if !ok {
		var zero V
		return zero, false
	}
	v := el.Value.(*entry[V]).value
	s.drop(el)
	return v, true
}

// expire drops every flow whose lifetime has passed at now; s.mu is held.
func (s *Store[V]) expire(now time.Time) {
	for el := s.order.Front(); el != nil && !now.Before(el.Value.(*entry[V]).began.Add(s.lifetime)); el = s.order.Front() {
		s.drop(el)
	}
}

// drop forgets the flow el holds; s.mu is held.
func (s *Store[V]) drop(el *list.Element) {
	e := s.order.Remove(el).(*entry[V])
	delete(s.byID, e.id)
	held := slices.DeleteFunc(s.byAddress[e.address], func(id string) bool { return id == e.id })
	if len(held) == 0 {
		delete(s.byAddress, e.address)
		return
	}
	s.byAddress[e.address] = held
}

// addressKey is what a client address counts as: an IPv6 address by its
// /64, any other as itself.
func addressKey(address string) string {
	a, err := netip.ParseAddr(address)
	if err != nil {
		return address
	}
	if a = a.Unmap(); !a.Is6() {
		return a.String()
	}
	p, _ := a.WithZone("").Prefix(64) // an IPv6 address always has a /64
	return p.String()
}
