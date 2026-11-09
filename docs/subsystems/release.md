# The release and install.sh

What a koko release publishes, how it is built, and what `install.sh` does
with it. Where the assets live and why their names are fixed is
[ADR 0026](../adr/0026-install-sh-and-the-binaries-are-release-assets-with-fixed-names.md);
what install does once the binary runs is the [installer](installer.md).

## The assets

A release is a GitHub Release whose tag is the version, such as `v0.3.0`. It
holds:

| Asset | What it is |
| --- | --- |
| `koko-linux-amd64`, `koko-linux-arm64` | the koko server, its web UI and the CLI |
| `koko-darwin-arm64` | the CLI for Apple Silicon macOS; it carries no koko server, so no web UI ([ADR 0024](../adr/0024-the-server-and-the-cli-are-separable.md)) |
| `install.sh` | [`release/install.sh`](../../release/install.sh) as it is at the tag |
| `SHA256SUMS` | the SHA-256 of each asset above, one `<hex>  <name>` line each, as `sha256sum` writes it |

Each binary is self-contained, built with `CGO_ENABLED=0`, `-trimpath` and
`-ldflags='-s -w'`. Every asset in `SHA256SUMS` has a GitHub artifact
attestation, which `gh attestation verify <file> --repo cieons/koko` checks.

