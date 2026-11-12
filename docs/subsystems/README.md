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
| The web UI build: Node, HeroUI and Tailwind, the size budget, the embedded app and placeholder, the generated types, page tests, the static checks and the real-browser check | [web-ui.md](web-ui.md) |
| The web UI shell: the frame (the top bar with its places, Language, Appearance and the account panel, and the phone's bar), routing and who the web UI acts as, language, mode and catalogues, the API client, the states every screen has, confirmations, edit forms, notices, what a page shows once, the page inventory | [web-shell.md](web-shell.md) |
| The Access and Account pages: the Users against the Spaces, a Space's and a person's panels, one's own devices and Browser Sign-ins, who sees and does what, what asks first, and a Join Code or Password Link shown once | [access-pages.md](access-pages.md) |
| The Host connector: logging in to a Host through its Relay, the pinned host key and connect retries | [host-connector.md](host-connector.md) |
| Enroll: `koko host enroll`, its inputs, the host key it pins, the states, and the one line it adds to `authorized_keys` | [enroll.md](enroll.md) |
| The resumable stream: the byte stream beneath a Tunnel, its frames, offset exchange and replay buffer | [resumable-stream.md](resumable-stream.md) |
| The Gateway, the Tunnel, `koko exec` and `koko tunnel`: the `tunnel@koko` channel, its open payload and Tunnel id, who may open an SSH Session and the refusals, the relay to the Host and its forwards, the web terminal's SSH Session, Detach, the grace and Resume, the log lines, `koko exec`, `koko tunnel`, ssh's ProxyCommand, and the device end of a Tunnel | [gateway.md](gateway.md) |
| Initialisation: the Owner, the koko server's own keys and the Server Address, what answers before it, and `server show` and `server set` after | [initialisation.md](initialisation.md) |
| The installer: `koko server install`, the upgrade, `koko server uninstall`, the machine interface, the unit, the checks before any write, and the fake machine | [installer.md](installer.md) |
| The release, `install.sh` and `install.ps1`: the assets and their names, `make dist`, the release workflow, what `install.sh` checks, runs and installs, what `install.ps1` does on Windows, the check workflow's jobs and their expected time, the real-systemd check, the container check, the skew check and the `windows` CI job | [release.md](release.md) |
| Join Codes: the `koko_` string, issuing them, device names, and the hash kept of each | [join-codes.md](join-codes.md) |
| `koko web` and the web front: the listener, the `web@koko` channel, the Host guard, the token and the cookie, the web listener behind the Owner's reverse proxy, its Host and Origin checks and the headers every answer carries, the web terminal's WebSocket and its first message, the port-owner lookup | [web-front.md](web-front.md) |
| The Device Connection and joining: the TCP listener and its probes, device authentication, `koko join`, the device's files, koko's ssh_config and its bare names, known_hosts and the Include line, which koko server the CLI reaches, `koko connect`, last used, revoking a Device Session, the access-change signal and the revocation record | [device-connection.md](device-connection.md) |
| The Device Mux: which way a command goes and the direct path, starting it, the lock, its socket and runtime directory, the local protocol, liveness and reconnecting, the bare-name refresh, its state, the idle exit, the revocation disconnect and `koko disconnect` | [device-mux.md](device-mux.md) |
| The access check and Spaces: who is calling, the rule, unseen equals unknown, memberships, what a Space holds, the access matrix | [access.md](access.md) |
| Users: the record, `user ls`, and `user remove` deleting a User who never used koko or Removing anyone else | [users.md](users.md) |
| Passwords, Password Links and Sign-ins: the hash, issuing and spending a link, the Sign-in cookie and its lifetime, the `/auth/` endpoints, the rate limits, what a Sign-in may do, the web UI's Sign in and Set a Password pages and its Password and Sign-ins parts of Account and a person's panel, the log lines | [signin.md](signin.md) |
| The record: a Host's fields and their rules, a Service and its ports, Attributes, the Preset Attributes and the four types, the Note, the host and service operations and `attr.list`, filtering and sorting, relative dates, the one leak | [record.md](record.md) |
| The record's pages: the Hosts page and its Services view, the view in the URL, the filter, relative dates, sorting and columns, Add Host, a Host's page and its actions, edit forms, and a Service's panel | [record-pages.md](record-pages.md) |
| Secrets: the record and its name rule, the commands and the one that prints a value, values in JSON, access, sealing, and the web UI's Secrets page | [secrets.md](secrets.md) |

## Maintaining a reference

Update the owning reference in the same change when that change alters a
documented responsibility, interface, state transition, runtime path,
compatibility rule or failure behaviour. A refactor that keeps these
contracts needs no prose change.

Keep each fact in one reference and link to it from elsewhere. Link source
files and named symbols rather than copying implementation detail that
changes often.
