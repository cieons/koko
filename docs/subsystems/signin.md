# Passwords, passkeys, the Identity Provider and Sign-ins

How a browser signs in to the web UI on the web listener, at the Web URL
([ADR 0043](../adr/0043-a-browser-signs-in-with-a-password-set-through-a-password-link.md),
[ADR 0060](../adr/0060-a-browser-signs-in-with-a-passkey-through-the-owners-identity-provider-or-with-a-password.md)):
each User's one Password, their Passkeys, the Sign-in Links the Owner or an
Administrator issues for a User to add a way to sign in,
the Sign-ins and how each was made, the web front's `/auth/` endpoints, the rate limits, and what
a Sign-in may do. The Identity Provider has a page of its own,
[the Identity Provider](identity.md). The source is
[`internal/server/signin`](../../internal/server/signin/signin.go), with the
passkeys in [`internal/server/passkey`](../../internal/server/passkey/passkey.go)
and koko's own WebAuthn verifier in
[`internal/server/passkey/webauthn`](../../internal/server/passkey/webauthn/webauthn.go)
([ADR 0061](../adr/0061-koko-verifies-webauthn-itself-and-uses-go-oidc-for-openid-connect.md)),
the flows of a ceremony in [`internal/server/flow`](../../internal/server/flow/flow.go), and the
`/auth/` endpoints and the cookie in
[`internal/server/web/auth.go`](../../internal/server/web/auth.go) and the
Sign-in caller in [`access.Who`](../../internal/server/access/access.go).
The web listener itself, its Host and Origin checks and its headers are in
[the web front](web-front.md#the-web-listener).

## The record

Three migration steps, [`signins.sql`](../../internal/server/store/migrations/signins.sql),
[`signins_browser_system.sql`](../../internal/server/store/migrations/signins_browser_system.sql),
which keeps a Sign-in's browser and system apart in place of its label,
and [`passkeys_identities.sql`](../../internal/server/store/migrations/passkeys_identities.sql),
which adds a Sign-in's method and the tables of passkeys, Linked
Identities and the Identity Provider (spec #228; the passkey and Identity
Provider tickets fill them):

| Table | What it holds |
| --- | --- |
| `passwords` | A User's one Password, or no row: its id (`password_…`), the User, its Argon2id hash as a PHC string sealed under the Root Key for the row's id ([the Seal](seal.md)), when it was set and how (`link`, `change`, or `signin` for a first Password from a live Sign-in). Setting a Password writes a new row. |
| `password_links` | Each Sign-in Link, under the name the Password Link had before it: its id (`passwordlink_…`), the User, the SHA-256 of its secret, what issued it (a Device Session's or a Sign-in's id, or NULL for the Unix socket), when it was issued, expires, was used and was voided. Spent links are kept. |
| `passkeys` | Each Passkey: its id (`passkey_…`), the User, its credential id (unique), its COSE_Key as the authenticator gave it and that key's algorithm (-8 EdDSA, -7 ES256, -257 RS256), the host it was made for (`rp_id`), its name, the authenticator's last signature counter (0 for one that keeps none), its backup eligibility when made and its backup state at its last use, the Sign-in or Sign-in Link it was made through (`added_with`), when it was made and last used. None of it is secret, so none of it is sealed: a copy of the data directory signs nobody in with it. A removed Passkey's row is deleted; the log keeps it. |
| `identities`, `identity_provider` | Each Linked Identity and the one Identity Provider, as spec #228 gives them; the Identity Provider's is in [the Identity Provider](identity.md#the-record), and `identities` is empty until the ticket that links them. |
| `signins` | Each Sign-in: its id (`signin_…`), the User, the SHA-256 of its cookie's token, how it was made (`method`: `password`, `passkey` or `identity`, `password` for every Sign-in recorded before the step) and the passkey or Linked Identity it was made with (`credential`, NULL for a Password), the browser and the system its User-Agent named (`browser` and `system`, `''` for none), the client's address at sign-in and when last used, its start, its Last used time, its expiry 30 days on, and once it ends, when, why and by whom. Ended Sign-ins are kept, as Revoked Device Sessions are. |

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
  Sign-in Links, and starts their name's count of failed sign-ins again.
- **A first Password from a live Sign-in.** A User without a Password, signed
  in another way, sets one with `/auth/password` and the cookie, giving no
  current one. It ends no Sign-in, since the Sign-in it comes from is of a
  way they hold already, and voids their unused Sign-in Link, as every way
  of setting a Password does. A User who has a Password gives the
  current one, as before; without it the change is refused
  `wrong_password`, counted as a wrong current Password.
- **`password.remove`** (`koko password rm <user>`, the Owner or an
  Administrator, irreversible tier) deletes the Password, ends the Sign-ins
  made with it (`password_removed`) and voids the User's unused link,
  keeping the User, their devices and the Sign-ins made another way. A Member is refused with `owner_only` before the
  name is read, and an Administrator naming the Owner or another
  Administrator with `owner_only` once it is
  ([`Identity.MayActOn`](../../internal/server/access/access.go)). A User
  without one is refused with `no_password` (409), changing nothing.

## Passkeys

A Passkey is a discoverable WebAuthn credential that requires user
verification, made for the Web URL's host, which signs its User in in place
of a Password (ADR 0060). A User may hold several, each named, and no
Password at all. Through `koko web`, at `http://localhost:<port>`, a browser
offers none.

- **The relying party.** The RP ID is the Web URL's host without its port
  (`koko.example.com`), and an answer's origin must be the Web URL itself.
  A ceremony begun under one Web URL and answered under another is refused.
- **What koko asks of an authenticator**, as the JSON forms of WebAuthn
  Level 3's options (`api.PasskeyCreationOptions`,
  `api.PasskeyRequestOptions`), binary members in base64url: to make one,
  `rp` `{"id": <host>, "name": "koko"}`, `user` the User's id (`user_…` as
  bytes) and name and nothing else about the person, a challenge of 32
  random bytes, the algorithms EdDSA, ES256 and RS256 in that order,
  `residentKey` and `userVerification` `required`, `attestation` `none`,
  `excludeCredentials` the User's passkeys for the host, and a timeout of
  300,000 ms; to sign in, the host as `rpId`, a challenge, `userVerification`
  `required` and `allowCredentials` empty, so the authenticator offers its
  discoverable passkeys.
