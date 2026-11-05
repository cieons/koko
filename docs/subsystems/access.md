# The access check and Spaces

Who a call acts as, what it may do, and the Spaces and memberships that
decide it. The rule is [ADR 0011](../adr/0011-access-is-space-membership.md)
and [ADR 0008](../adr/0008-local-callers-act-as-the-owner.md); the source is
[`internal/server/access`](../../internal/server/access/access.go) and
[`internal/server/space`](../../internal/server/space/space.go).

## The four questions

[`access`](../../internal/server/access/access.go) is the one place that
decides access. No other package reads roles or memberships to decide what a
caller may do. Every answer is read from the current records, inside the
call's transaction, so a change (a membership, a revocation) holds from the
next call. Every operation asks who is calling, even one every caller may
make, such as `server.show`, so a Revoked Device Session is refused
([revoking](device-connection.md#revoking)).

| Question | Answer |
| --- | --- |
| Who is calling | `access.Who(ctx, tx, caller)`: an `Identity`, the User and the Device Session the call came on, or the Owner with no Device Session for the Local caller |
| Is this caller the Owner | `Identity.IsOwner()`; `access.OwnerOnly(ctx, tx, caller, does)` also refuses anyone else with `owner_only`, "only the Owner `<does>`" |
| Which Spaces can this caller act in | `Identity.Spaces(ctx, tx)`, the one place the rule is written: every Space for the Owner; for any other User their own Personal Space and each Space they are a Member of. A `Scope` whose `Has(spaceID)` filters lists and counts |
| Can this caller act in this Space | `Identity.CanAct(ctx, tx, spaceID)`, by that rule |

Nothing else grants access: no Attribute, no per-record grant, no role on a
membership.

## What a caller cannot see

- **Unseen equals unknown.** A Space the caller cannot act in answers exactly
  as a name no Space holds, byte for byte. [`space.ByName`](../../internal/server/space/space.go)
  is the lookup that does so for every operation that names a Space, with
  `unknown_space`; an area whose records live in a Space looks the Space up
  through it, and leaves records in an unseen Space out of every list, count
  and filter through `Identity.Spaces`. A record the call names no Space for
  goes to the caller's Personal Space, which `space.Personal` returns.
- **Owner-only before anything else.** An operation only the Owner may make
  asks `access.OwnerOnly` before it reads anything the call names, so a Member
  gets the same `owner_only` for a name that is taken, free, unseen or
  unknown, and learns nothing about the records. `space.add`, `space.rename`,
  `space.remove`, `space.member.add`, `space.member.remove` and `user.invite`
  do so. `device.add` is anyone's for their own User; naming another User is
  refused with `owner_only` before that User is looked up.

## Spaces and memberships

- A Space is shared, or a User's Personal Space, which `spaces.user_id`
  names; that is not a membership. `api.Space.personal` and the CLI's
  `personal` say which: a Space is Personal exactly while `user_id` is set.
  Removing a User (#50) turns their Personal Space into an ordinary shared
  Space by clearing it, so the Owner can then rename it, give it Members and
  remove it once empty.
- A membership is a row of `memberships` (a Space and a User, no role). It
  goes with its Space when the Space is removed, and with its User when a
  User who never joined is deleted (`ON DELETE CASCADE`).
- The Owner can be made a Member like any User. It changes nothing the Owner
  can do, since the Owner acts in every Space, and `space show` lists them.
- **Names.** A new User's name must be free among Users and Spaces (the user
  area checks it). A Space's new name, on `space.add` or `space.rename`, is
  checked against Spaces only: `name_taken`.
- **A Personal Space** is never renamed, removed or given Members while its
  User exists, the Owner's included: `personal_space`. `space.member.remove`
  on one changes nothing, as it has no Members.
- **Members.** `space.member.add` and `space.member.remove` take one User or
  more, all or none: a name no User holds is refused with `unknown_user` and
  changes nobody. A User already a Member stays one, and a User who is not a
  Member stays out, so the call states the Members after it and a retry is
  harmless. Both answer the Space with its Members, in `space.show`'s shape.
  An invited User who has not joined can be made a Member.
- **Removing.** `space.remove` refuses a Space that holds any record, with
  `space_not_empty` naming what is left, and otherwise removes it with its
  memberships, answering the Space as it was.

## What a Space holds

`space show` counts, and `space rm` refuses a Space that is not empty, by
asking each area through one small interface: a
[`space.Holding`](../../internal/server/space/space.go), a function that sets
the area's own fields of `api.SpaceCounts` for one Space. The space area
never reads another area's records. `space show` and `space rm` read the
same counts, so a record `space rm` must keep a Space for but `space show`
should count apart takes a field of its own: `hosts` counts the Hosts that
are not Retired, as ordinary lists show them, and `retired_hosts` the
Retired ones ([`host.Holding`](../../internal/server/host/host.go)).

An area whose records live in a Space (Hosts, #47; Secrets, #48) writes its
`Holding` in its own package and adds one line to `holdings` in
[`server.go`](../../internal/server/server.go). `secrets` counts a Space's
Secrets ([`secret.Holding`](../../internal/server/secret/secret.go)); `space
rm` refuses while any count is above 0, and its message names each, as in
"2 Hosts and 1 Retired Host".

## The access matrix

[`internal/server/access/matrix_test.go`](../../internal/server/access/matrix_test.go)
calls every API operation as each kind of caller, over a real Device
Connection or the Unix socket, and checks the answer the operation's row
states.

- **The callers**, each acting in a target Space of a fresh koko server: the
  Owner through a Device Session and the Local caller, in the shared Space
  `ops`; a Member of `ops`; a Member in their own Personal Space; and a
  Member outside `ops`.
- **The answers:** `allowed` (the call succeeds), `ownerOnly`, `localOnly`,
  `unknown` (byte for byte the answer to the same call on a koko server where
  the target Space was never recorded), and `allowedBut(code)` for a call the
  access check lets through and the operation refuses by a rule of its own.
- **Rows.** Each area keeps its rows in `matrix_<area>_test.go` and adds one
  line to `areaRows`. A row gives the operation, its request for a target
  Space's name, and every caller's answer. A row whose call needs a record,
  such as a Host for `host.show`, gives a `setup` that records it into the
  target Space as the Local caller; it runs only where that Space is
  recorded, so the twin of an `unknown` answer holds nothing of it. An
  operation in `api.Operations`
  without a row, or a row without an answer for every caller, fails
  `TestEveryOperationHasRowsInTheAccessMatrix`; and
  [`TestTheServerServesExactlyTheDeclaredOperations`](../../internal/server/operations_test.go)
  fails when the koko server registers an operation `api.Operations` does not
  list (`op.API.Names`), so no registered operation escapes the matrix.
- **A Revoked Device Session** gets nothing from any operation: each answers
  `revoked`, or `local_only` for an operation only the Local caller makes.
  The transport refuses a Revoked key before any call, so no Device
  Connection can carry the matrix's call for it; only a call already on its
  way when the revocation commits reaches the operations.
  [`TestARevokedDeviceSessionIsRefusedByEveryOperation`](../../internal/server/revoked_test.go)
  therefore calls every registered operation as that caller beneath the
  transport, so a new operation that does not ask who is calling fails it.
  A Removed User's Device Session joins with #50.

## Verifying

- The matrix above, and its Revoked Device Session in
  [`revoked_test.go`](../../internal/server/revoked_test.go).
- [`internal/client/cli/space_test.go`](../../internal/client/cli/space_test.go):
  every `space` command over the CLI, with its goldens; what a Member lists
  and shows over their own Device Connection; a membership change holding
  from the next call; unseen equals unknown; the Owner-only refusals.
- [`internal/server/space/space_test.go`](../../internal/server/space/space_test.go):
  counts and the `space_not_empty` refusal through stand-in Holdings.
