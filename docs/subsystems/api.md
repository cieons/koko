# The API

How a call reaches the koko server and how the server answers it. The design
is [ADR 0015](../adr/0015-the-api-is-http-and-json-in-ssh-channels.md); the
types are in [`internal/shared/api`](../../internal/shared/api), and the web
UI's TypeScript types are generated from them
([web-ui.md](web-ui.md#generated-types)).

## The way in

- **The Unix socket** sits at the fixed path
  [`wire.SocketPath`](../../internal/shared/wire/wire.go), `/run/koko/koko.sock`,
  mode 0666. It speaks SSH with `none` authentication, a host key made in
  memory at each start, and the version string `SSH-2.0-koko`. Every
  connection on it is the Local caller, acting as the Owner
  ([ADR 0008](../adr/0008-local-callers-act-as-the-owner.md)). Source:
  [`transport.ListenSocket`](../../internal/server/transport/transport.go).
- **Channels.** An `api@koko` channel carries HTTP/1.1 to the API. A
  `web@koko` channel carries one browser connection of `koko web` to the web
  front, which asks for its cookie before it hands a call to the API
  ([`koko web` and the web front](web-front.md)). Any other channel type is
  refused with `unknown channel type`. The transport attaches the
  connection's caller, and the connection itself, to every request
  (`op.WithCaller`, `transport.ConnectionFrom`); the API refuses a request
  without a caller.
- **One channel, many calls.** A channel carries any number of calls in turn
  (HTTP keep-alive). The CLI's [`apiclient`](../../internal/client/apiclient/apiclient.go)
  reuses an idle channel and opens another only for a concurrent call.

## Calls

- Each operation is `POST /v1/<noun>.<verb>`, such as `/v1/space.add`, with a
  JSON object as the body and `Content-Type: application/json`.
- The server refuses a request field it does not know (`unknown_field`, naming
  it), so a newer CLI's option is never silently dropped. Clients ignore
  response fields they do not know.
- A response is always a JSON object: a record is wrapped
  (`{"space": {...}}`), and so is a list (`{"spaces": [...]}`), which holds
  every item and is `[]`, never `null`, when empty.
- An error is an HTTP status and `{"error": {"code": "...", "message": "..."}}`.
  The code is stable snake_case: within `/v1` codes are only added.

| Code | Status | When |
| --- | --- | --- |
| `invalid_request` | 400 | The body is not a JSON object of the operation's request type (malformed, wrong type, duplicate or trailing data). |
| `unknown_field` | 400 | The body holds a field the operation does not know. |
| `invalid_name` | 400 | A name is not a lowercase DNS label ([`name.Check`](../../internal/shared/name/name.go)). |
| `local_only` | 403 | The operation answers only the Local caller, over the Unix socket. |
| `unknown_operation` | 404 | No operation is served at the path. |
| `method_not_allowed` | 405 | The method is not `POST`. |
| `name_taken` | 409 | A record already holds the name where names must differ: a Space or a User for a new User's name, which they share; a Space for a Space's; one of the same User's devices or valid Join Codes for a device's. |
| `not_initialised` | 409 | The koko server is not initialised, so it answers no operation but `server.init` ([initialisation](initialisation.md#before-initialisation)). |
| `unsupported_media_type` | 415 | The body is not `application/json`. |
| `internal_error` | 500 | Anything the server did not expect; the message says to read the log, which holds the cause. |

An area adds its codes beside its types (see
[code layout](code-layout.md#where-an-area-adds-its-parts)). An operation
returns `code.Errorf(...)`; a code that is not registered reaches the caller as
`internal_error`, and the log names it.

## The operation framework

[`op.API`](../../internal/server/op/op.go) is the API's HTTP handler. An area
registers each operation with `a.Handle(api.SpaceAdd, fn)`, where `fn` gets the
request decoded into its Go type, the caller and a `store.Tx`. The framework:

1. refuses a call without a caller, an unknown path, another method, another
   content type, and a body that does not decode;
2. refuses every caller but the Local caller, with `local_only`, for an
   operation registered with `op.LocalOnly`;
3. runs `fn` inside one transaction it opens itself, `IMMEDIATE` for a write
   (`api.Op.Write`) and read-only for a read, after asking, in that
   transaction, whether the koko server is initialised: until it is, an
   operation not registered with `op.BeforeInit` is refused with
   `not_initialised`;
4. commits only when `fn` returns no error, so a refused write leaves nothing
   behind, and writes the response or the error object.

An operation reads the time from `op.Now(ctx)`: the koko server's clock
(`server.Config.Clock`, the clock hook) read once when the call began, in UTC
to the second as every stored time is, so the times one call records agree
and a test moves them all.

An operation never begins, commits or rolls back. Each call is logged at debug
level as `api call` with its path and status; an unexpected error is logged at
error level. Who may call an operation is decided by the access check (#39);
until then the only caller is the Local caller, who acts as the Owner.

Closing the transport stops new connections and channels, waits up to
`shutdownGrace` (5 s) for calls in flight, then ends every connection, so the
store is never closed under a running transaction.

## Verifying

- [`internal/server/op/op_test.go`](../../internal/server/op/op_test.go): the
  refusals, the error object, the list wrapper, rollback of a failed write,
  the refusals before initialisation and of a caller who is not the Local
  caller.
- [`internal/server/transport/transport_test.go`](../../internal/server/transport/transport_test.go):
  SSH on the socket, the version string, the Local caller, refused channel
  types, a new host key at each start, mode 0666.
