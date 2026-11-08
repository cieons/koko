# `koko web` and the web front

How a browser reaches the web UI: `koko web` on the person's machine, the
`web@koko` channel, and the web front on the koko server, which answers that
channel before the API does. The design is
[ADR 0022](../adr/0022-the-web-ui-cookie-lasts-until-its-device-session-is-revoked.md);
the web UI it serves is built as [the web UI build](web-ui.md) says.

On a joined device `koko web` goes through the Device Mux like any command
([the Device Mux](device-mux.md)), and its cookie is bound to the device's
Device Session: it lasts until that Device Session is Revoked. Over the Unix
socket, as the Local caller, its cookie is bound to the run and ends with it.

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
2. **The token.** It calls `web.start` and gets a one-time token.
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
   carried both ways until either side closes it. Through the Device Mux, a
   dropped Device Connection ends the channels on it but not `koko web`'s
   connection to the Device Mux, so the browser's next connections open
   new channels once the Device Mux has reconnected, waiting up to
   `timing.CommandWait` for it ([the Device Mux](device-mux.md)). A running
   `koko web` is a command connected to the Device Mux, so it keeps the
   Device Mux from idling out, with or without a browser connection open.
6. **The end** ([`webEnded`](../../internal/client/cli/web.go)). It runs
   until interrupted (exit 0), and it closes every browser connection before
   it exits. Otherwise it ends when its connection to the koko server does:
   - `koko disconnect` stopped the Device Mux: a stop, as Ctrl-C is, so it
     says `koko: koko disconnect stopped koko web.` on stderr and exits 0;
     `koko disconnect` counts it among the commands it ended.
   - The device's Device Session was Revoked: it exits 1 with `revoked`,
     once the device's key and client config are deleted, by the Device Mux
     or, on the direct path, by `koko web` itself
     ([the revocation cleanup](device-connection.md#on-the-device)).
   - Over the Unix socket the run and its cookie end with the connection,
     so it exits 1 with `server_unreachable`, saying to run it again.
   - On a joined device, for any other end, such as a crashed Device Mux or
     a dropped direct Device Connection, it exits 1 with
     `server_unreachable`, saying to run it again: the cookie still works.

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
2. **A Revoked Device Session.** A request on a connection of a Device
   Session Revoked since the koko server started is refused with `revoked`
   (`401`): a page says the Device Session was revoked, and an API call gets
   the error object, which the web UI shows as its revoked notice. Its
   connection stays open for a moment after the revocation commits, while
   its calls in flight are answered ([revoking](device-connection.md#revoking)).
3. **The token.** A `GET` of a page (any path outside `/v1/`) carrying
   `?t=<token>` from a browser without the cookie trades it: the browser gets
   the cookie and a `303` to the same page without `t`, its other query
   parameters kept. A browser holding the cookie already only gets the
   redirect. The trade is refused with a page saying why: `403` when the
   token was spent already (another browser, perhaps another OS user, opened
   the address first), `401` when it expired or was issued for another
   Device Session or another run.
4. **The cookie** is checked on every other request. Without a valid one, a
   page gets `401` saying to open the address `koko web` printed, and an API
   call gets `401` with `cookie_required`, without reaching the API.
5. **`/v1/`** goes to the API's handler, which takes only a JSON `POST`
   ([the API](api.md#calls)); everything else serves the embedded web UI
   ([`webui.FS`](../../internal/server/webui/webui.go)), and its `index.html`
   for a path that names no file, since the web UI routes its own pages. A
   build without the web UI's output serves the placeholder page there.

No answer carries a CORS header, so another site's page fails its preflight.
Refusals, the trade's redirect and every answer of the API carry
`Cache-Control: no-store`, so the browser's cache holds no record, nor the
Secret value `secret.show` answers with. The
token, the cookie and the request's query are in no log line or answer; the
log says `web cookie issued` and `web token refused` with the reason, at
info level.

| Code | Status | When |
| --- | --- | --- |
| `cookie_required` | 401 | An API call on `web@koko` came without a valid cookie of this `koko web`. |

The refusal of a Revoked Device Session is the device area's `revoked`.

## Runs, tokens and the cookie

A token and a cookie are bound to a **subject**
([`run.go`](../../internal/server/web/run.go)), which the request's
connection names (ADR 0022):

- **A Device Session**, for a connection of a joined device. Every Device
  Connection of that Device Session lets in the same cookie, so it outlives
  `koko web`, a dropped Device Connection and a koko server restart, until
  the Device Session is Revoked.
- **A run of `koko web`**, for the Local caller's connection over the Unix
  socket, which has no Device Session. A run is its connection: it starts
  at the connection's first `web.start` and ends when the connection
  closes, with its tokens, and its cookie stops working.

What the web front holds of them lives in the koko server's memory, never
in the database:

- **`web.start`** ([`api.WebStart`](../../internal/shared/api/web.go)), a read
  that answers every caller but a Revoked Device Session (`revoked`), issues
  a token for the caller's subject, starting the run over the Unix socket if
  none runs on the connection yet: `{"token": "..."}`, 128 random bits as 26
  base32 characters (`crypto/rand.Text`).
- **A token** is traded only on a `web@koko` channel of its subject, once,
  within `web.TokenLifetime` (5 minutes) of its issue, both read on the koko
  server's clock (the clock hook); it is kept by its SHA-256 until then, so
  that a spent one is told apart from an unknown one. A Device Session's
  token so outlives the `koko web` that printed it, and the Device
  Connection it was issued on, for the rest of its 5 minutes: another run of
  that device's `koko web` trades it, for the same cookie. A koko server
  restart forgets every token, but never ends a Device Session's cookie.
- **The cookie** is named `koko_<port>`, since browsers send a `localhost`
  cookie to every port; `HttpOnly`, `SameSite=Strict`, `Path=/`, with no
  expiry. Its value, in base64url without padding, is a version byte (2),
  the subject (a kind byte, `d` and the Device Session's id, or `r` and the
  run's 16 random bytes), and an HMAC-SHA256 over both (after the label
  `koko web cookie\0`) under the cookie key. It is valid only on a
  connection whose subject it names.
- **Revocation.** `server.Start` subscribes the web front's `Withdraw` to
  the access-change signal ([revoking](device-connection.md#revoking)). For
  each Device Session a change Revoked, a Removed User's included, it
  forgets the tokens and records the Device Session as Revoked, so its
  cookie lets nothing in from the moment the revocation commits, before its
  Device Connections close. The record lasts as long as the process, since
  a Revoked Device Session never connects again.
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
- [`web_device_test.go`](../../internal/client/cli/web_device_test.go), S1
  on a joined device: the cookie working with no token after `koko web`
  restarts, after a Device Connection drop and after a koko server restart
  on the same data directory; revoking the Device Session from its own web
  UI refusing a page and an API call with `revoked` while a held call keeps
  its Device Connection open, then `koko web` exiting with `revoked` and the
  device's files gone; the direct path running the revocation cleanup
  itself; a Local run's cookie, another Device Session's cookie and its token
  refused; the token and the cookie absent from stderr and every log;
  `koko disconnect` ending `koko web` and counting it; and, in
  `realTime`, a running `koko web` keeping the Device Mux from idling out.
- `web.start`'s rows in the [access matrix](access.md), and its refusal of
  a Revoked Device Session in
  [`revoked_test.go`](../../internal/server/revoked_test.go).
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
