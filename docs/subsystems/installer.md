# The installer

`koko server install`, which installs, starts and initialises the koko server
on a fresh Linux machine with systemd, as root, and upgrades one initialised
already; and `koko server uninstall`, which removes it and keeps its data; in
[`internal/server/installer`](../../internal/server/installer). Why the koko
server installs itself is
[ADR 0020](../adr/0020-the-koko-server-installs-itself-and-has-no-configuration-file.md);
the order, the paths and the unit's settings are P0 spec: install and run.

## The command and the installer

`koko server install [--owner <user>] [--address <host[:port]>] [--port <port>]`
is in the irreversible tier: it changes the machine and prints a credential,
the first Join Code. It never prompts. The CLI
([`server.go`](../../internal/client/cli/server.go)) refuses a
`--port` outside 1 to 65535, an `--owner` that is not a User name and an
`--address` that is not a Server Address with exit 2, before anything else.
`--port` has no fixed default: a port not given reaches the installer as 0,
which is 443 on a fresh machine and the port the koko server listens on now
on [an upgrade](#the-upgrade). The CLI then hands the rest to the installer
through `cli.Runner.ServerInstall`, which `cmd/koko` sets in Linux builds only
([code layout](code-layout.md#the-package-groups)); a build without it refuses
with `unsupported_platform`, as `koko server run` does. The two sides agree on
the shared [`install`](../../internal/shared/install/install.go) types: the
request, the report, the function that calls `server.init` over the socket
(the installer owns no API client), and `install.Refusal`, whose reason the
CLI turns into its code. `koko server uninstall`, also irreversible, takes no
flag but `--json` and dispatches the same way, through
`cli.Runner.ServerUninstall` ([uninstall](#uninstall)).

## The machine interface

The installer reaches the machine only through an
[`installer.Machine`](../../internal/server/installer/installer.go): a root
prefixed to every path it reads or writes, and what only the machine answers
or does. [`installer.System`](../../internal/server/installer/system.go) is
the real one, rooted at `/`:

| Method | On the machine |
| --- | --- |
| `SystemdVersion` | the first line of `systemctl --version` |
| `FilesystemType` | the latest mount on the longest mount point holding the path, in `/proc/self/mountinfo` |
| `PortOwner` | [`portowner.Lookup`](web-front.md#the-port-owner-lookup), which as root names any user's process |
| `SourceIP` | the source address of a UDP socket connected to a documentation address (192.0.2.1, then 2001:db8::1), which only the default route covers; connecting sends nothing, and no one else is asked |
| `AddUser` | `useradd --system --user-group --no-create-home --home-dir /var/lib/koko --shell /usr/sbin/nologin --comment "koko server" koko`, unless `koko` exists |
| `Chown` | `os.Chown` to the user and its primary group |
| `Systemctl` | `systemctl` with the arguments |
| `ServiceShow` | `systemctl show --property=<property> --value koko.service`, as systemd has the unit loaded: `MainPID` (0 while it does not run) and `Environment` (its assignments, each quoted as systemd quotes one a shell would take) |

## The steps

In this order ([`Installer.Install`](../../internal/server/installer/installer.go)):

1. **Every check, before any write.** Each refusal below says "nothing was
   written"; all but the schema's are an `install.Refusal`, whose reason the
   CLI turns into its code:
   - systemd not running the machine, as `sd_booted(3)` tells it: no
     `/run/systemd/system` directory under the root, as in a container
     whose init is something else, where `systemctl` may still be there and
     would fail only at `daemon-reload`, after the writes:
     `unsupported_platform`, before install asks the machine anything;
   - systemd older than 247, which `LoadCredential=` needs:
     `unsupported_platform`;
   - a data directory on a network filesystem (NFS, SMB, Ceph, 9p, sshfs and
     the like; `networkFilesystem`), where SQLite's WAL does not work:
     `unsupported_platform`. Before `/var/lib/koko` exists, the filesystem it
     will be made on is checked;
   - a `koko.db` at a schema this koko predates, which the koko server would
     refuse to start on, as when an older koko is installed to go back
     before the snapshot is put back: `internal_error`, naming both schema
     versions and the snapshot to put in place
     ([`store.CheckVersion`](store.md));
   - a koko server initialised already (below) makes the run
     [the upgrade](#the-upgrade), whose own checks follow;
   - no `--owner` for one not initialised yet: exit 2;
   - the port held by another process, named, with the hint to pass
     `--port`: `port_taken`. Install never moves to another port. The koko
     service's own process holding it is no refusal, since the restart frees
     it: install asks systemd for the service's `MainPID` and compares it
     with the PID holding the port, so another process, even one named
     `koko` or run by the `koko` user, is refused;
   - without `--address`, a default route's source IP that is not public,
     or none: exit 2, with the hint to pass `--address`. The address check
     ([`isPublic`](../../internal/server/installer/checks.go)) is a pure
     function: RFC 1918, CGNAT (100.64.0.0/10) and IPv6 ULA are private, and
     so are loopback, link-local, multicast and unspecified addresses. The IP
     becomes the Server Address without a port, an IPv6 one in brackets.

   A Root Key file that is there but does not load stops install too, before
   any write, with `seal.Load`'s error naming it (`internal_error`); it is
   left as it is, never replaced.
2. **The `koko` user and the directories:** `/etc/koko` (0700, root), given
   its mode and owner at every run, whatever the umask or a copy put back
   made them, and `/var/lib/koko` (0700, `koko`). The data directory is given
   its owner only when install makes it; one there already, such as a copy
   put back, gets its mode, and its owner from the unit's `StateDirectory=`
   at the start, which gives the directory and everything in it to `koko`
   when the directory is not `koko`'s. A chown of the directory alone would
   make systemd leave what is in it as the copy left it.
3. **The Root Key**, `/etc/koko/root.key` (0600, root): reused when it is
   there, else written new (`seal.NewRootKey`).
4. **The binary**, `/usr/local/bin/koko` (0755, root), copied from the
   running one unless it runs from there, and **the unit**,
   `/etc/systemd/system/koko.service` (0644, root). Every file is written
   through a temporary file beside it, given its mode and owner and synced,
   then renamed into place, so none is seen half-written and a running binary
   is replaced, never changed.
5. **`systemctl daemon-reload`, `enable koko.service`, `restart
   koko.service`.** Restart, not start, so that a koko service an earlier run
   left running takes the new unit and binary.
6. **The socket.** Install waits until the koko server accepts a connection
   on `/run/koko/koko.sock`, for up to `timing.InstallSocketWait` (30 s). It
   connects rather than looks for the file, which is there a moment before
   the koko server listens on it. A koko server that
   refuses to start never opens it, so the failure names
   `systemctl status koko` and `journalctl -u koko`, where its refusal is.
7. **Initialisation** over the socket ([`server.init`](initialisation.md)),
   with the Owner and the Server Address.

Until initialisation has succeeded, running install again is safe and
finishes the job: every step is done again or found done, and nothing is
sealed under the Root Key yet, so it is reused.

## What is there already

Install knows whether the data directory holds an initialised koko server
before the service runs by reading `koko.db` itself, as root, with
[`store.Peek`](store.md#reading-it-as-it-is): read-only, without migrating it,
beside a koko service that may run. It is initialised once the `deployment`
record exists ([initialisation](initialisation.md#the-record)); no
`koko.db`, or one no migration step has reached, is not. A database it cannot
read stops the install, never taken for an empty one.

On an initialised koko server install is the upgrade.

## The upgrade

Re-running install on an initialised koko server, as a newer release's
`install.sh` does, replaces the binary, writes the unit again from the
template, and runs `daemon-reload`, `enable` and `restart`, then waits for the
socket, as a fresh install does ([the steps](#the-steps) 2 to 6). It
initialises nothing and issues no Join Code: the restarted koko server keeps
its record, its Owner, its Server Address and its keys, and migrates its
schema at start ([the store](store.md)). Its own checks, after the machine's
and before any write, each leaving the running koko server as it was:

- **`--owner` and `--address`** are refused with `already_initialised`
  (exit 1): only initialisation takes them. The message names the way on,
  `koko device add` for a Join Code and `koko server set address=` for the
  Server Address.
- **The port.** Without `--port` the upgrade keeps the port the koko service
  listens on, the last `KOKO_LISTEN` in its `Environment` as systemd has the
  unit loaded, so that a port set with a drop-in (`systemctl edit koko`)
  counts as one install set; with it, the koko server moves to the port
  given. A drop-in in `/etc/systemd/system/koko.service.d/` whose
  `Environment=` lines set `KOKO_LISTEN` overrides the unit at the restart,
  so install reads those files itself (systemd loads none without the unit,
  and uninstall keeps them): the koko server listens on the port the last
  one sets, and a `--port` naming another is refused with exit 2, naming the
  drop-in. Without a unit or such a drop-in, as after
  [uninstall](#uninstall) or on a restore, the port the koko server
  listened on is unknown, and it listens on 443 or the port given. The port
  is then checked as on a fresh machine, the service's own process allowed.
  Install never reads its own unit: it writes it again at every run. It
  reads `KOKO_LISTEN` from `Environment=` settings only, not from an
  `EnvironmentFile=`.
- **The Root Key.** Install loads `/etc/koko/root.key` and opens the koko
  server's sealed keys with it, as the restarted koko server will. A file
  that is missing, does not parse or names another Root Key is refused
  (`internal_error`), naming the Root Key id the sealed data needs, and left
  as it is: no Root Key is ever written over sealed data.

An upgrade that leaves the koko server on another port than it listened on,
or on a port it cannot know was the same (no unit), says that devices joined
before must take the new address by hand, with `koko connect <host:port>`,
when the Server Address names no port of its own and so follows the listen
port (ADR 0020). A Server Address with its own port stays where devices
connect, and nothing is said.

## The unit

[`koko.service.tmpl`](../../internal/server/installer/koko.service.tmpl) is
embedded in the binary and rendered at every run
([`installer.Unit`](../../internal/server/installer/unit.go)), so the unit
always matches the binary; the
[golden file](../../internal/client/cli/testdata/server-install-koko.service)
pins its text, with every setting the spec lists. Two choices are the
installer's own:

- **The startup settings are `Environment=KOKO_*` lines, not flags**, so that
  a drop-in's own `Environment=` line overrides one and outlasts the next
  install, which rewrites the unit; a flag in `ExecStart=` would beat it.
  `KOKO_LISTEN` carries `--port`, for `koko server run`'s listen address.
- **The Root Key's path is not written**: `koko server run`'s default,
  `$CREDENTIALS_DIRECTORY/root-key`, is the file `LoadCredential=` makes,
  which is where systemd hands the key to the service and nowhere else.

## What it prints

The Server Address with its port, the host key's fingerprint, the Root Key's
path and id with the reminder to keep a copy off the machine, the port to
open in the provider's firewall, the line saying to run [`koko ssh
setup`](device-connection.md#on-the-koko-servers-own-machine) once as each
user who runs `ssh <host>.koko` on that machine, and the first Join Code,
for the Owner's `device-1`, with its 15 minutes and the `koko join` line.
Never the Root Key.
With `--json`:

```json
{
  "upgraded": false,
  "address": "203.0.113.7",
  "host_key_fingerprint": "SHA256:...",
  "root_key_file": "/etc/koko/root.key",
  "root_key_id": "rootkey_3f9k2m7q",
  "listen_port": 443,
  "join_code": {"id": "...", "code": "koko_...", "user": "alice", "device": "device-1", "expires_at": "..."}
}
```

`address` is the Server Address as stored: without a port, its port is
`listen_port`. `upgraded` is `false` here. An upgrade prints the same
fields without `join_code`, with `upgraded: true`, and, when devices joined
before must take the new address, `devices_must_connect` with it:

```json
{
  "upgraded": true,
  "address": "203.0.113.7",
  "host_key_fingerprint": "SHA256:...",
  "root_key_file": "/etc/koko/root.key",
  "root_key_id": "rootkey_3f9k2m7q",
  "listen_port": 8443,
  "devices_must_connect": "203.0.113.7:8443"
}
```

For people, an upgrade prints the same lines but the Root Key reminder and
the Join Code, and then the one `koko connect` line to run on each device
when devices must take the new address.

## Uninstall

[`Installer.Uninstall`](../../internal/server/installer/uninstall.go), when
the unit is there, reads the port the koko service listens on from systemd,
runs `systemctl stop` and `disable koko.service` while systemd still knows
the unit, removes the unit and runs `daemon-reload`; it then removes the
binary. It keeps `/var/lib/koko`, `/etc/koko/root.key` and the `koko` user,
and prints their paths, with the port, which a later install no longer
knows: the hint for people says to install koko again without `--owner`
and with that `--port`. There is no purge: P0 has no backup, and one command
would erase the record, the Secrets and the koko key. Drop-ins are the
Owner's and stay. What is not there is not removed, so running it again
removes nothing and asks systemd nothing. Install without `--owner` then
runs the koko server again on the data it kept, as
[the upgrade](#the-upgrade). With `--json` (`listen_port` left out when
systemd did not tell it):

```json
{
  "removed": ["/etc/systemd/system/koko.service", "/usr/local/bin/koko"],
  "data_dir": "/var/lib/koko",
  "root_key_file": "/etc/koko/root.key",
  "system_user": "koko",
  "listen_port": 443
}
```

## Verifying

[`server_install_test.go`](../../internal/client/cli/server_install_test.go)
runs `koko server install` through the CLI on
[`harness.NewMachine`](../../internal/harness/machine.go), a fake machine: a
temporary root holding only the system directories (`/run/systemd/system`
among them, as systemd runs the machine), and fakes that answer
what the test sets (systemd's version, the filesystem type, the processes on
ports, the source IP) and record every call with the files under the root at
that moment. Its `systemctl` acts as systemd does with the unit: it runs only
the unit loaded, with its drop-ins, at the last `daemon-reload`, refuses a
binary that does not run or a credential's missing file, makes the runtime
directory, gives the state directory and all in it to the unit's `User=`
unless the directory is that user's already (as `StateDirectory=` does),
shows the `MainPID` and the quoted `Environment` systemd would, and runs
the unit's `ExecStart` with its `Environment=` lines and
`CREDENTIALS_DIRECTORY` through `koko server run` as an in-process koko
server under the root,
listening on a loopback port, which its Join Codes therefore name in place
of the unit's port. So initialisation and the printout run for real. The tests cover a fresh install (files, modes, owners, the unit's
golden file, the `--json` golden, the koko server initialised), the order
(checks before any write, the Root Key and the binary before the start, a
late socket waited for), a socket file there before the koko server accepts
on it (`ListenDelay`), the printout for people, `--port`, `--address`,
every refusal writing nothing, systemd not running, private source IPs, bad
flags, running again after a refused address and after a service that never
opened its socket, and a Root Key file there already. Only a service that
never opens its socket (`NoSocket`) has install wait less than the product's
30 s, `harness.NoSocketWait`: no wait would see that socket, so a short one
fails no slower machine, and every socket that does open is waited for as
on a real machine. `WaitLog` waits for a line the koko server logs after
its socket accepts, such as its start.
[`server_upgrade_test.go`](../../internal/client/cli/server_upgrade_test.go)
tests the upgrade on an initialised koko server: the new release's binary,
the unit written again, the restart, the record kept, the `--json` goldens;
`--owner` and `--address` refused; the port kept without `--port`, moved with
it, and the warning only under a Server Address without a port of its own;
the service's own port allowed and another process's refused, a `koko`-named
one included; a missing, another or a damaged Root Key refused; a port set
with a drop-in kept, and a `--port` it would override refused, before and
after uninstall; no unit after
uninstall, and a unit changed by hand; the P0 restore of a copy onto a fresh
machine; and data at a newer schema refused.
[`server_uninstall_test.go`](../../internal/client/cli/server_uninstall_test.go)
tests uninstall: the service stopped while its unit is there, the unit and
the binary gone, what it keeps, the port, the golden, running it again,
install after it, and the build without the koko server. The fake machine's
`PortOwner` names the koko service on the port its loaded unit's
`KOKO_LISTEN` names while it runs, and `NewRelease` gives install a newer
binary.
[`checks_test.go`](../../internal/server/installer/checks_test.go),
[`system_test.go`](../../internal/server/installer/system_test.go) and
[`unit_test.go`](../../internal/server/installer/unit_test.go) test the
address check, the network filesystems, reading systemd's version, reading
mountinfo and reading the port from the unit's environment as systemd shows
it. The real machine's own calls run in
[the real-systemd check](release.md#the-real-systemd-check), all but
`SourceIP`, since every install there passes `--address`, and a
`PortOwner` naming another process than the koko service.
