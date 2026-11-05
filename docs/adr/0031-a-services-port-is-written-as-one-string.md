# A Service's port is written as one string, `<port>/<protocol>`

A Service's ports are a set of strings such as `443/tcp` and `51820/udp`, in
one form everywhere: `ports=` on the command line, the CLI's `--json`, the
API and the web UI. What koko prints always names the protocol, though
`ports=443` takes `tcp` for one left out, and a set comes back ordered by
port and then protocol, so two equal sets print alike. A Service that listens
on nothing has `[]`.

## Considered Options

- **An object per port, `{"port": 443, "protocol": "tcp"}`**: a script reads
  the number without splitting, and a later member, such as a name for the
  port, fits beside the two. But one port would then have two forms, the one
  people type and the one scripts read, every client would convert between
  them, and the spec, the web UI's default column and every example write
  `443/tcp`.
- **A bare number, tcp implied**: shortest, but it cannot say udp, which a
  WireGuard Service's `51820/udp` needs.

## Consequences

- `koko service show web1/nginx --json` prints the ports as `ports=` takes
  them, so recording a Service again on another Host copies strings.
- A script that wants the number splits at the `/`; one that asks whether a
  Service listens on 443 looks for `"443/tcp"` whole.
- The ports are no key to filter or sort `koko service ls` on: a set has no
  comparison. A release that adds one, or anything more per port, adds a
  member and never retypes `ports` (ADR 0016).
