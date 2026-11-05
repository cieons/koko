# Secrets

A Secret is a named, opaque value of up to 64 KiB that the koko server holds
in one Space, sealed under the Root Key. The spec is P0 spec: Secrets (#25);
the source is [`internal/server/secret`](../../internal/server/secret/secret.go)
on the koko server, [`internal/client/cli/secret.go`](../../internal/client/cli/secret.go)
in the CLI, and the API types in
[`internal/shared/api/secret.go`](../../internal/shared/api/secret.go).

Credentials go only into Secrets. Every other part of the record, Notes and
Attributes included, is stored plain in the database, so a copy of the data
directory shows it.

## The record

- **Fields:** an id (`secret_` and 8 characters, as every
  [record id](store.md#record-ids)), its Space, its name, its value, and
  `created_at` and `updated_at`. No description, Note or Attributes.
- **Name:** 1 to 63 of `a-z`, `0-9`, `-` and `_`, starting with a letter or a
  digit ([`name.CheckSecret`](../../internal/shared/name/name.go)); the koko
  server refuses any other with `invalid_name`. A name is unique within its
  Space only, so a name never tells what a Space the caller cannot see holds.
- **Qualified name:** a command always names a Secret `<space>/<secret>`,
  the Personal Space included; the CLI refuses one without its Space with
  exit 2 before any call ([the parser](cli.md#the-parser)). The API takes
  the two parts as `space` and `name`.
- **Value:** 1 byte to `api.SecretMaxSize` (64 KiB), any bytes, which koko
  never decodes. It is stored exactly as read, a trailing newline from stdin
  included.
- **Storage:** the `secrets` table, one row per Secret, `UNIQUE (space_id,
  name)`, its value in `sealed`: the Seal's sealed form for the row's id
  ([the Seal](seal.md#the-sealed-form)), never the value plain. A Space that
  holds a Secret cannot be removed, which the foreign key and `space rm`'s
  refusal both hold.

## Operations and commands

| Command | Tier | Operation | `--json` |
| --- | --- | --- | --- |
| `koko secret ls [<space>]` | read | `secret.list` | a bare array of Secrets, `{"id", "space", "name", "created_at", "updated_at"}`, sorted by Space, then name: of one Space, or of every Space the caller can act in |
| `koko secret show <space>/<secret>` | irreversible | `secret.show` | the Secret with `"value_base64"`; without `--json`, the value's bytes on stdout and nothing else |
| `koko secret add <space>/<secret> [--from-file <file>]` | reversible | `secret.add` | the Secret it stored, without its value |

- **Only `secret show` prints a value.** It sits in the tier that prints a
  credential (ADR 0019), so one harness rule asks before any value is
  shown. `secret.show` is the one response, and `secret.add` the one
  request, that holds a value; no flag of any other command adds one, and
  `koko --json secret show` is refused like any flag before the path.
  [`TestOnlySecretShowAnswersWithAValue`](../../internal/client/cli/secret_tier_test.go)
  fails when another response or command shape can hold an `api.SecretValue`.
- **A value in JSON** is standard base64 of its bytes, under `value_base64`
  ([ADR 0028](../adr/0028-a-secret-value-travels-in-json-as-base64.md)).
- **No upsert.** `secret add` under a name the Space holds fails with
  `name_taken` and leaves the value as it was.
- **Errors** name the Secret as `<space>/<secret>` and never hold a value.
  `api.SecretValue` prints as its length through `fmt`, and logs as its
  length through slog's text handler, the koko server's, so an error or a
  log line that formats one by mistake leaks nothing; a JSON encoder writes
  it as base64, as the API must.

| Code | Status | When |
| --- | --- | --- |
| `unknown_secret` | 404 | The Space, which the caller can act in, holds no Secret of that name. |
| `value_too_large` | 413 | A value over 64 KiB reached the koko server; the CLI refuses one first. |
| `secret_unreadable` | 500 | The sealed value does not open under the Root Key for its own record: the database was edited or damaged. The error names the Secret. |

An empty value is refused with `invalid_request`. A name the Space holds is
`name_taken`, a name that breaks the rule `invalid_name`, and a Space the
caller cannot act in `unknown_space`, as for every operation that names a
Space.

## Reading a value in

`secret add` reads the value through the
[secret-input reader](cli.md#the-secret-input-reader): `--from-file`, else
stdin when it is not a terminal, else a prompt with echo off on a terminal,
asked once. Without a terminal and with nothing on stdin it exits 2 at once,
naming the forms. A value is never an argument: a word past
`<space>/<secret>` is refused with exit 2 without being repeated, since it
may be the value ([the parser](cli.md#the-parser)). The reader refuses an
empty value and one over 64 KiB with exit 2, before any call, and reads no
file or stdin past 64 KiB and one byte; the koko server checks both again
for any other client.

## Access

Every operation asks the access check ([access.md](access.md)) on the
Secret's Space, through `space.ByName`, inside its transaction: a Member
acts on the Secrets of their Personal Space and of the Spaces they belong to,
the Owner on every Space's, and the Local caller as the Owner. A Space the
caller cannot act in answers as one that does not exist, and `secret.list`
without a Space leaves out the Secrets of every Space outside the caller's
scope. Membership is read on every call, so a Member taken out of a Space
loses its Secrets on their next call.

`space show` counts a Space's Secrets, and `space rm` refuses a Space that
holds one, through [`secret.Holding`](../../internal/server/secret/secret.go)
([what a Space holds](access.md#what-a-space-holds)).

## Sealing

`secret.add` seals the value with `RootKey.Seal` for the new Secret's id,
and `secret.show` opens it with `RootKey.Open` for the same id
([the Seal](seal.md)). The secret area gets the Root Key the koko server
loaded through its `Register`, which `server.Start` calls. A sealed value
copied onto another Secret's row, or with any byte changed, does not open:
`secret.show` fails with `secret_unreadable`, naming the Secret, and prints
no value. The id never changes, so a rename or a move (#56) never reseals.

## Verifying

- [`internal/client/cli/secret_test.go`](../../internal/client/cli/secret_test.go)
  (S1): each command with its goldens; values of any bytes up to exactly
  64 KiB from stdin and a file, and one byte more refused; the prompt on the
  fake terminal and the refusal without one; the name rule and a name without
  its Space; `koko --json secret show`; access over Device Connections and a
  membership change; `space show` and `space rm`; and the marker test, which
  finds a unique value only on `secret show`'s stdout, never on any stderr,
  in the koko server's log or in a file under a temporary HOME, the value
  written as an argument included.
- [`internal/server/secret_copy_test.go`](../../internal/server/secret_copy_test.go):
  the copy test, which reads every file of the data directory while the koko
  server runs and once it is stopped, with a snapshot written as an upgrade
  writes one, and finds no value, private key or Root Key.
- [`internal/server/secret/secret_test.go`](../../internal/server/secret/secret_test.go):
  the binding test, which edits the `secrets` table of a stopped koko server;
  the koko server's own value checks; unknown request fields.
- [`internal/server/access/matrix_secret_test.go`](../../internal/server/access/matrix_secret_test.go):
  every Secret operation in the access matrix.
