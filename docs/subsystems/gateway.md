# The Gateway, the Tunnel, `koko exec` and `koko tunnel`

The Gateway ends every SSH Session and logs in to the Host with the koko
key, which never leaves the koko server
([ADR 0002](../adr/0002-every-ssh-session-ends-at-the-gateway.md)). Each SSH
Session rides in a Tunnel: one `tunnel@koko` channel carrying a
[resumable stream](resumable-stream.md)
([ADR 0021](../adr/0021-an-ssh-session-survives-a-network-drop-by-resuming-its-tunnel.md)).
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
  example, `00 00 00 04 "open" 00 00 00 09 "web1.koko"`. A payload that is
  no kind, or an `open` with anything but one SSH Name, is refused at the
  channel open; so is any other kind, until the Gateway takes it (`resume`
  comes with resumption).
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
   is allowed when the caller may act in the Host's Space and the Host is
   Managed.
3. It accepts the channel, puts the Tunnel in the registry under a new id,
   and sends the id. An allowed SSH Session of a Device Session counts
   against its limit, 64 at once, attached or Detached (`maxSSHSessions`),
   from then until its Tunnel ends; the 65th is refused, and dials no Host.
   The Local caller has no Device Session and no limit.
4. When the SSH Session is allowed, it dials the Host through the
   [Host connector](host-connector.md), as the Host Account with the koko key
   and the Host's pinned host key, while the client's SSH handshake runs.
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
| The access check refuses the caller, such as a Revoked Device Session | `koko: ` and the refusal's message |
| The Device Session has 64 SSH Sessions open | `koko: this Device Session already has 64 SSH Sessions open, the most koko allows; this SSH Session did not start.` |
| Another user name, such as `root@web1.koko` | `koko: web1 is reached as its Host Account "deploy"; leave out "root@".` |
| The Host is down: the connector's 3 attempts each met a network error | `koko: web1 is down or unreachable: koko could not connect to it; this SSH Session did not start.` |
| The Host's host key is not the pinned one | `koko: the host key of web1 does not match the one pinned at Enroll (pinned SHA256:…, offered SHA256:…); koko did not log in. If web1 was rebuilt, pin its new key with koko host enroll web1 --host-key <the fingerprint its provider shows>, or with --replace-host-key, which trusts whatever key it offers.` |
| The Host refused the koko key | `koko: web1 refused the koko key for its Host Account "deploy"; this SSH Session did not start.` |
| The connector failed otherwise | `koko: koko could not connect to web1; this SSH Session did not start.` |

