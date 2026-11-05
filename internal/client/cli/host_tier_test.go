package cli

import "testing"

// Each host command sits in the tier P0 spec: the record gives it, so an
// agent's harness allows host ls and host show by prefix and asks before
// host rm.
func TestHostCommandsSitInTheirTiers(t *testing.T) {
	want := map[string]Tier{
		"host ls":     Read,
		"host show":   Read,
		"host add":    Reversible,
		"host set":    Reversible,
		"host rename": Reversible,
		"host move":   Reversible,
		"host rm":     Irreversible,
	}
	for _, c := range Tree() {
		if c.Noun != "host" {
			continue
		}
		tier, ok := want[c.Path()]
		switch {
		case !ok:
			t.Errorf("koko %s is a host command this test gives no tier", c.Path())
		case c.Tier != tier:
			t.Errorf("koko %s is %s, want %s", c.Path(), c.Tier, tier)
		}
		delete(want, c.Path())
	}
	for path := range want {
		t.Errorf("koko %s is not in the command tree", path)
	}
}
