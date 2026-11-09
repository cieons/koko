# koko is the CLI and kokod the koko server

koko ships two programs, built from one module at one commit and released
together under one version, the Owner's choice of 2026-11-09: `koko`, the
CLI, for every OS koko builds for, and `kokod`, the koko server, for Linux
only. `koko` holds every command a person or an agent runs and none of the
koko server's code. `kokod` holds the koko server, the Gateway, the store
with its SQLite, the web UI and the installer's machine steps, and none of
the CLI's code. systemd and the container image run it as `kokod run`.
This supersedes ADR 0024, and ADR 0001's one binary; Go and the embedded
SQLite file stay.

Most machines that run koko only use it, as a device or an agent's box. One
binary made each of them carry SQLite, the Gateway and the web UI for a
`koko server run` they never call. It also made the CLI ship whenever any
server code changed. ADR 0024 kept the code ready for this split, so the
split is two `main` packages, not a refactor.

## Considered Options

- **One binary** (ADR 0001, ADR 0024): one file to install and version, but
  every Linux device carries the koko server.
- **`koko-server` as the second name**: plain, but long to type beside
  `koko server …`, which names the CLI's own commands about it. `kokod`
  follows the convention of `sshd` and `tailscaled`.
- **Two Go modules, or two version numbers**: each program could ship on
  its own, but there would be two versions to keep apart on every machine,
  and a compatibility rule between them that no reader needs.
- **`koko` carrying `kokod` inside it, written out when installing**: one
  download for the koko server's machine, but every device would carry the
  koko server again, which is the size the split removes.

## Consequences

- Every package under `internal/` stays in its shared, server-only or
  client-only group, as ADR 0024 set out, and `internal/groups_test.go`
  also checks `cmd/`. `cmd/koko` may not depend on the server group,
  `cmd/kokod` may not depend on the client group, and any other `cmd/`
  package fails until it has a rule of its own. The test harness alone
  imports both groups.
- `kokod` has a small command line of its own (`run`, `install`,
  `uninstall`, `version`, `help`), since it may not use the CLI's parser.
  `kokod run` takes the flags and `KOKO_*` variables `koko server run` took,
  and `koko server run` is refused, naming it. `koko server install`,
  `uninstall`, `show` and `set` stay in `koko`.
- The two programs share every error code that both report, from
  `internal/shared`.
- On other OSes, `cmd/kokod` compiles a stub that refuses, so `make cross`
  and the per-OS lint still cover it. The koko server's tests still run on
  macOS through the S1 harness, in the test process.
- Only `kokod` embeds the web UI (ADR 0023).
- `kokod version` prints the build `koko version` prints. `koko status`
  already shows both versions, and on the koko server's own machine it
  names the fix when the two differ.
- The unit stays `koko.service`, and the system user `koko`.
