#!/bin/sh
# systemd-check.sh installs koko on this machine as its Owner would, through
# install.sh, and checks each step on the machine's real systemd:
#
#   release/systemd-check.sh <older dist> <newer dist> <upgrade dist> <failing dist>
#
# Each dist is a directory `make dist` wrote, at a tag, so that its version
# is a release's: koko server install downloads only the kokod of its own
# release. The older build must be at an older schema than the newer one, so
# that the upgrade migrates and leaves a snapshot. The upgrade and failing
# dists hold koko-linux-<arch>, kokod-linux-<arch> and SHA256SUMS of two
# releases newer than the newer one, which koko server upgrade asks for
# through the upgrade helper; the failing one's kokod exits as soon as kokod
# run starts. The real-systemd workflow (.github/workflows/systemd.yml) runs
# it on a fresh runner.
#
# In this order: a binary that does not match SHA256SUMS is never run; the
# older build installs, listens on 8443 as the koko user with the Root Key
# through LoadCredential=, and a device joins with the printed Join Code and
# makes an API call; install.sh of the newer build, without arguments,
# upgrades both programs, downloading kokod once, and the koko service
# restarts as kokod on the port it kept, with no capability, leaving the
# snapshot, and the device still works; the device installs the newer CLI
# with install.sh, without kokod; koko server upgrade goes through the real
# path unit and the root helper to the upgrade release, whose kokod runs as
# the koko user with no capability, and brings root's koko along; the
# helper puts the upgrade release's kokod back when the failing release's
# exits at its start; uninstall removes the helper's units with the koko
# unit, and keeps koko, the data directory,
# the Root Key and the koko user; and koko server install, run again on that
# data directory put back by root, downloads kokod again and starts the koko
# server with its record on the default port, 7822, where the device follows
# it with koko connect. On the way it checks what only a real systemd shows:
# the Environment systemctl show quotes. koko server install refusing a
# container that systemd does not run is the container check's
# (release/container-check.sh).
#
# install.sh downloads with curl and koko server install with Go, both from
# https://github.com, so the check serves a fake GitHub over HTTPS on
# 127.0.0.1:443, maps github.com to it in /etc/hosts, and adds the test CA
# that signed its certificate to the system's trust store, which curl and Go
# both read. Each build is the release of its own version, with every asset
# its dist holds; the release named by $damaged is the newer one with a
# binary that does not match its SHA256SUMS. The fake GitHub logs every
# request, so that a step can count its downloads. The device is a Docker
# container on the machine's network, so that it reaches the koko server
# over TCP, as a device does, and not through the Unix socket.
#
# Run it only on a throwaway Linux machine with systemd, Docker, jq, ss,
# python3, openssl, update-ca-certificates and passwordless sudo, where koko
# was never installed and nothing listens on 443, 8443 or 7822: it installs
# koko as root on port 8443, then 7822, and leaves its data, its Root Key and
# its user behind. It puts /etc/hosts and the trust store back as they were.

set -eu

fail() {
	printf '\nsystemd-check: FAIL: %s\n' "$1" >&2
	exit 1
}

step() {
	printf '\n=== %s\n' "$1"
}

[ $# -eq 4 ] || {
	echo 'usage: release/systemd-check.sh <older dist> <newer dist> <upgrade dist> <failing dist>' >&2
	exit 2
}
older=$(cd "$1" && pwd)
newer=$(cd "$2" && pwd)
upgrade=$(cd "$3" && pwd)
failing=$(cd "$4" && pwd)

case $(uname -m) in
x86_64) arch=amd64 ;;
aarch64) arch=arm64 ;;
*) fail "koko builds for amd64 and arm64, and this machine is $(uname -m)" ;;
esac
asset=koko-linux-$arch
kokod_asset=kokod-linux-$arch

for path in /etc/koko /var/lib/koko /etc/systemd/system/koko.service /etc/systemd/system/koko-upgrade.path \
	/etc/systemd/system/koko-upgrade.service /var/lib/koko-upgrade /usr/local/bin/koko /usr/local/bin/kokod; do
	[ ! -e "$path" ] || fail "$path is there: run this only on a machine koko was never installed on"
