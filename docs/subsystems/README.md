# Subsystem references

Each page describes one subsystem as it is now: responsibilities and
boundaries, source of truth, runtime paths, state transitions, constraints,
failure behaviour, and how to verify it. Why a design was chosen lives in an
ADR; a proposed change lives in its issue or PR.

| Subsystem | Reference |
| --- | --- |
| Code layout: the package groups, where an area adds its parts, and the rules for working in parallel | [code-layout.md](code-layout.md) |
| The API: the Unix socket, channels, calls, error codes and the operation framework | [api.md](api.md) |
| The CLI: the command tree, parser, runner, output layer and `--json` | [cli.md](cli.md) |
| The store: SQLite, transactions, migrations and record ids | [store.md](store.md) |
| The Seal: the Root Key's file, sealed values, and the Root Key at start-up | [seal.md](seal.md) |
| The web UI build: Node, the embedded app and placeholder, the generated types, page tests | [web-ui.md](web-ui.md) |
| The Host connector: logging in to a Host through its Relay, the pinned host key and connect retries | [host-connector.md](host-connector.md) |
| The resumable stream: the byte stream beneath a Tunnel, its frames, offset exchange and replay buffer | [resumable-stream.md](resumable-stream.md) |
| Initialisation: the Owner, the koko server's own keys and the Server Address, what answers before it, and `server show` and `server set` after | [initialisation.md](initialisation.md) |
| The installer: `koko server install`, the machine interface, the unit, the checks before any write, and the fake machine | [installer.md](installer.md) |
| The release and `install.sh`: the assets and their names, `make dist`, the release workflow, and what `install.sh` checks before it runs the binary | [release.md](release.md) |
| Join Codes: the `koko_` string, issuing them, device names, and the hash kept of each | [join-codes.md](join-codes.md) |
| `koko web` and the web front: the listener, the `web@koko` channel, the Host guard, the token and the cookie, the port-owner lookup | [web-front.md](web-front.md) |
| The Device Connection and joining: the TCP listener, device authentication, `koko join`, the device's files and which koko server the CLI reaches | [device-connection.md](device-connection.md) |
| The access check and Spaces: who is calling, the rule, unseen equals unknown, memberships, what a Space holds, the access matrix | [access.md](access.md) |

## Maintaining a reference

Update the owning reference in the same change when that change alters a
documented responsibility, interface, state transition, runtime path,
compatibility rule or failure behaviour. A refactor that keeps these
contracts needs no prose change.

Keep each fact in one reference and link to it from elsewhere. Link source
files and named symbols rather than copying implementation detail that
changes often.
