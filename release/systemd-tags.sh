#!/bin/sh
# systemd-tags.sh chooses the older build of the real-systemd check and
# the tags of its two builds (docs/subsystems/release.md), as the lines
# the systemd workflow (.github/workflows/systemd.yml) adds to
# $GITHUB_ENV:
#
#   release/systemd-tags.sh
#
# run at the top of a clone that holds the whole history and every tag.
# The older build is the commit just before the one that added the latest
# migration step, so that the upgrade from it migrates and snapshots.
# Go stamps a build with the highest tag on its commit, so both tags are
# pre-releases of the patch after the higher of compat.Oldest and the
# highest tag in the older commit's history, its own included:
# -check.1 for the older build and -check.2 for this commit's. Then a
# release tag on the older commit is below -check.1, and this commit's
# build is always the newer: on a release's own tag, which is above every
# tag before it, the release's tag wins and is newer still. Both are
# within the window, at or above compat.Oldest (ADR 0049).
set -eu

fail() {
	echo "systemd-tags: $*" >&2
	exit 1
}

file=internal/shared/compat/compat.go
oldest=$(sed -n 's/^const Oldest = "\(v[0-9]*\.[0-9]*\.[0-9]*\)"$/\1/p' "$file")
[ -n "$oldest" ] || fail "found no compat.Oldest in $file: keep the one line const Oldest = \"vX.Y.Z\""

added=$(git log -1 --format=%H --diff-filter=A -- internal/server/store/migrations)
[ -n "$added" ] || fail "no commit added a migration step, so no older build to upgrade from"
older=$(git rev-parse "$added^")

# The highest release among compat.Oldest and the tags in the older
# commit's history, each without its pre-release part.
base=$({
	echo "$oldest"
	git tag --merged "$older" --list 'v*' | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+(-.*)?$' | sed 's/-.*//'
} | sort -V | tail -n 1)
next=${base%.*}.$((${base##*.} + 1))

echo "OLDER_COMMIT=$older"
echo "OLDER_TAG=$next-check.1"
echo "NEWER_TAG=$next-check.2"