done
! id koko >/dev/null 2>&1 || fail "the koko user is there: run this only on a machine koko was never installed on"
listening=$(sudo ss -Hltn 'sport = :443')
[ -z "$listening" ] || fail "something listens on 443, where the fake GitHub must: $listening"

# version prints the version of the build in dist $1. Whether it is a
# release's is koko server install's to say: it refuses to download for a
# build that is not.
version() {
	v=$("$1/$asset" version --json) || fail "could not run $1/$asset version"
	printf '%s\n' "$v" | jq -r .version
}
older_tag=$(version "$older")
newer_tag=$(version "$newer")
[ "$older_tag" != "$newer_tag" ] || fail "the older and the newer build are both $older_tag"
upgrade_tag=$(version "$upgrade")
failing_tag=$(version "$failing")
# running and running_tag are the build the machine's koko and kokod are
# of, once the newer one is installed.
running=$newer
running_tag=$newer_tag
damaged=v0.0.0-damaged

work=$(mktemp -d)
# port is the port the older build is given with --port, not the default,
# so that the upgrade shows it kept it; default_port is the newer build's
# default (ADR 0036), which install takes once nothing tells it the port.
port=8443
default_port=7822
device=koko-systemd-check-device
# The device's image is Ubuntu 24.04 with curl and a trust store already in
# it (Docker's official buildpack-deps, its curl variant), pinned by digest
# and pulled before the koko server starts, so that no step needs Ubuntu's
# archive, or its name resolved, once the check is under way.
image=buildpack-deps:24.04-curl@sha256:a115b80bacdfad86050664accdf7e2d7be45edb87d98a3ba78b78f0f9a089c1a
github=$work/github
requests=$work/requests
ca=/usr/local/share/ca-certificates/koko-systemd-check.crt
docker pull -q "$image" >/dev/null

# On a failure, show what the koko server, the device and the fake GitHub
# logged. Then put back what the fake GitHub changed on this machine.
on_exit() {
	status=$?
	if [ "$status" != 0 ]; then
		step "after the failure: the koko service, the upgrade helper and their journals"
		systemctl status --no-pager --lines=0 koko.service koko-upgrade.path koko-upgrade.service || :
		sudo journalctl --no-pager -u koko.service -u koko-upgrade.service || :
		sudo cat /var/lib/koko-upgrade/outcome.json || :
		step "after the failure: the device's Device Mux log"
		docker exec "$device" sh -c 'cat /tmp/koko-0/*.log' || :
		step "after the failure: the fake GitHub's requests and its log"
		cat "$requests" "$work/github.log" || :
	fi
	docker rm -f "$device" >/dev/null 2>&1 || :
	[ -z "${server:-}" ] || sudo kill "$server" 2>/dev/null || :
	[ ! -f "$work/hosts" ] || sudo cp "$work/hosts" /etc/hosts || :
	if [ -e "$ca" ]; then
		sudo rm -f "$ca" && sudo update-ca-certificates --fresh >/dev/null || :
	fi
	exit "$status"
}
trap on_exit EXIT
# dash runs no EXIT trap for a signal, so a signal exits, through on_exit.
trap 'exit 1' HUP INT TERM

step "the fake GitHub serves https://github.com from this machine, under a test CA it trusts"
openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:prime256v1 -nodes -days 1 \
	-subj '/CN=koko systemd-check CA' -keyout "$work/ca.key" -out "$work/ca.crt" 2>/dev/null
printf 'basicConstraints=CA:FALSE\nkeyUsage=digitalSignature\nextendedKeyUsage=serverAuth\nsubjectAltName=DNS:github.com\n' >"$work/github.ext"
openssl req -new -newkey ec -pkeyopt ec_paramgen_curve:prime256v1 -nodes \
	-subj /CN=github.com -keyout "$work/github.key" -out "$work/github.csr" 2>/dev/null
openssl x509 -req -in "$work/github.csr" -CA "$work/ca.crt" -CAkey "$work/ca.key" -set_serial 1 -days 1 \
	-extfile "$work/github.ext" -out "$work/github.crt" 2>/dev/null
