# The Device Connection and joining

How a device reaches the koko server over TCP, how a new device joins with a
Join Code, what a joined device keeps, and how a Device Session is used and
revoked. The design is P0 spec: reach ("The Device Connection") and P0 spec:
people and Spaces ("Joining", "Device Sessions"); why devices join with a
one-time code is
[ADR 0013](../adr/0013-devices-join-with-a-one-time-join-code.md).

## The TCP listener

- **When it listens.** Only once the koko server is initialised: from
  initialisation on, and at every later start at once
  ([initialisation](initialisation.md#the-operation)); its lifecycle is
  [`tcpListener`](../../internal/server/listen.go).
- **Where.** `koko server run --listen <addr>` (`KOKO_LISTEN`, default
  `:443`), a host and a port number. A Server Address without a port names
  the port the listener bound ([`joinTarget`](../../internal/server/jointarget.go)).
- **Its host key** is the koko server's sealed host key, the one Join Codes
  name, the same across restarts. The Unix socket keeps its own key made at
  each start ([the API](api.md#the-way-in)).
- **SSH** with the version string `SSH-2.0-koko`, carrying the socket's
  channels, `api@koko` and `web@koko`, and refusing any other type
  ([`transport.ServeTCP`](../../internal/server/transport/transport.go)).
  `web.start` answers only the Local caller, so the web front serves a
  Device Connection nothing yet.

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
   to revoke this device's Device Session first.
3. It makes a new ed25519 key and writes it into `~/.ssh/koko/` under a
   temporary name, so a device that cannot keep its files learns it before it
   spends the code.
4. It connects to the code's address and checks the host key against the
   code's fingerprint before it authenticates; a mismatch fails with
   `host_key_mismatch`, naming both keys, and the code stays usable.
5. It authenticates in the two steps above
   ([`serverconn.Join`](../../internal/client/serverconn/serverconn.go)); the
   banner of a refusal is the error, with `invalid_join_code`.
6. On that connection it calls `user.current`, which names the User and the
   device name the code bound, and the Device Session's id. When the
   connection drops the call, the call is sent again, once, on a new Device
   Connection with the key, now a Device Session
   ([the API client](api.md#the-api-client)).
7. It puts the key in place and writes the client config, which marks the
   device joined, removes any revocation record, then prints the Server
   Address and the Device Session: `{"server_address", "device_session"}`
   with `--json`, the Device Session as `koko device ls --json` prints one.

**A failed join leaves nothing it wrote:** the staged key is removed, and so
are `~/.ssh` and `~/.ssh/koko/` when the join made them and they are empty
(a `~/.ssh/koko/` that was there stays, made private). A join that fails
after the koko server accepted the code (`user.current` lost on both
connections, before step 7) says that the code is spent and which Device
Session to revoke, when it knows.

## The device's files

[`devicefiles`](../../internal/client/devicefiles/devicefiles.go) is the one
place a device's join is written and read, so whatever ends a join on the
device removes it there.

| Path | Mode | What |
| --- | --- | --- |
| `~/.ssh/koko/` | 0700 | koko's directory, made private even when it was there; `~/.ssh`, when koko makes it, is 0700 too |
| `~/.ssh/koko/device_key` | 0600 | the device's private key: OpenSSH-format ed25519, unencrypted, since `koko tunnel` cannot prompt |
| `~/.ssh/koko/client.json` | 0600 | the client config, below |
| `~/.ssh/koko/revoked.json` | 0600 | the revocation record, in place of the key and the client config once the device learns that its Device Session was Revoked ([On the device](#on-the-device)) |

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

## Which koko server the CLI reaches

A command reaches the koko server on its first call
([`Input.connect`](../../internal/client/cli/cli.go)): over the Unix socket
when the socket is at its path, as the Local caller; otherwise over a direct
Device Connection to the koko server this device joined
([`serverconn.DialDevice`](../../internal/client/serverconn/serverconn.go)),
checked against the pinned host keys. Both are a
[`serverconn.Conn`](../../internal/client/serverconn/serverconn.go), so a
command does not know which it has. A failure is `not_joined`,
`revoked`, `server_unreachable` or `host_key_mismatch`, as [the CLI](cli.md#the-runner-and-the-output-layer)
describes them.

## Last used

A Device Session is used each time a Device Connection it authenticated
opens a channel (`api@koko` or `web@koko`): a command, a browser connection,
`koko join`'s own call. A long-lived connection, such as the Device Mux's,
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
which names the Device Sessions a committed change affects. `server.Start`
subscribes the transport, which closes their Device Connections
([`transport.Server.CloseDeviceSessions`](../../internal/server/transport/transport.go)):
each one once the calls in flight on it have answered (so a device that
revoked itself gets its answer), at most 5 s later, and any connection of
theirs that authenticated while the revocation committed is refused. Later
changes that withdraw access (a User Removed, a Member taken out of a Space,
a Host's access withdrawn) add fields to `access.Change`; later consumers
(the Gateway, the web front's cookies) subscribe.

## On the device

A command reaching the koko server over its Device Connection
([`Input.dialJoined`](../../internal/client/cli/cli.go)) learns of a
revocation from the banner, and `koko device revoke` of this device's own
Device Session learns it from the answer. Either way the device runs the
revocation cleanup ([`devicefiles.Revoke`](../../internal/client/devicefiles/devicefiles.go)):
it writes the revocation record, then deletes the client config and the
device's key, and the command fails with `revoked`, or says that it was this
device. From then on every command that would reach the joined koko server
explains the revocation from the record, with `revoked`, until the next
`koko join`, which the record does not block and which removes it.

## The record

| Table | What it holds |
| --- | --- |
| `device_sessions` | One row per device that joined: its id (`device_...`), its User, its device name, its public key (SSH wire format, plain, unique across every row), the Device Session that added it (NULL for the Unix socket), and its created, last used and revoked times. A device name is unique among a User's live Device Sessions. |

## Logging

Each join logs `device joined` with the User, the device, the Device
Session, `added_by` (a Device Session's id or `unix socket`), the Join Code's
id and the address it came from; each failed join logs `join failed` with
the address, the reason and the Join Code's id when the secret is one's.
Neither ever holds a code or its secret. Each revocation logs
`device revoked` with the User, the device, the Device Session, `revoked_by`
(a Device Session's id or `unix socket`) and `join_codes_voided`; each
Device Connection it closes logs `device connection closed` with the Device
Session, the address and `reason=revoked`; and each later try of the Revoked
key logs `revoked device refused` with the Device Session and the address.

## Verifying

- [`join_test.go`](../../internal/client/cli/join_test.go): a join and its
  files, `koko join -`, a damaged code with no connection made, a mismatch
  and the same code joining the right koko server, used, expired and wrong
  codes alike, a second join refused, the log lines.
- [`retry_test.go`](../../internal/client/cli/retry_test.go)
  `TestAJoinWhoseCallIsCutStillJoins`: `user.current` cut mid-flight and sent
  again on a Device Connection of the new key.
- [`connect_test.go`](../../internal/client/cli/connect_test.go): the pinned
  list of one and of several keys, an unknown key, `server_unreachable` with
  its hint, and the socket winning over the joined koko server.
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
- [`revoked_test.go`](../../internal/server/revoked_test.go): a Revoked
  Device Session's call refused by every operation; and the `device.list`
  and `device.revoke` rows of the
  [access matrix](access.md#the-access-matrix).
- [`tcp_test.go`](../../internal/server/transport/tcp_test.go): the
  transport's authentication and join step with the client's dialer, the
  Revoked banner only after the signature, and closing a Device Session's
  connections while refusing its new ones.