- **The flows** ([`flow.Store`](../../internal/server/flow/flow.go)). Each
  begin keeps its ceremony in memory under a random flow id of 128 bits,
  which the page posts back with the answer: its purpose (`signin` or
  `add`), the RP ID and origin, the challenge, and for adding one the User
  and the Sign-in or Sign-in Link it began from. The first answer that names
  a flow takes it, verified or not, and an answer is taken only for a flow
  of its purpose. A flow expires 5 minutes after its begin. At most 20 are
  held per client address, a 21st dropping that address's oldest, an IPv6
  address counting by its /64, which one host commonly holds whole, and at
  most 10,000 in all. When the store is full, the address holding the most
  flows loses its oldest, so addresses that fill it evict their own flows
  first: an address holding one flow loses it only once every address holds
  one alone, which takes 10,000 addresses or /64s beginning flows within 5
  minutes. What is left: a host with that many prefixes, such as a /48, can
  still drop a person's flow, and their passkey sign-in starts again; since
  an answer naming no flow held counts for no limit (below), it locks nobody
  out. A restart forgets the flows, and the page starts again.
  The Identity Provider's flows use the same store.
- **Making one** ([`Service.Add`](../../internal/server/passkey/ceremony.go)),
  through a Sign-in Link or from a live Sign-in, after
  [`webauthn.Register`](../../internal/server/passkey/webauthn/webauthn.go)
  has checked the answer: in one write transaction, the Web URL still the
  flow's, the Sign-in still the one the flow began from and live, no
  passkey holding the credential id (`credential_id` is unique besides), the
  User not Removed; then the row is written. Through a link the passkey is
  named with the browser's label (as a Sign-in's client is, "Firefox on
  Linux"), and the link is spent only then: the User's other Sign-ins end
  (`signin_link_used`), their unused links are voided, their name's count
  of failed sign-ins starts again, and this browser signs in with the
  method `passkey` ([`Service.SpendLink`](../../internal/server/signin/auth.go)).
  A begin, a ceremony the person cancels, or an answer refused, spends
  nothing. From a Sign-in it keeps the Sign-in and takes the name given, 1
  to 64 characters with no control character, trimmed (`invalid_name`
  otherwise), or the browser's label for none; a name through a link is
  `invalid_request`.
- **Signing in** ([`Service.SignIn`](../../internal/server/passkey/ceremony.go)):
  the credential id names a passkey of a User not Removed, made for the
  current host; [`webauthn.Assert`](../../internal/server/passkey/webauthn/webauthn.go)
  checks the answer against it, its `userHandle` against the passkey's own
  User. A counter that went back is refused and logged at warn level
  (`passkey counter went back`), since it suggests a copied authenticator;
  the passkey stays, for the Owner or its User to remove. The verifying runs
  outside any transaction; in the write transaction that starts the
  Sign-in, the passkey is read again, and its counter must be the one
  verified against, so two answers of a counting authenticator verified at
  once cannot both pass (a synced passkey's counter stays 0, so two of its
  ceremonies both pass, as the spec intends); the new
  counter, backup state and Last used are stored there.
- **Every refusal** of an answer is `passkey_refused` (401), the same bytes
  whichever step failed: the flow unknown, taken, expired or of the other
  purpose, the passkey unknown, of another host or of a Removed User, the
  answer not read, or any check of the verifier. Members of the answer koko
  does not read, which browsers add to, are taken and left unread. Every
  refusal counts as a failure for the client's address once the answer's
  flow is taken; an answer naming no flow held, never begun, taken already,
  expired or dropped, counts for none, since it can verify nothing, so there
  is nothing to guess, and another address's flood may have dropped its
  flow. It is logged at debug level.
- **A Web URL whose host is an IP address** has no passkeys: WebAuthn takes
  an IP address for no relying party, so a browser refuses every ceremony
  there. `/auth/methods` answers `passkeys` false then, and while the Web
  URL is unset, and the pages offer no passkey action.
- **Who made the link.** `added_by` is the User who issued the Sign-in Link
  a passkey was made through, the Owner for one the Unix socket issued, or
  `null` for one made from its User's own Sign-in.
- **`passkey.list`** (`koko passkey ls [--user <u>]`, read tier) answers the
  caller's own passkeys, or another User's for the Owner and the
  Administrators, in the order they were made; a Member naming another User
  gets none. Its `--json` is a bare array of `{"id", "user", "name",
  "host", "synced", "added_by", "added_at", "last_used_at"}`: `host` the
  host it was made for, `synced` its backup state, `added_by` and
  `last_used_at` `null` for none. Its text has a column for each, `-` for
  none.
- **`passkey.remove`** (`koko passkey rm <id>`, irreversible tier) deletes
  the row and ends the Sign-ins made with it (`passkey_removed`,
  [`Service.EndMadeWith`](../../internal/server/signin/signin.go)), and no
  others; it answers `{"passkey", "signins_ended"}`. Anyone removes their
  own; the Owner and the Administrators another User's, as
  [`Identity.MayActOn`](../../internal/server/access/access.go) lets them:
  an Administrator is refused `owner_only` for the Owner's or another
  Administrator's. Another User's, for a Member, is `unknown_passkey`
  (404), as an id no passkey holds. The authenticator keeps the passkey and
  the browser may still offer it, but it is refused; the web UI says to
  remove it in the device's own settings too. A Password set from a passkey's
  Sign-in outlives the passkey's removal, as every way to sign in is the
  User's own.
- **A change of the Web URL's host.** A passkey belongs to the host it was
  made for, the only place a browser offers it. It stays recorded and
  listed with its host, refused, and works again once the Web URL names
  that host again. `koko server set web_url=` says how many passkeys belong
  to another host than the new Web URL's (`server.set`'s
  `other_host_passkeys`), every one when it clears it; a change of port
  alone keeps every passkey. Their Users add new passkeys at the new host,
  and they or the Owner remove the old ones. A change of the Web URL signs
  every browser out and voids every unused link besides
  ([initialisation](initialisation.md)).