The first row is the same byte for byte for a name nobody holds and for a
Host the caller cannot see, whatever user name they ask for, so the Gateway
tells nothing of Hosts outside the caller's Spaces; the user name is
checked only for a Host they may reach. The connector's error, which names
addresses, goes to the log only. Only a network error is retried
([the Host connector](host-connector.md#retries-and-failures)), so a host
key mismatch or a refused key costs one connection.

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
The session carries on.

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
other channel the Host opens.

## When an SSH Session ends

An SSH Session ends when the client closes its connection, the Host's
connection ends, the Tunnel's channel fails (nothing Resumes a Tunnel yet),
its access is withdrawn ([below](#access-during-a-live-ssh-session)), or the
koko server stops. When the Host's connection ended first, what the
Host sent before, an `exit-status` included, is passed on to the client
first. Some ends come with a notice (`ending`): the Gateway writes it as one
`koko: ` line on the stderr of each session channel the client has open, so
ssh shows it in its own output, then closes the Tunnel's stream with the
notice as its close frame's reason, for a client with no session open, such
as one still in its handshake. It then closes both connections and takes
the Tunnel out of the registry.

| End | Notice | `reason` in the log |
| --- | --- | --- |
| The client closed its connection | none | `client closed` |
| The Host leg broke: the [Host connector](host-connector.md#keeping-a-connection-alive) heard nothing for 60 s, its probes unanswered | `lost the connection to web1 (no answer for 60 s); this SSH Session has ended.` | `Host leg lost` |
| The Host, or the network beneath, closed its connection | `web1 closed the connection.` | `Host closed` |
| The koko server stops cleanly, as for an upgrade | `the koko server is restarting; this SSH Session has ended.` | `server stopping` |
| The Tunnel's channel failed | none | `device connection lost` |
| Its Device Session was Revoked | `access to web1 was withdrawn: this device's Device Session was Revoked; this SSH Session is closed.` | `revoked` |
| Its User was Removed | `access to web1 was withdrawn: bob was Removed; this SSH Session is closed.` | `user removed` |
| The Host was Retired | `access to web1 was withdrawn: web1 was Retired; this SSH Session is closed.` | `host retired` |
| The Host went back to Registered | `access to web1 was withdrawn; this SSH Session is closed.` | `host registered` |
| The caller can no longer act in the Host's Space: they were taken out of it, or it was moved to one they cannot act in | `access to web1 was withdrawn; this SSH Session is closed.` | `access withdrawn` |
| The koko server could not read the record to check access again | `the koko server could not check access to web1, so this SSH Session is closed; its log says why.` | `access check failed` |

A channel the Host's connection took down is left open for the notice
(`Conn.Broken` tells it from one the Host closed), and its EOF and close
follow it. The Gateway waits for the notice's writes until they are done or
the Tunnel's stream is off its channel, which a stalled client's never
takes them from.

On a clean stop, [`server.Close`](../../internal/server/server.go) first
closes the Gateway, which ends every Tunnel with the restart notice, and
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
  SSH Session to one of those Hosts or of one of those Device Sessions
  again, in one read-only transaction, by the rule that admits a new one
  ([`host.ReachOf`](../../internal/server/host/reach.go) by the Host's id,
  so a rename changes nothing, and the Host Managed), and ends each one it
  would now refuse. A Retired Host is named as such only to a caller who
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

One line per event, never an exec's command text:

- `ssh session started`: `tunnel`, `user`, `device_session` (the Device
  Session's id, or `unix socket`), `host`, `host_id` and `addr`, where the
  connection came from.
- `ssh session request`: `tunnel`, `host` and `request`, once per SSH
  Session for each kind: `shell`, `exec` (never its text), `subsystem` with
  `subsystem`, its name, and each forward's `target`: `-L` with the address
  on the Host's side a `direct-tcpip` (`-L` or `-D`) or
  `direct-streamlocal@openssh.com` channel opens, `-R` with the address a
  `tcpip-forward` or `streamlocal-forward@openssh.com` asks the Host to
  listen on. A busy forward is logged once per target.
- `ssh session ended`: the same as the start but `addr`, then `duration`,
  `bytes_from_device` and `bytes_to_device` (the Tunnel's bytes each way),
  `exit_status` and `exit_signal` when the Host sent them, and `reason`, as
  the table above says.
- `ssh session refused`: `tunnel`, `name` (the SSH Name asked for),
  `device_session`, `addr` and `reason`: `unknown host`, the access check's
  code, `host registered`, `ssh session limit`, `user name` with `ssh_user`,
  the name asked for; and at warning level, since the Owner may have to act,
  `host unreachable`, `login refused` or `host connection failed` with
  `err`, and `host key mismatch` with `pinned` and `offered`, the two
  fingerprints. A client that leaves before its `none` is logged at debug
  level as `ssh session ended before it started`.
- `tunnel refused`: `device_session`, `addr` and `err`, for a payload the
  Gateway does not take, and with `name` before initialisation.
- `open a Tunnel`, at error level, with `device_session`, `addr`, `name`
  and `err`, when the koko server could not read what admitting a Tunnel
  needs; the channel is refused saying the log tells why.
- `access checked again`, at debug level, with `ssh_sessions`, how many
  live SSH Sessions an access change had checked again, and `ended`, how
  many of them it ended.
- `check access during an SSH Session`, at error level, with `err`, when
  the koko server could not read the record to check live SSH Sessions
  again after an access change; each ends with `reason="access check
  failed"`.

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
- It exits with the command's status, and 255 when there is none or koko
  itself fails, as ssh does: a command a signal ended (it names the signal),
  a refused command line, no koko server, a refused SSH Session, a lost
  Tunnel, or an interruption ([the CLI](cli.md#the-runner-and-the-output-layer)).
- When the Gateway ends the SSH Session with a notice, such as that the
  koko server is restarting, the notice's line is on its stderr already, as
  the session's, so it exits 255 and adds nothing; a notice that did not
  reach the session, one still opening, say, is its error.
- It forwards no signal: Ctrl-C ends the SSH Session and says so.

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
  included, it exits 0. When the Tunnel's channel fails, it prints `koko: lost
  the connection to the koko server at 12:01:07; the SSH Session to web1
  could not be resumed` and exits 1 (nothing Resumes a Tunnel yet).
- **stdin's EOF** half-closes the Tunnel; koko tunnel relays what the
  Gateway still sends until the Gateway closes it.
- **Signals**, through `cli.Runner.Signals`, which `cmd/koko` sets to
  `signal.Notify`: it ignores SIGINT, which ssh also gets when Ctrl-C is
  pressed without a tty, and which ends every other command's context; on
  SIGHUP, which ssh sends its ProxyCommand when it exits, or SIGTERM, it
  sends what is unsent and a close frame, then exits 0, so the Gateway ends
  the SSH Session as the client's close (`reason="client closed"`).
- A running koko tunnel stays connected to the Device Mux, so an open SSH
  Session keeps the Device Mux from idling out.

## Verifying

[`tunnel_test.go`](../../internal/client/cli/tunnel_test.go) drives `koko
tunnel` with an `x/crypto/ssh` client over its stdio (`h.Proxy`,
`d.Proxy`), against the S1 koko server and in-process Hosts: exec, a shell
with env, a pty and a window change, a signal and the sftp subsystem; `-L`
and `-D` to a TCP port, `-L` to a Unix socket, and `-R` from a TCP port and
a Unix socket with their cancels; each failure before the Device Connection
as one line (golden files `tunnel-*`; unreachable also through the Device
Mux), and the lost Tunnel's line; stdin's EOF; SIGINT ignored; SIGHUP and
SIGTERM sending a close frame, over the Unix socket, a direct Device
Connection and the Device Mux; the Device Mux kept from idling out; and the
command line, help, skill and completion.

[`exec_test.go`](../../internal/client/cli/exec_test.go) drives `koko exec`
against the S1 koko server and in-process Hosts: the command's stdin,
stdout, stderr and status, the words as the Host ran them, no pty, the koko
key as the Host Account; through the Device Mux, opening no Device
Connection of its own; both forms of the SSH Name; an unknown name and an
unseen Host refused alike with no connection to the Host, a Registered Host
refused, a Retired Host's old name refused as unknown; 255 for each failure
of koko's own and for a command a signal ended; Ctrl-C; a koko server stop
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
[`wire/tunnel_test.go`](../../internal/shared/wire/tunnel_test.go) pins the
open payload's bytes.
