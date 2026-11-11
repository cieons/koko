---
status: its swap and its PATH rule on Windows, refined by ADR-0052
---

# koko updates itself in its user's own directory

The CLI is installed per user, so that it upgrades without root, the
Owner's choice of 2026-11-11. `install.sh` run by a user other than root
writes `~/.local/bin/koko`; run as root, it writes `/usr/local/bin/koko`, as
before, for every user of the machine. `koko update` upgrades the CLI on
demand: to the release the `releases/latest` redirect names, or to the one
`--version` names, older or newer. It downloads `koko-<os>-<arch>` and the
release's `SHA256SUMS`, checks the file's SHA-256 and its Go build
information (`cmd/koko`, the version asked for, this platform), and renames
it over the file it runs from. It replaces only `~/.local/bin/koko` run by
its user, or `/usr/local/bin/koko` run by root; it refuses a development
build, the container image's koko, root's file run by another user, and a
file anywhere else, naming the way that file is upgraded. Nothing checks
for a release in the background, and there are no update channels:
`koko update --check` asks when a person or an agent wants to know.

This refines ADR 0026: every `koko`, not only `install.sh`, now reads the
`releases/latest` redirect and `koko-<os>-<arch>`. It refines ADR 0041:
`install.sh` writes the CLI where its user can, and needs root only for the
koko server.

## Considered Options

- **`/usr/local/bin` with `sudo`, as before:** one file for every user, but
  every upgrade of the CLI needs root, which an agent's box and many
  people's machines do not give a CLI.
- **A check in the background, or update channels,** as magpie has: people
  would learn of a release sooner, but a CLI would contact GitHub on its
  own from every device and agent's box, and channels are a second release
  stream to keep.
- **`install.sh` adding `~/.local/bin` to the shell's profile,** as rustup
  and uv do: new shells would find `koko` with no step, but `install.sh`
  would edit files the user owns, and a variable to opt out would join the
  contract. It prints the line for the user's shell instead.
- **`install.sh` removing a root-owned `/usr/local/bin/koko`** when `sudo -n`
  works: nothing would shadow the new file, but other users of a shared
  machine would lose their koko until they install their own.
- **A variable naming the CLI's directory:** one more input, for a need no
  one has shown.

## Consequences

- `koko update` restarts nothing. A running `koko`, a Device Mux of the old
  build among them, keeps its file, and its SSH Sessions and `koko web`
  carry on; the old Device Mux idles out (ADR 0017).
- koko's ssh_config names its ProxyCommand's koko by full path. A command
  run from the user's shell, by a release at the path `koko update` would
  replace, points the ProxyCommand at itself when it names another file,
  so a v0.3.x `/usr/local/bin/koko` can be removed once the new koko has
  run once. The Device Mux's refresh still never changes the path.
- `install.sh` leaves a root-owned `/usr/local/bin/koko` where it is,
  names it, and gives the line that removes it.
- Verification is what `install.sh` and `koko server install` already do:
  `SHA256SUMS` over HTTPS and the build information. Checking the release's
  attestation is a later decision.
- A release from before `koko update`, installed with `--version` or
  `KOKO_VERSION`, comes back up with the install line.
