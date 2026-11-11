// Package compat holds the window of versions within which the CLI and the
// koko server work together (ADR 0049), and compares koko's versions, as
// the CLI and the koko server name them (spec #136): a release such as
// v0.4.0 or v0.4.0-rc.1, a development build's pseudo-version, which sits
// just before its next release, or "(devel)", taken as the newest.
package compat

import (
	"strings"

	"golang.org/x/mod/semver"

	"github.com/cieons/koko/internal/shared/wire"
)

// Oldest is the oldest koko that the CLI and the koko server of this build
// work with, in both directions: the koko server turns away a CLI below it,
// and the CLI a koko server below it. It moves only at a minor release,
// whose notes say so; the release job refuses a patch release that moves
// it (release/oldest-check.sh reads this line).
const Oldest = "v0.3.0"

// unstated is the version of a CLI whose SSH version string states none:
// every CLI before the window sent wire.Version alone.
const unstated = "v0.3.0"

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

// SSHVersion returns the SSH version string a CLI of version sends the
// koko server: wire.Version with the version as a comment, as RFC 4253
// allows, such as "SSH-2.0-koko v0.4.0". It goes out before encryption, as
// OpenSSH's does.
func SSHVersion(version string) string {
	return wire.Version + " " + version
}

// CLIVersion returns the version a client's SSH version string states, as
// SSHVersion wrote it, or v0.3.0 for a string that states none.
func CLIVersion(sshVersion string) string {
	if v, ok := strings.CutPrefix(sshVersion, wire.Version+" "); ok && v != "" {
		return v
	}
	return unstated
}