sudo cp "$work/ca.crt" "$ca"
sudo update-ca-certificates >/dev/null
cp /etc/hosts "$work/hosts"
printf '127.0.0.1 github.com\n' | sudo tee -a /etc/hosts >/dev/null

# The fake GitHub serves https://github.com/cieons/koko as install.sh and
# koko server install ask for it: releases/latest redirects to the tag page
# of the tag $github/latest names, releases/tag/<tag> answers for a release
# there, and releases/download/<tag>/<file> is $github/<tag>/<file>. It logs
# each request to $requests as `<method> <path> <status>`, before it answers.
cat >"$work/github.py" <<'EOF'
import http.server
import os
import ssl
import sys

root, log, cert, key = sys.argv[1:]
repo = "/cieons/koko/releases/"


def named(*parts):
    return all(p not in ("", ".", "..") for p in parts)


class Handler(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        self.answer(True)

    def do_HEAD(self):
        self.answer(False)

    def answer(self, body):
        path = self.path.partition("?")[0]
        rest = path[len(repo):].split("/") if path.startswith(repo) else []
        status, location, data = 404, None, b"Not Found\n"
        if rest == ["latest"]:
            with open(os.path.join(root, "latest")) as f:
                tag = f.read().strip()
            status, location, data = 302, "https://github.com" + repo + "tag/" + tag, b""
        elif len(rest) == 2 and rest[0] == "tag" and named(rest[1]) and os.path.isdir(os.path.join(root, rest[1])):
            status, data = 200, b"the release's page\n"
        elif len(rest) == 3 and rest[0] == "download" and named(rest[1], rest[2]):
            file = os.path.join(root, rest[1], rest[2])
            if os.path.isfile(file):
                with open(file, "rb") as f:
                    status, data = 200, f.read()
        self.send_response(status)
        if location:
            self.send_header("Location", location)
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        if body:
            self.wfile.write(data)

    def log_request(self, code="-", size="-"):
        with open(log, "a") as f:
            f.write(f"{self.command} {self.path} {code}\n")


server = http.server.ThreadingHTTPServer(("127.0.0.1", 443), Handler)
context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
context.load_cert_chain(cert, key)
server.socket = context.wrap_socket(server.socket, server_side=True)
server.serve_forever()
EOF
mkdir "$github"
: >"$requests"
sudo python3 "$work/github.py" "$github" "$requests" "$work/github.crt" "$work/github.key" 2>"$work/github.log" &
server=$!
# Wait for an answer over HTTPS itself, through /etc/hosts and the trust
# store, as install.sh and koko will reach it.
for _ in $(seq 100); do
	if curl --proto =https -sS -o /dev/null https://github.com/ 2>/dev/null; then
		break
	fi
	sudo kill -0 "$server" 2>/dev/null || fail "the fake GitHub exited: $(cat "$work/github.log")"
	sleep 0.1
done
curl --proto =https -sS -o /dev/null https://github.com/ || fail "the fake GitHub does not answer over HTTPS"

# release publishes the build in dist $2 as the release tagged $1: every
# asset its SHA256SUMS lists, and SHA256SUMS.
release() {
	mkdir "$github/$1"
	cp "$2/SHA256SUMS" "$github/$1/"
	awk '{ sub(/^\*/, "", $2); print $2 }' "$2/SHA256SUMS" | while read -r file; do
		cp "$2/$file" "$github/$1/"
	done
}
release "$older_tag" "$older"
release "$newer_tag" "$newer"
release "$damaged" "$newer"
release "$upgrade_tag" "$upgrade"
release "$failing_tag" "$failing"
ls -l "$github"/*

# mark starts a count of the fake GitHub's requests; since_mark prints those
# requests, and served how many times among them it served the release
# asset $1 (<tag>/<file>).
mark() {
	marked=$(wc -l <"$requests")
}
since_mark() {
	tail -n "+$((marked + 1))" "$requests"
}
served() {
	since_mark | grep -Fcx "GET /cieons/koko/releases/download/$1 200" || :
}

# install_sh runs the install.sh of dist $1 as root, as
# `curl … | sudo sh -s -- <args>` does: with sudo's PATH, and the variables
# before -- in its environment. Its stdout goes to $work/out and its stderr
# to $work/err.
install_sh() {
	dist=$1
	shift
	# The redirects are this script's own files, as with curl and a terminal.
	# shellcheck disable=SC2024
	sudo env "$@" <"$dist/install.sh" >"$work/out" 2>"$work/err"
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
# on $port and holds the Root Key through LoadCredential=, and sets pid to
# its process.
check_running() {
	systemctl is-active --quiet koko.service || fail "the koko service is not active"
	pid=$(systemctl show --property=MainPID --value koko.service)
	[ "$pid" != 0 ] || fail "the koko service has no process"
	user=$(ps -o user= -p "$pid")
	[ "$user" = koko ] || fail "the koko server runs as $user, not koko"
	sudo ss -Hltnp "sport = :$port" | grep -q "pid=$pid," ||
		fail "the koko server (pid $pid) does not listen on $port: $(sudo ss -Hltnp "sport = :$port")"
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

# check_runs checks that /usr/local/bin/koko and /usr/local/bin/kokod are
# the build $running's, $running_tag, that the koko service runs kokod as the
# koko user on $port, holding no capability, and that the upgrade helper's
# path unit waits for a request.
check_runs() {
	for program in koko kokod; do
		cmp -s "/usr/local/bin/$program" "$running/$program-linux-$arch" ||
			fail "/usr/local/bin/$program is not $running_tag's $program-linux-$arch"
		got=$("/usr/local/bin/$program" version --json | jq -r .version)
		[ "$got" = "$running_tag" ] || fail "$program version says $got, not $running_tag"
	done
	grep -qx 'ExecStart=/usr/local/bin/kokod run' /etc/systemd/system/koko.service ||
		fail "the unit does not run /usr/local/bin/kokod run: $(grep ExecStart= /etc/systemd/system/koko.service)"
	check_running
	[ "$(ps -o comm= -p "$pid")" = kokod ] || fail "the koko service's process is $(ps -o comm= -p "$pid"), not kokod"
	# A port of 1024 or above needs no capability, so the unit leaves the
	# koko server none, not even one it could gain.
	for set in CapEff CapBnd; do
		held=$(awk -v set="$set:" '$1 == set { print $2 }' "/proc/$pid/status")
		[ "$held" = 0000000000000000 ] || fail "the koko server on $port holds the capabilities $held in $set"
	done
	systemctl is-active --quiet koko-upgrade.path || fail "the upgrade helper's path unit is not active"
	systemctl is-enabled --quiet koko-upgrade.path || fail "the upgrade helper's path unit is not enabled"
}

# check_downloaded checks that, since mark, the fake GitHub served the
# kokod of $running_tag exactly once, and that koko removed what it
# downloaded.
check_downloaded() {
	since_mark
	[ "$(served "$running_tag/$kokod_asset")" = 1 ] ||
		fail "the fake GitHub served $running_tag/$kokod_asset $(served "$running_tag/$kokod_asset") times, not once"
	left=$(sudo find /tmp -maxdepth 1 -name 'koko-kokod-*')
	[ -z "$left" ] || fail "koko server install left its download behind: $left"
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
printf '#!/bin/sh\ntouch %s/ran\n' "$work" >"$github/$damaged/$asset"
status=0
install_sh "$newer" KOKO_VERSION="$damaged" sh -s -- --owner alice --address localhost || status=$?
cat "$work/err" >&2
[ "$status" = 1 ] || fail "install.sh exited $status, not 1"
grep -q "$asset does not match the SHA256SUMS" "$work/err" || fail "install.sh did not say the binary does not match"
[ ! -e "$work/ran" ] || fail "install.sh ran the binary"
for path in /usr/local/bin/koko /usr/local/bin/kokod /etc/systemd/system/koko.service /etc/koko /var/lib/koko; do
	[ ! -e "$path" ] || fail "install.sh wrote $path"
done

step "install.sh installs the older build, $older_tag, which runs as the koko user on $port"
echo "$older_tag" >"$github/latest"
install_ok "$older" sh -s -- --owner alice --address localhost --port "$port" --json
# The Join Code is a credential, so it is not shown.
jq 'del(.join_code.code)' "$work/out"
code=$(jq -r .join_code.code "$work/out")
check_running

step "a device joins with the printed Join Code and makes an API call"
# The device's koko is a file of its own, which install.sh replaces later.
docker run -d --init --name "$device" --network host --add-host github.com:127.0.0.1 \
	"$image" sleep infinity >/dev/null
docker cp -q "$older/$asset" "$device:/usr/local/bin/koko"
printf '%s\n' "$code" | docker exec -i "$device" koko join -
device_calls

step "systemctl show quotes an Environment entry holding [ as install reads it"
sudo mkdir -p /etc/systemd/system/koko.service.d
printf '[Service]\nEnvironment=KOKO_LISTEN=[::]:%s\n' "$port" |
	sudo tee /etc/systemd/system/koko.service.d/listen.conf >/dev/null
sudo systemctl daemon-reload
environment=$(systemctl show --property=Environment --value koko.service)
case " $environment " in
*" \"KOKO_LISTEN=[::]:$port\" "*) ;;
*) fail "systemctl show prints the koko service's Environment as: $environment" ;;
esac

step "install.sh of the newer build, $newer_tag, without arguments, upgrades both, downloading kokod once"
echo "$newer_tag" >"$github/latest"
before=$pid
mark
install_ok "$newer" sh -s -- --json
jq . "$work/out"
jq -e --argjson port "$port" '.upgraded and .listen_port == $port' "$work/out" >/dev/null ||
	fail "install.sh did not upgrade on port $port"
check_downloaded
check_runs
[ "$pid" != "$before" ] || fail "the koko service did not restart"
sudo sh -c 'ls -l /var/lib/koko/koko.db.before-*' || fail "the upgrade left no snapshot"
device_calls

step "the device installs the newer CLI with install.sh without arguments, and no kokod"
# Its image brings curl and a trust store, and it trusts the test CA as
# this machine does, through its own store.
docker cp -q "$work/ca.crt" "$device:/usr/local/share/ca-certificates/koko-systemd-check.crt"
docker exec "$device" update-ca-certificates >/dev/null
# DIAG (branch only, #159)
{
	echo "--- diag: docker"; docker version --format '{{.Server.Version}} {{.Client.Version}}'
	docker image inspect --format '{{.RepoDigests}}' ubuntu:24.04 || docker pull -q ubuntu:24.04
	echo "--- host resolv.conf"; ls -l /etc/resolv.conf; cat /etc/resolv.conf
	echo "--- host hosts"; cat /etc/hosts
	echo "--- resolvectl"; resolvectl status 2>&1 | head -40
	echo "--- systemd-resolved"; systemctl is-active systemd-resolved; sudo ss -Hlnup 'sport = :53'; sudo ss -Hlntp 'sport = :53'
	echo "--- host getent"; getent hosts archive.ubuntu.com; echo "rc=$?"
	echo "--- device resolv.conf"; docker exec "$device" cat /etc/resolv.conf
	echo "--- device hosts"; docker exec "$device" cat /etc/hosts
	echo "--- device nsswitch"; docker exec "$device" grep hosts /etc/nsswitch.conf
	echo "--- device getent"; docker exec "$device" getent hosts archive.ubuntu.com; echo "rc=$?"
	echo "--- fresh host-network ubuntu:24.04"; docker run --rm --network host ubuntu:24.04 sh -c 'cat /etc/resolv.conf; getent hosts archive.ubuntu.com; echo rc=$?'
	echo "--- fresh bridge ubuntu:24.04"; docker run --rm ubuntu:24.04 sh -c 'cat /etc/resolv.conf; getent hosts archive.ubuntu.com; echo rc=$?'
	echo "--- fresh host-network + add-host"; docker run --rm --network host --add-host github.com:127.0.0.1 ubuntu:24.04 sh -c 'getent hosts archive.ubuntu.com; echo rc=$?; apt-get update -qq 2>&1 | tail -3'
	echo "--- nft/iptables"; sudo iptables -S 2>&1 | head -30
	echo "--- journal resolved"; sudo journalctl --no-pager -u systemd-resolved -n 20
} || :
mark
docker exec -i "$device" sh -s <"$newer/install.sh" || fail "the device's install.sh failed"
since_mark
[ "$(served "$newer_tag/$asset")" = 1 ] || fail "the device did not download $newer_tag/$asset once"
[ "$(served "$newer_tag/$kokod_asset")" = 0 ] || fail "the device downloaded $newer_tag/$kokod_asset"
got=$(docker exec "$device" koko version --json | jq -r .version)
[ "$got" = "$newer_tag" ] || fail "the device's koko version says $got, not $newer_tag"
docker exec "$device" sh -c '! test -e /usr/local/bin/kokod' || fail "the device has /usr/local/bin/kokod"
device_calls

# outcome prints what the upgrade helper recorded of the last upgrade.
outcome() {
	sudo cat /var/lib/koko-upgrade/outcome.json
}

step "koko server upgrade to $upgrade_tag goes through the path unit and the root helper, then brings root's koko along"
before=$pid
mark
status=0
# The redirect is this script's own file.
# shellcheck disable=SC2024
sudo /usr/local/bin/koko server upgrade --version "$upgrade_tag" --json >"$work/out" 2>"$work/err" || status=$?
cat "$work/err" >&2
[ "$status" = 0 ] || fail "koko server upgrade exited $status"
jq . "$work/out"
jq -e --arg from "$running_tag" --arg to "$upgrade_tag" \
	'.server == {"from": $from, "to": $to, "state": "succeeded", "message": null} and .cli.updated and .cli.to == $to' "$work/out" >/dev/null ||
	fail "koko server upgrade did not upgrade the koko server and koko from $running_tag to $upgrade_tag"
outcome | jq -e --arg to "$upgrade_tag" '.state == "succeeded" and .to == $to and .finished_at != null' >/dev/null ||
	fail "the helper's outcome is not succeeded: $(outcome)"
# Only the path unit starts the helper, which ran once and succeeded.
[ "$(systemctl show --property=Result --value koko-upgrade.service)" = success ] ||
	fail "koko-upgrade.service did not succeed: $(systemctl show --property=Result --value koko-upgrade.service)"
sudo journalctl --no-pager -u koko-upgrade.service | grep -q "$upgrade_tag" ||
	fail "koko-upgrade.service's journal does not name $upgrade_tag"
since_mark
[ "$(served "$upgrade_tag/$kokod_asset")" = 1 ] || fail "the helper did not download $upgrade_tag/$kokod_asset once"
[ "$(served "$upgrade_tag/$asset")" = 1 ] || fail "koko server upgrade did not download $upgrade_tag/$asset once"
[ ! -e /run/koko/upgrade.request ] || fail "the helper left the request"
left=$(sudo find /var/lib/koko-upgrade -mindepth 1 -maxdepth 1 -name 'download-*')
[ -z "$left" ] || fail "the helper left its download behind: $left"
cmp -s /var/lib/koko-upgrade/kokod.previous "$running/$kokod_asset" || fail "the helper did not keep the previous kokod"
running=$upgrade
running_tag=$upgrade_tag
check_runs
[ "$pid" != "$before" ] || fail "the koko service did not restart"
device_calls

step "the helper puts $upgrade_tag's kokod back when $failing_tag's exits at its start"
before=$pid
snapshots=$(sudo sh -c 'ls /var/lib/koko/koko.db.before-*')
mark
status=0
# The redirect is this script's own file.
# shellcheck disable=SC2024
sudo /usr/local/bin/koko server upgrade --version "$failing_tag" --json >"$work/out" 2>"$work/err" || status=$?
cat "$work/err" >&2
[ "$status" = 1 ] || fail "koko server upgrade to the failing release exited $status, not 1"
jq -e '.error.code == "upgrade_failed"' "$work/err" >/dev/null || fail "koko server upgrade did not fail with upgrade_failed"
outcome | jq -e --arg from "$upgrade_tag" --arg to "$failing_tag" \
	'.state == "failed" and .from == $from and .to == $to and (.message | contains("the previous kokod runs again"))' >/dev/null ||
	fail "the helper's outcome is not failed with the previous kokod running: $(outcome)"
[ "$(served "$failing_tag/$kokod_asset")" = 1 ] || fail "the helper did not download $failing_tag/$kokod_asset once"
[ "$(served "$failing_tag/$asset")" = 0 ] || fail "koko server upgrade updated koko after a failed upgrade"
# kokod.previous is the kokod the helper put back.
cmp -s /var/lib/koko-upgrade/kokod.previous "$upgrade/$kokod_asset" || fail "the helper did not keep $upgrade_tag's kokod"
check_runs
[ "$pid" != "$before" ] || fail "the koko service runs the process it ran before the upgrade, not the one put back"
# The failing kokod exited before it opened the database, so the schema did
# not move: the snapshot of the earlier upgrade stays, and nothing was set
# aside.
[ "$(sudo sh -c 'ls /var/lib/koko/koko.db.before-*')" = "$snapshots" ] || fail "the snapshots changed: $(sudo sh -c 'ls /var/lib/koko/koko.db*')"
! sudo sh -c 'ls /var/lib/koko/koko.db.failed-*' 2>/dev/null || fail "the helper set a database aside though the schema did not move"
device_calls

step "koko server uninstall removes the unit, the upgrade helper and kokod, and keeps koko, the data directory, the Root Key and the koko user"
mark
# The redirect is this script's own file.
# shellcheck disable=SC2024
sudo /usr/local/bin/koko server uninstall --json >"$work/out"
jq . "$work/out"
[ -z "$(since_mark)" ] || fail "koko server uninstall asked the fake GitHub for $(since_mark), though the kokod of its build was installed"
! systemctl is-active --quiet koko.service || fail "the koko service is still active"
! systemctl is-active --quiet koko-upgrade.path || fail "the upgrade helper's path unit is still active"
for path in /etc/systemd/system/koko.service /etc/systemd/system/koko-upgrade.path \
	/etc/systemd/system/koko-upgrade.service /usr/local/bin/kokod /var/lib/koko-upgrade; do
	[ ! -e "$path" ] || fail "uninstall left $path"
done
cmp -s /usr/local/bin/koko "$running/$asset" || fail "uninstall changed /usr/local/bin/koko"
listening=$(sudo ss -Hltn "sport = :$port")
[ -z "$listening" ] || fail "something still listens on $port: $listening"
sudo test -f /var/lib/koko/koko.db || fail "uninstall removed /var/lib/koko/koko.db"
sudo test -f /etc/koko/root.key || fail "uninstall removed /etc/koko/root.key"
id koko >/dev/null || fail "uninstall removed the koko user"

step "koko server install without arguments downloads kokod again and runs the data directory root put back, with its record, on the default port"
# Without the unit or the drop-in, install cannot know the port the koko
# server listened on, so it takes the default, and says so by
# naming the address devices must connect to, which the device then takes
# with koko connect.
sudo rm -f /etc/systemd/system/koko.service.d/listen.conf
sudo chown -R root:root /var/lib/koko
mark
# The redirect is this script's own file.
# shellcheck disable=SC2024
sudo /usr/local/bin/koko server install --json >"$work/out"
jq . "$work/out"
jq -e --argjson port "$default_port" '.upgraded and .listen_port == $port and .devices_must_connect == "localhost:\($port)"' "$work/out" >/dev/null ||
	fail "koko server install did not take the default port $default_port"
port=$default_port
check_downloaded
check_runs
not_koko=$(sudo find /var/lib/koko ! -user koko)
[ -z "$not_koko" ] || fail "systemd left these in the data directory to another user than koko: $not_koko"
docker exec "$device" koko connect "localhost:$port" || fail "the device's koko connect localhost:$port failed"
device_calls

step "every check passed"
