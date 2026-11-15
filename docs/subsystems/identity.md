# The Identity Provider

The one OpenID Connect provider the Owner configures, such as Authentik,
Keycloak or Pocket ID, through which a User with a Linked Identity signs
in to the web UI
([ADR 0060](../adr/0060-a-browser-signs-in-with-a-passkey-through-the-owners-identity-provider-or-with-a-password.md),
[ADR 0061](../adr/0061-koko-verifies-webauthn-itself-and-uses-go-oidc-for-openid-connect.md)).
This page covers configuring it, its record, signing in and linking
through it, and every call the koko server makes to it. The source is
[`internal/server/identity`](../../internal/server/identity/identity.go);
Sign-ins, and how one ends, are in [Sign-ins](signin.md#sign-ins).

## The record

`identity_provider` ([`passkeys_identities.sql`](../../internal/server/store/migrations/passkeys_identities.sql))
holds one row, or none: its id (`idprovider_…`), the issuer, the client ID,
the name on the button, the client secret sealed under the Root Key for the
row's id ([the Seal](seal.md)), when it was set, and the Device Session or
Sign-in that set it, NULL for the Unix socket. Every change writes a new
row under a new id, sealing the secret again for it.

`identities` holds each Linked Identity: its id (`identity_…`), its User
(unique: at most one each), the issuer and the subject (unique together:
an identity is both, never matched by email or name), its label, what the
provider called the account when it was linked (`preferred_username`, else
`email`, else `name`, control characters dropped, at most 255 bytes, else
the subject), shown and never matched, the Sign-in or Sign-in Link it was
linked through (`linked_with`), and when it was linked and last used.
Nothing of it is sealed: a copy of the data directory signs nobody in with
it. An unlinked identity's row is deleted, and so is a Removed User's.

## The operations

| Operation | CLI | Who | Answers |
| --- | --- | --- | --- |
| `server.oidc.show` | `koko server oidc show` | the Owner and the Administrators | `{"provider", "redirect_uri"}`: the provider, or `null`, and the redirect URI, `null` while the Web URL is not set |
| `server.oidc.set` | `koko server oidc set` | the Owner alone, from a device, a Sign-in or the Local caller | `{"provider", "redirect_uri", "identities_unlinked"}` |
| `server.oidc.remove` | `koko server oidc rm` | the Owner alone | `{"identities_unlinked"}` |

A provider is answered as `{"issuer", "client_id", "name",
"client_secret_set", "linked_identities", "set_at"}`; the client secret
never is. `linked_identities` counts the Linked Identities a change of
issuer or a removal would unlink, for the web UI's confirmation; the CLI
leaves it out. The web UI's panel is in [the Access
pages](access-pages.md#what-each-may-do). The redirect
URI is the Web URL followed by `/auth/oidc/callback` (`api.RedirectPath`),
which the Owner registers at the provider. `server.show`, which answers
Members, says nothing of the provider.

An Administrator or a Member is refused `server.oidc.set` and
`server.oidc.remove` with `owner_only` before anything is read, however
wrong the call: a change of issuer unlinks every identity, the Owner's
included, and only the Owner acts on the Owner's credentials (ADR 0056).

### Setting it

`server.oidc.set` takes the four settings, each optional, and changes only
those it names. It names at least one, and the first set all four, or is
refused with `invalid_request` naming those missing. A set whose settings
are all the stored ones writes nothing and logs nothing; every other
writes the row under a new id. It is refused with `web_url_unset`
while the Web URL is not set, since the redirect URI is part of it.

| Setting | Rule, refused with `invalid_provider` |
| --- | --- |
| `issuer` | `https://` with a host, a path allowed, no query, fragment or user information, printable ASCII without spaces, at most 512 bytes. It is kept as given, since discovery and every ID token must name it byte for byte, a trailing `/` included. |
| `client_id` | 1 to 255 bytes, no control character |
| `client_secret` | 1 to 1,024 bytes, no control character, so a line ending a file kept is refused rather than sent |
| `name` | 1 to 40 characters, no control character, not starting or ending with a space |

**The discovery check.** For a new or changed issuer, the set fetches
`<issuer without its trailing />/.well-known/openid-configuration` before
its write's transaction (`op.HandlePrepared`), outside any transaction.
The document must name the same `issuer`, give `https://`
`authorization_endpoint`, `token_endpoint` and `jwks_uri`, and list `code` in
`response_types_supported`. Anything else is `invalid_provider`, saying
which; so is an answer of 4xx, one that is not JSON, one whose headers or
body are over 1 MiB, more than 3 redirects or a redirect to `http://`. No
answer within the call's bound, or an answer of 5xx, is
`provider_unreachable`. Either changes nothing. The write checks
again who calls, the Web URL and the settings; an issuer another set
changed in between, which this set did not check, is refused with
`invalid_request`. The client secret cannot be checked without a sign-in.

**A change of issuer**, the first set included, deletes every Linked
Identity in the same transaction and ends the Sign-ins made through each
(`identity_unlinked`), which the signin area ends (`signin.EndMadeWith`)
and publishes on the access-change signal, so their SSH Sessions end with
their notice. A change of the client ID, the secret or the name keeps them.

**`server.oidc.remove`** does the same, and deletes the row. With no
provider set it is refused with `provider_unset`.

## Linked Identities

| Operation | CLI | Tier | Who | Answers |
| --- | --- | --- | --- | --- |
| `identity.list` | `koko identity ls [--user <u>]` | read | one's own; anyone's for the Owner and the Administrators; a Member naming another User gets none | `{"identities"}`, at most one each: `{"id", "user", "issuer", "subject", "label", "linked_by", "linked_at", "last_used_at"}` |
| `identity.unlink` | `koko identity unlink <user>` | irreversible | one's own; another's for the Owner and the Administrators, as [`Identity.MayActOn`](../../internal/server/access/access.go) lets them | `{"identity", "signins_ended"}` |

- **`linked_by`** is the User who issued the Sign-in Link the identity was
  linked through, the Owner for one the Unix socket issued, or `null` for
  one its User linked from their own Sign-in: what promotion's
  confirmation shows (ADR 0056).
- **`identity.unlink`** deletes the row and ends the Sign-ins made through
  it (`identity_unlinked`, `signin.EndMadeWith`), and no others. Without
  `user`, or naming the caller, it unlinks the caller's own. A Member
  naming another User is refused `owner_only` before the name is read; an
  Administrator naming the Owner or another Administrator, `owner_only`; a
  name no User holds, `unknown_user`; a User without one, `no_identity`
  (409). It logs `identity unlinked` (`user`, `identity`, `unlinked_by`,
  `signins_ended`) once it commits. Unlinking at koko changes nothing at
  the provider.
- `koko identity ls` prints a column for each field, `-` for none, and
  `koko user ls`'s `SIGN-IN` says `identity` for a User who has one.

## Signing in and linking through it

The Authorization Code flow with PKCE (S256), `state` and `nonce`, as a
confidential client (OpenID Connect Core 1.0 §3.1, RFC 7636, RFC 9700),
in [`flow.go`](../../internal/server/identity/flow.go). The scopes are
`openid profile email`: `profile` and `email` give only the label; the
identity is (`iss`, `sub`). Nobody gets a koko User by signing in at the
provider: an identity signs in only once its User linked it.

### The flows

A flow has one of three purposes: **sign in**, with `next`; **link to the
Sign-in** that began it; or **link through a Sign-in Link**, whose secret
stays in the page's memory and goes into no URL. Each is held in memory
under its `state` in [the flow store](signin.md#passkeys), taken by the
first callback that names it, for 10 minutes, with the store's bounds: 20
per address, an IPv6 address counted by its /64, and 10,000 in all, a full
store dropping the oldest of the address holding the most. A restart
forgets them all, and the person starts again.

**`POST /auth/oidc/begin`** (`api.AuthOIDCBegin`) is an ordinary `/auth/`
endpoint: Origin-checked, JSON, covered by the rate limits. It:

1. checks the request: `{"purpose": "signin", "next"}`,
   `{"purpose": "link"}` with the Sign-in cookie, or `{"purpose": "link",
   "link"}`; anything else is `invalid_request`;
2. refuses with `provider_unset` while no provider is configured; for
   linking to the Sign-in, without a live one, with `signin_required`; for
   a Sign-in Link, one no longer valid with `link_used` or `link_invalid`,
   which counts for the address;
3. keeps of `next` only a path of this origin outside `/signin`, `/link`,
   `/password` and `/auth/`, once its dot segments are resolved, else `/`;
4. fetches the issuer's discovery as kept (below), outside any transaction;
5. makes `state` (the flow's id) and `nonce`, 128 random bits each, a PKCE
   verifier (`oauth2.GenerateVerifier`) and a binding token of 128 bits,
   and keeps the flow: the binding's SHA-256, the nonce, the verifier, the
   purpose, its Sign-in or link and their User, `next`, the provider's
   issuer and client ID, and the redirect URI;
6. sets the binding token in `__Host-koko-oidc` (`Secure`, `HttpOnly`,
   `SameSite=Lax`, `Path=/`, no `Domain`, `Max-Age=600`), and answers
   `{"location"}`: the authorization URL, with `response_type=code`,
   `response_mode=query`, `client_id`, `redirect_uri`, `scope`, `state`,
   `nonce`, `code_challenge` and `code_challenge_method=S256`.

The page navigates there with `window.location.assign`. `response_mode=query`
makes the provider come back with a top-level `GET`; a `form_post` would be
a cross-site `POST`, which carries no `SameSite=Lax` cookie and which the
Origin check refuses.

### The callback

**`GET /auth/oidc/callback`** is the one `/auth/` endpoint that is a `GET`,
and the one that changes state on a `GET`, so its own checks stand in for
the Origin check ([the web listener](web-front.md#the-web-listener)). It
clears `__Host-koko-oidc` whatever follows, and answers a `303` in every
case. In order:

1. **The binding.** `state` names a held flow, which it takes at once, so
   a replay finds none, and the cookie's SHA-256 is the flow's binding. A
   failure goes to `/signin?refused=signin_expired`, changes nothing, is
   logged at debug level and **counts for no limit**: any site can make a
   browser load the callback, and counting it would let another site lock
   a visitor's address out. This binding is what keeps an attacker from
   having a victim's browser finish the attacker's flow (login CSRF), or
   finishing the victim's.
2. **The limits.** An address past its limit goes to the flow's page with
   `too_many_attempts`.
3. **The provider's answer.** An `error` parameter is `provider_refused`,
   logged with the provider's code when it is one of OAuth 2.0's error
   characters and at most 64 of them, else `other`, never its
   description. Otherwise a `code` must be present.
4. **The provider** must still have the flow's issuer and client ID; a new
   name or client secret keeps the flow, and any other change is
   `signin_expired`.
5. **The exchange**: one `POST` to the token endpoint with the code, the
   redirect URI and the verifier, the client authenticating as x/oauth2
   auto-detects, `client_secret_basic` and then, after a refusal,
   `client_secret_post`. It follows **no redirect**: one would send its
   body, the client secret, the code and the verifier, on to wherever it
   points, so a `3xx` answer is a failed exchange. A `4xx`, or an answer
   without an `id_token`, is `provider_failed` at the step `exchange`; no
   answer, or a `5xx`, is `provider_unreachable`. The access and refresh
   tokens are discarded unread.
6. **The ID token.** go-oidc's verifier checks its signature, by a key of
   `jwks_uri`, with an algorithm in discovery's
   `id_token_signing_alg_values_supported` (RS256 when absent), never
   `none`. koko checks every claim itself, each failure `provider_failed`
   with its step: `iss` is the issuer byte for byte (`issuer`); `aud`
   contains the client ID, `azp`, when present, is the client ID, and is
   present when `aud` holds more than one audience (`audience`); `exp` is
   after the koko server's clock less 60 seconds, and `nbf`, when present,
   within 5 minutes of it (`expiry`); `nonce` is the flow's (`nonce`);
   `sub` is 1 to 255 bytes (`subject`). A key set that does not answer,
   or answers a server error, when a token names a key it lacks, is
   `provider_unreachable`, not a bad signature.
7. **The outcome**, in one write transaction, which checks the provider
   and the Web URL again:
   - **Signing in:** (`iss`, `sub`) names the Linked Identity of a User
     not Removed. A Sign-in starts with `method` `identity`, and the
     identity's Last used moves. Otherwise `identity_not_linked`, the same
     whether or not anyone ever had it.
   - **Linking to the Sign-in:** the Sign-in cookie names the live
     Sign-in the flow began with, else `signin_required`; the User has no
     Linked Identity, else `identity_linked`; no other User has this
     identity, else `identity_taken`. Then the row is written, and the
     Sign-in goes on.
   - **Through a Sign-in Link:** the link is still valid (`link_used` or
     `link_invalid` otherwise), and the same two checks pass. Then the row
     is written, the link is spent, the User's other Sign-ins end
     (`signin_link_used`), and this browser signs in with `method`
     `identity`. A refusal leaves the link unspent.
8. **The answer:** after signing in, a `303` to `next`, with the Sign-in
   cookie; after linking, to `/account?linked=1`, either way; after a
   refusal, to `/signin?refused=<code>`, `/account?refused=<code>` or
   `/link?refused=<code>`, by purpose. The code is not secret, and is one
   the pages word: `signin_expired`, `provider_refused`,
   `provider_failed`, `provider_unreachable`, `identity_not_linked`,
   `identity_linked`, `identity_taken`, `signin_required`, `link_used`,
   `link_invalid`, `too_many_attempts` or `internal_error`. Any other
   refusal, such as a discovery fetched again since the flow began and no
   longer valid, is `provider_failed` at the step `discovery`, logged with
   its `cause`.

Once the binding has passed, `provider_failed`, `identity_not_linked` and a
refused link count for the address ([rate limits](signin.md#rate-limits));
`provider_refused` and `provider_unreachable` count nothing, since the
person did nothing wrong. Neither counts for a name, since none is typed.

**No back-channel logout.** A sign-out at the provider ends no Sign-in at
koko, and the reverse. A person removed at the provider keeps their koko
Sign-ins until they end, unless their identity is unlinked or they are
Removed.

## The calls to the provider

Every call to the provider, discovery, its keys and the token exchange,
goes through one client
([`client.go`](../../internal/server/identity/client.go)):

- its transport is `server.Config.IdentityClient`'s, which tests replace
  to trust their in-process issuer (`harness.IdentityProvider`), or the
  system's;
- `https://` only, the first request and every redirect, with at most 3
  redirects;
- each call bounded by `timing.ProviderCall` (10 s), from its first
  request to its answer read, every redirect included, and its answer,
  headers and body, read up to 1 MiB, past which it fails rather than cut
  short;
- at most 4 calls in flight across the koko server; a fifth waits up to
  `timing.ProviderTurn` (10 s) for its turn, then fails as one with no
  answer, `provider_unreachable`.

No call runs inside a transaction. The exchange's client is the same
client, through the same bounds, with redirects refused. A call its
caller gave up on, such as a browser that left in the middle of a
sign-in, says nothing of the provider: it is not logged, and a discovery
it cut short is not kept as a failure.

**What is kept** ([`discovery.go`](../../internal/server/identity/discovery.go)),
in memory: the issuer's discovery from a set onwards, fetched again at the
first flow after a restart, 24 hours after the last fetch, and at the next
flow after a failure, no sooner than a minute (a flow within that minute is
refused again without a call). With each discovery go-oidc's remote key
set keeps the issuer's keys, fetching them again when a token names a key
id it lacks, with requests that come together sharing one fetch.

## Logging

At info level, once the transaction commits:

- `identity provider set`: `user`, `issuer`, `client_id`, `name`,
  `secret_changed`, `set_by` and `identities_unlinked`;
- `identity provider removed`: `user`, `issuer`, `removed_by` and
  `identities_unlinked`;
- `identity unlinked`, for each identity a change of issuer or a removal
  unlinked: `user`, `identity`, `unlinked_by` and `signins_ended`, beside
  each Sign-in's `sign-in ended`;
- `identity linked`: `user`, `identity`, `issuer`, `subject`,
  `linked_with` and `address`;
- `signed in` with `method` `identity` and `identity`, the identity's id;
- `sign-in refused`, with `reason`, `purpose` and `address`: `step` for
  `provider_failed`, the provider's `error` for `provider_refused`, and
  `identity_not_linked`, `provider_unreachable`, `signin_required`,
  `identity_linked` and `identity_taken` alone; `signin_expired` at debug
  level, since any site can cause it.

At warn level, `the identity provider could not be reached`, with the
call's URL without its query and the error, once per run of calls without
an answer or answered with a server error (5xx): the next other answer
ends the run.

No line, answer or error holds the client secret, a code, `state`,
`nonce`, verifier or binding token, an ID, access or refresh token, the
callback's query, or the provider's label for an account. Begin's answer
alone holds the authorization URL, with its `state`, `nonce` and
`code_challenge`, which the browser carries to the provider.

## Error codes

| Code | Status | When |
| --- | --- | --- |
| `invalid_provider` | 400 | A setting is not one koko takes, or the issuer's discovery is not valid; the message says which, and `reason` names it: `issuer_invalid`, `client_id_invalid`, `client_secret_invalid` or `name_invalid` for a setting; `no_discovery` (a 4xx), `discovery_unreadable` (not JSON, over 1 MiB, more than 3 redirects or one to `http://`), `issuer_mismatch`, `endpoint_missing` or `code_unsupported` for the discovery. |
| `provider_unreachable` | 502 | The provider did not answer in time, or answered with a server error. |
| `provider_unset` | 409 | No Identity Provider is configured. |
| `web_url_unset` | 409 | `server.oidc.set` while the Web URL is not set. |
| `provider_refused` | 403 | The provider answered the sign-in with an error, such as the person cancelling. |
| `provider_failed` | 502 | The code exchange or the ID token failed a check. |
| `identity_not_linked` | 403 | No User has this identity linked. |
| `identity_linked` | 409 | The User has a Linked Identity already. |
| `identity_taken` | 409 | Another User has this identity linked. |
| `no_identity` | 409 | `identity.unlink` named a User without a Linked Identity. |
| `signin_expired` | 400 | A callback came with no flow this browser began, after 10 minutes, or after the provider's issuer or client changed. |

The callback's refusals reach the page as `?refused=<code>`; begin's are
the API's error object.

## Verifying

- [`identity_test.go`](../../internal/server/identity/identity_test.go),
  S1 against `harness.Issuer`
  ([`issuer.go`](../../internal/harness/issuer.go)), an OpenID Connect
  provider's discovery and key set on `httptest.NewTLSServer`, with knobs
  for a spoiled document, an answer in place of it, and a hold: the Owner
  setting the provider from the Unix socket, a device and a Sign-in, and
  an Administrator and a Member refused before anything is read or the
  provider asked; the first set and the Web URL; each setting refused;
  each discovery refused, changing nothing; the secret in no file of the
  data directory, answer, error or log line; a change of the client or
  name keeping identities written by the test, and a change of issuer or a
  removal unlinking them and ending only their Sign-ins; and the bound on
  calls in flight, with another write going on while they wait.
- [`oidc_test.go`](../../internal/server/web/oidc_test.go), S1 through
  the web listener with the harness's `Browser`, which follows no redirect
  and keeps the flow's cookie, so a test drops or swaps it between steps,
  against `harness.Issuer`'s authorization endpoint (a chosen subject, or
  `access_denied`) and token endpoint (client authentication, PKCE,
  single-use codes, ES256 ID tokens with a rotating key, each claim
  spoilable): linking from Account and through a link, then signing in;
  a known subject at another issuer; `identity_linked` and
  `identity_taken`; every spoiled claim and the skew; a rotated key after
  one fetch; the binding (no cookie, another browser's, a replay, after 10
  minutes, after a restart); 50 forged callbacks; an attacker's code in a
  victim's browser; a link spent only when linking; an ended Sign-in or
  link; `next`; the limits; the exchange following no redirect; the bound
  on calls in flight with no transaction held; a silent key set; Removal
  and the provider's removal; and a scan of every log line and answer for
  each secret of the flows.
- [`client_internal_test.go`](../../internal/server/identity/client_internal_test.go):
  `https://` only, the redirects, the 1 MiB answer, the unreachable line
  once per run, a call its caller gave up on, and the rules of what is
  kept, on a clock the test moves.
- The access matrix's rows for every operation of the area, for every
  caller ([`matrix_identity_test.go`](../../internal/server/access/matrix_identity_test.go)),
  on a fixture whose Owner, other Administrator and a Member each linked
  an identity through `harness.Issuer`.
- [`identity_test.go`](../../internal/client/cli/identity_test.go) in the
  CLI: `koko identity ls` and `unlink` and `user ls` with their goldens,
  text and `--json`, and `server_too_old` from a koko server before them.
