#!/bin/sh
# container-check.sh runs the koko server's image as its Owner would, through
# the install docs' release/compose.yaml, and checks each step on this
# machine's Docker:
#
#   release/container-check.sh <dist>
#
# <dist> is a directory `make dist` and then `make image` wrote: the image's
# archive, koko-image.tar, beside the binaries and SHA256SUMS. The container
# workflow (.github/workflows/container.yml) runs it on a fresh runner of
# each platform the image is built for, which runs that platform's image
# natively.
#
# First, the Compose file the install docs show is release/compose.yaml as
# it is, and they give the install -d line this check runs. Then, in this order: the first start, through the Compose file, on
# a state/ made by the docs' install -d line and a published port that
# differs from the container's, runs kokod run as uid and gid 65532 with no
# capability, writes state/ as the docs say, and logs no Root Key; koko
# version and kokod version in the container name the same build; a state/
# Docker made as root, and --user 0, are refused, writing nothing, and a
# named volume starts with no step on the host; docker exec koko koko device
# add issues a Join Code, which no log holds, and a device joins with it;
# docker kill, then docker start, takes over the stale socket; a changed
# KOKO_ADDRESS logs the warning and changes no record; docker compose down,
# then up, keeps the record on the same state/, and the device still works;
# a copy of data/ without root.key is refused, writing no Root Key;
# koko server install refuses in a plain container with no network, naming
# the image, before any download; and
# the image's index holds linux/amd64 and linux/arm64, each with the kokod
# and the koko SHA256SUMS list. Upgrades and going back are the
# real-systemd check's and the tests', since the image holds the same
# binaries.
#
# The image runs as a tag of its own, koko-check:local, which an override
# file beside the Compose file names, with the published port and the
# address; the Compose file itself is the docs' (Compose v2.24 or later reads
# the override's !override). The device is a Docker container on the
# machine's network, with the release's binary as its koko, so that it
# reaches the koko server over TCP at the published port, as a device does.
#
# Run it only on a throwaway Linux machine with Docker, skopeo, jq and
# passwordless sudo, where no container is named koko and nothing listens
# on 17822. It removes the containers, networks, volumes, image and files it
# made.

set -eu

fail() {
	printf '\ncontainer-check: FAIL: %s\n' "$1" >&2
	exit 1
}

step() {
	printf '\n=== %s\n' "$1"
}

[ $# -eq 1 ] || {
	echo 'usage: release/container-check.sh <dist>' >&2
	exit 2
}
dist=$(cd "$1" && pwd)
repo=$(cd "$(dirname "$0")/.." && pwd)

case $(uname -m) in
x86_64) arch=amd64 ;;
aarch64) arch=arm64 ;;
*) fail "koko builds for amd64 and arm64, and this machine is $(uname -m)" ;;
esac
asset=koko-linux-$arch

# port is the host port the koko server is published on, which differs from
# the container's 7822 so that the Server Address must name it.
port=17822
address=localhost:$port
# The local image is $repository:$version, which the override file names
# through KOKO_VERSION, as the Compose file names a release.
repository=koko-check
version=local
image=$repository:$version
# prepare is the line the install docs give to make state/ for a bind mount.
prepare='sudo install -d -o 65532 -g 65532 -m 0700 state'
device=koko-check-device
volume_container=koko-check-volume
ubuntu=ubuntu:24.04
work=$(mktemp -d)

# On a failure, show what the koko server and the device logged. Whatever
# the check made is removed either way.
on_exit() {
	status=$?
	if [ "$status" != 0 ]; then
		step "after the failure: docker logs koko"
		docker logs koko || :
		step "after the failure: the device's Device Mux log"
		docker exec "$device" sh -c 'cat /tmp/koko-0/*.log' || :
	fi
	docker rm -f "$device" "$volume_container" >/dev/null 2>&1 || :
	for d in koko rootmade volume copy; do
		[ ! -d "$work/$d" ] || compose "$d" down --volumes --remove-orphans >/dev/null 2>&1 || :
	done
	docker image rm "$image" >/dev/null 2>&1 || :
	sudo rm -rf "$work"
	exit "$status"
}
trap on_exit EXIT

# compose runs docker compose in the deployment folder $work/$1, as the docs
# run it beside compose.yaml.
compose() {
	dir=$work/$1
	shift
	(cd "$dir" && docker compose "$@")
}

