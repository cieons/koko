# The Device Connection and joining

How a device reaches the koko server over TCP, how a new device joins with a
Join Code, what a joined device keeps, how ssh is pointed at koko, and how
a Device Session is used and revoked. The design is P0 spec: reach ("The
Device Connection", "Device files and ssh integration") and P0 spec:
people and Spaces ("Joining", "Device Sessions"); why devices join with a
one-time code is
[ADR 0013](../adr/0013-devices-join-with-a-one-time-join-code.md).

## The TCP listener

- **When it listens.** Only once the koko server is initialised: from
  initialisation on, and at every later start at once
  ([initialisation](initialisation.md#the-operation)); its lifecycle is
  [`tcpListener`](../../internal/server/listen.go).
- **Where.** `kokod run --listen <addr>` (`KOKO_LISTEN`, default
  `:7822`, [ADR 0036](../adr/0036-the-koko-server-listens-on-7822-by-default.md)),
  a host and a port number. A Server Address without a port names
  the port the listener bound ([`joinTarget`](../../internal/server/jointarget.go)).
  An address another process holds is refused with `port_taken`, naming
  that process as [`portowner`](../../internal/shared/portowner/portowner.go)
  finds it, and the commands that name it (`ss -ltnp`, `lsof -i`) when
  portowner cannot tell its pid, as the web listener's refusal does.
- **Its host key** is the koko server's sealed host key, the one Join Codes
  name, the same across restarts. The Unix socket keeps its own key made at
  each start ([the API](api.md#the-way-in)).
- **SSH** with the version string `SSH-2.0-koko`, carrying the socket's
  channels, `api@koko`, `web@koko` and `tunnel@koko`, and refusing any other type
  ([`transport.ServeTCP`](../../internal/server/transport/transport.go)).
  A `web@koko` channel reaches the web front, which lets it in by the
  cookie of the connection's Device Session ([`koko web` and the web
  front](web-front.md#runs-tokens-and-the-cookie)).
- **Liveness.** The koko server notes when bytes last came from each Device
  Connection ([`liveness`](../../internal/shared/liveness/liveness.go)):
  after `timing.ServerProbe` (15 s) without any it sends
  `keepalive@openssh.com` wanting a reply, which the CLI's connection
  answers, and after `timing.ServerDead` (45 s) it declares the connection
  dead and closes it, logging `device connection closed` with `reason=dead`
  ([`transport.Server.watch`](../../internal/server/transport/transport.go)).
  The Unix socket's connections are not probed: a local socket does not die
  without a word. The device probes from its side sooner
  ([the Device Mux](device-mux.md#liveness-and-reconnecting)).

## Authentication

The SSH user name identifies nobody: the CLI sends `koko`.

1. **Public key.** Any key may try. Once the device has signed with it, the
   transport asks the device area what the key is
   ([`device.Authenticator.Key`](../../internal/server/device/join.go)): a
   live Device Session's key authenticates the connection as that Device
   Session, which every call on it carries (`op.Caller.DeviceSession`).
2. **A Revoked Device Session's key** is refused with the banner
   `koko: the Device Session "<device>" of this device was revoked.`
   ([`wire.RevokedBanner`](../../internal/shared/wire/wire.go)), only after
   the signature check, so offering a key without its private half learns
   nothing. A key stays a Device Session's for ever, so a Revoked key never
   reaches the join step. The log says `revoked device refused`.
3. **A key no Device Session holds** gets a partial success, with `password`
   as the only method left: the device has proved it holds the key, and may
   now join. A device that is not joining has no password to give: the CLI
   reports that the koko server holds no Device Session with its key
   (`not_joined`), naming no User and no device.
4. **The join step.** The password is the Join Code's secret in lowercase hex
   ([`wire.JoinSecret`](../../internal/shared/wire/wire.go)). In one
   transaction ([`device.Authenticator.Join`](../../internal/server/device/join.go))
   the koko server finds the valid Join Code whose secret it is, by its hash,
   records the key as a Device Session with the code's User and device name
   and the code's issuer as "added by", and marks the code used. The
   connection is then an ordinary Device Connection of that Device Session.
   Any failure (no such code, used, voided, expired, or no secret at all)
   gets the same banner, `koko: the Join Code is invalid or expired.`, and
   the log says which.

## Who is calling

Every call on a Device Connection carries its Device Session, which
[`access.Who`](../../internal/server/access/access.go) turns into its User
([the API](api.md#the-operation-framework)); `koko device add` on a joined
device therefore issues for that User ([Join Codes](join-codes.md#issuing)).
A Revoked Device Session's call, such as one in flight when its revocation
commits, is refused with `revoked`, naming the device.

## `koko join`

`koko join <join-code>`, or `koko join -` to read the code from stdin (a
terminal gets a no-echo prompt), in the irreversible tier, since it spends a
one-time code ([`join`](../../internal/client/cli/join.go)):

1. It decodes the code; one that cannot be read is refused with exit 2
   before any connection ([Join Codes](join-codes.md#the-string)).
2. It refuses, with `already_joined`, while this device is joined, and says
   to revoke this device's Device Session first, or, when the koko server
   no longer knows the device, to run [`koko leave`](#on-the-device).
3. It renders koko's ssh_config ([ssh](#ssh)), makes a new ed25519 key and
   writes it into `~/.ssh/koko/` under a temporary name, so a device that
   cannot keep its files, or whose paths ssh would misread, learns it before
   it spends the code.
4. It connects to the code's address and checks the host key against the
   code's fingerprint before it authenticates; a mismatch fails with
   `host_key_mismatch`, naming both keys, and the code stays usable. It
   waits at most `timing.CommandWait` for the connection and the report,
   and tries once: a join cut after the koko server took the code cannot
   join again.
5. It authenticates in the two steps above
   ([`serverconn.Join`](../../internal/client/serverconn/serverconn.go)); the
   banner of a refusal is the error, with `invalid_join_code`.
6. On that connection it calls `user.current`, which names the User and the
   device name the code bound, and the Device Session's id. When the
   connection drops the call, or sent no report, the call is sent again,
   once, on a new Device Connection with the key, now a Device Session,
   opened as the direct path opens one
   ([the API client](api.md#the-api-client)).
7. It puts the key in place, writes koko's known_hosts and ssh_config
   ([ssh](#ssh)), then the client config, which marks the device joined,
   and removes any revocation record. Then it writes the bare names of the
   Hosts its User can see into koko's ssh_config, listed on the same
   connection ([bare names](#ssh)).
8. Unless `--no-ssh-config`, it puts the Include line first in
   `~/.ssh/config`, then checks it as `koko status` does ([ssh](#ssh)). A
   file it leaves alone, or a line below a `Host` line, does not fail the
   join, which has happened: it says why, and how to fix it.
9. It prints the Server Address and the Device Session, and with `--json`
   that warning ([the CLI](cli.md#the-commands-so-far)).

**A failed join leaves nothing it wrote:** the staged key is removed, and so
are `~/.ssh` and `~/.ssh/koko/` when the join made them and they are empty
(a `~/.ssh/koko/` that was there stays, made private). A join that fails
after the koko server accepted the code (`user.current` lost on both
connections, before step 7) says that the code is spent and which Device
Session to revoke, when it knows. Step 7 failing removes what it wrote.

## The device's files

[`devicefiles`](../../internal/client/devicefiles/devicefiles.go) is the one
place a device's join is written and read, so whatever ends a join on the
device removes it there.

| Path | Mode | What |
| --- | --- | --- |
| `~/.ssh/koko/` | 0700 | koko's directory, made private even when it was there; `~/.ssh`, when koko makes it, is 0700 too |
| `~/.ssh/koko/device_key` | 0600 | the device's private key: OpenSSH-format ed25519, unencrypted, since `koko tunnel` cannot prompt |
| `~/.ssh/koko/client.json` | 0600 | the client config, below |
| `~/.ssh/koko/known_hosts` | 0600 | the host keys the client config pins, one line each under the alias `koko` ([ssh](#ssh)) |
| `~/.ssh/koko/ssh_config` | 0600 | koko's own ssh_config ([ssh](#ssh)) |
| `~/.ssh/koko/revoked.json` | 0600 | the revocation record, in place of the key, the client config and known_hosts once the device learns that its Device Session was Revoked ([On the device](#on-the-device)) |

The client config is JSON: `server_address` (`host:port`), `host_keys` (the
pinned host keys, each written as in `authorized_keys`; a Device Connection
takes any of them, and a join pins the one its code names), `user`, `device`
and `device_session` (the id). A client config or key that cannot be read or
parsed fails the command naming the file, and never counts as not joined.
Making the directory private sits behind a per-OS file; on Windows a join
fails as not supported until the Windows CLI (P1).

The revocation record is JSON: `server_address`, `user`, `device`,
`device_session` (the Revoked one's id) and `learned_at`, when the device
learned it, by its own clock, RFC 3339 in UTC. One that cannot be read or
parsed fails the command naming the file.

## ssh

[`devicefiles`](../../internal/client/devicefiles/ssh.go) also holds koko's
ssh integration (P0 spec: reach, "Device files and ssh integration").

- **koko's ssh_config** (`devicefiles.WriteSSHConfig`) is rewritten whole
  and atomically (a new file renamed over it), mode 0600: a comment, then a
  `Host` line naming `*.koko` and the bare names it is given, in their
  order, then the block, as [a join of a User who sees no Host writes it](../../internal/client/cli/testdata/join-ssh_config).
  It is written only when it would change: a regular file of mode 0600
  that holds the same bytes is left alone.
  The ProxyCommand runs the binary that wrote the file: by the path a PATH
  lookup of `koko` finds when that is the same file, such as a package
  manager's link that outlives an upgrade, and otherwise by the path the OS
  gives for it, which on Linux has every symlink resolved; the Device Mux's
  refresh keeps the binary the file runs ([the bare-name refresh](device-mux.md#the-bare-name-refresh)). known_hosts' path
  is resolved from `HOME`. Both are absolute and double-quoted, and a path
  holding `"`, `$`, `` ` ``, `\`, `%` or a control character is refused,
  since ssh or the shell running ProxyCommand would read it otherwise; a
  join renders the file before it connects, so such a path refuses the join
  while its code stays usable. A bare name must be a name (a DNS label).
  Every keyword is one OpenSSH 7.3 knows. A name list too long for one
  `Host` line is split across blocks identical but for their names, so that
  no line passes 1 KiB, newline included
  ([golden](../../internal/client/devicefiles/testdata/ssh_config-split)).
- **Bare names** are the names of the Hosts the device's User can see that
  are not Retired, as `host.list` answers them, by name, so that bare `ssh
  web1` works ([ADR 0014](../adr/0014-host-names-are-unique-across-the-koko-server.md)).
  `koko join` writes them; so does the CLI right after the caller's own
  `koko host add` (stdin included), `rename`, `move`, `retire` and `rm`
  succeed ([`rewriteBareNamesAfterHostChange`](../../internal/client/cli/barenames.go)),
  listing on the command's own connection, which also takes a Host someone
  else recorded since; and the Device Mux refreshes them while it runs
  ([the bare-name refresh](device-mux.md#the-bare-name-refresh)).
  A Host someone else records, or one in a Space the User is added to, is
  reachable as `web1.koko` at once, since the Gateway checks access at each
  SSH Session, and as bare `web1` after the next refresh. Over the Unix
  socket the caller is the Local caller, on the koko server's own machine,
  where nothing joins: there the Host changes rewrite them only for an OS
  user whose [`koko ssh setup`](#on-the-koko-servers-own-machine) wrote
  koko's ssh_config, and write none into a home that never asked for it
  (`devicefiles.HasSSHConfig`). A join or a Host change whose bare names cannot be
  written still holds and exits 0, and says so in one line on stderr,
  `koko: could not rewrite the bare names in koko's ssh_config (<why>);
  until koko does, reach a Host as ssh <host>.koko`.
- **known_hosts** holds each host key the client config pins, as
  `koko <key>`, mode 0600. P0 has one koko server per device, so every line
  in it is that koko server's.
- **The Include line** (`devicefiles.AddInclude`): `Include
  ~/.ssh/koko/ssh_config` goes first in `~/.ssh/config`, which is made with
  mode 0600 when absent, or rewritten atomically as the line followed by
  its bytes as they were, keeping its mode. A file that includes koko's
  ssh_config already, anywhere, is left alone; so is one that is a symlink,
  not a regular file (never read, since a named pipe would never end),
  read-only (no write bit, or this user cannot open it for writing; the
  check sits behind the per-OS file, and Windows rewrites none), or that
  cannot be read or written, and then the error says why, with the line to
  add. An `Include` names koko's ssh_config when one of its arguments,
  bare or double-quoted, resolved as OpenSSH does (`~/` from `HOME`, a
  relative path from `~/.ssh`) and read as a glob, matches that file;
  keywords are case-insensitive, and `=` may follow one.
- **The check** (`devicefiles.CheckInclude`), for `koko status` and after
  `koko join` and `koko ssh setup`: the Include line is missing, or it comes after the first
  `Host` or `Match` line, where ssh reads it only for that block's hosts; a
  `~/.ssh/config` that cannot be read, or is not a regular file, is reported
  as such, not as missing.

### On the koko server's own machine

Nothing joins on the koko server's own machine, yet `ssh web1.koko` works
there over the Unix socket, as the Local caller, once the OS user running
it has run **`koko ssh setup`** ([`sshSetup`](../../internal/client/cli/sshsetup.go)),
in the reversible tier, once (P0 spec: reach, story 65). Each OS user who
wants it runs it for themselves: it writes only under their own HOME, and
nothing outside it. `koko server install` says so in one line.

1. On a joined device it is refused with `already_joined` before any call:
   `koko join` wrote koko's ssh files there, and `koko status` says
   whether ssh reads them.
2. It reaches the koko server as any command does: with no join, only the
   Unix socket does, so a machine without one fails with `not_joined`,
   naming `koko join` and the socket's path.
3. It reads the koko server's host key from `server.show` (`host_key`,
   [the deployment](initialisation.md#the-deployment-afterwards)), the key
   the Gateway presents inside every Tunnel, and lists the Hosts, which for
   the Local caller, who acts as the Owner, is every Host but the Retired.
   A koko server that names no host key, one older than `host_key`, fails
   it with `server_too_old`.
4. It makes `~/.ssh` and `~/.ssh/koko/` as a join does, writes koko's
   known_hosts with that key under the alias `koko` and koko's ssh_config
   with those bare names
   ([`devicefiles.SetUpSSH`](../../internal/client/devicefiles/ssh.go)),
   then puts the Include line first in `~/.ssh/config` and checks it, by
   the join's rules: a file it leaves alone does not fail it, and the
   warning says why and which line to add.
5. It prints the path of koko's ssh_config, then that `ssh <host>.koko`
   now reaches a Host through koko, or the Include warning in its place;
   with `--json`, `{"ssh_config", "include_warning"}`.

Run again, it writes the same files anew. No Device Mux runs there, so
nothing refreshes the bare names in the background: a Host someone else
records is reachable as `name.koko` at once, and as its bare name after the
user's own next Host change over the socket or the next `koko ssh setup`.

## Which koko server the CLI reaches

A command reaches the koko server on its first call
([`Input.connect`](../../internal/client/cli/cli.go)): over the Unix socket
when the socket is at its path, as the Local caller; otherwise the koko
server this device joined, through this build's
[Device Mux](device-mux.md), which holds the device's Device Connection,
or over a direct Device Connection of its own
([`serverconn.DialDevice`](../../internal/client/serverconn/serverconn.go))
when no Device Mux can carry it ([the direct path](device-mux.md#which-way-a-command-goes)),
checked against the pinned host keys either way. Each is a
[`serverconn.Conn`](../../internal/client/serverconn/serverconn.go), so a
command does not know which it has. A failure is `not_joined`,
`revoked`, `server_unreachable` or `host_key_mismatch`, as [the CLI](cli.md#the-runner-and-the-output-layer)
describes them.

A command waits at most `timing.CommandWait` (15 s) for its connection and
the koko server's report on it, which `Input.connect` waits for before the
first call: over the socket, through the Device Mux, which tries meanwhile,
and on the direct path, which tries again with the Device Mux's backoff
([the direct path](device-mux.md#which-way-a-command-goes)). Then it fails
with `server_unreachable`, `cannot reach the koko server at <address>`,
whose hint names `koko connect <host:port>`. A call cut mid-flight is sent
again on a connection dialled the same way, so its one resend waits for the
reconnect ([the API client](api.md#the-api-client)).

## `koko connect`

`koko connect [<host:port>]`, in the reversible tier
([`connectDevice`](../../internal/client/cli/connect.go)), checks this
device's Device Connection:

- It reaches the koko server this device joined as any command does, never
  over the Unix socket, starting the Device Mux when none runs, and prints
  the Server Address, the fingerprints of the pinned host keys, one of which
  the koko server answered with, its version and the Device Mux, as `koko
  status` shows it, or `null` on the direct path
  ([the CLI](cli.md#the-commands-so-far)).
- **Given an address**, which must name its port (exit 2 otherwise, as for
  one that is no Server Address), it first opens a direct Device Connection
  there with the device's key and pinned host keys, waiting and trying as
  the direct path does, and only once the koko server answered there saves
  the address in the client config
  ([`devicefiles.SetServerAddress`](../../internal/client/devicefiles/devicefiles.go));
  the pinned host keys, and so known_hosts, stay. A host key that is not
  pinned is refused with `host_key_mismatch`, an address nothing answers at
  with `server_unreachable`, and a koko server that does not know the key
  with `not_joined`, each saying that the device keeps the address it has;
  a Revoked key runs the revocation cleanup, since a pinned host key
  answered. Nothing else moves a device to another address. The Device Mux
  takes the new address at the next Device Connection it opens; one it holds
  already reaches the same koko server.

## Last used

A Device Session is used each time a Device Connection it authenticated
opens a channel (`api@koko`, `web@koko` or `tunnel@koko`): a command, a
browser connection, an SSH Session, `koko join`'s own call. A long-lived connection, such as the Device Mux's,
counts only by what it opens, never by staying open. The transport tells the
device area ([`device.Authenticator.Used`](../../internal/server/device/join.go)),
which keeps the time to the minute: a use within a minute of the time it
last wrote skips the database, so a device running many commands costs one
write a minute, and a channel waits for none in between. `koko device ls` shows it, so the Owner finds devices nobody has used
for months.

## Revoking

`koko device revoke <device>` or `<user>/<device>` (`device.revoke`,
[`revoker.revoke`](../../internal/server/device/session.go)) revokes one live
Device Session: a User any of their own, the one in use included, and the
Owner anyone's. A Member naming another User's gets `unknown_device`, the
answer for one that is not there, byte for byte. The Owner's own last live
Device Session needs `--last` (`last_device_session` without it), since the
Owner then adds a device only over the Unix socket.

In one transaction it sets the revoked time, which keeps the record, its name
in `koko device ls --all` and its key for ever, and voids the Join Codes the
Device Session issued that are still valid. The Device Sessions it added stay.
A Revoked Device Session's name is free again for a new device, but
`device-<n>` counts past it ([Join Codes](join-codes.md#issuing)).

Once the transaction commits, the revocation is logged and announced on the
**access-change signal** ([`access.Signal`](../../internal/server/access/signal.go)),
which names the Device Sessions and the Hosts a committed change affects.
`server.Start` subscribes the koko server's `withdraw`
([`server.go`](../../internal/server/server.go)): the Gateway first ends
the Revoked Device Sessions' SSH Sessions, each with its notice
([the Gateway](gateway.md#access-during-a-live-ssh-session)), and once
they have ended, or `timing.StopNotice` (5 s) has passed, the transport
closes their Device Connections
([`transport.Server.CloseDeviceSessions`](../../internal/server/transport/transport.go)):
each one once the calls in flight on it have answered (so a device that
revoked itself gets its answer), at most 5 s later, and any connection of
theirs that authenticated while the revocation committed is refused. A call
in flight gets its own result when it looked up its Device Session before
the revocation committed, and `revoked` otherwise; either answer reaches the
command through the Device Mux ([the Device Mux](device-mux.md#state-and-exit))
and the browser through `koko web` ([the end](web-front.md#koko-web)). Just
before it closes one, it sends the **revocation disconnect**, the SSH
global request [`wire.RequestRevoked`](../../internal/shared/wire/wire.go)
(`revoked@koko`, no payload, no reply wanted), since SSH carries no reason
on a close; the device's `serverconn.Conn.Wait` then returns
`serverconn.ErrRevoked`. The close waits for the device to read what came
before it ([closing a connection](api.md#the-operation-framework)). A
device that misses it learns the same from the Revoked banner on its next
connection.
Removing a User revokes each of their live Device Sessions in the same way,
and names them all as Revoked on the signal, as a Removed User's
([Users](users.md#removing)). The other changes that withdraw access, a
Member taken out of a Space and a Host put out of a caller's reach, name
what they affect in fields of `access.Change` of their own, which only the
Gateway acts on. `server.Start` also subscribes the web front's
`Withdraw`, which ends the Revoked Device Sessions' cookies and tokens at
once, while their Device Connections may still be open
([the web front](web-front.md#runs-tokens-and-the-cookie)).

## On the device

The Device Mux learns of a revocation from the revocation disconnect, or
from the banner when it reconnects ([the Device Mux](device-mux.md#state-and-exit)),
and exits once it has run the cleanup; a command on the direct path
([`Input.dialJoined`](../../internal/client/cli/cli.go)) learns it from
the banner; and `koko device revoke` of this device's own Device Session
learns it from the answer. Each runs the
revocation cleanup ([`devicefiles.Revoke`](../../internal/client/devicefiles/devicefiles.go)):
it writes the revocation record, then deletes the client config, the
device's key and koko's known_hosts, and rewrites koko's ssh_config without
bare names: `*.koko` still runs `koko tunnel`, which explains the
revocation. The command fails with `revoked`, or says that it was this
device. From then on every command that would reach the joined koko server
explains the revocation from the record, with `revoked`, until the next
`koko join`, which the record does not block and which removes it.

**`koko leave`** ([`leave`](../../internal/client/cli/leave.go)), in the
irreversible tier, ends a join on the device alone, for a koko server that
can no longer revoke it, such as one initialised anew after its Root Key
was lost, and for a device retired or moved to another koko server. It
never contacts the koko server. It reads the client config, and on a
device that has not joined fails as any command does (`not_joined`, or
`revoked` with the record). It stops every Device Mux of this OS user as
`koko disconnect` does ([the Device Mux](device-mux.md#state-and-exit)),
so none writes into the device's files afterwards, and fails, deleting
nothing, when one answers but does not stop. Then
([`devicefiles.Leave`](../../internal/client/devicefiles/devicefiles.go))
it deletes what the revocation cleanup deletes, rewrites koko's ssh_config
without bare names in the same way, and deletes any revocation record,
writing none. The Include line in `~/.ssh/config` stays, which the next
join finds in place. It prints the Device Session it left behind and the
Server Address, and says to revoke it from another device or on the koko
server's machine (`koko device revoke <user>/<device>`) if that koko server
still runs; with its key deleted, no device can use it again.

## The record

| Table | What it holds |
| --- | --- |
| `device_sessions` | One row per device that joined: its id (`device_...`), its User, its device name, its public key (SSH wire format, plain, unique across every row), the Device Session that added it (NULL for the Unix socket and for a Sign-in, which `join_codes.issued_by` names), and its created, last used and revoked times. A device name is unique among a User's live Device Sessions. |

## Logging

Each join logs `device joined` with the User, the device, the Device
Session, `added_by` (a Device Session's or a Sign-in's id, or `unix
socket`), the Join Code's
id and the address it came from; each failed join logs `join failed` with
the address, the reason and the Join Code's id when the secret is one's.
Neither ever holds a code or its secret. Each revocation logs
`device revoked` with the User, the device, the Device Session, `revoked_by`
(a Device Session's id or `unix socket`) and `join_codes_voided`; each
Device Connection it closes logs `device connection closed` with the Device
Session, the address and `reason=revoked`; and each later try of the Revoked
key logs `revoked device refused` with the Device Session and the address.
A Device Connection declared dead logs `device connection closed` with the
Device Session, the address, `reason=dead` and the error, `no answer for
<time>`.

## Verifying

- [`join_test.go`](../../internal/client/cli/join_test.go): a join and its
  files, `koko join -`, a damaged code with no connection made, a mismatch
  and the same code joining the right koko server, used, expired and wrong
  codes alike, a second join refused, the log lines.
- [`leave_test.go`](../../internal/client/cli/leave_test.go): S1, a device
  whose koko server was replaced by one initialised anew at its address,
  refused a join naming both ways out, leaving with no connection or call
  made (golden), its Device Mux stopped, koko's files gone but for
  ssh_config without bare names, the user's own `~/.ssh` files byte for
  byte, and joining the new koko server; the Device Session left behind
  named and revoked on the koko server's machine; a device that has not
  joined (golden); a Device Mux that does not stop keeping the join; the
  irreversible tier.
- [`sshconfig_test.go`](../../internal/client/cli/sshconfig_test.go): a
  join's ssh_config (golden) and known_hosts, the Include line when
  `~/.ssh/config` is absent, the user's, already includes it (however
  written, or below a `Host` line, with the warning), is a symlink, is
  read-only or is not a regular file, `--no-ssh-config`, a HOME ssh would
  misread refused before the code is spent, and the revocation cleanup of
  the bare names and known_hosts;
  [`devicefiles/ssh_test.go`](../../internal/client/devicefiles/ssh_test.go):
  the 1 KiB split (golden), the keywords and paths, the ProxyCommand's path
  by PATH, the refusals, and the file written only when it would change.
- [`barenames_test.go`](../../internal/client/cli/barenames_test.go): the
  bare names a join writes for a Member and for the Owner, Retired Hosts
  left out; each of the caller's own Host changes rewriting them, with a
  Host someone else recorded since; none written over the Unix socket; a
  change whose bare names cannot be written holding, with its line on
  stderr; and the Device Mux's refresh ([the Device Mux](device-mux.md#verifying)).
- [`sshsetup_test.go`](../../internal/client/cli/sshsetup_test.go): `koko
  ssh setup` over the Unix socket (golden): koko's ssh_config with every
  Host but the Retired, known_hosts with the host key `server.show` names,
  the Include line, the modes, nothing of a join, the bare names rewritten
  by a Host change over the socket, and the same files when run again; a
  read-only `~/.ssh/config` left alone with its warning; refused on a
  joined device with no call made, and `not_joined` with no koko server.
- [`openssh_test.go`](../../internal/client/openssh/openssh_test.go): the
  system's OpenSSH through koko's ssh_config, on a joined device and on the
  koko server's own machine ([the Gateway](gateway.md#verifying)).
- [`status_test.go`](../../internal/client/cli/status_test.go): `koko
  status` joined, over the socket, not joined, revoked and unreachable, and
  its Include warnings.
- [`retry_test.go`](../../internal/client/cli/retry_test.go)
  `TestAJoinWhoseCallIsCutStillJoins`: `user.current` cut mid-flight and sent
  again on a Device Connection of the new key.
- [`connect_test.go`](../../internal/client/cli/connect_test.go): the pinned
  list of one and of several keys, an unknown key, `server_unreachable` with
  its hint, and the socket winning over the joined koko server.
- [`connect_command_test.go`](../../internal/client/cli/connect_command_test.go):
  `koko connect` through a Device Mux it starts (golden) and direct; a new
  address refused where another host key answers, where nothing answers and
  without a port, and taken where the pinned one answers (golden), the
  host keys kept and no connection to the old address after; and the P0
  restore: a stopped koko server's data directory copied and started
  elsewhere with the same Root Key, which a joined device reaches with its
  pin after `koko connect <host:port>`.
- [`reconnect_test.go`](../../internal/client/cli/reconnect_test.go): the
  15 s wait, and the koko server's probe and dead close
  ([the Device Mux](device-mux.md#verifying)).
- [`device_test.go`](../../internal/client/cli/device_test.go): a joined
  Member issues for themselves only, names held by live Device Sessions, and
  nothing of a code in the record or the log after joining; `koko device ls`
  for the Owner and for a Member, "added by", the last used time to the
  minute, and its table for people.
- [`device_revoke_test.go`](../../internal/client/cli/device_revoke_test.go): revoking
  anyone's as the Owner and only one's own as a Member (unknown, byte for
  byte), `--last`, the Device Connections closed and no other's, the device
  in use answered and cleaned up, a device revoked elsewhere told by name with
  its files deleted and the record explained until a join clears it, the
  issued Join Codes voided, `device-<n>` past Revoked ones, the log lines.
- [`devicemux_test.go`](../../internal/client/cli/devicemux_test.go)
  `TestTheDeviceMuxRunsTheRevocationCleanupAndExits`: the revocation
  disconnect cleans up a device whose Device Mux holds its Device
  Connection, with no command running.
- [`revoked_test.go`](../../internal/server/revoked_test.go): a Revoked
  Device Session's call refused by every operation; and the `device.list`
  and `device.revoke` rows of the
  [access matrix](access.md#the-access-matrix).
- [`tcp_test.go`](../../internal/server/transport/tcp_test.go): the
  transport's authentication and join step with the client's dialer, the
  Revoked banner only after the signature, and closing a Device Session's
  connections while refusing its new ones; and the answer and the
  revocation disconnect over a slow link ([the API](api.md#verifying)).
