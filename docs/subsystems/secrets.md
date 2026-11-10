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
action the same operation; what it looks like is [the web UI's
design](../web-design.md#secrets-secrets). It loads as a chunk of its own
the first time it is visited (`lazyPage`), and each of its dialogs is one
of the shell's [dialogs](web-shell.md#parts).

- **The list** is `secret.list` without a Space: the Secrets in every
  Space the caller can act in, as `koko secret ls` prints them, one panel
  per Space under its name, "Personal Space" where it is one, and its
  count. A Member sees only their Spaces' Secrets, since the koko server
  answers no others. A row shows the Secret's name and when it changed
  ("Changed 3 days ago"), then Copy, Show and its menu.
- **The search and the Space select.** The search narrows the rows to those
  whose `<space>/<name>` holds what was typed, whatever its case; the Space
  select, a searchable choice with "All Spaces" first, shown when the
  caller has two Spaces or more, narrows them to one Space. Both stay in
  the URL's query (`?q=`, `?space=`), refined in place rather than as a new
  visit, so a bookmark opens the page narrowed and a Host's page can link
  to it narrowed to its Space. Nothing matching says so, with a button that
  clears both; no Secret at all says what a Secret is for, with Add Secret.
- **A value.** No value is fetched until a row's Copy or Show is clicked,
  each of which calls `secret.show` and never asks first. Copy copies the
  value and puts nothing on the page. Show opens the value in its row, in
  an inset block in the code face, its lines and spaces kept, with a Copy of
  its own, and becomes Hide. The value leaves the page after Hide, after 30
  seconds, when its Secret is written to (from its row, or as a reload
  finds it changed), when the page is left within the web UI and when the
  browser leaves it (`pagehide`); a value `secret.show` answers after any
  of those never shows. If the focus was in the value when it left, it goes
  back to Show. A value lives only in the row's state ([shown
  once](web-shell.md#shown-once)), never in the URL or the browser's
  storage, and the web front sends every API answer with `Cache-Control:
  no-store` ([the web front](web-front.md#the-web-front)), so the browser's
  cache holds none. A refused Copy or Show says why in its row.
- **Copying.** Copy on a row hands the clipboard a `ClipboardItem` as it is
  clicked, which the value fills once `secret.show` answers, so a browser
  that allows a copy only straight after a click, as Safari does, still
  copies it ([Parts](web-shell.md#parts), `CopyButton`). A browser that
  does not allow copying says, on a row, to show the value and select it,
  and under a value shown, to select it.
- **A value that is not UTF-8** shows as the standard base64 it came in, as
  `koko secret show --json` prints it, marked so; both Copy buttons then
  copy the base64. A value of UTF-8 bytes shows as text, exactly, a leading
  byte-order mark and a trailing newline included.
- **The writes.** Add Secret opens a dialog that takes a name, a Space the
  caller can act in (the Space the page is narrowed to, else their Personal
  Space, unless they choose another, in a searchable choice) and a value
  typed into a field that keeps several lines, such as a private key; the
  value goes as the base64 of its UTF-8 bytes. A row's menu ("More actions
  for" the Secret) offers Set a new value, Rename, Move and Delete: set
  takes a new value, rename a new name, starting from the current one, and
  move another Space the caller can act in, in a searchable choice, each in
  a dialog of its own; Move is disabled when the caller has no other
  Space. Each form checks its fields when it is submitted. None of these
  asks first, and an add or set retried after a dropped connection carries
  the same `Idempotency-Key` ([calls](web-shell.md#calls)). Delete asks
  first, in a dialog saying that the Secret and its value are deleted
  permanently. A refusal shows its code's message in the dialog that made
  the call, the form kept as typed; a write that succeeded says so in a
  toast, and the list loads again.
- **On a Host's page.** A row is
  [`SecretRow`](../../web/src/pages/secret/SecretRow.tsx), which holds its
  value, its Copy, Show and menu and their dialogs, so the Secrets of a
  Host's Space show there with the same behaviour (#124).

## Verifying

- [`secrets.test.tsx`](../../web/src/pages/secret/secrets.test.tsx), Vitest
  and React Testing Library, with axe on the page and on every dialog and
  menu it opens: a Member's panels from `secret.list` without a Space, with
  each Secret's name and when it changed, an empty list saying so, and a
  list that does not load, with Try again; the search by Space or name and
  the Space select, kept in the URL in place, from a bookmark too, and
  nothing matching saying so; no `secret.show` until a click; Copy calling
  `secret.show` and copying without putting the value on the page; Show
  opening the value in its row, never asking, its own Copy, and Hide; the
  value gone after 30 seconds on a faked clock, with the focus back on
  Show; a refused Show and Copy; a value answered too late, after the
  browser left the page, a set from its row or a navigation, never shown;
  the value gone once its Secret is written to, from its row or as a reload
  finds it; the value gone once the page is left within the web UI or the
  browser leaves it; a value that is not UTF-8 shown and copied as base64
  by both Copy buttons; a browser that cannot copy, from a row and from a
  value shown; Add Secret and Set a new value never asking, sending the
  typed value's UTF-8 as base64, and resending the same `Idempotency-Key`
  after a drop; Add Secret taking the Space the page is narrowed to, and
  checking its fields before it sends; no value in the URL or the browser's
  storage; Rename and Move never asking, Move offering the caller's other
  Spaces; a refusal shown in its dialog; Delete asking first and deleting
  only once confirmed.
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
