# The koko server reports its version in an SSH global request

Once a connection to the koko server has authenticated, and before the koko
server serves any channel on it, the koko server sends one SSH global
request, `report@koko`, that wants no reply. Its payload is a JSON object:
the koko server's version and the name of every operation it serves. The
CLI's connection keeps it, and before each call the CLI checks that the
operation is listed; when it is not, the command fails with
`server_too_old`, naming the koko server's version and the command, and
sends nothing. SSH delivers a connection's messages in order, so the report
reaches the CLI before the answer to its first channel open: it costs no
round trip, as ADR 0015 asks.

## Considered Options

- **A call of its own, such as `server.report`**: plain HTTP, but one more
  round trip on every command.
- **An HTTP header on every response**: no SSH message, but the CLI learns
  the operations only after its first call, and every response, the web
  UI's included, carries the whole list.
- **`unknown_operation` read as `server_too_old`**, with the version in a
  header: no report at all, but every missing operation costs a call, and
  nothing lists what a koko server serves for the access matrix or for
  `koko status`.
- **The SSH banner**: it goes out before authentication, to callers the koko
  server does not know yet, and the TCP listener already uses it for its
  refusals.
- **A first message in each `api@koko` channel**: one per channel, and the
  channel would no longer carry plain HTTP/1.1.

## Consequences

- Every koko server sends `report@koko` on every connection, over the Unix
  socket and TCP alike, for as long as a CLI that waits for it is in use. A
  later koko server may add fields to the payload, and CLIs ignore fields
  they do not know; it never stops sending the request.
- A CLI waits for the report before its first call, so a connection that
  never sends one carries no call.
- The report lists the operations registered with the API's handler, so it
  says what this koko server serves, not what its build declares; a test
  fails when an operation is declared and not registered.
- The report belongs to the Device Connection, which the Device Mux holds
  (ADR 0017), so a command that reaches the koko server through the Device
  Mux needs the report passed on to it.
