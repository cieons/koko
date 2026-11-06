# Subsystem references

Each page describes one subsystem as it is now: responsibilities and
boundaries, source of truth, runtime paths, state transitions, constraints,
failure behaviour, and how to verify it. Why a design was chosen lives in an
ADR; a proposed change lives in its issue or PR.

| Subsystem | Reference |
| --- | --- |
| Code layout: the package groups, where an area adds its parts, and the rules for working in parallel | [code-layout.md](code-layout.md) |
| The API: the Unix socket, channels, the version and operations report, calls, error codes, the API client, the operation framework and `Idempotency-Key` | [api.md](api.md) |
| The CLI: the command tree, parser, runner, output layer and `--json` | [cli.md](cli.md) |
| The store: SQLite, transactions, migrations and record ids | [store.md](store.md) |
| The Seal: the Root Key's file, sealed values, and the Root Key at start-up | [seal.md](seal.md) |
| The web UI build: Node, the embedded app and placeholder, the generated types, page tests | [web-ui.md](web-ui.md) |
| The web UI shell: routing, the header and who the web UI acts as, language and catalogues, the API client, confirmations, edit forms, notices | [web-shell.md](web-shell.md) |
| The Spaces, People and Devices pages: who sees and does what, what asks first, and a Join Code shown once | [people-pages.md](people-pages.md) |
| The Host connector: logging in to a Host through its Relay, the pinned host key and connect retries | [host-connector.md](host-connector.md) |
| Enroll: `koko host enroll`, its inputs, the host key it pins, the states, and the one line it adds to `authorized_keys` | [enroll.md](enroll.md) |
| The resumable stream: the byte stream beneath a Tunnel, its frames, offset exchange and replay buffer | [resumable-stream.md](resumable-stream.md) |
| The Gateway, the Tunnel, `koko exec` and `koko tunnel`: the `tunnel@koko` channel, its open payload and Tunnel id, who may open an SSH Session and the refusals, the relay to the Host and its forwards, the log lines, `koko exec`, and `koko tunnel`, ssh's ProxyCommand | [gateway.md](gateway.md) |
| Initialisation: the Owner, the koko server's own keys and the Server Address, what answers before it, and `server show` and `server set` after | [initialisation.md](initialisation.md) |
| The installer: `koko server install`, the upgrade, `koko server uninstall`, the machine interface, the unit, the checks before any write, and the fake machine | [installer.md](installer.md) |
| The release and `install.sh`: the assets and their names, `make dist`, the release workflow, and what `install.sh` checks before it runs the binary | [release.md](release.md) |
| Join Codes: the `koko_` string, issuing them, device names, and the hash kept of each | [join-codes.md](join-codes.md) |
| `koko web` and the web front: the listener, the `web@koko` channel, the Host guard, the token and the cookie, the port-owner lookup | [web-front.md](web-front.md) |
| The Device Connection and joining: the TCP listener, device authentication, `koko join`, the device's files, koko's ssh_config, known_hosts and the Include line, which koko server the CLI reaches, last used, revoking a Device Session, the access-change signal and the revocation record | [device-connection.md](device-connection.md) |
| The Device Mux: which way a command goes and the direct path, starting it, the lock, its socket and runtime directory, the local protocol, its state, the idle exit and the revocation disconnect | [device-mux.md](device-mux.md) |
| The access check and Spaces: who is calling, the rule, unseen equals unknown, memberships, what a Space holds, the access matrix | [access.md](access.md) |
| Users: the record, `user ls`, and `user remove` deleting a User who never joined or Removing anyone else | [users.md](users.md) |
| The record: a Host's fields and their rules, a Service and its ports, Attributes, the Preset Attributes and the four types, the Note, the host and service operations and `attr.list`, filtering and sorting, relative dates, the one leak | [record.md](record.md) |
| Secrets: the record and its name rule, the commands and the one that prints a value, values in JSON, access, sealing | [secrets.md](secrets.md) |

## Maintaining a reference

Update the owning reference in the same change when that change alters a
documented responsibility, interface, state transition, runtime path,
compatibility rule or failure behaviour. A refactor that keeps these
contracts needs no prose change.

Keep each fact in one reference and link to it from elsewhere. Link source
files and named symbols rather than copying implementation detail that
changes often.
