# koko

> Your servers, all here.

[中文](README.zh-CN.md)

koko is a self-hosted asset gateway for the servers you rent across
providers. It keeps the inventory, holds every credential, and is the one
door people and AI agents use to reach your machines.

**Status:** early design. Nothing is usable yet.

## Planned

- An asset inventory: providers, hosts, services, domains, renewal dates.
- Credentials that never leave the server; hosts trust short-lived SSH
  certificates.
- A terminating SSH gateway behind a single HTTPS port, so `ssh`, `scp`,
  `rsync` and Ansible work unchanged.
- A dynamic Ansible inventory and runbooks.
- CLI first, with a built-in skill for coding agents (`koko skill`).

## License

[Apache-2.0](LICENSE)
