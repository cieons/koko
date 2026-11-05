package cli

import "testing"

// Each service command sits in the tier P0 spec: the record gives it, so an
// agent's harness allows service ls and service show by prefix and asks
// before service rm.
func TestServiceCommandsSitInTheirTiers(t *testing.T) {
	want := map[string]Tier{
		"service ls":     Read,
		"service show":   Read,
		"service add":    Reversible,
		"service set":    Reversible,
		"service rename": Reversible,
		"service rm":     Irreversible,
	}
	for _, c := range Tree() {
		if c.Noun != "service" {
			continue
		}
		tier, ok := want[c.Path()]
		switch {
		case !ok:
			t.Errorf("koko %s is a service command this test gives no tier", c.Path())
		case c.Tier != tier:
			t.Errorf("koko %s is %s, want %s", c.Path(), c.Tier, tier)
		}
		delete(want, c.Path())
	}
	for path := range want {
		t.Errorf("koko %s is not in the command tree", path)
	}
}
