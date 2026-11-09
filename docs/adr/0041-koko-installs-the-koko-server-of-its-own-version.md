# koko installs the koko server of its own version

The CLI comes first, and the koko server is installed when asked for, the
Owner's choice of 2026-11-09. `install.sh` without arguments installs `koko`
into `/usr/local/bin`, on Linux and macOS. With arguments, such as the
documented `--owner` line, or with none on a machine whose systemd knows
`koko.service`, it runs `koko server install` with them first. It writes the
CLI only once that has succeeded. `koko server install` gets the `kokod` of
its own build, in the first of these ways that applies: the file
`--kokod <file>` names, the installed `/usr/local/bin/kokod` when it is that
build, or else a download of `kokod-linux-<arch>` from the release named by
its own version. A download must match that release's `SHA256SUMS`. Every
`kokod`, whichever way it came, must be `koko`'s own build, read from its Go
build information without running it.
`koko server install` hands the machine's part to `kokod install`: the checks, the `koko`
user, the directories, the Root Key, `/usr/local/bin/kokod`, the unit
(`ExecStart=/usr/local/bin/kokod run`) and the start. Then it initialises
the koko server over the Unix socket with the client the CLI already has,
and prints the first Join Code. So `kokod` carries no client code.

This refines ADR 0020 and ADR 0026. The koko server still installs itself,
now through `kokod install`, and still has no configuration file.
`install.sh` is still the one line, and a fresh koko server's first Join
Code is still printed by it.

## Considered Options

- **`kokod` initialising itself at install**, with the Local caller's
  connection and the API client moved to `internal/shared`: `install.sh`
  could hand everything to `kokod`. But `kokod` would carry client code,
  about 700 lines would move, and the CLI that has the client runs during
  install anyway.
- **`install.sh` installing both programs on every Linux machine**, as
  tailscale's does: one rule, but every device would carry the koko server
  again.
- **`install.sh` downloading `kokod` itself**: no download in Go. But a
  `koko server install` run without `install.sh`, offline or from a package
  later, would need its own way anyway, and the rule that `kokod` is
  `koko`'s own build belongs in the program that runs it.
- **A `kokod` of another build, with a compatibility rule between
  `koko server install` and `kokod install`**: a second contract, for two
  files that always come from one release.
- **A variable or flag that downloads from elsewhere**, such as
  `KOKO_RELEASES_URL`: one more input to code that runs as root, for tests
  alone, which ADR 0026's check already refused for `install.sh`. Offline is
  `--kokod`, and tests use a build-time hook.

## Consequences

- Each release publishes `koko-<os>-<arch>`, now the CLI alone on Linux too,
  and `kokod-linux-amd64` and `kokod-linux-arm64`, all listed in
  `SHA256SUMS` and attested. No name changes, so ADR 0026's contract holds,
  but what `koko-linux-<arch>` holds does: `koko-linux-<arch> server run`
  is refused, naming `kokod run`.
- Every `koko` from now on reads
  `releases/download/v<version>/kokod-linux-<arch>` and `SHA256SUMS`, so
  that name and path join ADR 0026's contract.
- `install.sh` downloads only `koko-<os>-<arch>`, a name every release has.
  An earlier release's `koko-linux-<arch>` is the whole koko and runs
  `server install` by itself, so the newest `install.sh` installs v0.1.0
  and v0.2.0 with no case of its own. It no longer checks for systemd;
  `koko server install` does, before any download.
- Running the install line again upgrades both programs, the koko server
  first. A failed koko server upgrade leaves the installed CLI as it was. An
  installed v0.1.0 or v0.2.0 koko server takes that same upgrade, since
  install writes the unit again at every run (ADR 0020).
- A development build has no release to download from, so its
  `koko server install` needs `--kokod`.
- `koko server uninstall` runs `kokod uninstall`, which removes the unit and
  `kokod` and keeps `koko`, the data, the Root Key and the `koko` user.
- Going back past this change with an earlier release's `install.sh`
  leaves `/usr/local/bin/kokod` unused. The docs say to remove it.
- The container image holds both programs and runs `kokod run`. A Compose
  file that sets no `entrypoint` or `command`, as the docs' does, keeps
  working when only its version changes.
