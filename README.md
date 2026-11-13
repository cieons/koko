# koko

> Your servers, all in koko.

[中文](README.zh-CN.md)

koko is self-hosted. It keeps the record of servers rented across
providers, and it is the one door people and AI agents use to reach them.

**Status:** koko provides the features listed below and is still at an
early stage. The [releases](https://github.com/cieons/koko/releases) list
each version and what it changed.

## Installing

koko is two programs: `koko` is the CLI, on Linux, macOS and Windows, and
`kokod` is the koko server, on Linux.

The CLI, on Linux (amd64 or arm64) or macOS (Apple Silicon), for the user
who runs it, in `~/.local/bin`, without `sudo`:

```sh
curl -fsSL https://github.com/cieons/koko/releases/latest/download/install.sh | sh
```

It prints the line that puts `~/.local/bin` on `PATH` when it is not
there.

The CLI, on Windows 10 (1809 and later), Windows 11 or Windows Server 2019
and later, from v0.6.0 on, for the user who runs it, in
`%USERPROFILE%\.local\bin`, without administrator rights, in PowerShell:

```powershell
irm https://github.com/cieons/koko/releases/latest/download/install.ps1 | iex
```

It adds that directory to the user's `PATH` when it is not there, and says
so ([On Windows](docs/install.md#on-windows)).

`koko update` upgrades the CLI later, on every system.

The koko server as well, on a Linux machine with systemd, as root, naming
the Owner and the address devices will use to reach it:

```sh
curl -fsSL https://github.com/cieons/koko/releases/latest/download/install.sh | sudo sh -s -- --owner alice --address koko.example.com
```

As the root user, drop the `sudo`.

[Installing koko](docs/install.md) describes what each line does, and covers
upgrading, going back and restoring. Without systemd, run the image
`ghcr.io/cieons/koko` with Docker
([In a container](docs/install.md#in-a-container)).

The web UI opens with `koko web` on any joined device. People who do not
use the CLI, such as a teammate on a phone, use it in a browser instead:
the koko server serves it to a reverse proxy, such as Caddy, that holds a
domain and its certificate, and each person signs in with a Password,
which they set through a one-time Password Link the Owner or an Administrator issues. Signed in, they
keep the record their Spaces allow. Either way, the web UI's terminal opens
SSH Sessions, each in a tab of its own, to the Hosts the person may reach,
and its Files panel uploads and downloads files on them
([The web UI in a browser](docs/install.md#the-web-ui-in-a-browser)).

## What it does

- A record of hosts and the services on them, described by typed
  attributes such as provider, region and renewal date.
- Spaces that decide who can see and reach which hosts.
- One Owner, who may make trusted people Administrators to run the koko
  server with them. Once it has Administrators, the koko server's own
  machine is best kept out of its record: a shell there acts as the Owner.
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
