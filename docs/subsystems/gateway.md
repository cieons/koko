# The Gateway, the Tunnel, `koko exec` and `koko tunnel`

The Gateway ends every SSH Session and logs in to the Host with the koko
key, which never leaves the koko server
([ADR 0002](../adr/0002-every-ssh-session-ends-at-the-gateway.md)). Each SSH
Session rides in a Tunnel: one `tunnel@koko` channel carrying a
[resumable stream](resumable-stream.md)
([ADR 0021](../adr/0021-an-ssh-session-survives-a-network-drop-by-resuming-its-tunnel.md)),
or a [web terminal](#the-web-terminal)'s WebSocket carrying one.
The Gateway is [`internal/server/gateway`](../../internal/server/gateway)
(server-only); the device end of a Tunnel is
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
   its [Relay](#relay), if any, are Managed.
3. It accepts the channel, puts the Tunnel in the registry under a new id,
   and sends the id. An allowed SSH Session counts against its caller's
   limit, by the caller's id (`callerID`), attached or Detached, from then
   until its Tunnel ends: a Device Session holds 64 at once
   (`maxSSHSessions`), from the CLI and `koko web`'s
   [web terminal](#the-web-terminal) together, and a Sign-in 16
   (`maxBrowserSSHSessions`); one more is refused, and dials no Host. The
   Local caller has no limit.
4. When the SSH Session is allowed, it dials the Host through the
   [Host connector](host-connector.md), as the Host Account with the koko key
   and the Host's pinned host key, through its Relay if any, while the
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
| Its Relay is not Managed | `koko: web1's Relay gw is Registered: koko passes through it only once it is Enrolled, with koko host enroll gw.` |
| The access check refuses the caller, such as a Revoked Device Session | `koko: ` and the refusal's message |
| The Device Session has 64 SSH Sessions open | `koko: this Device Session already has 64 SSH Sessions open, the most koko allows; this SSH Session did not start.`, or for a Sign-in's web terminal `koko: this Sign-in already has 16 SSH Sessions open, …` |
| Another user name, such as `root@web1.koko` | `koko: web1 is reached as its Host Account "deploy"; leave out "root@".` |
| The Host is down: the connector's 3 attempts each met a network error | `koko: web1 is down or unreachable: koko could not connect to it; this SSH Session did not start.` |
| The Host's host key is not the pinned one | `koko: the host key of web1 does not match the one pinned at Enroll (pinned SHA256:…, offered SHA256:…); koko did not log in. If web1 was rebuilt, pin its new key with koko host enroll web1 --host-key <the fingerprint its provider shows>, or with --replace-host-key, which trusts whatever key it offers.` |
| The Host refused the koko key | `koko: web1 refused the koko key for its Host Account "deploy"; this SSH Session did not start.` |
| The connector failed otherwise | `koko: koko could not connect to web1; this SSH Session did not start.` |

A failure on the leg to the Host's Relay gets the Host's banner for the same
failure, with the Relay in the Host's place ([Relay](#relay)).

The first row is the same byte for byte for a name nobody holds and for a
Host the caller cannot see, whatever user name they ask for, so the Gateway
tells nothing of Hosts outside the caller's Spaces; the user name is
checked only for a Host they may reach. The connector's error, which names
addresses, goes to the log only. Only a network error is retried
([the Host connector](host-connector.md#retries-and-failures)), so a host
key mismatch or a refused key costs one connection.

## Relay

A Host may have one [Relay](record.md#a-relay), a Managed Host the Gateway
passes through to reach it: the Host connector logs in to the Relay as its
Host Account with the koko key, checking the Relay's pin, then reaches the
Host through a `direct-tcpip` channel there
([the Host connector](host-connector.md#connecting)). The caller need not
see the Relay. A message names it as `web1's Relay gw` only to a caller who
can see it, and as `web1's Relay` to one who cannot
([`host.Relay.Of`](../../internal/server/host/reach.go)), who learns
neither its fingerprints nor its Host Account:

| When | Banner, to a caller who can see the Relay | To one who cannot |
| --- | --- | --- |
| The Relay is down | `koko: web1's Relay gw is down or unreachable: koko could not connect to it; this SSH Session did not start.` | the same, `web1's Relay` |
| Its host key is not the pinned one | `koko: the host key of web1's Relay gw does not match the one pinned at Enroll (pinned SHA256:…, offered SHA256:…); koko did not log in. If gw was rebuilt, pin its new key with koko host enroll gw --host-key <…>, or with --replace-host-key, which trusts whatever key it offers.` | `koko: the host key of web1's Relay does not match the one pinned at Enroll; koko did not log in.` |
| It refused the koko key | `koko: web1's Relay gw refused the koko key for its Host Account "deploy"; this SSH Session did not start.` | `koko: web1's Relay refused the koko key; this SSH Session did not start.` |
| The connector failed otherwise | `koko: koko could not connect to web1's Relay gw; this SSH Session did not start.` | the same, `web1's Relay` |

Each is logged as `ssh session refused` at warning level with `relay_id`
and the reason `relay unreachable`, `relay host key mismatch` (with `pinned`
and `offered`), `relay login refused` or `relay connection failed`. A
failure past the Relay, such as the Host refusing the channel's connection,
is the Host's own. The Relay's connection has the Host's probes, retries and
ends ([when an SSH Session ends](#when-an-ssh-session-ends)).

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
([`terminal.go`](../../internal/server/gateway/terminal.go);
[ADR 0053](../adr/0053-a-browser-opens-ssh-sessions-through-a-websocket-to-the-gateway.md),
P1 spec: a terminal in the web UI). The web front reads a page's first
message on the WebSocket of `/terminal/socket`
([the web front](web-front.md#the-web-terminals-websocket)) and hands the
WebSocket over (`TerminalSocket`) with the caller (`TerminalCaller`): the
Sign-in, or on `web@koko` the Device Session or the Local caller and its
run of `koko web`, and the page's address.

- **The same registry.** A web terminal's SSH Session is a Tunnel in the
  registry, with its id, caller, admission, Host and Relay ids and
  ending, so `Close`, `Withdraw`, `recheck`, `keepEnding` and the limit
  serve it unchanged. Two fields tell it apart: its client (`ssh` or
  `browser`) and its grace.
- **Opening** (`OpenTerminal`). It is admitted as a Tunnel is (`admit`,
  the same refusals in the same order) and counted against its caller's
  limit. The Gateway then dials the Host through the Host connector, opens
  one `session` channel, sends a `pty-req` (`TERM` `xterm-256color`, the
  page's columns and rows, pixel sizes 0, and the mode `IUTF8`, opcode 42,
  so a line editor erases a whole Chinese character) and a `shell`, and
  only then answers the page with the SSH Session's id. Every refusal is
  that answer instead: the code the page tests and the banner a Tunnel's
  client gets, without `koko: ` and the newline, so an unseen Host gets
  the bytes of an unknown name. The Host refusing the session, its
  `pty-req` or its `shell` (its sshd policy) is `shell_refused`: `web1
  refused a shell with a terminal for its Host Account "deploy"; this SSH
  Session did not start.`
- **The relay** (`terminalShell`). The resumable stream runs on the
  WebSocket's binary messages and carries terminal records
  ([`wire/terminal.go`](../../internal/shared/wire/terminal.go)). The
  Host's stdout and stderr become data records, merged as a pty merges
  them; data records go to the shell's stdin, resize records become a
  `window-change`, pixel sizes 0, and the page's half-close the stdin's
  EOF. The Host's requests on the channel are refused, its
  `keepalive@openssh.com` among them, but for `exit-status` and
  `exit-signal`, which become records after the last output; the stream
  then closes with no reason (`shell ended`). Records from the Host's two
  streams, a notice and the exit record take turns. An ending with a notice
  writes it as a data line, `\r\nkoko: <notice>\r\n`, then closes the
  stream with the notice as its reason. Bytes from the page that are no
  record, or an exit record from it, end the SSH Session (`malformed
  record`), the stream's close saying why. The page's close frame is the
  client's close (`client closed`).
- **Detach and Resume** (`ResumeTerminal`), as a Tunnel's
  ([below](#detach-and-resume)): the WebSocket closing Detaches it, for
  `timing.BrowserGrace` (15 minutes), and its ending is kept as long. A
  resume is taken only from the caller who opened it, the same Sign-in,
  Device Session or run of `koko web`, and only once `recheckResume`
  passes; the Gateway answers with its received offset in the answer, not
  in the stream (`resumable.Stream.AcceptResumeAnswering`). The last
  attach wins. A resume of a Tunnel's id on a WebSocket, or of a web
  terminal's on `tunnel@koko`, is refused as an unknown id.
- **Ending on purpose** (`EndTerminal`). A page's `end` ends that SSH
  Session of the same caller as the client's close, cutting the WebSocket
  it may still seem to be on, since the page that held it is gone, and is
  answered once it has ended, so the old SSH Session no longer counts
  against the limit. Another caller's `end` ends nothing.
- **Liveness** (`watchSocket`). A page can neither send nor see a
  WebSocket's pings, so liveness is in the stream. The Gateway reads when
  the page's last byte arrived from a `liveness.Conn` around the
  WebSocket, and notes when the stream last sent: after `timing.ServerProbe`
  (15 s) without sending it asks the stream for an acknowledgement-only
  frame (`resumable.Stream.Probe`), and after `timing.ServerDead` (45 s)
  without a byte from the page it cuts the WebSocket, which Detaches the
  SSH Session. The page does the same with the same values. The 15-second
  traffic also keeps nginx, which cuts a quiet connection at 60 s by
  default, from cutting a terminal.
- **Stopping** (`CloseTerminals`). The web listener's shutdown does not
  track a WebSocket, which it has given up, so `server.Close` first has the
  Gateway end every web terminal with the restart notice, which closes its
  WebSocket, and refuse new ones; a WebSocket still at it after
  `timing.StopNotice` (5 s), a page too stalled to take the notice, is cut.
  The web listener follows, then the Tunnels and the transport, as below.

## Detach and Resume

Only the device's leg Resumes
([ADR 0021](../adr/0021-an-ssh-session-survives-a-network-drop-by-resuming-its-tunnel.md));
the Gateway's side is [`resume.go`](../../internal/server/gateway/resume.go).

- **Detached.** When a Tunnel's channel fails, as when the koko server
  declares its Device Connection dead (`timing.ServerDead`, 45 s, [the
  Device Connection](device-connection.md#the-tcp-listener)), or the
  connection closes, the Tunnel is Detached (`ssh session detached`). Its SSH
  Session and the Host connection carry on: what the Host sends waits in the
  stream's replay buffer, which blocks the relay once full
  ([the resumable stream](resumable-stream.md)), and the Host's keepalives
  are answered as above. The grace, `timing.Grace` (1 minute), or a web
  terminal's `timing.BrowserGrace` (15 minutes), starts then; when it runs
  out, the Tunnel ends (`grace over`). A Detached Tunnel still counts
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
([below](#access-during-a-live-ssh-session)), or the koko server stops.
When the Host's connection ended first, what the
Host sent before, an `exit-status` included, is passed on to the client
first. Some ends come with a notice (`ending`): the Gateway writes it as one
`koko: ` line on the stderr of each session channel the client has open, so
ssh shows it in its own output, then closes the Tunnel's stream with the
notice as its close frame's reason, for a client with no session open, such
as one still in its handshake. It then closes both connections and takes
the Tunnel out of the registry.

| End | Notice | `reason` in the log |
| --- | --- | --- |
| The client closed its connection, or a web terminal's page its stream or ended it | none | `client closed` |
| A web terminal's shell ended, with its exit record | none | `shell ended` |
| A web terminal's page sent bytes that are no record | none; the stream's close says why | `malformed record` |
| The Host leg broke: the [Host connector](host-connector.md#keeping-a-connection-alive) heard nothing for 60 s, its probes unanswered | `lost the connection to web1 (no answer for 60 s); this SSH Session has ended.` | `Host leg lost` |
| The Host, or the network beneath, closed its connection | `web1 closed the connection.` | `Host closed` |
| The Relay leg broke, as the Host leg's rows above | `lost the connection to web1's Relay gw (no answer for 60 s); this SSH Session has ended.`, or `web1's Relay gw closed the connection.`, the Relay unnamed to a caller who cannot see it | `Relay leg lost`, `Relay closed` |
| The koko server stops cleanly, as for an upgrade | `the koko server is restarting; this SSH Session has ended.` | `server stopping` |
| The Tunnel stayed Detached for the whole grace | none | `grace over` |
| Its Device Session was Revoked | `access to web1 was withdrawn: this device's Device Session was Revoked; this SSH Session is closed.` | `revoked` |
| Its User was Removed | `access to web1 was withdrawn: bob was Removed; this SSH Session is closed.` | `user removed` |
| The Host was Retired | `access to web1 was withdrawn: web1 was Retired; this SSH Session is closed.` | `host retired` |
| The Host went back to Registered | `access to web1 was withdrawn; this SSH Session is closed.` | `host registered` |
| Its Relay is no longer Managed | `access to web1 was withdrawn: web1's Relay gw is no longer Managed; this SSH Session is closed.`, the Relay unnamed to a caller who cannot see it | `relay registered`, `relay retired` |
| The caller can no longer act in the Host's Space: they were taken out of it, or it was moved to one they cannot act in | `access to web1 was withdrawn; this SSH Session is closed.` | `access withdrawn` |
| The koko server could not read the record to check access again | `the koko server could not check access to web1, so this SSH Session is closed; its log says why.` | `access check failed` |

A channel the Host's connection took down is left open for the notice
(`Conn.Broken` tells it from one the Host closed), and its EOF and close
follow it. The Gateway waits for the notice's writes until they are done or
the Tunnel's stream is off its channel, which a stalled client's never
takes them from.

On a clean stop, [`server.Close`](../../internal/server/server.go) first
ends the web terminals ([above](#the-web-terminal)), then closes the web
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
- **Any other change** names the Hosts it may have put out of a caller's
  reach (`host.move` to another Space, `host.retire`, and `host.set` sending
  a Managed Host back to Registered), or the live Device Sessions of each
  User `space.member.remove` took out of a Space. The Gateway checks each
  SSH Session to one of those Hosts, each through any Relay, since a named
  Host may be its Relay, or of one of those Device Sessions again, in one read-only transaction, by the
  rule that admits a new one
  ([`host.ReachOf`](../../internal/server/host/reach.go) by the Host's id,
  so a rename changes nothing, and the Host and its Relay Managed), and ends
  each one it would now refuse. A Relay sent back to Registered, or Retired
  once no Host uses it, ends every SSH Session still passing through it,
  and so does the Host's Relay now when it stops being Managed: a Host
  given another Relay, or none, keeps its live SSH Sessions on the Relay
  they started through while both are Managed
  ([`relayWithdrawal`](../../internal/server/gateway/withdraw.go)). A Retired Host is named as such only to a caller who
  can still see it.
- **The notice names the Host** by the name the SSH Session was opened
  with, and says nothing more for a Host the caller can no longer see, so it
  tells of no later name and of no Space.
- **Changes that keep access** publish nothing or pass the check, and
  interrupt nothing: a rename, the address, port, Note and Attributes, a
  move to a Space the caller still acts in, another Device Session's
  revocation.
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
`koko web` keeps `device_session`. A web terminal's `addr` is the page's:
the client's address as the web listener counts it (the last
`X-Forwarded-For` entry), or `koko web`'s connection's.

- `ssh session started`: `tunnel`, `client`, `user`, `device_session` or
  `signin`, `host`, `host_id`, `relay` and `relay_id`
  for a Host with a Relay, whether or not the caller can see it, and `addr`,
  where the connection came from.
- `ssh session request`: `tunnel`, `client`, `host` and `request`, once per SSH
  Session for each kind: `shell` (a web terminal's, once its shell starts), `exec` (never its text), `subsystem` with
  `subsystem`, its name, and each forward's `target`: `-L` with the address
  on the Host's side a `direct-tcpip` (`-L` or `-D`) or
  `direct-streamlocal@openssh.com` channel opens, `-R` with the address a
  `tcpip-forward` or `streamlocal-forward@openssh.com` asks the Host to
  listen on. A busy forward is logged once per target.
- `ssh session ended`: the same as the start but `addr`, then `duration`,
  `bytes_from_device` and `bytes_to_device` (the Tunnel's bytes each way),
  `exit_status` and `exit_signal` when the Host sent them, and `reason`, as
  the table above says.
- `ssh session refused`: `tunnel`, `client`, `name` (the SSH Name asked for),
  `device_session` or `signin`, `addr` and `reason`: `unknown host`, the access check's
  code, `host registered`, `relay registered`, `ssh session limit`, `user name` with `ssh_user`,
  the name asked for, `shell refused` with `request` or `err`; and at warning level, since the Owner may have to act,
  `host unreachable`, `login refused` or `host connection failed` with
  `err`, and `host key mismatch` with `pinned` and `offered`, the two
  fingerprints, and the Relay's own ([Relay](#relay)). A client that leaves before its `none`, or a page before its answer, is logged at debug
  level as `ssh session ended before it started`.
- `ssh session detached`: `tunnel`, `client` and `device_session` or
  `signin`, when the grace starts.
- `ssh session resumed`: `tunnel`, `client`, `device_session` or `signin`,
  and `addr`, where the
  resume came from: on TCP, a new address at each Device Connection.
- `ssh session resume refused`: `tunnel` (the id the resume named),
  `client`, `device_session` or `signin`, `addr` and `reason`: how the Tunnel ended, as its
  `ssh session ended` line says, or `unknown tunnel`; `ssh session
  resume failed`, the same with `err`, for one that failed once accepted.
- `tunnel refused`: `device_session`, `addr` and `err`, for a payload the
  Gateway does not take, and with `name` before initialisation.
- `open a Tunnel`, at error level, with `device_session`, `addr`, `name`
  and `err`, when the koko server could not read what admitting a Tunnel
  needs; the channel is refused saying the log tells why. `open a web
  terminal` is the same for a web terminal, which is answered
  `internal_error`.
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
writes a web terminal's records from the Host's stdout and stderr beside a
notice while the stream's buffer fills, which fails unless they take
turns, each whole and the notice last; and counts SSH Sessions by the
caller's id, 16 for a Sign-in, 64 for a Device Session from both clients,
none for the Local caller.
[`web/terminal_test.go`](../../internal/server/web/terminal_test.go)
drives the web terminal on the web listener with a Go page
(`harness.TerminalPage`, `harness.WebTerminal`) against in-process Hosts:
the `pty-req` and `shell` as the Host Account, 4 MiB each way byte for
byte, a resize as a `window-change`, the exit status and signal records;
each refusal of an open with its code and banner, an unseen and a Retired
Host as an unknown name, without a dial; the 17th SSH Session of a
Sign-in, and one more once an `end` is answered; a resume that wins over a
WebSocket a stalled path still holds and one after a drop, byte for byte,
and another Sign-in's refused; an idle WebSocket kept by both ends'
probes, a stalled one Detached no sooner than `ServerDead` after its last
byte, and a cut one at once, each Resumed byte for byte
(`harness.Forward`); the page's close, an `end` and another caller's, and
`BrowserGrace`; a malformed record; the restart notice before the web
listener closes, while a call in flight holds it, and a stalled page cut
at `StopNotice`; and the log lines, with no cookie in them.
[`wire/tunnel_test.go`](../../internal/shared/wire/tunnel_test.go) pins the
`open` and `resume` payloads' bytes.
