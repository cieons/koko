# A Host's Relay is an id and a name, both null when unseen

A Host's `relay` in the CLI's `--json`, and in the API, is `null` for a Host
without a Relay, `{"id": "host_...", "name": "web2"}` for a Relay the reader
can see, and `{"id": null, "name": null}` for one in a Space the reader
cannot act in, which a move may leave it in (P0 spec: the record, "Relay").
So a Relay the reader cannot see is present but unnamed, as the Gateway's
messages call it "its Relay", and a script tells the three cases apart
without parsing a string. `relay=` on the command line takes a name or an
id, as every other place that names a Host does.

## Considered Options

- **A string, as a Service's `host` is**: `"web2"`, or the id once the Relay
  is Retired, matching what `relay=` takes. But a Relay the reader cannot see
  then needs a sentinel, such as `"(unseen)"` or `""`, which a script would
  pass back to koko as a name, or a second member such as `relay_seen` that
  must be read together with the first.
- **The id alone**: stable through renames, but an id of a Host the reader
  cannot see still names it, and a person reads names.

## Consequences

- The object mirrors a Host's own identity, its `id` and its `name` (the name
  it had, once Retired), so a later member, such as the Relay's state, is
  added beside them without retyping `relay` (ADR 0016).
- `koko host add` reads `relay` on stdin in the same shape: by its `id` when
  given, which names it whatever its name became, else by its `name`. One
  with neither, as `host ls --json` prints an unseen Relay, is refused before
  any call rather than dropped, since koko sets only a Relay its setter can
  see.
- `host ls` filters and sorts on `relay` by the Relay's name. An unseen Relay
  has the key with an empty name, so `relay.has` and `relay!=web2` match it,
  `relay=web2` never does, and it sorts before every named Relay: no term
  compares a name the reader may not see.
