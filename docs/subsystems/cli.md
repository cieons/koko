# The CLI

The command tree, the parser, the runner and the output layer, in
[`internal/client/cli`](../../internal/client/cli). The grammar is
[ADR 0019](../adr/0019-the-command-path-carries-the-risk.md); the `--json`
contract is [ADR 0016](../adr/0016-the-cli-json-is-its-own-contract.md).

## The command tree

One list of every command, [`cli.Tree`](../../internal/client/cli/tree.go),
joined from each area's `<area>Commands`. An entry
([`cli.Command`](../../internal/client/cli/tree.go)) holds the noun and verb,
the positional arguments, the flags (each with its `KOKO_*` variable, default
and choices), the risk tier (required), Owner-only, the API operation it calls
and its `Run`, made with `action(f)` from a function returning the command's
`--json` type, which is also its shape. Every command takes `--json`, which is
not listed. [`tree_test.go`](../../internal/client/cli/tree_test.go) checks
every entry: a tier, a summary, a unique path, sound flags, and an operation
that `api.Operations` lists.

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
  `NAME` unset never reaches the koko server.

Flags come after the path, mixed with arguments in any order, as `--name value`
or `--name=value`; a value that starts with `--` needs the `=` form. A flag not
given takes its `KOKO_*` variable when that is set and not empty, else its
default.

## The runner and the output layer

[`cli.Runner.Run`](../../internal/client/cli/cli.go) takes argv, stdin,
stdout, stderr and the environment (HOME included) and returns the exit
status. A command reaches the koko server only on its first `in.Call`, so
`koko version` needs none.

- **Exit statuses:** 0 success; 1 the operation failed; 2 the command line was
  refused before any call. They never change meaning.
- **stdout** gets the result only on success, rendered in full first, so it
  stays empty on failure. **stderr** gets every error: `koko: <message>`, or
  with `--json` (anywhere on the line, even where refused) the error object
  `{"error": {"code": ..., "message": ...}}`.
- **Codes:** the API's pass through; the CLI adds `invalid_usage` (exit 2),
  `not_joined` (no koko server joined and no socket on this machine),
  `server_unreachable` (the socket does not answer, or the connection could
  not carry the call), `unsupported_platform`
  (`koko server run` outside Linux), and `internal_error` for a failure it did
  not expect.

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
and a list is a table with a header (`NAME  ID`), or a sentence when empty.

## The commands so far

| Command | Tier | Calls | `--json` |
| --- | --- | --- | --- |
| `koko space add <space>` (Owner; enforced from #39) | reversible | `space.add` | the Space: `{"id", "name"}` |
| `koko space ls` | read | `space.list` | an array of Spaces |
| `koko version` | read | none | `{"version", "revision", "go", "platform"}` |
| `koko server run [--data-dir <dir>] [--log-level <level>]` | irreversible | none | nothing on success |

`koko server run` takes `--data-dir` (`KOKO_DATA_DIR`, default
`/var/lib/koko`) and `--log-level` (`KOKO_LOG_LEVEL`: `debug`, `info`, `warn`
or `error`; default `info`). The koko server logs to stderr in slog's text
format (`time=... level=INFO msg=... key=value`), one line per event, for
journald to keep.
