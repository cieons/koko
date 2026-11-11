# A write of several records is applied all or none

The Owner moves, tags, retires and deletes Hosts and Secrets several at a
time, from the web UI's lists and from the CLI, and chose on a live preview
of #134 that such a batch succeeds or fails as one, the Owner's choice of
2026-11-11. ADR 0030 made `host.add` a batch; this extends its rule to every
write that changes or deletes records one at a time: `host.set`,
`host.move`, `host.retire` and `host.remove` take `hosts`, a list of Hosts
by name or id, and `secret.move` and `secret.remove` take `secrets`, a list
of `{"space", "name"}`, in place of the one record they name, and answer
`hosts` or `secrets` in the order asked. A batch runs in one transaction:
the koko server checks every record, and when any is at fault it applies
none and refuses with ADR 0030's error object, the code every item shares
or `invalid_batch`, and each item at fault by its index:

```json
{"error": {"code": "invalid_batch", "message": "none of the 20 Hosts was moved, for the 3 at fault: ...",
  "items": [{"index": 1, "code": "unknown_host", "message": "no Host is named \"web9\""}]}}
```

Each record is checked as the one-record form checks it, access included,
so a record the caller may not act on is at fault as one that does not
exist. A batch adds the rules that only several records need:

- `host.retire` counts only the Hosts outside the batch as using a Relay,
  so a Relay retires with the Hosts behind it; `host.set` does the same
  when it gives a Relay and the Hosts behind it a new Relay together.
- `host.set` changes each Host alike; a Host that holds the values already
  is not at fault, and a request that names nothing to change is refused
  once, with `invalid_request`, as for one Host.
- `secret.move` refuses two Secrets of one name bound for the same Space,
  each with `name_taken`, as it refuses a name the Space holds.
- A record named twice is at fault at its later index, with
  `invalid_request`.
- `host.move` ends the live SSH Sessions that lose their Host only once the
  batch commits, as the one-record form does.

The one-record request stays, so older CLIs keep working; a request that
names both forms, or an empty list, is `invalid_request`, and a batch of
one is refused with its record's own error and no `items`, as ADR 0030 has
it. The new fields raise each operation's revision (ADR 0049), so a CLI
sends a batch only to a koko server that takes one, and otherwise fails
with `server_too_old` before sending it; `--where` has read its Hosts by
then.

The CLI takes several names where it took one: `koko host set web1 web2
role=node`, `koko host move web1 web2 --to ops`, `koko host retire web1
web2`, `koko host rm web1 web2`, `koko secret move ops/a ops/b --to web`
and `koko secret rm ops/a ops/b`, sending one name in the one-record form
and several as a batch. Only `host set` and `host move` also take
`--where <term>`, with `host ls`'s terms: the CLI lists the matching Hosts
with one `host.list`, says how many it matched, and sends their ids as one
batch, so the batch is what matched when it ran. `host retire` and `host
rm` take names only, the router's decision for the Owner: an irreversible
command acts on what its command line names, keeping ADR 0019's risk in
the command path, where a filter could match more than its writer meant.

## Considered Options

- **Each record in turn, its failures reported one by one**: the web UI
  would call the one-record form for each, and a failure halfway would
  leave the batch half done, for the Owner to find and finish by hand.
- **Apply what can be applied, and refuse the rest**: one call, but the
  outcome is still partial, and a script must read which records changed
  before it can try again.
- **New operations, such as `host.move_many`**: no request with two forms,
  but twice the operations to list, check and document, for one change of
  shape.
- **`--where` on every batch command**: one rule, but `koko host rm
  --where state=registered` would delete what a filter happened to match,
  where a harness rule allowing `koko host rm` meant a named Host.

## Consequences

- `invalid_batch` is no longer only `host.add`'s: it is a code every batch
  shares, and `items` may follow any batch write's refusal.
- A batch costs as much to refuse as to apply: the koko server checks every
  record inside the transaction it then rolls back.
- An item's index is its place in the list the request sent. With
  `--where`, that is the order `host.list` answered, and each message names
  its Host, so a person need not count.
- A refused batch leaves the caller to send it again without the records
  at fault; the web UI offers that in one step.
- Responses gain `hosts` and `secrets`, and the one-record `host` or
  `secret` is left out of a batch's answer, so a client reads the field of
  the form it sent.
