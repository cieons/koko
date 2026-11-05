# `koko web` and the web front

How a browser reaches the web UI: `koko web` on the person's machine, the
`web@koko` channel, and the web front on the koko server, which answers that
channel before the API does. The design is
[ADR 0022](../adr/0022-the-web-ui-cookie-lasts-until-its-device-session-is-revoked.md);
the web UI it serves is built as [the web UI build](web-ui.md) says.

Today `koko web` runs over the Unix socket, as the Local caller. A joined
device's `koko web` (#62) binds its cookie to its Device Session instead of
the run, and lasts until that Device Session is Revoked.

## `koko web`

[`internal/client/cli/web.go`](../../internal/client/cli/web.go), in the
irreversible tier: it prints a credential, its token.

1. **The port.** `--port` (default 5656) takes a TCP port from 1 to 65535,
   else the command line is refused with exit 2. It listens on `127.0.0.1`
   only, before it calls the koko server. When the port is taken it exits 1
   with `port_taken`, naming the process holding it (from
   [`portowner`](#the-port-owner-lookup)) and `--port`, and never moves to
   another port, which would break bookmarks. A browser may try `localhost`
   on `[::1]` first, so the port is refused as taken when another process
   listens on `[::1]:<port>` too, though `koko web` does not listen there.
2. **The token.** It calls `web.start` and gets the run's one-time token.
3. **The address.** It prints `http://localhost:<port>/?t=<token>` as soon
   as it serves; `--json` prints `{"address": "..."}`, once, at that moment
   ([`longAction`](../../internal/client/cli/tree.go)). Showing the token is
   the command's purpose; nothing else prints it.
4. **Opening.** Unless `--no-open`, it runs the OS's opener on the address,
   found on the invocation's `PATH` (`xdg-open` on Linux, `open` on macOS;
   [`browser_<os>.go`](../../internal/client/cli/browser.go)), and serves at
   once. It never stops the opener, which may be the browser itself, and
   waits for it to exit before it exits itself. When the opener is missing or
   fails, it says so on stderr and serves on.
5. **Forwarding.** Each browser connection becomes one `web@koko` channel,
   carried both ways until either side closes it.
6. **The end.** It runs until interrupted (exit 0). When the connection to
   the koko server ends, the run and its cookie end with it, so `koko web`
   exits 1 with `server_unreachable`, saying to run it again. Either way it
   closes every browser connection before it exits.

## The `web@koko` channel

A channel type of its own, so that the cookie is required there and never on
`api@koko` (P0 spec: reach). Its open payload is the port `koko web` listens
on, an SSH `uint32` ([`wire.WebPayload`](../../internal/shared/wire/wire.go)).
The transport refuses a `web@koko` channel whose payload is not exactly a
port from 1 to 65535, and hands every other one to the web front with the
caller, the port and the SSH connection it came on
([`transport.WebPortFrom`, `transport.ConnectionFrom`](../../internal/server/transport/transport.go)).
The connection's context ends when the connection closes.

## The web front

[`internal/server/web`](../../internal/server/web/web.go) answers each
request on a `web@koko` channel in this order:

1. **The Host header** must be `localhost:<port>` or `127.0.0.1:<port>`, with
   the channel's port (for port 80 browsers leave `:80` out, and so may the
   header). Any other Host gets no answer: the connection is closed without
   a response. This defeats DNS rebinding.
2. **The token.** A `GET` of a page (any path outside `/v1/`) carrying
   `?t=<token>` from a browser without the cookie trades it: the browser gets
   the cookie and a `303` to the same page without `t`, its other query
   parameters kept. A browser holding the cookie already only gets the
   redirect. The trade is refused with a page saying why: `403` when the
   token was spent already (another browser, perhaps another OS user, opened
   the address first), `401` when it expired or belongs to another run.
3. **The cookie** is checked on every other request. Without a valid one, a
   page gets `401` saying to open the address `koko web` printed, and an API
   call gets `401` with `cookie_required`, without reaching the API.