- **A lost passkey** is recovered as a lost Password is: a Sign-in Link,
  then removing the lost one.

## Sign-in Links

The Password Link, renamed (ADR 0060): one link that lets its User add a
way to sign in, and signs that browser in: a passkey or a Password, and
their Linked Identity once the Identity Provider's ticket of spec #228
lands. Every
wire name stays: `password.link`, `password_link`, `password_links` and the
`passwordlink_…` ids.

- **What one is**: the Web URL followed by `/link#<secret>`, the secret
  128 random bits as 26 base32 characters (`crypto/rand.Text`), kept only as
  its SHA-256. It sits in the fragment, which no browser sends, so it reaches
  no proxy log and no `Referer`. A link a koko server issued before, the Web
  URL followed by `/password#<secret>`, keeps working until it expires: the
  web UI opens the same page at `/password`.
- **Issuing** ([`Service.IssueLink`](../../internal/server/signin/signin.go)),
  by the Owner or an Administrator, for any User not Removed, the Owner
  included:
  `user.invite` with `web: true` (`koko user invite <name> --web`) creates
  the User with a link instead of a Join Code; `password.link` (`koko
  signin link <user>`, and `koko password link <user>` as before) issues one
  for an existing User. A Member is
  refused with `owner_only` before the name is read; an Administrator
  issues one for a Member or for themselves, and is refused with
  `owner_only` for the Owner or another Administrator. Both take `valid_for`
  in seconds, 60 to 604,800 (`api.Validity`, 15 minutes by default), and
  are refused with `web_url_unset` (409) while the Web URL is not set.
- **One live link per User**: issuing one voids the User's earlier unused
  one, and Removing a User voids theirs and those they issued for others.
