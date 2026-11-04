# Which Go WebSocket library carries the Tunnel?

Research note for issue #3. Checked on 2026-11-05 against the repositories,
their source and the Go sources. Each claim links to what it rests on.

## Question

The koko CLI opens a Tunnel over WSS to the koko server's one HTTPS port
([ADR 0004](../adr/0004-one-https-port-and-wss-tunnels.md)). The Tunnel
carries one reliable byte stream (an SSH connection), must pass proxies and
CDNs that only allow 443, and may later get a browser client. Go 1.27,
`CGO_ENABLED=0`; the standard library and `golang.org/x` come first, and any
other dependency needs a reason. Candidates: `github.com/coder/websocket`,
`github.com/gorilla/websocket`, `github.com/gobwas/ws`,
`golang.org/x/net/websocket`.

## Summary and recommendation

Use **`github.com/coder/websocket`** (formerly `nhooyr.io/websocket`).

- It is the only candidate that ships a `net.Conn` adapter written for
  tunneling (`websocket.NetConn`), has zero module dependencies, allows
  concurrent writes, implements full RFC 7692 compression, offers a
  `Ping(ctx)` that waits for the pong, and compiles the client for
  `GOOS=js GOARCH=wasm` by wrapping the browser WebSocket API.
- It is actively maintained by Coder: v1.8.15 on 2026-06-15, commits on
  2026-11-03, `go 1.26` in `go.mod`, and an open PR for RFC 8441
  (WebSocket over HTTP/2) that tracks the Go toolchain's own gate.
- `golang.org/x/net/websocket` is the one `golang.org/x` option, but its own
  package doc says it lacks features and points at gorilla and coder; it has
  no compression and no ping API. `gorilla/websocket` is stable but had no
  release since 2024-06-14 and no commit since 2025-03-19, has no `net.Conn`
  adapter, and allows one writer at a time. `gobwas/ws` is a low-level
  frame toolkit: fast and freshly released, but the Tunnel would have to
  build its own stream, control-frame and concurrency layer on top.
- None of the four does WebSocket over HTTP/2 today, and Go 1.27 itself
  disables RFC 8441 extended CONNECT unless `GODEBUG=http2xconnect=1` is set,
  so the Tunnel speaks HTTP/1.1 Upgrade on the one HTTPS port for now. This
  is not a differentiator between libraries.

## Comparison table

