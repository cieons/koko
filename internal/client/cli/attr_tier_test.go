package cli

import "testing"

// koko attr ls only reads, so an agent's harness allows it by prefix.
func TestAttrCommandsSitInTheirTiers(t *testing.T) {
	t.Parallel()
	var paths []string
	for _, c := range Tree() {
		if c.Noun != "attr" {
			continue
		}
		paths = append(paths, c.Path())
		if c.Path() == "attr ls" && c.Tier != Read {
			t.Errorf("koko attr ls is %s, want %s", c.Tier, Read)
		}
	}
	if len(paths) != 1 || paths[0] != "attr ls" {
		t.Errorf("the attr commands are %v, want attr ls alone", paths)
	}
}
