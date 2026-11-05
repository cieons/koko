# The web UI's cookie lasts until its Device Session is Revoked

`koko web` listens on `127.0.0.1`, port 5656 by default, and forwards each
browser connection into a `web@koko` channel on the device's Device
Connection (ADR 0015). The first visit carries a one-time token, which
`koko web` prints and opens. The koko server trades it for a cookie bound to
the Device Session that `koko web` runs as, and checks that cookie on every
`web@koko` channel. The cookie lasts until that Device Session is Revoked, so
a bookmarked `http://localhost:5656` page opens with no token whenever
`koko web` runs, and an open tab outlives a restart of `koko web`. Over the
Unix socket there is no Device Session to revoke, and the Local caller acts
as the Owner (ADR 0008), so there the cookie lasts only as long as that run of
`koko web`.

## Considered Options

- **A cookie for each run of `koko web`**: a leaked cookie dies with the run,
  but every run needs the token link again, and every restart kills the open
  tabs.
- **A cookie for a fixed time, such as 12 hours**: a leak is bounded, but the
  koko server keeps a record for each browser, and tabs still die at an hour
  nobody chose.
- **A random port for each run**: never already taken, but no bookmark works.

## Consequences

- The cookie is a long-lived credential in the browser's cookie store, as the
  device's key is a long-lived credential in its files. The web UI has no
  sign-out in P0. Only revoking the Device Session ends the cookie. Neither a
  dropped and reconnected Device Connection nor a koko server restart ends it.
- Browsers send `localhost` cookies to every port, so the cookie also reaches
  any other server the browser visits on `localhost`. A process of the same
  OS user can read the device's key anyway (ADR 0017). Another OS user's
  local server can take the cookie and replay it through this user's
  `koko web` while it runs. That is the accepted cost on a device that OS
  users share.
- The cookie is `HttpOnly` and `SameSite=Strict`. The koko server answers
  `web@koko` traffic only when the Host header is `localhost:<port>` or
  `127.0.0.1:<port>`, which defeats DNS rebinding. The API takes only a JSON
  `POST` and sends no CORS headers, so another page's request fails its
  preflight.
- The token shows in the process list for about a second while `koko web`
  opens the browser, as a Join Code in `koko join <code>` does (ADR 0019). If
  another OS user spends the token first, the user's own browser is refused
  and says why.
- `koko web` never listens beyond loopback. When its port is taken, it
  refuses to start and names the process holding the port, instead of moving
  to another port and breaking the bookmark.
- The key the koko server signs cookies with is sealed with the Root Key
  (ADR 0018).
