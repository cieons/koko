# The Gateway, the Tunnel, `koko exec` and `koko tunnel`

The Gateway ends every SSH Session and logs in to the Host with the koko
key, which never leaves the koko server
([ADR 0002](../adr/0002-every-ssh-session-ends-at-the-gateway.md)). Each SSH
Session of the CLI's rides in a Tunnel: one `tunnel@koko` channel carrying
a [resumable stream](resumable-stream.md)
([ADR 0021](../adr/0021-an-ssh-session-survives-a-network-drop-by-resuming-its-tunnel.md)).
A [web terminal](#the-web-terminal)'s SSH Session rides in no Tunnel: the
Gateway holds it for its User's Workspace, and each page attached to it has
a resumable stream of its own on a WebSocket. The Gateway is
[`internal/server/gateway`](../../internal/server/gateway) (server-only); the device end of a Tunnel is
[`internal/client/tunnel`](../../internal/client/tunnel/tunnel.go)
(client-only); what both ends agree on is
[`wire/tunnel.go`](../../internal/shared/wire/tunnel.go) (shared). The design
is P0 spec: reach, "The Tunnel on the wire", "The Gateway" and "`koko exec`".

## The Tunnel on the wire

- **The channel.** The CLI opens a `tunnel@koko` channel
  (`wire.ChannelTunnel`) on its connection to the koko server: the Unix
  socket or a Device Connection, which hands it to the Gateway
  ([the API](api.md#the-way-in)).
- **The open payload** is in SSH's wire encoding: an SSH string, the kind,
  then what the kind carries. `open` (`wire.TunnelOpen`) carries one SSH
  string, the SSH Name as typed; the Gateway drops a trailing `.koko`
  (`wire.TunnelHost`), so `web1` and `web1.koko` name the same Host. For
  example, `00 00 00 04 "open" 00 00 00 09 "web1.koko"`. `resume`
  (`wire.TunnelResume`) carries the Tunnel id's 16 bytes as they are, then
  the offset the device has received of the stream, the id's 16 bytes
  included, as a uint64: `00 00 00 06 "resume"`, 16 bytes, then 8 bytes,
  big-endian. A payload that is no kind, an `open` with anything but one
  SSH Name, a `resume` with anything but an id and an offset, and any other
  kind are refused at the channel open, with reason `connect failed`.
- **The revision.** This is revision 1 of `tunnel@koko`
  (`wire.TunnelRevision`), which also covers the
  [resumable stream](resumable-stream.md)'s frames. The device opens a
  Tunnel at the lower of its own revision and the one the koko server
  reports (`wire.Report.ChannelRevision`), and Resumes it at the revision
  it opened at. From revision 2 a payload ends with its revision as a
  `uint32`; revision 1 carries none, which every koko server takes
  ([API](api.md#revisions)).
- **The Tunnel id.** The Gateway picks 128 random bits for each Tunnel it
  accepts and sends them as the first 16 bytes of the stream
  (`wire.TunnelIDSize`), so in its first frame, before the SSH Session's
  first byte. The device reads exactly those bytes, then runs its SSH
  client on the rest. Logs write the id as 32 lowercase hex characters.
- **Inside the stream** is one SSH connection to the Gateway's own SSH
  server: the version string `SSH-2.0-koko`, AES-GCM ciphers only
  (`wire.TunnelCiphers`), the koko server's sealed host key, the one Join
  Codes pin, and `none` authentication. koko's own clients ask for the user
  name `koko` (`wire.TunnelUser`).
- **The close frame's reason**, when the Gateway closes a Tunnel, is empty
  or the notice of [how the SSH Session ended](#when-an-ssh-session-ends):
  one sentence for the person, without `koko: `, such as `the koko server
  is restarting; this SSH Session has ended.`; `koko exec` prints it as its
  error when the SSH Session's stderr did not show it.

## Opening an SSH Session

For each `tunnel@koko` channel ([`Gateway.Open`](../../internal/server/gateway/gateway.go)):

1. Before initialisation the koko server has no host key, so the channel is
   refused, as is any channel while the koko server stops.
2. In one read-only transaction the Gateway opens the host key and the koko
   key with the Root Key and asks the access check for the Host by name
   ([`host.ReachOf`](../../internal/server/host/reach.go)). A new SSH Session
   is allowed when the caller may act in the Host's Space and the Host and
   every [Relay](#relay) of its chain are Managed.
3. It accepts the channel, puts the Tunnel in the registry under a new id,
   and sends the id. An allowed SSH Session counts against its caller's
   limit, by the caller's id (`callerID`), attached or Detached, from then
   until its Tunnel ends: a Device Session holds 64 Tunnels at once
   (`maxSSHSessions`); one more is refused, and dials no Host. The Local
   caller has no limit. A [web terminal](#the-web-terminal)'s SSH Sessions
   count against their Workspace's 16 tabs instead, whoever opened them.
4. When the SSH Session is allowed, it dials the Host through the
   [Host connector](host-connector.md), as the Host Account with the koko key
   and the Host's pinned host key, through its chain of Relays, while the
   client's SSH handshake runs.
5. The client's `none` takes the SSH user name `koko`, which koko's
   ssh_config and `koko exec` ask for, or the Host Account's own name, and
   no other, so a later release may give other names a meaning. It then
   waits for the dial. Every refusal is a banner on the refused `none`,
   which `koko exec` prints as its error and ssh in its own output, checked
   in this order:

| When | Banner |
| --- | --- |
| No Host the caller can see has the name: none has it, it is in a Space the caller cannot act in, or the SSH Name is not a Host name. A Retired Host holds no name. | `koko: no Host is named "web1".` |
| The Host is Registered | `koko: web1 is Registered: koko logs in to it only once it is Enrolled, with koko host enroll web1.` |
| A Relay of its chain is not Managed, the one nearest the koko server named | `koko: web1's Relay gw is Registered: koko passes through it only once it is Enrolled, with koko host enroll gw.` |
| Its chain of Relays is broken in the record ([a Relay](record.md#a-relay)) | `koko: koko could not connect to web1; this SSH Session did not start.` |
| The access check refuses the caller, such as a Revoked Device Session | `koko: ` and the refusal's message |
| The Device Session has 64 SSH Sessions open | `koko: this Device Session already has 64 SSH Sessions open, the most koko allows; this SSH Session did not start.` |
| Another user name, such as `root@web1.koko` | `koko: web1 is reached as its Host Account "deploy"; leave out "root@".` |
| The Host is down: the connector's 3 attempts each met a network error | `koko: web1 is down or unreachable: koko could not connect to it; this SSH Session did not start.` |
| The Host's host key is not the pinned one | `koko: the host key of web1 does not match the one pinned at Enroll (pinned SHA256:…, offered SHA256:…); koko did not log in. If web1 was rebuilt, pin its new key with koko host enroll web1 --host-key <the fingerprint its provider shows>, or with --replace-host-key, which trusts whatever key it offers.` |
| The Host refused the koko key | `koko: web1 refused the koko key for its Host Account "deploy"; this SSH Session did not start.` |
| The connector failed otherwise | `koko: koko could not connect to web1; this SSH Session did not start.` |

A failure on the leg to a Relay of the Host's chain gets the Host's banner
for the same failure, with that Relay in the Host's place ([Relay](#relay)).

The first row is the same byte for byte for a name nobody holds and for a
Host the caller cannot see, whatever user name they ask for, so the Gateway
tells nothing of Hosts outside the caller's Spaces; the user name is
checked only for a Host they may reach. The connector's error, which names
addresses, goes to the log only. Only a network error is retried
([the Host connector](host-connector.md#retries-and-failures)), so a host
key mismatch or a refused key costs one connection.

## Relay

A Host may have a [Relay](record.md#a-relay), a Managed Host the Gateway
passes through to reach it, and a Relay may have a Relay of its own, so a
Host's chain may hold up to 4 ([ADR 0055](../adr/0055-a-relay-may-have-a-relay-of-its-own.md)).
The Host connector logs in to each Relay of the chain in turn, as its Host
Account with the koko key, checking its pin, each through a `direct-tcpip`
channel on the one before, then reaches the Host through one on the last
([the Host connector](host-connector.md#connecting)). Each SSH Session opens
its own connection at every hop. The caller need not see any Relay of the
chain. A message names a Relay as `web1's Relay gw` only to a caller who can
follow the links to it, seeing it and every Relay between it and the Host,
and as `web1's Relay` to anyone else
([`host.Relay.Of`](../../internal/server/host/reach.go)), who learns
neither its fingerprints, its Host Account nor its place in the chain. A
Member who sees app1 and web1 but not db1, app1's own Relay, reads
`app1's Relay` for web1 too: the record never told them app1's chain holds
it. Each failure below is the Relay's at the hop that failed
([`RelayError.Hop`](host-connector.md#connecting)):

| When | Banner, to a caller who can follow the links to the Relay | To anyone else |
| --- | --- | --- |
| The Relay is down | `koko: web1's Relay gw is down or unreachable: koko could not connect to it; this SSH Session did not start.` | the same, `web1's Relay` |
| Its host key is not the pinned one | `koko: the host key of web1's Relay gw does not match the one pinned at Enroll (pinned SHA256:…, offered SHA256:…); koko did not log in. If gw was rebuilt, pin its new key with koko host enroll gw --host-key <…>, or with --replace-host-key, which trusts whatever key it offers.` | `koko: the host key of web1's Relay does not match the one pinned at Enroll; koko did not log in.` |
| It refused the koko key | `koko: web1's Relay gw refused the koko key for its Host Account "deploy"; this SSH Session did not start.` | `koko: web1's Relay refused the koko key; this SSH Session did not start.` |
| The connector failed otherwise | `koko: koko could not connect to web1's Relay gw; this SSH Session did not start.` | the same, `web1's Relay` |

Each is logged as `ssh session refused` at warning level with `relay`,
`relay_id` and `hop`, the Relay's place in the chain from 1, and the reason
`relay unreachable`, `relay host key mismatch` (with `pinned` and
`offered`), `relay login refused` or `relay connection failed`. A refused
`direct-tcpip` open on a Relay is the next hop's network error, so a failure
past the last Relay, such as the Host refusing the channel's connection, is
the Host's own. Each Relay's connection has the Host's probes, retries and
ends ([when an SSH Session ends](#when-an-ssh-session-ends)), and the whole
dial is bounded by `timing.HostConnect` (45 s). A chain the record holds
broken is refused with `host_connection_failed`'s banner and logged at
warning level as `relay chain broken`, with `read_relay_ids`, the ids read.

## Relaying

Once the client is in ([`relay.go`](../../internal/server/gateway/relay.go)),
each channel it opens is opened on the Host with the same type and payload,
or refused with the Host's own reason. The two channels are relayed both
ways: data and extended data (stderr), the EOF that half-closes a
direction, and each request with the reply the other side gives, such as
`exec`, `shell`, `subsystem`, `env`, `pty-req`, `window-change`, `signal`,
`exit-status` and `exit-signal`. So `-L` and `-D` (`direct-tcpip`) and `-L`
to a Unix socket (`direct-streamlocal@openssh.com`) reach the Host as any
channel does, and the Host's sshd policy decides. A channel closes once its
twin has closed, but only once a request it asked, which the other
direction is passing on, has its answer: an exec the Host answered just
before a command that ended at once closed its channel still gets its
answer, which would be lost after the close.

Agent forwarding (`auth-agent-req@openssh.com`, P2) and X11 forwarding
(`x11-req`, which koko leaves out) are refused on a session channel by the
Gateway itself (`refusedRequests`), so the Host never sees them, each with
one line on that session channel's stderr: `koko: agent forwarding is not available
through koko.` and `koko: X11 forwarding is not available through koko.`
ssh asks for an agent without wanting a reply, so the line is all it shows.
The session carries on. Such a line, and an ending's notice
([below](#when-an-ssh-session-ends)), take turns with what the Host sends
to the same stderr and with the EOF passed on from the Host
(`sessionChannel`), and none follows that EOF.

`-R` takes a global request and a channel the Host opens. The Gateway
passes the client's `tcpip-forward`, `streamlocal-forward@openssh.com` and
their cancels (`forwardRequests`) on to the Host, in order, and answers each
with the Host's reply, such as the port the Host chose for port 0. It opens
on the client each `forwarded-tcpip` and `forwarded-streamlocal@openssh.com`
channel the Host opens (`forwardedChannels`), with its payload, and relays
the two as above, or refuses the Host's with the client's reason. It
refuses every other global request of the client's itself, which answers
OpenSSH's `keepalive@openssh.com`: the Host never sees the client's
keepalives. The Host's client refuses the Host's global requests and every
other channel the Host opens. The Host's own `keepalive@openssh.com`, which
an sshd that sets `ClientAliveInterval` sends a quiet client, wanting a
reply, on an open channel if it has one (`hostKeepalive`), the Gateway
refuses on that channel itself, never passing it to the client: sshd takes
any reply as the answer, and a Detached client could not give one before
sshd gave up on it. On the connection, the Host's client refuses it as any
global request of the Host's.

## The web terminal

A second way into the Gateway beside `Open`
([`workspace.go`](../../internal/server/gateway/workspace.go),
[`session.go`](../../internal/server/gateway/session.go),
[`terminal.go`](../../internal/server/gateway/terminal.go);
[ADR 0053](../adr/0053-a-browser-opens-ssh-sessions-through-a-websocket-to-the-gateway.md),
refined by
[ADR 0057](../adr/0057-a-users-web-terminal-tabs-are-one-workspace-held-by-the-koko-server.md);
P1 spec: a terminal in the web UI, and spec #213). Each User has one
Workspace: the web terminal's tabs, the same in every page of the web UI
the User has open. The Gateway holds each tab's SSH Session apart from any
page, and pages attach to it. The web front reads a page's first message
on one of the web terminal's two WebSockets
([the web front](web-front.md#the-web-terminals-websocket)) and hands the
WebSocket over, `/terminal/workspace` as a `WorkspaceSocket` and
`/terminal/socket` as a `TerminalSocket`, with the caller
(`TerminalCaller`): the Sign-in, or on `web@koko` the Device Session or the
Local caller and its run of `koko web`, and the page's address. A page's
Workspace is its caller's User's: a Device Session's User's through `koko
web`, and the Owner's for the Local caller.

- **The same registry.** A web terminal's SSH Session is carried by no
  Tunnel, but the registry holds it as one, of the client `browser`, with
  its id, its opener as the caller, its admission, Host id, the ids of its
  chain and ending, so `Close`, `Withdraw`, `recheck` and `keepEnding` serve
  it unchanged and its log lines name it by `tunnel`. It has no stream of
  its own and counts against no caller's limit: a Workspace holds at most
  16 tabs (`maxTabs`), so a User holds at most 16 web terminal SSH Sessions,
  however many pages they have open.
- **The Workspaces** (`Workspaces`) are the one reader and writer of the
  table `workspace_tabs` ([the store](store.md#migrations)): each tab's id,
  Host id, label and place, who opened its SSH Session and when, and how
  its last one ended. Shells, screens and scrollback live in memory alone.
  A User's tabs are read when their first page after a start opens its
  Workspace socket. A Workspace applies its pages' requests one at a time,
  in the order they arrive from any page: each change is one transaction,
  then the whole Workspace to every page of the User, the asking page's
  answer after it. A tab's id is 128 random bits in lowercase hex and
  outlives the SSH Sessions opened in it, each of which has an id of its
  own. Its label is the Host's name when it was opened, or the lowest
  `web1 (2)` from 2 that no tab holds, and stays when the Host is renamed.
  A new tab goes at the end, and a closed tab's place closes up.
  `Workspaces.Delete` deletes a User's tabs inside the caller's
  transaction, offered for `user.remove` to call (#216); nothing calls it
  yet, so only a User deleted outright takes their tabs with them.
- **The Workspace socket** (`ServeWorkspace`) sends the page its Workspace
  first, then again after every change, whole: each tab's `id`, `host` (the
  Host's name now, read as the message is sent, so a renamed Host's tab
  reopens to the same Host, or the label for a Host the User can no longer
  see), `label`, `state`, `opened_at`, and `session`, its SSH Session's id,
  while it runs. A tab is `starting`, `running`, `ended` with how
  (`reason`, `notice`, `exit_status` or `exit_signal`), `refused` with the
  `code` and `message` an open would get, or `restarted`. The page's
  requests ([the web front](web-front.md#the-workspace-socket)):
  - **`open`** admits an SSH Session to the named Host as a Tunnel's is
    (`admit`, the same refusals in the same order). A refusal there answers
    the request and adds no tab; the 17th tab is refused so, with
    `ssh_session_limit`, `this Workspace holds 16 tabs; close one first`.
    Otherwise the tab is added, `starting`, and the request answered with
    its id. The Gateway then dials the Host through the Host connector,
    opens one `session` channel and sends a `pty-req` (`TERM`
    `xterm-256color`, the request's columns and rows, pixel sizes 0, and
    the mode `IUTF8`, opcode 42, so a line editor erases a whole Chinese
    character) and a `shell`, and the tab is `running`. A refusal of the
    Host, its chain or its shell leaves the tab `refused`, with the code
    the page tests and the banner a Tunnel's client gets, without `koko: `
    and the newline, so an unseen Host gets the bytes of an unknown name.
    The Host refusing the session, its `pty-req` or its `shell` (its sshd
    policy) is `shell_refused`: `web1 refused a shell with a terminal for
    its Host Account "deploy"; this SSH Session did not start.`
  - **`reopen`** opens a new SSH Session, as the asking page's caller's, in
    a tab that is `ended`, `refused` or `restarted`, to its Host by id. A
    tab `starting` or `running` is answered as it is, so two pages that
    reopen it at once open one SSH Session. A Host the User can no longer
    see is refused with `unknown_host`, and a tab that is gone with
    `unknown_tab`; a reopen the Host or its chain refuses leaves the tab
    `refused`, while one refused for the asking caller's own sake
    (`access_refused`) or for the koko server's failure answers that page
    alone.
  - **Each request acts for its caller as it is now.** The Gateway reads
    who the page is again before it applies a request; a caller that has
    ended, such as a revoked Sign-in, or that is no longer the Workspace's
    User, changes nothing, and its socket is closed, so the page learns
    why as it reconnects.
  - **`close`** removes the tab and ends its SSH Session (`tab closed`). A
    tab already gone is answered the same, so two pages closing one tab
    make one change and get two answers.
  - **Liveness.** The Gateway sends `{}` after `timing.ServerProbe` (15 s)
    without sending, and cuts the socket after `timing.ServerDead` (45 s)
    without a message from the page. A page that leaves 64 messages unread
    (`pageQueueLimit`) is cut, and gets the Workspace whole once it
    reconnects. A message that is no request closes the socket with status
    1008.
- **The SSH Session's loop** (`termSession.loop`). One goroutine per SSH
  Session owns its emulator, a [`vt.Terminal`](vt.md) at the pty's size.
  It reads the Host's stdout and stderr, merged as a pty merges them,
  writes them into the emulator, hands the emulator's replies to the
  shell's stdin, and appends the output to each attached page's queue
  without waiting for it. In turn with the output it takes attaches,
  detaches, the pages' records, a page fallen behind and the shell's end.
  It never waits for a page, the Host's stdin or the database. Data
  records from every page go to the shell's stdin in the order they reach
  the loop, through a queue of their own; once 64 KiB wait there
  (`stdinLimit`), the loop takes no more of the pages' records until the
  shell reads, as SSH's window holds back a client.
  - **Queries.** The koko server answers the shell's queries of the
    terminal: device attributes, the cursor's place, mode reports, OSC 10
    and 11 with the active page's colours, and `CSI 14 t` and `CSI 18 t`
    with the pty's cells and pixel sizes of 0. Each is answered once,
    whether several pages are attached or none.
  - **The Host's requests** on the channel are refused, its
    `keepalive@openssh.com` among them, but for `exit-status` and
    `exit-signal`, which become records after the last output.
  - **After a restart**, a reopened tab's SSH Session writes `koko: the
    koko server restarted; this is a new SSH Session.` before the shell's
    output.
  - Until the shell has started, the SSH Session's end closes the Host
    connection, so a Host that answers neither the session's open nor its
    requests holds nothing.
- **Attaching** (`AttachTerminal`). A page attaches to a running SSH Session
  by its id, with its box in cells; any live caller of the SSH Session's
  User may. An id that names no live SSH Session of that User is refused
  with `unknown_tunnel`, and one that has ended with `ssh_session_ended`.
  The Gateway answers with the attachment's id, starts the attachment's
  own resumable stream on the WebSocket's binary messages, carrying
  terminal records ([`wire/terminal.go`](../../internal/shared/wire/terminal.go),
  [the records](web-front.md#the-terminal-socket)), and hands it to the
  loop. The loop, in one step, queues a `reset` at the pty's size and the
  snapshot as data records, and registers the attachment, so no output
  falls between the snapshot and the live output or arrives twice. Every
  reset and snapshot ends with an `active` record telling the page whether
  it is the active page. At most 8 pages are attached to one SSH Session
  (`maxAttachments`): the 9th detaches the page least recently active in
  it, which gets a `displaced` record before its stream closes.
- **Fan-out.** Each attachment has a writer goroutine of its own that
  drains its queue into its stream. Output past 256 KiB waiting in one
  queue (`behindLimit`) marks the page behind: its queue is dropped, and
  once its writer has caught up, the loop hands it a `reset` and a fresh
  snapshot, which `behindLimit` does not count. A slow page so gets the
  screen as it is now, and the shell and the other pages never wait for
  it. A snapshot is encoded once and shared by every page a reset goes
  to, the `active` record apart, and a new one takes the place of any
  still queued; while pages hold one, queued or being written, the
  budget counts its bytes with the emulator's: at 500×200 one is tens of
  MiB.
- **The pty's size.** The pty starts at the size the `open` or `reopen`
  asked for. A page becomes the SSH Session's active page when it sends
  data or an `active` record; the first page of the opener's caller to
  attach is the first. The pty takes the active page's box, the attach's
  columns and rows until a `resize` replaces them, held to `vt.MaxCols` by
  `vt.MaxRows` (500×200, the spec's `maxGrid`), as a `window-change` on
  the Host, pixel sizes 0, and the emulator's resize;
  every page then gets a `reset` and a fresh snapshot at the new size, so
  no page reflows by itself. The loop sizes the pty once the pages'
  records already waiting are taken, so a burst of resizes takes the
  last alone, and the `window-change` waits in a place of its own ahead
  of the keys waiting for a shell that reads nothing, the last size
  replacing any not yet sent. Another page's `resize` changes nothing until
  that page becomes active. Attaching or resuming makes no page active,
  and a page that detaches leaves the size as it was. The `active` record
  tells the page that becomes active, and the one that stops being so. A
  page's `colors` record gives its theme, which the emulator answers OSC
  10 and 11 with while that page is active.
- **Detaching.** A page detaches when its stream half-closes or closes
  (`page left`), when its stream stays off its WebSocket for the grace
  (`page left`), when it sends bytes that are no record or a record only
  the Gateway sends (`malformed record`, the stream's close saying why), or
  when a 9th page displaces it (`displaced`). The SSH Session goes on with
  no page, and its shell's stdin stays open, since other pages share it.
- **Resuming an attachment** (`ResumeTerminal`). A WebSocket that drops
  leaves its attachment for `timing.Grace` (1 minute), within which the
  page resumes it byte for byte, as a device Resumes a Tunnel
  ([below](#detach-and-resume)). A resume is taken only from the caller who
  made the attachment, the same Sign-in, Device Session or run of `koko
  web`, and only once `recheckResume` passes; the Gateway answers with its
  received offset in the answer, not in the stream
  (`resumable.Stream.AcceptResumeAnswering`). The last attach wins. After
  the grace the attachment is gone: its resume is refused with
  `resume_refused` and an empty message, and the page attaches again. An
  attachment whose SSH Session ended with a notice is refused saying why,
  as long as that ending is kept. An attachment's id on `tunnel@koko`, or
  a Tunnel's id on the terminal socket, is refused as an unknown id.
- **Liveness** (`watchSocket`). A page can neither send nor see a
  WebSocket's pings, so liveness is in the stream. The Gateway reads when
  the page's last byte arrived from a `liveness.Conn` around the
  WebSocket, and notes when the stream last sent: after `timing.ServerProbe`
  (15 s) without sending it asks the stream for an acknowledgement-only
  frame (`resumable.Stream.Probe`), and after `timing.ServerDead` (45 s)
  without a byte from the page it cuts the WebSocket, which starts the
  attachment's grace. The page does the same with the same values. The
  15-second traffic also keeps nginx, which cuts a quiet connection at 60 s
  by default, from cutting a terminal.
- **Endings.** A web terminal's SSH Session ends when a page closes its
  tab, its shell ends, it has had no page attached for
  `timing.WorkspaceIdle` (7 days), counted from its last page's detach or
  from its start (`idle`), its access is withdrawn, its emulator cannot be
  made anew, or the koko server stops
  ([when an SSH Session ends](#when-an-ssh-session-ends)). An ending never
  goes through the loop, which may be inside a call into the emulator, and
  waits for no writer: each attachment gets its last records, the exit
  records or the notice as a data line, `\r\nkoko: <notice>\r\n`, then its
  stream closes, with the notice as its reason; a stream off its WebSocket
  is closed at once. The close releases a writer blocked on a stalled
  page's full buffer. The tab then keeps how its SSH Session ended, which
  every page shows, until a page closes or reopens it. A caller's own end
  (signing out, a Sign-in's expiry or revocation, a Device Session's
  revocation, a run of `koko web` ending) ends the SSH Sessions that caller
  opened, and leaves its pages attached to those another caller opened;
  #216 refines which of these only detach the caller's pages.
- **The memory budget** (`vtBudget`,
  [`budget.go`](../../internal/server/gateway/budget.go)). The emulators of
  every web terminal SSH Session, across all Users, hold at most
  `--terminal-memory` MiB together ([`kokod run`](cli.md#the-commands-so-far)):
  by default a quarter of the memory the koko server may use, on Linux the
  machine's or its cgroup's `memory.max`, whichever is less, at most 2 GiB,
  and 2 GiB where it cannot tell. Each emulator's `Memory`, with the
  snapshots its pages hold, is counted after each call into it and each
  reset. Once a call takes them past the budget, the largest emulator
  that holds at least 8 MiB (`budgetFloor`) fails, the one measured and
  not one made since, whichever call passed it.
- **A failed emulator**, failed by the budget or by itself (its core past
  its bound, a trap), is made anew: the loop makes a new one at the pty's
  size, writes `koko: this terminal's screen and history were dropped; the
  shell goes on.` into it, and sends every page a `reset` and its snapshot.
  The shell and the pages go on. Should the new one fail too, as it takes
  the old one's place, the SSH Session ends (`emulator failed`). Its
  failures are logged at warning level once a minute, the others at
  debug level, as a Host whose output fails every new emulator would
  fill the log.
- **Through `koko web`** the caller is the device's Device Session, acting
  as its User. Its SSH Sessions count against the Workspace, not against
  the Device Session's 64, and its revocation ends those it opened as it
  ends its Tunnels. The Local caller's pages join the Owner's Workspace; an
  attachment of theirs resumes only on the run of `koko web` that made it,
  and the SSH Sessions a run opened end when its connection does (`koko
  web ended`); the web front hands the Gateway the run's end
  (`TerminalCaller.RunEnded`). `koko web` forwards a browser connection's
  bytes as they are, so the WebSockets' upgrades and frames pass through
  it unchanged and no channel's revision changes; a dropped Device
  Connection ends the `web@koko` channels on it, and so the WebSockets, and
  the page resumes its attachments on a new one once the Device Mux has
  reconnected.
- **Stopping** (`CloseTerminals`). The web listener's shutdown does not
  track a WebSocket, which it has given up, so `server.Close` first has the
  Gateway end every web terminal SSH Session with the restart notice, which
  closes each attachment's WebSocket, send `{"stopping": "the koko server
  is restarting"}` on every Workspace socket and close it, and refuse new
  pages; a WebSocket still at it after `timing.StopNotice` (5 s), a page
  too stalled to take the notice, is cut. The web listener follows, then
  the Tunnels and the transport, as below.
- **A restart.** A stop records no ending in the tabs, so at the next
  start, after a clean stop or a crash, each tab whose SSH Session was
  live comes back `restarted`, and a tab ended before the stop stays
  ended. A page reopens each `restarted` tab with `reopen`, the next one
  only once the last one's tab has left `starting` (`running`, `refused`
  or `ended`), so that the Hosts are dialled one by one: the answer comes
  once the tab is `starting`, before its dial, and so throttles nothing.
  A Host no longer in reach leaves its tab `refused`.
- **Files** (`Files`, [`files.go`](../../internal/server/gateway/files.go)).
  The page's files calls ([the web front](web-front.md#the-web-terminals-files))
  move files over SFTP on the SSH Session's own Host connection, as the
  Host Account, so the web front never holds a Host connection.
  `Files(caller, id)` takes only the caller who opened the SSH Session, and
  returns its SFTP client
  ([`github.com/pkg/sftp`](https://github.com/pkg/sftp)): one `session`
  channel with the `sftp` subsystem, opened at the first call, outside the
  Gateway's locks, and shared by every call after it, whether pages are
  attached or not;
  the next call opens another once the Host ends it. pkg/sftp's open sends
  no mode, so the client writes through `privateCreates`, which gives each
  exclusive create, an upload's partial file, the permissions `0600`, so
  the Host makes it private as it creates it. Or it says why not:
  `unknown_tunnel` for an id that names no live web terminal of the
  caller's, `ssh_session_ended` for one that has ended, as long as its
  ending is kept, `sftp_unavailable` for a Host that refuses the subsystem,
  and `host_refused` for one that refuses its channel. Once the SSH Session
  ends, its files end first, while the Host connection still lives: every
  later call is `ssh_session_ended`, and the end hooks the web front
  registered (`Files.AtEnd`) run, waited for up to `timing.StopNotice`, so
  an upload's partial file is removed; then the SFTP client and the Host
  connection close, which cuts every transfer still running.

## Detach and Resume

Only the device's leg Resumes
([ADR 0021](../adr/0021-an-ssh-session-survives-a-network-drop-by-resuming-its-tunnel.md));
the Gateway's side is [`resume.go`](../../internal/server/gateway/resume.go).
A web terminal's SSH Session is never Detached or Resumed: its pages'
attachments are, each within `timing.Grace`
([the web terminal](#the-web-terminal)).

- **Detached.** When a Tunnel's channel fails, as when the koko server
  declares its Device Connection dead (`timing.ServerDead`, 45 s, [the
  Device Connection](device-connection.md#the-tcp-listener)), or the
  connection closes, the Tunnel is Detached (`ssh session detached`). Its SSH
  Session and the Host connection carry on: what the Host sends waits in the
  stream's replay buffer, which blocks the relay once full
  ([the resumable stream](resumable-stream.md)), and the Host's keepalives
  are answered as above. The grace, `timing.Grace` (1 minute), starts
  then; when it runs out, the Tunnel ends (`grace over`). A Detached Tunnel still counts
  against its caller's limit.
- **A resume** is a `tunnel@koko` channel whose payload names the Tunnel
  and carries the device's received offset. The Gateway takes it from the
  Device Session that opened the Tunnel only, on any of its connections,
  from any address, or for the Local caller's Tunnel, over the Unix socket.
  It first checks the SSH Session's access again, by the rule a live one
  goes on by ([below](#access-during-a-live-ssh-session),
  `recheckResume`), and ends it, refusing the resume, should a new one now
  be refused or the read fail. It then accepts the channel, answers with
  its own received offset and resends
  from the device's ([the resumable stream](resumable-stream.md#on-the-wire)),
  stops the grace and logs `ssh session resumed` with the address the
  resume came from. The last attach wins: a resume also takes the Tunnel
  off a channel the koko server has not yet found dead, as when the device
  found a stalled path dead first, or moved to another network, and that
  Tunnel is never Detached. A command that ends while its Tunnel is
  Detached, its output and exit status passed on into the stream, reaches
  the device with the Resume. The Host's connection ending while the Tunnel
  is Detached ends the Tunnel, whose resume is then refused, saying why.
- **Withdrawn access** ends a Detached Tunnel as it ends an attached one
  ([below](#access-during-a-live-ssh-session)), a Revoked Device Session's
  included; its resume is then refused, saying why.
- **A refused resume** is the channel open's refusal with reason
  `administratively prohibited` (`ssh.Prohibited`), which no other refusal
  of a `tunnel@koko` channel has, by the Gateway or by a Device Mux, so the
  device gives up rather than try again. Its message is why the Tunnel
  ended, a clause set beside each ending's notice (`ending.why`), such as
  `web1 closed the connection` or `access to web1 was withdrawn: web1 was
  Retired`, for a Tunnel of this Device Session that has ended, with a
  notice, and that the Gateway still knows of: it keeps how each Tunnel
  that ended with a notice ended (`keepEnding`) until its grace would have
  ended, counted from its Detach, or, for one that ended on a channel,
  whose close frame a stalled path may never deliver, from when it left
  the registry. The message is empty when the koko server holds no Tunnel of
  this Device Session with that id, as when its grace ran out, its kept
  ending has expired, or the koko server restarted, and the same for a
  Tunnel id another Device Session offers, so a stolen id is of no use.
  Every other refusal, such as the koko server stopping, is `connect
  failed`, and the device tries again.
- **A resume that fails** once the channel is accepted, as when the device
  is gone again before the answer, leaves the Tunnel Detached, its grace
  running (`ssh session resume failed`); an offset outside the replay buffer ends
  the stream and the SSH Session.

## When an SSH Session ends

An SSH Session ends when the client closes its connection, the Host's
connection ends, its Tunnel stays Detached for the whole grace
([Detach and Resume](#detach-and-resume)), its access is withdrawn
([below](#access-during-a-live-ssh-session)), or the koko server stops; a
web terminal's also when a page closes its tab, its shell ends, or it has
had no page attached for 7 days ([the web terminal](#the-web-terminal)).
When the Host's connection ended first, what the
Host sent before, an `exit-status` included, is passed on to the client
first. Some ends come with a notice (`ending`): the Gateway writes it as one
`koko: ` line on the stderr of each session channel the client has open, so
ssh shows it in its own output, then closes the Tunnel's stream with the
notice as its close frame's reason, for a client with no session open, such
as one still in its handshake. A web terminal's notice goes to each page
attached, as a line and as its stream's close, and into its tab. The
Gateway then closes both connections and takes the Tunnel out of the
registry.

| End | Notice | `reason` in the log |
| --- | --- | --- |
| The client closed its connection | none | `client closed` |
| A page closed a web terminal's tab | none | `tab closed`, with `closed_by`: the Sign-in's or Device Session's id, or `unix socket` |
| A web terminal's shell ended, with its exit record | none | `shell ended` |
| A web terminal had no page attached for `timing.WorkspaceIdle` (7 days), from its last page's detach or from its start | none | `idle` |
| A web terminal's emulator failed, and a new one failed too | `the koko server could not keep this terminal's screen, so this SSH Session is closed; its log says why.` | `emulator failed` |
| The run of `koko web` that opened a Local caller's web terminal ended | none | `koko web ended` |
| The Host leg broke: the [Host connector](host-connector.md#keeping-a-connection-alive) heard nothing for 60 s, its probes unanswered | `lost the connection to web1 (no answer for 60 s); this SSH Session has ended.` | `Host leg lost` |
| The Host, or the network beneath, closed its connection | `web1 closed the connection.` | `Host closed` |
| A Relay's leg broke, as the Host leg's rows above: the leg nearest the koko server that ended, which takes every leg after it down with it | `lost the connection to web1's Relay gw (no answer for 60 s); this SSH Session has ended.`, or `web1's Relay gw closed the connection.`, the Relay unnamed to a caller who cannot follow the links to it | `Relay leg lost`, `Relay closed` |
| The koko server stops cleanly, as for an upgrade | `the koko server is restarting; this SSH Session has ended.` | `server stopping` |
| The Tunnel stayed Detached for the whole grace; never a web terminal's | none | `grace over` |
| Its Device Session was Revoked | `access to web1 was withdrawn: this device's Device Session was Revoked; this SSH Session is closed.` | `revoked` |
| Its User was Removed | `access to web1 was withdrawn: bob was Removed; this SSH Session is closed.` | `user removed` |
| The browser that opened a web terminal signed out | `this browser signed out; this SSH Session is closed.` | `signed out` |
| Its Sign-in was revoked (`signin.revoke`) | `access to web1 was withdrawn: this browser's Sign-in was revoked; this SSH Session is closed.` | `signin revoked` |
| The Password was set or changed, ending the User's other Sign-ins | `access to web1 was withdrawn: the Password was changed; this SSH Session is closed.` | `password changed` |
| The Password was removed (`password.remove`) | `access to web1 was withdrawn: the Password was removed; this SSH Session is closed.` | `password removed` |
| Its Sign-in expired, 30 days from its start or 7 from its last use | `access to web1 was withdrawn: this browser's Sign-in has expired; this SSH Session is closed.` | `signin expired` |
| The Web URL changed, for a web terminal on the web listener | `the Web URL changed; this SSH Session is closed. Sign in at the new address.` | `web url changed` |
| The Host was Retired | `access to web1 was withdrawn: web1 was Retired; this SSH Session is closed.` | `host retired` |
| The Host went back to Registered | `access to web1 was withdrawn; this SSH Session is closed.` | `host registered` |
| A Relay of the chain it started through, or of its Host's chain now, is no longer Managed | `access to web1 was withdrawn: web1's Relay gw is no longer Managed; this SSH Session is closed.`, the Relay unnamed to a caller who cannot follow the links to it | `relay registered`, `relay retired` |
| The caller can no longer act in the Host's Space: they were taken out of it, it was moved to one they cannot act in, or they were demoted from Administrator and are no Member of it | `access to web1 was withdrawn; this SSH Session is closed.` | `access withdrawn` |
| The koko server could not read the record to check access again | `the koko server could not check access to web1, so this SSH Session is closed; its log says why.` | `access check failed` |
| Its Host's chain of Relays is broken in the record | the same | `relay chain broken` |

A channel the Host's connection took down is left open for the notice
(`Conn.Broken` tells it from one the Host closed), and its EOF and close
follow it. The Gateway waits for the notice's writes until they are done or
the Tunnel's stream is off its channel, which a stalled client's never
takes them from.

On a clean stop, [`server.Close`](../../internal/server/server.go) first
ends the web terminals and sends `stopping` on every Workspace socket
([above](#the-web-terminal)), then closes the web
listener, then the Gateway, which ends every Tunnel with the restart notice, and
only then the transport, which closes the Device Connections, so `koko
tunnel` exits at once rather than taking the stop for a drop. A connection
too stalled to take its Tunnel's close within `timing.StopNotice` (5 s) is
closed anyway, which ends the Tunnel.

## Access during a live SSH Session

A live SSH Session goes on only while a new one would be allowed (P0 spec:
reach, "Access during a live SSH Session"). The Gateway learns of each
change that can withdraw access once it commits, through the
[access-change signal](device-connection.md#revoking), which
[`server.Start`](../../internal/server/server.go) hands to
[`Gateway.Withdraw`](../../internal/server/gateway/withdraw.go). Each
SSH Session that ends gives its notice as any ending does
([above](#when-an-ssh-session-ends)), the rows from "Revoked" on.

- **A Revoked Device Session's** SSH Sessions end at once, without a read:
  a Revoked Device Session never comes back. Its notice names the
  revocation, or the User's removal when `user.remove` Revoked it, and
  reaches the client before its Device Connections close
  ([revoking](device-connection.md#revoking)).
- **An ended Sign-in's** web terminals, those it opened, end at once too,
  with pages attached or none, without a read: the change names the
  Sign-ins it ended and why (`SignIns`, `SignInEnd`), and each ends with
  the notice of its end reason ([above](#when-an-ssh-session-ends)): signed
  out, revoked, the Password changed or removed, the User Removed
  (`signInEnding`). A check that finds a Sign-in ended some other way, as
  `recheck` and `recheckResume` read it, ends its SSH Session as its record
  says it ended (`signInWithdrawal`). An attach or a resume by a Sign-in
  that has ended never reaches the Gateway: the web front refuses it first,
  with `signin_required`
  ([the web front](web-front.md#the-web-terminals-websocket)).
- **A Sign-in's expiry** publishes nothing, so the Gateway sweeps for it
  (`sweepSignIns`): every `timing.SignInCheck` (1 minute) one read-only
  transaction reads the Sign-in that opened each web terminal through
  `access.SignIns`, on the koko server's clock, and each ended one ends its
  SSH Sessions as its record says it ended, `signin expired` for an
  expired one. A read that fails ends nothing and is logged; the next sweep
  reads again. The first message of a Workspace socket or a terminal
  socket moves its Sign-in's Last used time, as the web front's cookie
  check does for a call; the Workspace socket's requests and keys typed do
  not, so a Sign-in whose only use for 7 days is a page attached without a
  drop expires, and ends the SSH Sessions it opened.
- **A Web URL change** (`WebURL`, published by `server.set` only when the
  stored address changes or is cleared) signs out every page on the web
  listener, so every Sign-in's SSH Session ends at once (`web url
  changed`). No check of access tells the Web URL, so the web front hands
  the Gateway the Web URL it checked the page against
  (`TerminalCaller.WebURL`): an open or a reopen whose Web URL has changed
  since, as from a page that upgraded its Workspace socket before the
  change, is refused
  with `access_refused`, `the Web URL changed; this SSH Session did not
  start. Sign in at the new address.`, and one admitted across the change
  ends once registered. Terminals through `koko web`, at its own address,
  go on.
- **Any other change** names the Hosts it may have put out of a caller's
  reach (`host.move` to another Space, `host.retire`, and `host.set` sending
  a Managed Host back to Registered or giving it another Relay, whose own
  chain may hold one that is not Managed), or the live Device Sessions and
  Sign-ins of each User `space.member.remove` took out of a Space or
  `user.demote` demoted, by the caller's id (`op.Caller.ID`). When a change names any Host, the Gateway
  checks every live SSH Session again, since a named Host may be the Relay
  one passes through or the one its Host was given since it started, even
  when it started with none; when it narrows callers, it checks each SSH
  Session of theirs. It checks them in one read-only transaction, by the
  rule that admits a new one
  ([`host.ReachOf`](../../internal/server/host/reach.go) by the Host's id,
  so a rename changes nothing, and the Host and every Relay of its chain
  Managed), and ends each one it would now refuse. Each SSH Session keeps
  the chain it was dialled through (the registry's `relayIDs`): every Relay
  of it, and of the Host's chain now, must stay Managed, and the first that
  is not ends it, in the chain it started through first, nearest the koko
  server first in each
  ([`relayWithdrawal`](../../internal/server/gateway/withdraw.go)). So a
  Relay sent back to Registered, or Retired once no Host uses it, ends every
  SSH Session still passing through it, wherever it sits in the chain, and
  a change of any Relay's own Relay, or of the Host's, keeps a live SSH
  Session on the chain it started through while every Relay of both is
  Managed. A Retired Host is named as such only to a caller who can still
  see it.
- **The notice names the Host** by the name the SSH Session was opened
  with, and says nothing more for a Host the caller can no longer see, so it
  tells of no later name and of no Space.
- **Changes that keep access** publish nothing or pass the check, and
  interrupt nothing: a rename, the address, port, Note and Attributes, a
  move to a Space the caller still acts in, another Device Session's
  revocation, another Sign-in's ending, and the Password changed from the
  Sign-in's own browser, which ends only the User's other Sign-ins.
- **An SSH Session admitted across a change**, its admission read before
  the change committed and its Tunnel registered after the Gateway took it,
  is checked again once registered, so none slips between the two.
- **A failed read** ends each SSH Session it was checking, as it would
  refuse a new one, and is logged ([logging](#logging)).

## Logging

One line per event, never an exec's command text. Every line about an SSH
Session names it by `tunnel`, then `client`: `ssh` for a Tunnel of the
CLI's, `browser` for a [web terminal](#the-web-terminal). Who it came from
is `device_session`, the Device Session's id or `unix socket`, or for a
Sign-in's web terminal `signin`, the Sign-in's id; a web terminal through
`koko web` keeps `device_session`. For a web terminal's SSH Session these
name who opened it, and for an attachment the page's caller. A web
terminal's `addr` is the page's: the client's address as the web listener
counts it (the last `X-Forwarded-For` entry), or `koko web`'s
connection's. A web terminal's lines add `tab`, the tab's id, and an
attachment's `attachment`, its id.

- `ssh session started`: `tunnel`, `client`, `user`, `device_session` or
  `signin`, `host`, `host_id`, and for a Host with a Relay `relay` and
  `relay_id`, its own Relay, then `relays` and `relay_ids`, the chain's
  names and ids in dial order, comma-separated, whether or not the caller
  can see them; and `addr`, where the connection came from; for a web
  terminal, then `tab`.
- `ssh session request`: `tunnel`, `client`, `host` and `request`, once per SSH
  Session for each kind: `shell` (a web terminal's, once its shell starts), `exec` (never its text), `subsystem` with
  `subsystem`, its name, and each forward's `target`: `-L` with the address
  on the Host's side a `direct-tcpip` (`-L` or `-D`) or
  `direct-streamlocal@openssh.com` channel opens, `-R` with the address a
  `tcpip-forward` or `streamlocal-forward@openssh.com` asks the Host to
  listen on. A busy forward is logged once per target.
- `file transferred`, once per file a web terminal moved
  (`Files.Transferred`), when its transfer ends: `tunnel`, `client`,
  `user`, `signin` or `device_session`, `host`, `host_id`, `direction`
  (`upload` or `download`), `path` (the file's absolute path on the Host: a
  download's as asked, an upload's final one), `bytes` (those moved), and
  `outcome`: `done`, `cancelled`, or `failed` with `err`. A listing is not
  logged, and no line holds a cookie, a download token or a file's
  content.
- `ssh session ended`: the same as the start but the Host's own Relay and
  `addr`, then for a web terminal `tab`, then `duration`,
  `bytes_from_device` and `bytes_to_device` (the Tunnel's bytes each way,
  or a web terminal's terminal records from and to all its pages),
  `exit_status` and `exit_signal` when the Host sent them; for an ending
  on a Relay, its leg's or its state's, that Relay as `relay` and
  `relay_id` with its `hop`, from 1, so `relay` names the Relay that ended
  it, never another; for a broken chain `read_relay_ids`, the ids read; for
  `tab closed`, `closed_by`; and `reason`, as the table above says.
- `ssh session refused`: `tunnel`, `client`, `name` (the SSH Name asked for),
  `device_session` or `signin`, `addr` and `reason`: `unknown host`, the access check's
  code, `host registered`, `relay registered`, `ssh session limit`, `user name` with `ssh_user`,
  the name asked for, `shell refused` with `request` or `err`; and at warning level, since the Owner may have to act,
  `host unreachable`, `login refused` or `host connection failed` with
  `err`, and `host key mismatch` with `pinned` and `offered`, the two
  fingerprints, and a Relay's own with `relay`, `relay_id` and `hop`
  ([Relay](#relay)), as is `relay registered`, and `relay chain broken`
  with `read_relay_ids`. A Workspace's 17th tab is refused so, with `ssh
  session limit`. A client that leaves before its `none`, or a web
  terminal whose tab closed before its shell started, is logged at debug
  level as `ssh session ended before it started`.
- `ssh session attached`, a page attached to a web terminal: `tunnel`,
  `client`, `tab`, `attachment`, `signin` or `device_session`, and `addr`.
- `ssh session detached`: for a Tunnel, `tunnel`, `client` and
  `device_session` or `signin`, when the grace starts. For a web terminal
  it means one page detached: `tunnel`, `client`, `tab`, `attachment`,
  `signin` or `device_session`, `addr` and `reason`: `page left`,
  `malformed record` or `displaced`. A page's WebSocket dropping, which
  starts its attachment's grace, is `web terminal dropped` at debug level,
  with the same fields but `reason`.
- `ssh session resumed`: `tunnel`, `client`, `device_session` or `signin`,
  and `addr`, where the
  resume came from: on TCP, a new address at each Device Connection. For an
  attachment, `tab` and `attachment` too.
- `ssh session resume refused`: `tunnel` (the id the resume named),
  `client`, `device_session` or `signin`, `addr` and `reason`: how the Tunnel ended, as its
  `ssh session ended` line says, or `unknown tunnel`; for an attachment,
  `attachment` (the id the resume named) in place of `tunnel`, and
  `unknown attachment`. `ssh session resume failed`, the same as `ssh
  session resumed` with `err`, for one that failed once accepted.
- `ssh session attach refused`, at debug level: `tunnel` (the id the attach
  named), `client`, `signin` or `device_session`, `addr` and `reason`,
  `unknown_tunnel` or `ssh_session_ended`.
- `workspace tab opened` and `workspace tab closed`, at debug level:
  `user` (the User's id), `tab`, `host` (the Host's name) for an open or
  `label` (the tab's) for a close, and `signin` or `device_session`, who
  opened or closed it.
- `web terminal emulator failed`, at warning level, with `tunnel`,
  `client`, `tab` and `err`, when an SSH Session's emulator fails and is
  made anew; at error level when the new one fails too, and the SSH
  Session ends.
- `web terminal emulator`, once at start: `engine`, which of wazero's
  engines runs the emulator, the compiler or the interpreter and why
  ([the emulator](vt.md#the-module-and-the-runtime)), and
  `memory_bound_mib`, the memory budget. `web terminal emulator
  unavailable`, at error level with `err`, when the emulator could not be
  loaded.
- `web terminal found dead` and `workspace socket found dead`, at debug
  level with `quiet`, when the Gateway cuts a silent WebSocket;
  `workspace socket closed`, at debug level with `reason="malformed
  message"`, for a Workspace socket's message that is no request.
- `tunnel refused`: `device_session`, `addr` and `err`, for a payload the
  Gateway does not take, and with `name` before initialisation.
- `open a Tunnel`, at error level, with `device_session`, `addr`, `name`
  and `err`, when the koko server could not read what admitting a Tunnel
  needs; the channel is refused saying the log tells why. `open a web
  terminal` is the same for a web terminal, whose request is answered, or
  whose tab refused, with `internal_error`. `open a Workspace`, `read the
  Hosts of a Workspace`, `close a web terminal's tab` and `record how a web
  terminal's tab ended`, at error level with `err`, are the same for the
  Workspace's own reads and writes.
- `access checked again`, at debug level, with `ssh_sessions`, how many
  live SSH Sessions an access change had checked again, and `ended`, how
  many of them it ended.
- `check access during an SSH Session`, at error level, with `err`, when
  the koko server could not read the record to check live SSH Sessions
  again after an access change, or a resuming one; each ends with
  `reason="access check failed"`.

## `koko exec`

`koko exec <host> <command...>` ([`exec.go`](../../internal/client/cli/exec.go)),
irreversible, runs one command on one Host through a Tunnel, as `ssh` would
but with Go's SSH client and no OpenSSH, Include line or terminal:

- It reaches the koko server as every command does, over the Unix socket as
  the Local caller or over the device's Device Connection, through the
  [Device Mux](device-mux.md), which carries the `tunnel@koko` channel and
  its payload unparsed, or direct
  ([which koko server](device-connection.md#which-koko-server-the-cli-reaches)).
  It opens a Tunnel to the SSH Name, and checks the Gateway's host key
  against the client config's pinned keys; over the socket, whose
  directory is its pin, nothing is pinned.
- Every word after the Host is the command's, flags included, joined with
  single spaces, as ssh joins them
  ([ADR 0033](../adr/0033-koko-exec-joins-its-words-as-ssh-does-and-forwards-no-signal.md)).
  It sends one `exec` request and no pty, passes stdin to the command, and
  stdout and stderr apart.
- Its Tunnel Resumes as koko tunnel's does ([below](#the-device-end-of-a-tunnel)),
  so a command in flight carries on across a drop. When it cannot Resume,
  koko exec prints [the same line](#when-a-tunnel-cannot-resume) as koko
  tunnel and exits 255.
- It exits with the command's status, and 255 when there is none or koko
  itself fails, as ssh does: a command a signal ended (it names the signal),
  a refused command line, no koko server, a refused SSH Session, a Tunnel
  that could not Resume or that koko disconnect ended, or an interruption
  ([the CLI](cli.md#the-runner-and-the-output-layer)).
- When the Gateway ends the SSH Session with a notice, such as that the
  koko server is restarting, the notice's line is on its stderr already, as
  the session's, so it exits 255 and adds nothing; a notice that did not
  reach the session, one still opening, say, is its error.
- It forwards no signal: Ctrl-C ends the SSH Session and says so, wherever
  the SSH Session has got to: its handshake, the open of the command's
  channel, or the command.

## `koko tunnel`

`koko tunnel <ssh-name>` ([`tunnel.go`](../../internal/client/cli/tunnel.go)),
irreversible, is the ProxyCommand of koko's ssh_config
([the device's ssh](device-connection.md#ssh)) and not for direct use. It
takes exactly one argument, the SSH Name as ssh passes `%h`, and no flag, not
even `--json` (`Command.Stdio`, [the CLI](cli.md#the-command-tree)).

- It reaches the koko server as every command does, through the
  [Device Mux](device-mux.md) or direct, or over the Unix socket as the
  Local caller, opens a Tunnel to the SSH Name, and relays the stream on its
  stdin and stdout. ssh's own SSH client runs inside it against the
  Gateway, which presents the koko server's host key, the one koko's
  known_hosts pins under the alias `koko`; koko tunnel checks no key itself.
- **Before the Tunnel opens**, a failure is the one `koko: ` line, exit 1,
  that every command prints for the same code, word for word: not joined
  (naming the socket path it found empty), the revocation record (when koko
  learned it), Revoked (and that koko deleted the device's key), the koko
  server unreachable (naming `koko connect <host:port>`) and its host key not
  the pinned one (naming both). ssh then adds `Connection closed by UNKNOWN
  port 65535` and exits 255.
- **Once it is open**, the Gateway's refusals are banners in ssh's own
  output, and its notices lines on the session channels' stderr, and koko tunnel
  prints nothing. When the Gateway closes the Tunnel, a koko server stop
  included, it exits 0. When the Tunnel's channel fails, it Resumes, and
  prints nothing ([below](#the-device-end-of-a-tunnel)). When it cannot
  Resume, it prints [the line that says so](#when-a-tunnel-cannot-resume)
  and exits 1, and ssh, its connection gone, ends with 255; when koko
  disconnect ended it ([below](#the-device-end-of-a-tunnel)), `koko: koko
  disconnect ended the SSH Session to web1`, and exits 1.
- **stdin's EOF** half-closes the Tunnel; koko tunnel relays what the
  Gateway still sends until the Gateway closes it.
- **Signals**, through `cli.Runner.Signals`, which `cmd/koko` sets to
  `signal.Notify`: it ignores SIGINT, which ssh also gets when Ctrl-C is
  pressed without a tty, and which ends every other command's context; on
  SIGHUP, which ssh sends its ProxyCommand when it exits, or SIGTERM, it
  sends what is unsent and a close frame, then exits 0, so the Gateway ends
  the SSH Session as the client's close (`reason="client closed"`).
- A running koko tunnel stays connected to the Device Mux, so an open SSH
  Session, or one waiting to Resume, keeps the Device Mux from idling out.

## The device end of a Tunnel

[`tunnel.Tunnel`](../../internal/client/tunnel/tunnel.go), which koko
tunnel and koko exec share, Resumes its Tunnel each time the stream's
channel fails, so the SSH Session above it only freezes; ssh's stdio stays
open meanwhile.

- **Which connection.** It resumes on the connection it opened on while
  that lasts: through a Device Mux, which closes the Tunnel's channel when
  its Device Connection drops, once the channel has passed on what came
  before the drop ([the Device Mux](device-mux.md#the-local-protocol)),
  the resume's open waits for the Device Mux's
  next Device Connection, and is sent again each time the Device Mux
  refuses it after `timing.CommandWait`, which keeps the Device Mux
  reconnecting. Once that connection has ended, as when the Device Mux
  crashed or a direct Device Connection died, it reaches the koko server
  again as the command did (`Input.redial`): over the Unix socket, through
  this build's Device Mux, starting a new one, or with `KOKO_DIRECT=1`
  direct, over a Device Connection of its own, which it closes when done.
- **Trying again.** Each try begins at least a wait after the last one
  began, the wait `timing.ReconnectFirst` (250 ms) at first and doubling
  up to `timing.ReconnectMax` (5 s), so a try that took longer, as one
  waiting out a Device Mux's `timing.CommandWait` does, is followed at
  once. Once its channel is open, the koko server's answer must come
  within `timing.ResumeExchange` (5 s), or the channel is closed and the
  resume sent again.
- **Giving up.** It ends with a `*tunnel.LostError`, at the time it lost
  the channel, when the koko server refuses the resume
  ([above](#detach-and-resume), `*tunnel.RefusedError` with its reason),
  when the connection's end says nothing
  would reach the koko server again (`serverconn.ErrDisconnected` from
  koko disconnect, `serverconn.ErrRevoked`), when reaching it anew fails
  for a reason trying again does not change (`tunnel.ErrFinal`: not
  joined, Revoked, a host key not the pinned one), when the channel
  failed before the Tunnel's id came, or once it has tried for
  `timing.ResumeGiveUp` (2 minutes) from the loss without the koko server
  Resuming or refusing it (`tunnel.ErrGaveUp`), a try waiting for a
  connection or a channel included; a resume whose channel the koko server
  took is let finish, within `timing.ResumeExchange`.
  That time is the device's awake time: it is counted on the monotonic
  clock, which stands still while the device sleeps, so a laptop that
  slept through the night still tries for 2 minutes once it wakes.
- **koko disconnect.** When the Device Mux it goes through says that koko
  disconnect stopped it (`serverconn.Conn.Disconnected`), it stops any
  Resume and closes for good, sending what is unsent and a close frame
  while the Device Mux waits for its channel
  ([the Device Mux](device-mux.md#state-and-exit)), so the Gateway ends
  the SSH Session as the client's close (`reason="client closed"`), never
  Detached. It then ends with a `*tunnel.LostError` wrapping
  `serverconn.ErrDisconnected`.
- **Closing** a Detached Tunnel, as on SIGHUP or koko disconnect, sends no
  close frame, as there is no channel to send it on: the Gateway holds the
  SSH Session until its grace ends.
- **Waiting to Resume** through a Device Mux, it says so to the Device Mux
  (`devicemux.Conn.WaitingToResume`), which counts it for `koko status`
  ([the Device Mux](device-mux.md#the-local-protocol)).

### When a Tunnel cannot Resume

koko tunnel and koko exec print one line for a Tunnel that ended with a
`*tunnel.LostError` (`sessionError` in
[`exec.go`](../../internal/client/cli/exec.go)): the time it lost its
channel, the Host, and why it could not Resume when that is known, after a
colon:

```
koko: lost the connection to the koko server at 12:01:07; the SSH Session to web1 could not be resumed.
koko: lost the connection to the koko server at 12:01:07; the SSH Session to web1 could not be resumed: access to web1 was withdrawn: web1 was Retired.
```

| Why | What follows `could not be resumed` |
| --- | --- |
| The koko server holds no such Tunnel: its grace ran out, or the koko server restarted | `.` |
| The koko server refused the resume with a reason ([above](#detach-and-resume)) | `: ` the reason, then `.`, such as `: web1 closed the connection.` |
| The device's Device Session was Revoked, so it reaches the koko server no more | `: this device's Device Session was Revoked.` |
| It gave up after `timing.ResumeGiveUp` awake | `: koko could not reach the koko server for 2 minutes.` |
| Any other final failure, such as a device no longer joined | `.` |

koko disconnect ending the Tunnel has its own line instead (`koko: koko
disconnect ended the SSH Session to web1`).

## Verifying

[`tunnel_test.go`](../../internal/client/cli/tunnel_test.go) drives `koko
tunnel` with an `x/crypto/ssh` client over its stdio (`h.Proxy`,
`d.Proxy`), against the S1 koko server and in-process Hosts: exec, a shell
with env, a pty and a window change, a signal and the sftp subsystem; `-L`
and `-D` to a TCP port, `-L` to a Unix socket, and `-R` from a TCP port and
a Unix socket with their cancels; each failure before the Device Connection
as one line (golden files `tunnel-*`; unreachable also through the Device
Mux); stdin's EOF; SIGINT ignored; SIGHUP and
SIGTERM sending a close frame, over the Unix socket, a direct Device
Connection and the Device Mux; the Device Mux kept from idling out; and the
command line, help, skill and completion.

[`openssh_test.go`](../../internal/client/openssh/openssh_test.go), the
real-OpenSSH test ([code layout](code-layout.md#the-real-openssh-test)),
runs the system's `ssh`, `scp` (SFTP and `-O`), `sftp`, `rsync`, `-L` (to a
TCP port and a Unix socket), `-D` and `-R` (from a TCP port and a Unix
socket) with `-F` pointed at koko's ssh_config under a temporary HOME,
through a koko binary built for the test, its `koko tunnel` and its real
Device Mux process, against the S1 koko server and in-process Hosts serving
sftp through the system's `sftp-server`: checksums both ways, exit statuses,
stdin and stderr, bare names, a bare name another block after koko's
Include gives a HostName and a Port, the banners for an unknown name, an
unseen Host and a Host that is down, which Host requests scp made, that ssh
authenticates with one `none` request, offers the user's own key to nothing,
never connects to their agent and names nothing in the real `~/.ssh` in its
`-v` output, and that `koko disconnect` with an SSH Session open finds the
one Device Mux with that session's `koko tunnel` connected to it. On the
koko server's own machine it runs `koko ssh setup`, then `ssh web1.koko` and
bare `ssh web1` over the Unix socket, as the Local caller, with no Device
Mux.

[`exec_test.go`](../../internal/client/cli/exec_test.go) drives `koko exec`
against the S1 koko server and in-process Hosts: the command's stdin,
stdout, stderr and status, the words as the Host ran them, no pty, the koko
key as the Host Account; through the Device Mux, opening no Device
Connection of its own; both forms of the SSH Name; an unknown name and an
unseen Host refused alike with no connection to the Host, a Registered Host
refused, a Retired Host's old name refused as unknown; 255 for each failure
of koko's own and for a command a signal ended; Ctrl-C, while the
command's channel opens and once it runs; a koko server stop
and its notice; the log lines without the command's text; and a Tunnel
counting as a use of its Device Session.
[`gateway_test.go`](../../internal/client/cli/gateway_test.go) drives the
Gateway through both: the user names it takes and the banner for any other,
which an unseen Host never gets; each Host connector refusal, a network
error retried and nothing else; the 64-SSH-Session limit; agent and X11
forwarding refused; `direct-tcpip` refused on a Device Connection and the
Unix socket; a quiet Host probed, a blip passing, a silent Host's lost line
and a closing Host's line through `koko exec` and ssh; the restart notice
over the Unix socket, a direct Device Connection and the Device Mux, and
`koko tunnel` exiting 0 at once; and each kind of request logged once.
[`access_live_test.go`](../../internal/client/cli/access_live_test.go)
holds live SSH Sessions through ssh and `koko exec` across each change that
withdraws access, each ending with its line, the Revoked one before the
Device Connection closes, while those that keep access, the Owner's among
them, go on; and across each change that keeps access, which ends none.
[`relay_test.go`](../../internal/client/cli/relay_test.go) reaches a Host
through its Relay, through koko exec and ssh, as the Owner and as a Member
who cannot see the Relay: the Relay carrying each connection, the start's
log line naming it, each refusal of the Relay's leg and of a Registered
Relay with its banner as each caller sees it, and live SSH Sessions ended
with their line once the Relay goes back to Registered, or once it closes
the connection; and one that goes on through its Relay when its Host gets
another, until that Relay is Retired or the new one goes back to
Registered.
[`chain_test.go`](../../internal/client/cli/chain_test.go) does the same
through a chain of two Relays, as the Owner and as Members who see none,
one or all of them, each refusal at each hop with its banner and its log
line's `hop`; live SSH Sessions ended once a Relay in the middle goes back to
Registered, closes its leg or stops answering, and kept across a change of
a Relay's own Relay until a Relay of either chain stops being Managed; and a
chain broken in the record, refused and never read as no Relay.
[`resume_test.go`](../../internal/client/cli/resume_test.go) drops the
Device Connection beneath an SSH Session with a
[`harness.Forward`](../../internal/harness/forward.go), cut, which both
ends see at once, or stalled with no close passed on (`f.Partition()`),
which the device finds dead first, so its resume from a new address wins
over the channel the koko server still holds: a shell through koko tunnel
and the Device Mux, and a 4 MiB upload and download through koko exec on
the direct path, carry on intact, and the log shows the Resume from a new
address, and no Detach after a stall; a command that ends while Detached,
the device kept from reconnecting until the Detach is logged, gives koko
exec, through the Device Mux, its output and exit status on the Resume; a
half-close before a stall
survives it; killing the Device Mux ends no
SSH Session, which Resumes through a new Device Mux, and with
`KOKO_DIRECT=1` over a Device Connection of its own; a Tunnel waiting to
Resume keeps the Device Mux from idling out and shows in `koko status`;
`koko disconnect` ends koko tunnel's and koko exec's SSH Sessions as the
client's close, never Detached, and without a Resume; a Host with
`ClientAliveInterval` (`sshhost.ClientAlive`) keeps a Detached SSH
Session; and another Device Session's resume is refused.
[`resume_refused_test.go`](../../internal/client/cli/resume_refused_test.go)
holds a Detached SSH Session through koko tunnel, direct and through the
Device Mux, while its grace runs out,
the koko server restarts, its access is withdrawn, its Host closes the
connection, its kept ending expires, or its Device Session is Revoked, and
while the device gives up, each ending with its line and exit 1, and the
koko server logging the refused resume's reason; and koko exec printing the
same line and exiting 255.
[`gateway/relay_test.go`](../../internal/server/gateway/relay_test.go)
writes lines on a session channel's stderr from several goroutines beside
its EOF, which `go test -race` fails unless they take turns.
[`gateway/terminal_test.go`](../../internal/server/gateway/terminal_test.go)
counts SSH Sessions by the caller's id, 64 Tunnels for a Device Session and
none for the Local caller, a Workspace's SSH Session counting against its
tabs whoever opened it; and maps the access check's refusals to
`unknown_host` and `access_refused`.
[`gateway/session_test.go`](../../internal/server/gateway/session_test.go)
runs an SSH Session's loop over a fake emulator and shell: in a
`testing/synctest` bubble, one with no page ends with `idle` a week after
its start, and one a page attached to on day 6 goes on until a week after
that page detached; an ending while the loop is inside an emulator call
that never returns finishes its page with the notice at once; and an
emulator that fails, past its own bound or past the budget, is made anew,
the shell and the page going on and the page getting a `reset` and the
dropped line, the budget failing the largest emulator, not the one whose
call passed it.
[`server/terminal_memory_internal_test.go`](../../internal/server/terminal_memory_internal_test.go)
checks the budget's default: the figure given, else a quarter of the
machine's memory at most 2 GiB, and 2 GiB when it cannot tell.
[`web/workspace_test.go`](../../internal/server/web/workspace_test.go)
drives the Workspace on the web listener with Go pages
(`harness.TerminalPage`, its `Workspace()` and `Attach`) against
in-process Hosts: two Sign-ins of one User sharing it, a tab opened in one
page showing in the other and in its own before the answer, labels
running `web1 (2)`, a tab closed from either going from both, two closes
of one tab making one change and two answers, two opens of the 16th tab
making one tab and one `ssh_session_limit`, a page joining late getting it
whole, and another User seeing none of it and unable to attach; a second
page attaching while a full-screen program shows, getting a `reset` and a
snapshot that draws the first page's screen, keys from both reaching the
shell in order, and a page attaching while output streams missing no byte
and getting none twice; a page that stops reading holding up neither the
shell nor the other page, then getting a `reset` and a fresh snapshot;
the pty following the active page, by typing and by `active` alone, each
page told whether it is active, a page that only reconnects or resumes
taking nothing, a page detaching leaving the size, and a box past
`maxGrid` held to it; a 9th page displacing the least recently active; a
shell's device attributes query answered once with two pages attached
and with none; and a koko server stopped and started on the same store,
cleanly and after a crash, bringing its tabs back `restarted`, reopened
one after another with the restart line, a second page's reopen at once
opening nothing more, a Host no longer in reach refused, and a tab ended
before the stop staying ended.
[`web/terminal_test.go`](../../internal/server/web/terminal_test.go)
drives the web terminal on the web listener with a Go page
(`harness.TerminalPage`, `harness.WebTerminal`) against in-process Hosts:
the `pty-req` and `shell` as the Host Account, 1 MiB each way byte for
byte, the active page's resize as a `window-change`, the exit status and
signal records; each refusal of an open with its code and banner, an
unseen and a Retired Host as an unknown name, without a dial; a Host
behind a chain of two Relays; the 17th tab of a Workspace refused, from
pages of two Sign-ins of the User, until one is closed; an attachment's
resume that wins over a WebSocket a stalled path still holds and one
after a drop, byte for byte with the keys typed meanwhile arriving once,
and another Sign-in's refused; a page's close only detaching it, and a
tab's close ending its SSH Session at once, a busy shell's whose page
reads nothing included, logging who closed it; a dropped page detached at
its grace, its resume then refused and a new attach taking it; an idle
WebSocket kept by both ends' probes, a stalled one dropped no sooner than
`ServerDead` after its last byte, and a cut one at once, each resumed
byte for byte (`harness.Forward`); a malformed record detaching its page
alone; the restart notice and `stopping` before the web listener closes,
while a call in flight is open on it, a stalled page cut at `StopNotice`,
and a Host sitting on the session's open ended; and the log lines, with
no cookie in them.
[`web/terminal_access_test.go`](../../internal/server/web/terminal_access_test.go)
holds a terminal with its page attached and one whose page's WebSocket
dropped across each change that withdraws a Sign-in's access: signing
out, `signin.revoke`, the Password changed (the changing Sign-in's own
terminal going on), `password.remove`,
`user.remove`, `space.member.remove`, `user.demote`, `host.move`, `host.retire`, a Relay
back to Registered, the Sign-in's expiry (the clock hook past it, the
sweep's interval shortened), with a Sign-in that resumed an attachment or
opened a terminal meanwhile going on, and a Web URL change, the same Web
URL set again ending none and an open sent after the change from a
Workspace socket upgraded before it refused; each
ends with its notice and log reason, and a later resume is refused, with
`signin_required` once the Sign-in has ended and saying why otherwise.
Changes that keep access end none.
[`web/terminal_files_test.go`](../../internal/server/web/terminal_files_test.go)
moves files on in-process Hosts serving SFTP (`sshhost.SFTP`): listings
of the home, `..`, a link to a directory, a file named as a directory and
an unreadable directory, past 5,000 entries, with the `sftp` subsystem
opened and logged once; a Host without SFTP, its terminal going on; the
Origin check, `signin_required`, another caller's SSH Session and an id
of none; files with no page attached, and `ssh_session_ended` once the
SSH Session has ended; uploads in chunks checksummed on the Host, a chunk sent
twice written once, a gap, a finish too early and a chunk past the size
refused, a finish sent again; `file_exists`, then a replace keeping the
old mode, and bad names; four uploads at once, a cancel; the SSH
Session's end and the idle limit each removing the partial file and
logging it; and a Host that fails writes (`sshhost.SFTPFailingWrites`),
each upload leaving nothing under its real name; downloads streamed byte
for byte with their headers, a Chinese name, a quote and a newline among
them, a directory and a FIFO, or a link to one, refused, a cancel midway
logged with the bytes moved, and a token spent once, expired after 60 s on
the koko server's clock, refused to another caller without being spent,
and absent from the log.
[`cli/web_terminal_access_test.go`](../../internal/client/cli/web_terminal_access_test.go)
keeps a terminal through `koko web` across a Web URL change that ends a
Sign-in's.
[`cli/web_terminal_test.go`](../../internal/client/cli/web_terminal_test.go)
does so through `koko web` (`h.Background`): as the Local caller with
`localhost` and `127.0.0.1` Origins, 256 KiB each way through its
forwarder, another Origin and no cookie refused, another run's page
joining the Owner's Workspace and attaching to the terminal but refused
this run's attachment, and the terminal ending with its run; on a joined
device, a terminal opening through `koko web` beside 64 Tunnels, and the
65th Tunnel then refused, since the Workspace counts the terminal, and the
Device Session's revocation's notice reaching the page; and, in
`realTime`, a cut and a stalled Device Connection beneath it, the page
resuming its attachment through `koko web` and the Device Mux byte for
byte.
[`wire/tunnel_test.go`](../../internal/shared/wire/tunnel_test.go) pins the
`open` and `resume` payloads' bytes.
