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
| shared | `internal/shared/` | API types and error codes (`api`), name rules (`name`), the Server Address's form (`serveraddr`), the wire constants, the version and operations report, the channel adapter and how a join carries its secret (`wire`), the build report (`build`), the P0 timing constants and their hook (`timing`), the resumable stream (`resumable`), the Join Code codec (`joincode`), the port-owner lookup (`portowner`), what `koko server install` and the installer agree on (`install`), the record's value rules: a Host's fields, Attribute keys and values, the Note (`record`) | server, client |
| server-only | `internal/server/` | the koko server process (`server`), store (`store`), operation framework (`op`), transport (`transport`), the access check (`access`), the Root Key and the Seal (`seal`), the koko server's own record and keys (`deployment`), one package per area (`space`, `user`, `device`, `host`, `web`: the web front), the embedded web UI (`webui`), the Host connector (`hostconn`), the installer (`installer`); later the Gateway | client |
| client-only | `internal/client/` | the command tree, parser, runner, output layer and the CLI's `--json` types (`cli`), the connection to the koko server (`serverconn`), the API client (`apiclient`), the device's files of its join (`devicefiles`); later the Device Mux, tunnel and ssh_config | server |

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
`secret`, `server`, `reach`, `web` and `skill`. Each adds its parts in files of
its own; a central list holds one line per area, which the area's first ticket
adds. A rebase conflict in a central list is a one-line addition: keep both
lines.

| Part | The area's own file | The central list |
| --- | --- | --- |
| API types, operations (`api.Op`) and error codes (`api.Code`) | `internal/shared/api/<area>.go`, with `<area>Operations` and, if it has codes of its own, `<area>Codes` | `operations` in [`api.go`](../../internal/shared/api/api.go), `codes` in [`errors.go`](../../internal/shared/api/errors.go) |
| The web UI's TypeScript types | `web/src/api/<area>.ts`, written by `make generate` from `<area>.go` ([web-ui.md](web-ui.md#generated-types)) | none |
| Server operations | `internal/server/<area>/`, with `Register(*op.API)`, or a `Register` that also takes what the process loaded (below) | `areas` in [`server.go`](../../internal/server/server.go), or one call in `server.Start` for a `Register` that takes more |
| Commands and their `--json` types | `internal/client/cli/<area>.go`, with `<area>Commands` | `commands` in [`tree.go`](../../internal/client/cli/tree.go) |
| API-to-CLI mappings | `<area>Mappings` in the same file | `mappings` in [`mapping.go`](../../internal/client/cli/mapping.go) |
| Migration steps | `internal/server/store/migrations/<what-it-adds>.sql` | `steps` in [`migrations.go`](../../internal/server/store/migrations.go) (rule 2) |
| S1 tests and golden files | `internal/client/cli/<area>_test.go`, `testdata/<command>[-<case>].json` | none |
| Access-matrix rows | `internal/server/access/matrix_<area>_test.go`, with `<area>Rows` | `areaRows` in [`matrix_test.go`](../../internal/server/access/matrix_test.go) (rule 6) |
| What a Space holds, for an area whose records live in a Space | a [`space.Holding`](../../internal/server/space/space.go) in the area's package | `holdings` in [`server.go`](../../internal/server/server.go) (rule 7) |

The `server` area's operations act on the koko server process itself (the
Root Key it loaded, its keys, its listeners), so the process package
registers them ([`init.go`](../../internal/server/init.go),
[`settings.go`](../../internal/server/settings.go)) rather than an area
package of its own. An area whose operations need what the process
loaded takes it in its `Register`, which [`server.Start`](../../internal/server/server.go)
calls after the `areas` list: `device` and `user` take the Join Code issuer,
which reads the host key with the Root Key ([Join Codes](join-codes.md#issuing)).
`space` takes `user.ByName`, since `user` imports `space` (every User gets a
Personal Space), and the `holdings` list.
The `web` area's front holds the runs of `koko web` in memory and reads the
koko server's clock, so `server.Start` makes it and it registers its own
operation (`web.Front.Register`).
The device area also answers the TCP listener's join step
([`device.Authenticator`](../../internal/server/device/join.go)), which
`server.Start` hands the transport ([the Device Connection](device-connection.md)).
`server.Start` also makes the access-change signal
([`access.Signal`](../../internal/server/access/signal.go)) and subscribes
the transport to it: an area whose operations withdraw access takes the
signal in its `Register`, as `device` does, and publishes on it; a later
consumer subscribes in `server.Start` (rule 12).

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
   error codes go in that pair. The completeness test (#46) spans every pair.
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
   the socket and over a direct Device Connection now, and through the Device
   Mux (#49) later, so commands written before it do not change.
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

A hook is a field of the product's own configuration that `cmd/koko` never
sets and no flag or environment variable exposes; its zero value means the
product's constant. Fields rather than package variables keep parallel tests
apart.

| Hook | Where | Made by |
| --- | --- | --- |
| The socket path | `server.Config.SocketPath`, `cli.Runner.SocketPath` | #27 |
| The fake terminal | `cli.Runner.Terminal` | #28 |
| The P0 timing constants | `hostconn.Connector.Timings` and `installer.Installer.Timings`, each a [`timing.Timings`](../../internal/shared/timing/timing.go), where a ticket that uses a new timing adds its constant and field | #32 |
| The clock | `server.Config.Clock`, which every operation reads once per call through `op.Now`; the harness's `h.Now()` and `h.Advance(d)` | #36 |

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
Local caller under a temporary HOME, and `h.Call(op, req)` calls an operation
no command calls, such as `server.init`. `h.Calls()` counts the API calls the
server answered since `Start` returned, so a refusal can be shown to make
none. The options `harness.NoInit()`, `harness.DataDir(dir)` and
`harness.RootKeyFile(path)` start it uninitialised or on given files,
`harness.Listen(addr)` listens on TCP at a known address, such as a
`harness.FreeAddr(t)`, and `h.Stop()` stops it before the test ends, to
start another on its data directory. The koko server's clock stands still at the second `Start` began
until `h.Advance(d)` moves it, so a test moves time, such as past a Join
Code's 15 minutes, and never waits; `h.Now()` reads it.
`h.Background(env, args...)` runs a command that runs until it is stopped,
such as `koko web`, with `WaitStdout`, `Stop` (as Ctrl-C) and `Wait`.
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
args...)` run command lines on it, `d.Home()` is its HOME, and `d.Dial()`
opens a Device Connection from its files and holds it until the test ends,
as a long-lived connection would.
`h.Join(user)` issues a Join Code for the User over the socket and returns a
new device joined with it. `h.Socket()` is the socket's path, for a runner on
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
answer arrives.

## The in-process Host

[`sshhost.Start`](../../internal/harness/sshhost/sshhost.go) starts an
`x/crypto/ssh` server on a loopback port that stands in for a Host or a Relay:
one Host Account (`sshhost.Account`, `deploy`) with a home directory and an
`authorized_keys` file read afresh at each login, password login over
`password` or `keyboard-interactive`, its own host keys, and `direct-tcpip`
forwarding, as a Relay's sshd does. `ResetFirst`, `StallFirst`, `SlowLogins`
and `DropConns` break it as a network path or a slow PAM stack would.
`Logins()` and `Requests()` record what it received; `Conns()` and `Open()`
count its connections. It refuses every session request for now; the ticket
that needs exec or a subsystem adds it, by addition like the harness.
`sshhost.NewKey` makes ed25519, ECDSA and RSA keys for tests.
