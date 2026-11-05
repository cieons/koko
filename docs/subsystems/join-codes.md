# Join Codes

How the koko server issues the one-time codes a device joins with, the
string they are written as, and what it keeps of them. Why devices join with
a Join Code is [ADR 0013](../adr/0013-devices-join-with-a-one-time-join-code.md);
the rules are P0 spec: people and Spaces.

## The string

The codec is [`joincode`](../../internal/shared/joincode/joincode.go), in the
shared group: pure, with no I/O, so the koko server encodes with it and
`koko join` decodes with it. A Join Code is `koko_` followed by these bytes in
lowercase RFC 4648 base32 (`a-z`, `2-7`) without padding, about 120
characters in all:

| Bytes | What |
| --- | --- |
| 1 | the format version, `1` |
| 1 | n, the length of the address |
| n | the Server Address as `host:port` in UTF-8, its port always written (an IPv6 host in brackets) |
| 32 | the SHA-256 of the koko server's host key in SSH wire format, the digest OpenSSH's `SHA256:` fingerprint shows |
| 16 | the secret, random |
| 4 | CRC-32 (IEEE, as zlib and PNG compute it) of every byte before it, big-endian |

`Code.Encode` refuses an address that is not `host:port` or is longer than
255 bytes. `Decode` takes the string exactly as printed and says why it
cannot read one, with one error per reason, checked in this order:

- `ErrPrefix`: it does not start with `koko_`;
- `ErrAlphabet`: after the prefix it holds a character other than `a-z` and
  `2-7` (uppercase, padding and white space included; a caller trims a pasted
  line first);
- `ErrChecksum`: it is cut short or damaged, so its length is not one base32
  writes or the checksum does not match;
- `ErrVersion`: the checksum holds and the version is not `1`;
- `ErrMalformed`: the checksum holds but the parts do not fit (the length
  byte, or an address that is not `host:port`), which only a code made by
  hand reaches.

Every format version keeps the same trailing CRC-32, so the checksum is
checked before the version and a damaged code is never taken for a newer
one. No error quotes the string, since a damaged code still holds most of its
secret, and a `Code` prints as its address alone.

## Issuing

[`device.Issuer.Issue`](../../internal/server/device/device.go) is the one
way a Join Code is issued: `device.add`, `user.invite` and `server.init` all
call it inside their own transaction. It:

1. finds the User: the one named, or the caller's own, which is its Device
   Session's User, or the Owner for the Local caller (ADR 0008,
   [`access.Who`](../../internal/server/access/access.go)). Only the Owner
   names another User: anyone else is refused with `owner_only` (403) before
   the name is looked up, so the refusal says nothing of who exists. A name
   that is not a User name is refused with `invalid_name`, and one no User
   holds with `unknown_user` (404);
2. binds the device name given, or `device-<n>`, where n counts up from one
   past the number of Device Sessions the User has ever had, Revoked ones
   included, past every name taken. A name is taken while one of that
   User's live Device Sessions or valid Join Codes holds it, so two live
   codes never collide; a name given
   is checked as a lowercase DNS label (`invalid_name`) and refused when
   taken (`name_taken`);
3. reads what the code names inside the transaction, so that the first code
   names what initialisation made in the same transaction: the Server
   Address, with the port the koko server listens on written when it names
   none, and the host key's fingerprint, opened with the Root Key
   ([`joinTarget`](../../internal/server/jointarget.go)). A Server Address
   too long for a Join Code with its port is refused with `invalid_address`,
   which also refuses such an address at initialisation;
4. makes a 16-byte secret with `crypto/rand`, records the code, and returns
   it once, as `{"id", "code", "user", "device", "expires_at"}`.

A code is valid for 15 minutes (`joincode.Lifetime`, a constant) from the
call's time, `op.Now`. Several codes may be valid at once. The issuer is
recorded: the caller's Device Session, or the Unix socket; it becomes the
"added by" of the Device Session the code joins
([the Device Connection](device-connection.md)).

`koko device add [<device>] [--user <user>]` and `koko user invite <user>`
print the code ([the CLI](cli.md#the-commands-so-far)); both are in the
irreversible tier, since they print a credential (ADR 0019).
`user.invite` creates the Member and their Personal Space
([`user.Register`](../../internal/server/user/user.go)), refusing a name any
User or any Space holds, then issues their first code, in one transaction.

## The record

| Table | What it holds |
| --- | --- |
| `join_codes` | One row per code issued: its id (`joincode_...`), its User, the hash of its secret, its device name, its issuer (NULL for the Unix socket, else the issuing Device Session's id), its expiry, and the times it was used or voided. A code is valid while it is neither used nor voided and has not expired; a join marks it used, and revoking its issuer voids it while it is valid ([revoking](device-connection.md#revoking)). Spent codes are kept, for the P1 audit log. |

The koko server keeps neither the code nor its secret, only
`SHA-256("koko Join Code secret\0" ‖ secret)`, unique per row, which joining
looks up. The secret's 128 random bits make a slow hash needless. Nothing
logs a code: the API logs a call's path and status only, and a join its Join
Code's id.

## Verifying

- [`joincode_test.go`](../../internal/shared/joincode/joincode_test.go):
  a known string worked out independently (Python's `base64` and `zlib`),
  round trips, the addresses a code cannot carry, every reason `Decode`
  gives, and that printing a code hides its secret.
- [`device_test.go`](../../internal/client/cli/device_test.go), through
  `koko device add` against the in-process koko server: the code for the
  Owner and what it names, `device-<n>` past taken names, a name held until
  the code expires at exactly 15 minutes (the clock moved, not waited for),
  names held by live Device Sessions, the refusals, a joined Member issuing
  for themselves only (`owner_only`), and that after issuing and joining
  neither `koko.db` and its `-wal` and `-shm`, while running and once
  stopped, nor the log holds a code or its secret in any common encoding.
- [`user_test.go`](../../internal/client/cli/user_test.go): `koko user
  invite`, its refusals, names per User, and an expired Invite that leaves
  its User to join with a new code; the one transaction is
  [`user/user_test.go`](../../internal/server/user/user_test.go).
- [`init_test.go`](../../internal/server/init_test.go): initialisation's
  first code, and a Server Address no code carries.
