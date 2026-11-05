# The CLI

The command tree, the parser, the runner and the output layer, in
[`internal/client/cli`](../../internal/client/cli). The grammar is
[ADR 0019](../adr/0019-the-command-path-carries-the-risk.md); the `--json`
contract is [ADR 0016](../adr/0016-the-cli-json-is-its-own-contract.md).

## The command tree

One list of every command, [`cli.Tree`](../../internal/client/cli/tree.go),
joined from each area's `<area>Commands`. An entry
([`cli.Command`](../../internal/client/cli/tree.go)) holds the noun and verb,
the positional arguments (each plain, qualified, or qualified and allowed
bare; each required, or optional after every required one, shown as
`[<device>]`), the flags (each with its `KOKO_*` variable, default and choices),
whether it takes `key=value` pairs and `--unset`, the risk tier (required),
Owner-only, the API operation it calls and its `Run`, made with `action(f)`
from a function returning the command's `--json` type, which is also its
shape. Every command takes `--json`, which is not listed.
[`tree_test.go`](../../internal/client/cli/tree_test.go) checks every entry: a
tier, a summary, a unique path, sound arguments and flags (neither `--json`
nor `--unset` listed, and `--unset` only beside pairs), and an operation that
`api.Operations` lists.

## The parser

[`parse`](../../internal/client/cli/parse.go) turns argv into an entry with its
arguments and flags, or refuses the command line with exit 2 and the code
`invalid_usage`, before any call:

- a flag before or inside the command path, naming the command as it should be
  written (`koko --json space ls` → `koko space ls --json`), including a flag
  after a shorter command that a later word extends (`koko skill --json
  install`);
- an unknown command, naming what the typed words could continue with;
- an unknown flag, a flag given twice, a value flag without a value or with an
  empty one, a value outside the flag's choices (from the flag or its
  variable), and `--json` or a boolean flag given a value;
- a missing, an empty or an extra argument, so `koko space add "$NAME"` with
  `NAME` unset never reaches the koko server; an optional argument may be
  left out, but given empty it is refused too;
- a qualified name (`<space>/<secret>`, `<host>/<service>`, `<user>/<device>`)
  with a part missing or empty, or a part that holds `/`; one that may be bare
  (`[<user>/]<device>`) is also taken without its `/`;
- for a command that takes `key=value` pairs: a pair with an empty key or an
  empty value (naming the key, so `region=$REGION` with `REGION` unset never
  clears it), a key given twice, `--unset` of one key twice, and a key both set
  and unset.

Flags come after the path, mixed with arguments in any order, as `--name value`
or `--name=value`; a value that starts with `--` needs the `=` form. A flag not
given takes its `KOKO_*` variable when that is set and not empty, else its
default.

A command with `Pairs` reads every word holding `=` as a pair, cut at its
first `=` (`note=a=b` sets `note` to `a=b`); no name holds `=`, so no argument
is mistaken for one. One with `Unset` also takes `--unset <key>`, repeatable.
`Input.Pairs` and `Input.Unset` return them in the order given; checking a key
against the field names and Attribute key rules is the command's. A qualified
argument keeps its text in `Input.Arg` and its parts in `Input.Qualified`.

## The secret-input reader

[`Input.ReadSecret`](../../internal/client/cli/secretinput.go) is the one way
a command reads a secret input: a Secret value, an Enroll password or a key's
passphrase, never an argument. The command declares its forms in a
`SecretInput` (a file flag, a stdin flag, stdin itself, and the forms it reads
itself, such as `--secret`, for the refusal to name), and the reader reads the
first one given, byte for byte. With none given it prompts on stderr
(`Enter the value of ops/db-password: `) and reads one line with echo off when
stdin is a terminal; without one it fails at once with exit 2, naming the
forms. It refuses:

- a stdin flag when stdin is a terminal, which would echo what is typed;
- an empty value from any form, so a pipe from a command that failed sends
  nothing; empty stdin, such as an agent's `/dev/null`, gives nothing;
- a file it cannot read, with exit 2.

It reads stdin only for a stdin form. Stdin itself as the form (`secret add`)
is read to its end whenever stdin is not a terminal, so a pipe that never
closes waits; every other secret input fails at once without a terminal.

