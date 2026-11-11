# The koko server upgrades itself through a root helper that takes only a newer release's version

The koko server learns of new releases by itself, and the Owner upgrades
it from the web UI with one button, the Owner's choice of 2026-11-11. Once
a day `kokod run` sends one `HEAD` to the `releases/latest` redirect,
carrying nothing about the deployment, unless the Owner turns the check off
(`koko server set update_check=off`). The web UI shows the Owner, and only
the Owner, that a newer release is out. On a koko server installed with
systemd, its button and `koko server upgrade` call one operation,
`server.upgrade`, for the Owner only.

kokod runs as `koko`, with no capability and a read-only system, and can
neither replace `/usr/local/bin/kokod` nor restart its unit. So the
operation only asks: kokod writes `/run/koko/upgrade.request`, holding one
release's version, and a path unit starts `koko-upgrade.service`, a
oneshot that runs `kokod upgrade` as root. The helper takes the request
only when it names a published release newer than the installed kokod. It
downloads that release's `kokod-linux-<arch>` and `SHA256SUMS` itself,
checks the SHA-256 and the build information, keeps the previous kokod,
and runs the new one's `kokod install`, the upgrade ADR 0020 describes.
When the new kokod does not start, the helper stops the service, puts the
database's snapshot back when the schema moved (the new kokod never served,
so nothing was written after it), and installs the previous kokod again.
It records the outcome in `/var/lib/koko-upgrade/outcome.json`, which kokod
reads for the web UI and `koko server show`.

In a container the image cannot replace itself: the web UI shows the
notice and the Compose commands, with no button, the Owner's choice.

This refines ADR 0020: install is still the install and the upgrade, and
the helper's upgrade runs it. It revises #101's rejection of a
`koko server upgrade` command, made while install was the only upgrade.

## Considered Options

- **kokod replacing itself,** with write access to `/usr/local/bin` and a
  polkit rule that lets `koko` restart the unit: no helper, but a
  compromised kokod could put any program where systemd runs it as root.
- **kokod following its own replaced binary and running it again,** as
  magpie's `serve` does: no second unit, but under systemd kokod cannot
  replace the binary at all, and a restart outside systemd's view loses the
  unit's sandbox.
- **The helper downloading as `koko` and installing as root:** root's part
  would be smaller, but it would trust a `SHA256SUMS` that kokod could have
  changed.
- **In a container, a newer kokod kept in the `/koko` volume** and run at
  the next start: a button there too, but the volume, which the koko
  process can write, would hold a program that runs at every start and
  outlives an image upgrade, and the image's tag and attestation would no
  longer describe what runs.
- **In a container, a helper container with the Docker socket,** in the
  style of Watchtower: the socket is root on the Docker host.
- **`koko update --server`, with a prompt:** the Owner's first wording, but
  a flag that raises a command's risk and a prompt are both against ADR
  0019, and a harness rule for `koko update` would allow restarting the
  koko server.

## Consequences

- What a compromised kokod can do through the helper is ask for a published
  release newer than the installed one. It cannot choose a file, a URL, an
  argument or an older release, and stopping the koko server it could
  already do.
- `kokod install` writes `koko-upgrade.path` and `koko-upgrade.service` with
  the unit at every run, and `kokod uninstall` removes them, so an
  installed v0.3.x koko server gains the helper with its next upgrade by
  the install line.
- An upgrade restarts the koko server, which ends every open SSH Session
  (ADR 0021). The web UI's confirmation says so; `koko server upgrade` is
  in the irreversible tier and never prompts (ADR 0019).
- `koko server upgrade` waits for the outcome, then brings the CLI that ran
  it to the same version when it can, in `install.sh`'s order: the koko
  server first.
- Going back stays the install line with `KOKO_VERSION`. The helper never
  installs an older release.
- The `releases/latest` redirect and `kokod-linux-<arch>` gain kokod as a
  reader (ADR 0026).