# deployment makes the deployment folder $work/$1 as the docs say: the Compose
# file as compose.yaml and its .env, with no state/. Beside them, the
# override file Compose reads by itself runs the local image, publishes
# $port and names $address, unless KOKO_ADDRESS is set when compose runs;
# $2, when given, is more lines for it.
deployment() {
	mkdir "$work/$1"
	cp "$repo/release/compose.yaml" "$work/$1/compose.yaml"
	echo "KOKO_VERSION=$version" >"$work/$1/.env"
	{
		cat <<EOF
services:
  koko:
    image: $repository:\${KOKO_VERSION}
    ports: !override ["$port:7822"]
    environment:
      KOKO_ADDRESS: \${KOKO_ADDRESS:-$address}
EOF
		[ $# -lt 2 ] || printf '%s\n' "$2"
	} >"$work/$1/compose.override.yaml"
}

# wait_serving waits until the koko server in container $1 answers a call
# over its Unix socket, which it opens only once it has started, and fails
# when the container stops first.
wait_serving() {
	deadline=$(($(date +%s) + 60))
	until docker exec "$1" koko server show --json >"$work/show" 2>/dev/null; do
		running=$(docker inspect --format '{{.State.Running}}' "$1" 2>/dev/null || echo false)
		[ "$running" = true ] || { docker logs "$1" >&2 || :; fail "container $1 stopped before its koko server answered"; }
		[ "$(date +%s)" -lt "$deadline" ] || fail "the koko server in container $1 did not answer within 60 s"
		sleep 0.5
	done
}

# wait_log waits until the log of container $1 holds $2, and saves the log
# in $work/log. Docker copies what a container writes into its log on its
# own time, so a line written before the socket opened may come after.
wait_log() {
	deadline=$(($(date +%s) + 30))
	until docker logs "$1" >"$work/log" 2>&1 && grep -qF -- "$2" "$work/log"; do
		[ "$(date +%s)" -lt "$deadline" ] || fail "docker logs $1 does not say: $2"
		sleep 0.5
	done
}

# show_is checks that koko server show, which wait_serving last saved, has
# the jq expression $1 true, saying $2 otherwise.
show_is() {
	jq -e "$1" "$work/show" >/dev/null || fail "$2: koko server show says $(cat "$work/show")"
}

# device_calls makes one API call from the device, user ls, and checks that
# it lists alice as the Owner.
device_calls() {
	docker exec "$device" koko user ls --json >"$work/users" ||
		fail "the device's koko user ls failed"
	jq -e 'any(.[]; .name == "alice" and .role == "owner")' "$work/users" >/dev/null ||
		fail "the device's koko user ls does not list alice as the Owner: $(cat "$work/users")"
}

# refused runs the koko server once in the deployment folder $work/$1, with
# docker compose run, so that no restart policy starts it again, and the
# options after $1; it must refuse to start within 60 s. Its stderr is in
# $work/err, shown.
refused() {
	dir=$work/$1
	shift
	status=0
	(cd "$dir" && timeout 60 docker compose run --rm "$@" koko) >"$work/out" 2>"$work/err" || status=$?
	cat "$work/err" >&2
	[ "$status" != 124 ] || fail "the koko server in $dir did not refuse within 60 s"
	[ "$status" != 0 ] || fail "the koko server in $dir did not refuse"
}

# says checks that the stderr refused saved holds each of its arguments.
says() {
	for s in "$@"; do
		grep -qF -- "$s" "$work/err" || fail "the refusal does not say: $s"
	done
}

# empty checks that directory $1 holds nothing.
empty() {
	left=$(sudo find "$1" -mindepth 1)
	[ -z "$left" ] || fail "$1 is not empty: $left"
}

# owned checks that path $1 is uid and gid 65532's and, when $2 is given,
# has mode $2.
owned() {
	if [ $# -eq 2 ]; then
		got=$(sudo stat -c '%u:%g %a' "$1") want="65532:65532 $2"
	else
		got=$(sudo stat -c '%u:%g' "$1") want=65532:65532
	fi
	[ "$got" = "$want" ] || fail "$1 is $got, not $want"
}

step "the install docs show release/compose.yaml as it is"
for doc in docs/install.md docs/install.zh-CN.md; do
	# The block in the docs is the yaml block whose first line is the
	# Compose file's.
	awk -v first="$(sed -n 1p "$repo/release/compose.yaml")" '
		inside && /^```/ { exit }
		inside { print; next }
		previous == "```yaml" && $0 == first { inside = 1; print }
		{ previous = $0 }' "$repo/$doc" >"$work/shown"
	diff -u "$repo/release/compose.yaml" "$work/shown" >&2 ||
		fail "the Compose file $doc shows is not release/compose.yaml"
	grep -qxF -- "$prepare" "$repo/$doc" || fail "$doc does not give the line: $prepare"
done

step "the image of $arch, loaded from $dist/koko-image.tar as $image"
skopeo copy --quiet "oci-archive:$dist/koko-image.tar" "docker-daemon:$image" ||
	fail "skopeo could not load the image"
docker pull -q "$ubuntu" >/dev/null

step "the first start, on state/ made by the docs' line, runs as 65532 with no capability"
deployment koko
(cd "$work/koko" && sh -c "$prepare")
compose koko up -d
wait_serving koko
show_is ".address == \"$address\"" "the Server Address is not $address"
pid=$(docker inspect --format '{{.State.Pid}}' koko)
# The image runs the koko server as kokod run, from its own entrypoint and
# command, which the Compose file leaves as they are.
cmdline=$(sudo cat "/proc/$pid/cmdline" | tr '\0' ' ')
[ "$cmdline" = 'kokod run ' ] || fail "the container runs '$cmdline', not kokod run"
# The redirect is this script's own file.
# shellcheck disable=SC2024
sudo cat "/proc/$pid/status" >"$work/status"
for ids in Uid Gid; do
	# real, effective, saved and filesystem
	got=$(awk -v ids="$ids:" '$1 == ids { print $2, $3, $4, $5 }' "$work/status")
	[ "$got" = '65532 65532 65532 65532' ] || fail "the koko server's $ids are $got, not 65532"
done
# Docker lists the primary gid as a supplementary group too.
groups=$(awk '$1 == "Groups:" { $1 = ""; print }' "$work/status")
for g in $groups; do
	[ "$g" = 65532 ] || fail "the koko server is in the groups $groups, not 65532 alone"
done
for set in CapEff CapPrm; do
	held=$(awk -v set="$set:" '$1 == set { print $2 }' "$work/status")
	[ "$held" = 0000000000000000 ] || fail "the koko server holds the capabilities $held in $set"
done
state=$work/koko/state
owned "$state" 700
owned "$state/root.key" 600
owned "$state/data" 700
# koko.db's own mode is SQLite's; data/, 0700, keeps it from others.
owned "$state/data/koko.db"
lines=$(sudo cat "$state/root.key" | wc -l | tr -d ' ')
[ "$lines" = 1 ] || fail "root.key holds $lines lines, not 1"
key_id=$(sudo cut -d: -f1 "$state/root.key")
key=$(sudo cut -d: -f2 "$state/root.key")
show_is ".root_key_id == \"$key_id\"" "the Root Key's id is not $key_id"
fingerprint=$(jq -r .host_key_fingerprint "$work/show")
# The start-up's last line names koko device add, after the one that says
# it initialised.
wait_log koko 'koko device add'
grep -q 'koko server initialised' "$work/log" || fail "docker logs koko does not say koko server initialised"
! grep -qF -- "$key" "$work/log" || fail "docker logs koko holds the Root Key"

step "docker exec koko koko version and kokod version name the same build"
docker exec koko koko version --json >"$work/koko-version"
docker exec koko kokod version --json >"$work/kokod-version"
build=$(jq -r '"\(.version) \(.revision)"' "$work/koko-version")
[ "$(jq -r '"\(.version) \(.revision)"' "$work/kokod-version")" = "$build" ] ||
	fail "kokod version says $(cat "$work/kokod-version"), but koko version says $build"
docker exec koko kokod version | grep -q '^kokod ' || fail "kokod version does not name kokod"
echo "both are $build"

step "a state/ Docker made as root is refused, naming it and uid 65532, and nothing is written"
deployment rootmade
refused rootmade
says 'uid 65532 and gid 65532' /koko
got=$(sudo stat -c '%u:%g' "$work/rootmade/state")
[ "$got" = 0:0 ] || fail "Docker made state/ as $got, not as root"
empty "$work/rootmade/state"

step "the image run with --user 0 is refused, naming the rule, and nothing is written"
refused rootmade --user 0
says 'does not run as root' '--user 0'
empty "$work/rootmade/state"

step "a named volume starts and initialises with no step on the host"
deployment volume '    volumes: !override ["koko:/koko"]
volumes:
  koko: {}'
compose volume run -d --name "$volume_container" koko >/dev/null
wait_serving "$volume_container"
show_is ".address == \"$address\"" "the Server Address on the named volume is not $address"
[ ! -e "$work/volume/state" ] || fail "the named volume's deployment has a state/"
docker rm -f "$volume_container" >/dev/null
compose volume down --volumes

step "docker exec koko koko device add issues a Join Code, and a device joins with it"
docker exec koko koko device add --json >"$work/code"
code=$(jq -r .code "$work/code")
[ -n "$code" ] && [ "$code" != null ] || fail "koko device add printed no Join Code"
docker run -d --init --name "$device" --network host \
	-v "$dist/$asset:/usr/local/bin/koko:ro" "$ubuntu" sleep infinity >/dev/null
printf '%s\n' "$code" | docker exec -i "$device" koko join -
device_calls
docker logs koko >"$work/log" 2>&1
! grep -qF -- "$code" "$work/log" || fail "docker logs koko holds the Join Code"
! grep -qF -- "$key" "$work/log" || fail "docker logs koko holds the Root Key"

step "docker kill, then docker start, takes over the stale socket"
docker kill koko >/dev/null
docker wait koko >/dev/null
docker diff koko | grep -qx 'A /run/koko/koko.sock' || fail "docker kill left no socket file behind"
docker start koko >/dev/null
wait_serving koko
wait_log koko 'removed a stale socket nobody answered on'
device_calls

step "a changed KOKO_ADDRESS logs the warning, and the record keeps the first address"
(
	export KOKO_ADDRESS=localhost:17823
	compose koko up -d
)
wait_serving koko
wait_log koko 'address=localhost:17823'
grep -q 'koko server set address=' "$work/log" || fail "docker logs koko does not warn of the changed KOKO_ADDRESS"
grep -qF "recorded_address=$address" "$work/log" || fail "docker logs koko does not name the recorded address $address"
show_is ".address == \"$address\"" "the changed KOKO_ADDRESS changed the Server Address"

step "docker compose down, then up, keeps the record on the same state/, and the device still works"
compose koko down
compose koko up -d
wait_serving koko
show_is ".address == \"$address\" and .root_key_id == \"$key_id\" and .host_key_fingerprint == \"$fingerprint\"" \
	"the recreated koko server lost its record"
# A warning would come before the line that says it started.
wait_log koko 'koko server started'
! grep -q 'koko server set address=' "$work/log" || fail "docker logs koko warns, with KOKO_ADDRESS as recorded"
device_calls

step "a copy of data/ without root.key is refused, naming the id it needs, and writes no Root Key"
deployment copy
(cd "$work/copy" && sh -c "$prepare")
compose koko stop
sudo cp -a "$state/data" "$work/copy/state/data"
refused copy
says "the sealed data needs $key_id"
! sudo test -e "$work/copy/state/root.key" || fail "the refused start wrote a Root Key"

step "koko server install refuses a container that systemd does not run, naming the image, before any download"
# With no network, a download tried before the refusal would fail as
# download_failed instead.
status=0
docker run --rm --init --network none -v "$dist/$asset:/usr/local/bin/koko:ro" "$ubuntu" sh -c '
	status=0
	koko server install --owner alice --address localhost --json || status=$?
	for path in /etc/koko /var/lib/koko /etc/systemd/system/koko.service; do
		[ ! -e "$path" ] || { echo "koko server install wrote $path" >&2; exit 99; }
	done
	exit "$status"' 2>"$work/err" || status=$?
cat "$work/err" >&2
[ "$status" = 1 ] || fail "koko server install exited $status, not 1"
jq -e '.error.code == "unsupported_platform" and (.error.message | contains("ghcr.io/cieons/koko"))' "$work/err" >/dev/null ||
	fail "koko server install did not refuse with unsupported_platform, naming ghcr.io/cieons/koko"

step "the image's index holds linux/amd64 and linux/arm64, each with the two binaries SHA256SUMS lists"
oci=$work/oci
mkdir "$oci"
tar -xf "$dist/koko-image.tar" -C "$oci"
# blob prints the file of the blob whose digest is $1.
blob() {
	echo "$oci/blobs/${1%%:*}/${1#*:}"
}
# The layout's index.json names the image's index, which names a manifest
# for each platform.
index=$(blob "$(jq -r '.manifests[0].digest' "$oci/index.json")")
platforms=$(jq -r '[.manifests[].platform | "\(.os)/\(.architecture)"] | sort | join(" ")' "$index")
[ "$platforms" = 'linux/amd64 linux/arm64' ] || fail "the image's index holds $platforms, not linux/amd64 and linux/arm64"
for a in amd64 arm64; do
	manifest=$(blob "$(jq -r --arg a "$a" '.manifests[] | select(.platform.architecture == $a) | .digest' "$index")")
	for program in kokod koko; do
		want=$(awk -v name="$program-linux-$a" '$2 == name { print $1 }' "$dist/SHA256SUMS")
		[ -n "$want" ] || fail "SHA256SUMS lists no $program-linux-$a"
		got=
		for layer in $(jq -r '.layers[].digest' "$manifest"); do
			if tar -tzf "$(blob "$layer")" | grep -qx "usr/local/bin/$program"; then
				got=$(tar -xzOf "$(blob "$layer")" "usr/local/bin/$program" | sha256sum | cut -d' ' -f1)
			fi
		done
		[ -n "$got" ] || fail "the linux/$a image holds no /usr/local/bin/$program"
		[ "$got" = "$want" ] || fail "the linux/$a image's /usr/local/bin/$program is $got, but SHA256SUMS lists $want"
		echo "linux/$a: /usr/local/bin/$program is $program-linux-$a, $got"
	done
done

step "every check passed"
