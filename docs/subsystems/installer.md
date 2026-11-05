# The installer

`koko server install`, which installs, starts and initialises the koko server
on a fresh Linux machine with systemd, as root, in
[`internal/server/installer`](../../internal/server/installer). Why the koko
server installs itself is
[ADR 0020](../adr/0020-the-koko-server-installs-itself-and-has-no-configuration-file.md);
the order, the paths and the unit's settings are P0 spec: install and run.
Upgrading an initialised koko server and `koko server uninstall` are #43's.

## The command and the installer

`koko server install [--owner <user>] [--address <host[:port]>] [--port <port>]`
is in the irreversible tier: it changes the machine and prints a credential,
the first Join Code. It never prompts. The CLI
([`server.go`](../../internal/client/cli/server.go)) refuses a
`--port` outside 1 to 65535, an `--owner` that is not a User name and an
`--address` that is not a Server Address with exit 2, before anything else.
It then hands the rest to the installer through `cli.Runner.ServerInstall`,
which `cmd/koko` sets in Linux builds only
([code layout](code-layout.md#the-package-groups)); a build without it refuses
with `unsupported_platform`, as `koko server run` does. The two sides agree on
the shared [`install`](../../internal/shared/install/install.go) types: the
request, the report, the function that calls `server.init` over the socket
(the installer owns no API client), and `install.Refusal`, whose reason the
CLI turns into its code.

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

## The steps

In this order ([`Installer.Install`](../../internal/server/installer/installer.go)):

1. **Every check, before any write.** Each refusal below is an
   `install.Refusal` saying "nothing was written":
   - systemd older than 247, which `LoadCredential=` needs:
     `unsupported_platform`;
   - a data directory on a network filesystem (NFS, SMB, Ceph, 9p, sshfs and
     the like; `networkFilesystem`), where SQLite's WAL does not work:
     `unsupported_platform`. Before `/var/lib/koko` exists, the filesystem it
     will be made on is checked;
   - a koko server initialised already (below): `already_initialised`;
   - no `--owner` for one not initialised yet: exit 2;
   - the port held by another process, named, with the hint to pass
     `--port`: `port_taken`. Install never moves to another port;
   - without `--address`, a default route's source IP that is not public,
     or none: exit 2, with the hint to pass `--address`. The address check
     ([`isPublic`](../../internal/server/installer/checks.go)) is a pure
     function: RFC 1918, CGNAT (100.64.0.0/10) and IPv6 ULA are private, and
     so are loopback, link-local, multicast and unspecified addresses. The IP
     becomes the Server Address without a port, an IPv6 one in brackets.

   A Root Key file that is there but does not load stops install too, before
   any write, with `seal.Load`'s error naming it (`internal_error`); it is
   left as it is, never replaced.
2. **The `koko` user and the directories:** `/etc/koko` (0700, root) and
   `/var/lib/koko` (0700, `koko`), each given its mode and owner at every run,
   whatever the umask or a copy put back made them.
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
6. **The socket.** Install waits for `/run/koko/koko.sock` for up to
   `timing.InstallSocketWait` (30 s). A koko server that
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

On an initialised koko server install refuses with `already_initialised`,
naming the Root Key the sealed data needs and `koko device add` for a Join
Code, and writes nothing: no Root Key is ever written over sealed data, even
when the Root Key's file is gone. #43 makes this run the upgrade.

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
open in the provider's firewall, and the first Join Code, for the Owner's
`device-1`, with its 15 minutes and the `koko join` line. Never the Root Key.
With `--json`:

```json
{
  "address": "203.0.113.7",
  "host_key_fingerprint": "SHA256:...",
  "root_key_file": "/etc/koko/root.key",
  "root_key_id": "rootkey_3f9k2m7q",
  "listen_port": 443,
  "join_code": {"id": "...", "code": "koko_...", "user": "alice", "device": "device-1", "expires_at": "..."}
}
```

`address` is the Server Address as stored: without a port, its port is
`listen_port`.

## Verifying

[`server_install_test.go`](../../internal/client/cli/server_install_test.go)
runs `koko server install` through the CLI on
[`harness.NewMachine`](../../internal/harness/machine.go), a fake machine: a
temporary root holding only the system directories, and fakes that answer
what the test sets (systemd's version, the filesystem type, the processes on
ports, the source IP) and record every call with the files under the root at
that moment. Its `systemctl` acts as systemd does with the unit: it runs only
the unit loaded at the last `daemon-reload`, refuses a binary that does not
run or a credential's missing file, makes the runtime directory, and runs the
unit's `ExecStart` with its `Environment=` lines and `CREDENTIALS_DIRECTORY`
through `koko server run` as an in-process koko server under the root,
listening on a loopback port. So initialisation and the printout run for
real. The tests cover a fresh install (files, modes, owners, the unit's
golden file, the `--json` golden, the koko server initialised), the order
(checks before any write, the Root Key and the binary before the start, a
late socket waited for), the printout for people, `--port`, `--address`,
every refusal writing nothing, private source IPs, bad flags, running again
after a refused address and after a service that never opened its socket,
a Root Key file there already, and an initialised koko server.
[`checks_test.go`](../../internal/server/installer/checks_test.go) and
[`system_test.go`](../../internal/server/installer/system_test.go) test the
address check, the network filesystems, reading systemd's version and
reading mountinfo. The real machine's own calls run in the real-systemd
workflow (#65).
