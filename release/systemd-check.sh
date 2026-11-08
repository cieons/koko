#!/bin/sh
# systemd-check.sh installs koko on this machine as its Owner would, through
# install.sh, and checks each step on the machine's real systemd:
#
#   release/systemd-check.sh <older dist> <newer dist>
#
# Each dist is a directory `make dist` wrote. The older build must be at an
# older schema than the newer one, so that the upgrade migrates and leaves a
# snapshot. The real-systemd workflow (.github/workflows/systemd.yml) runs it
# on a fresh runner.
#
# In this order: a binary that does not match SHA256SUMS is never run; the
# older build installs, listens on 443 as the koko user with the Root Key
# through LoadCredential=, and a device joins with the printed Join Code and
# makes an API call; the newer build upgrades it, restarting the service and
# leaving the snapshot, and the device still works; uninstall keeps the data
# directory, the Root Key and the koko user; and install, run again on that
# data directory put back by root, starts the koko server with its record.
# On the way it checks what only a real systemd shows: the Environment
# systemctl show quotes, and koko server install refusing a container that
# systemd does not run.
#
# install.sh downloads from GitHub, and runs here unchanged: a fake curl
# first on its PATH serves a fake GitHub from a directory, where release
# v0.0.1 is the older build, v0.0.2 the newer one, and v0.0.3 the newer one
# with a binary that does not match its SHA256SUMS. The device is a Docker
# container on the machine's network, so that it reaches the koko server
# over TCP, as a device does, and not through the Unix socket.
#
# Run it only on a throwaway Linux machine with systemd, Docker, jq, ss and
# passwordless sudo, where koko was never installed: it installs koko as
# root on port 443, and leaves its data, its Root Key and its user behind.

set -eu

fail() {
	printf '\nsystemd-check: FAIL: %s\n' "$1" >&2
	exit 1
}

step() {
	printf '\n=== %s\n' "$1"
}

[ $# -eq 2 ] || {
	echo 'usage: release/systemd-check.sh <older dist> <newer dist>' >&2
	exit 2
}
older=$(cd "$1" && pwd)
newer=$(cd "$2" && pwd)

case $(uname -m) in
x86_64) arch=amd64 ;;
aarch64) arch=arm64 ;;
*) fail "koko builds for amd64 and arm64, and this machine is $(uname -m)" ;;
esac
asset=koko-linux-$arch

for path in /etc/koko /var/lib/koko /etc/systemd/system/koko.service /usr/local/bin/koko; do
	[ ! -e "$path" ] || fail "$path is there: run this only on a machine koko was never installed on"
done
! id koko >/dev/null 2>&1 || fail "the koko user is there: run this only on a machine koko was never installed on"

work=$(mktemp -d)
device=koko-systemd-check-device
image=ubuntu:24.04
github=$work/github
mkdir "$work/bin"
docker pull -q "$image" >/dev/null

# On a failure, show what the koko server and the device logged.
on_exit() {
	status=$?
	if [ "$status" != 0 ]; then
		step "after the failure: the koko service and its journal"
		systemctl status --no-pager --lines=0 koko.service || :
		sudo journalctl --no-pager -u koko.service || :
		step "after the failure: the device's Device Mux log"
		docker exec "$device" sh -c 'cat /tmp/koko-0/*.log' || :
	fi
	docker rm -f "$device" >/dev/null 2>&1 || :
	exit "$status"
}
trap on_exit EXIT

# The fake curl serves https://github.com/cieons/koko from $github, as
# install.sh asks for it: releases/latest redirects to the tag that
# $github/latest names, and releases/download/<tag>/<file> is
# $github/releases/download/<tag>/<file>.
{
	printf "#!/bin/sh\ngithub='%s'\n" "$github"
	cat <<'EOF'
out= format= url=
while [ $# -gt 0 ]; do
	case $1 in
	-o) out=$2; shift ;;
	-w) format=$2; shift ;;
	--proto) shift ;;
	-*) ;;
	*) url=$1 ;;
	esac
	shift
done
repo=https://github.com/cieons/koko
case $url in
"$repo/releases/latest")
	[ "$format" = '%{url_effective}' ] || { echo "fake curl: $url without -w %{url_effective}" >&2; exit 2; }
	printf '%s' "$repo/releases/tag/$(cat "$github/latest")"
	;;
