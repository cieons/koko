# The installer

`koko server install`, which installs, starts and initialises the koko server
on a fresh Linux machine with systemd, as root, and upgrades one initialised
already; and `koko server uninstall`, which removes it and keeps its data.
Each is two halves: the CLI's command, in
[`server.go`](../../internal/client/cli/server.go) and
[`kokod.go`](../../internal/client/cli/kokod.go), which gets the `kokod` of
its own build and runs it, and `kokod install` or `kokod uninstall`, the
machine's part, in [`internal/server/installer`](../../internal/server/installer).
Why the koko server installs itself is
[ADR 0020](../adr/0020-the-koko-server-installs-itself-and-has-no-configuration-file.md),
and why the CLI installs the `kokod` of its own version is
[ADR 0041](../adr/0041-koko-installs-the-koko-server-of-its-own-version.md);
the order, the paths and the unit's settings are P0 spec: install and run.

## The two halves

`koko server install [--owner <user>] [--address <host[:port]>] [--port <port>] [--web-listen <host:port|off>] [--kokod <file>]`
is in the irreversible tier: it changes the machine and prints a credential,
the first Join Code. It never prompts. In this order, each refusal before
any write:

1. **Its flags.** A `--port` outside 1 to 65535, an `--owner` that is not a
   User name, an `--address` that is not a Server Address and a
   `--web-listen` that is neither `host:port`, with a port from 1 to 65535,
   nor `off` are refused
   with exit 2 ([`install.ParseRequest`](../../internal/shared/install/install.go),
   which `kokod install` checks with too), and so is a `--kokod` that names
   no file. `--port` has no fixed default: a port not given is 0, which is
   7822 on a fresh machine and the port the koko server listens on now on
   [an upgrade](#the-upgrade). `--web-listen` not given is off on a fresh
   machine, and on an upgrade the web listener the installed unit names.
2. **The machine, before any `kokod` is read**, each `unsupported_platform`:
   not Linux, such as macOS, naming the image `ghcr.io/cieons/koko` and the
   install docs' "In a container"; an architecture no `kokod` is built for
   (`install.KokodPlatforms`: `linux/amd64` and `linux/arm64`); koko not
   running as root, naming the command line again after `sudo`; and no
   `/run/systemd/system`, with the refusal `kokod install` makes too
   (`install.NoSystemd`), so that a container gets it at once.
3. **Getting `kokod`:** the file `--kokod` names, or else
   `/usr/local/bin/kokod` when its build is this `koko`'s, or else, when
   this `koko` is a release, [the download](#the-download) of that
   release's `kokod-linux-<arch>`. A `koko` that is no release (a
   pseudo-version, `+dirty` or `(devel)`, `build.Info.Release`) has no
   release to download from: it is refused with exit 2, naming the build,
   what is at `/usr/local/bin/kokod` (nothing, another build, or a file that
   is no `kokod`) and `--kokod`, with `--kokod bin/kokod` for a development
   build, and asks GitHub nothing.
4. **The build.** koko reads the file's Go build information without
   running it: a file that is not a Go binary, another program (its main
   package is not `github.com/cieons/koko/cmd/kokod`), a `kokod` of
   another build (its module version, `+dirty` included, or its commit
   differ), naming both builds, or one built for another platform than the
   machine's, naming both, is refused, and nothing runs: with exit 2 for
   `--kokod`'s, and as `download_failed` for a download's.
5. **`kokod install --json`**, with `--owner`, `--address` and `--port` as
   given, stdin empty, through `cli.Runner.Kokod`
   ([code layout](code-layout.md#the-package-groups)): a child process in
   `cmd/koko`, `kokod`'s command line in process in tests. It does
   [the steps](#the-steps) 1 to 6 and prints the shared
   [`install.Report`](../../internal/shared/install/install.go) as JSON on
   stdout. Its refusal, the error object it prints on stderr, becomes
   `koko server install`'s, code, message and exit status unchanged;
   anything else on its stderr is passed through.
6. **Initialisation** over the socket ([`server.init`](initialisation.md)),
   when the report has `initialise`: with the Owner and the Server Address
   it names, `--address`'s or the public IP `kokod install` found. The
   CLI's own API client makes the call, so `kokod` carries no client code.
7. **The printout**, as [below](#what-it-prints).

`kokod install` run by hand on a fresh machine installs and starts a koko
server it cannot initialise, and its report's `initialise.next` names what
is left: `koko server install` with the same flags, which runs the `kokod`
installed and initialises it:

```json
{
  "initialise": {"owner": "alice", "address": "203.0.113.7", "next": "sudo koko server install --owner alice"},
  "root_key_file": "/etc/koko/root.key",
  "root_key_id": "rootkey_3f9k2m7q",
  "port": 7822
}
```

An upgrade's report has no `initialise`, and has the stored `address`, the
`host_key_fingerprint` and, when they must, `devices_must_connect`. `koko`
runs only a `kokod` of its own build, so this JSON has no compatibility
rules. `koko server install` does not install `koko` itself: `install.sh`
does ([the release](release.md#installsh)).

`koko server uninstall [--kokod <file>]`, also irreversible, checks the
machine and gets `kokod` as install does (steps 2 to 4), then runs `kokod
uninstall --json` and prints its
[`install.UninstallReport`](../../internal/shared/install/install.go)
([uninstall](#uninstall)).

## The download

[`downloadKokod`](../../internal/client/cli/download.go) gets the `kokod`
of this `koko`'s release, `v<version>`, for the machine's platform
([ADR 0041](../adr/0041-koko-installs-the-koko-server-of-its-own-version.md)):

1. It downloads `SHA256SUMS` from
   `https://github.com/cieons/koko/releases/download/v<version>/`, and
   finds the line `sha256sum` wrote for `kokod-linux-<arch>`. A
   `SHA256SUMS` that lists none is refused before `kokod` is asked for.
2. It makes a new directory under `TMPDIR` (0700, `koko-kokod-*`) and
   downloads `kokod-linux-<arch>` from the same release into it (0700),
   hashing it as it writes.
3. A SHA-256 other than the one listed is refused, and so is a file of
   another build (step 4 above). Either way nothing runs.
4. `kokod` runs from that directory, which `koko` removes once it has
   ended, and on every refusal before.

Every request is HTTPS: a URL that is not is refused before it is asked
for, and a redirect is followed only to HTTPS, at most 10 deep, as GitHub
redirects each asset to its own storage. The whole download has 10 minutes,
about 40 MiB over a link of 0.6 Mbit/s. `SHA256SUMS` may be 1 MiB and
`kokod` 512 MiB; a longer answer is refused as it arrives. Go's default
transport makes the requests, so `HTTPS_PROXY` applies, and the system's
CA certificates check GitHub's. Each refusal is `download_failed` (exit 1),
naming the URL that failed and saying to give `kokod-linux-<arch>` of the
version with `--kokod <file>` on a machine that cannot download it, such as
one offline; nothing was written. `SHA256SUMS` stays in memory, as nothing
runs it.

`--kokod` and an installed `kokod` of this build make no request. Where
downloads come from is [the releases hook](code-layout.md#test-only-hooks),
`cli.Runner.Releases`, which only tests set: no flag or variable moves it.

## The machine interface

The installer reaches the machine only through an
[`installer.Machine`](../../internal/server/installer/installer.go): a root
prefixed to every path it reads or writes, and what only the machine answers
or does. [`installer.System`](../../internal/server/installer/system.go) is
the real one, rooted at `/`:

| Method | On the machine |
| --- | --- |
| `RunsAsRoot` | the effective uid is 0 |
| `SystemdVersion` | the first line of `systemctl --version` |
| `FilesystemType` | the latest mount on the longest mount point holding the path, in `/proc/self/mountinfo` ([`filesystem.System`](../../internal/server/filesystem/filesystem.go), which start-up reads too) |
| `PortOwner` | [`portowner.Lookup`](web-front.md#the-port-owner-lookup), which as root names any user's process |
| `SourceIP` | the source address of a UDP socket connected to a documentation address (192.0.2.1, then 2001:db8::1), which only the default route covers; connecting sends nothing, and no one else is asked |
| `AddUser` | `useradd --system --user-group --no-create-home --home-dir /var/lib/koko --shell /usr/sbin/nologin --comment "koko server" koko`, unless `koko` exists |
| `Chown` | `os.Chown` to the user and its primary group |
| `Systemctl` | `systemctl` with the arguments |
| `ServiceShow` | `systemctl show --property=<property> --value koko.service`, as systemd has the unit loaded: `MainPID` (0 while it does not run) and `Environment` (its assignments, each quoted as systemd quotes one a shell would take) |

## The steps

`kokod install` does these, in this order
([`Installer.Install`](../../internal/server/installer/installer.go)):

1. **Every check, before any write.** Each refusal below says "nothing was
   written"; all but the schema's are an `install.Refusal`, whose reason
   `kokod` and the CLI turn into its code:
   - a process that is not root (`Machine.RunsAsRoot`), as when `kokod
     install` is run by hand without `sudo`: `unsupported_platform`, naming
     `sudo koko server install`; `kokod uninstall` refuses it too;
   - systemd not running the machine, as `sd_booted(3)` tells it: no
     `/run/systemd/system` directory under the root, as in a container
     whose init is something else, where `systemctl` may still be there and
     would fail only at `daemon-reload`, after the writes:
     `unsupported_platform`, before install asks the machine anything,
     naming [the image](release.md#the-image) `ghcr.io/cieons/koko` and the
     install docs' "In a container" section, which run it there instead;
   - systemd older than 247, which `LoadCredential=` needs:
     `unsupported_platform`;
   - a data directory on a network filesystem (NFS, SMB, Ceph, 9p, sshfs and
     the like; [`filesystem.Network`](../../internal/server/filesystem/filesystem.go),
     which the koko server's start-up also checks at every start
     ([the Seal](seal.md#at-start-up))), where SQLite's WAL does not work:
     `unsupported_platform`. Before `/var/lib/koko` exists, the filesystem it
     will be made on is checked (`filesystem.Nearest`);
   - a `koko.db` at a schema this `kokod` predates, which the koko server
     would refuse to start on, as when an older one is installed to go back
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
   - with `--web-listen`, its port checked the same way, with the hint to
     pass `--web-listen`: `port_taken`; and a web listener on the port the
     koko server listens on for devices: exit 2;
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
4. **`kokod`**, `/usr/local/bin/kokod` (0755, root), copied from the
   running one unless it runs from there, as when `koko server install`
   runs the `kokod` installed, and **the unit**,
   `/etc/systemd/system/koko.service` (0644, root). Every file is written
   through a temporary file beside it, given its mode and owner and synced,
   then renamed into place, so none is seen half-written and a running binary
   is replaced, never changed.
5. **`systemctl daemon-reload`, `enable koko.service`, `restart
   koko.service`.** Restart, not start, so that a koko service an earlier run
   left running takes the new unit and `kokod`.
6. **The socket.** Install waits until the koko server accepts a connection
   on `/run/koko/koko.sock`, for up to `timing.InstallSocketWait` (30 s). It
   connects rather than looks for the file, which is there a moment before
   the koko server listens on it. A koko server that
   refuses to start never opens it, so the failure names
   `systemctl status koko` and `journalctl -u koko`, where its refusal is.
7. **The report**, which on a fresh koko server says what initialising it
   needs: `koko server install` initialises it
   ([the two halves](#the-two-halves)).

Until initialisation has succeeded, running install again is safe and
finishes the job: every step is done again or found done, nothing is
sealed under the Root Key yet, so it is reused, and the `kokod` the first
run installed is run again without `--kokod`. A run refused before any
write installed no `kokod`, so the next is given one again.

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
`install.sh` does, replaces `kokod`, writes the unit again from the
template, and runs `daemon-reload`, `enable` and `restart`, then waits for the
socket, as a fresh install does ([the steps](#the-steps) 2 to 6). It
initialises nothing and issues no Join Code: the restarted koko server keeps
its record, its Owner, its Server Address and its keys, and migrates its
schema at start ([the store](store.md)). A machine as v0.1.0 or v0.2.0 left
it, whose unit ran `/usr/local/bin/koko server run`, upgrades the same way:
the unit is written again to run `/usr/local/bin/kokod run`, its port and
drop-ins kept, and `/usr/local/bin/koko`, the CLI now, is left as it is.
Its own checks, after the machine's and before any write, each leaving the
running koko server as it was:

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
  listened on is unknown, and it listens on 7822 or the port given. The port
  is then checked as on a fresh machine, the service's own process allowed.
  Install reads `KOKO_LISTEN` from `Environment=` settings only, not from an
  `EnvironmentFile=`.
- **The web listener.** Without `--web-listen` the upgrade keeps the
  `KOKO_WEB_LISTEN` the installed unit at `/etc/systemd/system/koko.service`
  sets, which install wrote, and none when there is no unit, as after
  [uninstall](#uninstall); `--web-listen <host:port>` replaces it, and
  `--web-listen off` removes it, which later upgrades keep. Its port is
  checked as on a fresh machine. This is the one setting install reads from
  its own unit, which it otherwise writes again at every run. A drop-in
  that sets `KOKO_WEB_LISTEN` overrides the unit at the restart, and is not
  copied into it; install reads the drop-ins for it, the last winning, only
  to grant the right to bind its port below 1024, as for a drop-in's
  `KOKO_LISTEN`.
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
embedded in `kokod` and rendered at every run
([`installer.Unit`](../../internal/server/installer/unit.go)), so the unit
always matches `kokod`, which its `ExecStart=/usr/local/bin/kokod run`
runs; the
[golden file](../../internal/client/cli/testdata/server-install-koko.service)
pins its text, with every setting the spec lists. Three choices are the
installer's own:

- **The startup settings are `Environment=KOKO_*` lines, not flags**, so that
  a drop-in's own `Environment=` line overrides one and outlasts the next
  install, which rewrites the unit; a flag in `ExecStart=` would beat it.
  `KOKO_LISTEN` carries `--port`, for `kokod run`'s listen address, and
  `KOKO_WEB_LISTEN`, written only while the web listener is on, carries
  `--web-listen` ([the web listener](web-front.md#the-web-listener)).
- **The Root Key's path is not written**: `kokod run`'s default,
  `$CREDENTIALS_DIRECTORY/root-key`, is the file `LoadCredential=` makes,
  which is where systemd hands the key to the service and nowhere else.
- **`CAP_NET_BIND_SERVICE` only for a port below 1024**: the unit's
  `AmbientCapabilities=` and `CapabilityBoundingSet=` grant it only when
  the port install writes, or the web listener's, is one, such as
  `--port 443` or `--web-listen 127.0.0.1:80`, since the `koko` user binds
  such a port with it alone. The default 7822 needs none, and
  the unit says instead that a drop-in moving `KOKO_LISTEN` below 1024
  needs install run again, which reads the drop-in's port and grants it
  ([ADR 0036](../adr/0036-the-koko-server-listens-on-7822-by-default.md)).

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
  "listen_port": 7822,
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
the unit is there, reads the port the koko service listens on from systemd
and the web listener's address from the unit's `KOKO_WEB_LISTEN`, runs
`systemctl stop` and `disable koko.service` while systemd still knows the
unit, removes the unit and runs `daemon-reload`; it then removes
`/usr/local/bin/kokod`. It never removes `/usr/local/bin/koko`, the CLI,
which `install.sh` put there, so on a machine as v0.1.0 or v0.2.0 left it,
whose unit ran that file, it removes only the unit. It keeps
`/var/lib/koko`, `/etc/koko/root.key` and the `koko` user, and prints their
paths, with the port and the web listener's address, which a later
install no longer knows: the hint for people says to run
`sudo koko server install` without `--owner` and with that `--port` and,
when the unit set one, that `--web-listen`. Neither stops the uninstall
when it cannot be read: it is left out of the report and the hint, as
unknown. There is no purge: P0 has no backup, and one command would erase
the record, the Secrets and the koko key. Drop-ins are the
Owner's and stay. What is not there is not removed, so running it again
removes nothing and asks systemd nothing; it removed `kokod`, so a run
after it downloads it again, or is given one with `--kokod`, as install
after it is. Install
without `--owner` then runs the koko server again on the data it kept, as
[the upgrade](#the-upgrade). With `--json` (`listen_port` left out when
systemd did not tell it, and `web_listen`, such as `"127.0.0.1:7823"`,
left out when the unit set none or could not be read):

```json
{
  "removed": ["/etc/systemd/system/koko.service", "/usr/local/bin/kokod"],
  "data_dir": "/var/lib/koko",
  "root_key_file": "/etc/koko/root.key",
  "system_user": "koko",
  "listen_port": 7822
}
```

## Verifying

[`server_install_test.go`](../../internal/client/cli/server_install_test.go)
runs `koko server install` through the CLI on
[`harness.NewMachine`](../../internal/harness/machine.go), a fake machine: a
temporary root holding only the system directories (`/run/systemd/system`
among them, as systemd runs the machine), and fakes that answer
what the test sets (the platform, whether koko runs as root, systemd's
version, the filesystem type, the processes on ports, the source IP) and
record every call with the files under the root at that moment. It is the
`cli.Runner.Kokod` koko runs on: it states the build of each `kokod` file
it makes (`m.Kokod()`, of the test binary's build, `m.NewRelease()`, a new
file of the same build, and `m.NewKokod(build)`, of any build or program),
by the file's bytes so that the copy at `/usr/local/bin/kokod` keeps it, and
runs one as `kokod`'s command line in process on the machine, noting each
command line (`m.Ran()`). Its `systemctl` acts as systemd does with the unit: it runs only
the unit loaded, with its drop-ins, at the last `daemon-reload`, refuses a
binary that does not run or a credential's missing file, makes the runtime
directory, gives the state directory and all in it to the unit's `User=`
unless the directory is that user's already (as `StateDirectory=` does),
shows the `MainPID` and the quoted `Environment` systemd would, and runs
the unit's `ExecStart` (`kokod run`, or a v0.2.0 unit's `koko server run`,
which took the same flags) with its `Environment=` lines and
`CREDENTIALS_DIRECTORY` through `kokod run` as an in-process koko
server under the root,
listening on a loopback port, which its Join Codes therefore name in place
of the unit's port. So initialisation and the printout run for real. The tests cover a fresh install (files, modes, owners, the unit's
golden file, the `--json` golden, the koko server initialised), the order
(checks before any write, the Root Key and the binary before the start, a
late socket waited for), a socket file there before the koko server accepts
on it (`ListenDelay`), the printout for people, `--port` (and
`CAP_NET_BIND_SERVICE` only for one below 1024), `--web-listen` (written
into the unit, its port checked and taken refused, the capability for one
below 1024, and `koko server show` naming it beside the Web URL),
`--address`,
every refusal writing nothing, systemd not running (with goldens for the
text and `--json`, naming the image, and `kokod install` refusing the same),
macOS, an architecture with no `kokod` and not root refused before any
`kokod` is read or run, a `--kokod` of another build, another program, no
Go binary or no file refused before it runs, no `kokod` of koko's build
without `--kokod`, `kokod install` by hand then `koko server install`,
private source IPs, bad flags, running again after a refused address and,
reusing the `kokod` installed, after a service that never opened its
socket, and a Root Key file there already. Only a service that
never opens its socket (`NoSocket`) has install wait less than the product's
30 s, `harness.NoSocketWait`: no wait would see that socket, so a short one
fails no slower machine, and every socket that does open is waited for as
on a real machine. `WaitLog` waits for a line the koko server logs after
its socket accepts, such as its start.
[`server_upgrade_test.go`](../../internal/client/cli/server_upgrade_test.go)
tests the upgrade on an initialised koko server: the new release's `kokod`,
the unit written again, the restart, the record kept, the `--json` goldens;
`--owner` and `--address` refused; the port kept without `--port`, moved with
it, and the warning only under a Server Address without a port of its own;
the service's own port allowed and another process's refused, a `koko`-named
one included; a missing, another or a damaged Root Key refused; a port set
with a drop-in kept, and a `--port` it would override refused, before and
after uninstall; no unit after
uninstall, and a unit changed by hand; the P0 restore of a copy onto a fresh
machine; data at a newer schema refused; and a machine as v0.2.0 left it,
whose unit is written again to run `kokod run`, its port and drop-in kept
and `/usr/local/bin/koko` untouched; and the web listener kept without
`--web-listen`, replaced with it, removed by `off` and kept off, its port
checked and a taken one refused, and a drop-in's web listener below 1024
granted the bind.
[`server_uninstall_test.go`](../../internal/client/cli/server_uninstall_test.go)
tests uninstall: the service stopped while its unit is there, the unit and
`kokod` gone, `/usr/local/bin/koko` kept, what it keeps, the port, the
golden, running it again, install after it, and on a machine as v0.2.0 left
it, with no `kokod`, the unit alone removed.
[`server_download_test.go`](../../internal/client/cli/server_download_test.go)
tests the download, with `koko` stated as release `v9.9.9` on the fake
machine (`m.Release`), whose fake GitHub serves that release and logs every
request: exactly `SHA256SUMS` and `kokod-linux-<arch>` downloaded for each
platform, installed and initialised, and the temporary directory gone; a
`kokod` `SHA256SUMS` does not list, one with another SHA-256 or of another
build, a missing `SHA256SUMS` or `kokod`, and a redirect to plain HTTP each
refused, nothing run or written, no temporary directory left; `--kokod`
and a `kokod` installed of koko's build making no request; and a
pseudo-version, `+dirty` and `(devel)` refused without a request, as is a
machine that cannot run the koko server. The tests of running install
again after a refused address, uninstall again then install, an upgrade
after uninstall, the drop-in port across uninstall and a machine as v0.2.0
left it download too.

The fake machine's `PortOwner` names the koko
service on the port its loaded unit's `KOKO_LISTEN`, or `KOKO_WEB_LISTEN`,
names while it runs; the koko server it runs listens on loopback ports of
the test's choosing for both.
[`install_test.go`](../../internal/server/kokod/install_test.go) tests that
`kokod install` and `uninstall` print the shared reports as JSON, and that
their refusals have koko's error shape and exit statuses.
[`checks_test.go`](../../internal/server/installer/checks_test.go),
[`system_test.go`](../../internal/server/installer/system_test.go) and
[`unit_test.go`](../../internal/server/installer/unit_test.go) test the
address check, reading systemd's version and reading the port from the
unit's environment as systemd shows it;
[`filesystem_test.go`](../../internal/server/filesystem/filesystem_test.go)
tests the network filesystems and reading mountinfo. The real machine's own
calls run in [the real-systemd check](release.md#the-real-systemd-check), all but
`SourceIP`, since every fresh install there passes `--address`, and a
`PortOwner` naming another process than the koko service. The refusal of a
container that systemd does not run runs in
[the container check](release.md#the-container-check).
