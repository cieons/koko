# Passwords, Password Links and Sign-ins

How a browser signs in to the web UI on the web listener, at the Web URL
([ADR 0043](../adr/0043-a-browser-signs-in-with-a-password-set-through-a-password-link.md)):
each User's one Password, the Password Links the Owner issues to set it,
the Sign-ins, the web front's `/auth/` endpoints, the rate limits, and what
a Sign-in may do. The source is
[`internal/server/signin`](../../internal/server/signin/signin.go), with the
`/auth/` endpoints and the cookie in
[`internal/server/web/auth.go`](../../internal/server/web/auth.go) and the
Sign-in caller in [`access.Who`](../../internal/server/access/access.go).
The web listener itself, its Host and Origin checks and its headers are in
[the web front](web-front.md#the-web-listener).

## The record

One migration step, [`signins.sql`](../../internal/server/store/migrations/signins.sql):

| Table | What it holds |
| --- | --- |
| `passwords` | A User's one Password, or no row: its id (`password_…`), the User, its Argon2id hash as a PHC string sealed under the Root Key for the row's id ([the Seal](seal.md)), when it was set and how (`link` or `change`). Setting a Password writes a new row. |
| `password_links` | Each Password Link: its id (`passwordlink_…`), the User, the SHA-256 of its secret, what issued it (a Device Session's or a Sign-in's id, or NULL for the Unix socket), when it was issued, expires, was used and was voided. Spent links are kept. |
| `signins` | Each Sign-in: its id (`signin_…`), the User, the SHA-256 of its cookie's token, the client label, the client's address at sign-in and when last used, its start, its Last used time, its expiry 30 days on, and once it ends, when, why and by whom. Ended Sign-ins are kept, as Revoked Device Sessions are. |

## Passwords

- **The rule** ([`checkPassword`](../../internal/server/signin/signin.go)):
  15 to 128 Unicode code points after NFC normalisation, any characters, no
  composition rule and no expiry, and not the User's own name; anything else
  is refused with `invalid_password`. The NFC form is what is hashed, and a
  sign-in normalises what it is given the same way.
- **The hash** ([`hash.go`](../../internal/server/signin/hash.go)): Argon2id
  from `golang.org/x/crypto/argon2` with 19 MiB of memory, 2 passes, 1
  lane, a 16-byte random salt and a 32-byte key, kept as a PHC string
  (`$argon2id$v=19$m=19456,t=2,p=1$<salt>$<key>`, unpadded base64). A
  sign-in whose hash has other parameters rehashes it with these. At most
  two hashes run at once in the process; a request waits for its turn.
- **Comparison** is constant-time. A sign-in naming no User, a Removed User
  or a User without a Password still hashes once against a dummy hash, so
  its timing tells nothing.
- **Setting a Password**, through a link or by the current one, ends every
  other Sign-in of the User (`password_changed`), voids their unused
  Password Links, and starts their name's count of failed sign-ins again.
- **`password.remove`** (`koko password rm <user>`, Owner only, irreversible
  tier) deletes the Password, ends the User's Sign-ins (`password_removed`)
  and voids their unused link, keeping the User and their devices. A User
  without one is refused with `no_password` (409), changing nothing.

## Password Links

- **What one is**: the Web URL followed by `/password#<secret>`, the secret
  128 random bits as 26 base32 characters (`crypto/rand.Text`), kept only as
  its SHA-256. It sits in the fragment, which no browser sends, so it reaches
  no proxy log and no `Referer`.
- **Issuing** ([`Service.IssueLink`](../../internal/server/signin/signin.go)),
  by the Owner alone, for any User not Removed, the Owner included:
  `user.invite` with `web: true` (`koko user invite <name> --web`) creates
  the User with a link instead of a Join Code; `password.link` (`koko
  password link <user>`) issues one for an existing User. A Member is
  refused with `owner_only` before the name is read. Both take `valid_for`
  in seconds, 60 to 604,800 (`api.Validity`, 15 minutes by default), and
  are refused with `web_url_unset` (409) while the Web URL is not set.
- **One live link per User**: issuing one voids the User's earlier unused
  one, and Removing a User voids theirs.
- **Spending one**: `POST /auth/link` answers whom an unspent link is for,
  spending nothing; `POST /auth/password` with the link spends it, sets the
  Password and signs this browser in. A link used already is refused with
  `link_used` (410); an expired, voided, unknown or Removed User's one with
  `link_invalid` (404). Each refusal counts as a failed attempt for the
  client's address.

## Sign-ins

- **The cookie**: `__Host-koko`, `Secure`, `HttpOnly`, `SameSite=Lax`,
  `Path=/`, no `Domain`, `Max-Age` up to the Sign-in's expiry 30 days on.
  Its value is 256 random bits in unpadded base64url, kept as its SHA-256.
- **The client label** ([`clientLabel`](../../internal/server/signin/client.go))
  is made at sign-in from the `User-Agent` by a small table of the common
  browsers and systems, such as "Safari on iPhone", or "Browser".
- **Lifetime.** A Sign-in ends 30 days after it began
  (`access.SignInLifetime`) or 7 days after it was last used
  (`access.SignInIdle`), whichever comes first, on the koko server's clock.
  Each call on the web listener moves its Last used time and address, at
  most once a minute. [`access.SignIns`](../../internal/server/access/access.go),
  the one way a Sign-in is read, answers one past its expiry as ended then,
  `expired`; the web front records it so the first time its cookie comes
  back, and logs it.
- **Ending**: by its browser signing out, by being revoked, by the User's
  Password being set or removed, and by the User being Removed. Ending is
  immediate: every call reads the Sign-in in its own transaction.
- **`signin.list`** (`koko signin ls [--user <u>] [--all]`, read tier)
  answers the caller's own Sign-ins, everyone's for the Owner, without the
  ended ones unless `all`; a Member naming another User gets none. Its
  `--json` is a bare array of `{"id", "user", "client", "address",
  "started_at", "last_used_at", "expires_at", "ended_at", "end_reason"}`,
  the last two `null` while it is live; `address` is the client's when the
  Sign-in was last used.
- **`signin.revoke`** (`koko signin revoke <id>`, irreversible tier) ends a
  live Sign-in, anyone's own and anyone's for the Owner, and answers it as
  it left it; an ended one is answered as it is. Another User's, for a
  Member, is refused with `unknown_signin` (404), as an id no Sign-in holds.

## The `/auth/` endpoints

The web front's own endpoints on the web listener, apart from the API,
since they come before or apart from an authenticated call. Each takes and
answers JSON, at most 4 KiB (`api.MaxAuthBody`), after the Origin check;
every answer carries the listener's headers and `Cache-Control: no-store`,
and a refusal is the API's error object. Their types are in
[`api/signin.go`](../../internal/shared/api/signin.go).

| Endpoint | Takes | Does |
| --- | --- | --- |
| `POST /auth/signin` | `{"name", "password"}` | Signs in: sets the cookie and answers `{"user"}`. A wrong name or Password is `wrong_password` (401), the same bytes whatever the name. |
| `POST /auth/link` | `{"link"}`, the secret after `#` | Answers `{"user", "expires_at"}`, spending nothing. |
| `POST /auth/password` | `{"link", "password"}`, or `{"current", "password"}` with the cookie | Sets the Password through a link, signing this browser in, or changes it, keeping this Sign-in; answers `{"user"}`. A wrong `current` is `wrong_password`. |
| `POST /auth/signout` | `{}` with the cookie | Ends this Sign-in (`signed_out`) and clears the cookie; without a live one it only clears it. |

## Rate limits

Held in memory ([`limit.go`](../../internal/server/signin/limit.go)),
bounded to the 10,000 most recent addresses and as many names, and
forgotten at restart. The client's address is the web listener's
([the web front](web-front.md#the-web-listener)).

- **For each address**: 10 failed attempts (a wrong sign-in, a refused
  link, a wrong current Password) within 15 minutes, and its next attempts
  at any `/auth/` endpoint but `signout` are answered `too_many_attempts`
  (429) with `Retry-After` in seconds, until the oldest failure is 15
  minutes old.
- **For each name**: 10 failed sign-ins in a row with one name, a User's or
  not, and sign-ins with it are answered `too_many_attempts` for 15
  minutes from any address, a correct Password included, before any
  hashing. A wrong current Password counts for the User's name too, but a
  lockout does not stop a change of Password. A
  successful sign-in and setting the Password start the count again; a
  Password Link, or the 15 minutes, ends a lockout.
- **Locked out is not signed out**: the Sign-ins already open keep working,
  and may still change their Password; only the address's limit bounds
  that.
- **Attempts at once.** An attempt counts as a failure from its check to its
  outcome, so attempts sent together cannot pass the limits together: of
  15 sent at once from one address, 10 are checked and 5 refused. One
  refused only for the attempts running beside it waits 1 second.

## What a Sign-in may do

The web front hands a `/v1/` call with a live Sign-in's cookie to the API as
`op.Caller{SignIn: id}`; `access.Who` turns it into an `Identity` whose
`User` is the Sign-in's and whose `SignIn` is set, and refuses an ended one
with `signin_required` (401). It may do what a Device Session of its User
may, but:

- `web.start` is refused with `device_only` (403): a Sign-in runs no `koko
  web`;
- `server.set` stays `local_only`;
- it opens no Tunnel and no SSH Session, having no Device Connection.
  `device.add` for its own User stays allowed: `koko join` spends that code
  on a computer. A Join Code a Sign-in issues names it in the log's
  `added_by`, but the new Device Session's `added_by` is left out, as for
  the Unix socket's.

`user.current` answers the Sign-in as `signin`, `{"id", "client",
"started_at", "expires_at"}`, in place of `device_session`. Log lines that
name who acted (`issued_by`, `revoked_by`, `removed_by`, `ended_by`,
`enrolled_by`) give a Sign-in's id ([`Identity.By`](../../internal/server/access/access.go)).

## Users

Removing a User ([Users](users.md#removing)) ends their live Sign-ins
(`user_removed`), deletes their Password and voids their unused links in its
one transaction ([`Service.Remove`](../../internal/server/signin/signin.go)).
A User who set a Password, or had a Sign-in, has used koko, so they are
Removed, not deleted; deleting one who never did takes their Password Links
with them. `user.list` gains `password_set_at`.

## Logging

At info level, once its transaction commits, with the User, the Sign-in's or
link's id and the client's address where there is one: `signed in` (with
`client`), `sign-in refused` (with `reason`, and `user` only when a User
holds the name), `signed out`, `sign-in revoked` (`revoked_by`), `sign-in
ended` (`reason`, `ended_by`), `password link issued` (`expires_at`,
`issued_by`), `password link refused` (`reason`), `password set` (`how`:
`link` or `change`) and `password removed` (`removed_by`). A wrong current
Password is a `sign-in refused` line naming the Sign-in that sent it. No line holds a Password, a link's secret, a cookie
or an `/auth/` body.

## Error codes

| Code | Status | When |
| --- | --- | --- |
| `web_url_unset` | 409 | A Password Link was asked for while the Web URL is not set. |
| `invalid_password` | 400 | A new Password breaks the rule. |
| `wrong_password` | 401 | The name or the Password given is wrong. |
| `too_many_attempts` | 429 | The address or the name is past its limit; `Retry-After` says when to try again. |
| `link_used` | 410 | The Password Link was used already. |
| `link_invalid` | 404 | The Password Link expired, was voided, is a Removed User's or was never issued. |
| `unknown_signin` | 404 | No Sign-in the caller may see has the id. |
| `no_password` | 409 | The User has no Password to remove. |
| `device_only` | 403 | `web.start` from a Sign-in. |

## Verifying

- [`internal/server/web/signin_test.go`](../../internal/server/web/signin_test.go),
  S1 through the web listener with the harness's `Browser`
  ([`browser.go`](../../internal/harness/browser.go)) and `h.SignIn`: a
  Password Link setting the Password and the cookie's attributes; wrong
  names and Passwords answered the same bytes; the address limit with
  `Retry-After`, of each kind of failure, and attempts sent at once; the
  name limit, its lockout,
  an open Sign-in still working, a name no User holds, and the count
  started again by a sign-in and by a link; the 7 and 30 days and Last used
  moving once a minute; every way a Sign-in ends; Password Links refused,
  voided, spent and expired; the Password rule; what a Sign-in may do; the
  log lines and no secret in them; removing a User invited for the web.
- [`internal/server/signin/hash_test.go`](../../internal/server/signin/hash_test.go):
  the hash sealed, absent in plain from the data directory, of the stated
  parameters, and an older one rehashed at sign-in;
  [`hash_internal_test.go`](../../internal/server/signin/hash_internal_test.go):
  at most two hashes at once, the client labels, the limits' bound.
- The access matrix's two Sign-in callers, and
  `TestAnEndedSignInIsRefusedByEveryOperation` in
  [`revoked_test.go`](../../internal/server/revoked_test.go)
  ([the access matrix](access.md#the-access-matrix)).
- [`internal/client/cli/signin_test.go`](../../internal/client/cli/signin_test.go)
  and [`valid_test.go`](../../internal/client/cli/valid_test.go): `koko
  password link`, `password rm`, `signin ls`, `signin revoke` and `user
  invite --web` with their goldens, a Member's own Sign-ins, `--valid` on
  both and a link of 7 days.
