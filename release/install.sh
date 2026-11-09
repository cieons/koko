#!/bin/sh
# install.sh installs koko, the CLI, as /usr/local/bin/koko on this Linux or
# macOS machine, or upgrades it, and installs or upgrades the koko server
# first when asked for or already there:
#
#   curl -fsSL https://github.com/cieons/koko/releases/latest/download/install.sh | sh
#   curl -fsSL https://github.com/cieons/koko/releases/latest/download/install.sh | sh -s -- --owner alice
#
# It downloads the koko binary for this machine from the latest release, or
# the release KOKO_VERSION names (such as v0.3.0), and checks it against that
# release's SHA256SUMS. With arguments, or with none on a machine whose
# systemd knows koko.service, it runs that binary's `koko server install` as
# root with them, and stops there if that fails. Then it puts the binary in
# place as /usr/local/bin/koko. It checks only that the binary can run here;
# every decision about a koko server's machine is koko server install's.
# Nothing here prompts.
#
# Everything sits in main, run on the last line, so that a download cut short
# runs nothing.

set -eu

repo=https://github.com/cieons/koko
bin=/usr/local/bin

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

# sha256 prints the SHA-256 of the file $1, with sha256sum, or with
# shasum -a 256 where that is missing, as on macOS.
sha256() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1"
	else
		shasum -a 256 "$1"
	fi
}

main() {
	os=$(uname -s)
	machine=$(uname -m)
	case $os/$machine in
	Linux/x86_64 | Linux/amd64) goos=linux arch=amd64 ;;
	Linux/aarch64 | Linux/arm64) goos=linux arch=arm64 ;;
	Darwin/arm64) goos=darwin arch=arm64 ;;
	Linux/*) fail "koko runs on Linux on amd64 and arm64, and this machine is $machine; nothing was written" ;;
	Darwin/*) fail "koko runs on macOS on Apple silicon (arm64), and this Mac is $machine; nothing was written" ;;
	*) fail "koko runs on Linux and macOS, and this machine runs $os; nothing was written" ;;
	esac
	asset=koko-$goos-$arch

	# install.sh writes $bin and runs koko server install as root. sudo gets
	# -n, so that it fails rather than ask for a password.
	if [ "$(id -u)" = 0 ]; then
		as_root=
	elif command -v sudo >/dev/null 2>&1 && sudo -n true 2>/dev/null; then
		as_root='sudo -n'
	else
		hint="curl -fsSL $repo/releases/latest/download/install.sh | sudo sh"
		[ $# = 0 ] || hint="$hint -s -- <the same arguments>"
		fail "install.sh installs koko as root, and this user is not root and cannot use sudo without a password: run it as root, such as with $hint; nothing was written"
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

	printf 'install.sh: downloading koko %s for %s/%s\n' "$tag" "$goos" "$arch" >&2
	download "$repo/releases/download/$tag/SHA256SUMS" "$tmp/SHA256SUMS"
	download "$repo/releases/download/$tag/$asset" "$tmp/$asset"
	want=$(awk -v f="$asset" '$2 == f || $2 == "*" f { print $1; exit }' "$tmp/SHA256SUMS")
	[ -n "$want" ] || fail "the SHA256SUMS of koko $tag lists no $asset, so it was not run"
	got=$(sha256 "$tmp/$asset") || fail "could not compute the SHA-256 of $asset, so it was not run"
	got=${got%% *}
	[ "$got" = "$want" ] ||
		fail "$asset does not match the SHA256SUMS of koko $tag: the download is damaged or was swapped, so it was not run"
	chmod 0755 "$tmp/$asset"

	# The koko server first, when the arguments ask for it or systemd knows
	# one, so that an upgrade upgrades both. Every release's koko-<os>-<arch>
	# has koko server install; one from before kokod, v0.1.0 or v0.2.0, is
	# the whole koko server and installs it by itself. $as_root is unquoted
	# to split into sudo and -n. stdin is /dev/null: under curl | sh it is
	# the rest of this script.
	if [ $# -gt 0 ] || systemctl cat koko.service >/dev/null 2>&1; then
		$as_root "$tmp/$asset" server install "$@" </dev/null ||
			fail "koko server install failed, so koko at $bin/koko was left as it was" "$?"
	fi

	# The CLI last, written beside $bin/koko and renamed into place: a koko
	# running from the old file, such as a Device Mux, keeps it, none is seen
	# half written, and a v0.1.0 or v0.2.0 koko server, whose unit ran
	# $bin/koko, has had its unit rewritten by now. The file has one name, so
	# a run cut short leaves at most one, which the next run replaces.
	$as_root mkdir -p -m 0755 "$bin" &&
		$as_root install -m 0755 "$tmp/$asset" "$bin/.koko.new" &&
		$as_root mv -f "$bin/.koko.new" "$bin/koko" ||
		fail "could not write $bin/koko"
	printf 'install.sh: installed koko %s at %s/koko\n' "$tag" "$bin" >&2
	# Under curl | sh, sh would read on from stdin, which may stay open.
	exit 0
}

main "$@"