- **A change of role** voids links ([Users](users.md#administrators)):
  promoting a User voids their unused links issued by anyone but the Owner
  and the User, and demoting an Administrator voids the unused links they
  issued for other Users.
- **Spending one**: `POST /auth/link` answers whom an unspent link is for and
  the ways to sign in they hold already, spending nothing; `POST
  /auth/password` with the link spends it, sets the Password and signs this
  browser in, as `/auth/passkey/add` does with a passkey's answer.
  Spending happens at the end of the step chosen, never at its
  start, and ends the User's other Sign-ins: `password_changed` for a
  Password, as setting one always has, and `signin_link_used` for a
  passkey and, once it lands, a Linked Identity. A link used already is refused with
  `link_used` (410); an expired, voided, unknown or Removed User's one with
  `link_invalid` (404). Each refusal counts as a failed attempt for the
  client's address.

## Sign-ins

- **The cookie**: `__Host-koko`, `Secure`, `HttpOnly`, `SameSite=Lax`,
  `Path=/`, no `Domain`, `Max-Age` up to the Sign-in's expiry 30 days on.
  Its value is 256 random bits in unpadded base64url, kept as its SHA-256.
- **The browser and the system** ([`clientOf`](../../internal/server/signin/client.go))
  are found at sign-in in the `User-Agent` by a small table of the common
  browsers and systems, such as Safari and iPhone, each none when the
  table holds none it names. A Sign-in as the API answers it carries both,
  `browser` and `system`, each left out for none, and `client`, the
  English label the koko server makes of them when it answers
  ([`api.ClientLabel`](../../internal/shared/api/signin.go)): "Safari on
  iPhone", the browser alone without a system, or "Browser" without a
  browser. The CLI prints `client`; the web UI names the browser in the
  reader's language from `browser` and `system` by the same rule
  ([`browserName`](../../web/src/shell/browserName.ts)): "{browser} on
  {system}" and "Browser" in English, "{system} 上的 {browser}" and "浏览器"
  in Chinese. A Sign-in recorded before the two were kept apart keeps what
  its label said: the migration step
  [`signins_browser_system.sql`](../../internal/server/store/migrations/signins_browser_system.sql)
  split each label, which the koko server had made from the same table,
  exactly.
- **Lifetime.** A Sign-in ends 30 days after it began
  (`access.SignInLifetime`) or 7 days after it was last used
  (`access.SignInIdle`), whichever comes first, on the koko server's clock.
  Each call on the web listener moves its Last used time and address, at
  most once a minute. [`access.SignIns`](../../internal/server/access/access.go),
  the one way a Sign-in is read, answers one past its expiry as ended then,
  `expired`; the web front records it so the first time its cookie comes
  back, logs it, and has the access-change signal name it, so its web
  terminal's pages detach. Until then nothing publishes it, so the Gateway
  also reads the Sign-in of each page every minute, and each page's at its
  requests, and records an expired one the same way (`RecordExpiry`); the
  SSH Sessions it opened go on
  ([the Gateway](gateway.md#what-only-detaches)).
- **The method**: how the browser signed in, `password`, `passkey` or
  `identity`, recorded when the Sign-in starts ([`Start`](../../internal/server/signin/auth.go))
  with the passkey or Linked Identity it was made with. A Sign-in made by
  spending a link has its step's method. Every Sign-in a Password makes
  records `password`.
- **Ending**: by its browser signing out, by being revoked, by the User's
  Password being set, by the Password it was made with being removed, by
  the passkey it was made with being removed (`passkey_removed`), by a
  Sign-in Link of its User spent with a passkey (`signin_link_used`), and
  by the User being Removed. Spec #228 adds `identity_unlinked` (an
  identity unlinked ends those made through it), and `signin_link_used` for
  a link spent with an identity, which the access-change signal and the
  Gateway carry already. Ending is
  immediate: every call reads the Sign-in in its own transaction. Once the
  call that ends it commits, an `/auth/` endpoint's included, the
  access-change signal names it with its end reason (`access.Change.SignIns`,
  [the access check](access.md#the-access-change-signal)), and its web
  terminal's pages detach at once. Signing out takes back nothing: the SSH
  Sessions it opened go on, its User's. Every other ending ends the SSH
  Sessions it opened, each with its notice, in every page attached
  ([the Gateway](gateway.md#what-only-detaches)).
- **`signin.list`** (`koko signin ls [--user <u>] [--all]`, read tier)
  answers the caller's own Sign-ins, everyone's for the Owner or an
  Administrator, without the ended ones unless `all`; a Member naming
  another User gets none. Its
  `--json` is a bare array of `{"id", "user", "client", "method", "address",
  "started_at", "last_used_at", "expires_at", "ended_at", "end_reason"}`,
  the last two `null` while it is live, and `method` `null` from a koko
  server before it; `address` is the client's when the Sign-in was last
  used. Its text has a `METHOD` column, `-` for none.
- **`signin.revoke`** (`koko signin revoke <id>`, irreversible tier) ends a
  live Sign-in, anyone's own, anyone's for the Owner and any Member's for
  an Administrator, and answers it as it left it; an ended one is answered
  as it is. Another User's, for a Member, is refused with `unknown_signin`
  (404), as an id no Sign-in holds; the Owner's or another
  Administrator's, for an Administrator, with `owner_only`.

## The `/auth/` endpoints

The web front's own endpoints on the web listener, apart from the API,
since they come before or apart from an authenticated call. Each takes and
answers JSON, at most 4 KiB (`api.MaxAuthBody`), or 64 KiB
(`api.MaxPasskeyBody`) for the two that carry a passkey's answer, since an
RSA key or an attestation statement an authenticator kept takes more,
after the Origin check;
every answer carries the listener's headers and `Cache-Control: no-store`,
and a refusal is the API's error object. Their types are in
[`api/signin.go`](../../internal/shared/api/signin.go), with an
`api.AuthEndpoint` value for each (`AuthSignIn`, `AuthLink`,
`AuthPassword`, `AuthSignOut`, `AuthMethods`, and the passkey ones in
[`api/passkey.go`](../../internal/shared/api/passkey.go)), which the web UI's generated types carry
([the web UI build](web-ui.md#generated-types)).

| Endpoint | Takes | Does |
| --- | --- | --- |
| `POST /auth/signin` | `{"name", "password"}` | Signs in: sets the cookie and answers `{"user"}`. A wrong name or Password is `wrong_password` (401), the same bytes whatever the name. |
| `POST /auth/link` | `{"link"}`, the secret after `#` | Answers `{"user", "expires_at", "password_set", "passkeys", "identity_linked"}`, spending nothing: whether the User has a Password, how many passkeys and whether a Linked Identity, so the page can say what a choice replaces. |
| `POST /auth/password` | `{"link", "password"}`, `{"current", "password"}` with the cookie, or `{"password"}` alone with the cookie | Sets the Password through a link, signing this browser in; changes it, keeping this Sign-in; or sets a first one for a User who has none, ending no Sign-in; answers `{"user"}`. A wrong `current`, or none for a User with a Password, is `wrong_password`. |
| `POST /auth/signout` | `{}` with the cookie | Ends this Sign-in (`signed_out`) and clears the cookie; without a live one it only clears it. |
| `POST /auth/methods` | `{}` | Answers `{"passkeys", "provider"}`: whether a browser can make and use a passkey at the Web URL, false while it is unset or its host is an IP address, and the Identity Provider's button, `{"name"}`, or `null` while none is configured. |
| `POST /auth/passkey/signin/begin` | `{}` | Begins signing in with a passkey: answers `{"flow", "options"}`. |
| `POST /auth/passkey/signin` | `{"flow", "credential"}`, the answer as `toJSON()` gives it: `id`, `rawId`, `type`, `response.{clientDataJSON, authenticatorData, signature, userHandle}` | Signs in with the method `passkey`: sets the cookie and answers `{"user"}`. |
| `POST /auth/passkey/add/begin` | `{"link"}`, or `{}` with the cookie | Begins making a passkey for the link's User or the Sign-in's: answers `{"flow", "options"}`, spending nothing. Without a live Sign-in or a link, `signin_required`. |
| `POST /auth/passkey/add` | `{"flow", "credential", "name"}`, `name` only with the cookie; the answer's `response` holds `clientDataJSON` and `attestationObject` | Records the passkey: through a link spends it, signs this browser in and sets the cookie; from a Sign-in keeps it. Answers `{"user", "passkey"}`. |

## Rate limits

Held in memory ([`limit.go`](../../internal/server/signin/limit.go)),
bounded to the 10,000 most recent addresses and as many names, and
forgotten at restart. The client's address is the web listener's
([the web front](web-front.md#the-web-listener)).

- **For each address**: 10 failed attempts (a wrong sign-in, a refused
  link, a wrong current Password, `passkey_refused` for an answer whose flow
  was held) within 15 minutes, and its next attempts
  at any `/auth/` endpoint but `signout`, `methods` and
  `passkey/signin/begin` are answered `too_many_attempts`
  (429) with `Retry-After` in seconds, until the oldest failure is 15
  minutes old.
- **For each name**: 10 failed sign-ins in a row with one name, a User's or
  not, and sign-ins with it are answered `too_many_attempts` for 15
  minutes from any address, a correct Password included, before any
  hashing. A wrong current Password counts for the User's name too, but a
  lockout does not stop a change of Password. A
  successful sign-in and setting the Password start the count again; a
  Sign-in Link, or the 15 minutes, ends a lockout.
- **No name for a passkey.** A passkey's sign-in names no User before it
  verifies, so it counts for no name, and a name's lockout, which guards
  against guessing a Password, does not stop it.
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
- `server.set` stays `local_only`.

`device.add` for its own User stays allowed: `koko join` spends that code
on a computer. A Join Code a Sign-in issues names it in the log's
`added_by`, but the new Device Session's `added_by` is left out, as for the
Unix socket's.

A Sign-in also opens SSH Sessions in its User's Workspace, through the web
terminal's sockets ([the web front](web-front.md#the-web-terminals-websocket)),
by the Space membership rules a device follows
([ADR 0053](../adr/0053-a-browser-opens-ssh-sessions-through-a-websocket-to-the-gateway.md),
[ADR 0057](../adr/0057-a-users-web-terminal-tabs-are-one-workspace-held-by-the-koko-server.md)):
the Workspace holds at most 16 tabs, whichever page opens them, and a page's
attachment is resumed only from the same Sign-in
([the Gateway](gateway.md#the-web-terminal)). Each socket's first message
moves its Last used time, at most once a minute, as a call does; keys typed
do not. They are their User's, checked as the User's: a Space change that
takes the Host out of the User's reach ends them, whoever opened them, and
the Sign-in's signing out or expiry only detaches its pages, while its
revocation, or a Password change or removal that ends it, ends those it
opened ([the Gateway](gateway.md#what-only-detaches)). The terminal is the web front's own endpoint, as
`/auth/` is, so no operation is added and the access matrix gains no row.

`user.current` answers the Sign-in as `signin`, `{"id", "client",
"browser", "system", "method", "credential", "started_at", "expires_at"}`, `browser` and `system`
left out for none and `credential`, the passkey's or Linked Identity's id
it was made with, for a Password, in place of `device_session`; a Sign-in in
`signin.list` carries `credential` too. For every caller, `user.current`
answers the User's own ways to sign in as `user.list` does
(`password_set_at`, `passkeys`, `identity_linked_at`), so Account says
what removing one leaves. Log lines that
name who acted (`issued_by`, `revoked_by`, `removed_by`, `ended_by`,
`enrolled_by`) give a Sign-in's id ([`Identity.By`](../../internal/server/access/access.go)).

## The web UI's pages

How each looks is [the web UI's design](../web-design.md#sign-in-and-a-sign-in-links-page-signin-link).

- **The WebAuthn helper** ([`webauthn.ts`](../../web/src/pages/signin/webauthn.ts)),
  koko's own, with no npm package, so that every browser with WebAuthn
  works: base64url both ways; koko's options as
  `navigator.credentials.create()` and `.get()` take them, every member as
  given and the binary ones (`challenge`, `user.id`, each
  `excludeCredentials` and `allowCredentials` id) as bytes; and the
  browser's answer back as koko takes it, a `userHandle` left out sent as
  `""`. A browser without `window.PublicKeyCredential` and
  `navigator.credentials` (`passkeysAvailable`) gets neither Sign in with
  a passkey, nor Create a passkey, nor Add a passkey; it still lists and
  removes passkeys. Nor does a browser where `/auth/methods` answers
  `passkeys` false, the Web URL's host being an IP address
  (`usePasskeysOffered`): Sign in and a Sign-in Link's page ask it, and
  Account for a browser on the web listener, never through `koko web`; a
  failed `/auth/methods` offers passkeys, since the browser's own refusal
  still shows. A ceremony the person cancels or lets time out
  (`NotAllowedError`), or that the page aborts as it closes (`AbortError`),
  shows nothing; `InvalidStateError`, the device holding one of the User's
  passkeys already, says so, and any other failure says the browser could
  not complete it ([`refusalText`](../../web/src/pages/signin/refusal.ts)).
  Each press begins a ceremony of its own, since a flow is taken by its
  first answer. A ceremony runs only while its page is open
  (`useCeremony`): it checks after each wait, the begin and the browser's
  prompt, and sends nothing more once the page or panel has closed, and
  the page's closing aborts the prompt. A last answer that comes after
  the page closed, which signed this browser in, goes nowhere: a toast says
  "This browser is signed in as <name>.", from the answer's User.
- **Sign in** ([`SignInPage`](../../web/src/pages/signin/SignInPage.tsx),
  `/signin`), outside the frame: **Sign in with a passkey**, the primary
  action, where the browser has WebAuthn and `/auth/methods` offers
  passkeys: `/auth/passkey/signin/begin`,
  `navigator.credentials.get()`, then `/auth/passkey/signin`; then a
  divider and the User name and Password (`autocomplete` `username` and
  `current-password`), posted to `/auth/signin`, **Sign in** secondary
  beside a passkey and primary without one. With WebAuthn the panel shows
  Loading until `/auth/methods` answers, so no passkey action shows and
  then goes. Either goes on to the page
  `next` names, followed only when it is a path of this origin, else
  `/`, replacing the history entry. A refusal shows above them:
  `passkey_refused`, `wrong_password`, or `too_many_attempts` with the
  minutes its `Retry-After` leaves. One line says the Owner or an
  Administrator can send a Sign-in Link.
  The shell sends a browser here, with `next`, when any call is answered
  `signin_required` ([the web UI shell](web-shell.md#the-states-every-screen-has));
  `koko web` never answers so, so it never sends a browser there. A `next`
  naming Sign in or a Sign-in Link's page is not followed either.
- **A Sign-in Link's page** ([`LinkPage`](../../web/src/pages/signin/LinkPage.tsx),
  `/link`, and `/password` for a link issued before the Sign-in Link),
  outside the frame, which a Sign-in Link opens: its route
  ([`takingLinkSecret`](../../web/src/pages/signin/linkSecret.tsx)), part of
  the first load, takes the secret from the fragment and replaces the URL
  without it at once, before the page's chunk loads, and again when
  another link is opened on the page; the page then, afresh for each link,
  asks `/auth/link` whom the link is for, and shows Set up sign-in: "For
  <name>.", **Create a passkey** (primary) and **Set a Password instead**
  (secondary). Create a passkey posts the link to
  `/auth/passkey/add/begin`, makes the passkey with
  `navigator.credentials.create()` and posts its answer to
  `/auth/passkey/add`, which spends the link, names the passkey with the
  browser's label and signs this browser in; then `/`. The secret is sent
  only while the page is open: another link opened as the browser makes
  the passkey closes the first one's page, and its answer is not sent. Set
  a Password instead, or the page at once in a browser without WebAuthn or
  where `/auth/methods` offers no passkeys (the page shows Loading until
  both `/auth/link` and `/auth/methods` have answered), shows Set a Password, with Back to the choices beside a passkey: a line
  saying a Password set there replaces theirs when
  `password_set` says they have one, which describes the submit button
  (`aria-describedby`), and the new Password twice
  (`new-password`), a count toward 15 characters under
  the first ([`PasswordFields`](../../web/src/pages/signin/PasswordFields.tsx)),
  counted as the koko server counts them, in code points after NFC. The
  form checks the rule and that the two agree when submitted, then posts
  the link and the Password to `/auth/password`, which signs this browser
  in, and goes to `/`. `link_used` and `link_invalid`, from any call,
  and a URL with no fragment, replace the page with what happened and Go
  to Sign in.
- Both show koko's wordmark, one panel and the two languages as buttons
  ([`Door`](../../web/src/pages/signin/Door.tsx)).
- **Account's Sign-in**, one panel, its lead line naming how this browser
  signed in (`user.current`'s `signin.method`): with a passkey, a Password
  or through the identity provider.
  - **Passkeys** ([`Passkeys`](../../web/src/pages/signin/Passkeys.tsx),
    `passkey.list` naming the User): each one's name, "Made for <host>"
    where its host is not the Web URL's host (`server.show`'s `web_url`,
    without its port, [`useWebURL`](../../web/src/pages/signin/webURL.ts)),
    Synced, when it was added and last used ("Never used"), and Remove.
    **Add a passkey**, for a browser signed in with WebAuthn: a name,
    prefilled with this browser's label, checked on submit (1 to 64
    characters), then `/auth/passkey/add/begin` with no link,
    `navigator.credentials.create()` and `/auth/passkey/add` with the name,
    which keeps the Sign-in; every read on the page is read again. Where
    `/auth/methods` offers no passkeys, one sentence says in its place that
    passkeys need a Web URL whose host is a domain name. Through
    `koko web` or for the Local caller they are listed with Remove, and one
    sentence says passkeys are added in a browser signed in at the Web URL.
    Remove (`passkey.remove`) asks first and says who is signed out: this
    browser, when its Sign-in was made with that passkey (`user.current`'s
    `signin.credential`), else the browsers signed in with it; that the
    device or password manager keeps it, to remove in its own settings;
    and, when it is the User's last way to sign in (no Password, no Linked
    Identity and no other passkey), that their next sign-in needs a Sign-in
    Link. Removing this browser's own goes
    to Sign in; otherwise the focus moves to the heading.
  - **Password** ([`ChangePassword`](../../web/src/pages/signin/ChangePassword.tsx)),
    for a browser signed in: for a User who has one (`user.current`'s
    `password_set_at`), the current Password, then the new one twice as Set
    a Password asks it, checked on submit, posted as `{"current",
    "password"}` to `/auth/password`, which keeps this Sign-in and ends the
    User's others; the Browser Sign-ins are read again. A wrong current
    Password shows in place, the form kept. For a User without one, the new
    one twice alone, posted as `{"password"}`, which ends nothing; the page
    then offers to change it. Through `koko web` or for the Local caller,
    one sentence says a Password is changed in a browser signed in with it.
- **Browser Sign-ins** ([`SignIns`](../../web/src/pages/signin/SignIns.tsx)),
  on Account for one's own (`signin.list` naming the User): each browser's
  label and how it signed in (its `method`: "with a passkey", "with a
  Password" or "through the identity provider", a Password for a koko
  server that names none), this browser's marked, its address in the code face, when it was
  last used and when it ends; "Show ended Sign-ins" adds the ended ones,
  struck through, with when and why. Revoke asks first and says what
  follows, without assuming a Password; revoking this browser's own goes
  to Sign in. A refused revoke reads the list again.
- **A person's panel**, the Owner's and the Administrators' alone,
  under **Sign-in** ([`PersonSignIn`](../../web/src/pages/signin/PersonSignIn.tsx)):
  when the User's Password was set (`user.list`'s `password_set_at`), or
  that they have none; their passkeys (`passkey.list` naming them), as on
  Account, each with Remove, the last way to sign in counted from
  `user.list`'s `password_set_at`, `passkeys` and `identity_linked_at`;
  **Issue Sign-in Link** (`password.link`), valid
  for the time chosen and shown once, disabled, saying why, while
  `server.show` answers no Web URL ([`useWebURL`](../../web/src/pages/signin/webURL.ts)),
  its lead saying that an earlier unused link, an Invite's included, stops
  working; and **Remove Password** (`password.remove`), asking first,
  saying that the Sign-ins made with the Password end and, for one's own
  from a browser signed in with it (`user.current`'s `signin.method`), that
  this browser is signed out too; after it the panel and Access show none, the
  Sign-ins are read again, and the section's heading takes the focus.
  Viewing the Owner or another Administrator, an Administrator sees them
  without any action. Their Browser Sign-ins follow, as on Account, Revoke
  saying that User is signed out of that browser. Invite's In a browser issues a Sign-in
  Link the same way ([the Access and Account pages](access-pages.md#a-join-code-or-sign-in-link-on-screen)).
- **Sign out**, by Account's title and in the account panel for a browser
  signed in, whatever the way, posts to `/auth/signout` and goes to Sign in,
  replacing the history entry ([`useSignOut`](../../web/src/shell/signOut.ts)).

## Users

Removing a User ([Users](users.md#removing)) ends their live Sign-ins
(`user_removed`), deletes their Password and their passkeys and voids their unused links in its
one transaction ([`Service.Remove`](../../internal/server/signin/signin.go)),
and the unused links they issued for others
([`Service.VoidLinksIssuedBy`](../../internal/server/signin/signin.go)), as
demoting an Administrator does; promoting a User voids theirs issued by
anyone but the Owner and them
([`Service.VoidLinksOthersIssuedFor`](../../internal/server/signin/signin.go),
[Users](users.md#administrators)).
A User who set a Password, or had a Sign-in, has used koko, so they are
Removed, not deleted; deleting one who never did takes their Sign-in Links
with them. `user.list` gains `password_set_at`, `passkeys`, a count, and
`identity_linked_at`, `null` until the Identity Provider's ticket links
identities; `koko user ls` says in its `SIGN-IN` column how each User signs
in, such as `password, 2 passkeys`, or `-` for no way.

## Logging

At info level, once its transaction commits, with the User, the Sign-in's or
link's id and the client's address where there is one: `signed in` (with
`method`, the passkey's or identity's id under its method's name for
those, and `client`), `sign-in refused` (with `reason`, and `user` only
when a User holds the name), `signed out`, `sign-in revoked`
(`revoked_by`), `sign-in ended` (`reason`, `ended_by`), `sign-in link
issued` (`expires_at`, `issued_by`), `sign-in link refused` (`reason`),
`password set` (`how`: `link`, `change`, or `signin` for a first Password
from a live Sign-in), `password removed` (`removed_by`), `passkey added`
(`passkey`, `name`, `host`, `added_with`), `passkey removed` (`passkey`,
`name`, `removed_by`, `signins_ended`), and at warn level `passkey counter
went back` (`passkey`). A passkey's refusal is a `sign-in refused` line
with the reason `passkey_refused`, its `ceremony` (`signin` or `add`) and
its User where one is known. A link's id
keeps the attribute `password_link`. A wrong current
Password is a `sign-in refused` line naming the Sign-in that sent it. No line, answer or error holds a Password, a link's secret, a cookie,
an `/auth/` body, a flow id, or a WebAuthn challenge, client data,
attestation, assertion or signature.

## Error codes

| Code | Status | When |
| --- | --- | --- |
| `web_url_unset` | 409 | A Sign-in Link was asked for while the Web URL is not set. |
| `invalid_password` | 400 | A new Password breaks the rule. |
| `wrong_password` | 401 | The name or the Password given is wrong. |
| `too_many_attempts` | 429 | The address or the name is past its limit; `Retry-After` says when to try again. |
| `link_used` | 410 | The Sign-in Link was used already. |
| `link_invalid` | 404 | The Sign-in Link expired, was voided, is a Removed User's or was never issued. |
| `unknown_signin` | 404 | No Sign-in the caller may see has the id. |
| `no_password` | 409 | The User has no Password to remove. |
| `device_only` | 403 | `web.start` from a Sign-in. |
| `passkey_refused` | 401 | A passkey's answer did not verify, named no passkey for this host, or came for a flow used, expired, of the other purpose or never begun. |
| `unknown_passkey` | 404 | No passkey the caller may see has the id. |

## Verifying

- [`internal/server/web/signin_test.go`](../../internal/server/web/signin_test.go),
  S1 through the web listener with the harness's `Browser`
  ([`browser.go`](../../internal/harness/browser.go)) and `h.SignIn`: a
  Sign-in Link setting the Password and the cookie's attributes, and the
  Sign-in's browser and system in `user.current` and `signin.list`; wrong
  names and Passwords answered the same bytes; the address limit with
  `Retry-After`, of each kind of failure, and attempts sent at once; the
  name limit, its lockout,
  an open Sign-in still working, a name no User holds, and the count
  started again by a sign-in and by a link; the 7 and 30 days and Last used
  moving once a minute; every way a Sign-in ends; Sign-in Links refused,
  voided, spent and expired; the Password rule; what a Sign-in may do; an
  Administrator's Sign-in listing every Sign-in, revoking a Member's,
  removing their Password and issuing them a link, and refused each on the
  Owner and another Administrator; the
  log lines and no secret in them; removing a User invited for the web.
- [`internal/server/web/signin_link_test.go`](../../internal/server/web/signin_link_test.go):
  a Sign-in Link of the `/link#` form, the web UI served at `/link` and
  `/password`, `/auth/link`'s fields, and the method of the Sign-ins it
  makes; `password.remove` ending only the Password's Sign-ins beside one
  recorded with a passkey, a first Password set from that Sign-in, ending
  no Sign-in and voiding the unused link, and a User with a Password still
  asked for the current one.
- [`internal/server/web/passkey_test.go`](../../internal/server/web/passkey_test.go),
  S1 through the web listener with `Browser` and
  [`harness.Authenticator`](../../internal/harness/authenticator.go)
  ([`passkey.go`](../../internal/harness/passkey.go)): a passkey made through
  a link, spending it and ending the other Sign-ins, then signing in with
  the cookie's attributes, the method and the counter stored, and a counter
  gone back refused; one added from a Sign-in, its name, and the next
  creation excluding it; every refusal the same bytes, a flow taken by the
  first answer and one begun before a restart among them; a cancelled or
  refused ceremony spending no link; a link or Sign-in ended between begin
  and answer; a credential id another User's passkey holds; removing one
  ending only its Sign-ins and the web terminals they opened; a host
  change, a port change and the old host put back; 20 flows per address
  and per IPv6 /64; `passkey_refused` counting for the address, at a
  sign-in and at an add, a refused link at an add's begin too, while an
  answer naming a flow never begun or dropped counts for none; an add flow
  bound to its Sign-in; a sign-in and an add answered under another Web
  URL; a Member listing no other User's passkeys, from a Sign-in and a
  device; the bodies' limits; `/auth/methods` and no passkeys at an IP
  address; removing a User deleting their passkeys and
  `user.list`'s count; the log lines and no challenge, flow, client data
  or signature in a line or an answer.
- [`internal/server/flow/flow_test.go`](../../internal/server/flow/flow_test.go):
  a flow taken once, expiring, 20 per address and per IPv6 /64, 10,000
  in all, and addresses flooding the store from one /48 evicting their own
  flows before a flow of an address holding one;
  [`internal/server/passkey/passkey_test.go`](../../internal/server/passkey/passkey_test.go):
  two answers of one passkey held between their verifying and their write,
  of which one signs in.
- [`internal/server/signin/hash_test.go`](../../internal/server/signin/hash_test.go):
  the hash sealed, absent in plain from the data directory, of the stated
  parameters, and an older one rehashed at sign-in;
  [`hash_internal_test.go`](../../internal/server/signin/hash_internal_test.go):
  at most two hashes at once, the browser and system of each kind of
  `User-Agent` and their English label, the limits' bound.
- [`signins_browser_system_test.go`](../../internal/server/store/signins_browser_system_test.go):
  a Sign-in recorded before the browser and the system were kept apart
  keeps what its label said;
  [`passkeys_identities_test.go`](../../internal/server/store/passkeys_identities_test.go):
  every Sign-in recorded before reads `password`, nothing else changes, and
  the koko before the step refuses the database, naming its snapshot.
- The access matrix's two Sign-in callers, its rows for `passkey.list`
  and `passkey.remove` ([`matrix_passkey_test.go`](../../internal/server/access/matrix_passkey_test.go)), and
  `TestAnEndedSignInIsRefusedByEveryOperation` in
  [`revoked_test.go`](../../internal/server/revoked_test.go)
  ([the access matrix](access.md#the-access-matrix)).
- [`internal/client/cli/signin_test.go`](../../internal/client/cli/signin_test.go)
  and [`valid_test.go`](../../internal/client/cli/valid_test.go): `koko
  signin link` and its alias `password link`, each in its own shape,
  `password rm`, `signin ls` with its method, `signin revoke` and `user
  invite --web` with their goldens, a Member's own Sign-ins, `--valid` on
  both and a link of 7 days;
  [`signin_skew_test.go`](../../internal/client/cli/signin_skew_test.go):
  `signin link` against a koko server before the Sign-in Link printing its
  `/password#` link and `signin ls` no method, and an older CLI's
  `password link` against this one;
  [`passkey_test.go`](../../internal/client/cli/passkey_test.go): `koko
  passkey ls` and `rm` and `user ls` with their goldens, text and `--json`,
  `server set web_url=` counting the passkeys of another host, and against
  a koko server before passkeys, `server_too_old` and `user ls` saying the
  Password alone.
- [`webauthn.test.ts`](../../web/src/pages/signin/webauthn.test.ts): base64url
  both ways, padded or not; koko's options as `create()` and `get()` take
  them; the answers as koko takes them, a `userHandle` left out as `""`;
  which failures are a cancel; `passkeysAvailable`.
- [`passkeys.test.tsx`](../../web/src/pages/signin/passkeys.test.tsx), with
  [`passkeys.fixture.ts`](../../web/src/pages/signin/passkeys.fixture.ts)'s
  fake authenticator in place of `navigator.credentials`, axe on each
  state: Sign in with a passkey first and Sign in secondary, none without
  WebAuthn; Loading until `/auth/methods` answers, no passkey on Sign in or
  a Sign-in Link's page where it says an IP address, and one offered when
  it fails; `get()` given the challenge's bytes and `rpId`, the answer
  posted in base64url, `next` followed; nothing posted once Sign in closed
  during the prompt, and an answer after it closed going nowhere with its
  toast; a cancel showing nothing and the
  next press beginning anew; `passkey_refused`, `too_many_attempts` and a
  browser's failure; in Chinese. A Sign-in Link's choices; Create a passkey
  posting the link, then the answer, and going to `/`; the answer, or the
  ceremony, not sent once another link closed the page, and the last
  answer after it closed going nowhere with its toast; a cancel, a
  refusal and a passkey the device holds already; `link_used` from the
  answer and `link_invalid` from the begin; Set a Password instead, with
  and without a Password, Back, and setting it; `/password`; in Chinese.
  Account's Sign-in: the passkeys, one of another host, Synced and never
  used; the method in its lead; Add a passkey with the name typed, checked
  on submit, cancelled, refused, not sent once Account closed during the
  prompt, and said in a toast when Account closed during its answer; the
  sentence in both languages where `/auth/methods` says an IP address;
  removing this browser's own, another and the
  last way; through `koko web`, for the Local caller and without WebAuthn;
  a first Password; each Browser Sign-in's method in both languages; in
  Chinese. A person's passkeys, their last way, an Administrator viewing
  the Owner's, Make an Administrator listing them with who made their
  links, and Access's summary.
- [`signin.test.tsx`](../../web/src/pages/signin/signin.test.tsx), Vitest
  against the [fake API](web-ui.md#testing-pages)'s `/auth/` endpoints, axe
  on each state: Sign in outside the frame with its `autocomplete`, its
  fields checked on submit, signing in and following `next`, `next` of
  another origin or outside the frame never followed, `wrong_password` and
  `too_many_attempts` with its minutes, the language buttons; a read and a
  write answered `signin_required` going to Sign in with `next`; a Sign-in
  Link's page at `/link` and at `/password` taking the secret out of the
  URL, a second link read afresh, naming the User, saying a Password set
  there replaces theirs when they have one, its count in code points after NFC and past 128, the rule and the second field checked on submit, setting the
  Password and signing in, `invalid_password` in place, `link_used` and
  `link_invalid` from either call, no fragment, and a failed read with Try
  again.
- [`account.test.tsx`](../../web/src/pages/signin/account.test.tsx), with
  [`signins.fixture.ts`](../../web/src/pages/signin/signins.fixture.ts):
  Account for a Sign-in with its Sign-in panel's Password form, Sign-ins,
  each with its method, and Sign out;
  changing the Password, the other Sign-ins ended; its fields checked on
  submit; a wrong current Password in place; through `koko web` and for
  the Local caller no form and no Sign out; Revoke asking first, its
  consequence and the focus on the list's heading; revoking this browser's
  own going to Sign in; a refused revoke reading the list again; the ended
  Sign-ins with why, those a passkey, a Linked Identity or a Sign-in Link
  ended among them, in both languages; none; a browser koko does not recognise named
  Browser; in Chinese, each browser named in Chinese on Account, in the
  Browser Sign-ins, Revoke and its notice and the account panel; Sign out
  on Account and in the account panel, its failure in the panel.
- [`person.test.tsx`](../../web/src/pages/signin/person.test.tsx): a
  person's panel with its Sign-in: the Password, Issue Sign-in Link and
  Remove Password, and their Sign-ins; a User without a Password; Issue Sign-in
  Link for 7 days, shown once with Copy and its time left, gone once
  closed, and for 15 minutes with `valid_for` left out; no Web URL;
  Remove Password asking first, then none in the panel and on Access with
  the focus on the heading, the Owner's own from a browser signed in with
  it and from one signed in with a passkey, which stays, and refused;
  another User's Sign-in revoked; Invite in a browser for 7 days, with its
  hint, and in a terminal for 1 day; no Web URL for Invite; Add device
  for 1 hour.
