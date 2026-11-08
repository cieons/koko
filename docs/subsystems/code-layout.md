# Code layout and working in parallel

How koko's Go packages are grouped, where each area adds its parts, and the
rules that keep agents working side by side off each other's files. Why the
server and the CLI are kept apart is in
[ADR 0024](../adr/0024-the-server-and-the-cli-are-separable.md).

## The package groups

Every package under `internal/` sits in one of three groups, chosen by its
directory. [`internal/groups_test.go`](../../internal/groups_test.go) fails,
for Linux, macOS and Windows alike, when a package is in no group or depends
(directly or not) on a group it may not use.

| Group | Directory | Holds | May not depend on |
| --- | --- | --- | --- |
| shared | `internal/shared/` | API types and error codes (`api`), name rules (`name`), the Server Address's form (`serveraddr`), the wire constants, the version and operations report, the channel adapter, how a join carries its secret and what a Tunnel's two ends agree on (`wire`), the build report (`build`), the P0 timing constants and their hook (`timing`), the liveness watch of an SSH connection, which both ends of a Device Connection run (`liveness`), the SSH connection every channel koko opens goes through, whose opens fail once it has ended (`sshconn`), the resumable stream (`resumable`), the Join Code codec (`joincode`), the port-owner lookup (`portowner`), what `koko server install` and the installer agree on (`install`), the record's value rules: a Host's fields, a Service's ports, Attribute keys, the Preset Attributes and the four types of values, the Note (`record`) | server, client |
| server-only | `internal/server/` | the koko server process (`server`), store (`store`), operation framework (`op`), transport (`transport`), the access check (`access`), the Root Key and the Seal (`seal`), the koko server's own record and keys (`deployment`), one package per area (`space`, `user`, `device`, `host`, `service`, `attr`, `secret`, `web`: the web front), the filtering and sorting of lists (`query`), the embedded web UI (`webui`), the Host connector (`hostconn`), Enroll (`enroll`), the installer (`installer`), the Gateway (`gateway`) | client |
| client-only | `internal/client/` | the command tree, parser, runner, output layer and the CLI's `--json` types (`cli`), the connection to the koko server (`serverconn`), the API client (`apiclient`), the device's files of its join, koko's ssh_config and known_hosts, and the Include line (`devicefiles`), the Device Mux and the client of its local protocol (`devicemux`), the device end of a Tunnel (`tunnel`) | server |

