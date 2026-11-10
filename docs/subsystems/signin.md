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
[`api/signin.go`](../../internal/shared/api/signin.go), with an
`api.AuthEndpoint` value for each (`AuthSignIn`, `AuthLink`,
`AuthPassword`, `AuthSignOut`), which the web UI's generated types carry
([the web UI build](web-ui.md#generated-types)).

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

## The web UI's pages

How each looks is [the web UI's design](../web-design.md#sign-in-and-set-a-password-signin-password).

- **Sign in** ([`SignInPage`](../../web/src/pages/signin/SignInPage.tsx),
  `/signin`), outside the frame: the User name and Password (`autocomplete`
  `username` and `current-password`), posted to `/auth/signin`, then the
  page `next` names, followed only when it is a path of this origin, else
  `/`, replacing the history entry. A refusal shows above the fields:
  `wrong_password`, or `too_many_attempts` with the minutes its
  `Retry-After` leaves. One line says the Owner resets a lost Password.
  The shell sends a browser here, with `next`, when any call is answered
  `signin_required` ([the web UI shell](web-shell.md#the-states-every-screen-has));
  `koko web` never answers so, so it never sends a browser there. A `next`
  naming Sign in or Set a Password is not followed either.
- **Set a Password** ([`PasswordPage`](../../web/src/pages/signin/PasswordPage.tsx),
  `/password`), outside the frame, which a Password Link opens: its route
  ([`takingLinkSecret`](../../web/src/pages/signin/linkSecret.tsx)), part of
  the first load, takes the secret from the fragment and replaces the URL
  without it at once, before the page's chunk loads, and again when
  another link is opened on the page; the page then, afresh for each link,
  asks `/auth/link` whom the link is for, and shows "For <name>." with the
  new Password twice (`new-password`), a count toward 15 characters under
  the first ([`PasswordFields`](../../web/src/pages/signin/PasswordFields.tsx)),
  counted as the koko server counts them, in code points after NFC. The
  form checks the rule and that the two agree when submitted, then posts
  the link and the Password to `/auth/password`, which signs this browser
  in, and goes to `/`. `link_used` and `link_invalid`, from either call,
  and a URL with no fragment, replace the form with what happened and Go
  to Sign in.
- Both show koko's wordmark, one panel and the two languages as buttons
  ([`Door`](../../web/src/pages/signin/Door.tsx)).
- **Account's Password** ([`ChangePassword`](../../web/src/pages/signin/ChangePassword.tsx)),
  for a browser signed in with a Password: the current Password, then the
  new one twice as Set a Password asks it, checked on submit, posted as
  `{"current", "password"}` to `/auth/password`, which keeps this Sign-in
  and ends the User's others; the Browser Sign-ins are read again. A wrong
  current Password shows in place, the form kept. Through `koko web` or for
  the Local caller, one sentence says a Password is changed in a browser
  signed in with it.
- **Browser Sign-ins** ([`SignIns`](../../web/src/pages/signin/SignIns.tsx)),
  on Account for one's own (`signin.list` naming the User): each browser's
  label, this browser's marked, its address in the code face, when it was
  last used and when it ends; "Show ended Sign-ins" adds the ended ones,
  struck through, with when and why. Revoke asks first and says what
  follows; revoking this browser's own goes to Sign in. A refused revoke
  reads the list again.
- **A person's panel**, the Owner's alone
  ([`PersonPassword`](../../web/src/pages/signin/PersonPassword.tsx)):
  when the User's Password was set (`user.list`'s `password_set_at`), or
  that they have none; **Issue Password Link** (`password.link`), valid
  for the time chosen and shown once, disabled, saying why, while
  `server.show` answers no Web URL ([`useWebURL`](../../web/src/pages/signin/webURL.ts)),
  its lead saying that an earlier unused link, an Invite's included, stops
  working; and **Remove Password** (`password.remove`), asking first, and
  saying, for the Owner's own from a browser signed in with it, that this
  browser is signed out too; after it the panel and Access show none, the
  Sign-ins are read again, and the section's heading takes the focus. Their
  Browser Sign-ins follow, as on Account, Revoke saying that User is
  signed out of that browser. Invite's In a browser issues a Password
  Link the same way ([the Access and Account pages](access-pages.md#a-join-code-or-password-link-on-screen)).
- **Sign out**, by Account's title and in the account panel for a browser
  signed in with a Password, posts to `/auth/signout` and goes to Sign in,
  replacing the history entry ([`useSignOut`](../../web/src/shell/signOut.ts)).

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
- [`signin.test.tsx`](../../web/src/pages/signin/signin.test.tsx), Vitest
  against the [fake API](web-ui.md#testing-pages)'s `/auth/` endpoints, axe
  on each state: Sign in outside the frame with its `autocomplete`, its
  fields checked on submit, signing in and following `next`, `next` of
  another origin or outside the frame never followed, `wrong_password` and
  `too_many_attempts` with its minutes, the language buttons; a read and a
  write answered `signin_required` going to Sign in with `next`; Set a
  Password taking the secret out of the URL, a second link read afresh,
  naming the User, its count in code points after NFC and past 128, the rule and the second field checked on submit, setting the
  Password and signing in, `invalid_password` in place, `link_used` and
  `link_invalid` from either call, no fragment, and a failed read with Try
  again.
- [`account.test.tsx`](../../web/src/pages/signin/account.test.tsx), with
  [`signins.fixture.ts`](../../web/src/pages/signin/signins.fixture.ts):
  Account for a Sign-in with its Password form, Sign-ins and Sign out;
  changing the Password, the other Sign-ins ended; its fields checked on
  submit; a wrong current Password in place; through `koko web` and for
  the Local caller no form and no Sign out; Revoke asking first, its
  consequence and the focus on the list's heading; revoking this browser's
  own going to Sign in; a refused revoke reading the list again; the ended
  Sign-ins with why; none; Sign out on Account and in the account panel,
  its failure in the panel.
- [`person.test.tsx`](../../web/src/pages/signin/person.test.tsx): a
  person's panel with its Password, Issue Password Link and Remove
  Password, and their Sign-ins; a User without a Password; Issue Password
  Link for 7 days, shown once with Copy and its time left, gone once
  closed, and for 15 minutes with `valid_for` left out; no Web URL;
  Remove Password asking first, then none in the panel and on Access with
  the focus on the heading, the Owner's own from a browser signed in with
  it, and refused; another User's Sign-in revoked; Invite in a browser for
  7 days, and in a terminal for 1 day; no Web URL for Invite; Add device
  for 1 hour.
