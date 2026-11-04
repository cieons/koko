# RFC 8628 device authorization in Go: library or hand-written?

Research note for issue #5. Written 2026-11-05 against primary sources only:
the RFC text, package documentation, repository source and release metadata.
Every claim links to where it was read.

## Question

How should the koko server and CLI implement the OAuth 2.0 device
authorization grant (RFC 8628)? koko is one Go binary (Go 1.27,
`CGO_ENABLED=0`) that is both the authorization server and the only client
(its own CLI). A device signs in once and holds a revocable Device Session
([ADR 0005](../adr/0005-device-session-authenticates-the-ssh-session.md));
the web UI is where a person confirms a device sign-in
([ADR 0006](../adr/0006-the-cli-is-the-only-programmatic-surface.md)). There
is no third-party OAuth client and no OIDC federation in P0. The standard
library and `golang.org/x` come first; any dependency needs a reason
([code standards](../code-standards.md#go)).

## Summary and recommendation

Write it by hand. RFC 8628 adds one endpoint, one grant type, five response
fields and four error codes on top of RFC 6749; the server side is two
handlers, one table and a confirmation page, and the only Go products that
ship it (Dex, ZITADEL, Ory Hydra) each implement it in 350-800 lines.
No Go library fits koko: `golang.org/x/oauth2` is client-only;
`go-oauth2/oauth2` has no device grant at all; `ory/fosite`'s RFC 8628
handler never reached a tagged standalone release and the code now lives
inside the Hydra monorepo; `zitadel/oidc` works but is an OIDC provider
framework whose `Storage` interface demands signing keys, JWKS, userinfo and
introspection that koko has no use for, and its maintainers say feature
review capacity is limited. Both libraries would also leave the two parts
koko actually has to get right, the confirmation UI and polling rate limits,
to koko anyway. For the CLI, `golang.org/x/oauth2`'s `DeviceAuth` and
`DeviceAccessToken` implement the client side correctly and may be used; a
hand-written poll loop is about 60 lines if its one transitive dependency is
unwanted.

## What RFC 8628 requires

Source: [RFC 8628, "OAuth 2.0 Device Authorization Grant", August 2019,
Standards Track](https://www.rfc-editor.org/rfc/rfc8628.txt).

### Device authorization endpoint (section 3.1)

A new endpoint, separate from the RFC 6749 authorization endpoint. The client
`POST`s `application/x-www-form-urlencoded` with `client_id` (REQUIRED unless
the client authenticates) and `scope` (OPTIONAL). TLS is a MUST. Unrecognised
parameters MUST be ignored; parameters sent without a value are treated as
omitted; no parameter may appear twice. The client SHOULD start the flow only
when prompted by the user, never automatically, because of the polling load
([section 3.1](https://www.rfc-editor.org/rfc/rfc8628.html#section-3.1)).

### Device authorization response (section 3.2)

`200 OK`, `application/json`:

| Field | Requirement | Meaning |
| --- | --- | --- |
| `device_code` | REQUIRED | The device verification code; never shown to the user ([3.3](https://www.rfc-editor.org/rfc/rfc8628.html#section-3.3)). |
| `user_code` | REQUIRED | The end-user verification code. |
| `verification_uri` | REQUIRED | Where the user types the code; "should be short and easy to remember". |
| `verification_uri_complete` | OPTIONAL | The URI with the `user_code` embedded, for QR codes and the like. |
| `expires_in` | REQUIRED | Lifetime in seconds of both codes. |
| `interval` | OPTIONAL | Minimum seconds between polls; clients MUST default to 5 if absent. |

The example response uses `Cache-Control: no-store`, `expires_in: 1800`,
`interval: 5` and `user_code: "WDJB-MJHT"`. Errors are returned as a token
endpoint error per RFC 6749 section 5.2
([section 3.2](https://www.rfc-editor.org/rfc/rfc8628.html#section-3.2)).

### User interaction (section 3.3)

The server MUST implement an interaction that starts at `verification_uri`
and at some point takes the `user_code`; everything else (sign-in, sign-up,
extra checks) is up to the server. The server "should then inform the user
about the action they are undertaking and ask them to approve or deny the
request". Putting the `user_code` into `verification_uri` itself is NOT
RECOMMENDED. When the user arrives via `verification_uri_complete`, the server
SHOULD still display the code and ask the user to confirm it matches the
device ([section 3.3.1](https://www.rfc-editor.org/rfc/rfc8628.html#section-3.3.1)).
Clients MUST still display the `user_code` even when they show a QR code.

### Token endpoint polling (sections 3.4 and 3.5)

The client `POST`s the RFC 6749 token endpoint with
`grant_type=urn:ietf:params:oauth:grant-type:device_code`, `device_code`, and
`client_id` (REQUIRED for a public client)
([section 3.4](https://www.rfc-editor.org/rfc/rfc8628.html#section-3.4)).
Success is an RFC 6749 section 5.1 token response. Four error codes are added
to RFC 6749 section 5.2
([section 3.5](https://www.rfc-editor.org/rfc/rfc8628.html#section-3.5),
registered in [section 7.3](https://www.rfc-editor.org/rfc/rfc8628.html#section-7.3)):

| `error` | Meaning | Client behaviour |
| --- | --- | --- |
| `authorization_pending` | User has not finished. | Keep polling; wait at least `interval` (or 5 s) between requests. |
| `slow_down` | Still pending, but polling too fast. | Keep polling; "the interval MUST be increased by 5 seconds for this and all subsequent requests". |
| `access_denied` | User denied. | Stop. |
| `expired_token` | `device_code` expired; session over. | Stop; MAY start again but SHOULD wait for user interaction. |

Any other error code: the client MUST stop polling. On a connection timeout
the client MUST back off, exponential doubling RECOMMENDED. Confidential
clients MUST authenticate at the token endpoint; device clients are generally
public ([section 5.6](https://www.rfc-editor.org/rfc/rfc8628.html#section-5.6)).

### Discovery (section 4)

Optional: advertise the grant in `grant_types_supported` and the endpoint as
`device_authorization_endpoint` in RFC 8414 metadata
([section 4](https://www.rfc-editor.org/rfc/rfc8628.html#section-4)). koko's
CLI knows its server's URLs, so this is not needed in P0.

### User code format and entropy (sections 5.1 and 6.1)

- The user code is low-entropy by design; "it is recommended that the server
  rate-limit user code attempts". The worked example: an 8-character base-20
  code has roughly 34.5 bits, and to match a 2^-32 guessing probability the
  rate limit and lifetime "would need to only allow 5 attempts". Length and
  entropy are at the server's discretion given its mitigations
  ([section 5.1](https://www.rfc-editor.org/rfc/rfc8628.html#section-5.1)).
- Recommended charset: case-insensitive A-Z without vowels, the base-20 set
  `BCDFGHJKLMNPQRSTVWXZ`; dashes may be added for readability; example
  `WDJB-MJHT` (8 significant characters, 20^8). Numeric codes are also fine
  but need more digits (`019-450-730`, 10^9). On input the server should strip
  punctuation and characters outside the set, and uppercase before comparing.
  Avoid confusable characters (`0`/`O`, `1`/`l`/`I`)
  ([section 6.1](https://www.rfc-editor.org/rfc/rfc8628.html#section-6.1),
  non-normative).
- The device code is never typed, so "a very high entropy code SHOULD be
  used" ([section 5.2](https://www.rfc-editor.org/rfc/rfc8628.html#section-5.2)).

### Security considerations that shape the UI (section 5)

- **Remote phishing (5.4).** An attacker can start the flow on their own
  device and email the victim the URI and code. Mitigation: during the
  confirmation step tell the user they are authorizing a device, confirm it is
  in their possession, and "display information about the device so that the
  user could notice if a software client was attempting to impersonate a
  hardware device". With `verification_uri_complete` this matters more, since
  the user types nothing: show the code and ask them to compare. Keep the code
  lifetime long enough to be usable but short enough to limit a phished code
  ([section 5.4](https://www.rfc-editor.org/rfc/rfc8628.html#section-5.4)).
- **Session spying (5.5).** Someone who sees the code on screen can complete
  the flow first; the device should consider its environment when showing the
  code ([section 5.5](https://www.rfc-editor.org/rfc/rfc8628.html#section-5.5)).
- **Non-confidential clients (5.6).** Treat the device as a public client;
  a static client secret in a distributed binary adds nothing
  ([section 5.6](https://www.rfc-editor.org/rfc/rfc8628.html#section-5.6)).

## Library survey

Checked 2026-11-05 via the GitHub API, the Go module proxy and pkg.go.dev.

| Candidate | RFC 8628 server side | Latest release | Last commit / activity | Licence | Direct deps | Fit for one first-party client |
| --- | --- | --- | --- | --- | --- | --- |
| `golang.org/x/oauth2` | No: client only (`Config.DeviceAuth`, `Config.DeviceAccessToken`) | v0.37.0, 2026-08-25 | active (Go team) | BSD-3 | 1 (`cloud.google.com/go/compute/metadata`) | Usable for the CLI side only |
| `github.com/ory/fosite` | Yes, `handler/rfc8628`, but only on `master` after the last tag | v0.49.0, 2024-12-12 (predates the RFC 8628 commit) | last commit 2025-07-03; code merged into `ory/hydra` monorepo 2025-10-31 | Apache-2.0 | 26 in `go.mod`, incl. `ory/x`, `go-jose`, `gorilla/mux`, OpenTelemetry | Poor: full OAuth2/OIDC framework; 10-method `Client`, `Session`, ~50 config providers, access/refresh/revocation storage |
| `github.com/zitadel/oidc/v3` | Yes, `pkg/op/device.go` | v3.51.17, 2026-11-02 | 100+ commits in 2026; maintainers note limited feature-review capacity (2025-08) | Apache-2.0 | 18 in `go.mod`, incl. `chi`, `go-jose/v4`, `securecookie`, `rs/cors`, OpenTelemetry | Poor: OIDC provider framework; `Storage` needs signing keys, JWKS, userinfo, introspection; mounts 9 endpoints |
| `github.com/go-oauth2/oauth2/v4` | No device grant; issue open since 2023 | v4.6.0, 2026-09-01 | active | MIT | 7 | Not applicable |
| `github.com/dexidp/dex` | Yes, but inside a product (`server/device/device.go`, 432 lines), not a library | v2.46.1, 2026-10-21 | active | Apache-2.0 | (product) | Evidence of hand-written size, not a dependency |
| `github.com/ory/hydra/v2` | Yes (contains fosite) | v26.2.0, 2026-03-20 | active | Apache-2.0 | (product) | Importing `hydra/v2/fosite` means depending on the Hydra module |
| `openshift/osin`, `akhenakh/deviceflow`, `adderly/authgate` | osin: no `device_code`; the other two are 0-1 star personal repos | - | - | - | - | Not candidates |

## Detail per candidate

### golang.org/x/oauth2 (client only)

- The package "provides support for making OAuth2 authorized and
  authenticated HTTP requests, as specified in RFC 6749"
  ([oauth2.go](https://raw.githubusercontent.com/golang/oauth2/v0.37.0/oauth2.go)).
  pkg.go.dev lists `Config.DeviceAuth`, `Config.DeviceAccessToken`,
  `DeviceAuthResponse` and `Endpoint.DeviceAuthURL`, all added in v0.13.0, and
  nothing that serves an authorization endpoint
  ([pkg.go.dev/golang.org/x/oauth2](https://pkg.go.dev/golang.org/x/oauth2)).
- `deviceauth.go` implements the client correctly: it POSTs `client_id` and
  `scope` to `DeviceAuthURL`; `DeviceAccessToken` polls the token endpoint with
  `grant_type=urn:ietf:params:oauth:grant-type:device_code`, defaults
  `interval` to 5, adds 5 seconds on `slow_down`, keeps going on
  `authorization_pending`, stops on `access_denied`, `expired_token` or any
  other error, and bounds the loop by `expires_in` via a context deadline
  ([deviceauth.go](https://raw.githubusercontent.com/golang/oauth2/v0.37.0/deviceauth.go)).
  It does not implement the RFC's exponential back-off on connection timeout
  (a network error returns immediately).
- Latest version v0.37.0 (2026-08-25)
  ([module proxy](https://proxy.golang.org/golang.org/x/oauth2/@latest)).
  `go.mod` requires `go 1.26.0` and one module,
  `cloud.google.com/go/compute/metadata v0.3.0`
  ([go.mod](https://raw.githubusercontent.com/golang/oauth2/v0.37.0/go.mod)).
  Licence: BSD-style, Go Authors
  ([LICENSE](https://raw.githubusercontent.com/golang/oauth2/v0.37.0/LICENSE)).

### github.com/ory/fosite

- RFC 8628 support is `handler/rfc8628` (`auth_handler.go`,
  `token_handler.go`, `strategy_hmacsha.go`, `storage.go`, `strategy.go`) plus
  root `device_request*.go`, `device_response*.go`, `device_write.go`, wired by
  `compose/compose_rfc8628.go`
  ([handler/rfc8628](https://github.com/ory/fosite/tree/master/handler/rfc8628),
  [compose_rfc8628.go](https://github.com/ory/fosite/blob/master/compose/compose_rfc8628.go)).
  Non-test size: about 570 lines in the handler package and 230 in the root
  files (counted from the GitHub contents API).
- **Release gap.** The first and only commit touching `handler/rfc8628` is
  `2b393b6b` "Implement RFC 8628 (#826)" on 2025-02-03
  ([commits on that path](https://github.com/ory/fosite/commits/master/handler/rfc8628)),
  while the latest tag v0.49.0 was published 2024-12-12
  ([releases/latest](https://github.com/ory/fosite/releases/latest),
  [module proxy](https://proxy.golang.org/github.com/ory/fosite/@latest)).
  Device flow therefore exists only as an untagged pseudo-version of `master`.
- **Repository status.** Last commit on `master` 2025-07-03 (12 commits since
  2025-01-01). Hydra's `go.mod` no longer requires `github.com/ory/fosite`;
  the tree has a top-level `fosite/` directory, and the CHANGELOG records
  "Merge branch 'fosite-monorepo'" (commit `2c3ba13`, 2025-10-31) under
  release 25.4.0 (2025-11-07)
  ([hydra go.mod](https://github.com/ory/hydra/blob/master/go.mod),
  [hydra/fosite](https://github.com/ory/hydra/tree/master/fosite),
  [hydra CHANGELOG](https://github.com/ory/hydra/blob/master/CHANGELOG.md)).
  Maintained fosite is now the package `github.com/ory/hydra/v2/fosite`, so
  using it means depending on the Hydra module.
- **Footprint.** `go.mod` declares 26 direct requirements including
  `github.com/ory/x`, `go-jose/v3`, `gorilla/mux`, `gorilla/websocket`,
  `hashicorp/go-retryablehttp`, `dgraph-io/ristretto`,
  `go.opentelemetry.io/otel/trace`, `golang.org/x/oauth2`; `go 1.22`
  ([go.mod](https://github.com/ory/fosite/blob/master/go.mod)). Licence
  Apache-2.0 (repository metadata).
- **Model koko would carry.** `fosite.Client` has 10 methods (`GetID`,
  `GetHashedSecret`, `GetRedirectURIs`, `GetGrantTypes`, `GetResponseTypes`,
  `GetScopes`, `IsPublic`, `GetAudience`, ...)
  ([client.go](https://github.com/ory/fosite/blob/master/client.go));
  `fosite.Session` carries per-token expiries, username and subject
  ([session.go](https://github.com/ory/fosite/blob/master/session.go));
  `config.go` (331 lines) declares 50 `*Provider` configuration interfaces,
  composed into `fosite.Configurator` in `fosite.go`
  ([config.go](https://github.com/ory/fosite/blob/master/config.go)).
  The device token handler needs `RFC8628CoreStorage` = device sessions +
  `oauth2.AccessTokenStorage` + `oauth2.RefreshTokenStorage`, plus
  `oauth2.TokenRevocationStorage`, an `AccessTokenStrategy` and a
  `RefreshTokenStrategy`
  ([storage.go](https://github.com/ory/fosite/blob/master/handler/rfc8628/storage.go),
  [compose_rfc8628.go](https://github.com/ory/fosite/blob/master/compose/compose_rfc8628.go)).
  `NewDeviceRequest` authenticates the client, checks the client's grant
  types, scopes and audience
  ([device_request_handler.go](https://github.com/ory/fosite/blob/master/device_request_handler.go)).
- **What it still leaves to koko.** The default
  `DefaultDeviceStrategy.ShouldRateLimit` returns `false, nil`, so `slow_down`
  is never sent unless koko writes the rate limiter
  ([strategy_hmacsha.go](https://github.com/ory/fosite/blob/master/handler/rfc8628/strategy_hmacsha.go)).
  User codes are generated with `ory/x/randx` from configurable length and
  symbols; device codes are HMAC-signed `ory_dc_` tokens. The user-facing
  verification page, sign-in and approve/deny are not in fosite.

### github.com/zitadel/oidc/v3

- RFC 8628 server side is `pkg/op/device.go` (359 lines):
  `DeviceAuthorizationHandler`/`DeviceAuthorization` for the endpoint,
  `DeviceAccessToken` for the token grant, `NewDeviceCode` (16 random bytes,
  "128 bit of entropy", base64url) and `NewUserCode` with the RFC's base-20
  set (`UserCodeBase20`: 8 chars, dash every 4) or digits (`UserCodeDigits`: 9
  digits, dash every 3)
  ([device.go](https://github.com/zitadel/oidc/blob/main/pkg/op/device.go)).
  Error values `ErrAuthorizationPending`, `ErrSlowDown`, `ErrAccessDenied`,
  `ErrExpiredDeviceCode` (`expired_token`) match the RFC
  ([error.go](https://github.com/zitadel/oidc/blob/main/pkg/oidc/error.go)).
- Device storage is a two-method optional interface:
  `StoreDeviceAuthorization(ctx, clientID, deviceCode, userCode, expires, scopes)`
  and `GetDeviceAuthorizatonState(ctx, clientID, deviceCode)` (misspelling kept
  for compatibility); the grant is advertised iff the storage implements it
  ([storage.go](https://github.com/zitadel/oidc/blob/main/pkg/op/storage.go),
  [op.go `GrantTypeDeviceCodeSupported`](https://github.com/zitadel/oidc/blob/main/pkg/op/op.go)).
- **Rate limiting.** `deviceAccessToken` wraps the storage lookup in a
  4-second context and returns `slow_down` only when that deadline is
  exceeded; nothing enforces `interval` between polls or limits user-code
  guesses. koko would do both in its storage or in middleware
  ([device.go `CheckDeviceAuthorizationState`](https://github.com/zitadel/oidc/blob/main/pkg/op/device.go)).
- **Confirmation UI is not included.** The example server adds its own
  `GetDeviceAuthorizationByUserCode`, `CompleteDeviceAuthorization` and
  `DenyDeviceAuthorization` storage methods plus three chi routes and
  templates
  ([example/server/exampleop/device.go](https://github.com/zitadel/oidc/blob/main/example/server/exampleop/device.go),
  [example/server/storage/storage.go](https://github.com/zitadel/oidc/blob/main/example/server/storage/storage.go)).
- **Model koko would carry.** `op.Storage` = `AuthStorage` (auth requests,
  codes, access and refresh token creation, revocation, session termination,
  `SigningKey`, `SignatureAlgorithms`, `KeySet` returning `go-jose` keys) +
  `OPStorage` (`GetClientByClientID`, `AuthorizeClientIDSecret`, userinfo and
  introspection population, private claims, `GetKeyByIDAndClientID`,
  `ValidateJWTProfileScopes`) + `Health`
  ([storage.go](https://github.com/zitadel/oidc/blob/main/pkg/op/storage.go)).
  `op.Client` has 16 methods (redirect URIs, application type, auth method,
  response types, grant types, login URL, token type, ID token lifetime, scope
  checks, clock skew)
  ([client.go](https://github.com/zitadel/oidc/blob/main/pkg/op/client.go)).
  `op.Config` includes a `CryptoKey [32]byte` and OIDC toggles;
  `CreateRouter` mounts discovery, authorize, callback, token, introspection,
  userinfo, revocation, end-session, keys and device endpoints on a chi router
  ([op.go](https://github.com/zitadel/oidc/blob/main/pkg/op/op.go)).
- **Footprint and status.** `go.mod`: `go 1.26.0`, 18 direct requirements
  including `go-chi/chi/v5`, `go-jose/v4`, `gorilla/securecookie`, `rs/cors`,
  `go.opentelemetry.io/otel`, `golang.org/x/oauth2`, `golang.org/x/text`
  ([go.mod](https://github.com/zitadel/oidc/blob/main/go.mod)). Latest release
  v3.51.17 on 2026-11-02; 100+ commits in 2026
  ([releases/latest](https://github.com/zitadel/oidc/releases/latest),
  [module proxy](https://proxy.golang.org/github.com/zitadel/oidc/v3/@latest)).
  Maintainers posted on 2025-08-19 that they are "struggling allocating time
  for reviews of feature pull requests. Fixes and security updates still get
  our attention regularly."
  ([discussion #785](https://github.com/zitadel/oidc/discussions/785)).
  The README's certification badges are for the RP (client) side. Licence
  Apache-2.0.

### github.com/go-oauth2/oauth2/v4

- `const.go` defines `GrantType` values `authorization_code`, `password`,
  `client_credentials`, `refresh_token`, `__implicit` and
  `urn:ietf:params:oauth:grant-type:jwt-bearer`; there is no device grant
  ([const.go](https://github.com/go-oauth2/oauth2/blob/master/const.go)).
  `server/server.go`'s token request switch covers the same five
  ([server.go](https://github.com/go-oauth2/oauth2/blob/master/server/server.go)).
  A GitHub code search for `device_code` in the repository returns 0 results.
- Issue #236 "server support for device code auth flow" has been open since
  2023-02-21 with no comments
  ([issue #236](https://github.com/go-oauth2/oauth2/issues/236)).
- Latest release v4.6.0 on 2026-09-01 (adds the ID-JAG JWT bearer grant)
  ([releases/latest](https://github.com/go-oauth2/oauth2/releases/latest)).
  `go.mod`: `go 1.21`, 7 direct requirements including `golang-jwt/jwt/v5`,
  `tidwall/buntdb`, `go-session/session/v3`
  ([go.mod](https://github.com/go-oauth2/oauth2/blob/master/go.mod)). MIT.
  Not applicable to this question.

### Products with hand-written device flow (for sizing)

- **Dex** v2.46.1 (2026-10-21) implements the grant in
  `server/device/device.go`, 432 lines including the verification page,
  user-code form, upstream-connector callback and the token exchange handler
  ([server/device](https://github.com/dexidp/dex/tree/master/server/device)).
- **ZITADEL's library** does it in 359 lines plus the example UI above.
- **fosite** does it in roughly 800 lines spread over its framework
  abstractions.

## Hand-written estimate for koko

Everything below uses only the standard library and `golang.org/x`.

**Server, HTTP (two handlers, ~150 lines).**

1. `POST /api/device/code` (the device authorization endpoint). Parse the
   form, ignore unknown parameters, generate the two codes, insert one row,
   return the six JSON fields with `Cache-Control: no-store`. koko has one
   client, so `client_id` is a fixed constant checked for equality rather than
   a client registry.
2. `POST /api/token` with `grant_type=urn:ietf:params:oauth:grant-type:device_code`.
   Look the row up by device code; map its state to `authorization_pending`,
   `access_denied`, `expired_token`, or on approval mint the Device Session
   token, mark the row consumed in the same transaction (one-shot), and return
   `access_token`/`token_type: bearer`. Unknown or consumed code:
   `invalid_grant`. All errors are RFC 6749 section 5.2 JSON with HTTP 400.

**Codes (~30 lines).**

- `device_code`: `crypto/rand.Text()` gives "at least 128 bits of randomness"
  in base32 (`go doc crypto/rand.Text`, Go 1.27.1), satisfying section 5.2.
  Store only a SHA-256 of it so a copied database cannot be polled.
- `user_code`: 8 characters from `BCDFGHJKLMNPQRSTVWXZ` via
  `crypto/rand.Int`, rendered `XXXX-XXXX`; normalise input by uppercasing and
  dropping anything outside the set (section 6.1). Retry on a unique-index
  collision, as both fosite and zitadel do.

**Storage (one SQLite table, ~40 lines).**
`device_authorizations(device_code_hash PK, user_code UNIQUE, state
{pending,approved,denied,consumed}, user_id NULL, device_name, remote_addr,
created_at, expires_at, last_poll_at, poll_count, user_code_attempts)`.
Expire by `expires_at`; sweep rows older than the lifetime on a timer so
user codes can be reused (zitadel's storage comment warns about this).

**Rate limiting (~40 lines).**

- Polling: on each token request compare `now - last_poll_at` with
  `interval`; if too soon, answer `slow_down` and record it. The CLI then adds
  5 seconds. Updating `last_poll_at` in the same `UPDATE` keeps it one query.
- User-code guessing: count failed lookups per code and per source address
  in the table or with `golang.org/x/time/rate` (v0.16.0, 2026-08-19,
  no dependencies). With 34.5 bits of entropy, a 15-minute lifetime and
  ~5 attempts per address per code the RFC's 2^-32 target holds
  (section 5.1).

**Confirmation UI (~100 lines Go + one template).**
`GET /device` shows a form for the code (pre-filled from `?user_code=`),
requires the user's web session (sign-in first), then shows what is being
authorised: device name, CLI version, remote address, time started, and the
code itself for the user to compare, with Approve and Deny buttons
(sections 3.3, 5.4). `POST /device/approve` and `/device/deny` update the row
with `user_id`. Mounted on the one HTTPS port
([ADR 0004](../adr/0004-one-https-port-and-wss-tunnels.md)).

**CLI (~60 lines, or `golang.org/x/oauth2`).**
`koko login` POSTs for codes, prints the URI and the code (section 3.3
figure), opens the browser to `verification_uri_complete` when a display is
present, polls with `interval`, honours `slow_down` and `expires_in`, backs
off on network errors, stores the Device Session token. Using
`x/oauth2.Config{Endpoint: {DeviceAuthURL, TokenURL}}` with `DeviceAuth` and
`DeviceAccessToken` replaces the loop but pulls in
`cloud.google.com/go/compute/metadata`.

**Tests.** `httptest` server with the real handlers and an in-memory SQLite
file: pending, approve, deny, expiry, replay of a consumed code, `slow_down`
on fast polling, user-code normalisation, attempt limits; and the CLI driven
against the in-process server. These are the "highest seam" tests the code
standards ask for.

**Total:** roughly 400-500 lines of Go plus a template and tests, in line
with Dex's 432 lines, and all of it in koko's own vocabulary (Device Session,
User, Space) with no client registry, scopes, JWT or refresh tokens.

## What the choice costs

**Hand-written (recommended).**

- Cost: owning the protocol details, listed above. Mistakes to guard with
  tests: not enforcing `interval` (polling storms), leaking the device code
  to the user, accepting a consumed code twice, forgetting to display device
  information on the confirmation page.
- Lost: nothing koko needs in P0. Discovery metadata, scopes, refresh tokens
  and client registration can be added later if a second client ever
  appears; the RFC's shape does not change.
- Gained: zero new dependencies, a `CGO_ENABLED=0` build unaffected, an
  implementation that reads as koko (Device Session, not `fosite.Session`).

**zitadel/oidc.**

- Cost: 18 direct dependencies and their transitive graph (chi, jose v4,
  cors, OpenTelemetry, securecookie); implementing ~30 `Storage` methods of
  which koko would stub most (signing keys, JWKS, userinfo, introspection,
  auth-code flow); nine mounted endpoints of which koko wants two; the
  confirmation UI and real rate limiting still written by koko. Upstream
  feature review is self-described as limited.
- Gained: a reviewed implementation of code generation and the error table,
  roughly 100 lines of logic koko would otherwise write.

**ory/fosite.**

- Cost: everything above but larger (26 direct dependencies incl. `ory/x`,
  ~50 config providers, 10-method `Client`, access/refresh/revocation token
  storage), no tagged release containing RFC 8628, and the maintained code now
  living inside the Hydra module. Default rate-limit strategy is a no-op.
- Gained: HMAC-signed device codes and a transactional token issue path.

**go-oauth2/oauth2.** Does not implement the grant; not an option.

## Open questions

1. **Token shape.** Should the device token response carry only a Device
   Session token (`token_type: bearer`, no `refresh_token`, long-lived and
   revocable per ADR 0005), or a short-lived access token plus refresh? The
   RFC allows either; the glossary's Device Session suggests the former.
2. **Code lifetime and interval.** RFC example is 1800 s / 5 s; zitadel and
   fosite make both configurable. A shorter lifetime (10-15 min) tightens the
   phishing window at little usability cost. To be decided in the spec.
3. **Device information to display.** Which fields the CLI sends with the
   authorization request (hostname, OS, CLI version) and which the server
   records itself (remote address, time) for the confirmation page.
4. **Local callers.** [ADR 0008](../adr/0008-local-callers-act-as-the-owner.md)
   lets Unix-socket callers act as the Owner; whether `koko login` over the
   socket skips the device flow entirely belongs in the same spec.
5. **Whether to depend on `golang.org/x/oauth2` for the CLI poll loop** or
   write the ~60 lines; the only cost is `cloud.google.com/go/compute/metadata`
   in `go.sum`.

## Not verified

- Dependency counts are direct `require` lines in each `go.mod`; transitive
  graphs were not resolved with `go mod graph`.
- Line counts come from the GitHub contents API on the default branch on
  2026-11-05 and will drift.
- fosite's `fosite.Configurator` was located in `fosite.go` but its full
  method list was not read; the "50 provider interfaces" figure is a count of
  `type ...Provider interface` declarations in `config.go`.
