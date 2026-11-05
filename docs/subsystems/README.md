# Subsystem references

Each page describes one subsystem as it is now: responsibilities and
boundaries, source of truth, runtime paths, state transitions, constraints,
failure behaviour, and how to verify it. Why a design was chosen lives in an
ADR; a proposed change lives in its issue or PR.

| Subsystem | Reference |
| --- | --- |
| Code layout: the package groups, where an area adds its parts, and the rules for working in parallel | [code-layout.md](code-layout.md) |
| The API: the Unix socket, channels, calls, error codes and the operation framework | [api.md](api.md) |
| The CLI: the command tree, parser, runner, output layer and `--json` | [cli.md](cli.md) |
| The store: SQLite, transactions, migrations and record ids | [store.md](store.md) |

## Maintaining a reference

Update the owning reference in the same change when that change alters a
documented responsibility, interface, state transition, runtime path,
compatibility rule or failure behaviour. A refactor that keeps these
contracts needs no prose change.

Keep each fact in one reference and link to it from elsewhere. Link source
files and named symbols rather than copying implementation detail that
changes often.
