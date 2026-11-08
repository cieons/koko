#!/bin/sh
# install.sh installs the koko server on this Linux machine, or upgrades it:
#
#   curl -fsSL https://github.com/cieons/koko/releases/latest/download/install.sh | sh -s -- --owner alice
#
# It downloads the koko binary for this machine from the latest release, or
# the release KOKO_VERSION names (such as v0.3.0), checks it against that
# release's SHA256SUMS, and runs its `koko server install` as root with this
# script's arguments. It checks only that the binary can run here; every
# other decision about the machine is the binary's. Nothing here prompts.
#
# Everything sits in main, run on the last line, so that a download cut short
# runs nothing.

set -eu

repo=https://github.com/cieons/koko

# fail stops the script, saying why on stderr, with the exit status $2: 1,
# or 2 for a value given wrong, as koko's own commands do.
fail() {
	printf 'install.sh: %s\n' "$1" >&2
	exit "${2:-1}"
}

# is_tag says whether $1 is a release's tag, such as v0.3.0 or v0.3.0-rc.1.
is_tag() {
	case $1 in
	v[0-9]*) case $1 in *[!0-9A-Za-z.+-]*) return 1 ;; esac ;;
	*) return 1 ;;
	esac
}

# download writes the file at URL $1 to $2, over HTTPS only.
download() {
	curl --proto =https --tlsv1.2 -fsSL -o "$2" "$1" || fail "could not download $1"
}

main() {
	os=$(uname -s)
	[ "$os" = Linux ] ||
		fail "the koko server runs only on Linux with systemd, and this machine runs $os; nothing was written"
	# systemctl prints "offline" when systemd is not the init (PID 1), as in
	# most containers, and nothing in a chroot: no unit could start there.
	# The image runs the koko server in a container instead.
	state=$(systemctl is-system-running 2>/dev/null) || :
	case $state in
	'' | offline) fail "the koko server runs only on Linux with systemd, and systemd is not running this machine: to run it in a container, use the image ghcr.io/cieons/koko ($repo/blob/main/docs/install.md#in-a-container); nothing was written" ;;
	esac
	machine=$(uname -m)
	case $machine in
	x86_64 | amd64) arch=amd64 ;;
	aarch64 | arm64) arch=arm64 ;;
	*) fail "the koko server runs on amd64 and arm64, and this machine is $machine; nothing was written" ;;
	esac
	asset=koko-linux-$arch

	# koko server install runs as root. sudo gets -n, so that it fails rather
	# than ask for a password.
	if [ "$(id -u)" = 0 ]; then
		as_root=
	elif command -v sudo >/dev/null 2>&1 && sudo -n true 2>/dev/null; then
		as_root='sudo -n'
	else
		fail "koko server install runs as root, and this user is not root and cannot use sudo without a password: run it as root, such as with curl -fsSL $repo/releases/latest/download/install.sh | sudo sh -s -- <the same arguments>; nothing was written"
	fi

	if [ -n "${KOKO_VERSION:-}" ]; then
		tag=v${KOKO_VERSION#v}
		is_tag "$tag" || fail "KOKO_VERSION=$KOKO_VERSION is not a koko version, such as v0.3.0" 2
	else
		# GitHub redirects /releases/latest to the latest release's tag page,
		# through the repository's new path if it moved. It is read once, so
		# the binary and SHA256SUMS come from one release.
		page=$(curl --proto =https --tlsv1.2 -fsSIL -o /dev/null -w '%{url_effective}' "$repo/releases/latest") ||
			fail "could not find koko's latest release at $repo/releases/latest"
		case $page in
		*/releases/tag/*) tag=${page##*/releases/tag/} ;;
		*) tag= ;;
		esac
		is_tag "$tag" || fail "found no koko release at $repo/releases/latest"
	fi

	tmp=$(mktemp -d) || fail "could not make a temporary directory"
	trap 'rm -rf "$tmp"' EXIT
	trap 'exit 1' HUP INT TERM

	printf 'install.sh: downloading koko %s for linux/%s\n' "$tag" "$arch" >&2
	download "$repo/releases/download/$tag/SHA256SUMS" "$tmp/SHA256SUMS"
	download "$repo/releases/download/$tag/$asset" "$tmp/$asset"
	want=$(awk -v f="$asset" '$2 == f || $2 == "*" f { print $1; exit }' "$tmp/SHA256SUMS")
	[ -n "$want" ] || fail "the SHA256SUMS of koko $tag lists no $asset, so it was not run"
	got=$(sha256sum "$tmp/$asset") || fail "could not compute the SHA-256 of $asset, so it was not run"
	got=${got%% *}
	[ "$got" = "$want" ] ||
		fail "$asset does not match the SHA256SUMS of koko $tag: the download is damaged or was swapped, so it was not run"
	chmod 0755 "$tmp/$asset"

	status=0
	# $as_root is unquoted to split into sudo and -n. stdin is /dev/null:
	# under curl | sh it is the rest of this script.
	$as_root "$tmp/$asset" server install "$@" </dev/null || status=$?
	exit "$status"
}

main "$@"
