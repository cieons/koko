# A Secret value travels in JSON as base64

A Secret's value is opaque bytes (ADR 0018), and JSON strings hold only
text. Wherever JSON carries a value, in the API's `secret.add` request and
`secret.show` response and in `koko secret show --json`, it is standard
base64 (RFC 4648, section 4, with padding) of every byte, whether the value
is a password or a binary key, under a name that says so: `value_base64`.
`koko secret show` without `--json` prints the bytes themselves, which is
how a value reaches the command that needs it.

## Considered Options

- **A JSON string when the value is UTF-8, base64 otherwise, with a field
  naming which**: a password reads as itself, but every script and the web
  UI must branch on the field, and one that forgets it handles the rare
  binary value wrong, silently.
- **Always a JSON string**: lossless only for UTF-8, so a DER key or a value
  with a NUL byte would not survive, against the spec's any bytes up to
  64 KiB.
- **base64 under the name `value`**: the shortest name, but a script or an
  agent that reads `value` as the value itself would pass on the encoding
  and log in with the wrong password, with nothing to say why.

## Consequences

- One rule for every value: `jq -r .value_base64 | base64 -d` gives the bytes
  stored, a trailing newline included.
- In Go the field is a `[]byte` type (`api.SecretValue`), which
  encoding/json/v2 writes as standard base64 and reads strictly; the web
  UI's generated type is a `string`, which the page decodes.
- A value grows by a third in transit: one of 64 KiB travels as about
  87 KiB of base64.
