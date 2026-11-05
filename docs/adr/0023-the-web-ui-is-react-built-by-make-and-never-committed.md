# The web UI is React and TypeScript, built by make and never committed

The web UI is a React and TypeScript single-page app in `web/`. Vite builds
it during `make build`, `make check` and the release workflow, and the koko
binary embeds the result and serves it (ADR 0015). Agents write most of
koko's code, and React is the framework they write most reliably, with the
widest choice of libraries for tables and forms. The build output is never
committed. Parallel sessions would conflict on every bundle, and a committed
bundle can drift from its source. A `go build` without the build output
embeds one placeholder page, which says how to build the web UI. Users
install release binaries (ADR 0020), which always carry the web UI.

## Considered Options

- **Preact**: React's API at about 3 KB, but some React libraries need a
  compatibility layer.
- **Svelte**: small output, but agents write it less fluently.
- **No framework, bundled by esbuild's Go API**: no Node in the build at all,
  but every table, form and route is written by hand.
- **Committing `web/dist`**: every `go build` and `go install` would carry
  the web UI, and Go-only work would need no Node, at the cost above.

## Consequences

- Building and checking koko need Node as well as Go. `go install` builds a
  koko whose web UI is the placeholder page.
- The TypeScript types generated from the API's Go types (ADR 0015) are small
  and committed, and CI fails when they are out of date.
- Every string the web UI shows comes from a message catalogue, one for each
  language. P0 ships English and Simplified Chinese, and a test fails when
  either catalogue lacks a key, or when an API error code has no message.
