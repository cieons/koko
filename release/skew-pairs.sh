#!/bin/sh
# skew-pairs.sh prints the pairs of versions the skew check runs for the
# commit checked out (ADR 0049), as the JSON list the skew workflow
# (.github/workflows/skew.yml) takes as its matrix:
#
#   release/skew-pairs.sh
#
# run at the top of a clone that holds every release tag, with gh able to
# list the repository's releases (GH_TOKEN and GH_REPO). A pair is a CLI
# and a kokod, each a release's tag or "this", the commit's own build:
# this CLI with the kokod of the previous release and of compat.Oldest,
# and the CLI of each with this kokod. The previous release is the highest
# release tag, without a pre-release part, in the commit's history, not on
# the commit itself, and whose GitHub Release is published (a release
# still being built has none, nor its assets); compat.Oldest is read from
# its one line in compat.go. When the two are one release, its pairs are
# listed once. A release that is not there yet, as compat.Oldest's on the
# commits that lead to the minor release it names, or is not published
# yet, has no pair, and neither has a previous release below
# compat.Oldest, which this build turns away: a line on stderr says so. A
# failure to list the releases fails the script.
set -eu

fail() {
	echo "skew-pairs: $*" >&2
	exit 1
}

file=internal/shared/compat/compat.go
oldest=$(sed -n 's/^const Oldest = "\(v[0-9]*\.[0-9]*\.[0-9]*\)"$/\1/p' "$file")
[ -n "$oldest" ] || fail "found no compat.Oldest in $file: keep the one line const Oldest = \"vX.Y.Z\""

# A tag is pushed before its release is built, so a release counts once
# its GitHub Release is published, and with it its binaries and image.
published=$(gh release list --exclude-drafts --limit 1000 --json tagName --jq '.[].tagName' 2>&1) ||
	fail "could not read the published releases with gh release list: $published"

# The published releases before this commit's, lowest first.
here=$(git tag --points-at HEAD)
releases=$(git tag --merged HEAD --list 'v*' | grep -v -- - | sort -V | while read -r tag; do
	if printf '%s\n' "$here" | grep -qxF -- "$tag"; then
		continue
	elif printf '%s\n' "$published" | grep -qxF -- "$tag"; then
		echo "$tag"
	else
		echo "skew-pairs: $tag has no published release yet, so it has no pair" >&2
	fi
done)
previous=$(printf '%s\n' "$releases" | tail -n 1)

# released tells whether $1 is one of the releases before this commit's.
released() {
	printf '%s\n' "$releases" | grep -qxF -- "$1"
}

# below tells whether the release $1 is lower than the release $2.
below() {
	[ "$1" != "$2" ] && [ "$(printf '%s\n%s\n' "$1" "$2" | sort -V | head -n 1)" = "$1" ]
}

versions=
if [ -z "$previous" ]; then
	echo "skew-pairs: no release before this commit, so no pair with a previous release" >&2
elif below "$previous" "$oldest"; then
	echo "skew-pairs: the previous release, $previous, is below compat.Oldest ($oldest), so it has no pair" >&2
else
	versions=$previous
fi
if ! released "$oldest"; then
	echo "skew-pairs: compat.Oldest ($oldest) is no published release before this commit, so it has no pair yet" >&2
elif [ "$oldest" != "$versions" ]; then
	versions="$versions $oldest"
fi

pairs=
for version in $versions; do
	pairs="$pairs${pairs:+,}{\"cli\":\"this\",\"kokod\":\"$version\"},{\"cli\":\"$version\",\"kokod\":\"this\"}"
done
echo "[$pairs]"
