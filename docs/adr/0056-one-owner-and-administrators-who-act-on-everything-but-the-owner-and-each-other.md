# One Owner, and Administrators who act on everything but the Owner and each other

A koko server keeps exactly one Owner and may have any number of
Administrators, the Owner's choice of 2026-11-13, so that a fully trusted
person can run the koko server with the Owner without sharing the Owner's
identity. An Administrator does everything the Owner does except act on the
Owner or on another Administrator, and except change who the Owner is. They
see and act in every Space, Personal Spaces included, the Owner's among
them. Every other User stays a Member, whose access is Space membership
alone.

- **Making and unmaking.** Only the Owner makes a Member an Administrator
  (`koko user promote`, irreversible tier, since it hands over every Secret
  and no demotion takes back what was seen) and makes an Administrator a
  Member again (`koko user demote`, reversible tier), from a device, a
  browser Sign-in or the koko server's machine. An Administrator does not
  demote themselves.
- **No acting on each other.** An Administrator may not demote, Remove,
  revoke the devices or Sign-ins of, remove the Password of, or issue a
  Join Code or Password Link for the Owner or another Administrator. They
  act on Members and on their own credentials. Reading is not acting: they
  list every User, every Device Session and every Sign-in.
- **The Owner stays one.** The Owner is named at initialisation, never
  Removed and never demoted. The Local caller acts as the Owner (ADR 0008),
  and the container's `KOKO_OWNER` (ADR 0038) and the confirmation for the
  Owner's last Device Session keep their meaning.
- **The record.** `users.role` stays `owner` or `member`, the Owner being
  one. An Administrator is a Member with a row in a table of its own,
  `administrators`, which records when and by whom they were promoted. The
  API's `role` reads `"administrator"` for them, computed in the access
  check alone.
- **Promotion and demotion void what the other side prepared.** Promotion
  voids the User's unused Join Codes and Password Links issued by anyone
  but the Owner and the User themselves, so an Administrator cannot keep a
  code that becomes an Administrator's. Demotion voids the unused ones the
  Administrator issued for other Users, and ends their SSH Sessions to
  Hosts outside the Spaces they still act in.

This refines ADR 0011, where the Owner alone sees every Space: the Owner
and the Administrators do, and access stays Space membership for everyone
else. It refines ADR 0043, where the Owner alone issues Password Links: the
Owner or an Administrator does, each for whom they may act on. ADR 0008
stays as it is.

## Considered Options

- **Several Owners, as equals:** nothing new on the wire and only the
  one-Owner index dropped, but a stolen credential of any of them could
  demote, Remove or lock out the others, and the Local caller,
  `KOKO_OWNER` and the last Device Session rule would each need a rule for
  which Owner.
- **A third value of `users.role`:** one column for the role, but widening
  its CHECK needs the table rebuilt, and koko runs each migration step in
  one transaction with foreign keys on. There `DROP TABLE users` fails on
  the tables that reference it, or, without them, deletes every membership
  by cascade. Editing the CHECK in place through `PRAGMA writable_schema`
  changes the schema's text by hand, a risk not worth one more role.
- **An Administrator who sees no Personal Space but their own:** the
  Owner's Secrets would stay the Owner's, but an Administrator manages
  every Member's credentials and so could become any Member, and the Owner
  chose to share every Space.
- **Administrators who may act on each other,** with the Owner as the
  backstop: a stolen Administrator credential could revoke the others'
  devices and Sign-ins, demote them, or plant a device under their names.
- **The Owner acts on Administrators only from a device or the koko
  server's machine:** a phished Password could not promote an intruder, but
  a stolen Owner Sign-in can already issue a Join Code for the Owner's own
  account and read every Secret, so the rule would protect nothing and would
  take the Access page's purpose from an Owner who uses only a browser.

## Consequences

- An Administrator who reaches a shell on the koko server's machine acts
  as the Owner through its Unix socket, which is open to every local
  account (ADR 0008). Since an Administrator acts in every Space, that
  includes the koko server's machine, or the machine running its
  container, when it is a Managed Host. koko does not prevent it: an Owner
  who makes Administrators keeps that machine out of koko's record, or
  accepts that an Administrator who reaches it is the Owner's equal.
- A device or Sign-in a User already has stays when they are promoted:
  koko cannot tell their own device from one an Administrator joined with
  their code. The web UI's confirmation lists their live devices, each with
  who added it, and their live Sign-ins, for the Owner to read first.
- The codes `owner_only` and `owner_not_removable` keep their names. The
  first now also means "only the Owner or an Administrator" and "only the
  Owner acts on the Owner or an Administrator"; the second also covers a
  call that would change the Owner's role.
- A response's `role` takes a new value, `"administrator"`. No request
  changes, so no revision rises and `compat.Oldest` does not move;
  `user.promote` and `user.demote` are new operations, which an older koko
  server lacks, so a newer CLI fails them with `server_too_old`.
  `koko skill --json` gains `administrators` beside `owner_only`.
- A later server-wide role, such as a read-only auditor, sits beside
  `administrators` in a table nothing references, which a migration can
  rebuild freely. Roles inside a Space, Teams and conditions on a grant
  belong to memberships, a separate axis, and do not reuse the word
  Administrator.
