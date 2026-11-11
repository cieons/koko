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

// A CLI states its version in its SSH version string, and the koko server
// reads it back; a string that states none, as every CLI before the window
// sent, is v0.3.0.
func TestTheSSHVersionStringCarriesTheCLIsVersion(t *testing.T) {
	t.Parallel()
	for _, version := range []string{"v0.4.0", "v0.4.0-rc.1", "v0.4.1-0.20261112090000-0123456789ab+dirty", "(devel)"} {
		s := compat.SSHVersion(version)
		if want := "SSH-2.0-koko " + version; s != want {
			t.Errorf("SSHVersion(%q) = %q, want %q", version, s, want)
		}
		if got := compat.CLIVersion(s); got != version {
			t.Errorf("CLIVersion(%q) = %q, want %q", s, got, version)
		}
	}
	for _, s := range []string{"SSH-2.0-koko", "SSH-2.0-OpenSSH_9.6", "SSH-2.0-kokoX v9.0.0", ""} {
		if got := compat.CLIVersion(s); got != "v0.3.0" {
			t.Errorf("CLIVersion(%q) = %q, want v0.3.0, the version of a CLI that states none", s, got)
		}
	}
}
