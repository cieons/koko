# The access check and Spaces

Who a call acts as, what it may do, and the Spaces and memberships that
decide it. The rule is [ADR 0011](../adr/0011-access-is-space-membership.md),
[ADR 0008](../adr/0008-local-callers-act-as-the-owner.md) and, for the
Administrators, [ADR 0056](../adr/0056-one-owner-and-administrators-who-act-on-everything-but-the-owner-and-each-other.md); the source is
[`internal/server/access`](../../internal/server/access/access.go) and
[`internal/server/space`](../../internal/server/space/space.go).

## The four questions

[`access`](../../internal/server/access/access.go) is the one place that
decides access. No other package reads roles or memberships to decide what a
caller may do. A User's role, as the API answers it, is one SQL expression,
[`access.Role`](../../internal/server/access/access.go): `owner` for the
Owner, `administrator` for a User with a row in `administrators`, and
`member` for every other, a Removed User among them. `access.Who`,
`user.ByName`, `user.list` and every other read of a User select it, so no
other package reads `administrators`. Every answer is read from the current records, inside the
call's transaction, so a change (a membership, a revocation) holds from the
next call, and a live SSH Session lasts only while a new one would be allowed
([the Gateway](gateway.md#access-during-a-live-ssh-session)). Every
operation asks who is calling, even one every caller may make, such as
`server.show`, so a Revoked Device Session is refused
([revoking](device-connection.md#revoking)).

| Question | Answer |
| --- | --- |
| Who is calling | `access.Who(ctx, tx, caller)`: an `Identity`, the User and the Device Session or the Sign-in the call came on, or the Owner with neither for the Local caller. A Revoked Device Session is refused with `revoked`; so is a Removed User's, since Removing revokes each of theirs ([Users](users.md#removing)). An ended Sign-in, a Removed User's among them, is refused with `signin_required` ([Sign-ins](signin.md)). `Identity.By` names the caller in log lines |
| Is this caller the Owner, or an Administrator | `Identity.IsOwner()`, `Identity.IsAdministrator()`, and `Identity.Administers()` for either; `access.Administers(ctx, tx, caller, does)` also refuses anyone else with `owner_only`, "only the Owner or an Administrator `<does>`", and `access.OwnerOnly(ctx, tx, caller, does)` anyone but the Owner, "only the Owner `<does>`" |
| May this caller act on this User | `Identity.MayActOn(target, does)`, the one rule for a call that acts on a User's role or credentials: anyone acts on themselves, the Owner on anyone, and an Administrator on any Member. Anything else is refused with `owner_only`, "only the Owner `<does>` the Owner" or "… another Administrator" |
| Which Spaces can this caller act in | `Identity.Spaces(ctx, tx)`, the one place the rule is written: every Space, Personal Spaces included, for the Owner and the Administrators; for any other User their own Personal Space and each Space they are a Member of. A `Scope` whose `Has(spaceID)` filters lists and counts |
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
- **The gate before anything else.** An operation only the Owner or an
  Administrator may make asks `access.Administers` before it reads anything
  the call names, so a Member gets the same `owner_only` for a name that is
  taken, free, unseen or unknown, and learns nothing about the records.
  `space.add`, `space.rename`, `space.remove`, `space.member.add`,
  `space.member.remove`, `user.invite`, `user.list`, `user.remove`,
  `password.link`, `password.remove`, `server.check_update` and
  `server.upgrade` do so. `user.promote` and `user.demote` ask
  `access.OwnerOnly` the same way ([Users](users.md#administrators)).
  `server.set` stays the Local caller's alone (`local_only`), the Owner's
  devices included. `device.add` is anyone's for their own User; naming
  another User is refused with `owner_only` before that User is looked up,
  unless the caller `Administers()`.
- **Acting on another User.** `Identity.MayActOn` is asked once the User a
  call acts on is read, which reveals nothing, since an Administrator lists
  every User and their role. It governs `user.remove`, `device.add` naming a
  User, `device.revoke` and `signin.revoke` of another User's,
  `password.link` and `password.remove`. `device.revoke` asks it before it
  reads the device. `user.remove` refuses the Owner with
  `owner_not_removable` first, whoever calls, and an Administrator's own
  Removal with `owner_only`, as only the Owner removes an Administrator.
- **Reading is not acting.** `device.list`, `signin.list` and `user.list`
  answer an Administrator every User's records, the Owner's included, as
  they answer the Owner. A Member naming another User's Device Sessions is
  refused with `owner_only`, and gets no Sign-ins of another User's.

## Spaces and memberships

- A Space is shared, or a User's Personal Space, which `spaces.user_id`
  names; that is not a membership. `api.Space.personal` and the CLI's
  `personal` say which: a Space is Personal exactly while `user_id` is set.
  Removing a User turns their Personal Space into an ordinary shared Space
  with no Members by clearing it ([Users](users.md#removing)), so only the
  Owner and the Administrators see it, and can rename it, give it Members,
  move its records out and remove it once empty.
- A membership is a row of `memberships` (a Space and a User, no role). It
  goes with its Space when the Space is removed, and with its User when a
  User who never joined is deleted (`ON DELETE CASCADE`).
- The Owner, or an Administrator, can be made a Member like any User. It
  changes nothing they can do, since they act in every Space, and `space
  show` lists them. An Administrator demoted keeps their memberships.
- **Names.** A new User's name must be free among Users and Spaces (the user
  area checks it). A Space's new name, on `space.add` or `space.rename`, is
  checked against Spaces only: `name_taken`.
- **A Personal Space** is never renamed, removed or given Members while its
  User exists, the Owner's included: `personal_space`. `space.member.remove`
  on one changes nothing, as it has no Members.
- **Members.** `space.member.add` and `space.member.remove` take one User or
  more, all or none: a name no User holds is refused with `unknown_user`, and
  `space.member.add` refuses a Removed User with `user_removed`; either
  refusal changes nobody. A Removed User is a Member of no Space, so
  `space.member.remove` leaves them out like any User who is not a Member. A User already a Member stays one, and a User who is not a
  Member stays out, so the call states the Members after it and a retry is
  harmless. Both answer the Space with its Members, in `space.show`'s shape.
  An invited User who has not joined can be made a Member. Taking a User out
  ends their live SSH Sessions to the Space's Hosts
  ([the Gateway](gateway.md#access-during-a-live-ssh-session)).
- **Listing.** `space.list` answers every Space the caller can act in, by
  name, each in `space.show`'s shape, with its Members and counts: a Member
  sees the Members of the Spaces they can act in and nothing of any other,
  as `space.show` shows them.
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

## The access-change signal

A change that can withdraw access announces it on the access-change signal
([`access.Signal`](../../internal/server/access/signal.go)) once its
transaction commits ([revoking](device-connection.md#revoking)).
`access.Change` names what it affects. The Gateway ends at once, or checks
again, the SSH Sessions it touches, Tunnels and web terminals alike
([the Gateway](gateway.md#access-during-a-live-ssh-session)); the
transport closes the Revoked Device Sessions' Device Connections; and the
web front ends their `koko web` cookies.

| Field | What it names | Published by |
| --- | --- | --- |
| `Revoked`, `Removed` | The Device Sessions the change Revoked, and the id of the User it Removed, whose they were | `device.revoke`, `user.remove` |
| `SignIns`, `SignInEnd` | The Sign-ins the change ended, and why, as their records say: `signed_out`, `expired`, `revoked`, `password_changed`, `password_removed` or `user_removed` | signing out, the web front finding one expired as its cookie comes back or the Gateway as its page is open, `signin.revoke`, a Password set or changed (the User's other Sign-ins), `password.remove`, `user.remove` ([Sign-ins](signin.md#sign-ins)) |
| `Narrowed` | The ids of the Users taken out of a Space, or demoted from Administrator | `space.member.remove`, `user.demote` |
| `Hosts` | The Hosts the change may have put out of a caller's reach | `host.move`, `host.retire`, `host.set` sending a Host back to Registered |
| `WebURL` | The Web URL set to another address or cleared, which signs out every page on the web listener | `server.set` |

`Narrowed` and `Removed` name Users, not callers: a web terminal's SSH
Session is its User's, whoever opened it, so a Sign-in that has ended
since must not keep it from being checked. A Sign-in's expiry is published
only once its cookie comes back; until then the Gateway finds it in a sweep
([the Gateway](gateway.md#access-during-a-live-ssh-session)).

## The access matrix

[`internal/server/access/matrix_test.go`](../../internal/server/access/matrix_test.go)
calls every API operation as each kind of caller, over a real Device
Connection or the Unix socket, and checks the answer the operation's row
states.

- **The callers**, each acting in a target Space of a koko server of its
  own: the Owner through a Device Session and the Local caller, in the
  shared Space `ops`; a Member of `ops`; a Member in their own Personal
  Space; a Member outside `ops`; the Owner and a Member of `ops` through
  a Sign-in, calling on the web listener as a browser does (`h.SignIn`);
  and an Administrator, eve, who is a Member of no shared Space, through a
  Device Session and through a Sign-in, in `ops`. Every row gives each
  caller its answer explicitly. The records hold a second Administrator,
  frank, and a Sign-in each of the Owner and frank, for the rows that name
  them; a row whose request needs such a record's id gives `requestIn`,
  which reads the fixture.
  Each call's koko server starts on a copy of a template: the data
  directory of a koko server stopped once it held the matrix's Users, `ops`
  unless for a twin, and the caller's Sign-in. So the matrix records them,
  and hashes a Sign-in's Password, once per template rather than once per
  call (#186).
- **The answers:** `allowed` (the call succeeds), `ownerOnly` (the code
  `owner_only`, whether only the Owner or only the Owner or an
  Administrator may make the call), `localOnly`,
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
- **A Removed User's Device Session** gets the same answers, since Removing
  revoked it ([Users](users.md#removing)), and for the same reason is called
  beneath the transport, by
  [`TestARemovedUsersDeviceSessionIsRefusedByEveryOperation`](../../internal/server/revoked_test.go).
- **An ended Sign-in** gets nothing either: each operation answers
  `signin_required`, or `local_only`. The web front refuses its cookie
  before any call, so
  [`TestAnEndedSignInIsRefusedByEveryOperation`](../../internal/server/revoked_test.go)
  calls every registered operation as that caller beneath it.

## The limit: the koko server's own machine

The Unix socket is open to every local account, and its caller is the
Owner ([ADR 0008](../adr/0008-local-callers-act-as-the-owner.md)). An
Administrator acts in every Space, so when the koko server's own machine, or
the machine running its container, is a Managed Host, an Administrator can
open an SSH Session to it and call koko there as the Owner, acting on the
Owner and on other Administrators after all. koko does not prevent it, as it
does not stop anyone with a shell there: an Owner who makes Administrators
keeps that machine out of koko's record, or accepts that an Administrator
who reaches it is the Owner's equal ([ADR 0056](../adr/0056-one-owner-and-administrators-who-act-on-everything-but-the-owner-and-each-other.md)).

## Verifying

- The matrix above, and its Revoked and Removed User's Device Sessions and
  its ended Sign-in in
  [`revoked_test.go`](../../internal/server/revoked_test.go).
- [`internal/client/cli/space_test.go`](../../internal/client/cli/space_test.go):
  every `space` command over the CLI, with its goldens; what a Member lists,
  with each Space's Members and counts, and shows over their own Device
  Connection; a membership change holding
  from the next call; unseen equals unknown; the Owner-only refusals.
- [`internal/client/cli/user_admin_test.go`](../../internal/client/cli/user_admin_test.go):
  an Administrator's acts on Members, each act on the Owner or another
  Administrator refused with `owner_only` and changing nothing, and an
  Administrator reading every record, the Owner's Personal Space's Hosts
  and Secrets included ([Users](users.md#administrators)).
- [`internal/server/space/space_test.go`](../../internal/server/space/space_test.go):
  counts and the `space_not_empty` refusal through stand-in Holdings;
  `space.list` answering each Space as `space.show` does, and a Member's
  answering only their own Spaces, with their Members.
