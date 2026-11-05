# The API is HTTP and JSON inside SSH channels

The koko server's API is HTTP/1.1 with JSON bodies, one `POST
/v1/<noun>.<verb>` per operation, spoken inside SSH channels. Every way in is
an SSH connection carrying the same channels: a Device Connection over TCP,
authenticated by the device's key, and a connection over the Unix socket with
`none` authentication, which acts as the Owner (ADR 0008). The CLI makes its
calls in API channels, `koko web` forwards each browser connection into a
channel of its own, and a Tunnel (`tunnel@koko`) carries an SSH Session to the
Gateway. One HTTP handler serves the CLI and the web UI, and the outer SSH
gives it encryption and the caller's identity. The request and response types
are Go types in one internal package, used by the server and by the CLI; the
web UI's TypeScript types are generated from them.

## Considered Options

- **argv forwarded as an SSH `exec` request to the koko server's own command
  tree**: commands could never be newer than the server, but the web UI would
  need a second API, and the CLI's own work (relative dates in the caller's
  time zone, Hosts read as JSON on stdin) would move to the server.
- **JSON-RPC on one channel**: `fetch` cannot speak it, so `koko web` would
  translate or a second endpoint would exist.
- **Resource-style REST**: typed Attribute filters do not fit a query string,
  and commands would no longer map one to one onto operations.
- **protobuf or Connect**: both clients ship from this repository and the
  koko server serves the web UI itself, so a cross-language schema buys
  nothing.
- **Plain HTTP on the Unix socket**: a Tunnel there would need a second
  framing, an HTTP Upgrade.

## Consequences

- The API never streams. Whatever streams is an SSH Session through a Tunnel,
  as `koko exec` is, or a forward, as `koko web` is. So `koko exec` gets the
  Gateway's access checks, logging and revocation without a second path.
- The API never serves an unauthenticated caller. A device joins inside SSH
  authentication: its new key earns a partial success, and the Join Code's
  secret completes it.
- Only the CLI can run a different version from the koko server, because the
  server serves the web UI. Within `/v1` the server only adds operations and
  optional fields, and refuses request fields it does not know. It reports its
  version and its operations when the CLI connects, so a CLI that needs a
  missing operation says the koko server is older. A breaking change is a
  `/v2` served beside `/v1`.
- OpenSSH's own `ssh -W` cannot open a Tunnel: the server refuses
  `direct-tcpip`.
