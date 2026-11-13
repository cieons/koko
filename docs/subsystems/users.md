# Users

The people who belong to a koko server: exactly one Owner, any number of
Administrators, and Members ([ADR 0056](../adr/0056-one-owner-and-administrators-who-act-on-everything-but-the-owner-and-each-other.md)). How they are listed, how
the Owner makes a Member an Administrator and back, and how `koko user
remove` deletes or Removes one. The
source is [`internal/server/user`](../../internal/server/user/user.go); how a
User is invited is in [Join Codes](join-codes.md#issuing), and who may act
where is in [the access check](access.md).

## The record

| Table | What it holds |
| --- | --- |
| `users` | One row per User: their id (`user_...`), their name, which never changes, their stored role (`owner` for exactly one, kept so by the index `users_one_owner`, and `member` for every other), and their created and Removed times. A Removed User keeps their row for ever, so their name is never used again. |
| `administrators` | One row per Administrator, a `member` in `users`: their User's id, when they were promoted and the Device Session's or Sign-in's id that promoted them, `NULL` for the Unix socket. The Owner never has one. It goes with its User when a User who never used koko is deleted (`ON DELETE CASCADE`), and with the role when they are demoted or Removed. |

The API's `role` is `owner`, `administrator` for a User with a row in
`administrators`, or `member`, computed in one place,
[`access.Role`](../../internal/server/access/access.go), which every read
of a User selects ([the access check](access.md#the-four-questions)).

Every User has a Personal Space, named after them, from the moment they are
recorded (`spaces.user_id`; [Spaces and memberships](access.md#spaces-and-memberships)).
A User **never used koko** while they have never had a Device Session,
Revoked ones included, never set a Password and never had a Sign-in
([Sign-ins](signin.md)).

## Listing

`koko user ls [--all]` (`user.list`, the Owner or an Administrator, read
tier) lists the Users by name, each with their role (`owner`,
`administrator` or `member`; a Removed User is a `member`), created time and when they set their
Password (`PASSWORD`, `-` for none); `--all` adds the Removed ones, whose
`removed_at` says when. A User deleted is listed by neither. Its `--json`
is a bare array of `{"id", "name", "role", "created_at", "removed_at",
"password_set_at"}`, `removed_at` being `null` while the User belongs to
the koko server and `password_set_at` while they have no Password; `user
invite` prints a User in the same shape, `password_set_at` `null`.

## Administrators

`koko user promote <user>` (`user.promote`, irreversible tier, since it
hands over every Space and Secret, which no demotion takes back) makes a
Member an Administrator, and `koko user demote <user>` (`user.demote`,
reversible tier) makes an Administrator a Member again. Both take
`{"name"}` and answer the User in `user ls`'s shape, its `--json` a bare
object.

- **Who.** The Owner alone (`access.OwnerOnly`), from a device, a browser
  Sign-in or the Unix socket. A Member or an Administrator, the
  Administrator to be demoted included, is refused with `owner_only` before
  the name is read.
- **Refusals.** A name no User holds is refused with `unknown_user`, a
  Removed User with `user_removed`, and the Owner, named in either call,
  with `owner_not_removable`: "alice is the Owner, whose role never
  changes".
- **A retry.** Promoting an Administrator, or demoting a Member, changes
  nothing and logs nothing, and answers the User as they are. An invited
  User who has not joined can be promoted.
- **Promoting**, in one transaction, adds the row and voids the User's
  unused Join Codes and Password Links issued by anyone but the Owner and
  the User themselves: those an Administrator issued, so a code an
  Administrator prepared for a Member never becomes an Administrator's. A
  code issued over the Unix socket, or by the Owner's or the User's own
  Device Session or Sign-in, stays. It publishes nothing, since it only
  widens access. A device or Sign-in the User already has stays, since koko
  cannot tell their own from one an Administrator joined with their code:
  read `koko device ls --user <user>`, each device with who added it, and
  `koko signin ls --user <user>` before promoting.
- **Demoting**, in one transaction, deletes the row and voids the unused
  Join Codes and Password Links the Administrator issued for other Users,
  from any of their Device Sessions or Sign-ins; those they issued for
  themselves stay. Once it commits, the access-change signal names their
  live Device Sessions and Sign-ins in `Narrowed`, so their SSH Sessions and
  web terminals to Hosts outside the Spaces they still act in end, with
  `access withdrawn` ([the Gateway](gateway.md#access-during-a-live-ssh-session)).
  Their memberships stay.
- **The Owner stays.** The Local caller acts as the Owner, the one `owner`
  row ([ADR 0008](../adr/0008-local-callers-act-as-the-owner.md)): `koko
  device add` on the koko server's machine issues for the Owner, and with
  `--user` for any User, an Administrator included, which is the way back
  for an Administrator who has lost every device. `KOKO_OWNER` and the
  Owner's last Device Session confirmation (`--last`) stay the Owner's; an
  Administrator's own last device needs no `--last`.

## Removing

`koko user remove <user>` (`user.remove`, the Owner or an Administrator,
irreversible tier) takes the least permanent outcome the records allow, and
says which: it deletes a User who never used koko, and Removes anyone else.
A Member is refused with `owner_only` before the name is read. Only the
Owner removes an Administrator: an Administrator naming another, or
themselves, since none steps down by themselves, is refused with
`owner_only` ([`Identity.MayActOn`](access.md#the-four-questions)).
The Owner Removes an Administrator directly, with no demotion first.

- **The Owner** is refused with `owner_not_removable`, before and after their
  first join, whoever asks.
- **A Removed User** is refused with `user_removed`: there is nothing left to
  remove.
- **Deleting** a User who never used koko removes their row, their Join
  Codes and Password Links (all unused) and their Personal Space, and their memberships go with the
  row (`ON DELETE CASCADE`); the name is free again. The Personal Space must
  hold nothing: while it holds a Host, a Retired Host or a Secret, the call is
  refused with `space_not_empty`, naming what is left
  ([`space.DeletePersonal`](../../internal/server/space/space.go)), and
  changes nothing.
- **Removing** anyone else, in the call's one transaction:
  1. revokes every live Device Session they hold, at the call's time;
  2. voids their valid Join Codes, and those their Device Sessions and
     Sign-ins issued;
  3. ends their live Sign-ins (`user_removed`), deletes their Password and
     voids their unused Password Links and those they issued for others
     ([Sign-ins](signin.md#users));
  4. deletes an Administrator's row, so a Removed User is a `member`;
  5. takes them out of every Space, and turns their Personal Space into an
     ordinary shared Space with no Members by clearing `spaces.user_id`
     ([`space.PassToAdministrators`](../../internal/server/space/space.go)):
     it keeps its name and what it holds, only the Owner and the
     Administrators see it, and they can rename it, give it Members, move
     its Hosts and Secrets out and remove it once it is empty;
  6. marks them Removed, keeping the row.

  Once it commits, the access-change signal names the Device Sessions it
  Revoked, as a Removed User's, so their live SSH Sessions end, each with
  a line naming the removal
  ([the Gateway](gateway.md#access-during-a-live-ssh-session)), and their
  Device Connections then close ([revoking](device-connection.md#revoking));
  it also names the Sign-ins it ended, whose web terminals end alike;
  a later consumer ends their `koko web` cookies through the same Device
  Sessions. Who
  is calling then refuses each of them with `revoked`, by its device's name,
  so a Removed User's devices learn of it as of any revocation
  ([on the device](device-connection.md#on-the-device)).

The `--json` is `{"outcome", "user"}`: `outcome` is `"deleted"` or
`"removed"`, and `user` is the User as the call left them, in `user ls`'s
shape, with `removed_at` set once Removed.

**After Removing.** A Removed User's name stays theirs: `user invite` refuses
it with `name_taken`, even once their former Personal Space is renamed. The
Owner issues them no Join Code: `device add --user` looks them up through
[`user.Active`](../../internal/server/user/user.go), which refuses them with
`user_removed`. `space member add` refuses them with `user_removed` too,
adding nobody; `space member rm` leaves them out, as they are a Member of no
Space. [`user.ByName`](../../internal/server/user/user.go) finds a Removed
User like any other, as `device ls --user` does to list their Revoked Device
Sessions with `--all`.

## Logging

Removing logs, for each Device Session it Revoked, `device revoked` with
the User, the device, the Device Session and `revoked_by`, as revoking one
does ([logging](device-connection.md#logging)) but without
`join_codes_voided`, which the next line counts for them all; then `user
removed` with the
User, their id, the `role` they held (`member` or `administrator`), how
many Device Sessions it Revoked
(`device_sessions_revoked`), `join_codes_voided`, `signins_ended`,
`password_links_voided` and `removed_by`, after a `sign-in ended` line for
each Sign-in it ended. `revoked_by` and `removed_by` are a Device Session's
or a Sign-in's id, or `unix socket`. Deleting logs
`user deleted` with the User, their id and `deleted_by`. Promoting logs
`user promoted` with `user`, `user_id`, `promoted_by`, `join_codes_voided`
and `password_links_voided`, and demoting `user demoted` with `user`,
`user_id`, `demoted_by`, `join_codes_voided` and `password_links_voided`;
a retry that changes nothing logs nothing. An Administrator's acts are told
from the Owner's by the `*_by` id's User, which `koko device ls --all` and
`koko signin ls --all` name. Each line is written once the call's
transaction commits.

## Verifying

- [`user_admin_test.go`](../../internal/client/cli/user_admin_test.go):
  `koko user promote` and `demote` with their goldens, the retry and every
  refusal; an Administrator's acts on Members and the refusal of each on
  the Owner or another Administrator; an Administrator reading every
  record; promotion and demotion voiding what they should and keeping the
  rest; demotion ending a `koko exec` to the Owner's Personal Space and
  keeping one to a Space the User is a Member of; the Owner Removing an
  Administrator directly; and the Local caller issuing for the Owner and
  for an Administrator.

- [`user_remove_test.go`](../../internal/client/cli/user_remove_test.go):
  deleting a User who never joined and what goes with them, the refusal while
  their Personal Space holds a Host, Removing a User who joined (their
  devices refused by name, their open Device Connection closed, their codes
  voided, their Device Sessions kept Revoked, their memberships gone, the log
  lines), the former Personal Space in the Owner's hands, the refusals of a
  Removed User and `space member rm` leaving them out, the Owner never
  removed, and the output for people; with their goldens.
- [`user_test.go`](../../internal/client/cli/user_test.go): `koko user ls`
  with and without `--all`, for people, and the Owner-only refusals;
  [`signin_test.go`](../../internal/client/cli/signin_test.go): its
  `password_set_at`.
- [`internal/server/web/signin_test.go`](../../internal/server/web/signin_test.go):
  Removing a User who only set a Password, ending their Sign-in and counting
  it, and deleting one invited with `--web` who never set it, with their
  link.
- [`revoked_test.go`](../../internal/server/revoked_test.go): a Removed
  User's Device Session refused by every operation, the access matrix's
  caller for it ([the access matrix](access.md#the-access-matrix)).
