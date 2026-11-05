# The Device Mux

The background process on a joined device that holds the device's one
Device Connection and carries every koko command, `koko web` and `koko
tunnel` of the device over it, in
[`internal/client/devicemux`](../../internal/client/devicemux). Why it exists
is [ADR 0017](../adr/0017-a-device-mux-holds-the-device-connection.md); the
design is P0 spec: reach ("The Device Mux").

## Which way a command goes

A command reaches the koko server on its first call
([`Input.dialJoined`](../../internal/client/cli/cli.go)): the Unix socket
first, when one is at its path ([the Device Connection](device-connection.md#which-koko-server-the-cli-reaches)),
then, on a joined device, this build's Device Mux
([`Input.viaDeviceMux`](../../internal/client/cli/devicemux.go)):

1. It connects to the Device Mux socket of its build.
2. When none answers, it starts one (`cli.Runner.StartDeviceMux`, which
   `cmd/koko` sets to [`devicemux.Spawn`](../../internal/client/devicemux/socket.go))
   and polls for its socket, or the socket of the one that won the race to
   start, for at most `timing.DeviceMuxStart`, or until the Device Mux it
   started exits.
3. A Device Mux that answers decides the outcome: the command goes through
   it, or fails with the error it answered, mapped as a direct connection's
   would be (`not_joined`, `revoked`, `server_unreachable`,
   `host_key_mismatch`).

**The direct path.** The command opens its own Device Connection
(`serverconn.DialDevice`), with the same code, and logs one debug line on
stderr (`opening a direct Device Connection`, with the reason, when
`KOKO_LOG_LEVEL=debug`), when no Device Mux can carry it: `KOKO_DIRECT=1`
is set, the runtime directory is refused, the OS has no Device Mux yet
(Windows, P1), the Device Mux of its build carries another home's device
([one device, one home](#the-local-protocol)), or the Device Mux it
started exited, as in a sandbox that lets it start but not listen, or did
not answer in time. A runner without `StartDeviceMux`, as in most S1 tests,
always goes direct.

`koko join` starts this build's Device Mux once the device has joined, and
does not wait for it, so the next command finds the Device Connection open;
a join that cannot start one still joins. With `KOKO_DIRECT=1` it starts
none.

## Starting, the lock and the socket

- **The entry point.** `koko device-mux` runs a Device Mux in the
  foreground ([`Runner.runDeviceMux`](../../internal/client/cli/devicemux.go)).
  It is no command of the tree: help, completion and `koko skill` never
  list it. [`devicemux.Spawn`](../../internal/client/devicemux/socket.go)
  starts the running binary with it, in a session of its own (no
  controlling terminal, so no hang-up or Ctrl-C reaches it), stdin from
  `/dev/null`, working directory `/`, the command's environment, and stdout
  and stderr appended to the log beside its socket, emptied first past
  1 MiB. Nothing is installed as a service.
- **The runtime directory** ([`devicemux.Socket`](../../internal/client/devicemux/socket.go),
  per OS in `dir_linux.go` and `dir_darwin.go`): `$XDG_RUNTIME_DIR/koko/` on
  Linux, `$TMPDIR/koko/` on macOS, and `/tmp/koko-<uid>/` without that
  variable. koko makes it with mode 0700 and refuses it, so commands go
  direct, unless it is a directory (not a symlink) this OS user owns with
  mode 0700.
- **The name** ([`devicemux.Name`](../../internal/client/devicemux/socket.go))
  carries the build: the version, then the first 12 characters of the VCS
  revision unless the version holds them already (a pseudo-version does),
  with any character but letters, digits, `.`, `-` and `+` written `_`:
  `v0.3.0-1a2b3c4d5e6f.sock`, `.lock` and `.log`. A CLI reaches only a
  Device Mux of its own build, so the local protocol needs no compatibility
  rules; after an upgrade the old one serves what it carries and idles out.
  A socket path past 103 bytes (a Unix socket's limit on macOS is 104) is
  refused, and the command goes direct.
- **One per build** ([`devicemux.Run`](../../internal/client/devicemux/devicemux.go)):
  a Device Mux takes an exclusive `flock` on its `.lock` file first, never
  removed, which the kernel releases when the process ends, so one that
  crashed holds none. One that finds it taken retries for up to
  `timing.DeviceMuxStart`, for one that is exiting, then exits 0 without
  touching the socket. Holding the lock, it removes any socket left behind,
  listens, and opens its Device Connection at once.
- **Same user only.** Each connection's peer credentials (`SO_PEERCRED` on
  Linux, `LOCAL_PEERCRED` on macOS: `peerUIDOf` in `platform_linux.go` and
  `platform_darwin.go`) must name this OS user, or it is closed and logged.

## The local protocol

The koko server's own SSH, as on its Unix socket
([the API](api.md#the-way-in)), with the Device Mux's state in the banner
([`local.go`](../../internal/client/devicemux/local.go)):

- A command connects to the socket and pins no host key: the socket's
  private directory is its pin. The Device Mux's host key is made in memory
  at each start. Authentication is `none`.
- **One device, one home.** A device is an OS user's home (P0 spec: reach,
  "One koko server per device"), and a Device Mux carries only the device
  of the home it was started in. The SSH user name is the command's
  `HOME`; to a command of another home the Device Mux answers
  `{"state": "elsewhere", "home"}` at once (`devicemux.ErrOtherHome`), and
  that command goes direct.
- At the start of the authentication the Device Mux makes sure it holds a
  Device Connection, opening one when it has none (one try at a time, which
  every command waiting shares), and sends its state as the banner, JSON:
  `{"state": "connected"|"reconnecting", "since": <RFC 3339>, "error":
  {"kind", "message", ...}}`. `error` comes only while reconnecting; its
  `kind` is `host_key_mismatch` (with `address`, `presented`, `pinned`),
  `unknown_key`, `revoked` (with `device`), `refused`, `not_joined` or
  `failed`, and [`devicemux.Dial`](../../internal/client/devicemux/local.go)
  turns it back into the error `serverconn.DialDevice` would have returned.
  It accepts the authentication only when connected. A Device Mux that is
  stopping because it idled out sends no state, so the command starts a
  new one.
- It sends the report of its Device Connection as `report@koko`, and again
  on every open connection whenever it opens a new Device Connection, so
  `serverconn.Conn.Report` holds the latest.
- It carries each channel a command opens, of any type, onto its Device
  Connection with the same open payload, unparsed, and passes a refusal
  back with the koko server's reason and message. Bytes go both ways,
  half-closes included, until both directions end or the Device Connection
  does. It holds no Tunnel state.
- When the Device Connection drops, the channels on it end, so an API call
  in flight fails as a drop and [the API client](api.md#the-api-client)
  sends it once more; commands stay connected, and a channel opened next
  waits for the next Device Connection. An open is sent once: one the drop
  cut after it went out is refused, since the koko server may have acted on
  it, as on a Tunnel's open; only one that never went out, the Device
  Connection having ended before the Device Mux let go of it, goes on the
  next one.
- When the koko server revokes its Device Session, it sends each open
  connection `revoked@koko` before closing it, so `Conn.Wait` returns
  `serverconn.ErrRevoked`.

## State and exit

- **Connected**, since it opened its Device Connection, or **reconnecting**
  since it dropped (or since the first try failed), with the last try's
  error. In P0 it tries again only when a command connects or opens a
  channel; probing, backoff and the 15 s wait are #54's.
- **Idle exit.** It exits `timing.DeviceMuxIdle` (10 minutes) after the
  last command left: a command, `koko web` or `koko tunnel` connected keeps
  it running, with or without a channel open, so `koko web` between browser
  connections counts as open (#62). A command that connects as it idles out
  is refused, and starts a new one.
- **Revocation.** On the revocation disconnect
  ([`wire.RequestRevoked`](../../internal/shared/wire/wire.go)), or the
  Revoked banner when it reconnects, it runs the revocation cleanup
  ([`devicefiles.Revoke`](../../internal/client/devicefiles/devicefiles.go))
  holding its lock, so no command hears of the revocation before the
  cleanup is done, then tells each command and exits. A command it cut off
  finds the revocation record when it tries again. A device whose join is
  gone when it reconnects (`not_joined`) stops it too.
- On stopping it closes its listener, which removes the socket, then every
  connection, then releases the lock.

## Logging

One line per event on its stderr (the log beside its socket), at the level
`KOKO_LOG_LEVEL` names, info by default: `device mux started` (version,
revision, pid, socket), `device connection opened` (server, server
version), `cannot open a Device Connection` and `device connection
dropped` (server, error), the revocation cleanup done or failed, `refused a
connection from another OS user`, and `device mux stopped` with its reason.

## Verifying

- [`devicemux_test.go`](../../internal/client/cli/devicemux_test.go) (S1,
  the Device Mux run in the test process through
  [`harness.NewMuxDevice`](../../internal/harness/devicemux.go)): commands
  share its Device Connection and `KOKO_DIRECT=1` goes direct; commands
  racing to start it make one; a second one exits at the lock; the idle
  exit and a command connected keeping it; the revocation disconnect, and
  revoking the device in use through it; the runtime directory refusals and
  the debug line; another home going direct as its own User; a Device Mux
  that exits at once sending the command direct at once; an unknown
  `KOKO_LOG_LEVEL` refused; a call cut under it retried and applied once;
  channels across a drop, an open the drop cut not sent twice, a refusal
  passed back, and its report.

The peer-credential refusal of another OS user, and a runtime directory
another user owns, are not tested: both need a second OS user, which a
test run as one user cannot be, and a hook to fake one would be a fifth
test-only hook (code layout, rule 10). `peerUID` reading the connecting
user's id is tested.
- [`status_test.go`](../../internal/client/cli/status_test.go): `koko
  status` connected (golden) and reconnecting, and `null` on the direct
  path.
- [`devicemux` package tests](../../internal/client/devicemux): the name,
  the macOS path limit, the runtime directory, the peer credentials, and a
  command handed the report of the next Device Connection.
