package cli

import "testing"

// Each space command sits in the tier P0 spec: people and Spaces gives it,
// so an agent's harness allows space ls and space show by prefix.
func TestSpaceCommandsSitInTheirTiers(t *testing.T) {
	want := map[string]Tier{
		"space ls":         Read,
		"space show":       Read,
		"space add":        Reversible,
		"space rename":     Reversible,
		"space member add": Reversible,
		"space member rm":  Reversible,
		"space rm":         Irreversible,
	}
	for _, c := range Tree() {
		if c.Noun != "space" && c.Noun != "space member" {
			continue
		}
		tier, ok := want[c.Path()]
		switch {
		case !ok:
			t.Errorf("koko %s is a space command the spec gives no tier", c.Path())
		case c.Tier != tier:
			t.Errorf("koko %s is %s, want %s", c.Path(), c.Tier, tier)
		}
		delete(want, c.Path())
	}
	for path := range want {
		t.Errorf("koko %s is not in the command tree", path)
	}
}
