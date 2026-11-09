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
| `koko secret set <space>/<secret> [--from-file <file>]` | reversible | `secret.set` | the Secret it changed, without its value |
| `koko secret rename <space>/<secret> <new-name>` | reversible | `secret.rename` | the Secret under its new name |
| `koko secret move <space>/<secret> <space>` | reversible | `secret.move` | the Secret in its new Space |
| `koko secret rm <space>/<secret>` | irreversible | `secret.remove` | the Secret it deleted, as it was, without its value |

- **Only `secret show` prints a value.** It sits in the tier that prints a
  credential (ADR 0019), so one harness rule asks before any value is
  shown. `secret.show` is the one response, and `secret.add` and
  `secret.set` the only requests, that hold a value; no flag of any other
  command adds one, and `koko --json secret show` is refused like any flag
  before the path.
  [`TestOnlySecretShowAnswersWithAValue`](../../internal/client/cli/secret_tier_test.go)
  fails when another response or command shape can hold an `api.SecretValue`.
- **A value in JSON** is standard base64 of its bytes, under `value_base64`
  ([ADR 0028](../adr/0028-a-secret-value-travels-in-json-as-base64.md)).
- **No upsert.** `secret add` under a name the Space holds fails with
  `name_taken` and leaves the value as it was; `secret set` on a name the
  Space does not hold fails with `unknown_secret` and adds nothing.
- **The last writer wins.** `secret set` replaces the value with no
  revision check, and P0 keeps no value history, so the old value is gone
  (it sits in the reversible tier by the verb rule of ADR 0019 all the same).
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

## Renaming, moving and deleting

- **Rename** gives a Secret a new name in its Space; **move** takes it to
  another Space under its name. Each keeps its id, its `created_at` and
  its sealed value, and moves `updated_at` to the call. Each refuses a name
  the Space it lands in holds with `name_taken`, a rename to its own name
  included, and never overwrites a Secret. A move to the Space it is in
  changes nothing.
- **A move needs both Spaces.** It looks up the source, then the target,
  through `space.ByName`, so a caller who cannot act in either is answered
  as for a Space that does not exist, naming that Space; the Owner, and
  the Local caller, may move between any two.
