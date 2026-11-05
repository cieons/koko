# A refused batch lists each item at fault

`koko host add` records a batch of Hosts all or none, and when it refuses one
of several Hosts it checks every Host and lists each one at fault, in the
error object every failure prints: `{"error": {"code", "message", "items":
[{"index", "code", "message"}]}}`. `index` counts from 0 in the order the
batch gave, so it is the item's place in the JSON array on stdin; `code` and
`message` are what the Host would be refused with alone. The error's own
`code` is the one every item shares, so a batch whose Hosts are all at fault
for a taken name is `name_taken`, or `invalid_batch` when they differ; its
`message` lists each item on a line of its own, `[3] ...`. The CLI's own
checks before any call refuse a batch the same way, with `invalid_usage` and
exit status 2; the koko server's, with exit status 1. A request of one Host is
refused with that Host's own error and no `items`, as `koko host add <host>`
and the web UI's form send it.

## Considered Options

- **The first Host at fault only**, as `host.add` answered before: no new
  shape, but an agent fixing a batch of twenty would learn its faults one call
  at a time.
- **Every Host at fault in the message alone**: one call, but an agent would
  parse prose to learn which item to fix and why.
- **`invalid_batch` for every refused batch**: one rule, but a batch whose
  Hosts all hold taken names would not say `name_taken`, which a script
  branches on for a single Host.
- **`items` on every refusal of `host.add`, one Host included**: one shape,
  but `koko host add web1` would answer a taken name unlike `koko host rename`
  does, and each refusal would repeat itself.

## Consequences

- `items` and `invalid_batch` join the CLI's contract (ADR 0016): they are
  only ever added to, never renamed or removed, within a major version.
- A script reads `error.items` after a refused batch, and `error.code` and
  `error.message` after any other refusal.
- The koko server checks every Host of a refused batch, so a batch costs as
  much to refuse as to record, inside one transaction that it then rolls
  back.
- A later Host of a batch under an earlier one's name is at fault even when
  the earlier one is too, so fixing the earlier one never reveals the clash.
- What only the koko server can tell, such as a taken name or an unknown
  Space, comes after the CLI's own checks pass: a batch may take two calls to
  learn every fault, one refused with exit status 2 and the next with 1.
