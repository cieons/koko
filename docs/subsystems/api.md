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
- **The TCP listener**, once the koko server is initialised, speaks the same
  SSH with the sealed host key, and every connection on it is a Device
  Connection, authenticated by its device's key as a Device Session, or
  joining with a Join Code's secret
  ([the Device Connection](device-connection.md)).
- **Channels.** On either way in, an `api@koko` channel carries HTTP/1.1 to
  the API. A `web@koko` channel carries one browser connection of `koko web`
  to the web front, which asks for its cookie before it hands a call to the
  API ([`koko web` and the web front](web-front.md)). A `tunnel@koko`
  channel carries one SSH Session to the Gateway, which the transport hands
  it with the connection ([the Gateway](gateway.md)). Any other channel type
  is refused with `unknown channel type`. The transport attaches the
  connection's caller, and the connection itself, to every request
  (`op.WithCaller`, `transport.ConnectionFrom`); the API refuses a request
  without a caller.
- **The version and operations report.** Once a connection has
  authenticated, before any channel is served, the transport sends it the
  SSH global request [`wire.RequestReport`](../../internal/shared/wire/wire.go),
  `report@koko`, wanting no reply, whose payload is `wire.Report` in JSON:
  `{"version", "operations"}`, the build's version and the name of every
  operation registered with the API (`op.API.Names`), the same before
  and after initialisation. It reaches the client before the answer to its
  first channel open, so it costs no round trip
  ([ADR 0027](../adr/0027-the-koko-server-reports-its-version-in-an-ssh-global-request.md)).
  The client's connection keeps it (`serverconn.Conn.Report`).
