# koko

> Your servers, all in koko.

[中文](README.zh-CN.md)

koko is self-hosted. It keeps the record of servers rented across
providers, and it is the one door people and AI agents use to reach them.

**Status:** v0.3.0, the first release in which `koko`, the CLI, and
`kokod`, the koko server, are two programs. It provides the features listed
below and is still at an early stage.

## Installing

koko is two programs: `koko` is the CLI, on Linux and macOS, and `kokod`
is the koko server, on Linux.

The CLI, on Linux (amd64 or arm64) or macOS (Apple Silicon):

```sh
curl -fsSL https://github.com/cieons/koko/releases/latest/download/install.sh | sh
```

The koko server as well, on a Linux machine with systemd, naming the Owner
and the address devices will use to reach it:

```sh
curl -fsSL https://github.com/cieons/koko/releases/latest/download/install.sh | sh -s -- --owner alice --address koko.example.com
```

When not running as root, change the `sh` after the pipe to `sudo sh` in
either line.

[Installing koko](docs/install.md) describes what each line does, and covers
upgrading, going back and restoring. Without systemd, run the image
`ghcr.io/cieons/koko` with Docker
([In a container](docs/install.md#in-a-container)).

## What it does

- A record of hosts and the services on them, described by typed
  attributes such as provider, region and renewal date.
- Spaces that decide who can see and reach which hosts.
- Secrets: passwords, keys and tokens kept in a Space, sealed under a key
  that never sits beside the data, and printed only by
  `koko secret show`. Credentials go only into Secrets: notes and
  attributes are stored plain.
- A gateway that ends every SSH session, so only koko holds a key the
  hosts trust.
- Enrolling a host adds one public key to it and changes nothing else.
- One SSH port (7822 by default) and no certificates; `ssh`, `scp` and
  `rsync` work unchanged through it.
- CLI first, with a built-in skill for coding agents (`koko skill`).

## License

[Apache-2.0](LICENSE)
