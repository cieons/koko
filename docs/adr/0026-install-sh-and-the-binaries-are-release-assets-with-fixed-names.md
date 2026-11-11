---
status: refined by ADR-0041; its readers of the URLs, refined by ADR-0047
---

# install.sh and the binaries are release assets with fixed names

Each koko release on GitHub publishes `install.sh`, one binary per platform,
named `koko-<os>-<arch>` (`koko-linux-amd64`, `koko-linux-arm64`,
`koko-darwin-arm64`), and one `SHA256SUMS` listing them. The line that
installs koko is

```
curl -fsSL https://github.com/cieons/koko/releases/latest/download/install.sh | sh -s -- --owner alice
```

GitHub redirects `releases/latest/download/<asset>` to the latest release's
asset, so the line never changes, and the script it runs is always one that
was released and tested with the binaries beside it, never work in progress
on `main`. The binaries are bare files, not archives, and their names carry
no version, so one `install.sh` names the binary of any release
`KOKO_VERSION` pins, and every release's binary has a link that always
works.

## Considered Options

- **`install.sh` from `main` on raw.githubusercontent.com**: no release
  asset, but every pasted line runs whatever `main` holds at that moment,
  against binaries it was never released with.
- **A domain of koko's own, such as `get.koko.sh`**: a shorter line, but a
  domain and a host to keep, and one more party the installing person
  trusts.
- **`.tar.gz` archives**: room for more files, but a release has one file to
  install, and the script would need `tar` and an extract step.
- **The version in each asset's name, such as `koko_0.3.0_linux_amd64`**:
  self-describing files, but `install.sh` must learn the version before it
  can name the file, and no link to the latest binary stays the same.

## Consequences

- The URL and the asset names are a contract: docs, agents' prompts and
  scripts paste them, and every later `install.sh` must install every
  earlier release by these names. Renaming one needs the old name published
  beside it.
- The repository's path is in the line. Moving the repository relies on
  GitHub's redirect from the old path.
- `install.sh` reads which release is the latest once, from the redirect of
  `releases/latest`, so the binary and `SHA256SUMS` it downloads are always
  from the same release.
- A tag with a pre-release part, such as `v0.3.0-rc.1`, is published as a
  pre-release, which `releases/latest` skips: only `KOKO_VERSION` installs
  it.
- Downloading the assets without signing in needs the repository public, and
  so do artifact attestations on GitHub plans below Enterprise Cloud.