"$repo/releases/download/"*)
	file=$github/${url#"$repo/"}
	[ -f "$file" ] || { echo "curl: (22) The requested URL returned error: 404" >&2; exit 22; }
	cp "$file" "$out"
	;;
*) echo "fake curl: the fake GitHub has no $url" >&2; exit 22 ;;
esac
EOF
} >"$work/bin/curl"
chmod 0755 "$work/bin/curl"

# release publishes the build in dist $2 as the release tagged $1: its
# SHA256SUMS and the binary for this machine, all install.sh downloads.
release() {
	mkdir -p "$github/releases/download/$1"
	cp "$2/$asset" "$2/SHA256SUMS" "$github/releases/download/$1/"
}
release v0.0.1 "$older"
release v0.0.2 "$newer"
release v0.0.3 "$newer"

# install_sh runs the install.sh of dist $1 as root, as
# `curl … | sudo sh -s -- <args>` does: with sudo's PATH, the fake curl first,
# and the variables before -- in its environment. Its stdout goes to
# $work/out and its stderr to $work/err.
install_sh() {
	dist=$1
	shift
	set -- env PATH="$work/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin" "$@"
	# The redirects are this script's own files, as with curl and a terminal.
	# shellcheck disable=SC2024
	sudo "$@" <"$dist/install.sh" >"$work/out" 2>"$work/err"
}

# install_ok runs install_sh with its arguments, shows its stderr, and fails
# unless it succeeds.
install_ok() {
	status=0
	install_sh "$@" || status=$?
	cat "$work/err" >&2
	[ "$status" = 0 ] || fail "install.sh exited $status"
}

# check_running checks that the koko service runs as the koko user, listens
# on 443 and holds the Root Key through LoadCredential=, and sets pid to its
# process.
check_running() {
	systemctl is-active --quiet koko.service || fail "the koko service is not active"
	pid=$(systemctl show --property=MainPID --value koko.service)
	[ "$pid" != 0 ] || fail "the koko service has no process"
	user=$(ps -o user= -p "$pid")
	[ "$user" = koko ] || fail "the koko server runs as $user, not koko"
	sudo ss -Hltnp 'sport = :443' | grep -q "pid=$pid," ||
		fail "the koko server (pid $pid) does not listen on 443: $(sudo ss -Hltnp 'sport = :443')"
	# systemctl show prints LoadCredential as [unprintable], so ask systemd
	# over D-Bus.
	credential=$(busctl get-property org.freedesktop.systemd1 /org/freedesktop/systemd1/unit/koko_2eservice \
		org.freedesktop.systemd1.Service LoadCredential)
	[ "$credential" = 'a(ss) 1 "root-key" "/etc/koko/root.key"' ] ||
		fail "the koko service's LoadCredential is $credential, not root-key:/etc/koko/root.key"
	sudo test -f /run/credentials/koko.service/root-key ||
		fail "systemd holds no root-key credential for the koko service"
	readable=$(sudo -u koko sh -c 'if test -r /etc/koko/root.key; then echo yes; else echo no; fi')
	[ "$readable" = no ] || fail "the koko user can read /etc/koko/root.key"
}

# device_calls makes one API call from the device, user ls, and checks that
# it lists alice as the Owner.
device_calls() {
	docker exec "$device" koko user ls --json >"$work/users" ||
		fail "the device's koko user ls failed"
	jq -e 'any(.[]; .name == "alice" and .role == "owner")' "$work/users" >/dev/null ||
		fail "the device's koko user ls does not list alice as the Owner: $(cat "$work/users")"
}

step "install.sh never runs a binary that does not match SHA256SUMS"
printf '#!/bin/sh\ntouch %s/ran\n' "$work" >"$github/releases/download/v0.0.3/$asset"
status=0
install_sh "$newer" KOKO_VERSION=v0.0.3 sh -s -- --owner alice --address localhost || status=$?
cat "$work/err" >&2
[ "$status" = 1 ] || fail "install.sh exited $status, not 1"
grep -q "$asset does not match the SHA256SUMS" "$work/err" || fail "install.sh did not say the binary does not match"
[ ! -e "$work/ran" ] || fail "install.sh ran the binary"
for path in /usr/local/bin/koko /etc/systemd/system/koko.service /etc/koko /var/lib/koko; do
	[ ! -e "$path" ] || fail "install.sh wrote $path"
