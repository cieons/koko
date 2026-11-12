# A browser opens SSH Sessions through a WebSocket to the Gateway

The web UI has a web terminal: a page that opens an SSH Session to one Host
in the browser, on a desktop and on a phone, the Owner's choice of
2026-11-12. A browser signed in with a Password may open SSH Sessions by the
Space membership rules a device follows (ADR 0011), and so may a page
opened through `koko web`, as its device's Device Session. The page opens a
WebSocket at `/terminal` on the web listener, or on `web@koko`. The web
front checks its Origin and its cookie and hands it to the Gateway, which
admits the SSH Session as it admits a Tunnel's, dials the Host with the koko
key, and holds a pty and a shell there. The koko key and the Host Account
never leave the koko server (ADR 0002).

The WebSocket carries koko's resumable stream (ADR 0021), with an end in the
page, and inside it records of terminal data, resizes and the shell's exit.
When the WebSocket drops, the SSH Session is Detached for 15 minutes, against
a device's 1 minute, and the page Resumes it byte-exactly on a new WebSocket
while the page stays open. The terminal is one page of tabs, each its own
SSH Session, to the same Host or to different ones, which stays open while
the person moves about the web UI. A Sign-in holds at most 16 SSH Sessions
at once, attached or Detached, across all its tabs. Files move over SFTP on the same Host connection,
through the web front's own endpoints beside the terminal: a directory
listing, uploads in chunks, and downloads through a one-time token. Each
file moved is logged with its path, size and outcome.

This supersedes ADR 0043's "never reaches a Host through the Gateway". It
refines ADR 0021: the browser's leg Resumes too, with a grace of its own. It
fulfils ADR 0042's "where a browser terminal can later come". ADR 0006, as
ADR 0042 refined it, and ADR 0015 stand: the terminal and its files are the
web front's own endpoints, as `/auth/` is, for a person in a browser, not
API operations. ADR 0013's principle holds: SSH `none` is taken inside a
Tunnel because the connection beneath already says who is on it, and here
the cookie checked at the handshake says it, with no inner SSH.

## Considered Options

- **The web front as an SSH client of the Gateway:** the Gateway would not
  change, but a second SSH handshake and encryption would run inside one
  process, and the WebSocket's leg would need a second resumable stream.
- **SSH in the browser, compiled to WebAssembly:** the Gateway would not
  change and SFTP would come with it, but it costs several MB against the
  web UI's 3 MiB budget, a second SSH client to keep in step, and gains
  nothing, since the inner SSH is `none` authentication inside TLS.
- **Server-sent events down and `POST` up:** no new dependency, but proxies
  buffer them, and each keystroke would be a call that reads the Sign-in.
- **Only through `koko web`:** no ADR would change, but a phone would get no
  terminal.
- **A setting the Owner turns on, or a second factor first:** each guards a
  password's weakness, at the cost of one more setting, or of building TOTP
  before the terminal.
- **A grace of 1 minute, or of 1 hour:** a locked phone usually outlasts the
  first; the second holds a Host connection and up to 4 MiB of buffers per
  SSH Session for an hour.
- **Files over the WebSocket:** one connection, but a large file would wait
  in the stream's 2 MiB beside the shell's output, and a download would have
  to be assembled in the page's memory.
- **A path field for files:** less to build, but paths are hard to type on a
  phone. The Owner chose a listing.
- **One terminal per page, several terminals as several browser tabs:**
  less to build, but on a phone switching browser tabs is slow, and each
  would load the emulator again. The Owner chose tabs within the page.
- **64 SSH Sessions per Sign-in, as a Device Session has:** one rule, but
  four times the worst case, and a browser's are held fifteen times longer.

## Consequences

- A password, unlike a device's key, can be guessed or phished, and it now
  reaches Hosts. ADR 0043's guards stand: the length rule, the sealed
  Argon2id hash and the sign-in limits. TOTP fits beside the Password later.
- Every way a Sign-in ends now ends its SSH Sessions at once, Detached ones
  included: sign-out, revoke, a Password set, changed or removed, the User
  Removed. A Sign-in's expiry, which no call publishes, is found by a sweep
  every minute. A Space change rechecks a Sign-in's SSH Sessions as it
  rechecks a Device Session's, and a Web URL change ends every SSH Session
  that came through the web listener.
- The web listener's Origin check, until now on every `POST`, also covers
  the terminal's `GET` handshake, the defence against cross-site WebSocket
  hijacking. Refusals other than a bad Origin travel in the WebSocket's
  first answer, since a browser cannot read a refused upgrade.
- A page cannot see WebSocket pings, so both ends send an
  acknowledgement-only frame after 15 s of silence and take the connection
  for dead after 45 s, which also keeps a proxy from cutting a quiet
  terminal. nginx needs its WebSocket lines; the install guide gives them.
- The page and the koko server are one build, so the terminal's protocol
  has no window: a page that names another version is told to reload.
  `tunnel@koko` and `web@koko` keep their revisions, since `koko web`
  carries the WebSocket's bytes as they are.
- A reload, or a browser tab the system discarded, loses the page's state.
  The page keeps its tabs' SSH Session ids in `sessionStorage`, so the new
  page first ends the old SSH Sessions, then opens each tab's Host again as
  a new one. Replaying a screen after a reload is later; tmux on the Host
  covers it.
- A browser tab duplicated from a live one starts with a copy of its
  `sessionStorage`, ids and all, and must leave that one's SSH Sessions
  alone (the router's rule, 2026-11-12). Each document holds a Web Lock
  named after each SSH Session it holds, which the browser lets go with
  the document, so a new document ends only the kept ids no live document
  holds, and forgets the rest. A browser that cannot tell, having no Web
  Locks, ends none, and the Gateway ends them at the grace.
- koko gains two dependencies, `github.com/coder/websocket` and
  `github.com/pkg/sftp`, each the standard choice for what it does in Go.
