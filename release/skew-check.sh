#!/bin/sh
# skew-check.sh runs a CLI of one version against a koko server of another,
# real binaries both, and checks each step on this machine's Docker
# (ADR 0049):
#
#   release/skew-check.sh <cli> <kokod>
#
# <cli> is a release's tag, such as v0.3.0, whose koko for Linux and this
# machine's architecture is downloaded and checked against the release's
# SHA256SUMS, or the path of such a binary, as make dist wrote
# dist/koko-linux-<arch>. <kokod> is a release's tag, whose image
# ghcr.io/cieons/koko:<version> is pulled, or the path of the image's
# archive, as make image wrote dist/koko-image.tar, which skopeo loads. The
# skew workflow (.github/workflows/skew.yml) runs it for each pair
# release/skew-pairs.sh lists.
#
# In this order: the koko server starts from its image and initialises at
# its first start, on a Docker network of the check's own; docker exec
# issues a Join Code, and the CLI, in a container of its own, joins with it;
# koko space add, koko host add and koko host enroll make a throwaway sshd
# container a Managed Host; ssh web1.koko true, through koko's ssh_config,
# and koko exec web1 true reach it; against this build's kokod, a web
# terminal opened through the CLI's koko web, by cmd/skewterm as the page,
# answers a line and closes, so the forwarder is shown to carry the
# WebSocket unchanged; and a command that sends what the kokod lacks fails
# with server_too_old and its message, having changed nothing.
#
# The Host and the device are alpine containers: the Host installs and runs
# OpenSSH's sshd, with root as its Host Account, and the device installs
# OpenSSH's client, makes a key of its own, which the check adds to the
# Host's authorized_keys, and supplies it to koko host enroll, as a User
# who already logs in to the Host with a key does.
#
# It needs Docker, curl, jq and sha256sum, and skopeo and Go for an archive,
# on Linux or macOS, run from a clone of the commit, and reaches GitHub,
# ghcr.io, Docker Hub and alpine's package mirror. It removes the containers, the network, the files and
# the loaded image it made, and keeps the images it pulled.

set -eu

# current is the step under way, which a failure names.
current='reading the command line'

fail() {
	printf '\nskew-check: FAIL: %s: %s\n' "$current" "$1" >&2
	exit 1
}

step() {
	current=$1
	printf '\n=== %s\n' "$1"
}

[ $# -eq 2 ] || {
	echo 'usage: release/skew-check.sh <cli> <kokod>' >&2
	exit 2
}
cli=$1
kokod=$2

case $(uname -m) in
x86_64) arch=amd64 ;;
aarch64 | arm64) arch=arm64 ;;
*) fail "koko builds for amd64 and arm64, and this machine is $(uname -m)" ;;
esac
asset=koko-linux-$arch
releases=https://github.com/cieons/koko/releases/download
registry=ghcr.io/cieons/koko

# batch_since is the first release whose kokod takes several Hosts in one
# host.set (revision 2, ADR 0050): the newest thing this check knows that a
# kokod within the window may lack. When compat.Oldest passes it, name here
# the next command whose operation's revision rose.
batch_since=v0.5.0

network=koko-skew
server=koko-skew-server
host=koko-skew-host
device=koko-skew-device
local_image=koko-skew:local
alpine=alpine:3.22
address=$server:7822
work=$(mktemp -d)

# release tells whether $1 is a release's tag, and not a path.
release() {
	case $1 in
	v[0-9]*.[0-9]*.[0-9]*) [ ! -e "$1" ] ;;
	*) return 1 ;;
	esac
}

# below tells whether the release $1 is lower than the release $2.
below() {
	[ "$1" != "$2" ] && [ "$(printf '%s\n%s\n' "$1" "$2" | sort -V | head -n 1)" = "$1" ]
}

# On a failure, name the step, and show what the koko server and the
# device's Device Mux logged. Whatever the check made is removed either way.
on_exit() {
	status=$?
	if [ "$status" != 0 ]; then
		printf '\nskew-check: failed in the step: %s\n' "$current" >&2
		if docker inspect "$server" >/dev/null 2>&1; then
			printf '\n=== after the failure: docker logs %s\n' "$server"
			docker logs "$server" || :
		fi
		if docker inspect "$device" >/dev/null 2>&1; then
			printf '\n=== after the failure: the Device Mux log\n'
			docker exec "$device" sh -c 'cat /tmp/koko-*/*.log' || :
		fi
	fi
	docker rm -f -v "$server" "$host" "$device" >/dev/null 2>&1 || :
	docker network rm "$network" >/dev/null 2>&1 || :
	release "$kokod" || docker image rm "$local_image" >/dev/null 2>&1 || :
	rm -rf "$work"
	exit "$status"
}
trap on_exit EXIT

