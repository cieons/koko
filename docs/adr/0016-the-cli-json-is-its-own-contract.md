# The CLI's JSON is its own contract

`--json` prints the CLI's own shape, defined by Go types in the CLI package
and mapped by hand from the API's types (ADR 0015), and `koko host add` reads
the shape `koko host ls --json` prints. Scripts and agents depend on the CLI,
not on the API: an API change reaches them only when the CLI chooses to show
it. Within a koko major version the CLI's fields and error codes are only
added, never renamed, removed or retyped, and scripts ignore fields they do
not know.

## Considered Options

- **Printing the API's JSON as it is**: one shape everywhere and no mapping to
  write, but every API change would reach scripts unasked, and the output
  would change with the koko server's version.

## Consequences

- Lists print a bare JSON array. The CLI pages through the API itself, so the
  API's wrapper and cursor never show.
- A test fails when an API response field is neither mapped nor listed as left
  out on purpose, so adding a field to the API forces the decision in the same
  change. Golden tests pin every command's `--json`.
- A field a newer koko server adds reaches `--json` only once the CLI is
  upgraded.
- `koko skill` documents the CLI's shape, not the API's.
