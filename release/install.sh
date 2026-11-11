#!/bin/sh
# install.sh installs koko, the CLI, on this Linux or macOS machine, or
# upgrades it: as ~/.local/bin/koko for the user who runs it, or as
# /usr/local/bin/koko for every user when root runs it. It installs or
# upgrades the koko server first, as root, when asked for or already there:
#
#   curl -fsSL https://github.com/cieons/koko/releases/latest/download/install.sh | sh
#   curl -fsSL https://github.com/cieons/koko/releases/latest/download/install.sh | sudo sh -s -- --owner alice
#
# It downloads the koko binary for this machine from the latest release, or
# the release KOKO_VERSION names (such as v0.3.0), and checks it against that
# release's SHA256SUMS. With arguments, or with none on a machine whose
# systemd knows koko.service, it runs that binary's `koko server install` as
# root with them, and stops there if that fails. Then it puts the binary in
# place, and says what the user must do about PATH or an older koko first on
# it. It checks only that the binary can run here; every decision about a
# koko server's machine is koko server install's. Nothing here prompts, and
# install.sh itself writes nothing outside the CLI's directory: the user's
# shell files stay theirs.
#
# Everything sits in main, run on the last line, so that a download cut short
# runs nothing.

set -eu

repo=https://github.com/cieons/koko
line="curl -fsSL $repo/releases/latest/download/install.sh"
# root_bin is where root's install puts koko, for every user of the machine.
root_bin=/usr/local/bin

# say says $1 on stderr, as install.sh.
say() {
	printf 'install.sh: %s\n' "$1" >&2
}

# fail stops the script, saying why on stderr, with the exit status $2: 1,
# or 2 for a value given wrong, as koko's own commands do.
fail() {
	say "$1"
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

# advise_path says, when $bin is not on PATH, the line that puts it there
# for the user's shell, read from SHELL, and the full path to run koko by
# until a new shell starts. It edits no file: the user's shell files are
# theirs.
advise_path() {
	case :$PATH: in
	*:"$bin":* | *:"$bin/":*) return 0 ;;
	esac
	# The lines name the directory as the shell will, $HOME and ~ unexpanded.
	if [ "$bin" = "$root_bin" ]; then
		dir=$root_bin fish_dir=$root_bin
	else
		# shellcheck disable=SC2016,SC2088
		dir='$HOME/.local/bin' fish_dir='~/.local/bin'
	fi
	export_line="export PATH=\"$dir:\$PATH\""
	case ${SHELL:-} in
	*/zsh) how=run add="echo '$export_line' >> ~/.zprofile" ;;
	*/bash)
		if [ "$goos" = darwin ]; then
			add="echo '$export_line' >> ~/.bash_profile"
		else
			add="echo '$export_line' >> ~/.bashrc"
		fi
		how=run
		;;
	*/fish) how=run add="fish_add_path $fish_dir" ;;
	*) how="add this line to the shell's profile" add=$export_line ;;
	esac
	say "$bin is not on PATH; to put it there for new shells, $how:"
	printf '  %s\n' "$add" >&2
	say "until a new shell starts, run koko as $bin/koko"
}

