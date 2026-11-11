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
| `koko-linux-amd64`, `koko-linux-arm64` | the CLI only, without the koko server or its web UI; in v0.1.0 and v0.2.0, the koko server and the CLI in one ([ADR 0041](../adr/0041-koko-installs-the-koko-server-of-its-own-version.md)) |
| `koko-darwin-arm64` | the CLI for Apple Silicon macOS; it carries no koko server, so no web UI |
| `kokod-linux-amd64`, `kokod-linux-arm64` | `kokod`, the koko server, with its web UI ([ADR 0040](../adr/0040-koko-is-the-cli-and-kokod-the-koko-server.md)) |
| `install.sh` | [`release/install.sh`](../../release/install.sh) as it is at the tag |
| `SHA256SUMS` | the SHA-256 of each asset above, one `<hex>  <name>` line each, as `sha256sum` writes it |

Each binary is self-contained, built with `CGO_ENABLED=0`, `-trimpath` and
`-ldflags='-s -w'`. Every asset in `SHA256SUMS` has a GitHub artifact
attestation, which `gh attestation verify <file> --repo cieons/koko` checks.

The names, the `releases/latest` redirect and `SHA256SUMS` are read by
install.sh, by `koko server install`, which downloads `kokod-linux-<arch>`
of its own release, and by every `koko`'s `koko update`, which reads the
redirect and downloads `koko-<os>-<arch>`
([`internal/shared/release`](../../internal/shared/release/release.go),
[ADR 0047](../adr/0047-koko-updates-itself-in-its-users-own-directory.md)),
and by every `kokod run`, whose check for a new release reads the redirect
([the update check](initialisation.md#the-update-check),
[ADR 0048](../adr/0048-the-koko-server-upgrades-itself-through-a-root-helper-that-takes-only-a-newer-releases-version.md)).
The image sets `KOKO_IMAGE=ghcr.io/cieons/koko`, so that `koko update` in
it refuses and names a new image instead, and so that `kokod run` names
its `upgrade_way` `image` ([the update check](initialisation.md#the-update-check)).

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
| Binaries | `/usr/local/bin/kokod` and `/usr/local/bin/koko`: the release's `kokod-linux-<arch>` and `koko-linux-<arch>`, copied from `dist/`, never rebuilt: the binaries `SHA256SUMS` lists, whose version the release checks is the tag |
| Entrypoint, command | `["kokod"]`, `["run"]`: `kokod run`, which takes the flags and `KOKO_*` variables `koko server run` took before it, so a Compose file that sets no entrypoint or command, as `release/compose.yaml` sets none, needs no change |
| Environment | `KOKO_DATA_DIR=/koko/data`, `KOKO_ROOT_KEY_FILE=/koko/root.key` |
| Directories | `/koko` (0700, uid and gid 65532), a `VOLUME`, so a container without a mount keeps its data in an anonymous volume, which outlives `docker rm`; `/run/koko` (0755, 65532), the socket's directory |
| Port | `EXPOSE 7822` |
| Labels | `org.opencontainers.image.source`, `.version` (the tag without its `v`, or `git describe` elsewhere), `.revision` (the commit), `.licenses` (`Apache-2.0`) |

`kokod run` changes no file's owner. A named volume on `/koko` starts as a copy
of the image's, uid 65532's already; a bind mount must be made 65532's on
the Docker host first (`install -d -o 65532 -g 65532 -m 0700`), or start-up
refuses it as a directory it cannot write. `kokod run` refuses to run
as root ([the Seal](seal.md#at-start-up)), so `--user 0` does not start.
`docker exec koko koko …` runs the CLI over the container's socket as the
Local caller.

**Tags.** A release `vX.Y.Z` pushes the tag `X.Y.Z`, without the `v`, and
moves `latest` to it, unless the version has a pre-release part
(`0.3.0-rc.1`), which gets only its own tag, as `releases/latest` skips it.
There are no floating `X.Y` or `X` tags.

**`make image`** builds the index from the binaries `make dist` left in
`dist/` (its context, of which
[`Dockerfile.dockerignore`](../../release/Dockerfile.dockerignore) lets in
only the four Linux binaries) into `dist/koko-image.tar`, an OCI layout
archive. No `SHA256SUMS` lists it and no Release holds it. It needs Docker
with buildx, through a builder named `koko-image` with the
`docker-container` driver, which it makes once, since Docker's own driver
may not export a multi-platform image. It adds no provenance or SBOM
manifest of buildx's, so the index holds the two platforms only; the
attestation is GitHub's. `make check` never runs it, and needs no Docker.

**Checking it.** `gh attestation verify oci://ghcr.io/cieons/koko:X.Y.Z
--repo cieons/koko` checks where the image was built. Each platform's
`/usr/local/bin/kokod` and `/usr/local/bin/koko` have the SHA-256s
`SHA256SUMS` lists for its `kokod-linux-<arch>` and `koko-linux-<arch>`.

**One step only the user can do.** After the first release pushes the
image, set the GHCR package `koko` to public and link it to the repository
(the package's settings on GitHub), so that `docker pull` works without
signing in, as the release assets need the repository public.

## Building one

`make dist` builds the assets into `dist/`, which git ignores: the web UI
first, as `make build` does, then the binaries of `RELEASE_BINARIES` in the
[Makefile](../../Makefile), each `<program>-<os>-<arch>` from
`cmd/<program>` (`koko` from [`cmd/koko`](../../cmd/koko), `kokod` from
[`cmd/kokod`](../../cmd/kokod)), then `install.sh` and `SHA256SUMS`. Both
programs come from one module and one commit, and Go stamps each binary's
version from git ([`build.Read`](../../internal/shared/build/build.go)), so
`koko version` and `kokod version` say the tag in a checkout at the tag
with nothing changed, and a pseudo-version (with `+dirty` for changes)
anywhere else. `make build` writes `bin/koko` and `bin/kokod`; `make cross`
compiles every package for each target, `cmd/kokod`'s stub on macOS and
Windows included, which refuses with `kokod runs on Linux only`.

The [release workflow](../../.github/workflows/release.yml) runs on a pushed
`v*` tag, in these jobs:

1. **check**, **systemd** and **container**: the
   [check workflow](../../.github/workflows/check.yml) on the tag, on Linux
   and macOS, [the real-systemd check](#the-real-systemd-check) and
   [the container check](#the-container-check), on each platform; nothing
   is built unless all three pass;
2. **build**, with a read-only token and no credentials left in the
   checkout, since it runs koko's and npm's code: `make dist`, a check that
   `dist/koko-linux-amd64 version` and `dist/kokod-linux-amd64 version` are
   the tag, `make image`, and two
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
curl -fsSL https://github.com/cieons/koko/releases/latest/download/install.sh | sh                            # the CLI, for this user
curl -fsSL https://github.com/cieons/koko/releases/latest/download/install.sh | sudo sh -s -- --owner alice   # the koko server too, as root
```

A POSIX `sh` script that installs `koko`, the CLI, on Linux and macOS:
as `~/.local/bin/koko` for the user who runs it, or as
`/usr/local/bin/koko`, for every user of the machine, when root runs it
([ADR 0047](../adr/0047-koko-updates-itself-in-its-users-own-directory.md)).
It installs or upgrades the koko server first, as root, when asked for or
already there
([ADR 0041](../adr/0041-koko-installs-the-koko-server-of-its-own-version.md)).
It needs `curl`, `sha256sum` or `shasum`, `awk`, `mktemp`, `env`, `mkdir`,
`install`, `mv`, `ls` and, for the koko server when not run as root,
`sudo`. Everything is in `main`, called on the last line, so a download
cut short runs nothing. In this order:

1. **Checks before any download or write**, each ending with "nothing was
   written":
   - `uname -s` is neither `Linux` nor `Darwin`;
   - `uname -m` is neither `x86_64`/`amd64` nor `aarch64`/`arm64` on Linux,
     or not `arm64` on macOS, since a release has no Intel macOS binary.

   It does not check for systemd: the CLI needs none, and
   `koko server install` checks the machine itself.
2. **Where the CLI goes.** `/usr/local/bin` as root (`id -u` is 0);
   otherwise `$HOME/.local/bin`, refused, with nothing written, when
   `HOME` is unset, empty, or not a directory the user owns (`test -d` and
   `test -O`). Root is not needed for the CLI.
3. **Whether the koko server is part of this run.** It is with any
   argument, or with none when `systemctl cat koko.service` succeeds. That
   part runs as root, directly or through `sudo -n` when `sudo -n true`
   succeeds; `sudo` is not run otherwise. A user who can do neither is
   refused with arguments, with the hint to pipe to
   `sudo sh -s -- <the same arguments>`, and nothing written. Without
   arguments, the koko server is left out: install.sh says so, naming
   `koko server upgrade` and the line piped to `sudo sh`, and installs the
   CLI for this user.
4. **The release.** `KOKO_VERSION`, with or without its `v`, or else the tag
   of the page that `releases/latest` redirects to, followed through every
   redirect (a moved repository adds one) and read once. A `KOKO_VERSION`
   that is not `v<digit>` followed by letters, digits, `.`, `+` and `-` is
   refused with exit 2.
5. **Download** `SHA256SUMS` and `koko-<os>-<arch>`, and nothing else, from
   `releases/download/<tag>/` over HTTPS only (`--proto =https`), into a
   `mktemp -d` directory removed on every exit. On Linux it is under
   `TMPDIR`, so on a machine whose `/tmp` is mounted `noexec`, `TMPDIR`
   names another.
6. **The checksum**, with `sha256sum`, or `shasum -a 256` where that is
   missing, as on macOS. A binary that `SHA256SUMS` does not list, or whose
   SHA-256 differs, is never run or installed.
7. **The koko server, when part of this run.** It runs
   `env <pin> <tmp>/koko-<os>-<arch> server install "$@"` as root, with
   stdin from `/dev/null`, since under `curl | sh` stdin is the rest of the
   script. `<pin>` is `KOKO_VERSION=<tag>` when `KOKO_VERSION` was given,
   since `sudo` drops it from the environment, and nothing otherwise. When
   that fails, install.sh exits with its status, says the CLI was left as
   it was, and writes nothing.
8. **The CLI**, written by the user who runs install.sh:
   `mkdir -p -m 0755 <dir>`, then `install -m 0755` the binary as
   `<dir>/.koko.new`, then `mv -f` it onto `<dir>/koko`. A rename in one
   directory, it leaves a running `koko`, such as a Device Mux, its file,
   and nobody sees one half written. The name beside it is fixed, so a run
   cut short leaves at most one such file, which the next run replaces.
   install.sh says `install.sh: installed koko <tag> at <dir>/koko`.
9. **What the user must do next**, each only when it applies:
   - **PATH.** When `<dir>` is not an entry of `PATH`, the line that puts
     it there for the shell `SHELL` names: for zsh,
     `echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.zprofile`; for
     bash, the same into `~/.bashrc` on Linux and `~/.bash_profile` on
     macOS; for fish, `fish_add_path ~/.local/bin`; otherwise the `export`
     line, for the shell's profile (with `/usr/local/bin` for root). Then
     `<dir>/koko`, to run until a new shell starts.
   - **Another koko.** When `command -v koko` names another file than
     `<dir>/koko` (or `<dir>//koko`, as dash gives it for a `PATH` entry
     ending in `/`), install.sh names it. When that file is
     `/usr/local/bin/koko` and `ls -ldn` shows root owns it, an earlier
     install line put it there as root, and install.sh gives
     `sudo rm /usr/local/bin/koko`, to run once no other user of the
     machine runs it and a koko command has run with the new koko. On a
     koko server's machine (arguments given, or `koko.service` known), it
     says instead that the file is the koko server's CLI, to keep, and
     names the line piped to `sudo sh`, which upgrades both.

   install.sh itself writes nothing outside `<dir>`, edits no shell file
   and removes no `koko`; the koko server's files are
   `koko server install`'s. It exits 0.

The koko server goes first and the CLI last: a failed upgrade of the koko
server leaves the CLI of the version that matches it, and a v0.1.0 or
v0.2.0 koko server, whose unit runs `/usr/local/bin/koko server run`, has
its unit rewritten before that file is replaced. Every release is installed
by these rules: `koko-<os>-<arch>` is a name every release has, and one from
before `kokod` (v0.1.0, v0.2.0) is the whole koko, whose `server install`
installs the koko server by itself. A release from before `koko update`
installed with `KOKO_VERSION` goes into `~/.local/bin` the same way, and
comes back up with the install line.

Nothing prompts. install.sh writes only to stderr, as `install.sh: ...`, and
exits 1 when it stops; stdout is `koko server install`'s alone, so `--json`
passes through clean.

## Verifying

[`release/install_test.go`](../../release/install_test.go) runs `install.sh`
as `curl … | sh -s -- <args>` does, the script on `sh`'s stdin, which stays
open so that anything reading stdin, as a prompt would, times the test out.
The fake machine is a `PATH` of its own, its bin directory first, then
entries a test chooses (the user's `~/.local/bin` and `/usr/local/bin` by
default), under a `HOME` of its own: fakes for `uname` (Linux or `Darwin`),
`systemctl cat koko.service` (one that is not found on a Mac), `id`, `ls
-ldn` (a file under the fake root directory is root's) and `sudo`, which
logs each call and runs its command with an environment of its own, as
`env_reset` does; a `curl` that serves a fake GitHub from a directory and
logs every URL; a `mktemp -d` that makes its directory under the test's
own, as GNU's does under `TMPDIR`; a `shasum -a 256`; `mkdir`, `install`
and `mv` that log each call with its uid and write a path outside the
fake machine's directory under its root directory instead of `/`; and the
real `rm`, `chmod`, `awk`, `env`, `true` and, unless the machine is a Mac,
`sha256sum`. `command -v` is a builtin that looks on the real filesystem,
so the test puts a shell function of that name ahead of the script, which
looks each `PATH` entry up as the writing fakes write. The fake `curl`
follows redirects only with `-L` and fails on a missing file only with
`-f`, writing the error page otherwise, as curl does, and refuses a call
without `--proto =https`. Its release binaries are scripts that log their
path, arguments, uid, `KOKO_VERSION` and stdin to the same log as `mkdir`,
`install` and `mv`, so a test reads what ran in order. The tests cover the
CLI alone on Linux amd64 and arm64 and macOS arm64 (with `shasum`), with
no systemd and with no `sudo` too, in `~/.local/bin` with no `sudo` run,
and in `/usr/local/bin` as root; a `HOME` unset, empty, missing, a file or
another user's; the `PATH` line for zsh, bash on Linux and macOS, fish and
any other shell, and for root, with no file outside the CLI's directory
changed; another `koko` first on `PATH`, root's `/usr/local/bin/koko`
named with its removal line, or kept on a koko server's machine, and left
in place either way, and the new `koko` through an entry ending in `/`
named as no other; the koko server with
arguments and the upgrade of one `systemctl` knows, each before the CLI,
through `sudo` and as root; a koko server left out for a user who cannot
be root; `KOKO_VERSION` reaching `koko server install` through
`sudo -n env`; a release from before the split; the latest release
(through a moved repository too) and `KOKO_VERSION`; every refusal before a
download, an Intel Mac's included; a checksum that does not match, a binary
`SHA256SUMS` does not list, failed downloads, a release not published, no
release at all; and a `koko server install` that fails, whose exit status
install.sh takes, writing no CLI. CI runs them under Ubuntu's `sh` (dash)
and macOS's. `release/` holds no Go code but the test, so nothing imports
it, and it is in no [package group](code-layout.md#the-package-groups).

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
[the store](store.md#migrations)). Each build must be a release's, since
`koko server install` downloads only the `kokod` of its own release, and Go
stamps a release's version only at a tag ([Building one](#building-one)):
so the workflow first tags the commit `v0.0.2` and the older one `v0.0.1`,
in its checkout only, never pushed. On a release's tag, that tag is the
higher, and Go stamps it. Then
[`release/systemd-check.sh`](../../release/systemd-check.sh)
`<older dist> <newer dist>` checks, in order:

1. install.sh refuses a release whose binary does not match its
   `SHA256SUMS`, runs nothing and writes nothing;
2. install.sh of the older build installs it with `--owner alice --address
   localhost --port 8443`, a port other than the default, 7822
   ([ADR 0036](../adr/0036-the-koko-server-listens-on-7822-by-default.md)),
   so that the upgrade in step 5 shows it kept the port rather than took
   the default, through its own `server install`, which in a build from
   before `kokod` installs the whole koko server: the service is active, its
   process runs as `koko` and listens on 8443, systemd holds its `root-key`
   credential (`LoadCredential=`, asked over D-Bus, since `systemctl show`
   prints it as `[unprintable]`), and `koko` cannot read `/etc/koko/root.key`;
3. a device joins with the printed Join Code and lists the Users;
4. with a drop-in setting `KOKO_LISTEN=[::]:8443`, `systemctl show` quotes
   that entry as the installer reads it;
5. install.sh of the newer build, without arguments, upgrades both: the fake
   GitHub served the newer release's `kokod-linux-<arch>` once, and nothing
   of the download is left in `/tmp`; `/usr/local/bin/koko` and
   `/usr/local/bin/kokod` are the newer build's files, and `koko version`
   and `kokod version` say its version; the unit runs
   `/usr/local/bin/kokod run`, the service runs `kokod` as `koko` under a new
   process on the port it kept, 8443, with no capability held or to be
   gained (`CapEff` and `CapBnd` in `/proc/<pid>/status`), a
   `koko.db.before-<n>` snapshot is there, and the device still works. Then the device installs the
   newer CLI, as **The device** below says;
6. `koko server uninstall`, running the `kokod` installed and asking the
   fake GitHub nothing, leaves nothing running or listening, removes the
   unit and `kokod`, and keeps `/usr/local/bin/koko`, `koko.db`, the Root
   Key and the `koko` user;
7. with the drop-in removed, and after `chown -R root:root /var/lib/koko`,
   as a copy put back by root would be, `sudo koko server install` without
   arguments downloads the newer `kokod` again, once, since uninstall
   removed it, and starts the koko server again on the newer build's
   default port, 7822, saying devices must connect to `localhost:7822`, as
   it does for a port it cannot know was the same; systemd gives every file
   back to `koko`, and the device, moved there with `koko connect
   localhost:7822`, still works.

**The fake GitHub.** install.sh and `koko server install` run unchanged,
with the real curl and Go's own HTTPS client. The script makes a test CA
and a certificate it signs for `github.com` with `openssl`, adds the CA to
the system's trust store (`update-ca-certificates`), which curl and Go both
read, maps `github.com` to `127.0.0.1` in `/etc/hosts`, and serves
`https://github.com/cieons/koko` on `127.0.0.1:443` with a small Python
server of its own (`python3`'s `http.server` and `ssl`), so that no Go code
is added under `release/`. `releases/latest` redirects to the tag page of
the tag a file names, `releases/tag/<tag>` answers for a release there, and
`releases/download/<tag>/<file>` is a file. Each build is the release of
its own version (`koko version --json`), holding every asset its
`SHA256SUMS` lists, and the release `v0.0.0-damaged` is the newer one with
its `koko-linux-<arch>` swapped for a script that would leave a mark if it
ran. The server logs every request, before it answers, as
`<method> <path> <status>`, so that a step counts its downloads. On exit,
the script puts `/etc/hosts` and the trust store back. A setting in
install.sh or koko to download from elsewhere would be one more input to
what runs as root on every koko server, for a test alone.

**The device** is a Docker container (`ubuntu:24.04`) on the host's network,
with the older build's binary copied in as its `/usr/local/bin/koko`. Its
own filesystem has no `/run/koko/koko.sock`, so its commands go over TCP to
`localhost:8443`, then `localhost:7822` from step 7 on, as a device's do,
through its Device Mux, and not through the Unix socket as the Local
caller. It keeps the older CLI through the
upgrade, as a device does when its koko server is upgraded. After step 5,
it installs the newer CLI as a device does: `curl` and `ca-certificates`
from Ubuntu's archive, the test CA in its own trust store, `github.com`
mapped by `--add-host`, and the newer install.sh run without arguments. It
then has the newer `/usr/local/bin/koko`, downloaded once, and no
`kokod`, and lists the Users with it, through a Device Mux of the newer
build.

The script refuses a machine where koko was installed, or where something
listens on 443, and leaves koko's data, Root Key and user behind: run it
only on a throwaway machine with systemd, Docker, `jq`, `ss`, `python3`,
`openssl`, `update-ca-certificates` and passwordless `sudo`. On a failure it
prints the koko service's journal, the device's Device Mux log, and the
fake GitHub's requests and log.

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
`docs/install.zh-CN.md` show is `release/compose.yaml` as it is, that
both give the `install -d` line it runs, and that both show its two
commented lines naming port 7823 as they read once the `# ` that starts
them is removed, then, in order:

1. the first start, through the Compose file, in a deployment folder whose
   `state/` the docs' `sudo install -d -o 65532 -g 65532 -m 0700` line made,
   with the port published as `17822:7822` and `KOKO_ADDRESS` set to
   `localhost:17822`, so that the published port differs from the
   container's: the koko server answers over its socket with that Server
   Address; the container's process is `kokod run`; its real, effective, saved and filesystem uid and gid are 65532,
   it is in no group but 65532 (which Docker lists as a supplementary
   group), and `CapEff` and `CapPrm` are 0 (`/proc/<pid>/status`);
   `state/` (0700), `root.key` (0600, one line) and `data/` (0700) are
   65532's, and so is `data/koko.db`; `docker logs koko` says `koko server
   initialised` and names `koko device add`, and never holds the Root Key;
   and `docker exec koko koko version` and `docker exec koko kokod version`
   name the same version and commit;
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
7. the web listener, behind a real Caddy
   ([ADR 0042](../adr/0042-the-web-ui-is-also-served-over-plain-http-to-the-owners-reverse-proxy.md)):
   `server.show`'s `web_listen` is `null` until the Compose file's two
   commented lines for it are on, edited as the docs say; with them, it is
   set, the `web listener` line names its address rather than `off`, and
   `127.0.0.1:7823` on the Docker host answers `503` naming `koko server
   set web_url=`. A
   `caddy:2` container on the Compose project's network serves `koko.test`
   with `tls internal`, `encode zstd gzip` and `reverse_proxy koko:7823`,
   published on `127.0.0.1:18443`. After `docker exec koko koko server set
   web_url=https://koko.test` and `koko user invite bob --web --valid 1d`,
   `curl --connect-to` with Caddy's root certificate, as a browser at
   `https://koko.test` with the Web URL as its `Origin`: learns whom the
   link is for, spends it (getting the `__Host-koko` cookie), sees it
   refused as `link_used` the second time, signs in with the Password,
   calls `user.current` as bob's Sign-in, sees another `Origin` refused
   with `bad_origin`, signs out, and sees the signed-out cookie answered
   `signin_required`. The `signed in` log line names an address other
   than Caddy's own, the one Caddy appended to `X-Forwarded-For`, and no
   log line holds the Password or the link's secret. Every request has
   10 s, and Caddy 60 s to serve the site;
8. a copy of `data/`, taken with the container stopped, in a new
   `state/` without `root.key`, is refused at start with `KOKO_OWNER` set,
   naming the Root Key's id the sealed data needs, and no `root.key` is
   written;
9. `koko server install` in a plain `ubuntu:24.04` container with no
   network (`--network none`) refuses with `unsupported_platform`, naming
   `ghcr.io/cieons/koko`, and writes nothing there: a download tried first
   would have failed as `download_failed` instead;
10. the archive's index holds `linux/amd64` and `linux/arm64` and nothing
    else, and in each platform's layers `/usr/local/bin/kokod` and
    `/usr/local/bin/koko` have the SHA-256s `SHA256SUMS` lists for its
    `kokod-linux-<arch>` and `koko-linux-<arch>`.

**One build.** The check runs only the image of the commit it checks.
Upgrading across a migration step and going back belong to the binaries,
which are the same in the image as on systemd: [the real-systemd
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
`jq`, `curl` and passwordless `sudo`, where no container is named `koko` and
nothing listens on 17822, 7823 or 18443; it removes the containers,
networks, volumes, image and files it made. On a failure it prints `docker
logs koko`, the device's Device Mux log and, once Caddy runs, `docker logs`
of Caddy.
