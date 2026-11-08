# Installing and running the koko server

[中文](install.zh-CN.md)

The koko server runs on one Linux machine with systemd. This page covers
installing it, checking a release, upgrading and going back, keeping the
Root Key, restoring onto another machine, and what to do without the Root
Key. How each step works is in the [installer](subsystems/installer.md) and
[release](subsystems/release.md) references.

## What the machine needs

- Linux on amd64 or arm64, with systemd 247 or later running it: Debian 11,
  Ubuntu 22.04, RHEL 9 or later. A container that systemd does not run is
  refused.
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

To go back to the koko you upgraded from, put the snapshot in place, then
install the older release. An upgrade that left no `koko.db.before-<n>`
changed no schema: then just install the older release, the last line
below. On the machine, as root:

```sh
systemctl stop koko
cd /var/lib/koko
mkdir /var/lib/koko-newer
mv koko.db /var/lib/koko-newer/
mv koko.db-wal koko.db-shm /var/lib/koko-newer/ 2>/dev/null # there only after a crash
mv koko.db.before-* koko.db
curl -fsSL https://github.com/cieons/koko/releases/download/v0.3.0/install.sh | env KOKO_VERSION=v0.3.0 sh -s --
```

with the version you upgraded from in place of `v0.3.0`. Anything written
after the upgrade is lost with the newer database, which stays in
`/var/lib/koko-newer`.
An older koko refuses to install over a database newer than itself before
it writes anything, naming the snapshot to put in place.

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
