---
status: refined by ADR-0041
---

# The koko server installs itself and has no configuration file

The koko binary installs its own koko server. A thin `install.sh` downloads
the release binary and checks it, then `koko server install` creates the
`koko` system user and the data directory and writes the systemd unit from a
template inside the binary. On a fresh machine the same command initialises
the koko server over the Unix socket and prints the first Join Code.
Re-running it with a newer binary is the upgrade, so the unit always matches
the binary that runs it, on any Linux with systemd. There is no
configuration file. The settings the process needs before it opens its
database are flags, each with a `KOKO_*` environment variable, written into
the unit by `koko server install`. The settings of the deployment itself,
for now only the Server Address, live in the database and change through the
API over the Unix socket, where the CLI validates them.

## Considered Options

- **.deb and .rpm packages with apt and yum repositories**: upgrades through
  the system's package manager, but two package formats and a repository to
  host, and every other distribution still needs another path.
- **Documented manual steps** (download, copy a unit, `useradd`, `systemctl
  enable`): nothing to build, but every step is the user's or the agent's to
  get right, and the unit drifts from the binary across upgrades.
- **One configuration file, such as `/etc/koko/server.toml`**: familiar, but
  a file format and a parser to own, edits that nothing validates until the
  next restart, and a second home for settings the API already serves.
- **Every setting in the database, the listen port included**: one source,
  but a wrong port stops the server that serves the API needed to fix it.

## Consequences

- To change a flag, such as the port, re-run `koko server install` with it
  or add a systemd drop-in.
- The Server Address is set at initialisation, and its port defaults to the
  listen port. The Owner can change it over the Unix socket, and the change
  reaches only Join Codes issued afterwards. When the address changes, or
  the default port does, joined devices must be told the new address by
  hand (`koko connect <host:port>`), because nothing moves a device to a new
  address by itself.
- The unit hands the Root Key (ADR 0018) to the service from
  `/etc/koko/root.key` with `LoadCredential=`, so `koko server install`
  refuses systemd older than 247.
- Install never takes a port that is already in use: it names the process
  holding it and stops.
- `koko server uninstall` removes the unit and the binary. It keeps the data
  directory, the Root Key and the `koko` user.
- P1's container image has no systemd. It needs its own way to initialise.
