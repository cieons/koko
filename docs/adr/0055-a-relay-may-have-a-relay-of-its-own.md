# A Relay may have a Relay of its own

koko reaches a Host through a chain of Relays, the Owner's choice of
2026-11-13, so that a Host two or more networks deep lives in koko rather
than in a hand-written ProxyJump. The chain follows from each Host's own
`relay`: app1's Relay is db1 and db1's Relay is web1, so the Gateway reaches
app1 through web1, then db1. Nothing new is stored: `relay_id` already links
a Host to its Relay by id, and `relay=`, the `{id, name}` JSON of ADR 0035
and the web UI's Relay field keep their shape. A Host's path is a fact about
how koko reaches that Host, so every Host behind db1 shares db1's path, and
moving db1 behind another Relay is one edit.

- **Rules.** A chain never loops, and koko passes through at most 4 Relays
  to reach a Host, counted for the Host being set and for every Host behind
  it. The Relay named must be Managed and seen by the setter, as before; the
  Relays beyond it need not be seen, and each must be Managed when the
  Gateway uses it. A batch is checked against the links as the whole batch
  would leave it. Retire, rename, move and delete keep their rules.
- **Dialing.** The Host connector logs in to each Relay in turn with the
  koko key, each leg inside a `direct-tcpip` channel of the one before,
  checking each Relay's pinned host key, and to the Host last. One connect,
  every leg, is bounded by 45 seconds.
- **What a caller learns.** Only what the links show: a Host shows its own
  Relay, named or unseen, and the rest of the chain is found by following
  each Relay's `relay`, so the trail stops at the first Relay the caller
  cannot see. A message names a Relay of the chain only to a caller who
  can see it and every Relay between it and the Host, and otherwise says
  "app1's Relay". A refusal of a loop or a length gives no count of hops.
- **Devices see no change.** The ssh_config `koko ssh setup` writes, the
  SSH Name, `koko exec`, `koko tunnel` and the web terminal reach a Host
  behind a chain as any Host, since the Gateway makes every hop.

This lifts the P0 record spec's rule that a Relay is exactly one hop. It
refines ADR 0035, whose shape stays: `relay` is a Host's own Relay. It
refines ADR 0050's batch rule for `host.set`, which excepted the Hosts a
batch gives the same Relay only to keep chains one hop long.

## Considered Options

- **Each Host carries an ordered list of Relays,** as ssh's
  `ProxyJump web1,db1`: one Host's path would never change because another
  Host was edited, but it needs a new field in the API, the CLI's JSON and
  the web UI, a migration and a request revision; it repeats the same path
  on every Host behind db1, asks for an edit of each when it changes, and
  lets db1's own path and app1's list disagree on how db1 is reached.
- **A `relays` list in the CLI's JSON, unseen hops unnamed:** one place for
  an agent to read, but it tells a Member how many Relays outside their
  Spaces sit on a path, and where.
- **Naming every Relay of the chain the caller can see,** whether or not
  the links reach it: it would tell a Member that app1 passes through web1,
  which the record never told them.
- **No limit on length:** every hop is one more SSH connection for each SSH
  Session and more time to dial. **A limit of 2:** enough for the Owner's
  case today, but a third network is common enough not to stop there.
- **Only the Owner changes the Relay of a Host other Hosts use:** it guards
  against a Member making the Hosts behind their Relay unreachable, which a
  change of that Relay's Host Account already can, and refuses an ordinary
  edit for a reason the Member cannot see.

## Consequences

- No request field, operation revision, channel revision or migration
  changes, and `compat.Oldest` does not move. An older koko server refuses
  a Relay that has a Relay with `invalid_relay`, saying a Relay is exactly
  one hop; an older CLI reaches a Host behind a chain and prints only its
  own Relay.
- A Member who may act on a Relay may change its own Relay, which changes
  the path of every Host behind it, in Spaces they cannot see too. That
  gives no access: each leg runs inside the one before, so a Relay sees only
  the encrypted connection to the next hop and that hop's address, and the
  pins at every hop stop an impostor. It can make the Hosts behind it
  unreachable until put right, as a change of its Host Account already can.
- A refusal of a loop tells the setter that a Host they can see is reached
  through the one they are setting, and one of a length that some path is
  long, as `relay_in_use` tells that some Host uses a Relay: they learn that
  a path exists, never who is on it.
- A live SSH Session keeps the chain it started through while every Relay
  of it, and of its Host's chain now, stays Managed; any change that names a
  Host checks every live SSH Session again.
- `host ls relay=` keeps meaning a Host's own Relay. Finding every Host
  behind a Relay anywhere in its chain is later.
- Each SSH Session opens its own connection at every hop. Sharing a Relay's
  connection across SSH Sessions is later.