| | coder/websocket | gorilla/websocket | gobwas/ws | x/net/websocket |
|---|---|---|---|---|
| Latest release | v1.8.15, 2026-06-15 [[1]](#c1) | v1.5.3, 2024-06-14 [[12]](#g1) | v1.5.0, 2026-11-04 [[21]](#b1) | x/net v0.62.0, 2026-10-27 [[31]](#x1) |
| Last commit | 2026-11-03 [[2]](#c2) | 2025-03-19 [[13]](#g2) | 2026-11-03 [[22]](#b2) | package: 2026-01-06 [[32]](#x2) |
| Maintenance | Coder, active; roadmap in README [[3]](#c3) | Gorilla org, archived 2022-12-09, un-archived 2023-07-15 [[14]](#g3); API "stable" [[15]](#g4) | one author, active [[22]](#b2) | own doc: "lacks some features", points at gorilla and coder [[33]](#x3) |
| `net.Conn` for a byte stream | `websocket.NetConn(ctx, c, msgType)`, built for tunneling [[4]](#c4) | no; `Conn.NetConn()` returns the raw socket and corrupts the WebSocket if used [[16]](#g5) | no; `UpgradeHTTP` hands back the raw `net.Conn`, framing is yours [[23]](#b3) | `*Conn` has the `net.Conn` method set; Read/Write are frame-based [[34]](#x4) |
| Concurrency | all methods concurrent except `Reader`/`Read` [[5]](#c5) | one concurrent reader and one concurrent writer [[17]](#g6) | none built in; `wsutil.Reader` "not goroutine safe" [[24]](#b4) | "Multiple goroutines may invoke methods on a Conn simultaneously" [[34]](#x4) |
| permessage-deflate | yes, context takeover and no-context-takeover [[6]](#c6) | experimental, no context takeover [[18]](#g7) | `wsflate` package, bring your own flate [[25]](#b5) | no (`compress/flate` is not imported) [[35]](#x5) |
| Ping/pong | `Ping(ctx)` waits for pong; `CloseRead` keeps control frames answered [[7]](#c7) | `WriteControl` + `SetPingHandler`/`SetPongHandler`; app must keep reading [[19]](#g8) | `wsutil.ControlHandler` answers ping; you call it [[26]](#b6) | answers ping with pong while reading; no API to send one [[36]](#x6) |
| Server with `net/http` | `Accept(w, r, opts)`; needs `http.Hijacker`, follows `Unwrap` [[8]](#c8) | `Upgrader.Upgrade`; uses `ResponseController.Hijack` [[20]](#g9) | `UpgradeHTTP(r, w)`; uses `ResponseController.Hijack` [[23]](#b3) | `websocket.Handler`/`Server` as `http.Handler` [[37]](#x7) |
| WebSocket over HTTP/2 (RFC 8441) | not released; PR #539 open since 2025-09-29 [[9]](#c9) | no | no | no; CL abandoned 2024-12 [[38]](#x8) |
| Client dial | `net/http.Client` [[10]](#c10) | own TCP/TLS dial, `x/net/proxy` for proxies [[20]](#g9) | own TCP/TLS dial [[27]](#b7) | own TCP/TLS dial [[39]](#x9) |
| `GOOS=js` (browser) | client yes, via `syscall/js`; `Accept` errors, `Ping` is a no-op [[11]](#c11) | no; issue #432 open [[14]](#g3) | no `syscall/js` use [[28]](#b8) | not applicable (needs `net.Dial`) [[39]](#x9) |
| Module deps | none [[1]](#c1) | `golang.org/x/net` [[12]](#g1) | `github.com/gobwas/httphead` (no deps of its own) [[21]](#b1) | part of `golang.org/x/net` (requires x/crypto, x/sys, x/term, x/text) [[31]](#x1) |
| Assembly | masking in Go asm for amd64/arm64 [[11]](#c11) | none (`unsafe`) [[3]](#c3) | masking in asm for amd64/arm64 [[28]](#b8) | none |

Go assembly builds with `CGO_ENABLED=0`; none of these use cgo.

## Detail per candidate

### github.com/coder/websocket

- <a id="c1"></a>[1] Latest release v1.8.15, published 2026-06-15
  (GitHub releases API,
  <https://github.com/coder/websocket/releases/tag/v1.8.15>). `go.mod` is
  `module github.com/coder/websocket` / `go 1.26` with no `require` block
  (<https://github.com/coder/websocket/blob/master/go.mod>); the README
  calls this "Zero dependencies"
  (<https://github.com/coder/websocket/blob/master/README.md>).
- <a id="c2"></a>[2] Last commit on master 2026-11-03, "ci: run the test jobs
  on Go 1.26 and 1.27 (#594)"; `go.mod` moved to Go 1.26 on 2026-10-23
  (GitHub commits API, <https://github.com/coder/websocket/commits/master>).
- <a id="c3"></a>[3] README: "Coder now maintains this project ... grateful to
  nhooyr for authoring and maintaining this project from 2019 to 2024";
  Highlights list "Concurrent writes", "net.Conn wrapper", "Ping pong API",
  "RFC 7692 permessage-deflate compression", "Compile to Wasm", "Fully
  passes the WebSocket autobahn-testsuite". The comparison section says
  gorilla's masking "uses unsafe"
  (<https://github.com/coder/websocket/blob/master/README.md>).
- <a id="c4"></a>[4] `netconn.go`: "NetConn converts a *websocket.Conn into a
  net.Conn. It's for tunneling arbitrary protocols over WebSockets." Every
  `Write` is one message of the given type; the ctx bounds the conn's
  lifetime; a hit deadline with an active read or write goroutine closes the
  connection ("different from most net.Conn implementations"); `Dial`-side
  Addr methods return a mock `websocket/unknown-addr`; a received
  NormalClosure or GoingAway close becomes `io.EOF`; the read limit is set
  to -1 (<https://github.com/coder/websocket/blob/master/netconn.go>).
- <a id="c5"></a>[5] `conn.go`: "All methods may be called concurrently except
  for Reader and Read. You must always read from the connection. Otherwise
  control frames will not be handled."
  (<https://github.com/coder/websocket/blob/master/conn.go>).
- <a id="c6"></a>[6] `compress.go`: `CompressionDisabled` (default),
  `CompressionContextTakeover` (32 KB sliding window, 1.2 MB flate.Writer,
  falls back to no-context-takeover), `CompressionNoContextTakeover`
  (messages over 512 bytes, pooled writers). Only `compress/flate` from the
  standard library (<https://github.com/coder/websocket/blob/master/compress.go>).
- <a id="c7"></a>[7] `conn.go`: "Ping sends a ping to the peer and waits for a
  pong ... Ping must be called concurrently with Reader ... TCP Keepalives
  should suffice for most use cases." `read.go`: `CloseRead` "actively
  reads from the connection, it will ensure that ping, pong and close
  frames are responded to." A heartbeat helper is still a roadmap item
  (issue #267, open) (<https://github.com/coder/websocket/blob/master/conn.go>,
  <https://github.com/coder/websocket/blob/master/read.go>,
  <https://github.com/coder/websocket/issues/267>).
- <a id="c8"></a>[8] `accept.go`: `Accept` writes "http.ResponseWriter does not
  implement http.Hijacker" and 501 when hijacking is unavailable; "using
  the http.Request Context after Accept returns may lead to unexpected
  behavior (see http.Hijacker)". `hijack.go` walks `Unwrap()` like
  `http.ResponseController` so middleware wrappers work
  (<https://github.com/coder/websocket/blob/master/accept.go>,
  <https://github.com/coder/websocket/blob/master/hijack.go>).
- <a id="c9"></a>[9] HTTP/2: issue #4 "WebSockets over HTTP/2" open since the
  nhooyr era (<https://github.com/coder/websocket/issues/4>). PR #539 "feat:
  add experimental http2 support" (opened 2025-09-29, updated 2026-11-03,
  open) adds an opt-in `HTTPProtocol` field to `AcceptOptions` and
  `DialOptions`; the server process "must run with GODEBUG=http2xconnect=1,
  because Go has disabled extended CONNECT by default since Go 1.24
  (golang/go#71128)"; the client needs an `http.Client` on
  `golang.org/x/net/http2.Transport` and, on Go 1.27, `-tags http2legacy`
  (<https://github.com/coder/websocket/pull/539>).
- <a id="c10"></a>[10] `dial.go`: `DialOptions.HTTPClient` "is used for the
  connection. Its Transport must return writable bodies for WebSocket
  handshakes"; `Dial` calls `opts.HTTPClient.Do(req)`
  (<https://github.com/coder/websocket/blob/master/dial.go>). `net/http`
  itself keeps such requests on HTTP/1: `Request.requiresHTTP1` is true
  for `Connection: upgrade` + `Upgrade: websocket`, and the Transport then
  skips the HTTP/2 path and sets `onlyH1`
  (<https://github.com/golang/go/blob/master/src/net/http/request.go>,
  <https://github.com/golang/go/blob/master/src/net/http/transport.go>).
- <a id="c11"></a>[11] `doc.go`, Wasm section: "The client side supports
  compiling to Wasm. It wraps the WebSocket browser API." Caveats: "Accept
  always errors out; Conn.Ping is no-op; HTTPClient, HTTPHeader and
  CompressionMode in DialOptions are no-op". `ws_js.go` imports
  `syscall/js`. The repo has `mask_amd64.s`, `mask_arm64.s`, `mask_asm.go`
  (<https://github.com/coder/websocket/blob/master/doc.go>,
  <https://github.com/coder/websocket/blob/master/ws_js.go>,
  <https://github.com/coder/websocket/tree/master>).

### github.com/gorilla/websocket

- <a id="g1"></a>[12] Latest release v1.5.3, published 2024-06-14 (v1.5.2 is
  retracted: "tag accidentally overwritten"). `go.mod`: `go 1.20`,
  `require golang.org/x/net v0.26.0`, used by `proxy.go` for
  `golang.org/x/net/proxy`
  (<https://github.com/gorilla/websocket/releases>,
  <https://github.com/gorilla/websocket/blob/main/go.mod>,
  <https://github.com/gorilla/websocket/blob/main/proxy.go>).
- <a id="g2"></a>[13] Last commit 2025-03-19, "Implements HTTPS proxy
  functionality"; the four commits before it are from 2024-07/08
  (<https://github.com/gorilla/websocket/commits/main>).
- <a id="g3"></a>[14] Commit 76ecc29e (2022-12-09, "archive mode") added to
  the README: "The Gorilla project has been archived, and is no longer
  under active maintainenance"; commit 931041c5 (2023-07-15) removed that
  line (<https://github.com/gorilla/websocket/commit/76ecc29e>,
  <https://github.com/gorilla/websocket/commit/931041c5>). Issue #432
  "Support WebAssembly / GopherJS" is open (updated 2026-09-23)
  (<https://github.com/gorilla/websocket/issues/432>).
- <a id="g4"></a>[15] README Status: "a complete and tested implementation of
  the WebSocket protocol. The package API is stable." Passes the Autobahn
  server tests (<https://github.com/gorilla/websocket/blob/main/README.md>).
- <a id="g5"></a>[16] `conn.go`: "NetConn returns the underlying connection
  that is wrapped by c. Note that writing to or reading from this
  connection directly will corrupt the WebSocket connection." The message
  API is `NextReader`/`NextWriter`/`ReadMessage`/`WriteMessage`; a stream
  adapter is left to the caller
  (<https://github.com/gorilla/websocket/blob/main/conn.go>).
- <a id="g6"></a>[17] `doc.go`, Concurrency: "Connections support one
  concurrent reader and one concurrent writer ... The Close and
  WriteControl methods can be called concurrently with all other methods."
  (<https://github.com/gorilla/websocket/blob/main/doc.go>).
- <a id="g7"></a>[18] `doc.go`, "Compression EXPERIMENTAL": "experimentally
  supported ... in a limited capacity ... Currently this package does not
  support compression with 'context takeover' ... may result in decreased
  performance." (<https://github.com/gorilla/websocket/blob/main/doc.go>).
- <a id="g8"></a>[19] `doc.go`, Control Messages: ping is answered by the
  default ping handler; "If an application sends ping messages, then the
  application should set a pong handler"; "The application must read the
  connection to process close, ping and pong messages", with a suggested
  discard-read loop (<https://github.com/gorilla/websocket/blob/main/doc.go>).
- <a id="g9"></a>[20] `server.go`: `Upgrade` calls
  `http.NewResponseController(w).Hijack()` and writes the 101 itself.
  `client.go`: `Dialer` has `NetDial`, `NetDialContext`, `NetDialTLSContext`,
  `Proxy`; it refuses a `TLSClientConfig.NextProtos` entry other than
  `http/1.1` ("sharing tls.Config with net/http Transport can cause this
  error") (<https://github.com/gorilla/websocket/blob/main/server.go>,
  <https://github.com/gorilla/websocket/blob/main/client.go>).

### github.com/gobwas/ws

- <a id="b1"></a>[21] Latest release v1.5.0, published 2026-11-04, after
  rc.1 (2026-11-02) and rc.2 (2026-11-03); notes list buffer reuse, payload
  allocation caps, wsflate fixes and NEON/SSE2/AVX2 masking. `go.mod`:
  `go 1.26`, `require github.com/gobwas/httphead v0.1.0`; httphead's own
  `go.mod` has no requires
  (<https://github.com/gobwas/ws/releases/tag/v1.5.0>,
  <https://github.com/gobwas/ws/blob/master/go.mod>,
  <https://github.com/gobwas/httphead/blob/master/go.mod>).
- <a id="b2"></a>[22] Last commit 2026-11-03, "ws: mask payloads with NEON and
  SSE2/AVX2 kernels (#235)"; repo pushed 2026-11-04
  (<https://github.com/gobwas/ws/commits/master>).
- <a id="b3"></a>[23] README: "Zero-copy upgrade", "Low-level API which allows
  to build your own logic of packet handling and buffers reuse", with
  `wsutil` as the higher-level layer. `server.go`: `UpgradeHTTP(r, w)`
  returns `(net.Conn, *bufio.ReadWriter, Handshake, error)` after
  `http.NewResponseController(w).Hijack()`; the caller then reads frame
  headers and payloads itself (`ws.ReadHeader`, `ws.Cipher`,
  `ws.WriteHeader`) (<https://github.com/gobwas/ws/blob/master/README.md>,
  <https://github.com/gobwas/ws/blob/master/server.go>).
- <a id="b4"></a>[24] `wsutil/reader.go`: "Note that Reader's methods are not
  goroutine safe." The README documents no concurrency model; it is
  whatever the caller builds on the raw `net.Conn`
  (<https://github.com/gobwas/ws/blob/master/wsutil/reader.go>).
- <a id="b5"></a>[25] README, Compression: `ws/wsflate` "provides
  minimalistic I/O wrappers to be used in conjunction with any deflate
  implementation (for example, the standard library's compress/flate)";
  negotiation via `Upgrader.Negotiate: e.Negotiate`, framing via
  `wsflate.MessageState` with `wsutil` (<https://github.com/gobwas/ws/blob/master/README.md>).
- <a id="b6"></a>[26] `wsutil/handler.go`: `ControlHandler` "contains logic of
  handling control frames ... read the next frame header ... pass it to
  Handle()", which "will read out all control frame payload (if any) and
  write necessary bytes as a rfc compatible response"
  (<https://github.com/gobwas/ws/blob/master/wsutil/handler.go>).
- <a id="b7"></a>[27] `dialer.go`: `Dialer.NetDial` "is used instead of
  net.Dialer"; `TLSClient`/`TLSConfig` wrap the connection; no
  `http.Client` path (<https://github.com/gobwas/ws/blob/master/dialer.go>).
- <a id="b8"></a>[28] Repository root lists `cipher_amd64.s`,
  `cipher_arm64.s`, `cipher_generic.go`, `util_purego.go`, `util_unsafe.go`
  and no `_js.go` file; a code search for `syscall/js` in the repo returns
  nothing (<https://github.com/gobwas/ws/tree/master>).
- Limits: the README warns "The protocol does not limit the size of a frame
  or a message, and neither does this library by default" and asks callers
  to set `wsutil.SetMaxMessageSize`/`SetMaxFrameSize`
  (<https://github.com/gobwas/ws/blob/master/README.md>).

### golang.org/x/net/websocket

- <a id="x1"></a>[31] Module `golang.org/x/net` v0.62.0 tagged 2026-10-27;
  its `go.mod` is `go 1.26.0` and requires `golang.org/x/crypto`,
  `golang.org/x/sys`, `golang.org/x/term`, `golang.org/x/text`. The
  `websocket` package itself imports only the standard library
  (<https://github.com/golang/net/tags>,
  <https://github.com/golang/net/blob/master/go.mod>,
  <https://github.com/golang/net/tree/master/websocket>).
- <a id="x2"></a>[32] Commits touching `websocket/`: 2026-01-06 "add missing
  call to resp.Body.Close", 2025-11-28 printf fix, 2025-03-05 "re-recommend
  gorilla/websocket", 2024-09-25 rename to coder/websocket
  (<https://github.com/golang/net/commits/master/websocket>).
- <a id="x3"></a>[33] Package doc: "This package currently lacks some features
  found in an alternative and more actively maintained WebSocket packages:
  github.com/gorilla/websocket, github.com/coder/websocket"
  (<https://github.com/golang/net/blob/master/websocket/websocket.go>).
  golang/go#18152 "x/net/websocket: remedy neglect; merge with gorilla
  websocket?" is open with `NeedsDecision`
  (<https://github.com/golang/go/issues/18152>).
- <a id="x4"></a>[34] `websocket.go`: "Multiple goroutines may invoke methods
  on a Conn simultaneously." `Conn` has `Read`, `Write`, `Close`,
  `LocalAddr`, `RemoteAddr`, `SetDeadline`, `SetReadDeadline`,
  `SetWriteDeadline`. `Write` sends one frame of `ws.PayloadType` per call;
  `Read` returns frame data; `LocalAddr`/`RemoteAddr` return the Origin or
  Location URL, not a socket address; deadlines fail with "cannot set
  deadline: not using a net.Conn" when the underlying rwc is not one
  (<https://github.com/golang/net/blob/master/websocket/websocket.go>).
- <a id="x5"></a>[35] The union of imports across `client.go`, `dial.go`,
  `hybi.go`, `server.go`, `websocket.go` contains no `compress/flate`, and
  the package exposes no extension negotiation
  (<https://github.com/golang/net/tree/master/websocket>).
- <a id="x6"></a>[36] `hybi.go` `HandleFrame`: on `PingFrame` the handler
  reads the payload and calls `WritePong`; `PongFrame` is discarded. There
  is no exported method to send a ping
  (<https://github.com/golang/net/blob/master/websocket/hybi.go>).
- <a id="x7"></a>[37] `server.go`: `Handler` and `Server` implement
  `http.Handler` (<https://github.com/golang/net/blob/master/websocket/server.go>).
- <a id="x8"></a>[38] Gerrit CL 632899 "websocket: add http2 support using
  extended connect" and CL 632755 "RFC: websocket: support websockets over
  HTTP2" are both `ABANDONED` (2024-12)
  (<https://go-review.googlesource.com/c/net/+/632899>,
  <https://go-review.googlesource.com/c/net/+/632755>).
- <a id="x9"></a>[39] `client.go`: `DialContext` uses `net.Dialer` (or
  `config.Dialer`) and `tls` directly, not `http.Client`
  (<https://github.com/golang/net/blob/master/websocket/client.go>).

### The HTTP/2 question, independent of the library

- `net/http` enables HTTP/2 on HTTPS servers by default
  (<https://github.com/golang/go/blob/master/src/net/http/doc.go>), but a
  WebSocket handshake needs `http.Hijacker`, and the HTTP/2 server states
  "There is currently no plan for StateHijacked or hijacking HTTP/2
  connections"
  (<https://github.com/golang/go/blob/master/src/net/http/internal/http2/server.go>).
  A Go client forces HTTP/1 for a request with `Connection: upgrade` and
  `Upgrade: websocket` (`Request.requiresHTTP1`,
  <https://github.com/golang/go/blob/master/src/net/http/request.go>), so a
  Go CLI against a Go server negotiates HTTP/1.1 for the Tunnel even
  though both sides speak h2 for everything else.
- RFC 8441 (September 2018) runs WebSocket "over a single stream of an
  HTTP/2 connection" using a `:protocol` pseudo-header, which a client may
  use only after receiving `SETTINGS_ENABLE_CONNECT_PROTOCOL = 1`
  (<https://www.rfc-editor.org/rfc/rfc8441.html>).
- Go's HTTP/2 implementation has the server and transport halves of
  extended CONNECT, but `disableExtendedConnectProtocol = true` by default
  ("Issue #71128"), flipped only by `GODEBUG=http2xconnect=1`
  (<https://github.com/golang/go/blob/master/src/net/http/internal/http2/http2.go>,
  <https://github.com/golang/net/blob/master/http2/http2.go>).
  golang/go#71128 "do not enable HTTP/2 extended connect by default" is
  closed for Go 1.24; golang/go#53208 "add support for
  SETTINGS_ENABLE_CONNECT_PROTOCOL" is open on the Backlog; CL 776300
  "net/http: add HTTP2Config.EnableConnectProtocol" is `NEW`
  (updated 2026-10-30) (<https://github.com/golang/go/issues/71128>,
  <https://github.com/golang/go/issues/53208>,
  <https://go-review.googlesource.com/c/go/+/776300>).
- As of Go 1.27, `golang.org/x/net/http2` is a wrapper over `net/http`
  unless built with `-tags http2legacy`; "the source of truth has moved to
  the standard library package net/http/internal/http2"
  (<https://github.com/golang/net/blob/master/http2/README.md>,
  <https://github.com/golang/net/blob/master/http2/transport_wrap.go>).
- The Go 1.24 and 1.27 release notes do not mention WebSocket, RFC 8441 or
  `http2xconnect`; 1.24 added `Server.Protocols`/`Transport.Protocols` with
  `UnencryptedHTTP2` (prior knowledge only, "Upgrade: h2c" not supported)
  (<https://go.dev/doc/go1.24>, <https://go.dev/doc/go1.27>).

### Proxies and CDNs: what the Tunnel must do regardless of library

- nginx: "By default, the connection will be closed if the proxied server
  does not transmit any data within 60 seconds ... Alternatively, the
  proxied server can be configured to periodically send WebSocket ping
  frames"; `Upgrade` and `Connection` are hop-by-hop and must be set
  explicitly (<https://nginx.org/en/docs/http/websocket.html>).
- AWS ALB: `idle_timeout.timeout_seconds` defaults to 60
  (<https://docs.aws.amazon.com/elasticloadbalancing/latest/application/application-load-balancers.html>).
- Cloudflare: "will close a WebSocket connection when no data is
  transmitted in either direction for a period of time" and "may restart
  servers, which terminates WebSockets connections"; it recommends
  ping/pong heartbeats (<https://developers.cloudflare.com/network/websockets/>).
- So the Tunnel needs an application-level heartbeat under 60 s and
  reconnect logic, whichever library is chosen. SSH's own
  `ServerAliveInterval`-style keepalive counts as data for these proxies;
  WebSocket pings are the fallback when the SSH stream is idle. In a
  browser, `Ping` is unavailable (coder's `Ping` is a no-op on Wasm), so a
  browser client would need an in-band keepalive message.

## What the choice costs

- One third-party module, `github.com/coder/websocket`, with no transitive
  dependencies. It replaces nothing from the standard library; `x/net`
  would otherwise come in only for a package whose own doc discourages it.
- `NetConn` closes the whole connection when a deadline fires during an
  active read or write, unlike a TCP `net.Conn`
  ([[4]](#c4)). The Tunnel's SSH layer should use context cancellation and
  heartbeats rather than per-call deadlines on the adapter.
- `NetConn` sets the read limit to -1 ([[4]](#c4)); message size then
  depends on how the SSH layer writes. Each `Write` becomes one WebSocket
  message, so large SSH packets become large frames; benchmark before
  enabling compression ("Do not enable compression without benchmarking",
  [[6]](#c6)). SSH traffic is already encrypted, so permessage-deflate is
  unlikely to help and should stay off.
- No shipped heartbeat helper ([[7]](#c7)); the Tunnel owns a small ping
  loop that runs beside the reader.
- HTTP/2 Tunnels wait on both coder PR #539 and the Go toolchain's gate.
  Until then every Tunnel is an HTTP/1.1 Upgrade on the HTTPS port, which
  is what intermediaries accept today; the transport interface from
  ADR 0004 is where an HTTP/2 or QUIC Tunnel would land later.
- A browser client reuses the same package built for `GOOS=js GOARCH=wasm`
  with `Accept`, `Ping` and compression as no-ops ([[11]](#c11)).

## Open questions

- Coder PR #539 (HTTP/2) is open; whether and when it lands, and under what
  API, is not settled. Re-check when Go ships `HTTP2Config.EnableConnectProtocol`
  (CL 776300) or closes golang/go#53208.
- PR #539 states that `net/http`'s own client "rejects the :protocol
  pseudo-header"; this note did not verify that in the `net/http` source.
- Performance was not measured. coder's README claims faster masking than
  gorilla and gobwas v1.5.0 added SIMD masking; neither matters until a
  Tunnel benchmark shows masking on the profile.
- The exact Cloudflare idle timeout is not published on the page cited;
  only "a period of time" and Enterprise overrides. Treat 60 s (nginx, ALB)
  as the ceiling for the heartbeat interval.
- Whether the koko server will sit behind a proxy that terminates TLS and
  re-opens HTTP/1.1 to the origin (nginx's documented setup) or pass the
  TLS stream through; the first makes the HTTP/2 question moot at the
  origin, the second keeps it live for later.
