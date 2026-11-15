package flow_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/cieons/koko/internal/server/flow"
)

// fakeClock is a clock a test moves by hand.
type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time { return c.now }

func newFlows(lifetime time.Duration) (*flow.Store[string], *fakeClock) {
	c := &fakeClock{now: time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)}
	return flow.New[string](lifetime, c.Now), c
}

// A flow is taken once: the first take answers it, and every later one
// finds none.
func TestAFlowIsTakenOnce(t *testing.T) {
	t.Parallel()
	flows, _ := newFlows(5 * time.Minute)
	id := flows.Begin("192.0.2.1", "signin")
	if len(id) != 22 {
		t.Errorf("the flow id is %q, want 128 bits in base64url, 22 characters", id)
	}
	if v, ok := flows.Take(id); !ok || v != "signin" {
		t.Fatalf("the first take answered %q %v, want the flow", v, ok)
	}
	if _, ok := flows.Take(id); ok {
		t.Error("a second take found the flow, want none")
	}
	if _, ok := flows.Take("no-such-flow"); ok {
		t.Error("a take of an id no flow holds found one")
	}
}

// A flow expires once its lifetime has passed since it began, and is taken
// until then.
func TestAFlowExpires(t *testing.T) {
	t.Parallel()
	flows, clock := newFlows(5 * time.Minute)
	kept := flows.Begin("192.0.2.1", "kept")
	expired := flows.Begin("192.0.2.1", "expired")
	clock.now = clock.now.Add(5*time.Minute - time.Second)
	if _, ok := flows.Take(kept); !ok {
		t.Error("a flow 1 second short of its lifetime was not taken")
	}
	clock.now = clock.now.Add(time.Second)
	if _, ok := flows.Take(expired); ok {
		t.Error("a flow at its lifetime was taken, want it expired")
	}
}

// An address holds at most flow.PerAddress flows: one more drops its
// oldest, and no other address's. Every address of one IPv6 /64 counts as
// one.
func TestAnAddressHoldsALimitedNumberOfFlows(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name         string
		first, other func(i int) string
	}{
		{"IPv4", func(int) string { return "192.0.2.1" }, func(int) string { return "192.0.2.2" }},
		{
			"IPv6 /64",
			func(i int) string { return fmt.Sprintf("2001:db8:1:2::%x", i+1) },
			func(int) string { return "2001:db8:1:3::1" },
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			flows, _ := newFlows(5 * time.Minute)
			other := flows.Begin(tc.other(0), "other")
			var ids []string
			for i := range flow.PerAddress + 1 {
				ids = append(ids, flows.Begin(tc.first(i), fmt.Sprint(i)))
			}
			if _, ok := flows.Take(ids[0]); ok {
				t.Errorf("the oldest of %d flows from one address was kept, want it dropped", flow.PerAddress+1)
			}
			for i, id := range ids[1:] {
				if v, ok := flows.Take(id); !ok || v != fmt.Sprint(i+1) {
					t.Errorf("flow %d answered %q %v, want it kept", i+1, v, ok)
				}
			}
			if _, ok := flows.Take(other); !ok {
				t.Error("another address's flow was dropped")
			}
		})
	}
}

// Every address but those of one IPv6 /64 counts apart: an IPv4 address,
// and IPv6 addresses of two /64s.
func TestAddressesOfAnotherNetworkCountApart(t *testing.T) {
	t.Parallel()
	flows, _ := newFlows(5 * time.Minute)
	var ids []string
	for i := range flow.PerAddress {
		ids = append(ids, flows.Begin(fmt.Sprintf("2001:db8:1:%x::1", i), fmt.Sprint(i)))
	}
	ids = append(ids, flows.Begin("2001:db8:1:ff::1", "last"))
	for _, id := range ids {
		if _, ok := flows.Take(id); !ok {
			t.Fatal("a flow of another /64 was dropped")
		}
	}
}

// At most flow.Total flows are held in all: one more drops the oldest flow
// of the address holding the most, the oldest of all among addresses that
// hold one each.
func TestAtMostTotalFlowsAreHeld(t *testing.T) {
	t.Parallel()
	flows, _ := newFlows(5 * time.Minute)
	var ids []string
	for i := range flow.Total + 1 {
		ids = append(ids, flows.Begin(fmt.Sprintf("10.%d.%d.%d", i>>16&255, i>>8&255, i&255), ""))
	}
	if _, ok := flows.Take(ids[0]); ok {
		t.Errorf("the oldest of %d flows was kept, want it dropped", flow.Total+1)
	}
	if _, ok := flows.Take(ids[1]); !ok {
		t.Error("the second oldest flow was dropped, want only the oldest")
	}
	if _, ok := flows.Take(ids[len(ids)-1]); !ok {
		t.Error("the newest flow was dropped")
	}
}

// Addresses that fill the store evict their own flows first: a flow begun
// before them, from an address holding one, outlasts the store filled
// many times over from the 65,536 /64s of one /48.
func TestFloodingAddressesEvictTheirOwnFlowsFirst(t *testing.T) {
	t.Parallel()
	flows, _ := newFlows(5 * time.Minute)
	victim := flows.Begin("192.0.2.1", "victim")
	for i := range 3 * flow.Total {
		flows.Begin(fmt.Sprintf("2001:db8:1:%x::1", i%2000), "")
	}
	if v, ok := flows.Take(victim); !ok || v != "victim" {
		t.Error("the flood evicted a flow of an address that held one")
	}
}