# on_device runs a command in the device's container, as its User would.
on_device() {
	docker exec "$device" "$@"
}

# refused checks that the command koko refused, whose exit status is $1 and
# whose stderr is in $work/err, failed with the error code $2 and a message
# that holds each argument after it.
refused() {
	status=$1 code=$2
	shift 2
	cat "$work/err" >&2
	[ "$status" = 1 ] || fail "it exited $status, not 1"
	jq -e --arg code "$code" '.error.code == $code' "$work/err" >/dev/null ||
		fail "it did not fail with $code"
	for s in "$@"; do
		jq -e --arg s "$s" '.error.message | contains($s)' "$work/err" >/dev/null ||
			fail "its message does not say: $s"
	done
}

step "the CLI: $cli"
if release "$cli"; then
	curl -fsSL -o "$work/$asset" "$releases/$cli/$asset" || fail "could not download $cli's $asset"
	curl -fsSL -o "$work/SHA256SUMS" "$releases/$cli/SHA256SUMS" || fail "could not download $cli's SHA256SUMS"
	want=$(awk -v name="$asset" '$2 == name { print $1 }' "$work/SHA256SUMS")
	[ -n "$want" ] || fail "$cli's SHA256SUMS lists no $asset"
	got=$(sha256sum "$work/$asset" | cut -d' ' -f1)
	[ "$got" = "$want" ] || fail "$cli's $asset is $got, but its SHA256SUMS lists $want"
	echo "$asset of $cli is $got, as its SHA256SUMS lists"
else
	[ -f "$cli" ] || fail "$cli is neither a release's tag nor a koko binary's path"
	cp "$cli" "$work/$asset"
fi
chmod 0755 "$work/$asset"

