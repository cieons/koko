package build

import "testing"

// A release is a build at its tag: a version with an optional pre-release
// part; a pseudo-version, +dirty, (devel) and no version are not.
func TestRelease(t *testing.T) {
	for version, want := range map[string]bool{
		"v0.3.0":                               true,
		"v12.0.1":                              true,
		"v0.3.0-rc.1":                          true,
		"(devel)":                              false,
		"unknown":                              false,
		"":                                     false,
		"v0.3.0+dirty":                         false,
		"v0.3.0-rc.1+dirty":                    false,
		"0.3.0":                                false,
		"v0.3":                                 false,
		"v0.0.0-20261109080000-0123456789ab":   false,
		"v0.3.1-0.20261109080000-0123456789ab": false,
		"v0.3.0-rc.1.0.20261109080000-0123456789ab":  false,
		"v0.3.1-0.20261109080000-0123456789ab+dirty": false,
	} {
		if got := (Info{Version: version}).Release(); got != want {
			t.Errorf("Info{Version: %q}.Release() = %v, want %v", version, got, want)
		}
	}
}
