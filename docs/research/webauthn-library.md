# Which Go WebAuthn library signs people in to the web UI with passkeys?

Research note for issue #4. Checked on 2026-11-05 against the W3C WebAuthn
Level 3 Recommendation, MDN and browser sources, the repositories, their
source and `go.mod` files, release metadata from the GitHub API and the Go
module proxy, and throwaway Go programs built with Go 1.27.1 and
`CGO_ENABLED=0`. Each claim links to what it rests on.

## Question

If the koko web UI signs people in with passkeys, which Go library
implements the WebAuthn Relying Party (RP)? The web UI is a second client of
the API, for people: recording Hosts, accepting an Invite, confirming a
device sign-in ([ADR 0006](../adr/0006-the-cli-is-the-only-programmatic-surface.md)).
koko is one Go binary with embedded SQLite, Go 1.27, `CGO_ENABLED=0`
([ADR 0001](../adr/0001-go-single-binary-with-sqlite.md)), served on one HTTPS
port ([ADR 0004](../adr/0004-one-https-port-and-wss-tunnels.md)), possibly
behind a reverse proxy. The standard library and `golang.org/x` come first;
any other dependency needs a reason ([code standards](../code-standards.md#go)).
Evaluate discoverable credentials and conditional UI, attestation (none
expected), ceremony state storage, origin and RP ID behind a proxy,
dependency footprint and maintenance. Say what the browser needs and what a
passkey-only sign-in cannot do.

## Summary and recommendation

Use **`github.com/go-webauthn/webauthn`** (v0.18.x) inside one koko package
that owns ceremony storage and the credential table. Re-check
**`filippo.io/passkey`** before implementation starts.

- go-webauthn is the only maintained Go RP library with tagged releases:
  v0.18.2 on 2026-09-19, 15 releases in the last 12 months, commits on
  2026-11-04. Authelia, Pocket ID, Authgear, Ory Kratos and Hanko depend
  on it. Its predecessor `duo-labs/webauthn` is archived and points to it.
- It covers koko's case: `BeginDiscoverableMediatedLogin` with
  `MediationConditional` for autofill, `FinishPasskeyLogin` for usernameless
  sign-in, and attestation `"none"` as the default. It builds with
  `CGO_ENABLED=0` (verified; no package in its graph has cgo files).
- Behind a proxy, nothing is read from headers. The browser signs the
  origin into `clientDataJSON` and the authenticator signs a hash of the RP
  ID. koko must be configured with its public origin and RP ID; it must not
  derive them from `Host` or `X-Forwarded-*`.
- It is not small. It pulls 9 third-party modules (CBOR, TPM, JWT,
  MessagePack, mapstructure, uuid and others) and adds about 2.7 MB to a
  stripped linux/amd64 binary, mostly attestation and metadata code koko
  will not call. It is v0, and v0.18.0 broke the API and the `SessionData`
  encoding. It leaves single-use challenges to the caller: a probe accepted
  the same `SessionData` twice.
- `filippo.io/passkey` is the passkey-only RP that Filippo Valsorda wrote as
  the prototype for a proposed standard library package `crypto/passkey`
  (golang/go#80663, open). It fits koko more closely: one dependency, about
  1,500 lines, stateless, attestation none, user verification required, one
  string per stored passkey, about 0.2 MB of binary. It has no tagged
  release, its API changed as late as 2026-10-26, and the proposal is
  undecided. It is the long-term candidate, not the P0 choice.
- A switch later need not re-enroll users. A probe converted a credential
  registered through go-webauthn (its raw `attestationObject`) into a
  passkey record that `filippo.io/passkey` verified at login. koko keeps
  that path open by storing `Credential.Attestation.Object`, which
  go-webauthn already says to persist.
- Passkeys need a domain name and a certificate the browser trusts. An IP
  address can never be an RP ID, and Chromium and Firefox refuse WebAuthn on
  pages with certificate errors. `http://localhost` works. This constrains
  the TLS decision in "Install and run the koko server" (#14).

## Comparison table

| | go-webauthn/webauthn | filippo.io/passkey | duo-labs/webauthn |
|---|---|---|---|
| What it is | FIDO2 RP library: passkey and second-factor login, attestation formats, FIDO metadata [[5]](#w5) | passkey-only RP; prototype of the proposed `crypto/passkey` [[16]](#f2) | predecessor of go-webauthn [[22]](#d1) |
| Latest release | v0.18.2, 2026-09-19; 62 releases since 2022-03-04 [[1]](#w1) | none; pseudo-version of 2026-10-26 [[17]](#f3) | none since 2022; repository archived [[22]](#d1) |
| Maintenance | active; v0, breaking changes in v0.18.0 [[2]](#w2) [[3]](#w3) | one author; API changed 2026-10-13 to 2026-10-26; proposal open [[16]](#f2) [[17]](#f3) | "THIS LIBRARY IS DEPRECATED" [[22]](#d1) |
| Passkey (discoverable) login | `BeginDiscoverableLogin`, `FinishPasskeyLogin` [[6]](#w6) | `NewLogin`, `ParseResponse`, `Login` [[19]](#f5) | - |
| Conditional UI | `BeginDiscoverableMediatedLogin(protocol.MediationConditional)` [[6]](#w6) | same options for modal and autofill [[19]](#f5) | - |
| Attestation | `"none"` is the default; packed, TPM, Android, Apple and metadata checks available [[10]](#w10) | not supported; always `"none"` [[19]](#f5) | - |
| Ceremony state | `SessionData` stored by the app; not consumed; expires only if `Enforce` is set [[7]](#w7) | login `request` stored by the app and deleted after use; registration stateless; 24 h default [[19]](#f5) | - |
| Origin and RP ID | `RPID` plus an `RPOrigins` list; per-ceremony `WithLoginOrigin`; `/.well-known/webauthn` handler [[8]](#w8) | one RP ID and one origin per `RelyingParty` [[19]](#f5) | - |
| IP address as RP ID | rejected since v0.18.0; `localhost` accepted [[9]](#w9) | rejected; `localhost` accepted [[19]](#f5) | - |
| Stored per passkey | `Credential` struct; counters and flags written back after login [[5]](#w5) [[12]](#w12) | one immutable string, the "passkey record" [[19]](#f5) [[20]](#f6) | - |
| Module deps | 9 third-party plus `x/crypto`, `x/sys`; no cgo [[4]](#w4) | `filippo.io/mldsa` only [[18]](#f4) | - |
| Binary added (linux/amd64, stripped) | +2.66 MB [[4]](#w4) | +0.20 MB [[18]](#f4) | - |
| Licence | BSD-3-Clause [[1]](#w1) | BSD-3-Clause [[17]](#f3) | BSD-3-Clause [[22]](#d1) |
| Known users | Authelia, Pocket ID, Authgear, Ory Kratos, Hanko [[13]](#w13) | none found | - |

## Detail per candidate

### github.com/go-webauthn/webauthn

- <a id="w1"></a>[1] Latest release v0.18.2, published 2026-09-19; v0.18.0
  on 2026-08-27. 62 releases since v0.1.0 (2022-03-04), 15 of them after
  2025-11-05. Repository created 2021-12-15; licence BSD-3-Clause
  (GitHub releases and repository API,
  <https://github.com/go-webauthn/webauthn/releases>,
  <https://github.com/go-webauthn/webauthn/blob/master/LICENSE>).
- <a id="w2"></a>[2] Last commit on master 2026-11-04, a CodeQL action
  bump. Of 243 commits in 2026, 109 are from Renovate and Dependabot and
  132 from James Elliott; two other people made one each. The last human
  commit is 2026-09-27. Master is 25 commits past v0.18.2, mostly
  dependency bumps and attestation fixes. Six issues are open
  (`git log` on a clone, GitHub issues API,
  <https://github.com/go-webauthn/webauthn/commits/master>).
- <a id="w3"></a>[3] README: "This library is still version 0, as per
  Semantic Versioning 2.0 rules there may be breaking changes without
  warning"; supported Go versions are 1.27 and 1.26; "conformance tested
  against the conformance tools". The v0.18.0 notes list 12 breaking
  changes, among them: "An IP address configured as a relying party id is
  rejected", options now return errors, and "`SessionData.Extensions`
  changes both its type and its encoded representation, so a session
  persisted by an earlier version cannot be decoded by this one"
  (<https://github.com/go-webauthn/webauthn/blob/v0.18.2/README.md>,
  <https://github.com/go-webauthn/webauthn/releases/tag/v0.18.0>,
  <https://github.com/go-webauthn/webauthn/blob/master/MIGRATION.md>).
- <a id="w4"></a>[4] `go.mod` at v0.18.2: `go 1.26.0`; requires
  `fxamacker/cbor/v2`, `go-viper/mapstructure/v2`, `go-webauthn/x`,
  `golang-jwt/jwt/v5`, `google/go-tpm`, `google/uuid`, `tinylib/msgp`,
  plus `stretchr/testify` and `go.uber.org/mock` for tests
  (<https://github.com/go-webauthn/webauthn/blob/v0.18.2/go.mod>). A probe
  importing `webauthn` and running `go mod tidy` gets 11 modules:
  those runtime ones plus `philhofer/fwd`, `x448/float16`,
  `golang.org/x/crypto` (for `ocsp`) and `golang.org/x/sys` (for
  `go-tpm`'s transport). `go list -deps` with `CGO_ENABLED=1` finds no
  package with cgo files; `msgp` and `fwd` import `unsafe`. Stripped
  linux/amd64 builds of the same `net/http` program: 4,620,448 bytes
  without a WebAuthn library, 7,282,848 bytes with go-webauthn running both
  ceremonies.
- <a id="w5"></a>[5] `webauthn/doc.go` names the flows: registration via
  `BeginRegistration`/`FinishRegistration`, passkey login via
  `BeginDiscoverableLogin`/`FinishPasskeyLogin`, second-factor login via
  `BeginLogin`/`FinishLogin`. "The `SessionData` must be saved in a way
  which allows the implementer to restore it later ... safely anchored to a
  user agent without allowing the user agent to modify the contents". Its
  Storage section asks for one row per credential keyed by
  (RP ID, credential ID), a separate per-user User Handle, and that sign
  count, clone warning, user-verified and backup-state values "MUST be
  written back to storage on every successful FinishLogin". It warns that
  a `SessionData` serialized by an older release "will not decode cleanly
  after such an upgrade, so in-flight sessions must be drained"
  (<https://github.com/go-webauthn/webauthn/blob/v0.18.2/webauthn/doc.go>).
- <a id="w6"></a>[6] `webauthn/login.go`: `BeginDiscoverableLogin(opts...)`
  and `BeginDiscoverableMediatedLogin(mediation, opts...)` start a login
  with no user and no `allowCredentials`. `FinishPasskeyLogin(handler,
  session, r)` returns `(User, *Credential, error)`; the
  `DiscoverableUserHandler` receives the raw credential ID and the
  unverified User Handle and must return the owner. `ValidatePasskeyLogin`
  refuses a session begun for a known user and a blank User Handle. The
  mediation value is serialized next to `publicKey` in the options JSON
  (`protocol/options.go`, `CredentialAssertion`;
  <https://github.com/go-webauthn/webauthn/blob/v0.18.2/webauthn/login.go>,
  <https://github.com/go-webauthn/webauthn/blob/v0.18.2/protocol/options.go>).
- <a id="w7"></a>[7] `SessionData` holds the challenge, RP ID, optional
  bound origin, user ID, allowed credential IDs, `Expires`, user
  verification, extensions and mediation. "After the ceremony completes,
  the session data should be discarded"
  (<https://github.com/go-webauthn/webauthn/blob/v0.18.2/webauthn/types_session.go>).
  `Expires` is set only when `Config.Timeouts.Login.Enforce` (or
  `.Registration.Enforce`) is true; the defaults are 300 s (120 s when user
  verification is discouraged), sent to the browser as `timeout`
  (`webauthn/const.go`, `webauthn/types.go`). A probe ran
  `ValidatePasskeyLogin` twice with the same `SessionData` and the same
  assertion: both succeeded. Single use is the caller's job.
- <a id="w8"></a>[8] `Config` requires at least one `RPOrigins` entry
  ("must provide at least one value"); `RPID` "should generally be the
  origin without a scheme and port". Origins are compared on scheme and
  host including port, case-insensitively, with `:443` and `:80`
  normalized away (`protocol/client.go`, `IsOriginInHaystack`).
  `RPAllowCrossOrigin` is off by default. `WithLoginOrigin` and
  `WithRegistrationOrigin` bind a ceremony to one of the configured
  origins; `WithLoginRelyingPartyID` and `WithRegistrationRelyingPartyID`
  override the RP ID per ceremony. `WebAuthn.RelatedOrigins()` returns an
  `http.Handler` for `/.well-known/webauthn`
  (<https://github.com/go-webauthn/webauthn/blob/v0.18.2/webauthn/types.go>,
  <https://github.com/go-webauthn/webauthn/blob/v0.18.2/protocol/client.go>,
  <https://github.com/go-webauthn/webauthn/blob/v0.18.2/webauthn/login_opt.go>,
  <https://github.com/go-webauthn/webauthn/blob/v0.18.2/webauthn/related_origins.go>).
- <a id="w9"></a>[9] `protocol.ValidateRPID` returns "the value must be a
  domain and not an IP address" for any `net.ParseIP` match and rejects
  schemes, ports and paths. A probe: `127.0.0.1`, `192.168.1.10` and `::1`
  fail `webauthn.New`; `localhost` and `koko.example.com` pass
  (<https://github.com/go-webauthn/webauthn/blob/v0.18.2/protocol/utils.go>,
  <https://github.com/go-webauthn/webauthn/blob/v0.18.2/protocol/errors.go>).
- <a id="w10"></a>[10] `protocol.PreferNoAttestation` (`"none"`): "This
  is the default value." `Config.MDS` (FIDO Metadata Service) is optional
  and nil by default. A probe registered a virtual authenticator's packed
  self-attestation with `AttestationPreference: PreferNoAttestation`; the
  credential came back as format `packed`, type `basic_surrogate`
  (<https://github.com/go-webauthn/webauthn/blob/v0.18.2/protocol/options.go>,
  <https://github.com/go-webauthn/webauthn/blob/v0.18.2/webauthn/types.go>).
- <a id="w11"></a>[11] `WithResidentKeyRequirement` "sets both the
  resident key and require resident key protocol options". Setting only
  `Config.AuthenticatorSelection.ResidentKey` sent
  `"residentKey":"required"` without `requireResidentKey` in a probe; use
  the option
  (<https://github.com/go-webauthn/webauthn/blob/v0.18.2/webauthn/registration_opt.go>).
- <a id="w12"></a>[12] `Credential` holds ID, COSE public key,
  attestation type and format, transports, flags, authenticator (AAGUID,
  sign count, clone warning, attachment), `Attestation` and extensions.
  For `CredentialAttestation`: "Implementers MUST persist all fields of
  this struct." `Attestation.Object` is the raw `attestationObject`;
  `Attestation.AuthenticatorData` is filled only from the Level 3
  `response.authenticatorData` field (empty in a probe whose client did
  not send it; `Object` was 278 bytes)
  (<https://github.com/go-webauthn/webauthn/blob/v0.18.2/webauthn/credential.go>).
- <a id="w13"></a>[13] Default-branch `go.mod` files on 2026-11-05:
  `authelia/authelia` and `pocket-id/pocket-id` (backend) require v0.18.2,
  `authgear/authgear-server` v0.15.0, `ory/kratos` v0.11.2,
  `teamhanko/hanko` (backend) v0.10.2; `egregors/passkey` wraps v0.17.4
  (GitHub contents API).
- <a id="w14"></a>[14] No advisory names `github.com/go-webauthn/webauthn`
  in the GitHub advisory database or in the Go vulnerability database's
  module index on 2026-11-05. The index lists past issues in
  `go-viper/mapstructure/v2` (fixed 2.4.0), `golang-jwt/jwt/v5` (fixed
  5.2.2) and `google/go-tpm` (fixed 0.3.0), all below the versions
  go-webauthn requires (<https://vuln.go.dev/index/modules.json>).

### filippo.io/passkey (proposed crypto/passkey)

- <a id="f1"></a>[15] golang/go#71095 "proposal: crypto/webauthn: webauthn
  signature verification API" has been open since 2025-01-02. On
  2026-07-31 Filippo Valsorda wrote there: "I made a higher-level proposal
  in #80663" (<https://github.com/golang/go/issues/71095>).
- <a id="f2"></a>[16] golang/go#80663 "proposal: crypto/passkey: new
  package", opened 2026-07-31, open, label `Proposal`, 64 comments, last
  API change posted 2026-10-26. It proposes "a stateless, callback-less,
  interface-less API for passkey authentication ... for small and
  medium-sized websites". On 2026-08-09 the author answered a question
  about `golang.org/x/crypto`: "nothing will be added to x/crypto ... The
  choice is standard library or third-party module." There is no
  `golang.org/x` WebAuthn package (<https://github.com/golang/go/issues/80663>).
- <a id="f3"></a>[17] The module proxy lists no tagged version;
  `@latest` is `v0.0.0-20261026202536-7f1a37199ee0` from
  `github.com/FiloSottile/passkey`. The repository (created 2026-08-17,
  first commit 2026-08-02) has 42 commits, all by Filippo Valsorda; the
  last three (2026-10-26) changed the default timeout, user verification
  rules and request-storage guidance. Licence BSD-3-Clause ("Copyright
  2009 The Go Authors")
  (<https://proxy.golang.org/filippo.io/passkey/@latest>,
  <https://github.com/FiloSottile/passkey/commits/main>,
  <https://github.com/FiloSottile/passkey/blob/main/LICENSE>).
- <a id="f4"></a>[18] `go.mod`: `go 1.25.0`, one requirement,
  `filippo.io/mldsa` at a 2026-07-11 pseudo-version (mldsa tagged v1.0.0
  on 2026-09-24). Non-test files import only the standard library, mldsa
  and an internal CTAP2 CBOR subset; they total 1,516 lines. The same
  stripped linux/amd64 program as in [[4]](#w4) is 4,825,248 bytes with
  this package running both ceremonies
  (<https://github.com/FiloSottile/passkey/blob/main/go.mod>,
  <https://proxy.golang.org/filippo.io/mldsa/@latest>).
- <a id="f5"></a>[19] Package doc (as posted in #80663 and in
  `passkey.go`): discoverable credentials only; registration options send
  `residentKey: "required"`, user verification `"required"`, attestation
  `"none"` and `credProps`; "Attestation is not supported. It's an
  enterprise feature completely absent from the consumer passkey
  ecosystem." Registration "has no request value"; the app ties it to the
  signed-in session and protects it against CSRF. `NewLogin` returns a
  `request` the app stores "and, if possible, delete[s] it after use, so
  that each request is accepted at most once"; the default lifetime is
  24 h because "conditional UI (autofill) prompts can sit idle for a long
  time". "To accept ceremonies from multiple origins, create a
  RelyingParty per origin." Cross-origin (iframe) logins are not
  supported. `NewRelyingParty` rejects an IP address as RP ID; a probe
  confirmed `127.0.0.1` fails and `localhost` passes
  (<https://github.com/FiloSottile/passkey/blob/main/passkey.go>,
  <https://github.com/golang/go/issues/80663>).
- <a id="f6"></a>[20] Passkey records
  (`$webauthn$v=1$transports=...$<base64 authenticator data>`) are
  specified in C2SP; PR C2SP/C2SP#301 "passkey-record: new draft" merged
  2026-08-24. The repository has no tag for this spec
  (<https://github.com/C2SP/C2SP/blob/main/passkey-record.md>,
  <https://github.com/C2SP/C2SP/pull/301>).
- <a id="f7"></a>[21] Migration probe: register through go-webauthn
  v0.18.2 with a virtual authenticator, take `authData` out of
  `Credential.Attestation.Object` with go-webauthn's own CBOR decoder,
  wrap it as `$webauthn$v=1$transports=internal$<base64>`, then sign in
  through `filippo.io/passkey`: `Login` returned `Matched:0` with no error.

### github.com/duo-labs/webauthn

- <a id="d1"></a>[22] Archived; last commit 2022-12-05, "Emphasize
  deprecated status". README: "THIS LIBRARY IS DEPRECATED. Projects using
  this library should consider migrating to the community-maintained fork
  over at github.com/go-webauthn/webauthn." Licence BSD-3-Clause
  (<https://github.com/duo-labs/webauthn>).

### Not candidates

- <a id="o1"></a>[23] From GitHub searches for Go repositories on
  "webauthn" and "passkey" (2026-11-05):
  `fxamacker/webauthn` (last release v0.6.1 2020-03-08, last commit
  2020-03-12), `e3b0c442/warp` (v0.6.1 2020-04-13, last commit
  2021-01-01) and `koesie10/webauthn` (archived) are unmaintained.
  `aethiopicuschan/passkey-go` has no release and no push since
  2025-07-04. `egregors/passkey` is a wrapper over go-webauthn, not an RP
  implementation. `teamhanko/passkeys` is an AGPL-3.0 server, not a
  library. The other results are products (Authelia, Casdoor, Kratos,
  Pocket ID) that embed go-webauthn.

### Testing without a browser

- <a id="v1"></a>[24] `github.com/descope/virtualwebauthn` is a test
  helper, not an RP: it "provides a set of helper tools for testing full
  WebAuthn authentication flows in a relying party WebAuthn server
  implementation without requiring a browser or an actual
  authenticator". It parses creation and request options and produces
  attestation (packed only) and assertion responses with EC2 or RSA keys.
  Latest tag v1.0.5 (2026-05-10); bot commits through 2026-10-27; MIT. Its
  root package imports only `fxamacker/cbor` and the standard library
  (<https://github.com/descope/virtualwebauthn>,
  <https://github.com/descope/virtualwebauthn/blob/main/README.md>).
- <a id="v2"></a>[25] Probe, `CGO_ENABLED=0 go test`: register, then
  `BeginDiscoverableMediatedLogin(MediationConditional)` and
  `ValidatePasskeyLogin` succeed with a virtual authenticator holding the
  User Handle. An assertion whose client data says
  `http://127.0.0.1:8080` fails with "Error validating origin"; an RP
  configured for a different RP ID rejects the old credential. The same
  helper also drove `filippo.io/passkey` through registration and login.
  `filippo.io/passkey` has its own unexported software authenticator and
  replays of Chrome ceremonies in its tests
  (<https://github.com/FiloSottile/passkey/blob/main/authenticator_test.go>,
  <https://github.com/FiloSottile/passkey/blob/main/chrome_test.go>). Such
  tests fit the code standards: an in-process `httptest` server, no
  network, no real machine state.
- Browser end-to-end tests could use the spec's WebDriver "Add Virtual
  Authenticator" command
  (<https://www.w3.org/TR/webauthn-3/#sctn-automation-add-virtual-authenticator>);
  that needs a browser and stays out of `make test`.

## RP ID, origin and the reverse proxy

- <a id="s1"></a>[26] WebAuthn Level 3 is a W3C Recommendation dated
  25 August 2026 (<https://www.w3.org/TR/2026/REC-webauthn-3-20260825/>).
- <a id="s2"></a>[27] An RP ID "is a valid domain string". It must equal
  the origin's effective domain or be a registrable domain suffix of it,
  and the origin must be `https`, or `http` with host `localhost`. "A
  public key credential can only be used for authentication with the
  same entity (as identified by RP ID) it was registered with." Example:
  for `https://login.example.com:1337`, `login.example.com` and
  `example.com` are valid RP IDs, `com` is not
  (<https://www.w3.org/TR/webauthn-3/#rp-id>).
- <a id="s3"></a>[28] `create()` and `get()`: "If effective domain is not
  a valid domain, then throw a 'SecurityError' DOMException. Note: ...
  Only the domain format of host is allowed here", not IPv4 or IPv6
  addresses
  (<https://www.w3.org/TR/webauthn-3/#dom-publickeycredential-create-slot>,
  <https://www.w3.org/TR/webauthn-3/#dom-publickeycredential-discoverfromexternalsource-slot>).
- <a id="s4"></a>[29] "user agents only expose this API to callers in
  secure contexts. For web contexts in particular, this only includes
  those accessed via a secure transport (e.g., TLS) established without
  errors" (<https://www.w3.org/TR/webauthn-3/#web-authentication-api>).
- <a id="s8"></a>[30] "the Relying Party MUST validate the origin member
  of the client data"; a site served only at one origin "SHOULD require
  origin to exactly equal" it
  (<https://www.w3.org/TR/webauthn-3/#sctn-validating-origin>).
- <a id="s9"></a>[31] Related Origin Requests: an RP may let other
  origins use its RP ID by serving JSON `{"origins": [...]}` at
  `/.well-known/webauthn` on the RP ID's host over HTTPS; clients must
  support at least five registrable labels
  (<https://www.w3.org/TR/webauthn-3/#sctn-related-origins>).

What this means for koko:

- The origin the RP checks is whatever the browser's address bar shows,
  taken from signed `clientDataJSON` [[30]](#s8), and the RP ID is checked
  through its hash in the signed authenticator data [[25]](#v2). A proxy
  cannot change either, and koko should not try to reconstruct them from
  `Host` or `X-Forwarded-Host`. koko needs one configured public URL, for
  example `https://koko.example.com` or `https://koko.example.com:8443`;
  the origin is that URL's scheme, host and port, and the RP ID is its
  host [[8]](#w8).
- The RP ID is part of every passkey. Moving the koko server to a new host
  name strands every registered passkey [[27]](#s2) unless the old host
  keeps serving `/.well-known/webauthn` that lists the new origin
  [[31]](#s9). Choosing a parent domain (`example.com`) as RP ID survives
  renames within it, but every subdomain could then use koko's passkeys.
  The exact host is the safer default for a single-purpose server.
- An IP address cannot be an RP ID [[28]](#s3) [[9]](#w9). A koko server
  reached only as `https://203.0.113.5` cannot offer passkey sign-in.
  `http://localhost` is a valid origin [[27]](#s2), so an SSH port
  forward to the server works; passkeys made there belong to RP ID
  `localhost` and do not work at the public name.
- <a id="b4"></a>[32] Chromium returns `NotAllowedError` "WebAuthn is not
  supported on sites with TLS certificate errors" when
  `IsSecurityLevelAcceptable` fails for the caller
  (<https://chromium.googlesource.com/chromium/src/+/refs/heads/main/third_party/blink/renderer/modules/credentialmanagement/authentication_credentials_container.cc>,
  <https://chromium.googlesource.com/chromium/src/+/refs/heads/main/content/browser/webauth/authenticator_common_impl.cc>).
- <a id="b5"></a>[33] Firefox bug 1954033 (CVE-2025-6433), fixed in
  Firefox 140: WebAuthn is disallowed after an untrusted-issuer or
  domain-mismatch certificate override; an expired-certificate override
  is still allowed (<https://bugzilla.mozilla.org/show_bug.cgi?id=1954033>).
- So a self-signed certificate that only the CLI pins leaves browsers
  unable to use passkeys unless the user installs the CA. A publicly
  trusted certificate (ACME) or a trusted private CA is needed for the web
  UI.

## What the browser side needs

- <a id="b1"></a>[34] MDN browser-compat-data (`api/PublicKeyCredential.json`,
  last changed 2026-08-18): `parseCreationOptionsFromJSON`,
  `parseRequestOptionsFromJSON` and `toJSON` from Chrome 129, Firefox 119,
  Safari 18.4; `isConditionalMediationAvailable` from Chrome 108,
  Firefox 119, Safari 16; `getClientCapabilities` from Chrome 133,
  Firefox 135, Safari 17.4; `signalUnknownCredential` and
  `signalAllAcceptedCredentials` from Chrome 132 and Safari 26, not in
  Firefox
  (<https://github.com/mdn/browser-compat-data/blob/main/api/PublicKeyCredential.json>).
- <a id="b2"></a>[35] MDN: `parseCreationOptionsFromJSON` takes "an object
  with the same structure as a PublicKeyCredentialCreationOptions, but
  with base64url-encoded strings used in place of buffer properties";
  Baseline 2025, "Since March 2025". `toJSON()` "is automatically called
  by JSON.stringify()"
  (<https://developer.mozilla.org/en-US/docs/Web/API/PublicKeyCredential/parseCreationOptionsFromJSON_static>,
  <https://developer.mozilla.org/en-US/docs/Web/API/PublicKeyCredential/toJSON>).
- <a id="s5"></a>[36] With `mediation: "conditional"` the client empties
  `allowCredentials` and sets the ceremony timer "to a value of infinity"
  so the user has "the entire lifetime of the Document" to pick a passkey
  from an input whose autocomplete holds the `webauthn` token, which
  "must appear immediately after the last autofill detail token", as in
  `"username webauthn"`
  (<https://www.w3.org/TR/webauthn-3/#dom-publickeycredential-discoverfromexternalsource-slot>).
  Clients report `conditionalGet` through `getClientCapabilities()`
  (<https://www.w3.org/TR/webauthn-3/#dom-clientcapability-conditionalget>).
- <a id="b3"></a>[37] Chrome's guide: put `autocomplete="username webauthn"`
  on the username input; detect support with
  `getClientCapabilities().conditionalGet`; call `get()` with
  `mediation: 'conditional'` and an empty `allowCredentials` "when the
  sign-in page loads"; keep an `AbortController` to cancel it before
  another WebAuthn call; "generate a fresh challenge for every sign-in
  attempt" (<https://web.dev/articles/passkey-form-autofill>).

So the web UI needs, with no npm build:

1. Pages on the configured origin over a TLS connection the browser
   trusts, or on `http://localhost` [[29]](#s4).
2. One hand-written JavaScript file. Registration: fetch options, call
   `navigator.credentials.create({publicKey:
   PublicKeyCredential.parseCreationOptionsFromJSON(opts.publicKey)})`,
   POST `JSON.stringify(credential)`. Sign-in: on page load, if
   `conditionalGet` is available, fetch options and arm
   `navigator.credentials.get({publicKey: ..., mediation: "conditional",
   signal})`; a "Sign in with a passkey" button aborts it and runs a
   modal `get()`. go-webauthn's options JSON wraps the options in
   `publicKey` and carries `mediation` beside it [[6]](#w6); its `user.id`
   and `challenge` are base64url, which the parse functions expect
   [[35]](#b2).
3. One input with `autocomplete="username webauthn"` on the sign-in page,
   even though no username is needed [[36]](#s5).
4. A server-side login ceremony that outlives an idle page. The conditional
   request never times out in the browser [[36]](#s5), while go-webauthn
   with `Enforce` expires the session at its `Timeout` [[7]](#w7). Either
   use a long login timeout or have the script fetch new options and
   re-arm when the server answers "expired".
   `filippo.io/passkey` defaults to 24 h for this reason [[19]](#f5).
5. Browsers older than March 2025 lack the parse and `toJSON` functions
   [[34]](#b1); supporting them costs about 20 lines of base64url helpers.

## How passkey sign-in fits the device grant

The resolved ticket "Research: RFC 8628 device authorization in Go" (#5)
puts the device confirmation page in the web UI and requires a signed-in
browser there. With passkeys:

1. `koko login` on any device, headless or not, prints the verification URI
   and user code and polls.
2. The person opens the URI on a laptop or phone. If the browser is not
   signed in, the sign-in page offers the passkey through autofill or a
   button. A phone that scanned the QR code of `verification_uri_complete`
   uses its own synced passkey.
3. The confirmation page shows the device details and code; Approve mints
   the Device Session for the CLI.

The passkey never has to exist on the device that gets the Device Session.
That is how koko covers SSH-only servers, CI runners and agents: they get
Device Sessions through RFC 8628, never passkeys. Whether Approve also asks
for a fresh passkey assertion is an open question below.

## What a passkey-only sign-in cannot do

- <a id="s10"></a>[38] "Losing an authenticator therefore, in general,
  means losing all credentials bound to the lost authenticator, which
  could lock the user out ... Relying Parties SHOULD allow and encourage
  users to register multiple credentials"
  (<https://www.w3.org/TR/webauthn-3/#sctn-credential-loss-key-mobility>).
  A credential with backup eligibility 0 "is a single-device credential
  ... Relying Parties SHOULD ensure that each user account has additional
  authenticators registered and/or an account recovery process in place"
  (<https://www.w3.org/TR/webauthn-3/#sctn-credential-backup>).
- <a id="b6"></a>[39] passkeys.dev (last updated 2026-02-02): a synced
  passkey "can reliably be used for bootstrapping sign-in"; a
  device-bound passkey "is bound to a single authenticator", as on
  security keys; "FIDO Cross-Device Authentication (CDA) allows a passkey
  from one device to be used to sign in on another device", over the
  "hybrid" transport (<https://passkeys.dev/docs/reference/terms/>).
- <a id="b7"></a>[40] CTAP 2.3 (2026-02-26), section 11.5: in hybrid,
  "Bluetooth LE (BLE) advertisements are used for proof of proximity"
  (<https://fidoalliance.org/specs/fido-v2.3-ps-20260226/fido-client-to-authenticator-protocol-v2.3-ps-20260226.html>).
- <a id="s11"></a>[41] "the Relying Party MUST NOT include personally
  identifying information, e.g., e-mail addresses or usernames, in the
  user handle ... It is RECOMMENDED to let the user handle be 64 random
  bytes" (<https://www.w3.org/TR/webauthn-3/#sctn-user-handle-privacy>).

So, without something besides passkeys:

- **Recovery.** A User who loses every passkey is locked out
  [[38]](#s10). koko needs a way to issue a one-time link that registers a
  new passkey for an existing User. For the Owner, the natural break-glass
  is a local caller on the koko server, which already acts as the Owner
  ([ADR 0008](../adr/0008-local-callers-act-as-the-owner.md)).
- **First sign-in.** A passkey can only be registered by someone already
  identified. The first Owner and every Invite need a one-time link that
  creates the User and their first passkey in one step.
- **A second device.** A synced passkey follows the user's platform
  account; a security key does not [[39]](#b6). A new device outside that
  account signs in with a phone over hybrid, which needs Bluetooth on the
  new device [[40]](#b7), then registers its own passkey. koko should
  list a User's passkeys and let them add and remove them, and use
  `excludeCredentials` so one authenticator is not registered twice
  [[38]](#s10).
- **Headless devices and agents.** They have no browser and no
  authenticator. They use Device Sessions through RFC 8628, with the
  approval done in a browser elsewhere.
- **IP-only or self-signed servers.** No passkeys without a domain name
  and a browser-trusted certificate, except over `http://localhost`
  [[28]](#s3) [[32]](#b4) [[33]](#b5).
- **Renaming the server.** Passkeys stay with the old RP ID [[27]](#s2).
- **Removing a passkey everywhere.** Deleting a credential on the server
  leaves it in the user's password manager. Signal methods can tell the
  client, but Firefox lacks them [[34]](#b1).

## What the choice costs

- Nine third-party modules (`fxamacker/cbor/v2`, `x448/float16`,
  `go-viper/mapstructure/v2`, `go-webauthn/x`, `google/go-tpm`,
  `golang-jwt/jwt/v5`, `google/uuid`, `tinylib/msgp`, `philhofer/fwd`)
  and about 2.7 MB of binary, mostly attestation, TPM and metadata code
  koko will not call [[4]](#w4). The reason for the commit body: WebAuthn
  RP verification needs CBOR, COSE keys and signature checks; neither the
  standard library nor `golang.org/x` has it [[16]](#f2), and go-webauthn
  is the only maintained Go RP library with releases.
- v0 breaking changes [[3]](#w3). Each upgrade means reading
  `MIGRATION.md`; stored `SessionData` from the old version must be
  dropped. Short-lived ceremony rows make that a table truncation.
- koko owns what the library leaves out: a ceremony table in SQLite whose
  row is deleted when Finish reads it (the probe in [[7]](#w7) shows why),
  `Enforce` turned on, write-back of sign count and flags after each login
  [[5]](#w5), a random User Handle per User [[41]](#s11),
  `WithResidentKeyRequirement` [[11]](#w11), CSRF protection on the
  passkey registration endpoint, and the JavaScript file.
- <a id="g1"></a>[42] Go's own CSRF guard, `net/http.CrossOriginProtection`,
  uses `Sec-Fetch-Site` and falls back to "comparing the hostname of the
  Origin header with the Host header" (`go doc
  net/http.CrossOriginProtection`, Go 1.27.1). Behind a proxy that
  rewrites `Host`, that fallback needs the public origin added as trusted.
- Concentration: one maintainer wrote 132 of the 134 human commits in
  2026 [[2]](#w2). Five Go identity products depend on the library
  [[13]](#w13), which spreads the pressure to keep it alive.
- Keeping `Credential.Attestation.Object` (required anyway [[12]](#w12))
  keeps a later move to `filippo.io/passkey` or `crypto/passkey` a data
  conversion rather than a re-enrollment [[21]](#f7).
- Tests drive both ceremonies in process with
  `descope/virtualwebauthn` as a test-only dependency [[24]](#v1)
  [[25]](#v2), or with a few dozen lines of koko's own software
  authenticator.

## Open questions

- **Recovery and fallback.** Passkey only, or passkey plus another way in?
  How the first Owner gets a registration link (a local caller, ADR 0008),
  how an Invite carries the first passkey registration, and how a User who
  lost every passkey gets a new one. Handed to "Sign-in and identity:
  Owner bootstrap, Invite, web sign-in, Device Session lifecycle" (#9).
- **Step-up at device approval.** Whether approving a device asks for a
  fresh passkey assertion, and whether a browser sign-in is itself a
  Device Session or a separate web session. Handed to #9.
- **Ceremony and session storage.** One SQLite table for pending
  ceremonies, consumed once, versus a signed cookie;
  `filippo.io/passkey`'s author recommends against client-side storage
  [[19]](#f5). Handed to #9.
- **Public URL, RP ID and TLS.** A required public URL setting; exact host
  versus parent domain as RP ID; what the server does when the configured
  host is an IP address; and that a CLI-pinned self-signed certificate
  rules out browser passkeys. Handed to "Install and run the koko server"
  (#14).
- **Browser floor and pages.** Whether the web UI requires the
  parse/`toJSON` functions (March 2025 browsers) or ships base64url
  helpers; the sign-in page with autofill; a page to list, add and remove
  passkeys. Handed to "P0 web UI scope" (#13).
- **When to re-check `filippo.io/passkey`.** If golang/go#80663 is
  accepted or the module tags a release before koko's sign-in is built,
  compare again; it would remove nine modules and most of the storage
  rules. Handed to "Write the P0 specs" (#17), so the people-and-Spaces
  spec records the re-check.

## Not verified

- No real browser or authenticator was used. Ceremonies ran against
  `descope/virtualwebauthn` with EC2 keys and packed self-attestation.
  How Safari, Firefox and Chrome treat go-webauthn's options (for example
  `credProps`, hints, missing `requireResidentKey`) was not tested.
- Browser support for Related Origin Requests is not in MDN's compat
  data and was not checked per browser. MDN's data also lists Safari as
  lacking `residentKey`, which looks inconsistent with Safari's passkey
  support; not resolved.
- Binary sizes come from small linux/amd64 programs, stripped; koko's real
  binary will already contain some of the shared code (`x/crypto`).
- Module counts come from `go mod tidy` and `go list -deps` on the
  `webauthn` package only; `metadata/providers` was not imported.
- go-webauthn's "conformance tested" claim was not checked against FIDO's
  certification lists.
- The Chromium certificate-error check was read on the main branch on
  2026-11-05; the release that introduced it was not determined.
- Hybrid's Bluetooth requirement comes from CTAP 2.3; how each operating
  system behaves with Bluetooth off was not checked.