step "the kokod: $kokod"
if release "$kokod"; then
	image=$registry:${kokod#v}
	docker pull -q "$image" >/dev/null || fail "could not pull $image"
else
	[ -f "$kokod" ] || fail "$kokod is neither a release's tag nor an image archive's path"
	command -v skopeo >/dev/null || fail "loading an image's archive needs skopeo"
	image=$local_image
	skopeo copy --quiet "oci-archive:$kokod" "docker-daemon:$image" || fail "skopeo could not load $kokod"
fi
docker pull -q "$alpine" >/dev/null || fail "could not pull $alpine"

step "the koko server starts from its image and initialises at its first start"
docker network create "$network" >/dev/null
docker run -d --name "$server" --network "$network" \
	-e KOKO_OWNER=alice -e "KOKO_ADDRESS=$address" "$image" >/dev/null
# It answers koko device add over its socket only once it has started.
deadline=$(($(date +%s) + 60))
until docker exec "$server" koko device add --json >"$work/code" 2>"$work/err"; do
	running=$(docker inspect --format '{{.State.Running}}' "$server" 2>/dev/null || echo false)
	[ "$running" = true ] || fail "the container stopped before its koko server answered"
	[ "$(date +%s)" -lt "$deadline" ] || fail "koko device add was not answered within 60 s: $(cat "$work/err")"
	sleep 0.5
done
code=$(jq -r .code "$work/code")
[ -n "$code" ] && [ "$code" != null ] || fail "docker exec $server koko device add printed no Join Code"
kokod_version=$(docker exec "$server" kokod version --json | jq -r .version)
! release "$kokod" || [ "$kokod_version" = "$kokod" ] || fail "the image of $kokod holds kokod $kokod_version"

step "the CLI joins with the Join Code from docker exec"
docker run -d --init --name "$device" --network "$network" "$alpine" sleep infinity >/dev/null
docker cp -q "$work/$asset" "$device:/usr/local/bin/koko"
on_device apk add --no-cache -q openssh-client-default || fail "the device could not install OpenSSH's client"
cli_version=$(on_device koko version --json | jq -r .version)
! release "$cli" || [ "$cli_version" = "$cli" ] || fail "$cli's $asset is koko $cli_version"
echo "koko $cli_version with kokod $kokod_version"
printf '%s\n' "$code" | docker exec -i "$device" koko join - || fail "koko join failed"
on_device koko status --json >"$work/status" || fail "koko status failed after the join"
jq -e --arg address "$address" --arg version "$kokod_version" \
	'.server_address == $address and .server_version == $version and .server_error == null' "$work/status" >/dev/null ||
	fail "koko status does not report the koko server at $address as $kokod_version, reached: $(cat "$work/status")"

step "koko space add, koko host add and koko host enroll make the sshd container a Managed Host"
docker run -d --init --name "$host" --network "$network" "$alpine" sh -c '
	apk add --no-cache -q openssh-server && ssh-keygen -A >/dev/null &&
	mkdir -m 0700 /root/.ssh && exec /usr/sbin/sshd -D -e' >/dev/null
# sshd writes its pid file once it listens.
deadline=$(($(date +%s) + 60))
until docker exec "$host" test -s /run/sshd.pid 2>/dev/null; do
	running=$(docker inspect --format '{{.State.Running}}' "$host" 2>/dev/null || echo false)
	[ "$running" = true ] || { docker logs "$host" >&2 || :; fail "the Host's container stopped before its sshd listened"; }
	[ "$(date +%s)" -lt "$deadline" ] || fail "the Host's sshd did not listen within 60 s"
	sleep 0.5
done
on_device ssh-keygen -q -t ed25519 -N '' -f /root/enroll
on_device cat /root/enroll.pub | docker exec -i "$host" sh -c 'cat >/root/.ssh/authorized_keys'
on_device koko space add ops --json >/dev/null || fail "koko space add ops failed"
on_device koko host add web1 space=ops "address=$host" account=root --json >"$work/host" ||
	fail "koko host add web1 failed"
jq -e '.state == "registered"' "$work/host" >/dev/null || fail "koko host add did not record web1 as Registered: $(cat "$work/host")"
on_device koko host enroll web1 --key /root/enroll --json >"$work/host" || fail "koko host enroll web1 failed"
jq -e '.state == "managed" and .host_key != null' "$work/host" >/dev/null ||
	fail "koko host enroll did not make web1 Managed with a pinned host key: $(cat "$work/host")"

step "ssh web1.koko true, through koko's ssh_config"
on_device ssh -o BatchMode=yes web1.koko true || fail "ssh web1.koko true failed"

step "koko exec web1 true"
on_device koko exec web1 true || fail "koko exec web1 true failed"

step "a web terminal through koko web opens, answers a line and closes"
if release "$kokod"; then
	echo "the kokod is $kokod, and the forwarder of this CLI is the S1 tests' own: nothing to run"
else
	CGO_ENABLED=0 GOOS=linux GOARCH=$arch go -C "$(dirname "$0")/.." build -o "$work/skewterm" ./cmd/skewterm ||
		fail "could not build cmd/skewterm, the web terminal's page"
	docker cp -q "$work/skewterm" "$device:/usr/local/bin/skewterm"
	docker exec -d "$device" sh -c 'exec koko web --no-open --json >/tmp/web.json 2>/tmp/web.err'
	deadline=$(($(date +%s) + 30))
	until address=$(on_device cat /tmp/web.json 2>/dev/null | jq -er .address 2>/dev/null); do
		[ "$(date +%s)" -lt "$deadline" ] ||
			fail "koko web printed no address within 30 s: $(on_device cat /tmp/web.err 2>/dev/null || :)"
		sleep 0.5
	done
	id=$(on_device skewterm "$address" web1) || fail "the web terminal through koko web failed"
	deadline=$(($(date +%s) + 10))
	until docker logs "$server" 2>&1 | grep -qE "msg=\"ssh session ended\" tunnel=$id client=browser .* reason=\"client closed\""; do
		[ "$(date +%s)" -lt "$deadline" ] || fail "the koko server logged no end of the web terminal's SSH Session $id within 10 s"
		sleep 0.5
	done
	echo "the SSH Session $id answered through koko web $cli_version and ended as its page closed it"
fi

step "a command that sends what the kokod lacks fails with server_too_old"
if ! release "$kokod"; then
	echo "the kokod is this build, and a released CLI sends nothing it lacks: nothing to run"
elif ! below "$kokod" "$batch_since"; then
	echo "this check knows nothing a kokod from $batch_since on lacks: nothing to run"
elif release "$cli" && below "$cli" "$batch_since"; then
	echo "the CLI is $cli, from before $batch_since, and sends nothing the kokod lacks: nothing to run"
else
	on_device koko host add web2 space=ops "address=$host" account=root --json >/dev/null ||
		fail "koko host add web2 failed"
	status=0
	on_device koko host set web1 web2 provider=acme --json >"$work/out" 2>"$work/err" || status=$?
	refused "$status" server_too_old "$kokod_version" 'koko host set' 'upgrade the koko server'
	for name in web1 web2; do
		on_device koko host show "$name" --json >"$work/host" || fail "koko host show $name failed"
		jq -e '.attributes == {}' "$work/host" >/dev/null || fail "the refused koko host set changed $name: $(cat "$work/host")"
	done
fi

step "every check passed"
