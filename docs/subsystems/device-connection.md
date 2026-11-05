# The Device Connection and joining

How a device reaches the koko server over TCP, how a new device joins with a
Join Code, and what a joined device keeps. The design is P0 spec: reach ("The
Device Connection") and P0 spec: people and Spaces ("Joining"); why devices
join with a one-time code is
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
   ([`device.Authenticator.DeviceSession`](../../internal/server/device/join.go)): a
   live Device Session's key authenticates the connection as that Device
   Session, which every call on it carries (`op.Caller.DeviceSession`).
2. **A key no Device Session holds** gets a partial success, with `password`
   as the only method left: the device has proved it holds the key, and may
   now join. A device that is not joining has no password to give: the CLI
   reports that the koko server holds no Device Session with its key
   (`not_joined`), naming no User and no device.
3. **The join step.** The password is the Join Code's secret in lowercase hex
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
   device name the code bound, and the Device Session's id.
7. It puts the key in place and writes the client config, which marks the
   device joined, then prints the Server Address and the Device Session:
   `{"server_address", "device_session": {"id", "user", "device",
   "created_at"}}` with `--json`.

**A failed join leaves nothing it wrote:** the staged key is removed, and so
are `~/.ssh` and `~/.ssh/koko/` when the join made them and they are empty
(a `~/.ssh/koko/` that was there stays, made private). A join that fails
after the koko server accepted the code (a lost connection before step 7)
says that the code is spent and which Device Session to revoke, when it
knows.

## The device's files

[`devicefiles`](../../internal/client/devicefiles/devicefiles.go) is the one
place a device's join is written and read, so whatever ends a join on the
device removes it there.

| Path | Mode | What |
| --- | --- | --- |
| `~/.ssh/koko/` | 0700 | koko's directory, made private even when it was there; `~/.ssh`, when koko makes it, is 0700 too |
| `~/.ssh/koko/device_key` | 0600 | the device's private key: OpenSSH-format ed25519, unencrypted, since `koko tunnel` cannot prompt |
| `~/.ssh/koko/client.json` | 0600 | the client config, below |

The client config is JSON: `server_address` (`host:port`), `host_keys` (the
pinned host keys, each written as in `authorized_keys`; a Device Connection
takes any of them, and a join pins the one its code names), `user`, `device`
and `device_session` (the id). A client config or key that cannot be read or
parsed fails the command naming the file, and never counts as not joined.
Making the directory private sits behind a per-OS file; on Windows a join
fails as not supported until the Windows CLI (P1).

## Which koko server the CLI reaches

A command reaches the koko server on its first call
([`Input.connect`](../../internal/client/cli/cli.go)): over the Unix socket
when the socket is at its path, as the Local caller; otherwise over a direct
Device Connection to the koko server this device joined
([`serverconn.DialDevice`](../../internal/client/serverconn/serverconn.go)),
checked against the pinned host keys. Both are a
[`serverconn.Conn`](../../internal/client/serverconn/serverconn.go), so a
command does not know which it has. A failure is `not_joined`,
`server_unreachable` or `host_key_mismatch`, as [the CLI](cli.md#the-runner-and-the-output-layer)
describes them.

## The record

| Table | What it holds |
| --- | --- |
| `device_sessions` | One row per device that joined: its id (`device_...`), its User, its device name, its public key (SSH wire format, plain, unique across every row), the Device Session that added it (NULL for the Unix socket), and its created and revoked times. A device name is unique among a User's live Device Sessions. |

## Logging

Each join logs `device joined` with the User, the device, the Device
Session, `added_by` (a Device Session's id or `unix socket`), the Join Code's
id and the address it came from; each failed join logs `join failed` with
the address, the reason and the Join Code's id when the secret is one's.
Neither ever holds a code or its secret.

## Verifying

- [`join_test.go`](../../internal/client/cli/join_test.go): a join and its
  files, `koko join -`, a damaged code with no connection made, a mismatch
  and the same code joining the right koko server, used, expired and wrong
  codes alike, a second join refused, the log lines.
- [`connect_test.go`](../../internal/client/cli/connect_test.go): the pinned
  list of one and of several keys, an unknown key, `server_unreachable` with
  its hint, and the socket winning over the joined koko server.
- [`device_test.go`](../../internal/client/cli/device_test.go): a joined
  Member issues for themselves only, names held by live Device Sessions, and
  nothing of a code in the record or the log after joining.
- [`tcp_test.go`](../../internal/server/transport/tcp_test.go): the
  transport's authentication and join step with the client's dialer.
