# `koko web` and the web front

How a browser reaches the web UI: `koko web` on the person's machine, the
`web@koko` channel, and the web front on the koko server, which answers that
channel before the API does; and the web listener, where the Owner's
reverse proxy reaches the same web front at the Web URL
([ADR 0042](../adr/0042-the-web-ui-is-also-served-over-plain-http-to-the-owners-reverse-proxy.md)). The design is
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
   A taken port is told by the listen's error number, which differs by OS
   ([`sockerr.Is`](../../internal/shared/sockerr/sockerr.go): `EADDRINUSE`,
   or `WSAEADDRINUSE` on Windows). On Windows, which has no lookup, the
   refusal says "another process".
2. **The token.** It calls `web.start` and gets a one-time token.
3. **The address.** It prints `http://localhost:<port>/?t=<token>` as soon
   as it serves; `--json` prints `{"address": "..."}`, once, at that moment
   ([`longAction`](../../internal/client/cli/tree.go)). Showing the token is
   the command's purpose; nothing else prints it.
4. **Opening.** Unless `--no-open`, it opens the address in this OS's way
   (`openAddress`, in
   [`browser_<os>.go`](../../internal/client/cli/browser.go)) and serves at
   once. On Linux and macOS it runs the OS's opener, found on the
   invocation's `PATH` (`xdg-open` on Linux, `open` on macOS); it never
   stops the opener, which may be the browser itself, and waits for it to
   exit before it exits itself. On Windows it asks the shell to "open" the
   address (`windows.ShellExecute`), so no command line carries the token
   and no `cmd.exe` reads the address's `&` or `?`. When the opener is
   missing or the opening fails, it says so on stderr and serves on. The
   shell's "open" asks the registry, not `PATH`, and would open a real
   browser on a test's machine, so tests on Windows put a fake in its place
   through the browser hook (`cli.Runner.OpenBrowser`), which nothing else
   sets.
5. **Forwarding.** Each browser connection becomes one `web@koko` channel,
   carried both ways until either side closes it. Through the Device Mux, a
   dropped Device Connection ends the channels on it but not `koko web`'s
   connection to the Device Mux, so the browser's next connections open
   new channels once the Device Mux has reconnected, waiting up to
   `timing.CommandWait` for it ([the Device Mux](device-mux.md)). A browser
   connection whose channel cannot open gets `502 Bad Gateway` with a
   plain-text body naming the failure, as stderr does, not a reset
   ([`badGateway`](../../internal/client/cli/web.go)). It gets it only
   once `koko web` has read its request, since a browser fails a call
   whose answer came before the request was under way. A running
   `koko web` is a command connected to the Device Mux, so it keeps the
   Device Mux from idling out, with or without a browser connection open.
