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
   It first reads the build of the file it would start, its own
   (`cli.Runner.Executable`), and goes direct at once when that file is now
   another build, as after [`koko update`](cli.md#koko-update) replaced
   it: that Device Mux would listen on its own build's socket, never this
   one ([`Input.spawnDeviceMux`](../../internal/client/cli/devicemux.go)).
3. A Device Mux that answers decides the outcome: the command goes through
   it, or fails with the error it answered, mapped as a direct connection's
   would be (`not_joined`, `revoked`, `server_unreachable`,
   `host_key_mismatch`). A Device Mux answers within `timing.CommandWait`
   (15 s), the longest it waits for its Device Connection
   ([the local protocol](#the-local-protocol)); a command gives up on one
   that has not answered `timing.DeviceMuxStart` after that, and waits for
   the report it sends next within the same bound.

**The direct path.** The command opens its own Device Connection
(`serverconn.DialDevice`), with the same code, and logs one debug line on
stderr (`opening a direct Device Connection`, with the reason, when
`KOKO_LOG_LEVEL=debug`), when no Device Mux can carry it: `KOKO_DIRECT=1`
is set, the runtime directory is refused, the OS has no Device Mux (none
but Linux, macOS and Windows), the Device Mux of its build carries another home's device
([one device, one home](#the-local-protocol)), or the Device Mux it
started exited, as in a sandbox that lets it start but not listen, or did
not answer in time. A runner without `StartDeviceMux`, as in most S1 tests,
always goes direct. On the direct path the command
([`Input.dialDirect`](../../internal/client/cli/cli.go)) waits for the
Device Connection as a Device Mux would: while it cannot reach the koko
server it tries again after `timing.ReconnectFirst` (250 ms), doubling the
wait up to `timing.ReconnectMax` (5 s), each try lasting at most
`timing.DeviceConnectAttempt`, for what is left of `timing.CommandWait`
since the command began to connect, report included, and a failure trying
again does not change (`serverconn.Permanent`: a host key mismatch, an
unknown or Revoked key, a refusal) ends it at once; its Device Connection probes the koko server as
the Device Mux's does ([liveness](#liveness-and-reconnecting)).

`koko join` starts this build's Device Mux once the device has joined, and
does not wait for it, so the next command finds the Device Connection open;
a join that cannot start one still joins. With `KOKO_DIRECT=1` it starts
none.

## Starting, the lock and the socket

- **The entry point.** `koko device-mux` runs a Device Mux in the
  foreground ([`Runner.runDeviceMux`](../../internal/client/cli/devicemux.go)).
  It is no command of the tree: help, completion and `koko skill` never
  list it. [`devicemux.Spawn`](../../internal/client/devicemux/socket.go)
  starts the running binary with it, detached (`startDetached`, per OS in
  `sys_unix.go` and `sys_windows.go`), stdin from the null device, the
  command's environment, and stdout and stderr appended to the log beside
  its socket, emptied first past 1 MiB. On Linux and macOS it runs in a
  session of its own (no controlling terminal, so no hang-up or Ctrl-C
  reaches it), working directory `/`. On Windows it starts with
  `DETACHED_PROCESS` and `CREATE_NEW_PROCESS_GROUP` (no console, so
  neither Ctrl-C nor the closing of the console that started it reaches
  it), working directory the root of the Windows directory's drive, and
  asks for `CREATE_BREAKAWAY_FROM_JOB`, to outlive a job object that ends
  its processes when it closes, as some terminals' and CI runners' do; a
  job that forbids breaking away refuses that start, and it starts again
  inside the job. Nothing is installed as a service.
- **The runtime directory** ([`devicemux.Socket`](../../internal/client/devicemux/socket.go),
  per OS in `platform_unix.go` and `platform_windows.go`):
  `$XDG_RUNTIME_DIR/koko/` on Linux, `$TMPDIR/koko/` on macOS, and
  `/tmp/koko-<uid>/` without that variable; on Windows
  `%LOCALAPPDATA%\koko\run\`, local to the machine since a socket must not
  roam with a domain profile, and none without `LOCALAPPDATA`. koko makes
  it private to the user from the moment it exists
  ([`devicefiles.MkdirPrivate`](../../internal/client/devicefiles/private_windows.go)):
  mode 0700, or on Windows this user as owner and a protected access
  control list with full control for this user, `SYSTEM` and
  `Administrators`, passed on to the socket made in it, given at its
  creation so no racing command finds it open (`%LOCALAPPDATA%\koko`
  above it takes the user's own list); the lock and the log are made
  private as koko's own files are
  ([`devicefiles.MakePrivate`](../../internal/client/devicefiles/devicefiles.go)):
  mode 0600, or that list of their own, protected. It refuses
  it, so commands go direct, unless it is a directory (not a symlink or a
  junction) this OS user owns (on Windows, or `Administrators` for an
  elevated process) and no other can reach into
  ([`devicefiles.CheckPrivateDir`](../../internal/client/devicefiles/devicefiles.go)):
  mode 0700, or on Windows a protected list whose every entry that allows
  access, its own or one passed on to what it holds, names one of the three
  principals; an entry that denies access is no way in, and one of a type
  koko does not read refuses the directory. A Device Mux checks it again at
  each start.
- **The name** ([`devicemux.Name`](../../internal/client/devicemux/socket.go))
  carries the build: the version, then the first 12 characters of the VCS
  revision unless the version holds them already (a pseudo-version does),
  with any character but letters, digits, `.`, `-` and `+` written `_`:
  `v0.3.0-1a2b3c4d5e6f.sock`, `.lock` and `.log`. A CLI reaches only a
  Device Mux of its own build, so the local protocol needs no compatibility
  rules; after an upgrade the old one serves what it carries and idles out.
  A socket path past 103 bytes (a Unix socket's limit on macOS is 104, on
  Linux and Windows 108) is refused, and the command goes direct; Windows
  takes a path of 103 bytes. A profile path of ordinary length leaves room:
  `C:\Users\alice\AppData\Local\koko\run\v0.5.0-1a2b3c4d5e6f.sock` is 62
  bytes.
- **One per build** ([`devicemux.Run`](../../internal/client/devicemux/devicemux.go)):
  a Device Mux takes an exclusive lock on its `.lock` file first, never
  removed, which the OS releases when the process ends, so one that crashed
  holds none: `flock` on Linux and macOS, and on Windows `LockFileEx`,
  exclusive and failing at once, as Go's own toolchain locks a file
  (`tryLock` in `sys_unix.go` and `sys_windows.go`). One that finds it
  taken exits 0 at once, without touching the socket, when the socket
  accepts a connection: the holder serves, as the winner of a race to start
  does. Otherwise it retries for up to `timing.DeviceMuxStart`, for one that is
  exiting, which closes its socket as it stops, before it refuses any
  command; then it exits 0. A loser that waited on would take over should
  `koko disconnect` stop the winner meanwhile, and run with nothing to
  carry (#181). Holding the lock, it removes any socket left behind,
  listens, and opens its Device Connection at once.
- **Same user only** (`sameUser`, per OS in `platform_unix.go` and
  `platform_windows.go`). On Linux and macOS each connection's peer
  credentials (`SO_PEERCRED` on Linux, `LOCAL_PEERCRED` on macOS:
  `peerUIDOf` in `platform_linux.go` and `platform_darwin.go`) must name
  this OS user, or it is closed and logged. Windows gives a Unix socket no
  peer credentials, so there every connection is admitted: the runtime
  directory is the only gate, since connecting to a socket takes write
  access to its file, which the directory passes on to this user, `SYSTEM`
  and `Administrators` alone
  ([ADR 0051](../adr/0051-on-windows-the-cli-is-a-device-with-the-same-files-in-the-same-places.md)).

## The local protocol

The koko server's own SSH, as on its Unix socket
([the API](api.md#the-way-in)), with the Device Mux's state in the banner
([`local.go`](../../internal/client/devicemux/local.go)):

- A command connects to the socket and pins no host key: the socket's
  private directory is its pin. The Device Mux's host key is made in memory
  at each start. Authentication is `none`.
- **Watched on Windows.** There the end of a Unix socket that reads can
  miss the other end's close when it is writing itself as the close comes,
  and then waits for ever ([the `windows` job](release.md#the-windows-job)):
  a command writing as its Device Mux stops or exits, or a Device Mux
  passing on what the koko server sends as a command exits. The Device Mux
  never half-closes a command's connection, which is where the end went
  unseen first: it closes it outright. Still, every wait on the
  connection ends only with it: a command's `Conn.Wait`, on which `koko
  web`, `koko tunnel` and `koko exec` learn that it ended, and the reads
  and opens of its channels; and the Device Mux's wait for a command to
  leave, on which its idle exit and the channels it carries for the
  command hang. So on Windows (`watchLocal`) both ends watch the connection
  ([`liveness.Conn.WatchAnswers`](../../internal/shared/liveness/liveness.go)):
  once nothing has come from the other end for `timing.LocalProbe` (2 s),
  an end probes it with `keepalive@openssh.com`, and once a probe has gone
  unanswered for `timing.LocalAnswer` (5 s) it closes the connection. The
  command's side ([`serverconn.HandshakeLocal`](../../internal/client/serverconn/serverconn.go))
  then fails with `no answer for <time>` from `Conn.Wait`, as for a dead
  Device Connection, and the Device Mux's side
  (`deviceMux.watchCommand`) lets the command leave, so it can idle out. A
  silence alone is never counted: a process on the same machine answers
  whenever it is asked, and Windows' monotonic clock runs on while the
  machine sleeps, so a sleep would read as a silence. The Device Mux's
  side of the handshake is bounded by `timing.CommandWait` and
  `timing.DeviceMuxStart`, the longest a command waits for it; a
  command's side by the same (`dialJoined`), and that of `koko
  disconnect` and `koko leave` by `timing.CommandWait`
  (`devicemux.Stop`). A reading end can miss what
  comes before the close too: `koko disconnect` once missed a Device
  Mux's whole answer, banner and refusal, and the close 20 s after it
  (#184). On Linux and
  macOS, whose sockets always tell each end of the other's close, neither
  end probes.
- **One device, one home.** A device is an OS user's home (P0 spec: reach,
  "One koko server per device"), and a Device Mux carries only the device
  of the home it was started in. The SSH user name is the command's
  `HOME`; to a command of another home the Device Mux answers
  `{"state": "elsewhere", "home"}` at once (`devicemux.ErrOtherHome`), and
  that command goes direct.
- At the start of the authentication the Device Mux makes sure it holds a
  Device Connection, waiting up to `timing.CommandWait` while it reconnects
  ([liveness](#liveness-and-reconnecting)), and less when a try fails for a
  reason trying again does not change, and sends its state as the banner,
  JSON:
  `{"state": "connected"|"reconnecting", "since": <RFC 3339>, "error":
  {"kind", "message", ...}, "resuming": <n>}`. `resuming`, the Tunnels
  through it waiting to Resume (below), comes only when there are any.
  `error` comes only while reconnecting; its
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
- It carries each channel a command opens, of any type but its own
  `resuming@koko` (below), onto its Device
  Connection with the same open payload, unparsed, and passes a refusal
  back with the koko server's reason and message. Bytes go both ways,
  half-closes included, until both directions end or the Device Connection
  does. It holds no Tunnel state.
- When the Device Connection drops, the channels on it end once each has
  passed on to its command what the koko server sent before the drop, or
  `timing.PassOn` (5 s) has passed, for a command that does not read
  ([`pipe`](../../internal/client/devicemux/devicemux.go)). So an API call
  the koko server answered before the drop gets its answer; one still in
  flight fails as a drop, [the API client](api.md#the-api-client)
  sends it once more, and a Tunnel's process Resumes it
  ([the device end of a Tunnel](gateway.md#the-device-end-of-a-tunnel));
  commands stay connected, and a channel opened next waits for the next
  Device Connection. An open is sent once: one the drop
  cut after it went out is refused, since the koko server may have acted on
  it, as on a Tunnel's open; only one that never went out, the Device
  Connection having ended before the Device Mux let go of it, goes on the
  next one, whether the koko server closed it or the probe found it dead
  ([`sshconn.Wrap`](../../internal/shared/sshconn/sshconn.go)).
- **Waiting to Resume.** A command holds a `resuming@koko` channel open on
  the Device Mux while one of its Tunnels waits to Resume
  ([`devicemux.Conn.WaitingToResume`](../../internal/client/devicemux/local.go)).
  The Device Mux carries it to no koko server, and counts each one open in
  its state, so `koko status` shows how many SSH Sessions wait to Resume,
  and among what waits for a Device Connection, so it keeps reconnecting,
  with its backoff, for as long as one is open. One whose command exits,
  or whose Device Mux crashed, is no longer counted; the command tells the
  next Device Mux it reaches. A Tunnel on the direct path is counted
  nowhere.
- When the koko server revokes its Device Session, it sends each open
  connection `revoked@koko` before closing it, so `Conn.Wait` returns
  `serverconn.ErrRevoked`; when `koko disconnect` stops it, it sends
  `disconnected@koko` ([`wire.RequestDisconnected`](../../internal/shared/wire/wire.go)),
  which closes `Conn.Disconnected` at once, and `Conn.Wait` then returns
  `serverconn.ErrDisconnected`, so what the command carried ends for good.
- **The one exchange every build answers alike**
  ([ADR 0034](../adr/0034-koko-disconnect-stops-a-device-mux-of-any-build-through-one-fixed-exchange.md)):
  a connection whose SSH user name is `disconnect`, which no home is, asks
  the Device Mux to stop. It answers in the banner,
  `{"state": "stopped", "commands": <n>}`, the commands it carried, past
  the authentication (one still connecting gets no state, and starts a new
  Device Mux), refuses the authentication, and stops
  ([`devicemux.Stop`](../../internal/client/devicemux/local.go) is the
  asking side). The rest of the local protocol may change with any build.

## Liveness and reconnecting

The timings are the reach spec's ("Liveness and timings"), constants in
[`timing`](../../internal/shared/timing/timing.go) that tests shrink
through the timing-constants hook.

- **Probing.** Its Device Connection notes when bytes last came from the
  koko server ([`liveness`](../../internal/shared/liveness/liveness.go)):
  after `timing.DeviceProbe` (5 s) without any it sends
  `keepalive@openssh.com` wanting a reply, which any SSH server answers, a
  refusal included, and after `timing.DeviceDead` (15 s) it declares the
  connection dead and closes it, which drops it with the error `no answer
  for <time>`. The koko server probes from its side too
  ([the Device Connection](device-connection.md#the-tcp-listener)).
- **Probing at once.** On a wake from sleep and on a change of the
  device's routes it probes at once; that probe has `DeviceDead -
  DeviceProbe` (10 s) to be answered, as a probe sent after `DeviceProbe`
  has. Tests inject both signals through `timing.Timings.Wake` and
  `RouteChange`. While it reconnects, either signal makes it try again at
  once.
  - **A wake** is told by the clocks, looking every `timing.WakeCheck`
    (1 s): `slept`, per OS in `platform_unix.go` and `platform_windows.go`.
    On Linux and macOS the monotonic clock stands still while the device
    sleeps, so a wake is the wall clock having moved more than `WakeCheck`
    further than the monotonic one between two looks. On Windows it does
    not: Go's monotonic clock there is the interrupt time, which the
    runtime reads raw from `KUSER_SHARED_DATA` (`_INTERRUPT_TIME` in
    `runtime/time_windows.h`, read by `nanotime1` in
    `sys_windows_amd64.s`), and that count runs on through sleep and
    hibernation, as only the unbiased interrupt time does not
    ([`QueryUnbiasedInterruptTime`](https://learn.microsoft.com/en-us/windows/win32/api/realtimeapiset/nf-realtimeapiset-queryunbiasedinterrupttime)).
    So on Windows a wake is the gap between two looks alone having been
    more than `WakeCheck` longer than `WakeCheck`, and the liveness probe
    still ends a dead Device Connection within `timing.DeviceDead`. The
    same clock times the Device Connection's silence there, so after a
    sleep longer than `DeviceDead` the liveness watch counts the sleep as
    silence and ends the Device Connection as the device wakes, rather
    than probing it first; the Device Mux then reconnects at once, and its
    Tunnels Resume.
  - **A route change** is told by the OS (`watchRoutes`): a routing socket
    on macOS (`route_darwin.go`), netlink on Linux (`route_linux.go`), and
    on Windows `NotifyRouteChange2` and `NotifyUnicastIpAddressChange`, for
    IPv4 and IPv6, both cancelled when the Device Mux stops
    (`route_windows.go`). Windows calls one callback, made once for the
    process, which wakes every route watch the process holds.
- **Reconnecting.** When its Device Connection drops, and when it starts, it
  tries to open one at once. While tries fail and something waits for a
  Device Connection (a command connecting, a channel opening), it tries
  again after `timing.ReconnectFirst` (250 ms), doubling the wait up to
  `timing.ReconnectMax` (5 s); it stops once a try succeeds, fails for a
  reason trying again does not change, or nothing waits any more, when its
  wait ends. Each try lasts at most `timing.DeviceConnectAttempt` (10 s),
  TCP, SSH handshake and report, so an address that drops packets holds up
  no later try, and reads the device's files afresh, so it takes an address
  `koko connect` saved. Every command and channel waiting shares the
  tries.
- **What waits, and how long.** A command connecting, and a channel it
  opens while the Device Mux holds no Device Connection, wait up to
  `timing.CommandWait` (15 s), then get the state, reconnecting with the
  last try's error; a try that fails for a reason trying again does not
  change answers them at once.

## The bare-name refresh

The Device Mux keeps the [bare names](device-connection.md#ssh) in koko's
ssh_config following the record
([`refreshBareNames`](../../internal/client/devicemux/barenames.go)):

- **When.** Once it holds its first Device Connection; at once whenever
  the koko server says on it that the Hosts its User sees may have changed
  ([the host-list notice](device-connection.md#the-host-list-notice)); and
  otherwise `timing.BareNameRefresh` (10 minutes) after each refresh, the
  fallback for a koko server that sends no notice, until it stops.
  Once its Device Connection has ended it refreshes again on the next,
  since a notice sent while it held none is lost; a refresh that fails
  tries again then, or 10 minutes later. A refresh due while it holds no
  Device Connection waits for the next one without asking for it, so it
  never makes the Device Mux reconnect.
- **How.** It calls `host.list` on its Device Connection, through one API
  client per Device Connection, whose `api@koko` channel stays open between
  refreshes and carries nothing then, and rewrites koko's ssh_config with
  the names only when the file would change
  ([`devicefiles.RefreshSSHConfig`](../../internal/client/devicefiles/ssh.go)).
  The ProxyCommand keeps the koko the file runs: which koko ssh runs is for
  a command run from the user's shell to choose, never for a process in
  the background, whose own path may be an old build's after an upgrade, or
  one found without the user's PATH. It writes holding the Device Mux's
  lock and not once it is stopping, so it never puts back the names its own
  revocation cleanup took out.
- **At once.** [`koko ssh reload`](device-connection.md#ssh) rewrites them
  from the user's shell without waiting for the next refresh.
- **No command.** A refresh is not a command connected to the Device Mux:
  it keeps the Device Mux from idling out no more than it makes it
  reconnect.

## State and exit

- **Connected**, since it opened its Device Connection, or **reconnecting**
  since it dropped (or since the first try failed), with the last try's
  error.
- **Idle exit.** It exits `timing.DeviceMuxIdle` (10 minutes) after the
  last command left: a command, `koko web` or `koko tunnel` connected keeps
  it running, with or without a channel open, so `koko web` between browser
  connections counts as open, and so does a Tunnel waiting to Resume,
  whose process stays connected; its own bare-name refresh does not. A
  command that connects as it idles out is refused, and starts a new one.
- **Revocation.** On the revocation disconnect
  ([`wire.RequestRevoked`](../../internal/shared/wire/wire.go)), or the
  Revoked banner when it reconnects, it runs the revocation cleanup
  ([`devicefiles.Revoke`](../../internal/client/devicefiles/devicefiles.go))
  holding its lock, so no command hears of the revocation before the
  cleanup is done. Once its channels have passed on what the koko server
  sent before it closed the Device Connection, such as the answer to the
  call that revoked this device, each within `timing.PassOn`, it tells
  each command and exits. A command it cut off finds the revocation
  record when it tries again. A device whose join is
  gone when it reconnects (`not_joined`) stops it too.
- **koko disconnect.** `koko disconnect` ([`disconnect`](../../internal/client/cli/connect.go))
  lists every socket in the runtime directory
  ([`devicemux.Sockets`](../../internal/client/devicemux/socket.go)), every
  build's and every home's, asks each to stop through the exchange above,
  and prints how many commands each carried, which it ended: SSH Sessions,
  `koko web` runs and calls in flight alike. A socket nothing answers at,
  such as one a crashed Device Mux left, is passed over; one that answers
  anything else fails the command once it has asked every other, naming
  that socket and saying what it stopped. It waits for each answer at
  most `timing.CommandWait` ([`devicemux.Stop`](../../internal/client/devicemux/local.go)),
  since on Windows a Unix socket once kept the answer, and the close
  after it, from `koko disconnect` for good (#184). The banner alone is
  the answer. Without one by then, a Device Mux that still listens did
  not answer, and fails the command as one that does not stop; one that
  no longer listens stopped, but how many commands it ended is not known
  (`devicemux.ErrUncounted`), so `koko disconnect` fails saying so, and
  names the ones it stopped. The next command starts a new
  Device Mux. `koko leave` stops them in the same way
  (`stopDeviceMuxes`) before it deletes the device's files
  ([`koko leave`](device-connection.md#on-the-device)); it needs each
  stopped, not counted, so it passes over one whose answer never came.
- On stopping it closes its listener, which removes the socket, then every
  connection, then releases the lock. When koko disconnect stopped it, it
  first tells each command (`disconnected@koko`), then waits, within
  `timing.PassOn` (5 s) in all, for the `tunnel@koko` channels it carries
  to end, which their commands close for good on being told
  ([the device end of a Tunnel](gateway.md#the-device-end-of-a-tunnel)).
  The other channels, `api@koko` and `web@koko`, end with it, so it waits
  for none of them, and with no SSH Session open it stops at once. While
  it waits it holds its lock, so a Device Mux a command starts meanwhile
  waits for it, within `timing.DeviceMuxStart`.

## Logging

One line per event on its stderr (the log beside its socket), at the level
`KOKO_LOG_LEVEL` names, info by default: `device mux started` (version,
revision, pid, socket), `device connection opened` (server, server
version), `cannot open a Device Connection` (each try) and `device
connection dropped` (server, error, such as `no answer for 15s`), the
revocation cleanup done or failed, `refreshed the bare names` (names, how
many, and `rewrote`, whether it wrote the file) or `cannot refresh the
bare names` (error), `refused a connection from another OS
user` (error, naming the peer's OS user), `closed a command's connection
that stopped answering` (on Windows: error, such as `no answer for 7s`),
`koko disconnect stops the device mux` (commands), and `device mux
stopped` with its reason; at debug level, `probing the Device Connection
at once` (reason: wake or route change) and `no route-change watch`.

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
- [`barenames_test.go`](../../internal/client/cli/barenames_test.go): a
  Host someone else records in a Member's Space, and a Space the Member is
  added to, reached as `<name>.koko` at once and as bare names after a
  refresh, and the timer's refreshes, with nothing changed, leaving the
  file alone; [`hostschanged_test.go`](../../internal/client/cli/hostschanged_test.go):
  the refresh on the koko server's notice ([the Device Connection](device-connection.md#verifying)),
  and on the next Device Connection after a drop, whose notices it hears; the refresh at its start, and one a drop cut tried again on the
  next Device Connection; the idle exit however often it refreshes; and no
  reconnect for a refresh. [`devicefiles/ssh_test.go`](../../internal/client/devicefiles/ssh_test.go)
  `TestARefreshKeepsTheKokoTheFileRuns`: the ProxyCommand a refresh keeps.
- [`reconnect_test.go`](../../internal/client/cli/reconnect_test.go) (S1,
  the timings shrunk, a [`harness.Forward`](../../internal/harness/forward.go)
  stalled or pointed elsewhere): a command waiting at most
  `timing.CommandWait` on the direct path and through the Device Mux, then
  `server_unreachable`; a connection that never reports given up on; a cut
  call's retry waiting for the koko server and applying once; the Device
  Mux keeping a quiet Device Connection alive by its probes and replacing a
  stalled one; the koko server's probe and dead close; one try at once
  after a drop, then backoff only while a command waits; a wake and a route
  change probing at once, and a wake cutting the wait between tries short.
  [`liveness_test.go`](../../internal/shared/liveness/liveness_test.go)
  tests the probe and dead rules at the module's own interface.
- [`disconnect_test.go`](../../internal/client/cli/disconnect_test.go):
  `koko disconnect` stopping the Device Mux with its count (golden), each
  command told, none running (golden), the next command starting a new
  one, the Device Mux of another build stopped beside it, a stale socket
  passed over, and one that answers but does not stop failing the command
  only after the others stopped; an answer that never comes holding
  `koko disconnect` at most `timing.CommandWait`, a banner whose refusal
  never comes counted, and `koko leave` passing over a Device Mux that
  stopped without its answer coming.

The peer-credential refusal of another OS user, and a runtime directory
another user owns, are not tested: both need a second OS user, which a
test run as one user cannot be, and a hook to fake one would be a sixth
test-only hook (code layout, rule 10). `sameUser` admitting a connection
of this user is tested, by its peer credentials on Linux and macOS. A
wake on Windows is not: no CI runner sleeps; the rule is read from Go's
source (above) and its arithmetic tested.
- [`tunnel_test.go`](../../internal/client/cli/tunnel_test.go): `koko
  tunnel`'s Tunnel riding the Device Mux's Device Connection, and an open SSH
  Session keeping the Device Mux from idling out.
- [`resume_test.go`](../../internal/client/cli/resume_test.go): a Tunnel
  Resuming through the Device Mux across a cut and a stall, and through a
  new one after it was killed (`d.KillMux()`); a Tunnel waiting to Resume
  keeping it from idling out and counted in `koko status`; and `koko
  disconnect` ending koko tunnel's and koko exec's SSH Sessions as the
  client's close, never Detached, without a Resume.
- [`status_test.go`](../../internal/client/cli/status_test.go): `koko
  status` connected (golden) and reconnecting, and `null` on the direct
  path.
- [`devicemux` package tests](../../internal/client/devicemux): the name,
  the macOS path limit, a path of 103 bytes listening on every OS, the
  runtime directory, the same-user check, a command handed the report of
  the next Device Connection, and what the koko server sent before a
  revocation reaching the command in full, and a command that does not
  read keeping the Device Mux no longer than `timing.PassOn`
  ([`revoke_test.go`](../../internal/client/devicemux/revoke_test.go));
  200 commands writing as the Device Mux stops each seeing their
  connection end, and 200 commands closing as the Device Mux writes to
  them each leaving it, so it idles out
  ([`end_test.go`](../../internal/client/devicemux/end_test.go)), which on
  Windows is where an end the socket missed shows; a socket a crashed
  Device Mux left replaced by the next one
  ([`socket_test.go`](../../internal/client/devicemux/socket_test.go)); and
  on Windows, the Device Mux letting go of a command that answers no
  probe, and a command ending its connection to a Device Mux that answers
  none ([`watch_windows_test.go`](../../internal/client/devicemux/watch_windows_test.go)),
  which no missed end, too rare to wait for, could test, and the route
  watch registering and cancelling twice in one process, and the wake rule
  ([`route_windows_test.go`](../../internal/client/devicemux/route_windows_test.go)).
- [`muxprocess`](../../internal/client/muxprocess/muxprocess_test.go), a
  package of its own: `koko join` run as a real process, the test binary
  standing in for koko, starts a real Device Mux process through
  `devicemux.Spawn`, which answers once `koko join` has exited and carries
  `koko status`; `koko disconnect` stops it.
- The `cli` and `devicemux` tests above run on Windows as they are, in the
  [`windows` job](release.md#the-windows-job); there the runtime-directory
  refusal is a list that lets `Everyone` in.