# advise_other_koko names the koko a new shell would run when it is not the
# one just installed. A root-owned /usr/local/bin/koko there is what an
# earlier install line left as root, and install.sh gives the line that
# removes it, but never removes it: other users of the machine may run it.
# On a koko server's machine, that file is the koko server's CLI, so
# install.sh says to keep it instead.
advise_other_koko() {
	other=$(command -v koko 2>/dev/null) || return 0
	# A PATH entry with a trailing / gives dir//koko in dash.
	case $other in "$bin/koko" | "$bin//koko") return 0 ;; esac
	say "koko on PATH is $other, not the new $bin/koko"
	[ "$other" = "$root_bin/koko" ] || return 0
	owner=$(ls -ldn "$other" 2>/dev/null | awk '{ print $3 }')
	[ "$owner" = 0 ] || return 0
	if [ -n "$server_here" ]; then
		say "$other is the CLI of this machine's koko server: keep it, and upgrade both with $line | sudo sh"
		return 0
	fi
	say "an earlier install line put $other there as root; once no other user of this machine runs it, and a koko command has run with the new koko, remove it with:"
	printf '  %s\n' "sudo rm $other" >&2
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

	# The CLI goes where its user can write it: root's serves every user of
	# the machine, and anyone else's is their own, in their home.
	if [ "$(id -u)" = 0 ]; then
		root=1 bin=$root_bin
	else
		root=
		[ -n "${HOME:-}" ] ||
			fail "install.sh installs koko for this user in \$HOME/.local/bin, and HOME is not set; nothing was written"
		[ -d "$HOME" ] && [ -O "$HOME" ] ||
			fail "install.sh installs koko for this user in \$HOME/.local/bin, and HOME=$HOME is not a directory this user owns; nothing was written"
		bin=$HOME/.local/bin
	fi

	# The koko server is part of this run when the arguments ask for it or
	# systemd knows one, and that part runs as root. sudo gets -n, so that
	# it fails rather than ask for a password. Without arguments, a user
	# who cannot be root still gets the CLI, and the koko server is left to
	# koko server upgrade. server_here says this machine runs, or is about
	# to run, a koko server, whether or not this run installs it.
	server= server_here=
	if [ $# -gt 0 ] || systemctl cat koko.service >/dev/null 2>&1; then
		server=1 server_here=1
		if [ -n "$root" ]; then
			as_root=
		elif command -v sudo >/dev/null 2>&1 && sudo -n true 2>/dev/null; then
			as_root='sudo -n'
		elif [ $# -gt 0 ]; then
			fail "install.sh installs the koko server as root, and this user is not root and cannot use sudo without a password: run it as root, such as with $line | sudo sh -s -- <the same arguments>; nothing was written"
		else
			server=
			say "this machine runs a koko server, which only root can upgrade from here, so it was left as it is: upgrade it with koko server upgrade, or with $line | sudo sh"
		fi
	fi

	pin=
	if [ -n "${KOKO_VERSION:-}" ]; then
		tag=v${KOKO_VERSION#v}
		is_tag "$tag" || fail "KOKO_VERSION=$KOKO_VERSION is not a koko version, such as v0.3.0" 2
		# sudo drops KOKO_VERSION from the environment, so it is passed on
		# by name: koko server install reads it as a going back asked for.
		pin=KOKO_VERSION=$tag
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

	# The koko server first, so that an upgrade upgrades both. Every
	# release's koko-<os>-<arch> has koko server install; one from before
	# kokod, v0.1.0 or v0.2.0, is the whole koko server and installs it by
	# itself. $as_root is unquoted to split into sudo and -n, and $pin to
	# nothing when empty. stdin is /dev/null: under curl | sh it is the rest
	# of this script.
	if [ -n "$server" ]; then
		# shellcheck disable=SC2086
		$as_root env $pin "$tmp/$asset" server install "$@" </dev/null ||
			fail "koko server install failed, so koko at $bin/koko was left as it was" "$?"
	fi

	# The CLI last, written beside $bin/koko by its user and renamed into
	# place: a koko running from the old file, such as a Device Mux, keeps
	# it, none is seen half written, and a v0.1.0 or v0.2.0 koko server,
	# whose unit ran /usr/local/bin/koko, has had its unit rewritten by now.
	# The file has one name, so a run cut short leaves at most one, which
	# the next run replaces.
	{
		mkdir -p -m 0755 "$bin" &&
			install -m 0755 "$tmp/$asset" "$bin/.koko.new" &&
			mv -f "$bin/.koko.new" "$bin/koko"
	} || fail "could not write $bin/koko"
	printf 'install.sh: installed koko %s at %s/koko\n' "$tag" "$bin" >&2
	advise_path
	advise_other_koko
	# Under curl | sh, sh would read on from stdin, which may stay open.
	exit 0
}

main "$@"
