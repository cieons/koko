// Package compat holds the window of versions within which the CLI and the
// koko server work together (ADR 0049), and compares koko's versions, as
// the CLI and the koko server name them (spec #136): a release such as
// v0.4.0 or v0.4.0-rc.1, a development build's pseudo-version, which sits
// just before its next release, or "(devel)", taken as the newest.
package compat

import (
	"cmp"
	"strings"

	"golang.org/x/mod/module"
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
// or newer than b. A development build that names no release before it,
// as one built from a clone without tags (v0.0.0-<time>-<revision>), is
// newer than every release, as "(devel)" is, so that no build without a
// release's version falls below the window; "(devel)" is the newest. A
// version that is none of koko's forms is older than every one that is.
func Compare(a, b string) int {
	if a == b {
		return 0
	}
	if c := cmp.Compare(rank(a), rank(b)); c != 0 {
		return c
	}
	return semver.Compare(a, b)
}

// rank places a version among the three ranks Compare orders first: 2 for
// "(devel)", 1 for a pseudo-version that names no release before it, and
// 0 for every other, which compare by semver. A build with changes
// (+dirty) ranks as its pseudo-version does.
func rank(v string) int {
	if v == devel {
		return 2
	}
	pseudo, _, _ := strings.Cut(v, "+")
	if module.IsPseudoVersion(pseudo) {
		if base, err := module.PseudoVersionBase(pseudo); err == nil && base == "" {
			return 1
		}
	}
	return 0
}

// Valid tells whether v is one of koko's versions: a release, a
// pseudo-version or "(devel)". Any other, such as "unknown" from a build
// with no build information, cannot be placed in the window.
func Valid(v string) bool {
	return v == devel || semver.IsValid(v)
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
// SSHVersion wrote it, or v0.3.0 for a string that states none, or none
// that is Valid.
func CLIVersion(sshVersion string) string {
	if v, ok := strings.CutPrefix(sshVersion, wire.Version+" "); ok && Valid(v) {
		return v
	}
	return unstated
}
