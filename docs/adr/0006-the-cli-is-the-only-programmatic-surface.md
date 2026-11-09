---
status: its web UI only through `koko web`, refined by ADR-0042
---

# The CLI is the only programmatic surface

Scripts and agents drive koko through one CLI, which calls the same API over a
Unix socket on the koko server and over a Device Connection from anywhere else
(ADR 0012). The binary prints its own agent skill (`koko skill`), generated
from the command tree and versioned with the release, so a new feature reaches
agents the moment the binary ships. The web UI is a second client of the same
API, for people, and is reached only through `koko web` (ADR 0012).

## Considered Options

- **An MCP server**: a second programmatic surface to keep in step with the
  CLI, for agents that already run shell commands well.

## Consequences

Command names carry their risk (`koko host ls` versus `koko exec`), because
agent harnesses allow or ask by command prefix (ADR 0019). Output can be
`--json`, a command never prompts without a terminal (ADR 0019), and output
leaves Secrets out unless a command exists to show one. Anything the web UI
can do goes through the API, so the CLI can do it too.
