# The Identity Provider

The one OpenID Connect provider the Owner configures, such as Authentik,
Keycloak or Pocket ID, through which a User with a Linked Identity signs
in to the web UI
([ADR 0060](../adr/0060-a-browser-signs-in-with-a-passkey-through-the-owners-identity-provider-or-with-a-password.md),
[ADR 0061](../adr/0061-koko-verifies-webauthn-itself-and-uses-go-oidc-for-openid-connect.md)).
This page covers configuring it, its record, and every call the koko
server makes to it. The source is
[`internal/server/identity`](../../internal/server/identity/identity.go);
Sign-ins, and how one ends, are in [Sign-ins](signin.md#sign-ins).

## The record

`identity_provider` ([`passkeys_identities.sql`](../../internal/server/store/migrations/passkeys_identities.sql))
holds one row, or none: its id (`idprovider_…`), the issuer, the client ID,
the name on the button, the client secret sealed under the Root Key for the
row's id ([the Seal](seal.md)), when it was set, and the Device Session or
Sign-in that set it, NULL for the Unix socket. Every change writes a new
row under a new id, sealing the secret again for it.

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

## The calls to the provider

Every call to the provider, discovery, its keys and (with spec #228's
sign-in) the token exchange, goes through one client
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

No call runs inside a transaction.

**What is kept** ([`discovery.go`](../../internal/server/identity/discovery.go)),
in memory: the issuer's discovery from a set onwards, fetched again at the
first flow after a restart, 24 hours after the last fetch, and at the next
flow after a failure, no sooner than a minute (a flow within that minute is
refused again without a call). Of each discovery go-oidc makes its
provider, whose remote key set keeps the issuer's keys, fetching them again
when a token names a key id it lacks. `identity.Service.Discovered` hands
both to a sign-in's flow.

## Logging

At info level, once the transaction commits:

- `identity provider set`: `user`, `issuer`, `client_id`, `name`,
  `secret_changed`, `set_by` and `identities_unlinked`;
- `identity provider removed`: `user`, `issuer`, `removed_by` and
  `identities_unlinked`;
- `identity unlinked`, for each identity a change of issuer or a removal
  unlinked: `user`, `identity`, `unlinked_by` and `signins_ended`, beside
  each Sign-in's `sign-in ended`.

At warn level, `the identity provider could not be reached`, with the
call's URL without its query and the error, once per run of calls without
an answer or answered with a server error (5xx): the next other answer
ends the run.

No line, answer or error holds the client secret.

## Error codes

| Code | Status | When |
| --- | --- | --- |
| `invalid_provider` | 400 | A setting is not one koko takes, or the issuer's discovery is not valid; the message says which, and `reason` names it: `issuer_invalid`, `client_id_invalid`, `client_secret_invalid` or `name_invalid` for a setting; `no_discovery` (a 4xx), `discovery_unreadable` (not JSON, over 1 MiB, more than 3 redirects or one to `http://`), `issuer_mismatch`, `endpoint_missing` or `code_unsupported` for the discovery. |
| `provider_unreachable` | 502 | The provider did not answer in time, or answered with a server error. |
| `provider_unset` | 409 | No Identity Provider is configured. |
| `web_url_unset` | 409 | `server.oidc.set` while the Web URL is not set. |

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
- [`client_internal_test.go`](../../internal/server/identity/client_internal_test.go):
  `https://` only, the redirects, the 1 MiB answer, the unreachable line
  once per run, and the rules of what is kept, on a clock the test moves.
- The access matrix's rows ([`matrix_identity_test.go`](../../internal/server/access/matrix_identity_test.go)).