A package that holds only tests is in no group, since only non-test
dependencies count: so far the real-OpenSSH test's
[`internal/client/openssh`](#the-real-openssh-test).

Only [`cmd/koko`](../../cmd/koko) and the S1 harness
([`internal/harness`](../../internal/harness)) import both the server and the
client groups, and no product code imports the harness. Test files may import
the harness from any package; only non-test dependencies count.

`koko server run` dispatches into server code through
[`cli.Runner.ServerRun`](../../internal/client/cli/cli.go), which `cmd/koko`
sets in Linux builds only ([`server_linux.go`](../../cmd/koko/server_linux.go)).
`koko server install` and `koko server uninstall` dispatch the same way,
through `cli.Runner.ServerInstall` and `cli.Runner.ServerUninstall`, whose
requests, reports and refusals are the shared
[`install`](../../internal/shared/install/install.go) types. Every other
command is a client command that reaches the koko server over the socket.

## Where an area adds its parts

An area is one of `space`, `user`, `device`, `host`, `service`, `attr`,
`secret`, `server`, `reach`, `enroll`, `web` and `skill`. `enroll` is the
part of reach that is `koko host enroll`, kept apart so the reach tickets that
run beside it never share its files. Each adds its parts in files of
its own; a central list holds one line per area, which the area's first ticket
adds. A rebase conflict in a central list is a one-line addition: keep both
lines.

| Part | The area's own file | The central list |
| --- | --- | --- |
| API types, operations (`api.Op`) and error codes (`api.Code`) | `internal/shared/api/<area>.go`, with `<area>Operations` and, if it has codes of its own, `<area>Codes` | `operations` in [`api.go`](../../internal/shared/api/api.go), `codes` in [`errors.go`](../../internal/shared/api/errors.go) |
| The web UI's messages, the messages for the area's error codes among them (rule 5) | `web/src/i18n/en/<area>.ts` and `web/src/i18n/zh-CN/<area>.ts` | the imports and spreads in [`catalogues.ts`](../../web/src/i18n/catalogues.ts) |
| The web UI's pages | `web/src/pages/<area>/`, with `<area>Routes` in its `routes.ts` | `routes` in [`web/src/routes.ts`](../../web/src/routes.ts) |
| The web UI's TypeScript types | `web/src/api/<area>.ts`, written by `make generate` from `<area>.go` ([web-ui.md](web-ui.md#generated-types)) | none |
| Server operations | `internal/server/<area>/`, with `Register(*op.API)`, or a `Register` that also takes what the process loaded (below) | `areas` in [`server.go`](../../internal/server/server.go), or one call in `server.Start` for a `Register` that takes more |
| Commands and their `--json` types | `internal/client/cli/<area>.go`, with `<area>Commands` | `commands` in [`tree.go`](../../internal/client/cli/tree.go) |
| API-to-CLI mappings | `<area>Mappings` in the same file | `mappings` in [`mapping.go`](../../internal/client/cli/mapping.go) |
| Migration steps | `internal/server/store/migrations/<what-it-adds>.sql` | `steps` in [`migrations.go`](../../internal/server/store/migrations.go) (rule 2) |
| S1 tests and golden files | `internal/client/cli/<area>_test.go`, `testdata/<command>[-<case>].json` | `realTime` in [`realtime_test.go`](../../internal/client/cli/realtime_test.go), one line for each test whose outcome hangs on real time ([running the S1 tests](#running-the-s1-tests)) |
| Access-matrix rows | `internal/server/access/matrix_<area>_test.go`, with `<area>Rows` | `areaRows` in [`matrix_test.go`](../../internal/server/access/matrix_test.go) (rule 6) |
| What a Space holds, for an area whose records live in a Space | a [`space.Holding`](../../internal/server/space/space.go) in the area's package | `holdings` in [`server.go`](../../internal/server/server.go) (rule 7) |
| The Attribute keys in use, for an area whose records hold Attributes | an [`attr.Keys`](../../internal/server/attr/attr.go) in the area's package | `attrKeys` in [`server.go`](../../internal/server/server.go) |

The `server` area's operations act on the koko server process itself (the
Root Key it loaded, its keys, its listeners), so the process package
registers them ([`init.go`](../../internal/server/init.go),
[`settings.go`](../../internal/server/settings.go)) rather than an area
package of its own. An area whose operations need what the process
loaded takes it in its `Register`, which [`server.Start`](../../internal/server/server.go)
calls after the `areas` list: `device` and `user` take the Join Code issuer,
which reads the host key with the Root Key ([Join Codes](join-codes.md#issuing));
`user` also takes the `holdings` list, to refuse deleting a User whose
Personal Space holds a record ([Users](users.md#removing)).
`space` takes `user.ByName`, since `user` imports `space` (every User gets a
Personal Space), and the `holdings` list. `host` takes `service.OfHost`,
since `service` imports `host` (a Service lives in its Host's Space), so
that `host show` lists a Host's Services. Both also take the access-change
signal (below). `secret` takes the Root Key, which
seals and opens every value ([Secrets](secrets.md#sealing)). `enroll` takes
the Root Key, which opens the koko key, and the timing-constants hook
([Enroll](enroll.md)).
The `web` area's front holds the runs of `koko web` in memory and reads the
koko server's clock, so `server.Start` makes it and it registers its own
operation (`web.Front.Register`).
The device area also answers the TCP listener's join step
([`device.Authenticator`](../../internal/server/device/join.go)), which
`server.Start` hands the transport ([the Device Connection](device-connection.md)).
The Gateway is no area: it serves no operation but takes the transport's
`tunnel@koko` channels, so `server.Start` makes it with the store, the Root
Key and the timing-constants hook, hands it the transport
(`transport.Server.HandleTunnels`), and closes it before the transport, so
each SSH Session gets the restart notice before its Device Connection
closes, waiting at most `timing.StopNotice` for a connection too stalled to
take it ([the Gateway](gateway.md#when-an-ssh-session-ends)).
`server.Start` also makes the access-change signal
([`access.Signal`](../../internal/server/access/signal.go)) and subscribes
the koko server's `withdraw` to it, which hands each change to the Gateway
and then to the transport ([revoking](device-connection.md#revoking)): an
area whose operations withdraw access takes the signal in its `Register`, as
`device`, `user`, `space` and `host` do, and publishes on it; a later
consumer subscribes in `server.Start` (rule 12), as the web front's
`Withdraw` does.

Error codes every area shares (`invalid_request`, `unknown_field`,
`invalid_name`, `name_taken`, ...) are in `commonCodes`; an area adds a code of
its own only when an agent would act on it differently.

## Rules for working in parallel

1. **Separable server and CLI.** Put every new package in its group, as above,
   and keep `internal/groups_test.go` green.
2. **Schema migrations.** One ordered, append-only list of forward steps. A
   ticket adds its own step file and one line at the end of `steps`, and never
   edits a step another ticket landed. A rebase conflict in the list is
   resolved by keeping both lines in landing order; no database outside tests
   exists before P0 ships. Name a step file after what it adds, not by number:
   its position in the list is its schema version.
3. **Operations, commands, API types and error codes** come from each area's
   own files, joined by one line per area in the central lists above.
4. **Generated TypeScript types.** One generated file per API area. On a rebase
   conflict, re-run the generator (`make generate`); never hand-merge.
5. **Message catalogues.** One English and one Simplified Chinese file per
   area, so each page ticket adds its own pair; the messages for an area's API
   error codes go in that pair. The completeness test spans every pair
   ([the web UI shell](web-shell.md#language)).
6. **The access matrix.** Each area keeps its rows in a test file of its own;
   the matrix test fails for any registered operation without a row
   ([the access matrix](access.md#the-access-matrix)).
7. **"What a Space holds".** `space show` counts and the `space rm` refusal
   ask each area through one small interface, a `space.Holding`, so Hosts
   (#47) and Secrets (#48) each add their own file and one line to `holdings`
   ([what a Space holds](access.md#what-a-space-holds)).
8. **Transactions.** The operation framework ([`op`](../../internal/server/op/op.go))
   opens and commits each call's transaction; an operation gets a `store.Tx`,
   which cannot commit. The `Idempotency-Key` store
   ([`op/idempotency.go`](../../internal/server/op/idempotency.go)) joins
   every write there, without touching any operation.
9. **The CLI's channel interface.** All CLI traffic opens its channels through
   [`serverconn.Conn`](../../internal/client/serverconn/serverconn.go): over
   the socket, over a direct Device Connection and through the Device Mux
   ([`devicemux.Dial`](../../internal/client/devicemux/local.go)), so a
   command does not know which it has.
10. **Test helpers.** The S1 harness and the in-process Host (#32) grow by
    addition: a ticket adds helpers and `harness.Option`s, and never changes an
    existing helper's signature. The four test-only hooks are made by their
    first user and reused by everyone; no ticket adds a fifth (below).
11. **Golden files.** One file per command and case. `koko skill` is tested by
    properties (every command under its tier, every registered error code, the
    Presets), not by a whole-output golden, so adding a command never touches
    #57's tests.
12. **Code that several tickets grow on purpose** is serialised by edges:
    initialisation (#34 makes it, #36 adds the first Join Code, #37 adds TCP),
    `koko status` (#45, then #49, then #70), the access-change signal (#40 makes
    it; #50, #68 and #72 add sources; #62 and #68 consume it), the revocation
    cleanup on the device (#40, then #45, then #49), and the Host operations
    (#47, then #51 and #52, then #53, then #61).
13. **Platform-specific code** sits behind a small per-OS interface selected
    by build tags (`_linux.go`, `_darwin.go`, `_windows.go` or a `//go:build`
    line), never inline: the Device Mux socket and its same-user check, the
    route-change watch, the port-owner lookup, opening a browser, file
    permissions, the terminal (`golang.org/x/term` already splits it per OS,
    Windows included though untested until P1, so koko adds no files of its
    own), and signal forwarding. A ticket that adds one writes the Linux and
    macOS implementations the specs ask for, plus a Windows file that
    compiles and returns a "not supported on Windows" error. `make cross`
    compiles `windows/amd64` (compile only; a Windows CLI is P1). The server group runs
    on Linux only (and on macOS in tests), so it needs no per-OS files;
    `cmd/koko`'s `server_linux.go` and `server_other.go` are the pattern.

## Test-only hooks

A hook is a field of the product's own configuration that no flag or
environment variable exposes, and that `cmd/koko` never sets but from a
value linked in at build time, which only a test's own build links in: so
far the socket path, for [the real-OpenSSH test](#the-real-openssh-test).
Its zero value means the product's constant. Fields rather than package
variables keep parallel tests apart.

| Hook | Where | Made by |
| --- | --- | --- |
| The socket path | `server.Config.SocketPath`, `cli.Runner.SocketPath` (the harness's `harness.SocketPath(path)`); `cmd/koko` sets the latter from `main.socketPath`, which only [the real-OpenSSH test](#the-real-openssh-test)'s build sets, with `-ldflags -X`, and every other build leaves empty | #27 |
| The fake terminal | `cli.Runner.Terminal` | #28 |
| The P0 timing constants | `hostconn.Connector.Timings`, `installer.Installer.Timings`, `server.Config.Timings` (the harness's `harness.Timings(timings)`), `cli.Runner.Timings` and `devicemux.Config.Timings` (the harness's `harness.NewMuxDevice(t, timings)` and `d.UseTimings(timings)`), each a [`timing.Timings`](../../internal/shared/timing/timing.go), where a ticket that uses a new timing adds its constant and field; it also carries the wake and route-change signals the Device Mux probes on (`Timings.Wake`, `Timings.RouteChange`) | #32 |
| The clock | `server.Config.Clock`, which every operation reads once per call through `op.Now`, and `cli.Runner.Clock`, which a relative date counts from; the harness's `h.Now()`, `h.Advance(d)` and `h.SetNow(t)` | #36, the CLI's side #51 |

## The S1 harness

[`harness.Start`](../../internal/harness/harness.go) starts a real koko server
in the test process (a temporary data directory, a Root Key file made for the
test by `harness.NewRootKeyFile`, a temporary socket in a short path for
macOS's 104-byte limit, a loopback TCP port of the system's choosing,
logging at debug level) and initialises it over the socket, with the Owner
`harness.Owner` (`alice`) and the Server Address `harness.Address`
(`127.0.0.1`, without a port, so its Join Codes name the port it listens
on); so `koko space ls` lists `alice`'s Personal Space, and initialisation's
Join Code holds `alice`'s `device-1`.
`h.Koko(args...)` runs a command line in process through `cli.Runner` as the
Local caller under a temporary HOME (`h.Home()`), with empty stdin, on the
koko server's clock; `h.KokoEnv(env, args...)` adds variables such as `TZ`
to its environment, `h.KokoStdin(stdin, args...)` gives it stdin, and `h.KokoTerminal(term,
args...)` puts the fake terminal `term`, a `harness.NewTerminal(lines...)`
where a person types one line at each prompt, on its stdin through the
fake-terminal hook. `h.Call(op, req)` calls an operation
no command calls, such as `server.init`. `h.Calls()` counts the API calls the
server answered since `Start` returned, so a refusal can be shown to make
none. The options `harness.NoInit()`, `harness.DataDir(dir)` and
`harness.RootKeyFile(path)` start it uninitialised or on given files,
`harness.SocketPath(path)` serves its Unix socket at a given short path,
such as the one a koko binary built for a test reaches,
`harness.Listen(addr)` listens on TCP at a known address, such as a
`harness.FreeAddr(t)`, `harness.Timings(timings)` shrinks the P0 timings it uses,
such as the wait between attempts to connect to a Host, and `h.Stop()` stops it before the test ends, to
start another on its data directory. The koko server's clock stands still at the second `Start` began
until `h.Advance(d)` moves it, so a test moves time, such as past a Join
Code's 15 minutes, and never waits; `h.SetNow(t)` sets it to a given
instant, such as a leap day, and `h.Now()` reads it.
`h.Background(env, args...)` runs a command that runs until it is stopped,
such as `koko web`, with `WaitStdout`, `Stop` (as Ctrl-C) and `Wait`.
`h.Proxy(args...)` and `d.Proxy(env, args...)` run one as ssh runs its
ProxyCommand, such as `koko tunnel`: `p.Conn()` joins its stdout and stdin
into one connection for an SSH client, `p.Signal(sig)` sends it a signal
through `cli.Runner.Signals`, `p.Interrupt()` ends its context as Ctrl-C
does, `p.Done()` is closed once it exits, and `p.Wait()` returns its stderr
and exit status; when the test ends
it is stopped as ssh stops it, its pipes closed and a SIGHUP sent.
[`harness.Golden`](../../internal/harness/golden.go)
compares output with `testdata/<name>` after replacing each record id with a
numbered placeholder (`"space_<1>"`), each Join Code with `koko_<code>`,
each SHA-256 fingerprint with `SHA256:<fingerprint>` and each time with
`<time>`; `go test -update` rewrites the files.
[`harness.ServerStart`](../../internal/harness/harness.go) is a
`cli.Runner.ServerRun` for S1 tests of `koko server run`: the real start-up on
a temporary socket and, whatever `--listen` says, a loopback port, stopped at
once if it starts.
[`harness.NewMachine`](../../internal/harness/machine.go) is the fake machine
of `koko server install`'s system seam ([the installer](installer.md#verifying)).

Devices: `harness.NewDevice(t)` is a device that has not joined, with a HOME
of its own and nothing at its socket path, so it reaches a koko server only
over a Device Connection; `d.Koko(args...)` and `d.KokoStdin(stdin,
args...)` run command lines on it, `d.Background(env, args...)` one that
runs until it is stopped, such as a `koko exec` holding its SSH Session
open, `d.Home()` is its HOME, and `d.Dial()`
opens a Device Connection from its files and holds it until the test ends,
as a long-lived connection would, probing nothing itself.
`d.UseTimings(timings)` gives its commands a timing-constants hook, such
as a short wait for a koko server that does not answer. Its runner starts
no Device Mux, so its commands take the direct path.
[`harness.NewMuxDevice(t, timings)`](../../internal/harness/devicemux.go)
is such a device whose commands go through a Device Mux once it joins: each
one it starts runs in the test process as `koko device-mux`, with a
temporary runtime directory (`TMPDIR` and `XDG_RUNTIME_DIR`, `d.RuntimeDir()`)
and `timings` as its timing hook, logs to `d.MuxLog()` and stops when the
test ends; `d.KillMux()` stops those running as a crash would, as their
commands and the koko server see it, their connections closed with
nothing sent first. `d.KokoEnv(env, args...)` runs a command line with more
variables, such as `KOKO_DIRECT=1`, and `d.DialMux()` connects to the
Device Mux as a command does and holds the connection.
`harness.NewMuxDeviceBeside(t, d)` is another home of `d`'s OS user: a
HOME of its own, sharing `d`'s runtime directory.
`h.Join(user)` issues a Join Code for the User over the socket and returns a
new device joined with it, on the koko server's clock. `h.Socket()` is the socket's path, for a runner on
the koko server's own machine. `h.Send(op, body)` and `d.Send(op, body)`
call an operation with a raw JSON body, as the Local caller or as the
device's Device Session, and return the status and the body as the koko
server wrote them, for the access matrix and other byte-for-byte checks.
[`harness.Forward(t, target)`](../../internal/harness/forward.go) is a
loopback TCP forwarder whose target `f.To(addr)` changes and whose
`f.Conns()` counts its connections: initialised with its address as the
Server Address, a koko server issues codes that lead first to an impostor and
then to itself, and a refusal can be shown to make no connection.
`f.CutWhen(cut)` is its fault injection: the first bytes the koko server
sends once `cut` returns true are dropped with their connection, so a test
cuts a call after the koko server has logged its answer and before the
answer arrives. The cut falls on any connection, one that is ending
included, so a test that cuts one among several first waits until
`f.Carried()`, the connections the forwarder carries now, has let the
others go. `f.Stall()` makes the connections it carries pass nothing more,
either way, without closing them, as a network path that went dead does,
while connections it accepts after pass as before; `f.Partition()` also
passes on no close, so each end finds its connection dead by itself.

### Running the S1 tests

`go test` runs one package's tests one at a time unless they say otherwise,
and bounds each package's test binary at 5 minutes (`make test`'s
`-timeout`). `internal/client/cli` holds most of the S1 tests, so they run in
parallel. Each test starts its own koko server, HOME, socket, clock and
timing hook. The only thing tests share is the loopback port space: a port
picked as 127.0.0.1:0 is held while in use, and `harness.FreeAddr` hands
out ports below the range the OS picks from, never the same one twice in a
process. Every test is one of three kinds:

- **A test whose outcome does not hang on real time** calls `t.Parallel()`
  as its first line. Go runs these together, as many at once as there are
  CPUs, once the serial tests are done. Most tests are this kind. That
  includes a test that waits for an event within a bound, such as
  `waitLog`, `eventually` or `WaitStdout` within 10 s. It also includes a
  test that shortens a wait that never ends in what it waits for, such as
  the pause between tries at a port nothing listens on, or a command's wait
  for a koko server that was stopped. And it includes a test that holds a
  delay far inside the wait for it, such as the fake machine's 300 ms
  `StartDelay` under install's 30 s wait.
- **A test whose outcome hangs on real time** goes in `realTime` in
  [`realtime_test.go`](../../internal/client/cli/realtime_test.go). Such a
  test does one of three things. It shortens a wait that does end, such as
  for a probe that is answered, a reconnect, or a command waiting for a koko
  server that answers. It counts on what happens within a set time, such as
  a `time.Sleep` before checking that nothing happened, a login kept slow
  with `sshhost.SlowLogins`, or a `Freeze`. Or it measures how long
  something took (`time.Since`).

  Its name starts with a lower-case letter instead of `Test`. It does not
  call `t.Parallel()`. It is added to `realTime` in alphabetical order: one
  line, kept on a rebase conflict as in a central list. `TestRealTime` runs
  each as a parallel subtest named after it with a capital first letter,
  such as `TestRealTime/TheGatewayProbesAQuietHost`, which is the name to
  give `go test -run`. `TestRealTime` itself is serial, so these tests run
  together before the parallel batch and never share the CPU with it. They
  mostly wait, so running them together loads them little; their margins
  still allow for a loaded machine ([LESSONS.md](../../LESSONS.md)).
- **A test that calls `t.Chdir` or `t.Setenv`** stays serial, neither
  calling `t.Parallel()` nor going in `realTime`, since Go refuses both in
  a parallel test. Its whole time adds to the package's.

`make lint` catches a test that is none of the three. The `paralleltest`
linter finds a `Test` function in `internal/client/cli` that neither calls
`t.Parallel()` nor is one Go refuses to run in parallel. The `unused` linter
finds a real-time test left out of `realTime`.

## The real-OpenSSH test

[`internal/client/openssh`](../../internal/client/openssh/openssh_test.go)
runs the system's OpenSSH through koko end to end (P0 spec: reach, "Real
OpenSSH"). It is a package of its own, holding only tests, so that its
build of koko and the processes it runs count against no other package's
5-minute limit; it builds only on Unix (`//go:build unix`), since it runs
each process in a session of its own, with no controlling terminal, so
that anything that would prompt fails rather than hangs.

- **The tools.** `ssh`, `scp`, `sftp` and `rsync` from `PATH`, OpenSSH 9.0
  or later (whose scp speaks SFTP and takes `-O`), and `sftp-server` from
  where Debian, Fedora, Arch and macOS put it. A missing one fails the
  test, never skips it; CI's Ubuntu job installs them
  (`.github/workflows/check.yml`), and macOS ships them.
- **The koko binary.** It builds `cmd/koko` once, with `-race` exactly
  when the test runs with it, so the build takes the packages `go test`
  compiled from the cache, and with `-ldflags -X main.socketPath=<path>`,
  a short temporary path, so the binary never reaches a koko server
  running on the machine. Every koko command and every ProxyCommand runs
  that binary: the join writes it into koko's ssh_config.
- **Each OS user** is a temporary HOME, a runtime directory under `/tmp`
  for its Device Mux (macOS's `TMPDIR` is too long a path for the Device
  Mux's socket), a key of its own and a stand-in ssh-agent that counts
  connections. Every OpenSSH command gets `-F` pointed at koko's
  ssh_config under that HOME, since OpenSSH finds `~` from the password
  database, not from `HOME`, and `-o IdentityFile=<the user's key>`, which
  also keeps ssh from reading the default keys in the real `~/.ssh`. The
  user's Device Mux is stopped with `koko disconnect` when the test ends,
  and a command the test ends kills its whole process group, ssh's
  ProxyCommand included.
- **The order.** The joined device's part runs first, its subtests in
  parallel; then the koko server's machine's part starts a koko server
  with `harness.SocketPath` at the binary's socket path, which from then
  on every command of that binary reaches.

## The in-process Host

[`sshhost.Start`](../../internal/harness/sshhost/sshhost.go) starts an
`x/crypto/ssh` server on a loopback port that stands in for a Host or a Relay:
one Host Account (`sshhost.Account`, `deploy`) with a home directory and an
`authorized_keys` file read afresh at each login, password login over
`password` or `keyboard-interactive`, its own host keys, and `direct-tcpip`
forwarding, as a Relay's sshd does. `ResetFirst`, `StallFirst`, `SlowLogins`,
`ResetNext(n)` (once it is Enrolled, say), `DropConns` and `Freeze` (until
`Thaw`, reading and writing nothing on any connection) break it as a
network path or a slow PAM stack would. `ClientAlive(interval, countMax)`
probes a quiet client with `keepalive@openssh.com` and ends the connection
when it leaves them unanswered, as sshd's `ClientAliveInterval` does.
`Logins()` and `Requests()` record what it received; `Conns()` and `Open()`
count its connections. It runs a session's command as sshd does: an exec
request through `/bin/sh -c`, a shell request as `/bin/sh` reading its
stdin, and a subsystem that a `Subsystem(name, command)` option names
through `/bin/sh -c`; each a local process in the Host Account's home
directory (`Home()`) with `HOME` set to it, the variables of the session's
`env` requests and `TERM` from its `pty-req` (it allocates no terminal), its
stdio on the channel. It sends the exit status, or `exit-signal` when a
signal ended the command, sends the command a `signal` request's signal,
takes a `window-change`, and refuses every other session request. It
forwards `-L` to a Unix socket (`direct-streamlocal@openssh.com`), and takes
`-R` from a loopback TCP port or a Unix socket (`tcpip-forward`,
`streamlocal-forward@openssh.com` and their cancels), carrying each
connection back in a `forwarded-tcpip` or
`forwarded-streamlocal@openssh.com` channel; it refuses every other global
request. `AuthorizedKeys()` reads the Host Account's `authorized_keys`
as it is. `sshhost.NewKey` makes ed25519, ECDSA and RSA keys for tests.
