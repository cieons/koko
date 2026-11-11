package compat_test

import (
	"testing"

	"github.com/cieons/koko/internal/shared/compat"
)

// Releases compare by their numbers, a pre-release before its release, a
// pseudo-version just before the release that follows it, and (devel) as
// the newest.
func TestCompareOrdersKokosVersions(t *testing.T) {
	t.Parallel()
	ordered := []string{
		"not-a-version",
		"v0.3.0",
		"v0.3.1",
		"v0.4.0-0.20261111090000-0123456789ab",
		"v0.4.0-rc.1",
		"v0.4.0",
		"v0.4.1-0.20261112090000-0123456789ab+dirty",
		"v0.10.0",
		"v1.0.0",
		"(devel)",
	}
	for i, a := range ordered {
		for j, b := range ordered {
			want := 0
			switch {
			case i < j:
				want = -1
			case i > j:
				want = +1
			}
			if got := compat.Compare(a, b); got != want {
				t.Errorf("Compare(%q, %q) = %d, want %d", a, b, got, want)
			}
			if got := compat.Newer(a, b); got != (want > 0) {
				t.Errorf("Newer(%q, %q) = %t, want %t", a, b, got, want > 0)
			}
		}
	}
}
