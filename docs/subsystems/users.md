# Users

The people who belong to a koko server: exactly one Owner, and Members. How
they are listed, and how `koko user remove` deletes or Removes one. The
source is [`internal/server/user`](../../internal/server/user/user.go); how a
User is invited is in [Join Codes](join-codes.md#issuing), and who may act
where is in [the access check](access.md).

## The record

| Table | What it holds |
| --- | --- |
| `users` | One row per User: their id (`user_...`), their name, which never changes, their role (`owner` for exactly one, `member` for every other), and their created and Removed times. A Removed User keeps their row for ever, so their name is never used again. |

Every User has a Personal Space, named after them, from the moment they are
recorded (`spaces.user_id`; [Spaces and memberships](access.md#spaces-and-memberships)).
A User **never used koko** while they have never had a Device Session,
Revoked ones included, never set a Password and never had a Sign-in
([Sign-ins](signin.md)).

## Listing

`koko user ls [--all]` (`user.list`, Owner only, read tier) lists the Users
by name, each with their role, created time and when they set their
Password (`PASSWORD`, `-` for none); `--all` adds the Removed ones, whose
`removed_at` says when. A User deleted is listed by neither. Its `--json`
is a bare array of `{"id", "name", "role", "created_at", "removed_at",
"password_set_at"}`, `removed_at` being `null` while the User belongs to
the koko server and `password_set_at` while they have no Password; `user
invite` prints a User in the same shape, `password_set_at` `null`.

## Removing

`koko user remove <user>` (`user.remove`, Owner only, irreversible tier)
takes the least permanent outcome the records allow, and says which: it
deletes a User who never used koko, and Removes anyone else. A Member is
refused with `owner_only` before the name is read.

- **The Owner** is refused with `owner_not_removable`, before and after their
  first join.
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
  2. voids their valid Join Codes, and those their Device Sessions issued;
  3. ends their live Sign-ins (`user_removed`), deletes their Password and
     voids their unused Password Links ([Sign-ins](signin.md#users));
  4. takes them out of every Space, and turns their Personal Space into an
     ordinary shared Space with no Members by clearing `spaces.user_id`
     ([`space.PassToOwner`](../../internal/server/space/space.go)): it keeps
     its name and what it holds, only the Owner sees it, and the Owner can
     rename it, give it Members, move its Hosts and Secrets out and remove
     it once it is empty;
  5. marks them Removed, keeping the row.

  Once it commits, the access-change signal names the Device Sessions it
  Revoked, as a Removed User's, so their live SSH Sessions end, each with
  a line naming the removal
  ([the Gateway](gateway.md#access-during-a-live-ssh-session)), and their
  Device Connections then close ([revoking](device-connection.md#revoking));
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
User, their id, how many Device Sessions it Revoked
(`device_sessions_revoked`), `join_codes_voided`, `signins_ended`,
`password_links_voided` and `removed_by`, after a `sign-in ended` line for
each Sign-in it ended. `revoked_by` and `removed_by` are a Device Session's
or a Sign-in's id, or `unix socket`. Deleting logs
`user deleted` with the User, their id and `deleted_by`. Each line is
written once the call's transaction commits.

## Verifying

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
