# koko

> Your servers, all here.

[中文](README.zh-CN.md)

koko is self-hosted. It keeps the record of the servers you rent across
providers, and it is the one door people and AI agents use to reach them.

**Status:** early design. Nothing is usable yet.

## Planned

- A record of your hosts and the services on them, described by typed
  attributes such as provider, region and renewal date.
- Spaces that decide who can see and reach which hosts.
- A gateway that ends every SSH session, so only koko holds a key your
  hosts trust.
- Enrolling a host adds one public key to it and changes nothing else.
- One HTTPS port; `ssh`, `scp` and `rsync` work unchanged through it.
- CLI first, with a built-in skill for coding agents (`koko skill`).

## License

[Apache-2.0](LICENSE)
