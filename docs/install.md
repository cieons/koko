# Installing and running the koko server

[中文](install.zh-CN.md)

The koko server runs on one Linux machine with systemd, or in a container
from its image, `ghcr.io/cieons/koko` ([In a container](#in-a-container)).
This page covers installing it, checking a release, upgrading and going
back, keeping the Root Key, restoring onto another machine, and what to do
without the Root Key, then the same for the container. How each step works
is in the [installer](subsystems/installer.md) and
[release](subsystems/release.md) references.

## What the machine needs

- Linux on amd64 or arm64, with systemd 247 or later running it: Debian 11,
  Ubuntu 22.04, RHEL 9 or later. A container that systemd does not run is
  refused: run [the image](#in-a-container) there instead.
- `/var/lib/koko` on a local filesystem, not NFS or another network
  filesystem.
- A free TCP port, 7822 unless you pick another with `--port`.
- `curl`, `sha256sum`, `awk` and `mktemp`, and root, as the root user or
  through `sudo`.

## Installing

Run this on the machine, as root, naming yourself as the Owner and the
address your devices will reach it at:

```sh
curl -fsSL https://github.com/cieons/koko/releases/latest/download/install.sh | sh -s -- --owner alice --address koko.example.com
```

Not root? Pipe it to `sudo sh` instead of `sh`.

- **`--address`**: a DNS name is best. It is the Server Address every Join
  Code carries, and devices keep reaching the koko server through it when
  the machine's IP changes. Without `--address`, install takes the
  machine's public IP, and refuses a private one (10.x, 192.168.x,
  100.64.x and the like), since your devices could not reach it. Behind NAT
  that maps another port, write that port: `--address koko.example.com:8443`.
- **`--port`**: the port the koko server listens on, 7822 by default. Install
  never moves to another port by itself: when the port is taken, it names
  the process holding it and stops. A network that lets only web traffic
  out (ports 80 and 443) cannot reach 7822: pick `--port 443` if your
  devices sit behind one and nothing else on the machine holds 443.
- **`KOKO_VERSION`** pins a release instead of the latest:
  `curl … | sudo env KOKO_VERSION=v0.3.0 sh -s -- …`. Plain `sudo` drops
  the variable, hence `env`.
- **A `/tmp` mounted `noexec`**: install.sh runs the binary from a
  temporary directory, so point `TMPDIR` at one that allows running
  programs: `curl … | TMPDIR=/var/tmp sh -s -- …`, or through `sudo`,
  `curl … | sudo env TMPDIR=/var/tmp sh -s -- …`.

install.sh downloads the binary for the machine, checks it against the
release's `SHA256SUMS`, and runs it only when it matches. It prints:

- the Server Address and the host key's fingerprint;
- where the Root Key is and its id ([keep a copy](#keeping-the-root-key));
- the port to open: **open that TCP port in your provider's firewall**, since
  koko opens none itself;
- your first Join Code, which works once, within 15 minutes, and the
  `koko join` line to run with it.

On your own computer, put the koko CLI on your `PATH` (below), then run the
printed `koko join koko_…` line. Lost the code, or it expired? On the
machine, `koko device add` prints a new one.

### The CLI on your computer

The same release holds the CLI: `koko-darwin-arm64` for Apple Silicon Macs,
and `koko-linux-amd64` or `koko-linux-arm64` for Linux. For a Mac:

```sh
base=https://github.com/cieons/koko/releases/latest/download
curl -fsSL -O "$base/koko-darwin-arm64" -O "$base/SHA256SUMS"
grep ' koko-darwin-arm64$' SHA256SUMS | shasum -a 256 -c
sudo install -m 0755 koko-darwin-arm64 /usr/local/bin/koko
```

## Checking a release

Every file of a release is attested by the GitHub workflow that built it.
With the [GitHub CLI](https://cli.github.com), check that a downloaded file
was built by koko's release workflow:

```sh
gh attestation verify koko-linux-amd64 --repo cieons/koko
```

This works for every file `SHA256SUMS` lists, `install.sh` included. The
koko server's machine needs no `gh`: install.sh checks the SHA-256 alone.

## Upgrading

Run the install line again, without `--owner` and without `--address`:

```sh
curl -fsSL https://github.com/cieons/koko/releases/latest/download/install.sh | sudo sh -s --
```

It replaces the binary, writes the systemd unit again and restarts the koko
server, which keeps its record, its Owner, its Server Address and its keys.
Every open SSH Session ends, with a line saying the koko server is
restarting. `--owner` and `--address` are refused on an upgrade: change the
Server Address with `koko server set address=<host[:port]>` instead.

The upgrade keeps the port the koko server listens on, even one set with a
drop-in (`systemctl edit koko`); after a drop-in moves it below 1024, run
the install line again, which lets the koko server bind it. `--port` moves
it. When the Server Address names no port of its own, devices follow the
listen port, so after a move the output names the `koko connect
<host:port>` line to run on each device.

When the new koko changes the database's schema, the koko server first
copies the database as it was to `/var/lib/koko/koko.db.before-<n>`, the
snapshot, and keeps only the latest one.

## Going back

To go back to the koko you upgraded from, install its release. On the
machine, as root:

```sh
curl -fsSL https://github.com/cieons/koko/releases/download/v0.3.0/install.sh | env KOKO_VERSION=v0.3.0 sh -s --
```

with the version you upgraded from in place of `v0.3.0`. When the upgrade
changed no schema, that is all. When it did, the older koko refuses to
install over the newer database and writes nothing, while the newer koko
server keeps running. Its refusal names the snapshot to put in place:

```text
koko: /var/lib/koko/koko.db: it is at schema version 14, which this koko (schema version 13) predates: run the newer koko that migrated it, or go back by putting koko.db.before-14 in place of koko.db; nothing was written
```

Put it in place, then install the older release again:

```sh
systemctl stop koko
cd /var/lib/koko
mkdir /var/lib/koko-newer
mv koko.db /var/lib/koko-newer/
mv koko.db-wal koko.db-shm /var/lib/koko-newer/ 2>/dev/null # there only after a crash
mv koko.db.before-<n> koko.db
curl -fsSL https://github.com/cieons/koko/releases/download/v0.3.0/install.sh | env KOKO_VERSION=v0.3.0 sh -s --
```

with the snapshot the refusal named in place of `koko.db.before-<n>`, and
the same version as above in place of `v0.3.0`. Anything written after the
upgrade is lost with the newer database, which stays in
`/var/lib/koko-newer`.

## Keeping the Root Key

`/etc/koko/root.key` seals every Secret and the koko server's own keys.
Without it, nothing sealed can be opened again, so **keep a copy off the
machine**, such as in a password manager. The file is one line of text
that names its own id, `rootkey_…`, which `koko server show` also prints:

```sh
sudo cat /etc/koko/root.key
```

A copy of `/var/lib/koko` without the Root Key reveals no Secret and no
private key, so it is safe to keep anywhere; a [restore](#restoring-onto-another-machine)
needs both.

## Restoring onto another machine

koko has no backup command yet. A copy of the data directory taken while the
koko server is stopped, kept with the Root Key, restores it:

1. On the old machine: `systemctl stop koko`, copy `/var/lib/koko` as a
   whole directory, and keep it with your copy of the Root Key: a restore
   needs both.
2. On the new machine, as root, put both back: the directory at
   `/var/lib/koko`, and the Root Key at `/etc/koko/root.key`, owned by root
   with mode 0600, in `/etc/koko`, mode 0700.
3. Run the install line without `--owner`, adding `--port <port>` if the old
   koko server listened on another port than 7822. It finds the koko
   server's record and starts it.

The host key travels in the data directory, so devices still trust the koko
server. If its address changed, set the new one with
`koko server set address=<host[:port]>`, and run
`koko connect <host:port>` on each device.

systemd gives the data directory and everything in it to the `koko` user
when the directory itself belongs to someone else, as a copy put back by
root does. If you copied the files into a `/var/lib/koko` that already
belongs to `koko`, run `chown -R koko:koko /var/lib/koko` before installing.

## Without the Root Key

If the Root Key and every copy of it are lost, the record cannot be opened
again. The only way on is to start over: move `/var/lib/koko` aside, along
with any `/etc/koko/root.key` that does not load, and install again with
`--owner`. That costs everything sealed and everything joined: every Host
is added and Enrolled anew, every Secret is added again, and every device
runs `koko leave`, then joins with a new Join Code.

## Uninstalling

`koko server uninstall` stops the koko server and removes its unit and
binary. It keeps the data directory, the Root Key and the `koko` user, and
prints the port it listened on: install again without `--owner`, with that
`--port`, to run it on the same data.

## In a container

The image `ghcr.io/cieons/koko` runs the koko server where systemd does
not, such as beside your other containers in Docker. Each release pushes
it for amd64 and arm64, holding that release's own `koko-linux-<arch>`
binary. The container starts as root only to give its one directory to
uid 65532, then serves as uid 65532 with no capability. Why it works this
way is in [ADR 0037](adr/0037-the-container-keeps-its-data-and-its-root-key-in-one-directory.md)
and [ADR 0038](adr/0038-a-container-initialises-at-its-first-start-and-gives-its-first-join-code-to-docker-exec.md).

### What the Docker host needs

- Docker with Compose v2 (`docker compose`), on amd64 or arm64.
- A directory for koko, `/srv/koko` below, on a local filesystem, not NFS
  or another network filesystem: the koko server refuses one at start.
- A free TCP port to publish, 7822 below. **Open it in your provider's
  firewall**, since koko opens none itself.

Nothing else is prepared on the Docker host: no user, no key, no
directory. Docker makes `/srv/koko` when it is missing, and the container
gives it to uid 65532 with mode 0700, so on the Docker host only root can
read it.

### Starting it

Save [`release/compose.yaml`](../release/compose.yaml) as `compose.yaml` in
a directory of its own:

```yaml
# The koko server in a container: see docs/install.md, "In a container".
# Before the first start, beside this file:
#   echo KOKO_VERSION=0.2.0 > .env    # the release to run, without its v
# After it: docker exec koko koko device add    # your first Join Code
services:
  koko:
    image: ghcr.io/cieons/koko:${KOKO_VERSION:?set KOKO_VERSION in .env, such as KOKO_VERSION=0.2.0}
    container_name: koko
    restart: unless-stopped
    stop_grace_period: 30s
    # Root only long enough to give /koko to uid 65532, then never again.
    cap_drop: [ALL]
    cap_add: [CHOWN, DAC_OVERRIDE, FOWNER, SETGID, SETUID]
    security_opt: [no-new-privileges:true]
    ports:
      - "7822:7822"        # with another host port, name it in KOKO_ADDRESS
    volumes:
      - /srv/koko:/koko    # the data and the Root Key; Docker makes it if missing
    environment:
      KOKO_OWNER: alice                  # read only at the first start
      KOKO_ADDRESS: koko.example.com     # host[:published port]; read only at the first start
```

Put yourself as the Owner in `KOKO_OWNER`, and in `KOKO_ADDRESS` the
address your devices will reach the koko server at: a DNS name is best, as
for [`--address`](#installing). Then, beside the file:

```sh
echo KOKO_VERSION=0.2.0 > .env     # the release to run, without its v
docker compose up -d
docker exec koko koko device add   # your first Join Code
```

`KOKO_VERSION` is required, so the container always runs a release you
chose. At its first start the koko server makes the Root Key,
`/srv/koko/root.key`, and initialises itself. `docker logs koko` then says
`koko server initialised`, with the host key's fingerprint, the listen
address, the Root Key's path and id, and the `koko device add` line to run
next. No log line holds the Root Key or a Join Code. A fresh koko server
without `KOKO_ADDRESS` refuses to start, naming it: the container's own IP
is private, and no device could reach it.

`koko device add` prints your first Join Code, which works once, within 15
minutes, and the `koko join` line to run with it on your computer
([the CLI on your computer](#the-cli-on-your-computer)). Lost the code, or
it expired? Run the same line again, as you do after losing every device.

Without Compose, the same container with `docker run`:

```sh
docker run -d --name koko --restart unless-stopped --stop-timeout 30 \
  --cap-drop ALL --cap-add CHOWN --cap-add DAC_OVERRIDE --cap-add FOWNER \
  --cap-add SETGID --cap-add SETUID --security-opt no-new-privileges:true \
  -p 7822:7822 -v /srv/koko:/koko \
  -e KOKO_OWNER=alice -e KOKO_ADDRESS=koko.example.com \
  ghcr.io/cieons/koko:0.2.0
```

### Owner commands

`docker exec koko koko <command>` runs the koko CLI inside the container,
where it reaches the koko server over its Unix socket as the Local caller,
which acts as the Owner. So the Owner needs no device to act:

```sh
docker exec koko koko server show
docker exec koko koko server set address=<host[:port]>
docker exec koko koko device add
docker exec koko koko user invite bob
```

The Docker host's own shell is not a Local caller, since the socket stays
inside the container: from the Docker host, use `docker exec`, or join it
as a device. Mounting the container's `/run/koko` onto the Docker host is
not supported.

### The port

The container listens on 7822, so any other port, 443 included, is only a
port mapping on the Docker host. When the published port is 7822,
`KOKO_ADDRESS: koko.example.com` is enough. When it differs, name the
published port in the address, since it is the port devices dial:

```yaml
    ports:
      - "443:7822"
    environment:
      KOKO_ADDRESS: koko.example.com:443
```

`KOKO_OWNER` and `KOKO_ADDRESS` are read only at the first start. A later
start whose values differ from the record logs a warning and changes
nothing. To move the published port later, change the mapping and
`KOKO_ADDRESS`, run `docker compose up -d`, then
`docker exec koko koko server set address=koko.example.com:<port>`, and
`koko connect koko.example.com:<port>` on each device.

With `network_mode: host`, the container's port is the machine's: leave
out `ports:`, and set `KOKO_LISTEN` (such as `:8443`) to listen on another
port than 7822. It must be 1024 or above, since the koko server holds no
capability. When `KOKO_ADDRESS` names no port, devices dial the one the
koko server listens on.

### Backups and the Root Key

`/srv/koko` holds everything the container keeps: the Root Key,
`root.key`, one line that names its own id, and the data directory,
`data/`, with `koko.db` and the latest `koko.db.before-<n>` snapshot.

The Root Key seals every Secret and the koko server's own keys. A copy of
`data/` alone reveals no Secret and no private key. **A copy of the whole
directory holds the record and its key together**, and the koko process can
read, change or delete `root.key`, which systemd prevents on an installed
koko server. So:

1. Right after the first start, copy `root.key` off the machine, such as
   into a password manager. It is one line: `sudo cat /srv/koko/root.key`.
   `docker exec koko koko server show` prints its id.
2. Back up `data/` on its own, copied as root while the container is
   stopped (`docker compose stop`, then `docker compose start`). That copy
   can be kept anywhere.
3. Keep a copy of the whole directory only where you would keep the Secrets
   themselves.
4. A disk snapshot of the machine holds both, but your provider controls
   the machine already.

The koko server never makes a Root Key over sealed data: a missing or wrong
`root.key` is refused at start, naming the id the data needs. Without any
copy of it, the way on is the one in
[Without the Root Key](#without-the-root-key): move `data/` aside and start
again.

### Upgrading the container

Change `KOKO_VERSION` in `.env`, then run `docker compose up -d` again.
Compose pulls the new image and replaces the container, which keeps its
record, its Owner, its Server Address and its keys in `/srv/koko`. Every
open SSH Session ends, with a line saying the koko server is restarting.
When the new koko changes the database's schema, the koko server first
copies the database as it was to `/srv/koko/data/koko.db.before-<n>`, and
keeps only the latest snapshot.

With `docker run`, run `docker stop koko` and `docker rm koko`, which keep
`/srv/koko`, then the `docker run` line with the new version.

### Going back in a container

To go back to the koko you upgraded from, set its version in `.env` and run
`docker compose up -d`. When the upgrade changed no schema, that is all.
When it did, the older image refuses at start and writes nothing, and
`docker logs koko` names the version it needs and the snapshot to put in
place. Put it in place on the Docker host, in a root shell (`sudo -s`),
beside `compose.yaml`:

```sh
docker compose down
mkdir /srv/koko-newer
mv /srv/koko/data/koko.db /srv/koko-newer/
mv /srv/koko/data/koko.db-wal /srv/koko/data/koko.db-shm /srv/koko-newer/ 2>/dev/null # there only after a crash
mv /srv/koko/data/koko.db.before-<n> /srv/koko/data/koko.db
docker compose up -d
```

with the snapshot the refusal named in place of `koko.db.before-<n>`.
Anything written after the upgrade is lost with the newer database, which
stays in `/srv/koko-newer`.

### Restoring or moving the container

1. On the old machine: `docker compose down`, then copy `/srv/koko` as a
   whole directory, as root. Or copy `data/` alone and keep it with your
   copy of `root.key`: a restore needs both.
2. On the new machine, put both back at `/srv/koko`, `root.key` beside
   `data/`, with any owner: the container gives them to uid 65532 when it
   starts.
3. Run `docker compose up -d` there, with the same `compose.yaml` and
   `.env`.

The host key travels sealed in `data/`, so devices still trust the koko
server. If its address changed, run
`docker exec koko koko server set address=<host[:port]>`, and
`koko connect <host:port>` on each device.

### Moving from systemd

`koko server uninstall` stops an installed koko server and keeps its data
directory and Root Key. Put them in the container's directory, one that
holds no `data/` yet, as root:

```sh
koko server uninstall
mkdir -p /srv/koko
cp -a /var/lib/koko /srv/koko/data
cp /etc/koko/root.key /srv/koko/root.key
```

Then start the Compose file as above, without `docker exec … device add`:
the devices you joined keep working. Set `KOKO_OWNER` and `KOKO_ADDRESS`
to its Owner and Server Address, or each start logs a warning that they
differ. If the installed koko server listened on another port than 7822,
publish that port (`"443:7822"` for 443), and, when its Server Address
names no port, name it, in `KOKO_ADDRESS` and with
`docker exec koko koko server set address=koko.example.com:443`.

Going the other way, put `data/` at `/var/lib/koko` and `root.key` at
`/etc/koko/root.key`, then follow
[Restoring onto another machine](#restoring-onto-another-machine) from
step 2, with `--port` set to the published port when it was not 7822.

### Checking the image

Each image carries a GitHub artifact attestation, which the
[GitHub CLI](https://cli.github.com) checks:

```sh
gh attestation verify oci://ghcr.io/cieons/koko:0.2.0 --repo cieons/koko
```

The image's `/usr/local/bin/koko` is the release's `koko-linux-<arch>`
byte for byte, the file `SHA256SUMS` lists.

### Starting as uid 65532

Where an image may not start as root, such as under Kubernetes'
`runAsNonRoot`, run it as uid 65532 from the start, on a directory that
uid 65532 can already write: `user: "65532:65532"` in the Compose file,
`docker run --user 65532:65532`, or a pod with `runAsUser: 65532`,
`runAsGroup: 65532` and `fsGroup: 65532`. A directory on the Docker host
is made first, as root: `install -d -o 65532 -g 65532 -m 0700 /srv/koko`.
The koko server
then prepares nothing, drops nothing and needs no capability. A directory
it cannot write is refused at start, naming the directory and the uid and
gid it runs as. Another uid works only where both `/koko` and the image's
`/run/koko` are writable by it.

### The Root Key kept apart

To keep the Root Key out of the directory you back up, mount it read-only
from elsewhere and point `KOKO_ROOT_KEY_FILE` at it. After the first start,
run `docker compose down`, then, as root, move `/srv/koko/root.key` into a
directory of its own, say `/etc/koko-key/root.key` (`mkdir /etc/koko-key`
first), still uid 65532's, and `chmod 0400` it. Then add
`/etc/koko-key/root.key:/run/secrets/koko-root.key:ro` to `volumes:` and
`KOKO_ROOT_KEY_FILE: /run/secrets/koko-root.key` to `environment:`, and run
`docker compose up -d`. The koko server leaves a file on a read-only mount
as it is; one it cannot read is refused at start.