- **One channel, many calls.** A channel carries any number of calls in turn
  (HTTP keep-alive). The CLI's [`apiclient`](../../internal/client/apiclient/apiclient.go)
  reuses an idle channel and opens another only for a concurrent call.
  Both ends run HTTP over a channel through
  [`wire.Conn`](../../internal/shared/wire/wire.go), whose Write goes to the
  channel itself, so a call counts as answered only once its answer has gone
  to the SSH connection, and closing the connection then (a Revoked Device
  Session's, or at shutdown) does not cut it.

## The API client

[`apiclient.Client`](../../internal/client/apiclient/apiclient.go) is how the
CLI and the S1 harness call an operation. It dials its connection on the
first call, through the dial it was given, and for each call:

- sends nothing for an operation the report does not list, failing with
  `apiclient.MissingOperationError`, which the CLI reports as
  `server_too_old`, naming the koko server's version and the command
  ([the CLI](cli.md#the-runner-and-the-output-layer)); within `/v1` a koko
  server only adds operations, so an older CLI always works;
- gives a write a new `Idempotency-Key` (`api.NewIdempotencyKey`);
- when the connection drops the call (no channel opened, or no answer
  came), closes that connection, dials a new one and sends the call once
  more, a write with the same key, so the write applies once; when that one
  drops too, the call fails wrapping `apiclient.ErrUnreachable`.

## Calls

- Each operation is `POST /v1/<noun>.<verb>`, such as `/v1/space.add`, with a
  JSON object as the body and `Content-Type: application/json`. The body is
  at most `api.MaxRequestBody`, 16 MiB: a batch `host add` of hundreds of
  Hosts with 64 KiB Notes fits, and one call holds no more in memory.
- Every write carries the header `Idempotency-Key`
  (`api.IdempotencyKeyHeader`): 128 random bits in 32 lowercase hex
  characters, new for each write and the same on its retry. A write without
  one, or with another form, is refused with `invalid_request`; a read's is
  ignored. See [Idempotency-Key](#idempotency-key).
- The server refuses a request field it does not know (`unknown_field`, naming
  it), so a newer CLI's option is never silently dropped. Clients ignore
  response fields they do not know.
- A response is always a JSON object: a record is wrapped
  (`{"space": {...}}`), and so is a list (`{"spaces": [...]}`), which holds
  every item and is `[]`, never `null`, when empty.
- An error is an HTTP status and `{"error": {"code": "...", "message": "..."}}`.
  The code is stable snake_case: within `/v1` codes are only added. The
  refusal of a batch of several items, so far a `host.add` of several Hosts
  ([a refused batch](record.md#a-refused-batch)), also holds `items`
  ([`api.ItemError`](../../internal/shared/api/errors.go), [ADR
  0030](../adr/0030-a-refused-batch-lists-each-item-at-fault.md)): each item
  at fault as `{"index", "code", "message"}`, its place in the batch counted
  from 0 and the code and message it would be refused with alone, in the
  batch's order. Every other error leaves `items` out.

| Code | Status | When |
| --- | --- | --- |
| `invalid_request` | 400 | The body is not a JSON object of the operation's request type (malformed, wrong type, duplicate or trailing data), or a write carries no valid `Idempotency-Key`; or a list's filter term or sort key is malformed or names a key the list does not take ([filtering and sorting](record.md#filtering-and-sorting)). |
| `unknown_field` | 400 | The body holds a field the operation does not know. |
| `invalid_name` | 400 | A name breaks its rule: a lowercase DNS label ([`name.Check`](../../internal/shared/name/name.go)), or for a Secret, `name.CheckSecret`'s. |
| `invalid_value` | 400 | A Host's field, an Attribute or the Note, or a value given for one, breaks its rule ([the record](record.md#a-host)): a Preset's value that does not parse as its type, or comes in another JSON type, among them, and a filter term's value not of its key's type. |
| `invalid_batch` | 400 | The Hosts at fault in a `host.add` of several are at fault for different reasons; `items` gives each its own code ([a refused batch](record.md#a-refused-batch)). Hosts all at fault for one reason are refused with that reason's code. |
| `revoked` | 401 | The call came on a Device Session that is Revoked ([the Device Connection](device-connection.md#who-is-calling)). |
| `local_only` | 403 | The operation answers only the Local caller, over the Unix socket; `server.set` not even a browser tab of `koko web` there ([initialisation](initialisation.md#the-deployment-afterwards)). |
| `owner_only` | 403 | Only the Owner may do what the call asks, such as changing Spaces and their Members, inviting a User, or issuing a Join Code for another User's device; it is answered before the call reads anything it names ([the access check](access.md#what-a-caller-cannot-see)). |
| `unknown_device` | 404 | No live Device Session the caller may revoke has the name: another User's, for a Member, is answered the same. |
| `unknown_host` | 404 | No Host the caller can see has the name or id given: a Host in a Space they cannot act in is answered the same ([the record](record.md#operations)). |
| `last_device_session` | 409 | The call would revoke the Owner's last live Device Session without `last` (`--last`). |
| `unknown_operation` | 404 | No operation is served at the path. |
| `method_not_allowed` | 405 | The method is not `POST`. |
| `name_taken` | 409 | A record already holds the name where names must differ: a Space or a User for a new User's name, which they share; a Space for a Space's; one of the same User's devices or valid Join Codes for a device's; a Host that is not Retired, in any Space, for a Host's, with the same message whichever Space holds it; a Secret of the same Space for a Secret's. |
| `not_initialised` | 409 | The koko server is not initialised, so it answers no operation but `server.init` ([initialisation](initialisation.md#before-initialisation)). |
| `request_too_large` | 413 | The body is over 16 MiB. |
| `unsupported_media_type` | 415 | The body is not `application/json`. |
| `idempotency_key_reused` | 422 | The caller's `Idempotency-Key` came with another request (another operation or body) within the last 10 minutes. |
| `unexpected_host_key` | 409 | Enroll met a host key other than the pinned one, or the one `host_key` names, and did not log in; the message holds both fingerprints ([Enroll](enroll.md)). |
| `login_refused` | 403 | The Host refused the password or private key given to Enroll. |
| `host_unreachable` | 502 | Enroll could not connect to the Host, after retrying network errors. |
| `key_not_added` | 502 | Enroll logged in but could not append the koko key to the Host Account's `authorized_keys`. |
| `already_managed` | 409 | Enroll of a Managed Host without `host_key` or `replace_host_key`. |
| `host_changed` | 409 | The Host's Host Account, state or pin changed while Enroll logged in to it, so it recorded nothing. |
| `host_was_managed` | 409 | `host.remove` of a Host that has been Managed, as its pin shows ([the record](record.md#operations)). |
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
   content type, a body over the limit, and a body that does not decode;
2. refuses every caller but the Local caller, with `local_only`, for an
   operation registered with `op.LocalOnly`;
3. refuses a write without a valid `Idempotency-Key`, and waits while a
   write with the same caller and key runs;
4. runs `fn` inside one transaction it opens itself, `IMMEDIATE` for a write
   (`api.Op.Write`) and read-only for a read, after asking, in that
   transaction, whether the koko server is initialised: until it is, an
   operation not registered with `op.BeforeInit` is refused with
   `not_initialised`. For a write it first looks up the key, and answers
   with the stored response without running `fn`
   ([Idempotency-Key](#idempotency-key));
5. encodes the response and, for a write, stores it, in the same
   transaction, and commits only when all of it succeeds, so a refused write
   leaves nothing behind and a response that cannot be stored rolls its
   write back; then writes the response or the error object.

A write runs on a context its caller's drop does not cancel
(`context.WithoutCancel`): it runs to its end and stores its response for the
caller's retry.

**A prepared write.** A write whose work takes long, such as logging in to a
Host (`host.enroll`), registers with `a.HandlePrepared(o, prepare, fn)`. Its
`prepare` runs after the wait for a retry's first attempt and before the
write's transaction, outside any, reading through `op.Read`, which runs each
read in a read-only transaction of its own: so it never holds SQLite's write
lock, which every other write would wait for and give up on after 5 s. A
read-only transaction first checks initialisation and looks up the stored
response, so a retry answered with it never prepares again. Then `fn` runs in
the write's transaction as any operation does, given what `prepare` returned,
and checks again what it relies on, since the record may have changed
meanwhile. A `prepare` that fails ends the write, which applies and stores
nothing; a caller's drop cancels neither part.

An operation reads the time from `op.Now(ctx)`: the koko server's clock
(`server.Config.Clock`, the clock hook) read once when the call began, in UTC
to the second as every stored time is, so the times one call records agree
and a test moves them all.

An operation never begins, commits or rolls back. What must outlive the call,
such as the TCP listener initialisation starts, waits for the transaction's
end through `op.AfterTx`, which is told whether it committed. Each call is
logged at debug level as `api call` with its path and status; an unexpected
error is logged at error level.

The caller (`op.Caller`) is the Local caller or a Device Connection's Device
Session. Who it acts as comes from the current records on every call
([`access.Who`](../../internal/server/access/access.go)): a Device Session's
User, or the Owner for the Local caller; `user.current` answers it. A
Revoked Device Session is refused with `revoked`. What each caller may do,
and in which Spaces, is the access check's
([the access check and Spaces](access.md)), which each operation asks inside
its transaction; the access matrix states every operation's answer to each
kind of caller.

An operation that withdraws access announces it on the access-change signal
([`access.Signal`](../../internal/server/access/signal.go)) with
`Publish`, which reaches its subscribers only once the call's transaction
has committed ([revoking](device-connection.md#revoking)).

Closing the transport stops new connections and channels, waits up to
`shutdownGrace` (5 s) for calls in flight, then ends every connection, so the
store is never closed under a running transaction.

## Idempotency-Key

The store is [`op/idempotency.go`](../../internal/server/op/idempotency.go),
over the table `idempotency_keys`; the design is P0 spec: reach and
[ADR 0021](../adr/0021-an-ssh-session-survives-a-network-drop-by-resuming-its-tunnel.md).

- **Per caller.** A key belongs to its caller: a Device Session, or the
  Local caller. Another caller's write with the same key is a write of its
  own.
- **What is kept.** For each write that succeeded, in its own transaction:
  the caller, the key, when it expires, and, sealed under the Root Key for
  the caller and the key ([the Seal](seal.md)), the SHA-256 of the
  operation's name and the request body, then the response. A response may
  hold a Join Code (`device.add`, `user.invite`, `server.init`), which the
  database otherwise never holds; the digest keeps no request body, which
  may hold a Secret's value.
- **A retry** with the same key and the same request (operation and body,
  byte for byte) gets the stored response, logged as `api call` with
  `replayed=true`, and the write does not run again. The same key with
  another request is refused with `idempotency_key_reused`. A replay
  answers the caller's own earlier write as it was answered, before the
  operation's access check: it reveals nothing the caller was not told, and
  a Revoked Device Session never gets one, since its connections close and
  no new one authenticates.
- **A failed write** applies nothing and stores nothing, so its retry runs
  again.
- **A retry during the first attempt** waits for it, in memory and before
  its own transaction begins (`op.inflight`, logged as `api call waits for
  its first attempt`), then gets its response, or runs when the first
  attempt failed. The wait does not rest on SQLite's lock, which gives up
  after 5 s, and a waiting retry holds no lock.
- **10 minutes.** A response is kept for 10 minutes on the koko server's
  clock (`op.Now`); each write first deletes the expired ones, so the key is
  free again. Stored in the write's own transaction, a response outlives a
  restart.

## Verifying

- [`internal/server/op/op_test.go`](../../internal/server/op/op_test.go): the
  refusals, the body limit, the error object, the list wrapper, rollback of a
  failed write, the refusals before initialisation and of a caller who is
  not the Local caller.
- [`internal/server/op/idempotency_test.go`](../../internal/server/op/idempotency_test.go):
  a write without a valid key refused; a replay; the same key with another
  request refused; keys per caller; 10 minutes on the clock hook; a retry
  waiting for the first attempt; a failed write storing nothing; a write and
  its stored response committing together (a store that fails, a response
  that cannot be encoded); a stored response outliving a restart; a write
  not cancelled when its caller drops.
- [`internal/server/op/prepare_test.go`](../../internal/server/op/prepare_test.go):
  a prepared write holding no lock while it prepares, a retry answered
  without preparing again, a failed `prepare` storing nothing, a `prepare`
  not cancelled when its caller drops, and none before initialisation.
- [`internal/server/transport/transport_test.go`](../../internal/server/transport/transport_test.go):
  SSH on the socket, the version string, the report sent unasked before any
  channel, the Local caller, refused channel types, a new host key at each
  start, mode 0666.
- [`internal/server/report_test.go`](../../internal/server/report_test.go):
  the koko server reports every operation and its build's version.
- [`internal/client/cli/skew_test.go`](../../internal/client/cli/skew_test.go):
  `server_too_old` from a koko server that lacks the command's operation,
  with nothing sent, and an older CLI working against a newer koko server.
- [`internal/client/cli/retry_test.go`](../../internal/client/cli/retry_test.go):
  a write and a read cut mid-flight on a direct Device Connection retried
  once on a new one, the write applied once.
- [`internal/client/cli/device_test.go`](../../internal/client/cli/device_test.go)
  `TestNoJoinCodeIsStoredOrLogged`: no Join Code in the database, the
  responses kept for its writes' keys included.