done

step "koko server install refuses a container that systemd does not run"
status=0
docker run --rm --init -v "$newer/$asset:/usr/local/bin/koko:ro" "$image" sh -c '
	status=0
	koko server install --owner alice --address localhost --json || status=$?
	for path in /etc/koko /var/lib/koko /etc/systemd/system/koko.service; do
		[ ! -e "$path" ] || { echo "koko server install wrote $path" >&2; exit 99; }
	done
	exit "$status"' 2>"$work/err" || status=$?
cat "$work/err" >&2
[ "$status" = 1 ] || fail "koko server install exited $status, not 1"
jq -e '.error.code == "unsupported_platform"' "$work/err" >/dev/null || fail "koko server install did not refuse with unsupported_platform"

step "install.sh installs the older build, which runs as the koko user on 443"
echo v0.0.1 >"$github/latest"
install_ok "$older" sh -s -- --owner alice --address localhost --json
# The Join Code is a credential, so it is not shown.
jq 'del(.join_code.code)' "$work/out"
code=$(jq -r .join_code.code "$work/out")
check_running

step "a device joins with the printed Join Code and makes an API call"
docker run -d --init --name "$device" --network host \
	-v "$older/$asset:/usr/local/bin/koko:ro" "$image" sleep infinity >/dev/null
printf '%s\n' "$code" | docker exec -i "$device" koko join -
device_calls

step "systemctl show quotes an Environment entry holding [ as install reads it"
sudo mkdir -p /etc/systemd/system/koko.service.d
printf '[Service]\nEnvironment=KOKO_LISTEN=[::]:443\n' |
	sudo tee /etc/systemd/system/koko.service.d/listen.conf >/dev/null
sudo systemctl daemon-reload
environment=$(systemctl show --property=Environment --value koko.service)
case " $environment " in
*' "KOKO_LISTEN=[::]:443" '*) ;;
*) fail "systemctl show prints the koko service's Environment as: $environment" ;;
esac

step "install.sh upgrades to the newer build: the service restarts and leaves the snapshot"
echo v0.0.2 >"$github/latest"
before=$pid
install_ok "$newer" sh -s -- --json
jq . "$work/out"
jq -e '.upgraded and .listen_port == 443' "$work/out" >/dev/null || fail "install.sh did not upgrade on port 443"
cmp -s /usr/local/bin/koko "$newer/$asset" || fail "/usr/local/bin/koko is not the newer build"
check_running
[ "$pid" != "$before" ] || fail "the koko service did not restart"
sudo sh -c 'ls -l /var/lib/koko/koko.db.before-*' || fail "the upgrade left no snapshot"

step "the device still works"
device_calls

step "koko server uninstall keeps the data directory, the Root Key and the koko user"
# The redirect is this script's own file.
# shellcheck disable=SC2024
sudo /usr/local/bin/koko server uninstall --json >"$work/out"
jq . "$work/out"
! systemctl is-active --quiet koko.service || fail "the koko service is still active"
for path in /etc/systemd/system/koko.service /usr/local/bin/koko; do
	[ ! -e "$path" ] || fail "uninstall left $path"
done
listening=$(sudo ss -Hltn 'sport = :443')
[ -z "$listening" ] || fail "something still listens on 443: $listening"
sudo test -f /var/lib/koko/koko.db || fail "uninstall removed /var/lib/koko/koko.db"
sudo test -f /etc/koko/root.key || fail "uninstall removed /etc/koko/root.key"
id koko >/dev/null || fail "uninstall removed the koko user"

step "install.sh without --owner runs the data directory root put back, with its record"
sudo chown -R root:root /var/lib/koko
install_ok "$newer" sh -s -- --json
jq . "$work/out"
check_running
not_koko=$(sudo find /var/lib/koko ! -user koko)
[ -z "$not_koko" ] || fail "systemd left these in the data directory to another user than koko: $not_koko"
device_calls

step "every check passed"
