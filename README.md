# koko

> Your servers, all here.

[中文](README.zh-CN.md)

koko is self-hosted. It keeps the record of the servers you rent across
providers, and it is the one door people and AI agents use to reach them.

**Status:** v0.1.0, the first release. It does what the list below says;
expect rough edges.

## Installing

[Installing and running the koko server](docs/install.md): one line on a
Linux machine with systemd, then upgrading, going back and restoring.
Without systemd, run the image `ghcr.io/cieons/koko` with Docker
([In a container](docs/install.md#in-a-container)).

## What it does

- A record of your hosts and the services on them, described by typed
  attributes such as provider, region and renewal date.
- Spaces that decide who can see and reach which hosts.
- Secrets: passwords, keys and tokens kept in a Space, sealed under a key
  that never sits beside the data, and printed only by
  `koko secret show`. Credentials go only into Secrets: notes and
  attributes are stored plain.
- A gateway that ends every SSH session, so only koko holds a key your
  hosts trust.
- Enrolling a host adds one public key to it and changes nothing else.
- One SSH port (7822 by default) and no certificates; `ssh`, `scp` and
  `rsync` work unchanged through it.
- CLI first, with a built-in skill for coding agents (`koko skill`).

## License

[Apache-2.0](LICENSE)
