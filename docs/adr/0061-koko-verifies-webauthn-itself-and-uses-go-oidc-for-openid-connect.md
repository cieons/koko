# koko verifies WebAuthn itself, and uses go-oidc for OpenID Connect

koko verifies passkeys (ADR 0060) with a small verifier of its own, on the
standard library alone, the router's choice of 2026-11-15 (#228). It covers
only what koko asks of an authenticator:

- **The algorithms** EdDSA (Ed25519), ES256 (P-256) and RS256.
- **Attestation `none`.** The statement of any other format is read past
  unverified, since koko trusts no authenticator's maker.
- **CBOR:** a reader of the subset WebAuthn uses, and COSE_Key for those
  three key types. Containers nest at most 5 deep, as in the library's
  decoder (#230, the router's choice over #228's 16).
- **The checks:** the client data's type, challenge and origin; the RP ID
  hash; user presence and user verification; the backup flags; the
  signature; and a signature counter that refuses a sign-in when it goes
  back.

For OpenID Connect, koko uses `github.com/coreos/go-oidc/v3` (Apache-2.0)
with `golang.org/x/oauth2` (BSD-3-Clause). They bring only
`github.com/go-jose/go-jose/v4` (Apache-2.0) into the binaries, and each
licence passes the third-party notices gate. koko itself checks the claims
go-oidc leaves to the caller: `azp`, which must be present when `aud`
names more than one audience, the `nonce`, and `exp` with 60 seconds of
clock skew.

`github.com/go-webauthn/webauthn` stays in the repository as the verifier's
oracle, imported only by tests. The notices gate reads only what each binary
links, so a test-only import does not reach it. A differential fuzz target
makes and mutates ceremonies with a software authenticator, and the two
verifiers judge each one. The agreement runs one way (#230): koko accepts
nothing the library refuses. It checks the answer's own `type` and `id` as
the library does. When koko refuses what the library accepts, one of the
rules koko adds to the library's must explain it. The test computes each
rule from the input, not with koko's code:

- **The origin** is exactly the Web URL. The library folds case and drops
  a default port, a path and userinfo.
- **The client data** is JSON as `encoding/json/v2` reads it. Names must
  match exactly, and a duplicate name or invalid UTF-8 is refused. The
  library's `encoding/json` takes all three.
- **The attestation object's keys** match exactly. The library's CBOR
  decoder folds case.
- **CBOR map keys** are integers or text strings, and one CBOR value holds
  at most 1,024 items. Inside a value it decodes as any, the library's
  decoder also takes byte strings, `null`, `undefined`, floats and booleans
  as keys.
- **A COSE_Key's `kty`, `alg` and `crv`** are CBOR integers. The library's
  decoder also takes a simple value for an integer.
- **The extension outputs** are a map keyed by text strings. The library's
  decoder also takes `null` or `undefined` as the map or as a key.
- **An RSA key** has a 2,048- to 8,192-bit modulus and the exponent
  65,537.
- **The key** is checked when the passkey is made. The library checks it
  only at the passkey's first use, so the test checks it then with the
  library's own parser. An EC2 key's point must be on P-256 and an RSA key
  within the bounds above. An Ed25519 key is checked for its length only:
  its point goes unchecked until a signature, which a bad point fails.
- **A signature counter** that did not go up is refused. The library only
  sets `CloneWarning`, and the test compares the two.

Only the library verifies statements other than `none`. So the test makes
only `none` ceremonies, and when a mutation turns one into another format,
the library judges it as `none`.

koko records a passkey's backup eligibility (BE) and refuses a sign-in
whose BE flag differs from it, as WebAuthn Level 3 and the library do. A
passkey also records its owner's User id. A sign-in's `userHandle` is
compared with that recorded id, never with a value the caller passes. A
ceremony whose challenge is under 16 bytes, or whose RP ID or origin is
empty, is refused, so that a flow the caller failed to find checks nothing
by mistake.

## Considered Options

- **go-webauthn linked into `kokod`:** a vetted implementation of everything
  WebAuthn defines. But it brings 10 more modules: go-tpm, golang-jwt, msgp,
  mapstructure, cbor and others. Its two packages hold about 16,000 lines,
  most of them attestation formats and the FIDO Metadata Service, which
  koko never asks for. Each would have to be kept current, against about
  500 lines of koko's own.
- **The public key from the browser's `getPublicKey()`,** with no CBOR at
  all: it trusts the browser for the key, needs a newer browser API, and
  cannot be tested against recorded vectors, which carry COSE keys.
- **JOSE and the OpenID Connect checks written by koko:** no module at all.
  But verifying a JWS against a provider's key set is where the known
  mistakes live (`alg: none`, algorithm confusion, a key of the wrong type),
  and go-jose is what most Go OpenID Connect code relies on.

## Consequences

- The verifier is security-critical code koko maintains. Its tests are:
  - one test per refusal;
  - the W3C WebAuthn Level 3 test vectors. Most were made without user
    verification, so the test reaches the verifier without requiring it,
    through an unexported parameter, and shows those vectors refused for
    that reason by the exported functions;
  - the real-authenticator registrations in go-webauthn's own tests;
  - the differential fuzz target, with its seed corpus run by `go test`;
  - a fuzz target for the CBOR reader.
- Real-world assertions come only from the Owner's own check before
  release. A captured one cannot be committed, since its signature covers
  the Owner's domain, so a refusal found there is reproduced with the
  software authenticator.
- The library is revisited if koko comes to require attestation, the FIDO
  Metadata Service, or an algorithm outside the three.
- go-webauthn's modules appear in `go.mod` and `go.sum` as test
  dependencies, and in no release binary or notices file.
