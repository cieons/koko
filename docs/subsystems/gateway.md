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
   and sends the id.
4. When the SSH Session is allowed, it dials the Host through the
   [Host connector](host-connector.md), as the Host Account with the koko key
   and the Host's pinned host key, while the client's SSH handshake runs.
5. The client's `none` waits for the dial. Every refusal is a banner on the
   refused `none`, which `koko exec` prints as its error and ssh in its own
   output:

| When | Banner |
| --- | --- |
| No Host the caller can see has the name: none has it, it is in a Space the caller cannot act in, or the SSH Name is not a Host name. A Retired Host holds no name. | `koko: no Host is named "web1".` |
| The Host is Registered | `koko: web1 is Registered: koko logs in to it only once it is Enrolled, with koko host enroll web1.` |
| The access check refuses the caller, such as a Revoked Device Session | `koko: ` and the refusal's message |
| The Host connector failed: unreachable after its retries, the login refused, or a host key mismatch | `koko: koko could not connect to web1; this SSH Session did not start.` |

The first row is the same byte for byte for a name nobody holds and for a
Host the caller cannot see, so the Gateway tells nothing of Hosts outside
the caller's Spaces. The connector's error, which names addresses, goes to
the log only.

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

An SSH Session ends when the client closes its connection, the Host closes
its own, the Tunnel's channel fails (nothing Resumes a Tunnel yet), or the
koko server stops. When the Host closed first, what it sent before, an
`exit-status` included, is passed on to the client first. The Gateway then
closes both connections and the stream, and takes the Tunnel out of the
registry. A koko server stop closes the Gateway beside the transport, so a
connection too stalled to take a Tunnel's close cannot hold it up.

## Logging

One line per event, never an exec's command text:

- `ssh session started`: `tunnel`, `user`, `device_session` (the Device
  Session's id, or `unix socket`), `host`, `host_id` and `addr`, where the
  connection came from.
- `ssh session ended`: the same but `addr`, then `duration`,
  `bytes_from_device` and `bytes_to_device` (the Tunnel's bytes each way),
  `exit_status` and `exit_signal` when the Host sent them, and `reason`:
  `client closed`, `Host closed`, `device connection lost` or
  `server stopping`.
- `ssh session refused`: `tunnel`, `name` (the SSH Name asked for),
  `device_session`, `addr` and `reason`; at warning level, with `err`, when
  the Host connector failed. A client that leaves before its `none` is
  logged at debug level as `ssh session ended before it started`.
- `tunnel refused`: `device_session`, `addr` and `err`, for a payload the
  Gateway does not take, and with `name` before initialisation.
- `open a Tunnel`, at error level, with `device_session`, `addr`, `name`
  and `err`, when the koko server could not read what admitting a Tunnel
  needs; the channel is refused saying the log tells why.

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
  output, and koko tunnel prints nothing. When the Gateway closes the
  Tunnel it exits 0. When the Tunnel's channel fails, it prints `koko: lost
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
of koko's own and for a command a signal ended; Ctrl-C; a koko server stop; the log lines without the command's
text; and a Tunnel counting as a use of its Device Session.
[`wire/tunnel_test.go`](../../internal/shared/wire/tunnel_test.go) pins the
open payload's bytes.