**Stdin decides "on a terminal".** The prompt reads its answer there, and a
terminal anywhere else (stderr, or the controlling terminal of an agent's
harness) has nobody typing into it. Nothing else in the CLI prompts, and
nothing asks "are you sure?". The terminal sits behind
[`cli.Terminal`](../../internal/client/cli/terminal.go), whose per-OS code is
under [rule 13](code-layout.md#rules-for-working-in-parallel). Ctrl-C at the
prompt turns echo back on and ends the command.

## The runner and the output layer

[`cli.Runner.Run`](../../internal/client/cli/cli.go) takes argv, stdin,
stdout, stderr and the environment (HOME included) and returns the exit
status. A command reaches the koko server only on its first `in.Call`, so
`koko version` needs none.

- **Exit statuses:** 0 success; 1 the operation failed; 2 the command line was
  refused before any call. They never change meaning.
- **stdout** gets the result only on success, rendered in full first, so it
  stays empty on failure. A command that runs until it is stopped, such as
  `koko web`, prints its result once, as soon as it is ready, and nothing
  when it ends; a failure after that still goes to stderr with its status.
  **stderr** gets every error: `koko: <message>`, or with `--json` (anywhere
  on the line, even where refused) the error object
  `{"error": {"code": ..., "message": ...}}`.
- **Codes:** the API's pass through; the CLI adds `invalid_usage` (exit 2),
  `not_joined` (no koko server joined and no socket on this machine, or the
  joined koko server holds no Device Session with this device's key),
  `server_unreachable` (the socket or the joined koko server does not answer,
  or the connection could not carry the call, or ended under `koko web`; for
  the joined koko server the hint names `koko connect <host:port>`),
  `host_key_mismatch` (the koko server presented a host key other than the
  one pinned), `already_joined` (`koko join` on a joined device),
  `invalid_join_code` (the koko server refused the Join Code, used, expired
  or never issued alike), `unsupported_platform` (`koko server run` or
  `koko server install` outside Linux, or a machine install cannot use),
  `port_taken` (another process holds the port `koko web` or `koko server
  install` listens on), and `internal_error` for a failure it did not expect.
- **Which koko server:** the Unix socket when it is at its path, and
  otherwise the koko server this device joined, over a direct Device
  Connection ([the Device Connection](device-connection.md#which-koko-server-the-cli-reaches)).

## `--json`

- The shapes are the CLI's own Go types (`spaceJSON`), mapped by hand from the
  API's. A list prints a bare array of every item (`[]` when empty), and a
  write prints the record it made or changed.
- Indented by two spaces, one value, ending in a newline.
- Every record carries an `id`: its kind, `_` and 8 lowercase Crockford base32
  characters, such as `space_3f9k2m7q`. No name holds `_`.
- Timestamps, when a record has them, are RFC 3339 in UTC to the second, as
  `2026-11-05T14:03:07Z`.
- **The mapping test**
  ([`mapping_test.go`](../../internal/client/cli/mapping_test.go)) fails when a
  field of an API response a command reads is neither in the CLI's type under
  the same JSON name nor listed in the area's `mapping.omitted` with a reason.
- Golden files in [`testdata`](../../internal/client/cli/testdata) pin each
  command's output, one file per command and case.

## Output for people

Not a contract: a write says what it did (`Added Space ops (space_3f9k2m7q).`),
and a list is a table with a header (`NAME  ID`), or a sentence when empty. A
command that issues a Join Code says for whom and until when, then prints the
one `koko join koko_...` line to paste on the new device. `koko join` says
which koko server it joined, as whose device.

## The commands so far

| Command | Tier | Calls | `--json` |
| --- | --- | --- | --- |
| `koko space add <space>` (Owner; enforced from #39) | reversible | `space.add` | the Space: `{"id", "name"}` |
| `koko space ls` | read | `space.list` | an array of Spaces |
| `koko user invite <user>` (Owner; enforced from #39) | irreversible | `user.invite` | `{"user": {"id", "name", "role", "created_at"}, "join_code": ...}`, the Join Code as `device add` prints it |
| `koko device add [<device>] [--user <user>]` | irreversible | `device.add` | the Join Code: `{"id", "code", "user", "device", "expires_at"}` |
| `koko join <join-code>` or `koko join -` | irreversible | `user.current`, after joining in SSH authentication | `{"server_address", "device_session": {"id", "user", "device", "created_at"}}`; a Device Session's id is `device_...` |
| `koko version` | read | none | `{"version", "revision", "go", "platform"}` |
| `koko web [--port <port>] [--no-open]` | irreversible | `web.start` | `{"address"}`, once it serves ([`koko web`](web-front.md#koko-web)) |
| `koko server run [--data-dir <dir>] [--root-key-file <file>] [--listen <addr>] [--log-level <level>]` | irreversible | none | nothing on success |
| `koko server install [--owner <user>] [--address <host[:port]>] [--port <port>]` | irreversible | `server.init` | `{"address", "host_key_fingerprint", "root_key_file", "root_key_id", "listen_port", "join_code"}`, the Join Code as `device add` prints it ([the installer](installer.md#what-it-prints)) |
| `koko server show` | read | `server.show` | `{"address", "listen_port", "host_key_fingerprint", "version", "root_key_file", "root_key_id", "data_dir"}` ([the deployment afterwards](initialisation.md#the-deployment-afterwards)) |
| `koko server set address=<host[:port]>` (Owner, over the Unix socket only) | reversible | `server.set` | the settings as `server show` prints them, as the change left them |

`koko server run` takes `--data-dir` (`KOKO_DATA_DIR`, default
`/var/lib/koko`), `--root-key-file` (`KOKO_ROOT_KEY_FILE`, default
`$CREDENTIALS_DIRECTORY/root-key`, the file systemd's
`LoadCredential=root-key` hands the unit), `--listen` (`KOKO_LISTEN`, default
`:443`: an optional host and a port number, where devices reach the koko
server once it is initialised) and `--log-level` (`KOKO_LOG_LEVEL`:
`debug`, `info`, `warn` or `error`; default `info`). Left to its default
without `CREDENTIALS_DIRECTORY` set, the Root Key file is refused with exit 2;
left to it under systemd, the file is a copy of `/etc/koko/root.key`, the
file the unit's `LoadCredential=` names, which `koko server show` names as
the Root Key's file.
The flag takes the file's absolute path, never the key, and the koko server
refuses to start without a usable Root Key ([the Seal](seal.md#at-start-up)). The koko
server logs to stderr in slog's text format (`time=... level=INFO msg=...
key=value`), one line per event, for journald to keep.
