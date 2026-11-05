# The record: Hosts

The Hosts koko records, each in one Space, with their Attributes and Notes,
and the filters and sorting of their lists.
The rules are [P0 spec: the record](https://github.com/cieons/koko/issues/22),
[ADR 0014](../adr/0014-host-names-are-unique-across-the-koko-server.md) and
[ADR 0029](../adr/0029-a-number-attribute-is-a-decimal-a-javascript-number-holds.md);
the operations are [`internal/server/host`](../../internal/server/host/host.go)
and [`internal/server/attr`](../../internal/server/attr/attr.go), the
filtering and sorting [`internal/server/query`](../../internal/server/query/query.go),
the value rules [`internal/shared/record`](../../internal/shared/record/record.go),
and the commands [`internal/client/cli/host.go`](../../internal/client/cli/host.go),
[`attr.go`](../../internal/client/cli/attr.go) and
[`filter.go`](../../internal/client/cli/filter.go).

## A Host

Stored in `hosts`, with its Attributes in `host_attributes`
([`hosts.sql`](../../internal/server/store/migrations/hosts.sql)).

| Field | Rule |
| --- | --- |
| `id` | `host_` and 8 characters, set by koko. A Host name never holds `_`, so an id stands wherever a name does. |
| `name` | The SSH Name: a lowercase DNS label ([`name.Check`](../../internal/shared/name/name.go)), unique among the Hosts that are not Retired, whatever Space each is in. The partial unique index `hosts_live_name` backs the check, which runs inside the write's transaction. |
| `space` | The name of its one Space. `host add` puts it in the caller's Personal Space ([`space.Personal`](../../internal/server/space/space.go)) unless `space=` names another; only `host move` changes it. |
| `address` | An IP address without a zone, kept in its canonical form (`2001:db8::1`), or a DNS name, kept in lowercase: labels of letters, digits and `-`, 253 characters at most, the last not all digits. Not unique. |
| `port` | 1 to 65535; 22 when not given. Not unique. |
| `account` | The Host Account: 1 to 32 of `a-z`, `A-Z`, `0-9`, `.`, `_` and `-`, not starting with `-`. No default. |
| `state` | `registered`, `managed` or `retired`, set only by koko. Every Host is Registered until Enroll makes it Managed ([Enroll](enroll.md)), and retiring sets the third. A change of address or port keeps a Host Managed, since its pin proves the Host wherever it answers; a change of Host Account sends it back to Registered, keeping its pin, since the koko key went only to the old Host Account. |
| `host_key` | The pinned host key's SHA-256 fingerprint, `SHA256:...`, or `null` for a Host with no pin. Stored as the key in SSH wire format, which Enroll writes. |
| `attributes` | Values by key, each of its key's type ([Attributes](#attributes-and-the-preset-attributes)). A key is snake_case (`a-z`, `0-9`, `_`, starting with a letter, 1 to 63 characters) and not a field's name: `id`, `name`, `space`, `address`, `port`, `account`, `relay`, `state`, `host_key`, `host`, `ports`, `attributes`, `note`, `created_at` or `updated_at`. |
| `note` | 1 byte to 64 KiB of UTF-8 on any number of lines, with no control character but tab, line feed and carriage return; `null` for none. |
| `created_at`, `updated_at` | RFC 3339 in UTC to the second; every write moves `updated_at`. |

The CLI checks each value rule before any call and refuses a bad one with
exit 2 and `invalid_usage`; the koko server checks them again on every
write, whichever client sent it, and refuses with `invalid_value`, or
`invalid_name` for a name ([the codes](api.md#calls)). An empty value and an
empty Note are refused, so clearing is always `--unset`.

## Attributes and the Preset Attributes

Every Attribute key that is not a Preset's takes free text. The Presets
([`record.Presets`](../../internal/shared/record/preset.go), [ADR
0010](../adr/0010-provider-details-are-attributes.md)) have fixed types, the
same on a Host and a Service:

| Key | Type |
| --- | --- |
| `provider`, `provider_account`, `region` | text |
| `price` | number, at least 0 |
| `currency` | enum: the 178 alphabetic codes of ISO 4217's List One as SIX published it on 2026-09-17, sorted |
| `billing_cycle` | enum: `monthly`, `quarterly`, `semiannual`, `yearly`, `biennial`, `triennial` |
| `expires_at` | date: the last day already paid for |
| `auto_renew` | enum: `no`, `yes` |

The four types ([`record.Type`](../../internal/shared/record/value.go)):

| Type | A value | Compared | In JSON |
| --- | --- | --- | --- |
| text | 1 byte to 1 KiB of UTF-8 on one line, no control character | in natural order: a run of ASCII digits against a run of digits by its value, anything else byte by byte, so `8` sorts before `16` and `EU` before `eu` | a string |
| number | an exact decimal: plain decimal, at most 15 digits and 6 after the point, stored with no leading zeros, no trailing zeros after the point and no `-0`, so `9.90` is `9.9` ([`record.ParseDecimal`](../../internal/shared/record/decimal.go), [ADR 0029](../adr/0029-a-number-attribute-is-a-decimal-a-javascript-number-holds.md)) | by value | a number, written exactly as stored |
| date | `YYYY-MM-DD`, from 0001-01-01 to 9999-12-31, no time | by the calendar | a string |
| enum | one of its values | in their declared order | a string |

- A Preset's value that does not parse as its type is refused, never kept as
  text. A value must also come in its type's JSON type: `"price": "10"` and
  `"region": 5` are refused with `invalid_value`, and a value that is neither
  a string nor a number with `invalid_request`.
- The API carries each value as a [`record.Value`](../../internal/shared/record/value.go),
  which keeps a number's text as JSON wrote it and never decodes it into a
  float; the web UI's type for it is `string | number`.
- `host_attributes.value` holds each value's text in the one form it is
  stored in; the key's type is the Preset table's, not stored. A stored value
  that does not parse for its key's type fails the read as an error, since
  every write checks it.

## Filtering and sorting

`host.list` takes `filters`, terms a Host must match, every one, and `sort`,
keys it orders by in turn ([`api.Filter`, `api.SortKey`](../../internal/shared/api/attr.go)).
A term is `{"key", "op", "value"}` with `op` one of `eq`, `ne`, `lt`, `le`,
`gt`, `ge`, `has` and `lacks`; `has` and `lacks` take no value, and the
others a value of the key's type in its JSON type. A sort key is `{"key",
"desc"}`.

- **Keys.** The fields of [`api.HostListFields`](../../internal/shared/api/host.go)
  with their types: `id`, `name`, `space`, `address`, `account` and
  `host_key` as text, `port` as a number, and `state` as an enum (`registered`,
  `managed`, `retired`). Any other key is an Attribute key, of its Preset's
  type or text. The Note, `created_at` and `updated_at` are not keys to
  filter or sort on: a timestamp is no date, and comparing one with a date
  would need the caller's time zone, which the API never sees.
- **A Host that lacks the key** (an Attribute it does not have, or
  `host_key` without a pin) matches `ne` and `lacks` and no other term, and
  sorts after every Host that has it, in either direction.
- **Order.** By the sort keys in turn; Hosts that tie stay by name, in
  natural order as `--sort name` gives it (`web2` before `web10`), and then
  id, the order without `sort`.
- **Refusals.** A malformed term, a key the list does not take, and a key
  sorted by twice are refused with `invalid_request`; a value not of its key's
  type with `invalid_value`. The API compares only absolute dates, so
  `"+30d"` is refused too.
- [`query.New`](../../internal/server/query/query.go) checks the terms and
  sort keys and `Query.Apply` applies them to the Hosts the caller can see,
  through each Host's values (`hostValues`); Services will take the same
  query with fields of their own.

### Relative dates

The CLI resolves `today`, `+30d`, `-2w`, `+3m` and `+1y` (a sign, at most 6
digits and a unit) into absolute dates before any call, in filters and in
writes alike ([`resolveDate`](../../internal/client/cli/reldate.go)). A month
or a year that lands past the end of its month clamps to the month's last
day: `+1m` from 2026-01-31 is 2026-02-28, and `+1y` from 2028-02-29 is
2029-02-28. Today is the clock's date in the time zone `TZ` names in the
run's environment, as Go reads `TZ` for a process: unset is the machine's
zone, empty or `UTC` is UTC, and any other value is an IANA name or a
zoneinfo file's path, after an optional `:`. A `TZ` that names no zone koko
can load is refused with exit 2 where a relative date needs it, rather than
read as UTC. The cases are one table,
[`internal/client/cli/testdata/relative-dates.json`](../../internal/client/cli/testdata/relative-dates.json)
(`case`, `now` in RFC 3339, `tz`, `date`, and `want`, or `null` for a date
refused), which the CLI's S1 tests run and the web UI's Hosts list (#71) is
to run too, so the two resolvers cannot drift.

## Operations

| Operation | Does |
| --- | --- |
| `host.add` | Records the Hosts in `hosts`, all or none, each Registered. One Host at fault is refused with its own error; in a batch of several, every Host is checked and the refusal lists each one at fault ([a refused batch](#a-refused-batch)). |
| `host.list` | Every Host the caller can see that is not Retired and matches the filters, by the sort keys and then by name, in natural order, and id ([filtering and sorting](#filtering-and-sorting)); the Notes only with `with_note`. |
| `host.show` | One Host. |
| `host.set` | Sets the address, port, Host Account, Attributes and Note it names, and clears the Attribute keys in `unset`, the Note for `note`, and the port back to 22 for `port`; nothing else changes. It refuses a request that changes nothing, a key both set and cleared, and clearing the address or the Host Account ([`record.Required`](../../internal/shared/record/record.go)). It takes no `name`, `space` or `state`. |
| `host.rename` | A new name, checked as on `host.add`; the id and everything else stay. |
| `host.move` | Another Space, with the Attributes and Note; the name stays. |
| `host.remove` | Deletes the Host with its Attributes and Note; answers it as it was. A Host with a pin has been Managed, and is refused with `host_was_managed`: it stays on record. |
| `host.enroll` | Makes the Host Managed with its pinned host key ([Enroll](enroll.md)). |
| `attr.list` | Every Preset with its type, an enum's values and a number's least value, in the order of the Preset table, then every other key in use on what the caller can see, by key; each says whether it is a Preset and whether something the caller can see has it. A key held only in a Space the caller cannot act in is answered as if nothing held it. Each area whose records hold Attributes lists its keys through an `attr.Keys`, Hosts through `host.AttributeKeys`, Retired ones among them. |

Enroll reads what logging in to a Host needs through
[`host.ReachOf`](../../internal/server/host/reach.go) (address, port, Host
Account, state and pin, under the same access check), and records the
Registered to Managed transition through `host.Enrolled`, which refuses with
`host_changed` a Host whose Host Account, state or pin changed while Enroll
logged in to it.

- **Naming a Host.** A request names a Host by its name, which names only a
  Host that is not Retired, or by its id (`host_...`), which names any.
- **Who.** Space membership decides every call ([the access check](access.md)):
  a Host in a Space the caller cannot act in answers exactly as one that
  does not exist, `unknown_host`, by name and by id; `host.move` needs the
  target Space too, looked up through `space.ByName`. The Owner and the
  Local caller act in every Space.
- **The one leak.** A name a Host holds is refused with `name_taken`, "a Host
  named "web1" already exists", the same whichever Space holds it, one the
  caller can see or not. It names nothing else.
- **The Note** is in every answer about one Host; `host.list` leaves it out
  unless `with_note` asks for it.
- **What a Space holds** is counted by [`host.Holding`](../../internal/server/host/host.go)
  ([what a Space holds](access.md#what-a-space-holds)).

### A refused batch

A `host.add` of several Hosts that refuses any of them checks every one,
adds none, and lists each Host at fault in the error's `items` ([the error
object](api.md#calls), [`api.HostBatchError`](../../internal/shared/api/host.go)).
The error's code is the one every item shares, or `invalid_batch` when they
differ; its message lists each item on a line of its own:

```json
{"error": {"code": "invalid_batch", "message": "none of the 3 Hosts was added, for the 2 at fault:\n[0] a Host named \"web1\" already exists\n[2] no Space is named \"nowhere\"",
  "items": [{"index": 0, "code": "name_taken", "message": "a Host named \"web1\" already exists"},
            {"index": 2, "code": "unknown_space", "message": "no Space is named \"nowhere\""}]}}
```

A Host under the name of an earlier Host of the batch is refused with
`name_taken`, whether or not the earlier one is at fault. A request of one
Host is refused with that Host's own error and no `items`, as before. The
Hosts the batch recorded before a refusal go with the rolled-back
transaction.

## The CLI

`koko host add`, `ls`, `show`, `set`, `rename`, `move` and `rm` ([the commands
so far](cli.md#the-commands-so-far)). Fields and Attributes are both
`key=value`; `koko host set` refuses `name=`, `space=` and `state=`, naming
`koko host rename` and `koko host move`. `--note-file <file>` reads the Note from a file
and `--note-file -` from stdin, which it refuses when stdin is a terminal.
`--json` prints the CLI's own Host, `hostJSON`, with its Attributes in their
own object, each in its JSON type, and `note` a string or `null`, which `ls`
leaves out without `--with-note`. For people, `ls` prints a table (`NAME
SPACE STATE ADDRESS PORT ACCOUNT ID`) followed, with `--with-note`, by each
Host's Note. A Preset's value that does not parse, and a relative date that
does not resolve, are refused with exit 2 before any call.

`koko host ls` takes filter terms that need no quoting in a shell
([`readFilters`](../../internal/client/cli/filter.go)): `key=value`,
`key!=value`, `key.lt=value`, `key.le=value`, `key.gt=value`, `key.ge=value`,
`key.has` and `key.lacks`, ANDed, and `--sort key` or `--sort key.desc`,
repeatable. It checks each term's syntax, key and value as the koko server
would and refuses a bad one with exit 2 before any call:
`koko host ls expires_at.le=+30d auto_renew=no --sort expires_at` lists the
Hosts due for renewal, soonest first.

`koko attr ls` prints what `attr.list` answers, as a bare array of `{"key",
"type", "values", "min", "preset", "in_use"}`, `values` an enum's values or
`null` and `min` a number's least value (`price`'s `0`) or `null`; for
people, a table `KEY TYPE PRESET IN USE`, an enum of more than 10 values
named by its count, first and last.

### Hosts as JSON on stdin

`koko host add` without a name reads Hosts as JSON on stdin
([`hostAddStdin`](../../internal/client/cli/host_stdin.go)), so an agent
that turns a provider's welcome email into JSON pipes it in, and `koko host
ls --json --with-note | koko host add` copies Hosts, Notes and all. It reads
what `host ls --json` prints: one Host as an object, or several as an array,
recorded in one `host.add` call, all or none.

- **The members** are `hostJSON`'s. `name`, `address` and `account` are
  required; `space`, `port`, `attributes` and `note` may be left out or
  `null`, for the caller's Personal Space, port 22, no Attributes and no
  Note. `id`, `state`, `host_key`, `created_at` and `updated_at`, which only
  koko sets, are ignored whatever they hold, so every Host added is
  Registered, with no pin. Any other member is refused by name. An
  Attribute value comes in its JSON type, as `ls --json` prints it, and a
  date is absolute: stdin's dates are not resolved.
- **Before any call**, with exit 2 and `invalid_usage`: a terminal on stdin,
  which nobody may type into; stdin that holds no Hosts as JSON, or more
  than the 16 MiB one call carries; and `key=value` words or `--note-file`,
  which only `koko host add <host>` takes. Each Host meets the value rules
  `key=value` meets, and holds its JSON members in their types. A batch with
  Hosts at fault lists each of them, as the koko server does ([a refused
  batch](#a-refused-batch)), every item with the code `invalid_usage`. A
  Host's name is the koko server's to check, as for `koko host add <host>`.
- **It prints** what it read: one Host for an object, in `host show`'s shape,
  and an array of Hosts for an array, each with its Note. For people, one
  line per Host: `Added Host web1 (host_...) to Space alice.`

## Verifying

- [`internal/client/cli/host_test.go`](../../internal/client/cli/host_test.go):
  every `host` command with its goldens, each value rule refused once before
  any call, the one leak answered alike whichever Space holds the name, a Host
  the caller cannot see answering as unknown, the Spaces a move needs,
  concurrent adds of one name, the Note from a file and from stdin, and
  `space show` and `space rm` counting Hosts.
- [`internal/client/cli/host_stdin_test.go`](../../internal/client/cli/host_stdin_test.go):
  `koko host add` on stdin with its goldens: an array and one object, the
  fields only koko sets ignored, an unknown member refused by name, each
  value rule refused before any call, a refused batch listing every Host at
  fault, before any call and from the koko server, a terminal refused, and
  `ls --json --with-note | host add` keeping Notes and typed values.
- [`internal/client/cli/enroll_test.go`](../../internal/client/cli/enroll_test.go):
  the state table through the real `koko host enroll`, and `host rm` refused
  once a Host has a pin.
- [`internal/server/host/host_test.go`](../../internal/server/host/host_test.go):
  the koko server's own checks on raw `host.add` and `host.set` calls, a batch
  added all or none, and which answers carry the Note.
- [`internal/server/host/preset_test.go`](../../internal/server/host/preset_test.go):
  each Preset's value refused when it does not parse or comes in the wrong
  JSON type, and numbers stored in their one form and written as JSON
  numbers exactly as stored.
- [`internal/server/host/list_test.go`](../../internal/server/host/list_test.go):
  the comparisons as a table (exact decimals, dates, enum order, natural
  order, case, a Host lacking the key under every operator and in both sort
  directions, the fields) and each refusal of a bad filter.
- [`internal/client/cli/host_filter_test.go`](../../internal/client/cli/host_filter_test.go):
  the renewal question with its golden, each operator, repeated `--sort`,
  each malformed term refused before any call, a `TZ` koko cannot load, the
  relative-date table in writes, a Preset's value refused before any call,
  and typed values in `--json`.
- [`internal/client/cli/attr_test.go`](../../internal/client/cli/attr_test.go):
  `koko attr ls` with its golden, and the keys in use as each caller sees
  them.
- [`internal/server/access/matrix_host_test.go`](../../internal/server/access/matrix_host_test.go)
  and [`matrix_attr_test.go`](../../internal/server/access/matrix_attr_test.go):
  every host operation and `attr.list` as each caller.