Beside the Release, each release pushes [the image](#the-image).

## The image

`ghcr.io/cieons/koko` runs the koko server in a container that systemd does
not run ([P1 spec: the container image](https://github.com/cieons/koko/issues/91)). It is one index for `linux/amd64`
and `linux/arm64`, so `docker pull` picks the machine's.
[`release/Dockerfile`](../../release/Dockerfile) builds it with no `RUN`
step, so building for the other architecture needs no emulation:

| What | Value |
| --- | --- |
| Base | `gcr.io/distroless/static-debian12:nonroot`, pinned by digest; [Dependabot](../../.github/dependabot.yml) opens a pull request when the digest moves |
| User | the base's default, the numeric `65532` (`nonroot` in its `/etc/passwd`, home `/home/nonroot`), from the start: the image never runs as root and holds no capability ([ADR 0039](../adr/0039-the-container-runs-as-uid-65532-from-the-start-and-koko-changes-no-owner.md)) |
| Binary | `/usr/local/bin/koko`: the release's `koko-linux-<arch>`, copied from `dist/`, never rebuilt: the binary `SHA256SUMS` lists, whose version the release checks is the tag |
| Entrypoint, command | `["koko"]`, `["server", "run"]` |
| Environment | `KOKO_DATA_DIR=/koko/data`, `KOKO_ROOT_KEY_FILE=/koko/root.key` |
| Directories | `/koko` (0700, uid and gid 65532), a `VOLUME`, so a container without a mount keeps its data in an anonymous volume, which outlives `docker rm`; `/run/koko` (0755, 65532), the socket's directory |
| Port | `EXPOSE 7822` |
| Labels | `org.opencontainers.image.source`, `.version` (the tag without its `v`, or `git describe` elsewhere), `.revision` (the commit), `.licenses` (`Apache-2.0`) |

`koko server run` changes no file's owner. A named volume on `/koko` starts as a copy
of the image's, uid 65532's already; a bind mount must be made 65532's on
the Docker host first (`install -d -o 65532 -g 65532 -m 0700`), or start-up
refuses it as a directory it cannot write. `koko server run` refuses to run
as root ([the Seal](seal.md#at-start-up)), so `--user 0` does not start.

**Tags.** A release `vX.Y.Z` pushes the tag `X.Y.Z`, without the `v`, and
moves `latest` to it, unless the version has a pre-release part
(`0.3.0-rc.1`), which gets only its own tag, as `releases/latest` skips it.
There are no floating `X.Y` or `X` tags.

**`make image`** builds the index from the binaries `make dist` left in
`dist/` (its context, of which
[`Dockerfile.dockerignore`](../../release/Dockerfile.dockerignore) lets in
only the two Linux binaries) into `dist/koko-image.tar`, an OCI layout
archive. No `SHA256SUMS` lists it and no Release holds it. It needs Docker
with buildx, through a builder named `koko-image` with the
`docker-container` driver, which it makes once, since Docker's own driver
may not export a multi-platform image. It adds no provenance or SBOM
manifest of buildx's, so the index holds the two platforms only; the
attestation is GitHub's. `make check` never runs it, and needs no Docker.

**Checking it.** `gh attestation verify oci://ghcr.io/cieons/koko:X.Y.Z
--repo cieons/koko` checks where the image was built. Each platform's
`/usr/local/bin/koko` has the SHA-256 `SHA256SUMS` lists for its
`koko-linux-<arch>`.

**One step only the user can do.** After the first release pushes the
image, set the GHCR package `koko` to public and link it to the repository
(the package's settings on GitHub), so that `docker pull` works without
signing in, as the release assets need the repository public.

## Building one

`make dist` builds the assets into `dist/`, which git ignores: the web UI
first, as `make build` does, then the binaries of `RELEASE_BINARIES` in the
[Makefile](../../Makefile), then `install.sh` and `SHA256SUMS`. Go stamps
each binary's version from git ([`build.Read`](../../internal/shared/build/build.go)),
so `koko version` says the tag in a checkout at the tag with nothing
changed, and a pseudo-version (with `+dirty` for changes) anywhere else.

The [release workflow](../../.github/workflows/release.yml) runs on a pushed
`v*` tag, in these jobs:

1. **check**, **systemd** and **container**: the
   [check workflow](../../.github/workflows/check.yml) on the tag, on Linux
   and macOS, [the real-systemd check](#the-real-systemd-check) and
   [the container check](#the-container-check), on each platform; nothing
   is built unless all three pass;
2. **build**, with a read-only token and no credentials left in the
   checkout, since it runs koko's and npm's code: `make dist`, a check that
   `dist/koko-linux-amd64 version` is the tag, `make image`, and two
   workflow artifacts: `dist/` without the image's archive, and the
   archive;
3. **publish**, which alone may write and sign, and runs no code of koko's
   or npm's. In order:
   1. one attestation that covers every subject in `SHA256SUMS`
      (`actions/attest`, `subject-checksums`);
   2. the image pushed from the archive to `ghcr.io/cieons/koko:X.Y.Z`, by
      `skopeo copy --all --preserve-digests`, from Ubuntu 24.04's archive on
      the job's `ubuntu-24.04` runner, which pins it, so the index
      keeps the digest the build gave it, logged in with `docker login` and
      the job's token (`packages: write`);
   3. an attestation of that index digest, pushed to the registry beside
      it (`actions/attest`, `subject-name`, `subject-digest`,
      `push-to-registry`);
   4. `gh release create` with every file in the `dist/` artifact and notes
      generated from the commits. A tag with a pre-release part
      (`v0.3.0-rc.1`) is published as a pre-release, which
      `releases/latest` skips;
   5. `latest` moved to the index digest by `skopeo copy`, unless the tag
      has a pre-release part. Moved last, `latest` never names an image
      whose Release failed to publish.

Attestations and anonymous downloads need the repository public (or, for
attestations, GitHub Enterprise Cloud).

## install.sh

```
curl -fsSL https://github.com/cieons/koko/releases/latest/download/install.sh | sh -s -- --owner alice
```

A POSIX `sh` script that needs `curl`, `sha256sum`, `awk`, `mktemp` and,
when not run as root, `sudo`. Everything is in `main`, called on the last
line, so a download cut short runs nothing. In this order:

1. **Checks before any download or write**, each ending with "nothing was
   written":
   - `uname -s` is not `Linux`;
   - `systemctl is-system-running` is missing, prints `offline` (systemd is
     not PID 1, as in most containers) or prints nothing (a chroot). A
     booting or degraded systemd is taken. The refusal names
     [the image](#the-image) and the install docs' "In a container"
     section;
   - `uname -m` is neither `x86_64`/`amd64` nor `aarch64`/`arm64`;
   - not root, and `sudo -n true` fails: the hint is to pipe to `sudo sh`.
2. **The release.** `KOKO_VERSION`, with or without its `v`, or else the tag
   of the page that `releases/latest` redirects to, followed through every
   redirect (a moved repository adds one) and read once. A `KOKO_VERSION`
   that is not `v<digit>` followed by letters, digits, `.`, `+` and `-` is
   refused with exit 2.
3. **Download** `SHA256SUMS` and `koko-linux-<arch>` from
   `releases/download/<tag>/` over HTTPS only (`--proto =https`), into a
   `mktemp -d` directory removed on every exit. It is under `TMPDIR`, so on
   a machine whose `/tmp` is mounted `noexec`, `TMPDIR` names another.
4. **The checksum.** A binary that `SHA256SUMS` does not list, or whose
   SHA-256 differs, is never run.
5. **`koko server install "$@"`**, as root (directly, or through `sudo -n`),
   with stdin from `/dev/null`, since under `curl | sh` stdin is the rest of
   the script. install.sh exits with its status.

Nothing prompts. install.sh writes only to stderr, as `install.sh: ...`, and
exits 1 when it stops; stdout is `koko server install`'s alone, so `--json`
passes through clean.

## Verifying

[`release/install_test.go`](../../release/install_test.go) runs `install.sh`
as `curl … | sh -s -- <args>` does, the script on `sh`'s stdin, which stays
open so that anything reading stdin, as a prompt would, times the test out.
The fake machine is a `PATH` of its own: fakes for `uname`, `systemctl`,
`id` and `sudo`, a `curl` that serves a fake GitHub from a directory and logs
every URL, and the real `mktemp`, `rm`, `chmod`, `awk`, `sha256sum` and
`true`. The fake `curl` follows redirects only with `-L` and fails on a
missing file only with `-f`, writing the error page otherwise, as curl does,
and refuses a call without `--proto =https`. Its release binaries are
scripts that log their arguments, uid and stdin. The tests cover the latest
release (through a moved repository too) and `KOKO_VERSION`, each
architecture, every refusal before a download, a checksum that does not
match, a binary `SHA256SUMS` does not list, failed downloads, a release not
published, no release at all, root and sudo, and koko's exit status. CI runs
them under Ubuntu's `sh` (dash) and macOS's. `release/` holds no Go code but
the test, so nothing imports it, and it is in no
[package group](code-layout.md#the-package-groups).

The release workflow runs only on a tag, and no test runs it; actionlint
checks it by hand. [The container check](#the-container-check) runs what
`make image` builds: its index's platforms and binaries, and, by running
it, its user, entrypoint, command, environment, `/koko` and `/run/koko`.
Its `EXPOSE` and labels are checked by hand.

## The real-systemd check

The [systemd workflow](../../.github/workflows/systemd.yml) installs koko
on a real systemd machine, a fresh `ubuntu-latest` runner, on every push to
`main`, on every pull request, and on a release's tag before the build job,
which needs it. It runs `make dist` twice: on the commit, and in a worktree
on the commit just before the one that added the latest migration step, so
that the upgrade from that older build migrates and snapshots (every
migration step is a file added to `internal/server/store/migrations/`,
[the store](store.md#migrations)). Then
[`release/systemd-check.sh`](../../release/systemd-check.sh)
`<older dist> <newer dist>` checks, in order:

1. install.sh refuses a release whose binary does not match its
   `SHA256SUMS`, runs nothing and writes nothing;
2. install.sh installs the older build with `--owner alice --address
   localhost --port 7822`, the port given since the older build may be from
   before 7822 was the default
   ([ADR 0036](../adr/0036-the-koko-server-listens-on-7822-by-default.md)):
   the service is active, its process runs as `koko` and
   listens on 7822, systemd holds its `root-key` credential
   (`LoadCredential=`, asked over D-Bus, since `systemctl show` prints it
   as `[unprintable]`), and `koko` cannot read `/etc/koko/root.key`;
3. a device joins with the printed Join Code and lists the Users;
4. with a drop-in setting `KOKO_LISTEN=[::]:7822`, `systemctl show` quotes
   that entry as the installer reads it;
5. install.sh of the newer build, without `--owner` or `--port`, upgrades:
   the binary is the newer one, the service runs again under a new process
   on the port it kept, with no capability held or to be gained (`CapEff`
   and `CapBnd` in `/proc/<pid>/status`), a `koko.db.before-<n>` snapshot
   is there, and the device still works;
6. `koko server uninstall` leaves nothing running or listening, removes the
   unit and the binary, and keeps `koko.db`, the Root Key and the `koko`
   user;
7. with the drop-in removed, and after `chown -R root:root /var/lib/koko`,
   as a copy put back by root would be, install.sh without `--owner` or
   `--port` starts the koko server again on the newer build's default port,
   7822, saying devices must connect to `localhost:7822`, as it does for a
   port it cannot know was the same; systemd gives every file back to
   `koko`, and the device still works.

**How install.sh reaches a local build.** install.sh runs unchanged. A fake
`curl`, first on its `PATH`, serves `https://github.com/cieons/koko` from a
directory: `releases/latest` redirects to the tag a file there names, and
`releases/download/<tag>/<file>` is a file. The older build is release
`v0.0.1`, the newer `v0.0.2`, and `v0.0.3` the newer one with its binary
swapped for a script that would leave a mark if it ran. Real curl, TLS and
GitHub are what the check leaves out; a setting in install.sh to download
from elsewhere would be one more input to the script that runs as root on
every koko server, for a test alone.

**The device** is a Docker container (`ubuntu:24.04`) on the host's network,
with the older build's binary as its `koko`. Its own filesystem has no
`/run/koko/koko.sock`, so its commands go over TCP to `localhost:7822`, as
a device's do, through its Device Mux, and not through the Unix socket as
the Local caller. It keeps the older CLI through the upgrade, as a device
does when its koko server is upgraded.

The script refuses a machine where koko was installed, and leaves koko's
data, Root Key and user behind: run it only on a throwaway machine with
systemd, Docker, `jq`, `ss` and passwordless `sudo`. On a failure it prints
the koko service's journal and the device's Device Mux log.

## The container check

The [container workflow](../../.github/workflows/container.yml) runs
[the image](#the-image) as its Owner would, through the install docs'
[`release/compose.yaml`](../../release/compose.yaml), on every push to
`main`, on every pull request, and on a release's tag before the build job,
which needs it. It runs once for each platform the image is built for, each
on a runner of that platform (`ubuntu-24.04` and `ubuntu-24.04-arm`), so
the image runs natively, with no emulation. Each runs `make dist` and `make
image` once, as the release's build job does, and loads its own platform's
image from the archive into Docker with `skopeo`. Then
[`release/container-check.sh`](../../release/container-check.sh) `<dist>`
checks, first, that the Compose file `docs/install.md` and
`docs/install.zh-CN.md` show is `release/compose.yaml` as it is, and that
both give the `install -d` line it runs, then, in order:

1. the first start, through the Compose file, in a deployment folder whose
   `state/` the docs' `sudo install -d -o 65532 -g 65532 -m 0700` line made,
   with the port published as `17822:7822` and `KOKO_ADDRESS` set to
   `localhost:17822`, so that the published port differs from the
   container's: the koko server answers over its socket with that Server
   Address; its real, effective, saved and filesystem uid and gid are 65532,
   it is in no group but 65532 (which Docker lists as a supplementary
   group), and `CapEff` and `CapPrm` are 0 (`/proc/<pid>/status`);
   `state/` (0700), `root.key` (0600, one line) and `data/` (0700) are
   65532's, and so is `data/koko.db`; `docker logs koko` says `koko server
   initialised` and names `koko device add`, and never holds the Root Key;
2. without that line, Docker makes `state/` as root, and the start is
   refused, naming `/koko` and uid and gid 65532, with nothing written in
   it; run with `--user 0` there, the image is refused, naming the rule,
   with nothing written; and with a named volume in place of `./state`, it
   starts and initialises with no step on the host;
3. `docker exec koko koko device add` prints a Join Code, which no log
   line holds, and a device joins with it at `localhost:17822` and lists
   the Users;
4. `docker kill`, then `docker start`: the socket file the kill left in the
   container is taken over, as the log says, and the device still works;
5. a restart with another `KOKO_ADDRESS` logs the warning naming that
   address, the recorded one and `koko server set address=`, and the
   Server Address stays the first one;
6. `docker compose down`, then `up -d`, recreates the container on the
   same `state/`: the Server Address, the Root Key's id and the host key's
   fingerprint are the same, nothing warns, and the device still works;
7. a copy of `data/`, taken with the container stopped, in a new
   `state/` without `root.key`, is refused at start with `KOKO_OWNER` set,
   naming the Root Key's id the sealed data needs, and no `root.key` is
   written;
8. `koko server install` in a plain `ubuntu:24.04` container refuses with
   `unsupported_platform`, naming `ghcr.io/cieons/koko`, and writes
   nothing there;
9. the archive's index holds `linux/amd64` and `linux/arm64` and nothing
   else, and in each platform's layers `/usr/local/bin/koko` has the
   SHA-256 `SHA256SUMS` lists for its `koko-linux-<arch>`.

**One build.** The check runs only the image of the commit it checks.
Upgrading across a migration step and going back belong to the binary,
which is the same in the image as on systemd: [the real-systemd
check](#the-real-systemd-check) and the store's tests cover them.

**How the image runs.** The deployment folder holds `release/compose.yaml`
as `compose.yaml` and a `.env` naming `KOKO_VERSION`, as the docs say. The
`compose.override.yaml` beside them, which Compose reads by itself, names
the local image, `koko-check:${KOKO_VERSION}`, replaces the published port
(`!override`, Compose v2.24 or later), and sets `KOKO_ADDRESS`. The
refused starts run with `docker compose run --rm`, so that no restart
policy starts them again, within 60 s, so that a start that no longer
refuses fails the check rather than holding it until the job's timeout.
The log lines it looks for are waited for, since Docker copies what a
container writes into its log on its own time.

**The device** is a Docker container (`ubuntu:24.04`) on the host's
network, with the release's `koko-linux-<arch>` as its `koko`, so that its
commands go over TCP to the published port, as a device's do.

Run the script only on a throwaway Linux machine with Docker, `skopeo`,
`jq` and passwordless `sudo`, where no container is named `koko` and
nothing listens on 17822; it removes the containers, networks, volumes,
image and files it made. On a failure it prints `docker logs koko` and the device's Device
Mux log.