- **Delete** (`secret rm`, `secret.remove`) removes the Secret's row, its
  sealed value with it. A Secret has no Retired state and P0 keeps no value
  history, so nothing brings it back. Its `--json` is the Secret as it was,
  in `secret ls`'s shape, without its value, as every `rm` prints the record
  it removed ([`--json`](cli.md#--json)).

## Reading a value in

`secret add` and `secret set` read the value through the
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

A Removed User's former Personal Space is an ordinary shared Space with no
Members ([Users](users.md#removing)), so its Secrets stay there, sealed,
and only the Owner sees them and moves them out or deletes them.

`space show` counts a Space's Secrets, and `space rm` refuses a Space that
holds one, through [`secret.Holding`](../../internal/server/secret/secret.go)
([what a Space holds](access.md#what-a-space-holds)).

## Enroll's read

`koko host enroll <host> --secret <space>/<secret>` logs in to a Host with
the password or private key a Secret holds; how Enroll tells them apart is
[Enroll's](enroll.md#reading-a-secret). Only the name crosses the API, in
`host.enroll`'s `secret`; the koko server reads the value through
[`secret.Open`](../../internal/server/secret/open.go), which is
`secret.show` without the API: the same access check on the Secret's Space,
for the same caller, so the Secret's Space is checked apart from the Host's;
the same refusals, `unknown_space` for a Space the caller cannot act in among
them; and the value opened for its own record. No operation makes this
read, so the value never reaches the device, and `secret.show` stays the one
response that holds one. Enroll uses the value once, changes nothing in the
Secret, and stores nothing it read. Its tests are Enroll's
([Enroll](enroll.md#verifying)).

## Sealing

`secret.add` seals the value with `RootKey.Seal` for the new Secret's id,
`secret.set` seals the new value afresh, with a fresh nonce, for the same
id, and `secret.show` opens it with `RootKey.Open` for that id
([the Seal](seal.md)). The secret area gets the Root Key the koko server
loaded through its `Register`, which `server.Start` calls. A sealed value
copied onto another Secret's row, or with any byte changed, does not open:
`secret.show` fails with `secret_unreadable`, naming the Secret, and prints
no value. The id never changes, so a rename or a move never reseals.

A Secret write's response holds no value, so the response the koko server
keeps for its [`Idempotency-Key`](api.md#idempotency-key) holds none
either: a `secret.add` retried with its key adds one Secret and answers the
kept response, and the same key with another value is refused with
`idempotency_key_reused`.

## The web UI's Secrets page

[`SecretsPage`](../../web/src/pages/secret/SecretsPage.tsx), at `/secrets`,
in the [web UI shell](web-shell.md), offers what the commands do, each
action the same operation. It follows Astryx's `table-page` template (spec
#108): a header with the title, the number of Secrets and Add Secret, a
search field, and the table, which on a phone scrolls sideways in its own
box with the Space and name pinned. It loads as a chunk of its own the
first time it is visited (`lazyPage`), and each of its dialogs is one of
the shell's [dialogs](web-shell.md#dialogs), a bottom sheet on a phone.

- **The list** is `secret.list` without a Space: the names of the Secrets
  in every Space the caller can act in, by Space, then name, as `koko
  secret ls` prints them. A Member sees only their Spaces' Secrets, since the
  koko server answers no others. The search narrows the rows to those whose
  `<space>/<name>` holds what was typed, whatever its case; it stays in the
  URL's query (`?q=`), refined in place rather than as a new visit, so a
  bookmark opens the list narrowed. Nothing matching says so, with a button
  that clears the search.
- **Revealing.** No value is fetched until a row's Show is clicked, which
  calls `secret.show` and never asks first. The value shows in a dialog, in
  the shell's `CopyLine` (Astryx's `Code`, its lines and spaces kept, and a
  button that copies it, as a [line shown once](web-shell.md#shown-once)
  has), and is forgotten when the dialog
  closes (Hide, its close button or Escape), after 30 seconds, when the
  Secret is written to, or when the page is left. A value `secret.show`
  answers after a later reveal, a close, a write to its Secret or the
  browser leaving the page never shows. It lives only in the page's state
  ([shown once](web-shell.md#shown-once)), never in the URL or the
  browser's storage, and the web front sends every API answer with
  `Cache-Control: no-store` ([the web front](web-front.md#the-web-front)), so
  the browser's cache holds none. A refused reveal says why in the dialog.
- **A value that is not UTF-8** shows as the standard base64 it came in, as
  `koko secret show --json` prints it, marked so; Copy then copies the
  base64. A value of UTF-8 bytes shows as text, exactly, a leading byte-order
  mark and a trailing newline included.
- **The writes.** Add Secret opens a dialog that takes a Space the caller
  can act in (their Personal Space unless they choose another), a name and
  a value typed into a field that keeps several lines, such as a private
  key; the value goes as the base64 of its UTF-8 bytes. A row's overflow
  menu ("Actions for" the Secret) offers Set value, Rename, Move and Delete:
  set takes a new value, rename a new name, and move another Space, each in
  a dialog of its own. None of these asks first, and an add or set retried
  after a dropped connection carries the same `Idempotency-Key`
  ([calls](web-shell.md#calls)). Delete asks first, in a dialog saying that
  the Secret and its value are deleted permanently. A refusal shows its
  code's message in the dialog that made the call.

## Verifying

- [`secrets.test.tsx`](../../web/src/pages/secret/secrets.test.tsx), Vitest
  and React Testing Library, with axe on the page and on every dialog it
  opens: a Member's list from `secret.list` without a Space, and an empty
  list saying so; the search by Space or name, kept in the URL, from a
  bookmark too, and nothing matching saying so; no `secret.show` until a
  click; a click revealing the value in a dialog, never asking, and the
  value gone after 30 seconds on a faked clock; Copy, Hide and the close
  button; a refused reveal; a value answered too late, after a later
  reveal, a close, the browser leaving the page or a set, never shown; the
  value gone once the page is left within the web UI or the browser leaves
  it; a value that is not UTF-8 shown and copied as base64; a browser that
  cannot copy; add and set never asking, sending the typed value's UTF-8 as
  base64, and resending the same `Idempotency-Key` after a drop; no value
  in the URL or the browser's storage; rename and move never asking, move
  offering the caller's other Spaces; a refusal shown in its dialog; delete
  asking first and deleting only once confirmed.
- [`secret_web_test.go`](../../internal/client/cli/secret_web_test.go) (S1,
  against `koko web`): `secret.show` over `web@koko`, answered or refused,
  carries `Cache-Control: no-store`.
- [`internal/client/cli/secret_test.go`](../../internal/client/cli/secret_test.go)
  and [`secret_change_test.go`](../../internal/client/cli/secret_change_test.go)
  (S1): each command with its goldens; values of any bytes up to exactly
  64 KiB from stdin and a file, and one byte more refused; the prompt on the
  fake terminal and the refusal without one; the name rule and a name without
  its Space; `koko --json secret show`; access over Device Connections and a
  membership change; `set` on a missing Secret and the last writer winning;
  `rename` and `move` onto a taken name, and a move with access to the
  source only, the target only and both; `rm`; the Secrets of a Removed
  User's former Personal Space in the Owner's hands; `space show` and
  `space rm`; and the marker test, which
  finds a unique value only on `secret show`'s stdout, never on any stderr,
  in the koko server's log or in a file under a temporary HOME, the value
  written as an argument included.
- [`internal/server/secret_copy_test.go`](../../internal/server/secret_copy_test.go):
  the copy test, which reads every file of the data directory while the koko
  server runs and once it is stopped, with a snapshot written as an upgrade
  writes one, and finds no value, private key or Root Key.
- [`internal/server/secret/secret_test.go`](../../internal/server/secret/secret_test.go):
  the binding test, which edits the `secrets` table of a stopped koko server;
  `TestOnlySecretSetReseals`, which reads that table to find a rename and a
  move leaving the sealed value as it was and `secret set` sealing it
  afresh; the koko server's own value checks, on `secret.add` and
  `secret.set`; unknown request fields; and, beneath HTTP, a `secret.add`
  retried with its `Idempotency-Key`.
- [`internal/server/access/matrix_secret_test.go`](../../internal/server/access/matrix_secret_test.go):
  every Secret operation in the access matrix.
