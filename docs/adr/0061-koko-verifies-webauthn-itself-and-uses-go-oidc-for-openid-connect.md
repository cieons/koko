# koko verifies WebAuthn itself, and uses go-oidc for OpenID Connect

koko verifies passkeys (ADR 0060) with a small verifier of its own, on the
standard library alone, the router's choice of 2026-11-15 (#228). It covers
only what koko asks of an authenticator:

- **The algorithms** EdDSA (Ed25519), ES256 (P-256) and RS256.
- **Attestation `none`.** The statement of any other format is read past
  unverified, since koko trusts no authenticator's maker.
- **CBOR:** a reader of the subset WebAuthn uses, and COSE_Key for those
  three key types.
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
verifiers must agree on every accept and every refusal. The two intended
differences are the counter, which the library only flags as
`CloneWarning`, and statements other than `none`, which only the library
verifies.

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
  - the W3C WebAuthn Level 3 test vectors;
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
