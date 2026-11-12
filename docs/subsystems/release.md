# The release and install.sh

What a koko release publishes, how it is built, and what `install.sh` and
[`install.ps1`](#installps1) do with it. Where the assets live and why their names are fixed is
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
| `koko-windows-amd64.exe` | the CLI for Windows on x64, which Windows on Arm64 runs under emulation; from v0.6.0 on. `.exe` is part of the name, since Windows runs a file by its extension ([ADR 0052](../adr/0052-install-ps1-installs-the-windows-cli-and-puts-its-directory-on-the-users-path.md)); it carries no Authenticode signature |
| `install.sh` | [`release/install.sh`](../../release/install.sh) as it is at the tag |
| `install.ps1` | [`release/install.ps1`](../../release/install.ps1) as it is at the tag; from v0.6.0 on |
| `SHA256SUMS` | the SHA-256 of each asset above, one `<hex>  <name>` line each, as `sha256sum` writes it |

Each binary is self-contained, built with `CGO_ENABLED=0`, `-trimpath` and
`-ldflags='-s -w'`. Every asset in `SHA256SUMS` has a GitHub artifact
attestation, which `gh attestation verify <file> --repo cieons/koko` checks.

The names, the `releases/latest` redirect and `SHA256SUMS` are read by
install.sh and install.ps1, by `koko server install`, which downloads
`kokod-linux-<arch>` of its own release, and by every `koko`'s
`koko update`, which reads the redirect and downloads `koko-<os>-<arch>`,
with `.exe` on Windows
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
[Makefile](../../Makefile), each `<program>-<os>-<arch>`, with `.exe` for
Windows, from `cmd/<program>` (`koko` from [`cmd/koko`](../../cmd/koko),
`kokod` from [`cmd/kokod`](../../cmd/kokod)), then `install.sh` and
`install.ps1` (`RELEASE_SCRIPTS`), then `SHA256SUMS`. Both
programs come from one module and one commit, and Go stamps each binary's
version from git ([`build.Read`](../../internal/shared/build/build.go)), so
`koko version` and `kokod version` say the tag in a checkout at the tag
with nothing changed, and a pseudo-version (with `+dirty` for changes)
anywhere else. `make build` writes `bin/koko` and `bin/kokod`; `make cross`
compiles every package for each target, `cmd/kokod`'s stub on macOS and
Windows included, which refuses with `kokod runs on Linux only`.

The [release workflow](../../.github/workflows/release.yml) runs on a pushed
`v*` tag, in these jobs:

1. **check**, **systemd**, **container**, **skew** and **window**: the
   [check workflow](../../.github/workflows/check.yml) on the tag, on Linux
   and macOS, with the CLI's tests on Windows
   ([the `windows` job](#the-windows-job)),
   [the real-systemd check](#the-real-systemd-check),
   [the container check](#the-container-check), on each platform,
   [the skew check](#the-skew-check), for each pair of versions, and
   [`release/oldest-check.sh`](../../release/oldest-check.sh), which
   refuses a patch release whose `compat.Oldest` differs from the
   previous release's ([before tagging](#before-tagging)); nothing is
   built unless all five pass;
2. **build**, with a read-only token and no credentials left in the
   checkout, since it runs koko's and npm's code: `make dist`, a check that
   `dist/koko-linux-amd64 version` and `dist/kokod-linux-amd64 version` are
   the tag, and that the Go build information of
   `dist/koko-windows-amd64.exe`, which cannot run there, names the tag as
   its version (`go version -m`), `make image`, and two
   workflow artifacts: `dist/` without the image's archive, and the
   archive;
3. **windows**, on `windows-latest`, with a read-only token: it downloads
   the `dist/` artifact and runs `koko-windows-amd64.exe version --json`,
   which must say the tag. Publishing waits for it;
4. **publish**, which alone may write and sign, and runs no code of koko's
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
      generated from the commits, which the person releasing then replaces
      with the release's notes (below). A tag with a pre-release part
      (`v0.3.0-rc.1`) is published as a pre-release, which
      `releases/latest` skips;
   5. `latest` moved to the index digest by `skopeo copy`, unless the tag
      has a pre-release part. Moved last, `latest` never names an image
      whose Release failed to publish.

**Release notes.** Three sections, each item one line: a short summary of
what changed for the reader, ending with its issue (`#146`) or, without
one, its commit's short hash, which GitHub links:

- **What's new**: the `feat` commits.
- **Fixes**: the `fix` commits.
- **Upgrading**: only what this release asks of the reader, such as a
  schema change and its snapshot, or a step that `koko update` and the
  Compose lines do not cover.

Details stay in the issue and the docs; the notes link to them.

Attestations and anonymous downloads need the repository public (or, for
attestations, GitHub Enterprise Cloud).

### Before tagging

- **`compat.Oldest`** ([ADR 0049](../adr/0049-the-cli-and-the-koko-server-work-together-within-a-window-of-versions.md)),
  the oldest koko the CLI and the koko server of the build work with, moves
  only at a minor release (`vX.Y.0`), and that release's notes say so,
  naming the version below which the CLI and the koko server turn away
  each other. A patch release (`vX.Y.Z`, Z > 0) keeps the previous
  release's: the release job's **window** job refuses one that moves it,
  as a pre-release of one, comparing with the highest release tag below
  it; a previous release from before the window, which has none, passes.
- **The first release with a Windows CLI** is named in
  [`install.ps1`](../../release/install.ps1), as `$first`, `v0.6.0`: it
  tells a person who asks for an earlier release which one to ask for. If
  that release gets another number, `$first`, the script's tests and the
  table of [the assets](#the-assets) change with it, before the tag.

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

## install.ps1

```powershell
irm https://github.com/cieons/koko/releases/latest/download/install.ps1 | iex
$env:KOKO_VERSION = 'v0.6.0'; irm https://github.com/cieons/koko/releases/latest/download/install.ps1 | iex   # a pinned release
powershell -NoProfile -Command "irm https://github.com/cieons/koko/releases/latest/download/install.ps1 | iex"   # from cmd, or an agent's harness
```

A PowerShell script that installs `koko`, the CLI, on Windows, as
`%USERPROFILE%\.local\bin\koko.exe` for the user who runs it
([ADR 0051](../adr/0051-on-windows-the-cli-is-a-device-with-the-same-files-in-the-same-places.md),
[ADR 0052](../adr/0052-install-ps1-installs-the-windows-cli-and-puts-its-directory-on-the-users-path.md)).
It runs in Windows PowerShell 5.1 and in PowerShell 7, needs no
administrator and no tool beyond PowerShell, and takes no arguments: the
koko server runs on Linux only. Everything is in the function
`Install-Koko`, called on the last line, so a download cut short runs
nothing. The file is ASCII, since Windows PowerShell 5.1 reads other bytes
in the system's code page. In this order:

1. **Checks before any download or write**, each ending with "nothing was
   written":
   - the system is not Windows (PowerShell 7 on another system);
   - the machine is neither `AMD64` nor `ARM64`, read from
     `PROCESSOR_ARCHITEW6432`, which a 32-bit PowerShell on 64-bit Windows
     has, or else from `PROCESSOR_ARCHITECTURE`: 32-bit Windows is refused,
     and Windows on Arm64 gets the amd64 file;
   - `USERPROFILE` is unset or names no directory.
2. **The release.** `KOKO_VERSION`, with or without its `v`, refused when
   it is not `v<digit>` followed by letters, digits, `.`, `+` and `-`; or
   else the tag of the page that `releases/latest` redirects to, followed
   through every redirect and read once (`Invoke-WebRequest -Method
   Head`). The page's address is where each PowerShell keeps it:
   `BaseResponse.ResponseUri` in 5.1, `BaseResponse.RequestMessage.RequestUri`
   in 7.
3. **`SHA256SUMS`**, downloaded with `Invoke-WebRequest -UseBasicParsing`
   into a new directory under the user's temporary directory, removed on
   every exit. Every URL the script names is HTTPS. Where Windows
   PowerShell 5.1 is set to offer only TLS older than 1.2, TLS 1.2 is
   added for the run and the setting put back afterwards; the system's
   default already offers it. PowerShell 7 follows no redirect from HTTPS
   to HTTP; Windows PowerShell 5.1 would follow one that GitHub sent.
4. **A release without a Windows CLI.** When `SHA256SUMS` lists no
   `koko-windows-amd64.exe` and the release is older than `v0.6.0`,
   install.ps1 says `koko <tag> has no Windows CLI; the first release with
   one is v0.6.0`. For a later release it says that `SHA256SUMS` lists no
   such file. Either way the file is never asked for.
5. **The download of `koko-windows-amd64.exe` and its checksum**, with
   `Get-FileHash`. A file whose SHA-256 differs is never installed.
   install.ps1 never runs the file.
6. **The CLI.** `%USERPROFILE%\.local\bin` is made when missing, the file
   is copied there as `.koko.new.exe`, and put in place by
   [the swap `koko update` uses](cli.md#the-swap), since a `koko.exe` may
   be running: `koko.exe` is renamed to `.koko.old-<random>.exe`, the new
   file is renamed to `koko.exe`, and the old one is renamed back when
   that fails. Then every `.koko.old-*.exe` in the directory, this run's,
   an earlier run's or `koko update`'s, is deleted; one that still runs
   cannot be, and is given the hidden attribute and left for the next run.
   It says `install.ps1: installed koko <tag> at <path>`, the path in
   full.
7. **PATH.** The user's PATH is the value `Path` under `HKCU:\Environment`,
   read with its variables unexpanded. When no entry of it is the
   directory, however the entry spells it (with a variable such as
   `%USERPROFILE%`, in quotes, in another case, with a `\` at its end),
   install.ps1 appends the directory, writes the value back with the
   registry type it had (`REG_EXPAND_SZ` when there was none), tells
   running programs that the environment changed (`WM_SETTINGCHANGE`,
   through `SendMessageTimeout`), adds the directory to the PATH of the
   PowerShell session it runs in, and says what it added and that a
   terminal already open must be reopened. When the directory is there
   already, it changes nothing and says nothing. A value that is not text
   is left alone, with the directory to add by hand. It never touches the
   machine's PATH.
8. **Another koko.** When `Get-Command koko` finds another file first than
   the one just installed, install.ps1 names it, and removes nothing.

Nothing prompts. install.ps1 writes its messages to the error stream, as
`install.ps1: ...`, and nothing to the output stream. A failure is a
terminating error whose message starts the same way, which sets a
non-zero exit status under `powershell -Command`; it never calls `exit`,
which would close the PowerShell the line was pasted into, so it has no
exit status 2 for a bad `KOKO_VERSION` as install.sh has. The line runs
text through `Invoke-Expression`, not a script file, so the default
execution policy does not stop it.

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
and macOS's. `release/` holds no Go code but the tests, so nothing imports
it, and it is in no [package group](code-layout.md#the-package-groups).

[`release/install_ps1_windows_test.go`](../../release/install_ps1_windows_test.go),
which only Windows builds, runs `install.ps1` as the line does: its text
given to `Invoke-Expression`, in a PowerShell started with `-NoProfile
-Command` whose stdin stays open, once under Windows PowerShell 5.1 and
once under PowerShell 7, both of which the machine must have. The fake
machine is a profile directory, a temporary directory and a session PATH
of its own, and PowerShell functions defined ahead of the script, which
PowerShell finds before the cmdlets of the same names: an
`Invoke-WebRequest` that serves a fake GitHub from a directory, follows
its redirects, logs every URL, refuses one that is not HTTPS, and answers
in the shape of the PowerShell it runs in; and a `Get-Item` and a
`Set-ItemProperty` that are the registry for the user's PATH alone, kept
in a file as its type and value, and refuse any other path. The
machine's architecture is set once PowerShell runs. The release's
`koko-windows-amd64.exe` is a few bytes, since nothing runs it. The tests
cover the latest release, read once; `KOKO_VERSION` with and without its
`v`, and values that are no version, with nothing asked of GitHub; a
checksum that does not match, a file `SHA256SUMS` does not list, a failed
download of either file, no release, and a `releases/latest` that leads
elsewhere; releases from before `v0.6.0` refused naming it; the directory
made with `koko.exe` alone in it, and nothing else written in the profile,
but for the `AppData` files each PowerShell keeps there for itself, or
left in the temporary directory; the user's PATH appended to, with its
type kept, for a value with variables, a plain one, an empty one and none,
and the session's PATH with it, or left alone for each spelling of the
directory; a `koko.exe` that runs, which is the test binary copied there
and started, renamed aside and hidden, with one that `koko update` left
deleted, and itself deleted by the next run once it has stopped; another
`koko.exe` first on PATH named and left; 32-bit Windows refused, and
Arm64 and a 32-bit PowerShell on 64-bit Windows given the amd64 file; a
`USERPROFILE` unset or naming a file; and, in every run, nothing on the
output stream. Telling running programs that PATH changed is the real
call, which changes nothing on the machine; the script would say so had it
failed. Not covered: the swap's rename back, TLS older than 1.2, and a
system that is not Windows.
[`release/install_ps1_test.go`](../../release/install_ps1_test.go) reads
the script on every OS: it is ASCII, nothing but comments stands outside
`Install-Koko` and its call on the last line, it never calls `exit`, it
reaches the network and the registry only through the cmdlets the fake
machine stands in for, and it installs the file `release.KokoAsset` names.
There too, a test reads `make -n dist`: every `koko-…` that `SHA256SUMS`
lists has the name `release.KokoAsset` gives its platform, the four
platforms are there, and both install scripts are copied and listed.

The release workflow runs only on a tag, and no test runs it; actionlint
checks it by hand.
[`release/oldest_check_test.go`](../../release/oldest_check_test.go) runs
`oldest-check.sh` on a git repository of releases made in a temporary
directory: a patch release, and a pre-release of one, that moves
`compat.Oldest` refused, naming both; one that keeps it, a minor release
that moves it, and a patch release after a release from before the
window passing; the previous release found by version, `v0.10.0` after
`v0.5.2`; a tag without `compat.Oldest`, a missing tag, and a previous
release whose `Oldest` is in a form the script cannot read, refused. A
test keeps `compat.Oldest` on the one line of `compat.go` that the script,
`skew-pairs.sh` and the real-systemd workflow read.
[`release/skew_pairs_test.go`](../../release/skew_pairs_test.go) runs
`skew-pairs.sh` on such a repository too: the pairs of a commit after a
release and of a release's own tag, one release that is both the previous
one and `compat.Oldest`'s listed once, the previous release found by
version and never a pre-release, and no pair for a release that is not
there yet or is below `compat.Oldest`. No test runs `skew-check.sh`, which
needs Docker and the network: [the skew check](#the-skew-check) is its
run. [The container check](#the-container-check) runs what
`make image` builds: its index's platforms and binaries, and, by running
it, its user, entrypoint, command, environment, `/koko` and `/run/koko`.
Its `EXPOSE` and labels are checked by hand.

## The check workflow

The [check workflow](../../.github/workflows/check.yml) runs `make check`
on every push to `main`, on every pull request, and on a release's tag. Its
**check** job runs once on `ubuntu-latest` and once on `macos-latest`, each
with a limit of 20 minutes:

1. It sets up Go and Node, and takes golangci-lint's binary and the cache
   of its last run from the workflow's cache. Where the cache holds none
   (a new golangci-lint, Go or `go.mod`), it builds the binary with
   `go install`, which takes about a minute.
2. On Ubuntu it checks that the tools of
   [the real-OpenSSH test](code-layout.md#the-real-openssh-test) are
   installed, and runs apt only for a missing one. The runner's image ships
   them all.
3. `npm ci` and `npm audit signatures` in `web/`.
4. `make check`: the web UI's build, lint for each OS koko builds for,
   vet, the Go tests with `-race`, Vitest, and the cross builds.

What each step of the Ubuntu job took on `main` on 2026-11-11, on the
fastest and the slowest of five runs (#161):

| Step | Seconds |
| --- | --- |
| Checkout, Go and Node | 21 to 24 |
| Building golangci-lint (before its binary was cached) | 56 to 72 |
| The OpenSSH tools | 5 with them installed |
| `npm ci` and `npm audit signatures` | 10 to 14 |
| `make webui` | 2 to 3 |
| `make lint`, three OSes | 84 to 109 |
| `go vet` and `go test -race` | 226 to 291 |
| Vitest | 60 to 93 |
| `make cross`, four targets | 63 to 79 |

The Go tests' time is that of `internal/client/cli` (168 to 218 s) and
`internal/server/access` (161 to 211 s), which run side by side; every
other package takes under 45 s. The same commit's steps take up to 1.3
times as long on a slow runner as on a fast one. The Ubuntu job is expected
to take 8 to 10 minutes and the macOS job 7 to 12. If the Ubuntu job grows
past 12 minutes, move `make lint` and `make cross` into a job of their own
beside the tests.

### The `windows` job

The workflow has a second job, `windows`, on `windows-latest`, beside the
**check** job's two runs, on every push and pull request, and so before a
release
([ADR 0051](../adr/0051-on-windows-the-cli-is-a-device-with-the-same-files-in-the-same-places.md)).
It is what the tickets of
[#151](https://github.com/cieons/koko/issues/151) build the Windows CLI
against, and where [install.ps1's tests](#verifying) run.

**What it runs.** `go vet ./...`, then `go test`, without the race
detector, on `./cmd/...`, `./internal/shared/...`, `./internal/client/...`,
`./internal/harness/...` and `./release/...`. It calls `go` itself, since
the image has no GNU Make, under a temporary `USERPROFILE` and
`LOCALAPPDATA`, as `make test` runs under a temporary `HOME`, with Go's
caches pinned first. `go test` bounds a test binary at 10 minutes and the
job ends at 20.

**How long it takes.** `internal/client/cli` took 138 to 233 s in five
runs on 2026-11-11 and 2026-11-12, and every other package under 2 s; the
job took 4 to 6 minutes. The same package takes 168 to 218 s in the Ubuntu
job, which runs it under the race detector. With the Device Mux's tests
running on Windows too ([#153](https://github.com/cieons/koko/issues/153)),
`internal/client/cli` took 156 and 172 s in two runs on 2026-11-12, and
`internal/client/devicemux` about 6 s. `release`, which starts a
PowerShell for each run of install.ps1, takes about 40 s beside them. If
`internal/client/cli` passes 5 minutes on
Windows, the next ticket that adds tests there says so before it adds
them.

**What it does not run.** `./internal/server/...`: the koko server ships
for Linux, and is tested there and on macOS. The web UI's build and
Vitest, which do not differ by OS. Lint, which `make lint` runs for
`GOOS=windows` in the other two jobs. The race detector, which needs a C
toolchain everywhere but on macOS; the Linux and macOS jobs run the same Go
code under it, but for the per-OS files.

**The real OpenSSH test on Windows**
([#155](https://github.com/cieons/koko/issues/155);
[code layout](code-layout.md#the-real-openssh-test)) builds `koko.exe`,
joins under a profile directory named `Alice Smith`, and runs OpenSSH for
Windows (`ssh`, `scp`, `sftp`, a local forward) and Git for Windows' `ssh`
and `scp` through `koko tunnel` and a real Device Mux. It settled the
spec's last open point on 2026-11-12: OpenSSH for Windows 9.5p2 starts the
ProxyCommand's double-quoted koko path written with forward slashes, a
space in it, so the fallback to single backslashes was not needed; Git
for Windows' OpenSSH 10.6p1 runs the same line through its `sh`. Git's
client reads the `~` of `UserKnownHostsFile` from `HOME`, and from
`USERPROFILE` when `HOME` is unset: a run without `HOME` passed too.
`internal/client/openssh` took 13 to 17 s in four runs on the runner, and
`internal/client/cli` 199 to 212 s beside it.

**The harness on Windows.** The CLI's tests start the koko server in the
test process, as on macOS; `kokod` itself still refuses to start there.
The in-process Host runs a command through the `sh.exe` of Git for
Windows, found on PATH, and a test that needs one fails naming it when
there is none. A test that cannot hold on Windows is left out through
`skip.OnWindows`, with its reason, and counted
([code layout](code-layout.md#rules-for-working-in-parallel), rule 13).
`.gitattributes` checks every file out with the bytes it was committed
with, so the golden files compare on Windows too.

**What the first run found** (2026-11-12, for the points
[#151](https://github.com/cieons/koko/issues/151) left to verify):

- **The in-process koko server runs on Windows:** its Unix socket, its
  files and SQLite. Three things differed, each now behind a per-OS file or
  a check that holds on every OS, with nothing changed on Linux:
  - Windows flushes only a file opened for writing, and no directory, so
    the store's `syncPath` has a file for each kind of OS;
  - a socket call fails with Windows' own error numbers (`WSAECONNREFUSED`
    for `ECONNREFUSED`), which Go does not match to the Unix ones:
    [`sockerr`](../../internal/shared/sockerr/sockerr.go) matches both, for
    the Gateway's retries, a taken port and a socket left behind;
  - connecting to a Unix socket path that holds nothing is refused on
    Windows, where Linux and macOS say the path does not exist, so
    `serverconn.DialSocket` looks at the path after a failed connect.
- **The clock moves in coarse steps on Windows**, so two moments a few
  milliseconds apart can read as the same time. `liveness.Watch` told an
  answered probe by comparing two times, and a probe sent in the same step
  as the last bytes counted as answered: the connection was never declared
  dead. It now counts the reads that brought bytes.
- **The ssh clients on the runner:** `where ssh` finds
  `C:\Windows\System32\OpenSSH\ssh.exe` first, OpenSSH for Windows 9.5p2
  (LibreSSL 3.8.2), then `C:\Program Files\Git\usr\bin\ssh.exe`, OpenSSH
  10.6p1 (OpenSSL 3.5.9). Git for Windows' `sftp-server.exe` is
  `C:\Program Files\Git\usr\lib\ssh\sftp-server.exe`, and its `sh.exe`,
  `C:\Program Files\Git\bin\sh.exe`, is on PATH. The job's first step
  prints all of these on each run.
- **A Unix socket's close can go unseen on Windows.** When the end that
  reads is itself writing as its peer half-closes, the read sometimes
  never returns, neither for the half-close nor for the close that
  follows: once in 400 connections of plain `net` sockets on the runner,
  and never over TCP or without that write. `koko web` over the koko
  server's Unix socket met it when the koko server stopped (3 runs of 25),
  a path only tests take on Windows, so
  `TestWebEndsWhenTheKokoServerStops` is left out there. The Device Mux on
  Windows ([#153](https://github.com/cieons/koko/issues/153)) listens on a
  Unix socket, so there both ends of a command's connection to it probe
  the other once it goes quiet and close it once a probe goes unanswered
  ([the Device Mux](device-mux.md#the-local-protocol)). How rare it is:
  #153 probed plain `net` sockets on the runner, the reading end writing
  all the while, and no end went unseen in 4,000 connections the peer
  closed, 4,000 it half-closed and then closed, and 400 whose peer
  process exited. It is rarer than #152's one sighting in 400 suggested,
  but `koko web` over SSH met it, and whatever brings it about, the probe
  bounds it.

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
so the workflow first tags the commit and the older one, in its checkout
only, never pushed. The CLI and the koko server turn away each other below
`compat.Oldest` ([the window](api.md#the-way-in)), so the tags are
pre-releases of the patch after it, read from
[`compat.go`](../../internal/shared/compat/compat.go): `v0.3.1-check.2`
for the commit and `v0.3.1-check.1` for the older one while it is
`v0.3.0`. On a release's tag from that patch on, the release's tag is the
higher, and Go stamps it. For `koko server upgrade` it then builds two
releases newer than the commit's, of `koko-linux-amd64` and
`kokod-linux-amd64` alone, with the commit's web UI and a `SHA256SUMS`:
each in a worktree of its own, on an empty commit of its own tagged a
pre-release of the patch after the commit's build (`-check.3` and
`-check.4`), so that no other tag on it wins and both stay newer than it.
The second is built with `go build -overlay`, which keeps Go's version
stamp: its `main` exits at once for `kokod run` and is kokod's own
otherwise, so that its `kokod install` still upgrades and its koko service
fails at its start, with no test code in the repository. Then
[`release/systemd-check.sh`](../../release/systemd-check.sh)
`<older dist> <newer dist> <upgrade dist> <failing dist>` checks, in order:

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
   `koko.db.before-<n>` snapshot is there, the upgrade helper's path unit
   is active and enabled, and the device still works. Then the device
   installs the newer CLI, as **The device** below says;
6. `sudo koko server upgrade --version <upgrade>` goes through the real
   path unit and the root helper ([the installer](installer.md#the-upgrade-helper)):
   its JSON says the koko server and root's koko went from the newer
   release to the upgrade release, `outcome.json` says `succeeded`,
   `koko-upgrade.service`'s result is `success` and its journal names the
   release, the fake GitHub served that release's `kokod` and `koko` once
   each, the request and the download are gone, `kokod.previous` is the
   newer build's, and the service runs the new `kokod` as `koko` under a
   new process, with no capability, and the device still works;
7. `koko server upgrade --version <failing>` exits 1 with
   `upgrade_failed`: `outcome.json` says `failed` and that the previous
   kokod runs again, the helper downloaded that release's `kokod` once and
   koko nothing, and the service runs the upgrade release's `kokod` again,
   kept as `kokod.previous`, under a new process. The failing `kokod`
   exited before it opened the database, so the earlier snapshot is still
   there and nothing was set aside as `koko.db.failed-<to>`. The device
   still works;
8. `koko server uninstall`, running the `kokod` installed and asking the
   fake GitHub nothing, leaves nothing running or listening, removes the
   unit, the helper's two units, `/var/lib/koko-upgrade` and `kokod`, and
   keeps `/usr/local/bin/koko`, `koko.db`, the Root Key and the `koko`
   user;
9. with the drop-in removed, and after `chown -R root:root /var/lib/koko`,
   as a copy put back by root would be, `sudo koko server install` without
   arguments downloads the upgrade release's `kokod` again, once, since
   uninstall removed it, and starts the koko server again on the default
   port, 7822, saying devices must connect to `localhost:7822`, as
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

**The device** is a Docker container on the host's network, of
`buildpack-deps:24.04-curl`, Docker's official Ubuntu 24.04 image with
`curl` and a trust store (`ca-certificates`) already in it. The script names
it by digest and pulls it before the koko server starts, so no step installs
a package or needs Ubuntu's archive once the check is under way; to move to
a newer image, change the digest in the script. The device has the older
build's binary copied in as its `/usr/local/bin/koko`. Its own filesystem
has no `/run/koko/koko.sock`, so its commands go over TCP to
`localhost:8443`, then `localhost:7822` from step 9 on, as a device's do,
through its Device Mux, and not through the Unix socket as the Local
caller. It keeps the older CLI through the upgrade, as a device does when
its koko server is upgraded. After step 5,
it installs the newer CLI as a device does: with its image's `curl`, the
test CA added to its own trust store (`update-ca-certificates`), `github.com`
mapped by `--add-host`, and the newer install.sh run without arguments. It
then has the newer `/usr/local/bin/koko`, downloaded once, and no
`kokod`, and lists the Users with it, through a Device Mux of the newer
build.

The script refuses a machine where koko was installed, or where something
listens on 443, and leaves koko's data, Root Key and user behind: run it
only on a throwaway machine with systemd, Docker, `jq`, `ss`, `python3`,
`openssl`, `update-ca-certificates` and passwordless `sudo`. On a failure it
prints the koko service's and the upgrade helper's journals and the
helper's outcome, the device's Device Mux log, and the fake GitHub's
requests and log.

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

## The skew check

The CLI and the koko server work together within a window of versions
([ADR 0049](../adr/0049-the-cli-and-the-koko-server-work-together-within-a-window-of-versions.md)),
and the [skew workflow](../../.github/workflows/skew.yml) runs real
binaries of different versions against each other, on every push to `main`
and on a release's tag before the build job, which needs it.

**The pairs.** [`release/skew-pairs.sh`](../../release/skew-pairs.sh)
lists them for the commit checked out, as the JSON list the workflow takes
as its matrix; a pair is a CLI and a kokod, each a release's tag or
`this`, the commit's own build:

- this CLI with the previous release's kokod, and with `compat.Oldest`'s;
- the previous release's CLI with this kokod, and `compat.Oldest`'s.

The previous release is the highest release tag, without a pre-release
part, in the commit's history and not on the commit itself, so a release's
tag is checked against the release before it. `compat.Oldest` is read from
its one line in `compat.go`. When the two are one release, its pairs are
listed once. A release that is not there yet has no pair, as
`compat.Oldest`'s on the commits that lead to the minor release it names,
and neither has a previous release below `compat.Oldest`, which the build
turns away; the script says so on stderr, and a commit with no pair runs
the build job alone.

**The workflow.** Its **build** job runs `make dist` and `make image`
once, as the release's build job does, lists the pairs, and uploads
`koko-linux-amd64` and the image's archive as the artifact `skew`. Its
**pair** jobs, one for each pair, run in parallel on `ubuntu-24.04`
runners: each downloads the artifact and runs `release/skew-check.sh`
with `dist/koko-linux-amd64` or `dist/koko-image.tar` for `this`; a pair
with this kokod sets up Go first, for step 7. The build job takes about
as long as the container check's build, and a pair under a minute.

**One pair.** [`release/skew-check.sh`](../../release/skew-check.sh)
`<cli> <kokod>` takes, for each, a release's tag or a path: the path of a
`koko` binary for Linux and the machine's architecture, and of the image's
archive. Every failure names the step it came in, and shows `docker logs`
of the koko server and the device's Device Mux log. In order:

1. **the CLI**: a release's `koko-linux-<arch>` is downloaded from its
   GitHub Release and checked against that release's `SHA256SUMS`;
2. **the kokod**: a release's image, `ghcr.io/cieons/koko:X.Y.Z`, is
   pulled; an archive is loaded with `skopeo`, as the container check
   loads it;
3. the koko server starts from its image on a Docker network of the
   check's own, with `KOKO_OWNER=alice` and its container's name as
   `KOKO_ADDRESS`, and initialises at its first start; `docker exec`
   `koko device add` issues a Join Code once it answers, and the image's
   `kokod version` is the tag asked for;
4. the CLI, copied into an `alpine` container as its `koko`, is the version
   asked for and joins with the Join Code; `koko status` reports the koko
   server reached, with its version and no error;
5. `koko space add ops`, `koko host add web1` and `koko host enroll web1
   --key` make the sshd container a Managed Host with a pinned host key;
6. `ssh web1.koko true`, through koko's ssh_config, and `koko exec web1
   true` both exit 0;
7. **the web terminal through `koko web`**, against this kokod alone: the
   check builds [`cmd/skewterm`](../../cmd/skewterm/main.go), the web
   terminal's page, from the commit, and runs it on the device beside the
   CLI's `koko web --no-open --json`. It trades the printed address's
   token for the `koko_<port>` cookie, opens an SSH Session to `web1`
   over `/terminal/socket` with `koko web`'s own Origin, sends the shell
   `echo koko-skew-$((6*7))`, reads `koko-skew-42`, which only the
   shell's answer holds, and closes the stream as the page's tab does;
   the koko server then logs the SSH Session ended with `client=browser`
   and `reason="client closed"`. The page and the koko server are one
   build, and `koko web` of another release forwards the WebSocket's
   upgrade and frames as they are, which this shows for the previous
   release's CLI and `compat.Oldest`'s. With a released kokod the step
   runs nothing: the CLI is then this build, whose forwarder the S1 tests
   cover (`internal/client/cli/web_terminal_test.go`), and a released
   kokod may have no web terminal;
8. a command that sends what the kokod lacks fails with `server_too_old`,
   exit 1, and a message naming the kokod's version, the command and
   `upgrade the koko server`, and changes nothing: `koko host set web1
   web2 provider=acme`, several Hosts in one `host.set`, which a kokod
   before v0.5.0 lacks ([ADR 0050](../adr/0050-a-write-of-several-records-is-applied-all-or-none.md)).

**The Host and the device** are `alpine` containers on the check's
network. The Host installs OpenSSH's server and runs `sshd`, with `root`
as its Host Account. The device installs OpenSSH's client and makes a key
of its own, which the check writes as the Host's `authorized_keys`, and
supplies it to `koko host enroll --key`, as a User who already logs in to
the Host with a key does.

**What step 8 does not run.** It runs only for this CLI with a kokod
before v0.5.0, the script's `batch_since`. A kokod from v0.5.0 on lacks
nothing the script knows, and the step says so: when a later operation's
revision rises, or `compat.Oldest` passes v0.5.0, the script names the
next such command. With a released CLI and this kokod nothing is run
either: a CLI within the window sends nothing this kokod lacks, and
`cli_too_old` comes only to a CLI below `compat.Oldest`, which no pair
holds; every release before v0.3.0 states no version and is taken as
v0.3.0. The S1 tests cover `cli_too_old`
([`internal/client/cli/skew_test.go`](../../internal/client/cli/skew_test.go)).

**Running one pair locally** needs Docker, `curl`, `jq` and `sha256sum`,
on Linux or macOS, and `skopeo` and Go for an archive, run from the
commit's clone; it takes under a minute
once the images are pulled, and removes the containers, the network and
the loaded image it made:

```sh
make dist && make image
release/skew-check.sh dist/koko-linux-arm64 v0.3.0   # this CLI, a release's kokod
release/skew-check.sh v0.3.0 dist/koko-image.tar     # a release's CLI, this kokod
```

`dist/koko-linux-amd64` is the CLI on an x86-64 machine. Two releases
pair as well: `release/skew-check.sh v0.5.0 v0.3.0`.
