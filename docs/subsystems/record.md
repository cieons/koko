# The record: Hosts and Services

The Hosts koko records, each in one Space, and the Services that run on
them, with their Attributes and Notes, and the filters and sorting of their
lists.
The rules are [P0 spec: the record](https://github.com/cieons/koko/issues/22),
[ADR 0014](../adr/0014-host-names-are-unique-across-the-koko-server.md),
[ADR 0029](../adr/0029-a-number-attribute-is-a-decimal-a-javascript-number-holds.md) and
[ADR 0031](../adr/0031-a-services-port-is-written-as-one-string.md);
the operations are [`internal/server/host`](../../internal/server/host/host.go),
[`internal/server/service`](../../internal/server/service/service.go)
and [`internal/server/attr`](../../internal/server/attr/attr.go), the
filtering and sorting [`internal/server/query`](../../internal/server/query/query.go),
the value rules [`internal/shared/record`](../../internal/shared/record/record.go),
and the commands [`internal/client/cli/host.go`](../../internal/client/cli/host.go),
[`service.go`](../../internal/client/cli/service.go),
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
| `relay` | The Host the Gateway passes through to reach this one, or `null` for none ([a Relay](#a-relay)): `{"id", "name"}`, both `null` for a Relay the reader cannot see ([ADR 0035](../adr/0035-a-relay-is-an-id-and-a-name-both-null-when-unseen.md)). Stored as `relay_id`, the Relay's id ([`hosts_relay.sql`](../../internal/server/store/migrations/hosts_relay.sql)). |
| `state` | `registered`, `managed` or `retired`, set only by koko. Every Host is Registered until Enroll makes it Managed ([Enroll](enroll.md)), and `host retire` makes it Retired, which is terminal ([retiring a Host](#retiring-a-host)). A change of address or port keeps a Host Managed, since its pin proves the Host wherever it answers; a change of Host Account sends it back to Registered, keeping its pin, since the koko key went only to the old Host Account, and ends its live SSH Sessions ([the Gateway](gateway.md#access-during-a-live-ssh-session)). |
| `host_key` | The pinned host key's SHA-256 fingerprint, `SHA256:...`, or `null` for a Host with no pin. Stored as the key in SSH wire format, which Enroll writes. |
| `attributes` | Values by key, each of its key's type ([Attributes](#attributes-and-the-preset-attributes)). A key is snake_case (`a-z`, `0-9`, `_`, starting with a letter, 1 to 63 characters) and not a field's name: `id`, `name`, `space`, `address`, `port`, `account`, `relay`, `state`, `host_key`, `host`, `ports`, `attributes`, `note`, `created_at` or `updated_at`. |
| `note` | 1 byte to 64 KiB of UTF-8 on any number of lines, with no control character but tab, line feed and carriage return; `null` for none. |
| `created_at`, `updated_at` | RFC 3339 in UTC to the second; every write moves `updated_at`. |

The CLI checks each value rule before any call and refuses a bad one with
exit 2 and `invalid_usage`; the koko server checks them again on every
write, whichever client sent it, and refuses with `invalid_value`, or
`invalid_name` for a name ([the codes](api.md#calls)). An empty value and an
empty Note are refused, so clearing is always `--unset`.

### Retiring a Host

`host.retire` makes a Registered or Managed Host Retired, for a Host that is
gone. It keeps its record: its id, Attributes, Note, Services, Space and
pin, and the name it had, which it no longer holds. From then on:

- **Its name is free.** Only a Host that is not Retired holds a name (the
  `name` rule above), so a new Host can be `web1` again, and `web1` names
  that one. Only its id names the Retired Host, in every request, its
  Services' `<host>/<service>` among them, and a Service answers its
  `host` as that id ([`host.Ref`](../../internal/server/host/host.go)).
- **Lists leave it out.** `host.list` lists it only with `all`, and then
  takes it as any other Host, so `state=retired` keeps only the Retired ones.
  `service.list` leaves its Services out with it, with or without filters
  ([`host.ListedNames`](../../internal/server/host/host.go)). `host.show` by
  its id shows it with its Services, and `service.show` shows one. A Space
  counts it apart ([what a Space holds](access.md#what-a-space-holds)), and
  `attr.list` still counts its keys and its Services' ([operations](#operations)).
- **It takes only changes to its Attributes and Note, and moves**: `host.set`
  of them alone, and `host.move`. Everything else is refused with
  `host_retired`, whose message names it by name and id, and changes
  nothing: `host.set` naming the address, port or Host Account, or clearing
  the port, even beside changes it takes; `host.rename`; `host.retire`
  again; `host.enroll`, before any login; and `service.add`, `service.set`,
  `service.rename` and `service.remove` on it, since its Services are kept
  as they were with it.
- **`host.remove`** goes by the pin, as for any Host: a Retired Host that was
  never Managed is deleted with its Services, and one with a pin is refused
  with `host_was_managed`. For a Host that is not Retired, that refusal
  names `koko host retire` as the way on.
- **Nothing reaches it.** The Gateway resolves only names, which name no
  Retired Host, so its old name answers as one no Host holds
  ([the Gateway](gateway.md)).

Retiring ends every live SSH Session to the Host, each with a line naming
the retirement ([the Gateway](gateway.md#access-during-a-live-ssh-session)).

A Relay that a Host that is not Retired uses is not retired: `host.retire`
refuses it with `relay_in_use`, whose message names no Host, not even to a
caller who can see them, and points at `koko host ls relay=<relay>`
([a Relay](#a-relay)). Only a Host's own Relay blocks it, and a batch counts
only the Hosts outside it, so a whole chain retires in one batch.

## A Relay

A Host's Relay is the one Host the Gateway passes through to reach it, for a
Host the koko server cannot reach directly (P0 spec: the record, "Relay").
A Relay may have a Relay of its own ([ADR 0055](../adr/0055-a-relay-may-have-a-relay-of-its-own.md)):
a Host's chain is every Relay the Gateway passes through to reach it, in the
order it dials them, found by following each Host's own `relay` from the
Host. The P0 rule that a Relay is exactly one hop is lifted; the comment of
the migration that added `relay_id` still names it, as it was applied.
`host.add` and `host.set` take a Host's own Relay as `relay`, by name or id,
and `host.set` clears it with `"unset": ["relay"]`; [`relayFor`](../../internal/server/host/host.go)
checks it inside the write's transaction, and refuses with
`invalid_relay` unless said otherwise:

- **The setter must see it**: a Host in a Space they cannot act in is
  refused as one that does not exist, `unknown_host`, its message starting
  `the Relay of web1: `. A Relay may sit in another Space they can see. The
  Relays beyond it need not be seen: they are its own, set by whoever could
  act on it.
- **It is Managed**: a Registered Host is refused, naming `koko host enroll`,
  and a Retired one too, which only its id names in the request. Only the
  Relay being set is checked; the Gateway checks every Relay of a chain when
  it uses it.
- **Not the Host itself**, and **no loop**: `app1 cannot take db1 as its
  Relay: koko reaches db1 through app1, and a chain of Relays never loops`,
  when db1's chain holds app1.
- **At most 4 Relays** (`host.MaxRelays`) for the Host being set, whose chain
  becomes its Relay's chain and the Relay, and for every Host behind it that
  is not Retired. The refusal for a Host behind it names neither that Host
  nor how far behind it sits, and points at `koko host ls relay=<host>`; no
  refusal gives a count.
- **A Retired Host** takes no change of Relay, set or cleared: `host_retired`.

Clearing a Relay only shortens chains, and is never refused for one. The
chains are checked against the links read once before the write
([`readLinks`](../../internal/server/host/chain.go)): a batch
(`koko host set --where … relay=db1`) is checked as it would leave them,
each of its Hosts given the new Relay and so behind none other, and each Host
at fault is listed whatever its place ([ADR 0030](../adr/0030-a-refused-batch-lists-each-item-at-fault.md),
[ADR 0050](../adr/0050-a-write-of-several-records-is-applied-all-or-none.md)).

One walk reads a chain for these rules, for `host.ReachOf` and for Enroll's
`host.Enrolled` ([`walk`](../../internal/server/host/chain.go)): it follows
`relay_id` at most 4 links. A chain that loops, or whose fourth Relay has a
Relay of its own, which the rules never write, is a read that fails
(`host.BrokenChainError`), never no Relay: the Gateway and Enroll refuse the
Host behind it, and `relayFor` refuses it as a Relay, with
`internal_error`. Clearing the Relay of a Host behind it reads no chain, so
it mends the record.

`host.Reach.Relays` is the chain in dial order. Each Relay is `Seen` only
when the caller can see it and every Relay between it and the Host, which is
how far the links show it to them; `Relay.Of` names it so, as `app1's Relay
web1`, and otherwise as `app1's Relay`.

The link is the Relay's id, so a rename keeps it, and a move of any Host of
a chain leaves the chain: the visibility rule is checked only when a Relay is
set. A Relay later in a Space the reader cannot see is present but unnamed:
`null` id and name in `--json` ([ADR 0035](../adr/0035-a-relay-is-an-id-and-a-name-both-null-when-unseen.md)),
`one you cannot see` in `host show` and `(unseen)` in `host ls`, as the
Gateway calls it "its Relay" ([the Gateway](gateway.md#relay)). `relay`,
`relay=` and `host ls`'s `RELAY` column are a Host's own Relay. As P0 spec:
the record accepts, a refused `invalid_relay` or `relay_in_use` tells that
some Host uses the Relay, or that some chain loops or is long, and nothing
about which Hosts are on it.

`host.show`'s answer carries the Host's `path` beside it, for people
([`pathOf`](../../internal/server/host/host.go)): the chain in dial order as
far as `Seen` reaches, each Relay `{"id", "name"}`, the Host's own Relay
last. Where the trail stops at a Relay the reader cannot see, `path` begins
with one `{"id": null, "name": null}` for that Relay and every one before
it, so an unseen Relay is told from none; a Host without a Relay has `[]`,
and a chain the record holds broken shows the Host's own Relay alone, so
the Host that mends it can still be read. `host.list` carries no `path`.
`koko host show` prints it after the Relay, the rest of the chain in dial
order: `Relay:       db1 (host_…), through web1`, or `through one you cannot
see` where the trail stops. Its `--json` prints no `path`, as an agent
follows each `relay.id` instead, and a `host show` against a koko server
older than chains prints the Host's own Relay alone.

A new Relay anywhere in a chain, or none, interrupts no live SSH Session
while every Relay of the chain it started through, and of its Host's chain
now, is Managed. One that is not ends it: a Relay that stops being Managed,
as when its Host Account changes, or one in a new Relay's own chain, which
`relayFor` does not check; so `host.set` announces a Host given another
Relay, and the Gateway checks every live SSH Session again
([the Gateway](gateway.md#access-during-a-live-ssh-session)).

## A Service

What runs on one Host, stored in `services`, with its ports in
`service_ports` and its Attributes in `service_attributes`
([`services.sql`](../../internal/server/store/migrations/services.sql)). A
Service is keyed by its Host's id, so it lives in its Host's Space, moves and
renames with it, goes when `host rm` deletes it (`ON DELETE CASCADE`), and
is hidden and kept as it was with a Retired Host ([retiring a
Host](#retiring-a-host)). It never moves to another Host: to change Host,
record it again. People and agents record Services; koko discovers nothing
itself.

| Field | Rule |
| --- | --- |
| `id` | `service_` and 8 characters, set by koko. |
| `name` | A lowercase DNS label ([`name.Check`](../../internal/shared/name/name.go)), unique on its Host, so `nginx` can run on many Hosts; the unique index on `(host_id, name)` backs the check, which runs inside the write's transaction. Written with its Host as `<host>/<service>`, where a Host id stands for the Host's name. |
| `host` | The name of the Host it runs on, or the Host's id once it is Retired, so that `<host>/<service>` always names the Service. |
| `ports` | A set, which may be empty, of `<port>/<protocol>` strings: a port 1 to 65535, then `tcp` or `udp` in lowercase; `443` alone is `443/tcp` ([`record.ParseServicePorts`](../../internal/shared/record/ports.go), [ADR 0031](../adr/0031-a-services-port-is-written-as-one-string.md)). Answered by port and then protocol, `[]` for none. A port given twice, as `443` and `443/tcp`, is refused; other Services' ports, on the Host or elsewhere, are not checked. |
| `attributes`, `note`, `created_at`, `updated_at` | As on a Host. |

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
  with their types: `id`, `name`, `space`, `address`, `account`, `relay` (the
  Relay's name) and `host_key` as text, `port` as a number, and `state` as an
  enum (`registered`, `managed`, `retired`). A Host without a Relay lacks
  `relay`; one whose Relay the caller cannot see has it with an empty name,
  so `relay.has` matches it and `relay=` never does. Any other key is an Attribute key, of its Preset's
  type or text. The Note, `created_at` and `updated_at` are not keys to
  filter or sort on: a timestamp is no date, and comparing one with a date
  would need the caller's time zone, which the API never sees.
- **The keys of `service.list`** are the fields of
  [`api.ServiceListFields`](../../internal/shared/api/service.go), `id`,
  `name` and `host` (its Host's name), all text, and the Services' own
  Attribute keys. A Host's fields (`space`, `state`, ...) and a Host's
  Attributes are no keys of a Service's: one key would otherwise mean a
  Host's value or a Service's, a Preset being the same on both. To find the
  Services of some Hosts, list the Hosts and then each one's with `host=`.
  The ports, a set, have no comparison.
- **A Host that lacks the key** (an Attribute it does not have, or
  `host_key` without a pin) matches `ne` and `lacks` and no other term, and
  sorts after every Host that has it, in either direction.
- **Order.** By the sort keys in turn; Hosts that tie stay by name, in
  natural order as `--sort name` gives it (`web2` before `web10`), and then
  id, the order without `sort`. Services that tie stay by Host name, then by
  name, each in natural order, and then id, so each Host's Services come
  together.
- **Refusals.** A malformed term, a key the list does not take, and a key
  sorted by twice are refused with `invalid_request`; a value not of its key's
  type with `invalid_value`. The API compares only absolute dates, so
  `"+30d"` is refused too.
- [`query.New`](../../internal/server/query/query.go) checks the terms and
  sort keys and `Query.Apply` applies them to the Hosts the caller can see,
  through each Host's values (`hostLookup`); `service.list` takes the same
  query with its own fields, through `serviceLookup`.

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
refused), which the CLI's S1 tests run and the web UI's resolver runs too
([the record's pages](record-pages.md#relative-dates)), so the two
resolvers cannot drift.

## Operations

| Operation | Does |
| --- | --- |
| `host.add` | Records the Hosts in `hosts`, all or none, each Registered. One Host at fault is refused with its own error; in a batch of several, every Host is checked and the refusal lists each one at fault ([a refused batch](#a-refused-batch)). |
| `host.list` | Every Host the caller can see that matches the filters, the Retired ones only with `all`, by the sort keys and then by name, in natural order, and id ([filtering and sorting](#filtering-and-sorting)); the Notes only with `with_note`. |
| `host.show` | One Host, with its Services as `services`, by name in natural order, without their Notes, which the service area lists for it ([where an area adds its parts](code-layout.md#where-an-area-adds-its-parts)). |
| `host.set` | Sets, on the Host `host` names or on each of `hosts` ([a refused batch](#a-refused-batch)), the address, port, Host Account, Relay, Attributes and Note it names, and clears the Attribute keys in `unset`, the Note for `note`, the Relay for `relay`, and the port back to 22 for `port`; nothing else changes. It refuses a request that changes nothing, a key both set and cleared, and clearing the address or the Host Account ([`record.Required`](../../internal/shared/record/record.go)). It takes no `name`, `space` or `state`, and on a Retired Host only Attributes and the Note ([retiring a Host](#retiring-a-host)). In a batch, a Host that holds the values already is not at fault. |
| `host.rename` | A new name, checked as on `host.add`; the id and everything else stay. A Retired Host is refused with `host_retired`. |
| `host.move` | Another Space, for the Host `host` names or each of `hosts`, with the Attributes, Note and Services; the name stays. A Retired Host moves too. It ends each live SSH Session to the Host of a caller who cannot act in the new Space ([the Gateway](gateway.md#access-during-a-live-ssh-session)). |
| `host.retire` | Makes a Registered or Managed Host Retired, or each of `hosts`, keeping its record, and answers it so; one Retired already is refused with `host_retired`, and the Relay of a Host that is not Retired with `relay_in_use`, counting in a batch only the Hosts outside it, so a Relay retires with the Hosts behind it ([retiring a Host](#retiring-a-host)). |
| `host.remove` | Deletes the Host, or each of `hosts`, with its Attributes, Note and Services; answers it as it was. A Host with a pin has been Managed, and is refused with `host_was_managed`: it stays on record, and retiring it is the way on. |
| `host.enroll` | Makes the Host Managed with its pinned host key ([Enroll](enroll.md)). |
| `service.add` | Records a Service on a Host, with its ports, none when left out. On a Retired Host it, `service.set`, `service.rename` and `service.remove` are refused with `host_retired`. |
| `service.list` | Every Service that matches the filters, on the Hosts the caller can see that are not Retired, by the sort keys and then by Host name, name and id ([filtering and sorting](#filtering-and-sorting)); the Notes only with `with_note`. |
| `service.show` | One Service. |
| `service.set` | Sets the ports (replacing the whole set), Attributes and Note it names, and clears the Attribute keys in `unset`, the Note for `note`, and every port for `ports`; nothing else changes. It refuses a request that changes nothing, a key both set and cleared, and `"ports": []`, since clearing is `unset`. It takes no `new_name` and no Host. |
| `service.rename` | A new name on the same Host, checked as on `service.add`; the id and everything else stay. |
| `service.remove` | Deletes the Service with its ports, Attributes and Note; answers it as it was. |
| `attr.list` | Every Preset with its type, an enum's values and a number's least value, in the order of the Preset table, then every other key in use on what the caller can see, by key; each says whether it is a Preset and whether something the caller can see has it. A key held only in a Space the caller cannot act in is answered as if nothing held it. Each area whose records hold Attributes lists its keys through an `attr.Keys`, Hosts through `host.AttributeKeys`, Retired ones among them, and Services through `service.AttributeKeys`. |

Enroll and the Gateway read what logging in to a Host needs through
[`host.ReachOf`](../../internal/server/host/reach.go) (address, port, Host
Account, state and pin, under the same access check), with its Relay's, read
whether or not the caller can see it, and `Seen`, which says whether a
message may name it (`Relay.Of`). Enroll records the Registered to Managed
transition through `host.Enrolled`, which refuses with `host_changed` a Host
whose Host Account, state, pin or Relay changed while Enroll logged in to it.

- **Naming a Host.** A request names a Host by its name, which names only a
  Host that is not Retired, or by its id (`host_...`), which names any.
- **Who.** Space membership decides every call ([the access check](access.md)):
  a Host in a Space the caller cannot act in answers exactly as one that
  does not exist, `unknown_host`, by name and by id; `host.move` needs the
  target Space too, looked up through `space.ByName`. The Owner and the
  Local caller act in every Space.
- **Naming a Service.** A request names a Service by its Host, `host`, named
  as above, and its `name` on that Host. A Service is seen by whoever sees
  its Host ([`host.Find`](../../internal/server/host/host.go), and
  `host.ListedNames` for the lists): on a Host the caller cannot see, every service
  operation answers `unknown_host`, exactly as for a Host that does not
  exist, and on one they can see, a name the Host has no Service of answers
  `unknown_service`. A name another Service on the Host holds is refused
  with `name_taken`, which tells nothing the caller could not list.
- **The one leak.** A name a Host holds is refused with `name_taken`, "a Host
  named "web1" already exists", the same whichever Space holds it, one the
  caller can see or not. It names nothing else.
- **The Note** is in every answer about one Host; `host.list` leaves it out
  unless `with_note` asks for it.
- **What a Space holds** is counted by [`host.Holding`](../../internal/server/host/host.go)
  ([what a Space holds](access.md#what-a-space-holds)).

### A refused batch

A `host.add` of several Hosts, and from their revision 2 a `host.set`,
`host.move`, `host.retire` or `host.remove` of several, named in `hosts`,
that refuses any of them checks every one, applies none, and lists each
Host at fault in the error's `items` ([batches](api.md#batches),
[`api.BatchError`](../../internal/shared/api/errors.go)).
The error's code is the one every item shares, or `invalid_batch` when they
differ; its message lists each item on a line of its own:

```json
{"error": {"code": "invalid_batch", "message": "none of the 3 Hosts was added, for the 2 at fault:\n[0] a Host named \"web1\" already exists\n[2] no Space is named \"nowhere\"",
  "items": [{"index": 0, "code": "name_taken", "message": "a Host named \"web1\" already exists"},
            {"index": 2, "code": "unknown_space", "message": "no Space is named \"nowhere\""}]}}
```

A Host under the name of an earlier Host of the batch is refused with
`name_taken`, whether or not the earlier one is at fault; in the other
batches, a Host named twice, by name or by id, is at fault at its later
index with `invalid_request`. A request of one
Host is refused with that Host's own error and no `items`, as before. The
Hosts the batch recorded before a refusal go with the rolled-back
transaction.

## The CLI

`koko host add`, `ls`, `show`, `set`, `rename`, `move`, `retire` and `rm`
([the commands so far](cli.md#the-commands-so-far)); `set`, `move`,
`retire` and `rm` take several Hosts at once, all or none, and `set` and
`move` take `--where <term>` in their place ([several records at
once](cli.md#several-records-at-once)). Fields and Attributes are both
`key=value`; `relay=<host>` takes a Host name or id, checked for its form before any call,
and `koko host set --unset relay` clears it;
`koko host set` refuses `name=`, `space=` and `state=`, naming
`koko host rename`, `koko host move` and `koko host retire`. `--note-file <file>` reads the Note from a file
and `--note-file -` from stdin, which it refuses when stdin is a terminal.
`--json` prints the CLI's own Host, `hostJSON`, with its Attributes in their
own object, each in its JSON type, `relay` as [a Relay](#a-relay) says, and
`note` a string or `null`, which `ls`
leaves out without `--with-note`. For people, `ls` prints a table (`NAME
SPACE STATE ADDRESS PORT ACCOUNT RELAY ID`, the Relay `-` for none) followed, with `--with-note`, by each
Host's Note, and `show` prints the Host's Services, each name with its
ports, after its Attributes; `show --json` stays the Host as `ls` prints
one, and `koko service ls host=<host> --json` lists its Services. A Preset's
value that does not parse, and a relative date that does not resolve, are
refused with exit 2 before any call.

`koko host retire <host>` prints the Host as it left it, Retired, in `host
show`'s shape, Note and all, as `rm` prints the Host it deleted; for people,
`Retired Host web1 (host_...); name it by its id from now on.` `koko host ls
--all` adds the Retired Hosts, and `koko host ls --all state=retired` lists
only them; without `--all`, `state=retired` matches nothing.

On a joined device, the Host commands that change which Hosts the caller
sees, or by what name, also rewrite the [bare names](device-connection.md#ssh)
in koko's ssh_config at once.

`koko host ls` takes filter terms that need no quoting in a shell
([`readFilters`](../../internal/client/cli/filter.go)): `key=value`,
`key!=value`, `key.lt=value`, `key.le=value`, `key.gt=value`, `key.ge=value`,
`key.has` and `key.lacks`, ANDed, and `--sort key` or `--sort key.desc`,
repeatable. Its entry in the command tree carries `api.HostListFields` as
its `Filters`, which `readFilters` and `readSort` check against and `koko
skill` lists. It checks each term's syntax, key and value as the koko server
would and refuses a bad one with exit 2 before any call:
`koko host ls expires_at.le=+30d auto_renew=no --sort expires_at` lists the
Hosts due for renewal, soonest first.

`koko service add`, `ls`, `show`, `set`, `rename` and `rm` name a Service
`<host>/<service>`. Its ports are `ports=443/tcp,51820/udp`, comma-separated,
which `koko service set` replaces whole and `--unset ports` clears; every
other `key=value` is an Attribute. `name=`, `host=` and `space=` are refused,
naming `koko service rename` for the first and saying that a Service never
moves to another Host, nor out of its Host's Space; `koko service rename`
takes a new name without a Host. `--json` prints the CLI's own Service,
`serviceJSON`: `{"id", "name", "host", "ports", "attributes", "note",
"created_at", "updated_at"}`, the Note as on a Host. For people, `ls` prints a
table (`NAME PORTS ID`, the name as `<host>/<service>` and the ports as
`ports=` takes them, `-` for none) followed, with `--with-note`, by each
Service's Note. `koko service ls` takes the filter terms and `--sort` of
`koko host ls`, on its own keys (its entry's `Filters`,
`api.ServiceListFields`): `koko service ls host=web1` lists one Host's
Services, and `koko service ls kind=proxy` every Service of a kind.

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
  required; `space`, `port`, `relay`, `attributes` and `note` may be left out
  or `null`, for the caller's Personal Space, port 22, no Relay, no
  Attributes and no Note. `relay` names the Relay by its `id` when given,
  else by its `name`; one with neither, as `ls --json` prints a Relay the
  reader cannot see, is refused rather than dropped. `id`, `state`, `host_key`, `created_at` and `updated_at`, which only
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
- [`internal/client/cli/relay_test.go`](../../internal/client/cli/relay_test.go):
  a Relay set only when it can be one, each refusal changing nothing and a
  malformed `relay=` refused before any call; the link kept across a rename
  and moves; a Relay a Member cannot see unnamed in `host show`, `host ls`
  and `--json` (golden files `host-show-relay*.json`), found by `relay.has`
  and by no name, and passed through; a Member setting a Relay in another
  Space they can see, and refused one they cannot; a Relay in use not
  retired; and `relay` read on stdin. The Gateway and Enroll through a Relay
  are in [the Gateway](gateway.md#verifying) and [Enroll](enroll.md#verifying).
- [`internal/client/cli/chain_test.go`](../../internal/client/cli/chain_test.go):
  chains of 2 and 4 set one Host at a time, a Relay with Hosts behind it
  given a Relay, a fifth Relay refused on the Host and on a Relay with Hosts
  behind it, loops through 1 and 3 hops, `host add` at the limit and past
  it, `--unset relay` taken; batches through `koko host set --where`, all
  or none, with a loop and a length listed by place, and a Host of the batch
  behind none other; the middle Relay not retired and the whole chain
  retired at once; a Member setting a Relay whose own Relay they cannot see
  (golden file `host-show-chain.json`); and a chain broken in the store,
  refused and never read as no Relay, mended by clearing a Relay.
- [`internal/client/cli/host_retire_test.go`](../../internal/client/cli/host_retire_test.go):
  `koko host retire` with its goldens, a Registered and a Managed Host
  retiring, its name taken by a new Host while its id shows it, `host ls
  --all` and `--all state=retired`, each change a Retired Host refuses and
  takes, Enroll refused without a connection, `host rm` by the pin, its
  Services hidden from `service ls` and kept as they were, and `space show`
  and `space rm` counting it. `exec_test.go` refuses its old name as no
  Host's.
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
- [`internal/client/cli/service_test.go`](../../internal/client/cli/service_test.go):
  every `service` command but `ls` with its goldens, ports by default `tcp`
  and an empty set, each value rule refused once before any call, names
  unique on their Host, a Service on a Host the caller cannot see answering
  as for no Host, a Host id in `<host>/<service>`, a moved or renamed Host
  keeping its Services, `host rm` deleting them, `koko host show` listing
  them for people only, and `koko attr ls` listing their keys.
- [`internal/client/cli/service_ls_test.go`](../../internal/client/cli/service_ls_test.go):
  `koko service ls` with its goldens, as each caller sees it, its order,
  `host=` and Attribute filters, and a Host's fields refused.
- [`internal/server/service/service_test.go`](../../internal/server/service/service_test.go):
  the koko server's own checks on raw `service.add`, `service.set`,
  `service.rename` and `service.list` calls, which answers carry the Note,
  and `host.show` answering the Host's Services.
- [`internal/server/access/matrix_host_test.go`](../../internal/server/access/matrix_host_test.go),
  [`matrix_service_test.go`](../../internal/server/access/matrix_service_test.go)
  and [`matrix_attr_test.go`](../../internal/server/access/matrix_attr_test.go):
  every host and service operation and `attr.list` as each caller.
