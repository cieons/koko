# The server and the CLI are separable, and only Linux builds carry the server

koko stays one binary (ADR 0001), but its code is kept ready to split into a
koko server and a CLI. Every package under `internal/` sits in a shared, a
server-only or a client-only group, chosen by its directory, and a Go test
fails when a client package depends on a server package or the reverse, on
every OS koko builds for. Only `cmd/koko` and the test harness import both:
`cmd/koko` hands `koko server run` the koko server through the CLI's runner,
so no client package needs a server import. Because the koko server runs on
Linux only, `cmd/koko` links it only into Linux builds; the macOS build (and
the compile-only Windows build) carries the CLI alone. This refines ADR 0001:
the Linux binary is still the one binary that is the koko server, the CLI and
the Gateway, and the other builds are its CLI.

## Considered Options

- **One package tree with no rule**: nothing to enforce, but the first import
  across the line makes a later split a refactor instead of a build change.
- **Two binaries or two Go modules now**: the split made, but two release
  artifacts to install and version, against ADR 0001.
- **A linter rule** (such as depguard): it checks direct imports only, for the
  host's OS only, and one line of lint configuration loosens it.
- **The koko server in every build**: no build tags, but the macOS binary
  would carry SQLite and the Gateway for a `koko server run` that cannot work
  there.

## Consequences

- Placing a new package is a choice of directory: `internal/shared`,
  `internal/server` or `internal/client` (docs/subsystems/code-layout.md). A
  shared package depends on neither of the others.
- Splitting koko into two binaries later means two `main` packages, not a
  refactor.
- On macOS and Windows, `koko server run` fails with `unsupported_platform`.
  The koko server is still compiled there by `make cross`, and its tests run
  on macOS through the S1 harness, which starts it in the test process.
