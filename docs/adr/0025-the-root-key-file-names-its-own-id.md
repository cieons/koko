# The Root Key file names its own id

The Root Key's file (ADR 0018) is one line, `<id>:<key>`, such as
`rootkey_3f9k2m7q:` followed by the 256-bit key in 43 characters of
base64url without padding. The id is derived from the key: the first 40 bits
of SHA-256 over a fixed label and the key, written as koko writes record ids,
`rootkey_` and 8 lowercase Crockford base32 characters. Every sealed value,
`koko server show` and every refusal name the key by this id, so the Owner can
tell which copy in a password manager fits which koko server by reading it,
with no tool. The koko server checks the id against the key when it loads the
file, so a damaged copy does not parse instead of passing for another key.

## Considered Options

- **The key alone in the file, its id computed when needed**: shorter, but a
  copy kept off the machine does not show its id, P0 has no command that
  computes one, and a damaged copy reads as another key.
- **Hex instead of base64url**: no `-` or `_` to stop a double-click
  selection, but 64 characters for the key instead of 43.
- **A format tag before the id, such as `koko-root-key-1:`**: it names the
  format, but repeats what the `rootkey_` prefix says, and a later format can
  be told apart by its own shape.
- **A longer id**: 40 bits tells a koko server's few Root Keys apart. The id
  is a name, not a check: AES-GCM still refuses a value sealed under another
  key whose id is the same.

## Consequences

- A copy of the file names its key's id in plain text, and a copy whose id
  and key disagree is refused as a file that does not parse.
- The id is in record-id form, so the S1 harness's golden files number it as
  they number record ids (`"rootkey_<1>"`).
- Changing how the id is derived would orphan every sealed value's id, so the
  derivation is fixed for the life of the format. Rotating the Root Key (P1)
  adds keys, not a new derivation.
