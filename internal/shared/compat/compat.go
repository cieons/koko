// Package compat compares koko's versions, as the CLI and the koko server
// name them (spec #136): a release such as v0.4.0 or v0.4.0-rc.1, a
// development build's pseudo-version, which sits just before its next
// release, or "(devel)", taken as the newest.
package compat

import "golang.org/x/mod/semver"

// devel is the version of a build with no version of its own, such as one
// go run makes.
const devel = "(devel)"

// Compare returns -1, 0 or +1 as the version a is older than, the same as,
// or newer than b. A version that is none of koko's forms is older than
// every one that is.
func Compare(a, b string) int {
	switch {
	case a == b:
		return 0
	case a == devel:
		return +1
	case b == devel:
		return -1
	}
	return semver.Compare(a, b)
}

// Newer tells whether the version a is newer than b.
func Newer(a, b string) bool {
	return Compare(a, b) > 0
}
