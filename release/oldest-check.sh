#!/bin/sh
# oldest-check.sh refuses a patch release whose compat.Oldest differs from
# the previous release's (ADR 0049):
#
#   release/oldest-check.sh <tag>
#
# run at the top of a clone that holds the tag and every release tag before
# it. compat.Oldest, the oldest koko the CLI and the koko server of a build
# work with, moves only at a minor or major release (vX.Y.0), whose notes
# say so. A patch release (vX.Y.Z, Z > 0), or a pre-release of one, keeps
# the previous release's: the highest release tag, without a pre-release
# part, below it. A previous release from before the window, which has no
# compat.Oldest, has nothing to compare with. The release workflow
# (.github/workflows/release.yml) runs it before it builds anything.
set -eu

fail() {
	echo "oldest-check: $*" >&2
	exit 1
}

[ $# -eq 1 ] || fail "usage: release/oldest-check.sh <tag>"
tag=$1
file=internal/shared/compat/compat.go

# oldest prints the compat.Oldest of the commit $1 names, from its one line
# in compat.go, which the real-systemd workflow reads the same way. It
# prints nothing for a commit from before the window, whose compat.go,
# if it has one, names no Oldest, and fails for one that names it in a
# form it cannot read: unknown is never "none".
oldest() {
	content=$(git show "$1:$file" 2>/dev/null) || return 0
	version=$(printf '%s\n' "$content" | sed -n 's/^const Oldest = "\(v[0-9]*\.[0-9]*\.[0-9]*\)"$/\1/p')
	if [ -z "$version" ] && printf '%s\n' "$content" | grep -q Oldest; then
		fail "$1's $file names Oldest in a form this check cannot read: keep the one line const Oldest = \"vX.Y.Z\""
	fi
	printf '%s\n' "$version"
}

# The release the tag leads to, without its pre-release part.
core=${tag%%-*}
case $core in
v[0-9]*.[0-9]*.[0-9]*) ;;
*) fail "$tag is not a release's tag, such as v0.4.1" ;;
esac
git rev-parse -q --verify "refs/tags/$tag" >/dev/null || fail "no tag $tag in this clone"
now=$(oldest "$tag")
[ -n "$now" ] || fail "$tag has no compat.Oldest in $file"

case $core in
*.0)
	echo "oldest-check: $tag is not a patch release, so it may move compat.Oldest ($now); its notes say when it does"
	exit 0
	;;
esac

# The highest release tag below the tag's release, by version.
previous=$({
	git tag --list 'v*' | grep -v -- - || true
	echo "$core"
} | sort -u -V | awk -v core="$core" '$0 == core { print p; exit } { p = $0 }')
[ -n "$previous" ] || fail "found no release before $tag to compare compat.Oldest with"
before=$(oldest "$previous")
if [ -z "$before" ]; then
	echo "oldest-check: $previous, the release before $tag, has no compat.Oldest: nothing to compare with"
	exit 0
fi
[ "$now" = "$before" ] ||
	fail "$tag is a patch release, but its compat.Oldest is $now where $previous's is $before: only a minor release moves it"
echo "oldest-check: $tag keeps compat.Oldest $now, as $previous does"