6. **The end** ([`webEnded`](../../internal/client/cli/web.go)). It runs
   until interrupted (exit 0), and it closes every browser connection before
   it exits. Otherwise it ends when its connection to the koko server does,
   once each browser connection has passed on what the koko server sent on
   its channel before, such as the answer to a call in flight when the
   Device Session was Revoked, or `timing.PassOn` (5 s) has passed, for a
   browser that does not read ([`serve`](../../internal/client/cli/web.go)):
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
on, an SSH `uint32` ([`wire.WebPayload`](../../internal/shared/wire/wire.go)):
revision 1 of `web@koko` (`wire.WebRevision`). `koko web` sends the lower of
its own revision and the one the koko server reports, and from revision 2
the payload ends with its revision as a `uint32` ([API](api.md#revisions)).
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
   a response. This defeats DNS rebinding. `/terminal/workspace` and
   `/terminal/socket` then go to
   [the web terminal's WebSockets](#the-web-terminals-websocket), and a path
   under `/terminal/files/` to [the web terminal's
   files](#the-web-terminals-files), each of which checks the rest itself.
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
Every answer carries the headers the web listener's carry
([below](#the-headers-every-answer-carries)) but
`Strict-Transport-Security`, since `koko web` serves `http://localhost`.
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

## The web listener

[`kokod run --web-listen <host:port>`](initialisation.md) (`KOKO_WEB_LISTEN`)
turns on a second listener: plain HTTP/1.1, no TLS, meant for the Owner's
reverse proxy alone, which holds the domain and its certificate. Empty, the
default, is off: a koko server without it listens on nothing new.

- **Its address** is `host:port` with a numeric port; anything else is
  refused with exit 2 before the koko server starts. It is bound
  ([`listenWeb`](../../internal/server/weblisten.go)) before the store
  opens; an address another process holds is refused with `port_taken`,
  naming that process as [`portowner`](#the-port-owner-lookup) finds it,
  and the commands that name it (`ss -ltnp`, `lsof -i`) when portowner
  cannot tell its pid.
  The docs give `127.0.0.1:7823` on systemd, and `0.0.0.0:7823` in a
  container, published on the host's loopback. Outside a container
  (`server.Config.InContainer`, which `cmd/kokod` sets when `/.dockerenv` or
  `/run/.containerenv` is there), an address that is not loopback, every
  address included, gets a warning at start. `koko server install
  --web-listen` writes it into the unit ([the installer](installer.md)).
- **It serves** on a fresh koko server too, from the start, once the Unix
  socket listens. Its start log line, just before `koko server started`, is
  `web listener` with `listen=` its bound address, or `off`, and `web_url=`
  the Web URL, or that none is set.
- **At shutdown** the web terminals end first, each with the Gateway's
  restart notice and its pages' WebSockets closed, and every Workspace
  socket gets `stopping`, since the listener's shutdown does not track a
  WebSocket ([the Gateway](gateway.md#the-web-terminal)).
  The listener is closed next, before the Tunnels' notices: it stops
  accepting and waits up to 5 s for its calls in flight, then cuts them
  ([`Server.Close`](../../internal/server/server.go)).
- **`server.show`** names its bound address as `web_listen`, `null` while it
  is off ([initialisation](initialisation.md#the-deployment-afterwards)).

[`Front.Listener`](../../internal/server/web/listener.go) answers each
request in this order, reading the deployment's row at every request, so a
change of the Web URL takes effect on the next one. Only the `/auth/`
endpoints set the Sign-in cookie, and only they and `/v1/` read it.

1. **Before initialisation**, every request gets a `503` page saying the
   koko server is not initialised yet.
2. **Without a Web URL**, every request gets a `503` page naming `koko
   server set web_url=https://…`.
3. **The Host header** must name the Web URL's host and port, without
   regard to case, the default port 443 written or not
   (`https://koko.example.com` takes `koko.example.com` and
   `koko.example.com:443`). Any other gets a `421` page saying that this
   koko server answers at its Web URL alone and that the proxy must pass
   the Host header on; it names the Web URL, never the Host the request
   carried. Caddy passes it by default; nginx needs `proxy_set_header Host
   $host`. `X-Forwarded-Host` and `X-Forwarded-Proto` are never read.
4. **The Origin check.** Every `POST`, on any path, must carry an `Origin`
   header equal to the Web URL; another, or none, is refused with
   `bad_origin` (403) from its headers alone, before its body is read or
   anything is counted. An API path and an `/auth/` endpoint get the error
   object, any other path a page; a files endpoint gets the error object
   too.
5. **`/terminal/socket`** and **`/terminal/workspace`** are [the web
   terminal's WebSockets](#the-web-terminals-websocket).
6. **`/auth/`** is the web front's own endpoints, which sign a browser in,
   set a Password and sign out ([Sign-ins](signin.md#the-auth-endpoints)).
7. **`/terminal/files/`** is [the web terminal's
   files](#the-web-terminals-files).
8. **`/v1/`** goes to the API as the live Sign-in the `__Host-koko` cookie
   names (`op.Caller.SignIn`), moving its Last used time; without one it is
   answered `signin_required` (401) without reaching the API
   ([`Front.serveSignedIn`](../../internal/server/web/listener.go)).
9. **Every other path** serves the web UI's files as `web@koko` does, the
   same code path (`Front.serveFile`).

Refusals, `/auth/` and API answers carry `Cache-Control: no-store`. The
client's address, which the log names and the sign-in limits count, is the last address in `X-Forwarded-For`, the
one the proxy appended, or the TCP peer's when the header is absent or its
last entry is not an IP address
([`clientAddress`](../../internal/server/web/listener.go)); no list of
trusted proxies is kept, since every peer is the proxy. A refusal of the
Host or Origin check is logged at debug level with its reason and that
address.

| Code | Status | When |
| --- | --- | --- |
| `signin_required` | 401 | A call on the web listener came without a live Sign-in, or as one that has ended. |
| `bad_origin` | 403 | A `POST` on the web listener, or a web terminal's WebSocket on either way in, came without the `Origin` header of a page of this koko server. |

### The headers every answer carries

Every answer of the web listener carries
`Content-Security-Policy: default-src 'self'; script-src 'self'
'wasm-unsafe-eval'; style-src 'self'
'sha256-gYiS/BvZvRcK27JIXTuwhZ3hs2+VJ1X+2gUlE+farlg='; frame-ancestors
'none'; base-uri 'none'; form-action 'self'`, `X-Content-Type-Options:
nosniff`, `Referrer-Policy: no-referrer` and `Strict-Transport-Security:
max-age=31536000`; `koko web`'s carry the same but the last
([`securityHeaders`](../../internal/server/web/web.go)). The policy lets a
page load nothing from another site, nor run an inline script, apply an
inline style sheet or load a `data:` URL, so the web UI's build must hold
none of them. The one inline style sheet it allows, by its hash, is the
`<style>` element React Aria adds on iOS Safari while an overlay is open
([ADR 0046](../adr/0046-the-web-ui-is-built-on-heroui.md));
[the web UI's static checks](web-ui.md#static-checks) fail when the pinned
React Aria adds another text. `script-src` adds to `'self'` only
`'wasm-unsafe-eval'`, the compiling of WebAssembly, which the web
terminal's emulator needs
([ADR 0054](../adr/0054-the-web-terminal-is-wterm-on-its-ghostty-core-behind-one-component-of-kokos-own.md));
no inline script, `eval` or other source is allowed.

### The web UI's files and the browser's cache

Both ways in serve the web UI's files through
[`Front.serveFile`](../../internal/server/web/web.go).

- **A file under the build's `assets/`**, where Vite gives every name a
  hash of the file's content, carries `Cache-Control: public,
  max-age=31536000, immutable`, so a browser downloads each chunk, font and
  `.wasm` file once per release.
- **`index.html` and every other file** carry no caching header, since a
  release changes what they hold under the same name. That covers the
  `index.html` a path naming no file is answered with, a missing file
  under `assets/` included.
- **The `Content-Type`** follows the file's extension (Go's `mime` table):
  a `.wasm` file is `application/wasm`, so a browser compiles it while it
  downloads.

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

## The web terminal's WebSocket

A page at `/terminal` keeps two kinds of WebSocket
([`terminal.go`](../../internal/server/web/terminal.go);
[ADR 0053](../adr/0053-a-browser-opens-ssh-sessions-through-a-websocket-to-the-gateway.md),
refined by
[ADR 0057](../adr/0057-a-users-web-terminal-tabs-are-one-workspace-held-by-the-koko-server.md);
P1 spec: a terminal in the web UI, "The terminal endpoint", and spec
#213), each a WebSocket handshake
([RFC 6455](https://www.rfc-editor.org/rfc/rfc6455)) on the web listener
and on `web@koko` alike:

- **`GET /terminal/workspace`** (`web.WorkspacePath`), the Workspace
  socket, one per page: its User's Workspace, and its requests to open,
  reopen and close tabs ([below](#the-workspace-socket)).
- **`GET /terminal/socket`** (`web.TerminalPath`), the terminal socket,
  one for each SSH Session the page attaches to
  ([below](#the-terminal-socket)).

After the checks every request gets, the 503 pages and the Host check on
the listener, or the Host guard on `web@koko`, the web front answers both
with the same checks in this order (`Front.acceptSocket`, then
`Front.admitPage`):

1. **Another method** gets a `405` page, and **a `GET` that is no
   WebSocket upgrade** (`Connection: upgrade` and `Upgrade: websocket`) a
   `426` page.
2. **The Origin** must be the Web URL on the listener, or on `web@koko`
   `http://localhost:<port>` or `http://127.0.0.1:<port>` with the channel's
   port (without it for port 80). Any other, or none, gets a `403` page
   (`bad_origin`) before the cookie is read: the defence against
   cross-site WebSocket hijacking, since `SameSite=Lax` keeps the cookie
   off a cross-site handshake but same-site is wider than same-origin.
3. **Accepted** with [`github.com/coder/websocket`](https://github.com/coder/websocket)'s
   `Accept`, compression off, no subprotocol and a read limit of 64 KiB a
   message. The answer carries the way in's headers and no CORS header. On
   the listener `http.Server` gives the connection up to it; on `web@koko`
   the channel's connection is given up the same way.
4. **The first message**: one text message of JSON, at most 4 KiB, within
   `timing.FirstMessage` (10 s) of the upgrade. None in time, binary,
   larger or no JSON closes the WebSocket with status 1008 and no answer.
5. **The protocol.** A first message naming another `protocol` than this
   koko server's (`wire.TerminalProtocol`, 2) is refused with
   `reload_page`, on both sockets, a protocol 1 page's among them; the page
   and the koko server are one build.
6. **The caller**: the live Sign-in on the listener, and on `web@koko` the
   connection's Device Session or the Local caller with its run of `koko
   web`.

A browser reports a refused upgrade as an error alone, so every refusal
but the two before the upgrade is the first message's answer,
`{"refused": "<code>", "message": "<text>"}`, and then a close with
status 1008; the page then makes no further attempt. The web front answers
these itself, in this order, before the Gateway sees the message:

| Code | When |
| --- | --- |
| `reload_page` | `protocol` is not this koko server's (`wire.TerminalProtocol`): `this page is out of date: reload it`. |
| `signin_required` | On the listener, no live Sign-in; the check moves its Last used time, as a call does. |
| `revoked` | On `web@koko`, the connection's Device Session was Revoked, as for every request there. |
| `cookie_required` | On `web@koko`, no valid cookie of this `koko web`. |

The first message is then handed to the
[Gateway](gateway.md#the-web-terminal) with the caller, which answers it.
While the koko server stops, a new WebSocket is closed at once. No secret
is in the URL: the cookie authenticates, and the ids travel inside the
WebSockets. A refusal is logged at debug level as `web terminal refused`
with its reason and the client's address, never with the cookie.

### The Workspace socket

Every message either way is one text message of JSON
([`wire/workspace.go`](../../internal/shared/wire/workspace.go)), at most
4 KiB from the page (`wire.WorkspaceMaxMessage`).

| Page sends | The koko server answers |
| --- | --- |
| `{"protocol": 2}`, first | the Workspace, below |
| `{"request": 7, "open": "web1", "cols": 120, "rows": 36}` | `{"request": 7, "tab": "<32 hex>"}` once the tab is added, its SSH Session `starting`; or `{"request": 7, "refused": "<code>", "message": "<text>"}` |
| `{"request": 8, "reopen": "<tab>", "cols": 120, "rows": 36}` | the same, for a tab `ended`, `refused` or `restarted`: a new SSH Session in it; a tab `starting` or `running` is answered as it is, and one that is gone with `unknown_tab` |
| `{"request": 9, "close": "<tab>"}` | `{"request": 9}` once the tab is gone and its SSH Session ending; a tab already gone is answered the same |
| `{}` | nothing: liveness alone |

The Workspace, sent first and again after every change to every page of
the User, whole, before the answer to the request that changed it:

```json
{"tabs": [
  {"id": "<32 hex>", "host": "web1", "label": "web1", "session": "<32 hex>", "state": "running", "opened_at": "…"},
  {"id": "<32 hex>", "host": "db2", "label": "db2", "state": "ended", "opened_at": "…",
   "ended": {"reason": "shell ended", "exit_status": 0}},
  {"id": "<32 hex>", "host": "web1", "label": "web1 (2)", "state": "refused", "opened_at": "…",
   "refused": {"code": "host_unreachable", "message": "…"}}
]}
```

- **An open's refusals** carry `unknown_host`, `host_registered`,
  `relay_registered`, `access_refused`, `ssh_session_limit` (the 17th
  tab), `host_unreachable`, `host_key_mismatch`, `login_refused`,
  `host_connection_failed` or `shell_refused`, with the Gateway's banner
  for it, and a failure of the koko server's own `internal_error`. The
  limit and the admission's refusals answer the request; those of the Host, its chain or
  its shell come after the answer, in the tab's `refused`
  ([the Gateway](gateway.md#the-web-terminal)).
- **A message that is no request**, one asking for none or several of
  `open`, `reopen` and `close`, one without its `request` number, an open
  at a size outside 1 to 4,096, or a tab's id that is not 32 lowercase hex
  characters, closes the socket with status 1008; one over 4 KiB with
  1009, the WebSocket's own.
- **Each request acts for its caller as it is now:** a caller that has
  ended since the first message, such as a revoked Sign-in, changes
  nothing, and its socket is closed.
- **Reopening after a restart.** The page reopens its `restarted` tabs in
  tab order, sending each `reopen` only once the last one's tab has left
  `starting`, so that the koko server dials their Hosts one by one.
- **Liveness.** Each end sends `{}` after `timing.ServerProbe` (15 s)
  without sending, and cuts the WebSocket after `timing.ServerDead` (45 s)
  without receiving. The first message moves the Sign-in's Last used time;
  the messages after it do not.
- **The koko server stopping** sends `{"stopping": "the koko server is
  restarting"}` and closes the socket.

### The terminal socket

| Page sends | Answered with one text message |
| --- | --- |
| `{"protocol": 2, "attach": "<SSH Session id>", "cols": 120, "rows": 36}` | `{"attachment": "<32 hex>"}`, then the stream; or `unknown_tunnel` for an id that names no live SSH Session of the caller's User, `ssh_session_ended` for one that has ended |
| `{"protocol": 2, "resume": "<attachment id>", "offset": 4096}` | `{"offset": 2048}`, the koko server's received offset, or `{"refused": "resume_refused", "message": "<why>"}`, the message empty for an id the Gateway holds nothing of for this caller |

([`wire.TerminalFirst`](../../internal/shared/wire/terminalfirst.go).) A
first message that asks for neither or both of `attach` and `resume`, an
attach at a size outside 1 to 4,096, or an id that is not 32 lowercase hex
characters is closed with 1008 and no answer; so is one at protocol 2
that carries protocol 1's `open` or `end`, since a tab is opened and
closed on the Workspace socket. Once
accepted, every message either way is binary, and their bytes are the
attachment's resumable stream (`websocket.NetConn`) carrying terminal
records ([`wire/terminal.go`](../../internal/shared/wire/terminal.go)): a
one-byte type, a uvarint length of at most 32 KiB, then the payload. A
text message then closes the WebSocket with status 1003, `NetConn`'s own.

| Record | Way | Payload |
| --- | --- | --- |
| `data` (0x00) | both | bytes typed or pasted, or the shell's output |
| `resize` (0x01) | page → Gateway | uvarint columns, uvarint rows, each 1 to 4,096: the page's box in cells |
| `exit status` (0x02) | Gateway → page | the shell's exit status as a uvarint |
| `exit signal` (0x03) | Gateway → page | the name of the signal that ended the shell |
| `reset` (0x04) | Gateway → page | uvarint columns, uvarint rows: clear the emulator, scrollback included, to this size; a snapshot follows as data records |
| `active` (0x05) | page → Gateway | nothing: a person acted in this page |
| `active` (0x05) | Gateway → page | one byte: 1 when this page is the SSH Session's active page, 0 when it is no longer |
| `colors` (0x06) | page → Gateway | the theme's foreground, then its background, each 3 bytes of RGB |
| `displaced` (0x07) | Gateway → page | nothing: more pages attached than the SSH Session takes, and this one, the least recently active, is detached; its stream closes next |

[`wire/testdata/terminal.golden`](../../internal/shared/wire/testdata/terminal.golden)
holds each record's bytes, and the bytes refused, in a section per
protocol, which the page's end reads too
([the resumable stream](resumable-stream.md#verify)).

## The web terminal's files

The Files panel's calls are the web front's own endpoints under
`/terminal/files/` ([`files.go`](../../internal/server/web/files.go),
`api.FilesPrefix`), on the web listener and on `web@koko` alike (P1 spec:
a terminal in the web UI, "Files over SFTP"; ADR 0053), apart from the API
as `/auth/` is, so no operation is added. Each names a web terminal's SSH
Session by its id, and moves files on its Host over SFTP, as the Host
Account, through the [Gateway](gateway.md#the-web-terminal)'s `Files`:
the web front holds no Host connection. The calls stay off the WebSocket,
so a large file never waits in the stream's buffer beside the shell's
output.

A request is answered in this order (`Front.serveFiles`), every answer
JSON with `Cache-Control: no-store` beside the way in's headers:

1. **An unknown endpoint** is `unknown_operation` (404), and **another
   method** than a JSON endpoint's `POST` `method_not_allowed` (405). A
   download's `GET` skips the Origin check and the body, and follows its
   own order (a download, below).
2. **The Origin check**: on the listener as for every `POST`, and on
   `web@koko` `http://localhost:<port>` or `http://127.0.0.1:<port>`, where
   the API needs none since it takes only JSON. Another, or none, is
   `bad_origin` (403), before the cookie or the body is read.
3. **The caller**, as for the WebSockets: on the listener the live Sign-in,
   moving its Last used time as a call does, else `signin_required`; on
   `web@koko` `revoked`, then `cookie_required`.
4. **A JSON endpoint** takes `application/json` of at most 4 KiB
   (`api.MaxFilesBody`), a member it does not know refused.
5. **The Gateway** takes any live caller of the SSH Session's User, from
   any page of theirs, so another User's id is `unknown_tunnel`, as an id
   of none is. The files move whether any page is attached to the SSH
   Session or none. Uploads and download tokens stay the caller's own.
   Each upload chunk reads the caller anew, and a download is cut once
   the access-change signal ends its caller (`downloads.cutEnded`: a
   Device Session Revoked, a Sign-in ended, or every Sign-in's on a Web
   URL change), from the moment the web front read its caller, the
   Gateway's read in `Files` being the read after. One cut before its
   first byte is refused, as the caller now is. Once it streams, the file
   closes and a past write deadline frees a write that waits for a browser
   reading nothing, so the answer ends short and the log says
   `outcome=failed` with `err="its caller has ended, and the download with
   it"`; a download is logged `done` only once its last bytes left the
   answer's buffer. Through `koko web`, whose
   connection takes no deadline, a Revoked Device Session's Device
   Connection closing ends it.

| Endpoint | Takes | Answers |
| --- | --- | --- |
| `POST /terminal/files/list` | `{"tunnel", "path"}`; `path` absolute, or empty for the Host Account's home | `{"path", "entries": [{"name", "kind", "size", "modified"}], "more"}` |
| `POST /terminal/files/upload` | `{"tunnel", "dir", "name", "size", "replace"}` | `{"upload": "<32 hex>"}`, once the partial file is made |
| `POST /terminal/files/chunk?upload=<id>&offset=<n>` | the bytes, `application/octet-stream`, at most 512 KiB (`api.MaxFilesChunk`) | `{"received": <n>}` |
| `POST /terminal/files/finish` | `{"upload"}` | `{"path", "size"}` |
| `POST /terminal/files/cancel` | `{"upload"}` | `{}` |
| `POST /terminal/files/download` | `{"tunnel", "path"}`; `path` absolute | `{"token": "<32 hex>", "name", "size"}` |
| `GET /terminal/files/download/<token>` | | the file's bytes |

- **A listing** resolves the path on the Host (SFTP `realpath`), so `..`
  goes up and a link to a directory is entered, and answers it as
  resolved. An entry's `kind` is `file`, `dir`, `link` (not followed) or
  `other`, `size` is in bytes and `modified` RFC 3339 in UTC. At most
  5,000 entries (`api.MaxListed`) are listed, directories first, then by
  name byte for byte, and `more` counts the rest; the page sorts them by
  the browser's collation. A file named as a directory, or one the Host
  Account cannot read, is `host_refused` with the Host's message. A
  listing is not logged.
- **An upload** ([`upload.go`](../../internal/server/web/upload.go)) is
  held in the web front's memory, by its 128-bit id, for its caller alone.
  - **Starting it.** A name that is empty, `.`, `..` or holds `/` is
    `invalid_name` (400), and one that exists `file_exists` (409) unless
    `replace` is set; the page asks first. A fifth upload while the SSH
    Session runs four is `too_many_uploads` (429); the page queues it.
    `upload` then creates `.<name>.koko-upload-<8 hex>` in `dir`,
    exclusively, with the mode `0600` in the open itself, so no other
    account can open it first ([the Gateway](gateway.md#the-web-terminal)).
  - **Chunks** are read whole before the upload is touched, so a page that
    sends slowly holds nothing of it, then written at `offset`. A chunk at
    an offset already received is answered with the count and not written
    again, so the page resends safely after a drop; a gap is `bad_offset`
    (409), and a chunk past the size `invalid_request`. A write the Host
    refuses, as on a full disk, is `host_refused`, and the page sends the
    chunk again.
  - **Finishing** takes every byte, else `bad_offset`. It gives the partial
    file the mode of the file it replaces, or `0644`, then renames it onto
    its name: with `posix-rename@openssh.com` where the Host offers it, so
    a replace is one step, or else removing the old file first; without
    `replace`, SFTP's rename, which never replaces a file made meanwhile.
    A finish sent again, after an answer lost to a drop, is answered the
    same.
  - **The partial file goes** on `cancel`; after `timing.UploadIdle` (15
    minutes) without a call of it; and when the SSH Session ends, while its
    Host connection lives (`Files.AtEnd`), after which a call of it is
    `ssh_session_ended`. A broken Host connection, or a koko server that
    stops, leaves it under its hidden name, never its real one. A cancelled
    or given-up upload is then unknown.
  - Each is logged once it ends: `file transferred` with `direction=upload`
    ([the Gateway's log](gateway.md#logging)), `outcome=done`,
    `cancelled`, or `failed` with `err` (`no chunk came for 15m0s`, `the SSH
    Session ended`).
- **A download** ([`download.go`](../../internal/server/web/download.go))
  goes in two steps, so the file's path never enters a URL, and so never
  the proxy's log, and a cross-site page gets nothing from a `GET`.
  - **The token.** `download` takes a regular file only, links followed, so
    a FIFO or `/dev/zero` is `not_a_file` (409) and never streams for ever.
    It answers a token, 128 random bits kept only by their SHA-256, good
    once, for 60 seconds on the koko server's clock, for the same caller.
  - **The `GET`** of `/terminal/files/download/<token>`, a download link,
    checks the caller as every files call does, then spends the token: one
    of another caller's is `unknown_download` (404) and is not spent, as is
    one spent or past its time. The file is checked again before it is
    opened, since opening a FIFO waits for a writer and would hold the SFTP
    channel every call shares. The answer streams the file's size, read
    from the open file, with `Content-Type: application/octet-stream`,
    `Content-Length`, `Content-Disposition: attachment` (RFC 6266) with the
    name in UTF-8 as `filename*` and an ASCII stand-in as `filename`, and
    `no-store`, beside the way in's headers. The browser shows its own
    progress and Cancel, so a large file never sits in the page's memory.
  - Each is logged once it ends, `file transferred` with
    `direction=download` and the path as asked: `done`; `cancelled` when
    the browser closed the request; `failed` with `err` when the Host's
    read failed, the file ended early, or the SSH Session ended, which cuts
    the answer short. No line holds the token: a failure's log line names
    the path as `/terminal/files/download/<token>` (`loggedPath`), whichever
    step fails, the listener's own included.

| Code | Status | When |
| --- | --- | --- |
| `unknown_tunnel` | 404 | A files call named no live web terminal SSH Session of this caller's User. |
| `ssh_session_ended` | 410 | The web terminal's SSH Session ended, while its tab holds it as its last, which ends every transfer of it. |
| `sftp_unavailable` | 409 | The Host offers no SFTP: `web1 offers no SFTP, which koko moves files through`. The terminal is unaffected. |
| `host_refused` | 403 | The Host refused a listing, a read or a write; the message is the Host's, such as `Permission denied`. |
| `invalid_name` | 400 | An upload's name is empty, `.`, `..` or holds `/` (the API's own code). |
| `file_exists` | 409 | An upload's name exists and `replace` was not given. |
| `bad_offset` | 409 | A chunk would leave a gap, or a finish came before every byte. |
| `unknown_upload` | 404 | No live upload of this caller has the id. |
| `too_many_uploads` | 429 | The SSH Session already runs 4 uploads (`api.MaxUploads`). |
| `not_a_file` | 409 | A download named something other than a regular file, links followed. |
| `unknown_download` | 404 | No unspent download token of this caller is the one given. |

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
- **Windows** has none: `ErrUnsupported`. Naming the process that holds a
  port there is out of scope
  ([P1 spec: the koko CLI on Windows](https://github.com/cieons/koko/issues/151)).

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
- [`web_forward_test.go`](../../internal/client/cli/web_forward_test.go),
  `koko web`'s forwarder over a fake connection to the koko server that
  ends, revoked: a 16 MB answer the browser reads only after the end
  reaching it in full, and a browser that reads nothing keeping the
  forwarder no longer than `timing.PassOn`; and over one that opens no
  channel, a browser's call getting `502 Bad Gateway` in plain text, and a
  connection that ends without a request getting no answer.
- `web.start`'s rows in the [access matrix](access.md), `device_only` for a
  Sign-in among them, and its refusal of a Revoked Device Session in
  [`revoked_test.go`](../../internal/server/revoked_test.go).
- [`listener_test.go`](../../internal/server/web/listener_test.go), S1
  against the web listener on `127.0.0.1:0` (`harness.WebListen`, with
  `h.WebAddress`), the test setting `Host`, `Origin` and `X-Forwarded-For`
  as a proxy would: off by default; the two `503` pages, before
  initialisation and without a Web URL, and again once it is cleared; the
  Host check, 443 written or not, another port or host refused with the
  page naming the Web URL alone, and a change taking effect at once; the
  Origin check on `/v1/`, each `/auth/` endpoint and another path, before
  the API, before the body and before anything is counted;
  `signin_required`; every answer's headers and `no-store` on
  refusals; the client's address in the log; and the start log naming the
  Web URL.
- [`terminal_test.go`](../../internal/server/web/terminal_test.go), S1
  against the web listener with a Go page (`harness.TerminalPage`): a `GET`
  that is no upgrade answered `426`; another Origin, none and the Web URL
  on another scheme or port refused `403` before the upgrade; `reload_page`,
  a protocol 1 page's among them, and `signin_required` without a cookie
  and for an ended Sign-in, on the terminal socket and the Workspace socket
  alike, each closed with 1008; a malformed first message, protocol 1's
  `open` and `end` at protocol 2 among them, a binary one, one over 4 KiB
  and none within `timing.FirstMessage` closed with 1008 and no answer; and
  no Host dialled for any of them. What follows the first message is the
  Gateway's ([its tests](gateway.md#verifying)).
  [`wire/terminal_test.go`](../../internal/shared/wire/terminal_test.go)
  encodes and decodes each line of `terminal.golden`.
- [`terminal_files_test.go`](../../internal/server/web/terminal_files_test.go),
  S1 against the web listener and in-process Hosts serving SFTP: the
  listings, a Host without SFTP, every refusal of a files call as JSON,
  files with no page attached and once ended, the uploads and the downloads
  ([the Gateway's tests](gateway.md#verifying));
  [`web_terminal_files_test.go`](../../internal/client/cli/web_terminal_files_test.go)
  the Origin check on every files `POST` through `koko web`,
  `cookie_required`, and a listing as the Local caller;
  [`download_test.go`](../../internal/server/web/download_test.go) a
  download's failure logged without its token.
- [`run_checks_test.go`](../../internal/server/kokod/run_checks_test.go):
  a taken web listener address refused naming the process holding it;
  [`kokod_test.go`](../../internal/server/kokod/kokod_test.go):
  `--web-listen` and `KOKO_WEB_LISTEN`, and their refusals;
  [`weblisten_internal_test.go`](../../internal/server/weblisten_internal_test.go):
  which addresses draw the warning.
- `TestWebAnswersCarryTheSecurityHeaders` in
  [`web_test.go`](../../internal/client/cli/web_test.go): `koko web`'s
  answers carry the headers, without `Strict-Transport-Security`.
- [`files_test.go`](../../internal/server/web/files_test.go): a file under
  `assets/` served with the year-long cache, a `.wasm` file as
  `application/wasm`, and `index.html`, a file outside `assets/` and a
  missing asset's `index.html` with no caching header.
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
