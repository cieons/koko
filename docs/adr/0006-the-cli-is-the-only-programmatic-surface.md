# The CLI is the only programmatic surface

Scripts and agents drive koko through one CLI, which calls the same API over a
Unix socket on the koko server and over HTTPS from anywhere else. The binary
prints its own agent skill (`koko skill`), generated from the command tree and
versioned with the release, so a new feature reaches agents the moment the
binary ships. The web UI is a second client of the same API, for people:
recording Hosts, accepting an Invite, confirming a device sign-in.

## Considered Options

- **An MCP server**: a second programmatic surface to keep in step with the
  CLI, for agents that already run shell commands well.

## Consequences

Command names carry their risk (`koko host ls` versus `koko exec`), because
agent harnesses allow or ask by command prefix. Output can be `--json`, never
prompts, and leaves Secrets out unless a command exists to show one. Anything
the web UI can do goes through the API, so the CLI can do it too.