4. **`/v1/`** goes to the API's handler, which takes only a JSON `POST`
   ([the API](api.md#calls)); everything else serves the embedded web UI
   ([`webui.FS`](../../internal/server/webui/webui.go)), and its `index.html`
   for a path that names no file, since the web UI routes its own pages. A
   build without the web UI's output serves the placeholder page there.

No answer carries a CORS header, so another site's page fails its preflight.
Refusals and the trade's redirect carry `Cache-Control: no-store`. The
token, the cookie and the request's query are in no log line or answer; the
log says `web cookie issued` and `web token refused` with the reason, at
info level.

| Code | Status | When |
| --- | --- | --- |
| `cookie_required` | 401 | An API call on `web@koko` came without a valid cookie of this run of `koko web`. |

## Runs, tokens and the cookie

- **`web.start`** ([`api.WebStart`](../../internal/shared/api/web.go)), a read
  that answers only the Local caller (`op.LocalOnly`) until a Device Session
  can run `koko web` (#62), starts a run on the caller's SSH connection, if
  none runs there yet, and issues a token for it: `{"token": "..."}`, 128
  random bits as 26 base32 characters (`crypto/rand.Text`).
- **A run is its connection.** The runs and their tokens live in the koko
  server's memory ([`run.go`](../../internal/server/web/run.go)), never in
  the database. A token is traded only on a `web@koko` channel of the
  connection that started its run, once, within `web.TokenLifetime` (5
  minutes) of its issue, both read on the koko server's clock (the clock
  hook); it is kept by its SHA-256 until then, so that a spent one is told
  apart from an unknown one. When the connection closes,
  the run ends with its tokens, and its cookie stops working: over the Unix
  socket the cookie lasts only for that run of `koko web` (ADR 0022).
- **The cookie** is named `koko_<port>`, since browsers send a `localhost`
  cookie to every port; `HttpOnly`, `SameSite=Strict`, `Path=/`, with no
  expiry. Its value, in base64url without padding, is a version byte (1), the
  run's 16-byte random id, and an HMAC-SHA256 over both (after the label
  `koko web cookie\0`) under the cookie key. It is valid only with a run of
  that id on the request's connection.
- **The cookie key** is 32 random bytes, made on first use, the first time
  the web front checks a token or a cookie, and stored in `own_keys` (kind
  `cookie`) sealed under the Root Key for its own row's id
  ([`deployment.CookieKey`](../../internal/server/deployment/cookie.go),
  [the Seal](seal.md)). The web front loads it once per process.

## The port-owner lookup

[`portowner.Lookup`](../../internal/shared/portowner/portowner.go), in the
shared group so that `koko server install` (#38) reuses it, names the process
listening on a TCP port on any local address, IPv4 or IPv6: its pid, command
and user. It is per OS ([rule 13](code-layout.md#rules-for-working-in-parallel)):

- **Linux** reads `/proc/net/tcp` and `tcp6` for the listening socket, then
  the `/proc/<pid>/fd` links this user may read. Another user's process is
  named by its user alone.
- **macOS** runs `/usr/sbin/lsof`, which sees only this user's processes
  unless run as root.
- **Windows** returns a "not supported" error.

`ErrNotFound` means no process this user can see holds the port.

## Verifying

- [`web_test.go`](../../internal/client/cli/web_test.go), S1 against
  `koko web` running in the background (`h.Background`): the printed
  address (golden `web.json`) and the trade; refusals without a token or a
  cookie, without reaching the API; a token working once, with the reason
  given to the second browser; a token working only through its own run; the
  Host guard; only a JSON `POST` reaching the API and no CORS header; the
  cookie ending with the run; a token's 5 minutes (`h.Advance`); the taken
  port, on `127.0.0.1` and on `[::1]`, and `--port`'s range; the opener with
  and without `--no-open`, and a missing one; `koko web` ending when the koko
  server stops; the token and the cookie absent from stderr, the log and
  every answer; and, in a nested `go test` whose overlay hides the web UI's
  build output, the placeholder page served over `web@koko`.
- [`internal/server/web/web_test.go`](../../internal/server/web/web_test.go):
  the cookie key made on first use, sealed under the Root Key, and in no file
  of the data directory; [`host_test.go`](../../internal/server/web/host_test.go):
  the Host header's forms, port 80's included.
- [`transport_test.go`](../../internal/server/transport/transport_test.go):
  `web@koko` reaching the web front with the port, the caller and the
  connection, and its payload checked.
- [`portowner_test.go`](../../internal/shared/portowner/portowner_test.go):
  the lookup names this test's own process on IPv4 and IPv6, and finds none
  on a free port, on Linux and macOS.
