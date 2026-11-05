# How does an SSH Session through koko survive a network drop on the device?

Research note for issue #20. Checked on 2026-11-05 against the source of
Teleport (`gravitational/teleport` at master `1283425`, newest commit
2026-08-25, and the release tags v14.3.0 to v18.11.1), Eternal Terminal
(`060b18e`, 2026-11-04; release et-v7.1.1), mosh (`decd9b7`, 2026-03-06;
release 1.4.0), quic-go (`4ba9d8a`, 2026-11-04; release v0.65.0),
`golang.org/x/crypto` v0.58.0, openssh-portable (`3c6ec4c`, 2026-11-03), Go
1.27.1, Tailscale's `net/netmon`, Coder's reconnecting PTY and Microsoft's
dev-tunnels-ssh protocol notes; RFC 9000, RFC 4253, the AGPL-3.0 text, the
Linux `ip-sysctl` documentation; and searches of GitHub and pkg.go.dev. Each
claim links to what it rests on. Observations made on this machine (macOS
27.0.1, Go 1.27.1, `OpenSSH_10.3p1`) are marked as local and list the exact
command in [Local observations](#local-observations).

## Question

When a laptop sleeps or changes network, a live SSH Session through koko (an
interactive shell, `koko exec`, scp, sftp, rsync, `-L`, `-D` and `-R`
forwards) must carry on once the device reconnects within a grace period of a
few minutes. The Gateway already holds its own connection to the Host
([ADR 0002](../adr/0002-every-ssh-session-ends-at-the-gateway.md)), so only
the device-to-koko-server leg has to survive. That leg is the OpenSSH client
on stdio to `koko tunnel`, a local socket to the Device Mux, and the Device
Mux's one Device Connection over TCP 443 to the koko server, which hands
each Tunnel to the Gateway's inner SSH server
([ADR 0012](../adr/0012-ssh-end-to-end-and-the-web-ui-through-the-cli.md),
[ADR 0015](../adr/0015-the-api-is-http-and-json-in-ssh-channels.md),
[ADR 0017](../adr/0017-a-device-mux-holds-the-device-connection.md)). Today
the Device Mux exits when its Device Connection drops, and every SSH Session
on it ends.

Constraints: pure Go with `CGO_ENABLED=0`
([ADR 0001](../adr/0001-go-single-binary-with-sqlite.md)); the OpenSSH
client unchanged; one Device Connection per device; revoking a Device Session
or removing a User ends that device's SSH Sessions at once, detached ones
included ([ADR 0013](../adr/0013-devices-join-with-a-one-time-join-code.md));
a licence compatible with koko's Apache-2.0; SSH over WSS on the same port
as the P1 fallback and QUIC as P2.

What should koko adopt, and what does it change in the Tunnel, the Device
Mux and the Gateway?

## Summary and recommendation

**Adopt Teleport's architecture, not its code: a resumable byte stream per
Tunnel, beneath the SSH Session and above the Device Connection, whose two
ends sit where the SSH Session's two ends sit: `koko tunnel` (and `koko
exec`) on the device, the Gateway on the koko server.** This is the shape
Teleport ships: its OpenSSH ProxyCommand, `tsh proxy ssh`, holds one end of
a resumable byte stream and the Teleport SSH service holds the other, with
Teleport's own connections in between re-dialed after a drop ([[12]](#t12),
[[5]](#t5)). In koko, after a drop the Device Mux opens a new Device
Connection, authenticated by the device's key like any other, and each
Tunnel re-attaches by its id and the byte offset it has received; each end
then resends what the other lacks. A Revoked key cannot open that Device
Connection, so revocation needs no new mechanism beyond closing the detached
Tunnels it already owns.

No library can be imported. Teleport's `lib/resumption` is AGPL-3.0 in every
release that contains it ([[1]](#t1)–[[4]](#t4)). Eternal Terminal is C++
and its design differs ([[16]](#e1)–[[22]](#e7)). The Go candidates found are
weeks old, have no users, or have no licence ([[31]](#g1)). So koko writes
the layer itself, from Teleport's published design and this note: by
Teleport's sizes, about 1,000 to 1,500 lines of Go plus tests ([[14]](#t14);
an estimate).

| Setting | Value | Basis |
| --- | --- | --- |
| Grace period | 5 minutes, one koko server setting, counted from the moment the koko server sees the Tunnel detach | Teleport 1 min ([[6]](#t6), [[8]](#t8)); ET none by default, one week for editors ([[22]](#e7)); mosh none ([[25]](#m3)); the user's "a few minutes" |
| Replay buffer | at most 2 MiB per direction per Tunnel, grown on demand; a full buffer blocks the writer | Teleport's 2 MiB send buffer ([[10]](#t10)); the 2 MiB SSH channel window bounds what an idle peer can owe ([[35]](#x1), [[41]](#o5)) |
| Throughput cap it implies | 2 MiB / RTT per Tunnel: 50 MiB/s at 40 ms, 20 MiB/s at 100 ms, 8 MiB/s at 250 ms | the same cap one SSH channel already has; the prototype measured 36 to 39 MiB/s at 40 ms RTT (README on branch `prototype/ssh-tunnel`) |
| Frame size | at most 32 KiB of data per frame | one SSH channel packet ([[35]](#x1)); Teleport allows 128 KiB ([[9]](#t9)) |
| Dead Device Connection | the Device Mux probes with `keepalive@openssh.com` after 5 s without inbound bytes and drops the Device Connection after 15 s without any; it also probes at once on a wall-clock jump or a route change | ET drops after one missed 5 s keepalive ([[21]](#e6)); Go's TCP keepalive alone takes 150 s ([[43]](#r2)); Linux retransmission 924.6 s ([[44]](#r3)) |
| koko server side | probes each Device Connection after 15 s without inbound bytes, drops it after 45 s | a proposal: it starts the grace timer for a device that vanished without closing its TCP connection |
| Reconnect | first attempt at once, then backoff from 250 ms doubling to 5 s, until the koko server answers | Teleport 50 ms doubling to 10 s ([[8]](#t8)); ET every 1 s ([[20]](#e5)) |
| Re-attach exchange | 5 s timeout | Teleport's `handshakeTimeout` ([[9]](#t9)) |
| `ssh_config` | `ServerAliveInterval 0` in koko's `Host *.koko` block (needs a decision) | a user's `ServerAliveInterval 15` would end the SSH Session after about 60 s of silence ([[38]](#o2), local [[47]](#l2)) |

What changes, per component:

- **The Tunnel (`tunnel@koko`).** Its bytes are framed: each frame carries
  how many bytes the sender has received since its last acknowledgement,
  then a length and data, as in Teleport's frames ([[9]](#t9)). On accepting
  a Tunnel the koko server sends a first frame holding the Tunnel's id, 128
  random bits. A re-attach is a Tunnel opened with that id and the received
  offset instead of a Host name (a field on `tunnel@koko` or a channel type
  of its own); the koko server answers with its own offset, and both ends
  resend from the other's offset. A close frame ends the Tunnel for good. The
  koko server keeps a Tunnel only for the Device Session that opened it.
- **`koko tunnel` and `koko exec`.** They hold the device end: the bytes
  from ssh not yet acknowledged by the Gateway, and the count of bytes
  received from it. When the Device Mux reports a drop, or dies, they keep
  ssh's stdio open and re-attach through the Device Mux once it has
  reconnected; if no Device Mux of their build can run, through a Device
  Connection of their own, the fallback ADR 0017 already has. They exit,
  with a message, when the koko server says the Tunnel is gone or the Device
  Session is Revoked. One Go package serves both commands.
- **The Device Mux.** It no longer exits when its Device Connection drops.
  It detects a dead one (table above), reconnects, and tells its local
  clients to re-attach. It stays a byte relay for Tunnels and keeps no
  replay state. API calls in flight fail with an error the command can
  retry; `koko web`'s browser connections close and the browser opens new
  ones. A local client waiting to re-attach counts as an open channel for
  the ten-minute idle exit.
- **The Gateway.** Its inner SSH server runs on the resumable conn, not
  directly on the Tunnel, so it outlives a Device Connection. A registry maps
  Tunnel id to that conn, its Device Session, its Host and its grace timer.
  It answers the Host's `keepalive@openssh.com` channel requests itself
  instead of relaying them to the OpenSSH client ([[40]](#o4)). Revoking a
  Device Session, removing a User, or a Member leaving the Host's Space
  closes the SSH Sessions of that Device Session, attached or detached; a
  re-attach checks access again.
- **The koko server.** SSH Sessions are indexed by Device Session, not by
  Device Connection, so revocation reaches detached ones. When a Device
  Connection dies, its Tunnels detach and their grace timers start; a
  re-attach on a newer Device Connection detaches a Tunnel from an older one
  that has not died yet.
- **koko's `ssh_config` block.** `ServerAliveInterval 0`, if the decision
  below goes that way.

Decisions that need the user, not #16:

1. **koko writes the layer.** No adoptable library exists, so "adopt, don't
   build" can only mean adopting Teleport's proven design. Teleport's code is
   AGPL-3.0 and cannot be copied into Apache-2.0 koko ([[1]](#t1)–[[4]](#t4),
   [[15]](#t15)). Should the implementation be written from Teleport's RFD
   and this note, by someone who does not work from the AGPL source?
2. **The grace period.** 5 minutes is proposed. Teleport chose 1 minute; ET,
   mosh and tsshd keep a detached terminal for days or forever
   ([[22]](#e7), [[25]](#m3), [[26]](#m4)). Longer costs only memory and
   Host connections held open, and revocation still ends them at once.
3. **Overriding the user's `ServerAliveInterval` for `.koko` names.** The
   issue #6 note left it as the user's choice. Without an override, a user
   who sets `ServerAliveInterval 15` gets about 60 seconds of grace, not 5
   minutes ([[38]](#o2), local [[47]](#l2)).

## Teleport's SSH connection resumption

### Where it lives and when it shipped

- <a id="t1"></a>[1] Teleport moved from Apache-2.0 to AGPL-3.0 in commit
  `b178b8b` "Updated Teleport codebase to AGPL3 license (#35259)",
  2023-12-01; the first release containing it is v15.0.0
  (<https://github.com/gravitational/teleport/commit/b178b8b7322e7c6348bbd17ed1d3251ad34d946b>;
  `git tag --contains b178b8b`).
- <a id="t2"></a>[2] `lib/resumption` was first committed on 2023-12-21, three
  weeks after that: `4f77140` "Add `resumption.managedConn` (#34938)". Its
  `managedconn.go` carried an Apache-2.0 header while the repository's
  `LICENSE` at that commit was already AGPL-3.0; the next day `54d0ff7`
  "fix wrong license blurb added in 4f771403dc (#35983)" replaced it with the
  AGPL header. The wire protocol (`c394e81`, 2024-01-18) and the SSH wiring
  (`d25385d`, 2024-01-19) were AGPL from their first commit
  (<https://github.com/gravitational/teleport/commit/4f771403dc4177dc26ee0370f7332f3fe54bee0f>,
  <https://github.com/gravitational/teleport/commit/54d0ff7e3f1c427609c554920d9c1a82e8d88b14>,
  <https://github.com/gravitational/teleport/commit/c394e81c909ae20189e14e969a1939b188b1baef>,
  <https://github.com/gravitational/teleport/commit/d25385dc3b782d4a5eac4eb8f35014a75b0f57b5>).
- <a id="t3"></a>[3] Release: no v14.3.x tag has `lib/resumption`;
  v15.0.0-alpha.1 has only `managedconn.go`; v15.0.0 (2024-01-31) has the
  whole package, and every tag since (v15.4.0, v16.0.0, v17.0.0, v18.0.0,
  v18.11.1) has it (`git ls-tree <tag> lib/resumption/`). The 15.0.0
  changelog: "Teleport v15 introduces automatic SSH connection resumption if
  the network path between the client and the Teleport node is interrupted
  ... The feature is active by default when a v15 client (`tsh`, OpenSSH or
  PuTTY configured by `tsh config`, or Teleport Connect) connects to a v15
  Teleport node"
  (<https://github.com/gravitational/teleport/blob/v15.0.0/CHANGELOG.md#L40-L49>).
- <a id="t4"></a>[4] Since v16: "The Teleport API module (all code in this
  repository under `/api`) is available under the Apache 2.0 license. The
  remainder of the source code in this repository is available under the
  GNU Affero General Public License ... Teleport Community Edition builds
  distributed on http://goteleport.com/download are available under a
  modified Apache 2.0 license"
  (<https://github.com/gravitational/teleport/blob/v16.0.0/README.md#L396-L408>).
  That licence, added in `1d0c382` "Add community edition license for
  Teleport 16+ (#41054)" (2024-05-03), is "AVAILABLE ONLY TO ORGANIZATIONS
  THAT HAVE LESS THAN ONE HUNDRED (100) EMPLOYEES AND LESS THAN TEN MILLION
  DOLLARS ($10,000,000.00) IN ANNUAL REVENUE"
  (<https://github.com/gravitational/teleport/blob/v16.0.0/LICENSE-community#L1-L7>).
  It covers binaries, not source. `api/` holds nothing about resumption
  (`git ls-tree -r origin/master api/ | grep -i resum` is empty), and
  `LICENSE` is AGPL-3.0 at master.
- <a id="t5"></a>[5] RFD 0150, "SSH connection resumption", state
  implemented. Goals: survive a restart of Teleport's front-end service and
  "general loss of end-to-end connectivity", for shells, command execution,
  port forwarding and OpenSSH through `tsh proxy ssh`. It rejects Microsoft's
  `session-reconnect@microsoft.com` SSH extension because it would need a
  fork of `golang.org/x/crypto/ssh` and client support, so OpenSSH and PuTTY
  could not use it. The rest of this section's protocol prose is from it
  (<https://github.com/gravitational/teleport/blob/1283425b60ec5f60d509ba4c791183d452923ff7/rfd/0150-ssh-connection-resumption.md#L13-L49>).

### How a server advertises it, and why plain SSH clients are unaffected

- <a id="t6"></a>[6] The server's first bytes are its SSH identification
  string with the advertisement in the comments field: `SSH-2.0-Teleport
  resume-v1 <base64 P-256 ECDH public key> <host ID>\r\n`. A resumption
  client answers `SSH-2.0-\x00teleport-resume-v1`. Anything else is a plain
  SSH client, and the connection goes to the SSH server with the version
  line already sent. `detachedTimeout = time.Minute`
  (<https://github.com/gravitational/teleport/blob/1283425b60ec5f60d509ba4c791183d452923ff7/lib/resumption/server_detect.go#L38-L60>,
  <https://github.com/gravitational/teleport/blob/1283425b60ec5f60d509ba4c791183d452923ff7/lib/resumption/server_detect.go#L134-L177>;
  RFD [[5]](#t5) L57-L65). RFC 4253 §4.2: the identification string is
  `SSH-protoversion-softwareversion SP comments CR LF`, at most 255
  characters; "The null character MUST NOT be sent"; the server "MAY send
  other lines of data before sending the version string"
  (<https://www.rfc-editor.org/rfc/rfc4253#section-4.2>). So the server's
  line is a valid identification string, and the client's NUL is a byte no
  compliant SSH client sends. Teleport uses the comments field, not a line
  before the version.

### Handshake, token and binding

- <a id="t7"></a>[7] A new connection: the client sends its own ECDH public
  key and tag `0`; the resumption token is the first 16 bytes of SHA-256 of
  the shared secret. A reconnection: a fresh ECDH exchange, tag `1`, then the
  token XORed with the first 16 bytes of SHA-256 of the new secret. The
  server answers `0` (resume), `1` (unknown token) or `2` (token known,
  client address differs). `resumeConnection` refuses any change of remote
  IP: "resumable connection attempted resumption from a different remote
  address"
  (<https://github.com/gravitational/teleport/blob/1283425b60ec5f60d509ba4c791183d452923ff7/lib/resumption/server_exchange.go#L33-L205>).
  The ECDH keys are not signed by anything: the RFD rests the protocol's
  security on SSH above it ("the worst that can happen ... is that the
  connection ends up erroring out due to a MAC failure"), claims robustness
  only "against passive eavesdropping", and pins the source address because
  Teleport records it in its audit log and offers IP pinning (RFD
  [[5]](#t5) L67-L77, L93-L117). The test `no roaming` checks the refusal
  (<https://github.com/gravitational/teleport/blob/1283425b60ec5f60d509ba4c791183d452923ff7/lib/resumption/resumption_test.go#L102-L127>).
  So Teleport's resumption survives a drop and a restart of its front end,
  but not a move to another network.

### Acknowledgements, replay buffer and backpressure

- <a id="t8"></a>[8] Client constants: `replacementInterval = 3 *
  time.Minute` (it moves to a new underlying connection every 3 minutes even
  when healthy, so front-end restarts are ridden out), `reconnectTimeout =
  time.Minute`, `minBackoff = 50 * time.Millisecond`, `maxBackoff = 10 *
  time.Second`. The redial goes through Teleport's existing client to its
  front end, and resumption gives up for good when it fails with `remote
  error: tls: expired certificate`
  (<https://github.com/gravitational/teleport/blob/1283425b60ec5f60d509ba4c791183d452923ff7/lib/resumption/client.go#L39-L46>,
  <https://github.com/gravitational/teleport/blob/1283425b60ec5f60d509ba4c791183d452923ff7/lib/resumption/client.go#L179-L272>,
  <https://github.com/gravitational/teleport/blob/1283425b60ec5f60d509ba4c791183d452923ff7/lib/resumption/client.go#L304-L390>).
- <a id="t9"></a>[9] `handshakeTimeout = 5 * time.Second`; `maxFrameSize =
  128 * 1024`; `errorTag = math.MaxUint64` closes the connection. On
  re-attach both sides write their received position as a uvarint (the RFD
  says little-endian 64-bit; the code is uvarint), and a position outside
  the replay buffer marks the connection closed. A frame is a uvarint
  acknowledgement (bytes received since the last one), a uvarint length and
  the data. A new attachment first asks the current one to detach and waits:
  the last attach wins
  (<https://github.com/gravitational/teleport/blob/1283425b60ec5f60d509ba4c791183d452923ff7/lib/resumption/resumable.go#L61-L69>,
  <https://github.com/gravitational/teleport/blob/1283425b60ec5f60d509ba4c791183d452923ff7/lib/resumption/resumable.go#L79-L213>,
  <https://github.com/gravitational/teleport/blob/1283425b60ec5f60d509ba4c791183d452923ff7/lib/resumption/resumable.go#L215-L423>).
- <a id="t10"></a>[10] `receiveBufferSize = 128 * 1024`, `sendBufferSize =
  2 * 1024 * 1024`, `initialBufferSize = 4096` (buffers double as needed),
  "chosen after a little manual testing to reach full throughput in the data
  transfer over a single SSH channel". `Write` blocks while the send buffer
  is full: backpressure, never an overflow
  (<https://github.com/gravitational/teleport/blob/1283425b60ec5f60d509ba4c791183d452923ff7/lib/resumption/managedconn.go#L30-L39>,
  <https://github.com/gravitational/teleport/blob/1283425b60ec5f60d509ba4c791183d452923ff7/lib/resumption/managedconn.go#L209-L261>).
  RFD: "a buffer size of 2MiB seems to be sufficient to maintain near full
  bandwidth"; it proposes keepalive frames every "30 seconds?" and a grace
  period "defaulting to one minute" (RFD [[5]](#t5) L83-L91). The package
  sends no keepalives (`grep -i keepalive lib/resumption/*.go` finds none
  outside tests): a drop is noticed when the underlying connection returns
  an error.

### Wiring and restarts

- <a id="t11"></a>[11] The Teleport SSH service wraps its SSH server in
  `resumption.NewSSHServerWrapper` unless
  `TELEPORT_UNSTABLE_DISABLE_SSH_RESUMPTION` is set
  (<https://github.com/gravitational/teleport/blob/1283425b60ec5f60d509ba4c791183d452923ff7/lib/service/service.go#L3895-L3936>).
  For graceful restarts, each resumable connection gets a Unix socket in
  the data directory, and a new process forwards a resumption it does not
  know to the old process's socket: the old process keeps serving its
  connections
  (<https://github.com/gravitational/teleport/blob/1283425b60ec5f60d509ba4c791183d452923ff7/lib/resumption/handover.go#L44-L60>).
- <a id="t12"></a>[12] The client end sits in `DialHostWithResumption`,
  beneath `tsh`'s end-to-end SSH and above the connection Teleport's front
  end forwards. `tsh proxy ssh`, the OpenSSH ProxyCommand, uses it and
  copies the resumable conn to stdio
  (<https://github.com/gravitational/teleport/blob/1283425b60ec5f60d509ba4c791183d452923ff7/lib/client/cluster_client.go#L153-L180>,
  <https://github.com/gravitational/teleport/blob/1283425b60ec5f60d509ba4c791183d452923ff7/tool/tsh/common/proxy.go#L58-L99>).
- <a id="t13"></a>[13] Inside the resumed connection the Teleport SSH
  service sends keepalives every 5 minutes and drops after 3 missed
  (<https://github.com/gravitational/teleport/blob/1283425b60ec5f60d509ba4c791183d452923ff7/api/defaults/defaults.go#L38-L41>,
  <https://github.com/gravitational/teleport/blob/1283425b60ec5f60d509ba4c791183d452923ff7/api/defaults/defaults.go#L94-L98>),
  far beyond the 1-minute grace.

### Reusing it

- <a id="t14"></a>[14] Size at master: 2,148 non-test lines in 8 files (the
  buffer and protocol core is `managedconn.go` 454 and `resumable.go` 428;
  ECDH and detection are `client.go` 390, `server_detect.go` 229,
  `server_exchange.go` 205; `handover.go` 269), plus 1,190 lines of tests.
  Internal imports: `lib/multiplexer` (2,372 non-test lines), `lib/sshutils`,
  `lib/utils`, `lib/utils/log` and the root `teleport` package; external:
  `gravitational/trace`, `jonboulle/clockwork`, `golang.org/x/sync/errgroup`
  (`wc -l`, `grep` on the sparse checkout). Teleport's `go.mod` is 646 lines
  with 14 `replace` directives, among them forks of `gogo/protobuf`,
  `crewjam/saml` and `go-libfido2`
  (<https://github.com/gravitational/teleport/blob/1283425b60ec5f60d509ba4c791183d452923ff7/go.mod#L630-L643>),
  and "`replace` directives only apply in the main module's `go.mod` file
  and are ignored in other modules"
  (<https://go.dev/ref/mod#go-mod-file-replace>). Importing the module would
  pull Teleport's whole dependency graph without the forks it builds with.
- <a id="t15"></a>[15] AGPL-3.0 §5(c): "You must license the entire work,
  as a whole, under this License to anyone who comes into possession of a
  copy" (<https://www.gnu.org/licenses/agpl-3.0.html>).

What this means:

- No released version of `lib/resumption` is Apache-2.0. Copying or forking
  it, with or without a NOTICE, would put koko under AGPL-3.0
  ([[1]](#t1)–[[4]](#t4), [[15]](#t15)). The one-day Apache header on the
  first file sat in a repository already licensed AGPL-3.0 and was called
  "wrong" by its author ([[2]](#t2)); this note does not rely on it. The
  v16 change concerns Teleport's binaries, not this question.
- Importing the module is impractical on top of that ([[14]](#t14)).
- The design transfers: byte offsets acknowledged in-band, a bounded replay
  buffer that blocks the writer, one exchange of received offsets on
  re-attach, last attach wins, and the resumable end inside the ProxyCommand
  ([[9]](#t9), [[10]](#t10), [[12]](#t12)).
- What koko does not take: the ECDH token (koko re-attaches inside an
  authenticated Device Connection), the source-address pin (koko must
  survive a network change), the 3-minute replacement (no rotating front
  end), and the 1-minute grace.

## Eternal Terminal

- <a id="e1"></a>[16] Apache-2.0, no NOTICE file. Active: 199 commits in
  2026, newest 2026-11-04; release et-v7.1.1 on 2026-10-28; protocol
  version 6
  (<https://github.com/MisterTea/EternalTerminal/blob/060b18e7c4c8ac2ece6cac383282d7aefb942817/LICENSE>,
  <https://github.com/MisterTea/EternalTerminal/blob/060b18e7c4c8ac2ece6cac383282d7aefb942817/src/base/Headers.hpp#L210>).
- <a id="e2"></a>[17] Architecture: `et` runs `etterminal` on the remote host
  over SSH, which registers a 16-character client id and a 32-character passkey
  with `etserver`; the client then connects to `etserver` on TCP 2022, and
  the SSH connection is no longer used. Reconnection: `ConnectRequest`
  (client id), a fresh challenge answered with a keyed proof, then both
  sides exchange `SequenceHeader` (the last packet number each received) and
  a `CatchupBuffer` of the packets the other lacks. Port forwards (`-L`,
  `--tunnel`, `-R`, `-r`, and `-D`) travel in the same connection
  (<https://github.com/MisterTea/EternalTerminal/blob/060b18e7c4c8ac2ece6cac383282d7aefb942817/docs/protocol.md#L7-L60>,
  <https://github.com/MisterTea/EternalTerminal/blob/060b18e7c4c8ac2ece6cac383282d7aefb942817/docs/protocol.md#L93-L133>,
  <https://github.com/MisterTea/EternalTerminal/blob/060b18e7c4c8ac2ece6cac383282d7aefb942817/docs/protocol.md#L145-L147>,
  <https://github.com/MisterTea/EternalTerminal/blob/060b18e7c4c8ac2ece6cac383282d7aefb942817/docs/protocol.md#L204-L206>,
  <https://github.com/MisterTea/EternalTerminal/blob/060b18e7c4c8ac2ece6cac383282d7aefb942817/src/terminal/TerminalClientMain.cpp#L558>,
  <https://github.com/MisterTea/EternalTerminal/blob/060b18e7c4c8ac2ece6cac383282d7aefb942817/README.md#L229-L231>).
- <a id="e3"></a>[18] `BackedWriter`: `MAX_BACKUP_BYTES = 64 * 1024 * 1024`
  and `DISCONNECT_BUFFER_BYTES = 64 * 1024 * 1024`. Every packet is
  encrypted and kept; while connected the oldest are dropped beyond 64 MiB;
  while disconnected nothing is dropped, and past 64 MiB the write is
  `SKIPPED` and the caller waits. There are no acknowledgements: on
  reconnect `recover` resends the packets after the peer's sequence number,
  or fails with "Client is too far behind server."
  (<https://github.com/MisterTea/EternalTerminal/blob/060b18e7c4c8ac2ece6cac383282d7aefb942817/src/base/BackedWriter.hpp#L31-L34>,
  <https://github.com/MisterTea/EternalTerminal/blob/060b18e7c4c8ac2ece6cac383282d7aefb942817/src/base/BackedWriter.cpp#L16-L114>).
- <a id="e4"></a>[19] `Connection::recover` exchanges sequence headers and
  catchup buffers of up to 144 MiB under both the reader's and the writer's
  lock
  (<https://github.com/MisterTea/EternalTerminal/blob/060b18e7c4c8ac2ece6cac383282d7aefb942817/src/base/SocketHandler.hpp#L78>,
  <https://github.com/MisterTea/EternalTerminal/blob/060b18e7c4c8ac2ece6cac383282d7aefb942817/src/base/Connection.cpp#L1-L10>,
  <https://github.com/MisterTea/EternalTerminal/blob/060b18e7c4c8ac2ece6cac383282d7aefb942817/src/base/Connection.cpp#L119-L215>).
- <a id="e5"></a>[20] The client's reconnect loop retries every second
  until shutdown; `INVALID_KEY` ends ET's session and `RETRY_LATER` means the
  server is recovering
  (<https://github.com/MisterTea/EternalTerminal/blob/060b18e7c4c8ac2ece6cac383282d7aefb942817/src/base/ClientConnection.cpp#L140-L290>).
- <a id="e6"></a>[21] Drop detection: `MAX_CLIENT_KEEP_ALIVE_DURATION = 5`
  seconds; the client sends a keepalive each period and, if the previous
  one is still unanswered, "Missed a keepalive, killing connection." and
  reconnects
  (<https://github.com/MisterTea/EternalTerminal/blob/060b18e7c4c8ac2ece6cac383282d7aefb942817/src/base/Headers.hpp#L224-L228>,
  <https://github.com/MisterTea/EternalTerminal/blob/060b18e7c4c8ac2ece6cac383282d7aefb942817/src/terminal/TerminalClient.cpp#L687-L698>).
- <a id="e7"></a>[22] Grace: `etserver --disconnect-timeout` "Minutes a
  disconnected etterminal may stay alive ... 0 means no timeout", default
  `0`; the `et1` wrapper for VS Code uses `--disconnect-timeout=10080`, a
  week. After an `etserver` restart unknown ids get `RETRY_LATER` for
  `recoveryGraceSeconds = 60`
  (<https://github.com/MisterTea/EternalTerminal/blob/060b18e7c4c8ac2ece6cac383282d7aefb942817/src/terminal/TerminalServerMain.cpp#L82-L85>,
  <https://github.com/MisterTea/EternalTerminal/blob/060b18e7c4c8ac2ece6cac383282d7aefb942817/README.md#L317-L330>,
  <https://github.com/MisterTea/EternalTerminal/blob/060b18e7c4c8ac2ece6cac383282d7aefb942817/src/base/ServerConnection.hpp#L130-L134>).

What this means: ET resumes one connection that carries everything (a
terminal and its forwards), the layering of option (A) below. It cannot be
used as is: it needs `etserver` on every Host, which
[ADR 0003](../adr/0003-enroll-only-adds-the-koko-key.md) rules out; it
replaces the OpenSSH client; and it carries no scp, sftp or rsync. Its
licence would allow porting with attribution, but its design is the weaker
one for koko: without acknowledgements the sender must keep 64 MiB in case
the peer is that far behind, where acknowledged offsets need only what is in
flight. Its 5 s keepalive and its unlimited default grace are the useful
data points.

## mosh, and UDP terminals in Go

- <a id="m1"></a>[23] mosh "keeps the session alive if the client goes to
  sleep", roams across IP addresses, and "does not support X forwarding or
  the non-interactive uses of SSH, including port forwarding". `mosh` uses
  SSH to start `mosh-server` on the remote host, which "listens on a high
  UDP port" (60000 to 61000) and returns an AES-128 key over SSH; the SSH
  connection then closes
  (<https://github.com/mobile-shell/mosh/blob/decd9b705eb81626f694335b8d5940538beb06da/README.md#L6-L37>,
  <https://github.com/mobile-shell/mosh/blob/decd9b705eb81626f694335b8d5940538beb06da/README.md#L93-L108>).
  The protocol is the State Synchronization Protocol: "SSP runs over UDP,
  synchronizing the state of any object from one host to another"; mosh
  synchronises the screen, not a byte stream (<https://mosh.org/>).
- <a id="m2"></a>[24] GPL-3.0 with an exception for linking OpenSSL
  (`COPYING`;
  <https://github.com/mobile-shell/mosh/blob/decd9b705eb81626f694335b8d5940538beb06da/src/network/network.cc#L1-L32>).
  Latest release mosh-1.4.0, 2022-10-27 (`gh release list -R
  mobile-shell/mosh`).
- <a id="m3"></a>[25] `MOSH_SERVER_NETWORK_TMOUT`: unset, "mosh-server will
  wait indefinitely for a client to reappear"; the man page suggests a week
  or 30 days
  (<https://github.com/mobile-shell/mosh/blob/decd9b705eb81626f694335b8d5940538beb06da/man/mosh-server.1#L96-L106>).
- <a id="m4"></a>[26] Go descendants: `unixshells/mosh-go`, "Pure Go mosh",
  MIT, 27 stars, created 2026-03-13, last push 2026-04-05; `trzsz/tsshd`, a
  "UDP-based SSH server" with roaming, MIT, 559 stars, v0.1.10 on
  2026-10-03. tsshd needs its own client (`tssh`) and a `tsshd` process per
  connection on the remote host; it runs QUIC or KCP over a `net.PacketConn`
  whose peer address can change after re-authentication, and keeps a
  disconnected session for `UdpAliveTimeout`, default ten days
  (`gh api repos/unixshells/mosh-go`;
  <https://github.com/trzsz/tsshd#how-it-works>,
  <https://github.com/trzsz/tsshd#reconnection>).

What this means: none fits. mosh and tsshd need a server program on the
Host ([ADR 0003](../adr/0003-enroll-only-adds-the-koko-key.md)) and their
own client instead of OpenSSH; mosh carries a terminal only; both need UDP.
mosh's GPL would also bar copying.

## QUIC connection migration

- <a id="q1"></a>[27] RFC 9000 §9: "The use of a connection ID allows
  connections to survive changes to endpoint addresses"; "Clients are
  responsible for initiating all migrations"; an endpoint "MUST perform path
  validation ... if it detects any change to a peer's address" (NAT
  rebinding). §10.1: a connection idle for longer than the minimum of both
  endpoints' `max_idle_timeout` "is silently closed and its state is
  discarded" (<https://www.rfc-editor.org/rfc/rfc9000#section-9>,
  <https://www.rfc-editor.org/rfc/rfc9000#section-10.1>).
- <a id="q2"></a>[28] quic-go v0.50.0 (2025-02-16) added server-side
  migration and v0.51.0 (2025-04-20) "client-side support for QUIC
  Connection Migration. Clients can now probe multiple paths and switch
  between them mid-connection"
  (<https://github.com/quic-go/quic-go/releases/tag/v0.51.0>; commit
  `24acc54` "add support for connection migration (#4960)"). MIT.
- <a id="q3"></a>[29] API: `Conn.AddPath(*Transport)` (client only, refused
  if the server set `disable_active_migration`), `Path.Probe(ctx)`,
  `Path.Switch()`, `Path.Close()`. The server validates and switches to a
  new peer address by itself. `MaxIdleTimeout` defaults to 30 s, the
  effective value is the smaller of both peers', and `KeepAlivePeriod` is at
  most half of it. `Dial` takes a `*tls.Config`: QUIC means TLS 1.3
  (<https://github.com/quic-go/quic-go/blob/4ba9d8a7886e6baab5d84460e86f75ff07b91ec6/connection.go#L3201-L3215>,
  <https://github.com/quic-go/quic-go/blob/4ba9d8a7886e6baab5d84460e86f75ff07b91ec6/path_manager_outgoing.go#L38-L93>,
  <https://github.com/quic-go/quic-go/blob/4ba9d8a7886e6baab5d84460e86f75ff07b91ec6/connection.go#L1250-L1330>,
  <https://github.com/quic-go/quic-go/blob/4ba9d8a7886e6baab5d84460e86f75ff07b91ec6/interface.go#L118-L123>,
  <https://github.com/quic-go/quic-go/blob/4ba9d8a7886e6baab5d84460e86f75ff07b91ec6/connection.go#L2505-L2510>,
  <https://github.com/quic-go/quic-go/blob/4ba9d8a7886e6baab5d84460e86f75ff07b91ec6/client.go#L87>).
- <a id="q4"></a>[30] quic-go's documentation: NAT rebinding is handled
  automatically; for a network change the application creates a new
  `Transport`, then calls `AddPath`, `Probe` and `Switch`; the server's
  preferred address is not supported
  (<https://quic-go.net/docs/quic/connection-migration/>).

What this means:

- QUIC migrates a live connection only while both ends hold its state
  ([[27]](#q1)). A laptop asleep for longer than the idle timeout, 30 s by
  default in quic-go ([[29]](#q3)), loses it, unless the timeout is set that
  long, and then the koko server holds the state just
  as resumption would. QUIC also loses everything on a koko server restart,
  and the device must notice the network change itself, as with TCP
  ([[30]](#q4)).
- Networks that block UDP need TCP 443 or WSS, and neither can migrate.
  QUIC also brings TLS, which P0 does not have
  ([ADR 0012](../adr/0012-ssh-end-to-end-and-the-web-ui-through-the-cli.md)).
- So QUIC (P2) does not replace resumption; with per-Tunnel resumption in
  place it only spares some re-attaches. The P1 WSS fallback gains
  resumption without change, because the re-attach rides inside whatever
  Device Connection the Device Mux opens.

## Go libraries for resumable byte streams, and other prior art

<a id="g1"></a>[31] Searched on 2026-11-05: GitHub (`gh search repos`,
language Go: "resumable connection", "resumable net.Conn", "reconnecting
net.Conn replay buffer", "session resumption ssh", "eternal tcp",
"persistent tcp reconnect sequence ack", "roaming ssh", "reconnectable
connection", "connection resumption", "seamless reconnect ssh", "survive
network change tcp", "mosh go") and pkg.go.dev ("resumable net.Conn": no
results; "resumable connection": 25). What came up, besides Teleport:

| Candidate | What it is | Licence | Activity and users | Fit |
| --- | --- | --- | --- | --- |
| `k4lleanka/resumux` | TCP forwarded over WebSockets "where the connection survives the WebSocket" | Apache-2.0 | created 2026-10-29, 2 stars, v0.1.0, 0 importers | too new to adopt; WebSocket-bound |
| `colitu/csl` | a resumable session layer for VPN carriers | Apache-2.0 | created 2026-10-10, 0 stars | too new |
| `discobox-ai/discobox` `execstream/resume` | keeps one app's exec connection alive across transports | Apache-2.0 | created 2026-06-26, 108 stars, 0 importers of the package | internal to that app |
| `yurenchen000/ssh-keep` | a relay client as ProxyCommand and a relay server beside sshd, "Inspired by eternalterminal" | none | 5 stars, last push 2026-02-12 | same shape as koko's; no licence, so unusable |
| `trzsz/tsshd`, `unixshells/mosh-go` | UDP terminals ([[26]](#m4)) | MIT | active | need a program on the Host |
| `xtaci/smux`, `hashicorp/yamux` | stream multiplexers | MIT, MPL-2.0 | widely used | no resumption; known, not searched up |

(`gh api repos/<name>` for stars, dates and licences.)

- <a id="g2"></a>[32] Microsoft dev-tunnels-ssh (C# and TypeScript, MIT)
  defines `session-reconnect@microsoft.com`: after a new key exchange the
  client proves it knows a secret of the old SSH connection, both sides resend
  messages after the other's last sequence number, and the cache "can grow
  only as large as **1 MB per channel** due to the channel flow-control
  built into the SSH protocol"
  (<https://github.com/microsoft/dev-tunnels-ssh/blob/e7d14b3be4a5b57772255b23a54c80dfd96f6952/ProtocolExtensions.md#L17-L50>).
  Both SSH ends must implement it; the OpenSSH client does not, and
  x/crypto would need a fork ([[5]](#t5)).
- <a id="g3"></a>[33] Coder's reconnecting PTY keeps the last 64 KiB of a
  PTY's output in a ring buffer and replays it to a client that reattaches
  (`circbuf.NewBuffer(64 << 10)`); it restores a terminal's look, not a byte
  stream, and is AGPL-3.0
  (<https://github.com/coder/coder/blob/63b5c4d64adc5b23b7b1a26d8a8eef3f54cc04ed/agent/reconnectingpty/buffered.go#L51-L57>;
  `gh api repos/coder/coder --jq .license.spdx_id`).
- <a id="g4"></a>[34] MPTCP from Go: `Dialer.SetMultipathTCP` uses MPTCP "if
  supported by the operating system" and falls back to TCP; off Linux,
  `dialMPTCP` is plain `dialTCP` (`//go:build !linux`), so on macOS it does
  nothing
  (<https://github.com/golang/go/blob/go1.27.1/src/net/dial.go#L415-L421>,
  <https://github.com/golang/go/blob/go1.27.1/src/net/mptcpsock_stub.go#L1-L24>).
  MPTCP also keeps no state across a koko server restart and would not
  survive minutes with no path at all (not verified).

What this means: nothing maintained and used exists to import. The layer is
small enough to own, and its design is proven by Teleport in Go and by ET
and dev-tunnels-ssh in other languages.

## x/crypto/ssh and OpenSSH at the edges

- <a id="x1"></a>[35] x/crypto v0.58.0: `channelMaxPacket = 1 << 15` and
  `channelWindowSize = 64 * channelMaxPacket`, 2 MiB, fixed; `Timeout` in
  `ClientConfig` covers only the TCP dial. Searching the 25 non-test files
  of `ssh/` for `time.After`, `NewTimer`, `AfterFunc`, `NewTicker`, `Tick`
  and `Set*Deadline` finds only the deadline methods of `tcpip.go`'s
  `chanConn`, which the caller sets: after the handshake the library runs no
  timer of its own
  (<https://github.com/golang/crypto/blob/v0.58.0/ssh/channel.go#L22-L24>,
  <https://github.com/golang/crypto/blob/v0.58.0/ssh/client.go#L321-L324>,
  <https://github.com/golang/crypto/blob/v0.58.0/ssh/tcpip.go#L524-L547>).
- <a id="x2"></a>[36] `SendRequest` with want-reply blocks until a reply
  arrives or the connection closes, with no timeout; a pending key exchange
  queues at most `maxPendingPackets = 64` packets
  (<https://github.com/golang/crypto/blob/v0.58.0/ssh/mux.go#L144-L190>,
  <https://github.com/golang/crypto/blob/v0.58.0/ssh/handshake.go#L28-L31>).
  Local [[46]](#l1): an SSH connection whose transport passed no byte for 5
  minutes, with a channel write and a want-reply request pending, completed
  both afterwards and went on working.
- <a id="o1"></a>[37] `ssh_config(5)`: `ServerAliveInterval` "Sets a timeout
  interval in seconds after which if no data has been received from the
  server, ssh(1) will send a message through the encrypted channel", default
  0 (off); `ServerAliveCountMax` default 3, "if ServerAliveInterval ... is
  set to 15 and ServerAliveCountMax is left at the default, if the server
  becomes unresponsive, ssh will disconnect after approximately 45 seconds";
  `TCPKeepAlive` default `transport`
  (<https://github.com/openssh/openssh-portable/blob/3c6ec4c637342c51d9c54bbeecef25fd185cfdb8/ssh_config.5#L1939-L1972>,
  <https://github.com/openssh/openssh-portable/blob/3c6ec4c637342c51d9c54bbeecef25fd185cfdb8/ssh_config.5#L2090-L2116>).
- <a id="o2"></a>[38] `clientloop.c`: any input from the server resets the
  timer; on expiry `server_alive_check` increments the count first and exits
  with "Timeout, server %s not responding." when it exceeds
  `ServerAliveCountMax`, otherwise sends `keepalive@openssh.com`; any packet
  received resets the count
  (<https://github.com/openssh/openssh-portable/blob/3c6ec4c637342c51d9c54bbeecef25fd185cfdb8/clientloop.c#L488-L512>,
  <https://github.com/openssh/openssh-portable/blob/3c6ec4c637342c51d9c54bbeecef25fd185cfdb8/clientloop.c#L754-L762>,
  <https://github.com/openssh/openssh-portable/blob/3c6ec4c637342c51d9c54bbeecef25fd185cfdb8/clientloop.c#L792-L800>,
  <https://github.com/openssh/openssh-portable/blob/3c6ec4c637342c51d9c54bbeecef25fd185cfdb8/packet.c#L1880-L1890>,
  <https://github.com/openssh/openssh-portable/blob/3c6ec4c637342c51d9c54bbeecef25fd185cfdb8/readconf.c#L3024-L3027>).
  So ssh gives up after `ServerAliveInterval × (ServerAliveCountMax + 1)` of
  silence, about 60 s for 15 × 3, and `ServerAliveCountMax 0` exits at the
  first check. Local [[47]](#l2) measured 20 s for 5 × 3 and 5 s for 5 × 0.
- <a id="o3"></a>[39] `ssh_connect` passes `TCPKeepAlive` only to
  `ssh_connect_direct`; with a ProxyCommand ssh talks to a pipe and sets no
  keepalive
  (<https://github.com/openssh/openssh-portable/blob/3c6ec4c637342c51d9c54bbeecef25fd185cfdb8/sshconnect.c#L599-L623>).
- <a id="o4"></a>[40] sshd's `client_alive_check` sends
  `keepalive@openssh.com` as a **channel** request when a channel is open,
  and a global request otherwise; only a reply resets its count.
  `ClientAliveInterval` defaults to 0; with `ClientAliveCountMax` 3 and an
  interval of 15 "unresponsive SSH clients will be disconnected after
  approximately 45 seconds"
  (<https://github.com/openssh/openssh-portable/blob/3c6ec4c637342c51d9c54bbeecef25fd185cfdb8/serverloop.c#L112-L140>,
  <https://github.com/openssh/openssh-portable/blob/3c6ec4c637342c51d9c54bbeecef25fd185cfdb8/serverloop.c#L405-L414>,
  <https://github.com/openssh/openssh-portable/blob/3c6ec4c637342c51d9c54bbeecef25fd185cfdb8/sshd_config.5#L616-L650>).
- <a id="o5"></a>[41] OpenSSH's windows: `CHAN_SES_WINDOW_DEFAULT` and
  `CHAN_TCP_WINDOW_DEFAULT` are 64 × 32 KiB, 2 MiB
  (<https://github.com/openssh/openssh-portable/blob/3c6ec4c637342c51d9c54bbeecef25fd185cfdb8/channels.h#L224-L227>).

What this means:

- x/crypto/ssh survives minutes without a byte, on both the SSH Session and
  the Device Connection ([[35]](#x1), local [[46]](#l1)). What ends a stalled
  SSH Session is keepalive logic at its ends: the OpenSSH client's
  `ServerAlive*` and the Host's `ClientAlive*`.
- **The OpenSSH client.** During a drop its keepalives sit unanswered in
  `koko tunnel`, so `ServerAliveInterval × (ServerAliveCountMax + 1)` caps
  the grace ([[37]](#o1), [[38]](#o2)). koko's `Host *.koko` block comes first and the
  first value wins (issue #6 note), so `ServerAliveInterval 0` there
  restores OpenSSH's default for `.koko` names; only `-o` on the command
  line beats it. For `.koko` names ssh's transport is a local pipe, so the
  setting buys nothing koko does not do itself: the Device Mux watches the
  Device Connection, and `koko tunnel` exits, and so ends ssh, when the
  Tunnel cannot come back. `TCPKeepAlive` does not apply to a ProxyCommand
  ([[39]](#o3)).
- **The Host's sshd.** The Gateway's own connection to the Host stays up
  through the drop, but the prototype relays every channel request, so
  sshd's channel keepalive would wait for the OpenSSH client and a Host with
  `ClientAliveInterval` set would end the SSH Session ([[40]](#o4)). The
  Gateway must answer `keepalive@openssh.com` from the Host itself.
- **The Gateway's inner server** sends no keepalives today. If it ever does,
  missed replies must not end a detached SSH Session.

## Where the resumable layer goes

Three placements carry the same SSH Sessions:

- **(A) Beneath the Device Connection.** Wrap the TCP connection under the
  outer SSH, so the Device Connection and all its channels survive. This is
  ET's layering: one resumable connection under everything it carries
  ([[17]](#e2)).
- **(B) Per Tunnel, device end in the Device Mux.** Each Tunnel's bytes are
  acknowledged and buffered by the Device Mux and the Gateway; after a drop
  the Device Mux opens a new Device Connection and re-attaches each Tunnel.
- **(B′) Per Tunnel, device end in `koko tunnel`.** As (B), but the device
  end lives in the process that owns ssh's stdio; the Device Mux only
  carries bytes. This is Teleport's layering: the ProxyCommand holds one end
  and the server where the SSH ends holds the other ([[12]](#t12)).

| | (A) beneath the Device Connection | (B) per Tunnel, in the Device Mux | (B′) per Tunnel, in `koko tunnel` |
| --- | --- | --- | --- |
| Reconnect authenticated by | a token beneath SSH: koko's own crypto, Teleport's ECDH or a device-key signature over the outer SSH's session identifier | the device's key, a normal Device Connection | the same |
| Revocation, Removed User | koko server must close the detached outer SSH as well; the token alone re-attaches | a Revoked key cannot reconnect; the koko server closes detached Tunnels | the same |
| x/crypto on a stall | outer SSH survives (local [[46]](#l1)) | the SSH Session survives | the same |
| API and web channels | survive | calls in flight fail; browsers reconnect | the same |
| WSS (P1), QUIC (P2), switching transport on reconnect | the framing must sit on each transport, and a re-attach over another transport needs a registry shared between them | nothing changes: the re-attach rides inside any Device Connection | the same |
| Port 443 first-bytes detection | a third prelude beside the SSH banner and the TLS ClientHello ([[6]](#t6)) | unchanged | unchanged |
| State on the koko server while detached | the whole outer SSH and everything in it | per SSH Session: inner SSH, Host connection, ≤ 2 MiB | the same |
| Device Mux crash or upgrade | everything on the device ends | every SSH Session ends | survives: `koko tunnel` re-attaches through a new Device Mux or its own Device Connection |
| Code koko owns | resumable conn, token handshake, prelude detection, registry | resumable conn, re-attach request, registry | the same, and the Device Mux stays a relay |
| Production precedent | ET ([[17]](#e2)) | none found | Teleport ([[12]](#t12)) |

Other layerings found, none of them usable:

- **Inside SSH** (`session-reconnect@microsoft.com`, [[32]](#g2)): both SSH
  ends must implement it. The SSH Session's client end is the unchanged
  OpenSSH client; for the Device Connection it would take a fork of
  x/crypto, which Teleport declined ([[5]](#t5)).
- **A swappable packet path beneath QUIC or KCP** (tsshd, [[26]](#m4)): UDP
  only; the QUIC answer of P2.
- **Terminal state** (mosh's SSP, Coder's PTY ring buffer, [[23]](#m1),
  [[33]](#g3)): terminals only.

**Recommendation: (B′).** It needs no crypto of its own, because the
Device Connection already authenticates and encrypts the re-attach. It
keeps revocation as it is, and it is independent of the transport, so WSS
and QUIC inherit it. It survives a crash or upgrade of the Device Mux
([ADR 0017](../adr/0017-a-device-mux-holds-the-device-connection.md) runs
one per build). It is the layering Teleport ships. What it gives up next to
(A) is the API and web channels, which are short HTTP calls and browser
connections that can simply be retried
([ADR 0015](../adr/0015-the-api-is-http-and-json-in-ssh-channels.md)).

## Buffer sizing

- **What can be owed.** A sender's replay buffer holds bytes it wrote that
  the other end has not acknowledged. While the network works, that is what
  is in flight, about throughput × RTT. During a drop the SSH Session above
  keeps writing only until each channel's window is spent: 2 MiB per channel
  per direction in both OpenSSH and x/crypto ([[35]](#x1), [[41]](#o5)),
  plus small control messages. dev-tunnels-ssh relies on the same bound
  ([[32]](#g2)).
- **What bounds the whole SSH Session.** Nothing in SSH: windows are per
  channel, so the sum grows with open channels (`-D` and VS Code open many).
  Hence a cap, and a full buffer blocks the writer, as in Teleport
  ([[10]](#t10)). Blocking is safe beneath SSH: the writer is the SSH
  library or ssh's own poll loop, and the reading side keeps draining.
- **The cap: 2 MiB per direction per Tunnel**, grown from a few KiB as
  needed. A sender with B unacknowledged bytes moves at most B per RTT, so
  2 MiB gives 200 MiB/s at 10 ms, 50 MiB/s at 40 ms, 20 MiB/s at 100 ms and
  8 MiB/s at 250 ms. One SSH channel's window already sets the same cap, and
  the prototype's 36 to 39 MiB/s at 40 ms RTT sits under it; several busy
  channels in one SSH Session share it. Teleport uses 2 MiB for the same
  reason; ET's 64 MiB is the price of having no acknowledgements
  ([[10]](#t10), [[18]](#e3)).
- **Memory on the koko server per detached SSH Session**: at most 2 MiB of
  replay buffer, plus what the Gateway's connection to the Host buffers
  anyway, up to its 2 MiB window per open channel once the relay stalls. An
  idle shell holds a few KiB. On the device, `koko tunnel` holds at most
  2 MiB.

## Detecting a dead Device Connection

- **No byte, no error.** After a sleep or a network change, a TCP
  connection often gives no error at all. Go's default TCP keepalive (15 s
  idle, 15 s interval, 9 probes) declares an idle peer dead after about
  150 s, and with data unacknowledged Linux retransmits for at least 924.6 s
  ([[43]](#r2), [[44]](#r3)).
- **koko's own probe is the main signal.** After 5 s without inbound bytes
  the Device Mux sends `keepalive@openssh.com` with want-reply in a
  goroutine (x/crypto applies no timeout, [[36]](#x2)); 15 s without any
  inbound byte drops the Device Connection. ET drops after one missed 5 s
  keepalive ([[21]](#e6)); Teleport has no probe and waits for an error
  ([[10]](#t10)). A false alarm costs one reconnect, about five round trips
  ([ADR 0017](../adr/0017-a-device-mux-holds-the-device-connection.md)), and
  no SSH Session.
- **Sleep and network changes from pure Go.** Tailscale's `netmon` checks
  the wall clock every 15 s and treats a jump of more than 1.5 intervals as
  a wake ("we probably just came out of sleep"). It watches routes on macOS
  through an `AF_ROUTE` socket and `golang.org/x/net/route`, and on Linux
  through netlink route groups; all of this is pure Go, BSD-3-Clause
  ([[45]](#r4)). Go's monotonic clock "will stop if the computer goes to
  sleep" on some systems ([[42]](#r1)), which is why the wall clock shows
  the jump. Either event can trigger an immediate probe or reconnect; the
  15 s deadline remains the backstop.
- **Timers on the device.** Since the monotonic clock may stop during
  sleep, a device-side grace timer would not count the sleep. `koko tunnel`
  therefore lets the koko server decide whether a Tunnel still exists, and
  gives up on its own only after the grace plus a minute of waking time
  without reaching the koko server.

- <a id="r1"></a>[42] Go `time` package: "On some systems the monotonic
  clock will stop if the computer goes to sleep"
  (<https://github.com/golang/go/blob/go1.27.1/src/time/time.go#L54>).
- <a id="r2"></a>[43] Go `net`: `defaultTCPKeepAliveIdle = 15 *
  time.Second`, `defaultTCPKeepAliveInterval = 15 * time.Second`,
  `defaultTCPKeepAliveCount = 9`
  (<https://github.com/golang/go/blob/go1.27.1/src/net/dial.go#L17-L27>).
- <a id="r3"></a>[44] Linux `tcp_retries2`: "The default value of 15 yields
  a hypothetical timeout of 924.6 seconds and is a lower bound for the
  effective timeout"
  (<https://github.com/torvalds/linux/blob/05441d697f7f9a927264f4058cbbc6a03dfcac9d/Documentation/networking/ip-sysctl.rst#L829-L843>).
- <a id="r4"></a>[45] Tailscale `net/netmon`: `pollWallTimeInterval = 15 *
  time.Second`, `checkWallTimeAdvanceLocked`, `wallTime` as
  `time.Now().Round(0)`; macOS `unix.Socket(unix.AF_ROUTE, ...)` parsed with
  `route.ParseRIB`; Linux `netlink.Dial(unix.NETLINK_ROUTE, ...)` with
  `RTMGRP_IPV4_IFADDR | RTMGRP_IPV6_IFADDR | RTMGRP_IPV4_ROUTE | ...`
  (<https://github.com/tailscale/tailscale/blob/a967b7d811b7ead64e4fa5c030284ed0769abc88/net/netmon/netmon.go#L28-L42>,
  <https://github.com/tailscale/tailscale/blob/a967b7d811b7ead64e4fa5c030284ed0769abc88/net/netmon/netmon.go#L703-L741>,
  <https://github.com/tailscale/tailscale/blob/a967b7d811b7ead64e4fa5c030284ed0769abc88/net/netmon/netmon_darwin.go#L35>,
  <https://github.com/tailscale/tailscale/blob/a967b7d811b7ead64e4fa5c030284ed0769abc88/net/netmon/netmon_linux.go#L60-L66>).

## Failure modes

| Event | What happens | How koko handles it |
| --- | --- | --- |
| Device asleep or offline longer than the grace | the Gateway closes the SSH Session and its Host connection | the re-attach is refused; `koko tunnel` prints why and exits; ssh ends |
| koko server restart or upgrade | in-memory Tunnels are lost | the re-attach finds no Tunnel; same as above. Teleport's answer is to keep the old process serving and pass resumptions to it over a Unix socket ([[11]](#t11)); P0 can accept the loss |
| Device Mux crash or upgrade | the Device Connection goes with it | `koko tunnel` keeps its buffer, starts or finds a Device Mux of its build, or opens its own Device Connection, and re-attaches |
| The Device Mux's ten-minute idle exit | would cut a Tunnel waiting to re-attach | a local client waiting to re-attach counts as an open channel |
| Device Session Revoked, User Removed, or access to the Host lost while detached | — | the koko server closes those SSH Sessions at once; the reconnect fails authentication, and the Device Mux records the revocation as ADR 0017 says; a re-attach rechecks access |
| Replay buffer full | the writer blocks | backpressure only; nothing is dropped, so nothing overflows |
| An offset outside the buffer, or a malformed frame | cannot resume | end the SSH Session, as Teleport does ([[9]](#t9)) |
| Two re-attaches racing, or a half-open older Device Connection | — | per Tunnel, the last attach wins and detaches the earlier one under a lock ([[9]](#t9)) |
| A replayed or stolen Tunnel id | — | it is accepted only inside a Device Connection of the Device Session that opened the Tunnel, so using it needs the device's key; at worst that device's own Device Connection detaches the Tunnel |
| Re-attach from a new IP address | Teleport refuses it ([[7]](#t7)) | koko accepts it, since that is the network change to survive; the new address is logged |
| ssh exits while its Tunnel is detached | the Gateway cannot be told | the SSH Session and its connection to the Host linger until the grace ends |
| The user's `ServerAliveInterval` | ssh ends itself before the grace | `ServerAliveInterval 0` in koko's block, or documentation ([[38]](#o2)) |
| The Host's `ClientAliveInterval` | sshd ends the SSH Session | the Gateway answers the Host's keepalives itself ([[40]](#o4)) |
| Many detached SSH Sessions | memory | bounded by 2 MiB each plus Host-side windows; a cap per Device Session is a #16 question |
| ControlPersist | one Tunnel carries the control master and every ssh that shares it | all resume together; `koko tunnel`'s notices go to `/dev/null` (issue #6 note) |

## Local observations

All on macOS 27.0.1 with Go 1.27.1 and `OpenSSH_10.3p1`. `$S` is this
session's scratch directory, outside the repository, and `$A` is
`$S/exp/alive`; nothing touched `~/.ssh` (ssh ran with `-F`, scratch keys
and a scratch known_hosts).

- <a id="l1"></a>[46] `$S/exp/stall/main.go`, built with
  `golang.org/x/crypto v0.58.0`: an SSH client and server (`NoClientAuth`)
  over loopback TCP whose `Read` and `Write` block while a gate is shut.
  After an echo, the gate shut; a channel `Write` and a want-reply
  `keepalive@openssh.com` were started; the gate opened after the stall.
  `go build -o stall . && ./stall -stall 300s` printed:
  `gate open after 5m0.002s`, `global request: ok=true payload="pong"
  err=<nil> after 5m0.002s`, `channel write: n=6 err=<nil> after 5m0.002s`,
  `echo after stall: "during" err=<nil>`, `fresh global request after stall:
  ok=true`. The TCP connection itself stayed up on loopback; this shows the
  library's behaviour, not a real network's.
- <a id="l2"></a>[47] `$S/exp/alive/stallproxy`, a ProxyCommand that runs
  `/usr/sbin/sshd -i -f $A/sshd_config` (scratch host key and
  `AuthorizedKeysFile`, `UsePAM no`) and stops relaying in both directions
  after `-after`, closing nothing; `$A/ssh_config` passes `-after
  ${STALL_AFTER}`. `STALL_AFTER=3s ssh -F $A/ssh_config -o
  ServerAliveInterval=5 -o ServerAliveCountMax=3 stall 'sleep 120'` printed
  "Timeout, server stall not responding." and exited 255 20.0 s after it
  started (the server's last byte came at login); with `STALL_AFTER=12s`
  after 29.7 s, 20 s after the last answered keepalive at about 10 s; with
  `STALL_AFTER=12s` and `-o ServerAliveCountMax=0` after 5.0 s, before the
  stall began, at the first 5 s of silence.
- <a id="l3"></a>[48] The same with `-o ServerAliveInterval=0` and
  `STALL_AFTER=2s`, running `sleep 30; echo done`: ssh was still running
  after 120 s and was killed.

## Open questions

All handed to
[Gateway and Tunnel behaviour details](https://github.com/cieons/koko/issues/16)
(#16) unless another ticket is named.

- **The re-attach on the wire.** A field on `tunnel@koko` or a channel type
  of its own; who picks the Tunnel id (the koko server, sent in a first
  frame, is proposed); the frame format (Teleport-style uvarint
  acknowledgement and length, data up to 32 KiB, a close frame); half-close;
  how a CLI without resumption and a koko server with it meet (ADR 0015's
  rule that the server only adds).
- **The constants.** Grace (5 min, a user decision above), replay cap
  (2 MiB), probe and drop times on both sides (5 s / 15 s on the device,
  15 s / 45 s on the koko server), backoff (250 ms to 5 s), re-attach
  timeout (5 s); which are settings and which are constants.
- **Wake and route triggers.** Whether P0 adds the wall-clock jump and
  route-socket or netlink watch ([[45]](#r4)), or relies on the 15 s probe.
- **What the user sees.** Whether `koko tunnel` prints a notice on its
  stderr while detached (mid-screen in a raw terminal, lost under
  ControlPersist), what it prints when the grace ends, and what the
  Gateway logs for a detach, a re-attach and an address change.
- **`ServerAliveInterval 0` in koko's block** (a user decision above), and
  where the block is written: with
  [CLI command tree](https://github.com/cieons/koko/issues/12) (#12).
- **Keepalives at the Gateway.** It answers the Host's
  `keepalive@openssh.com` channel requests itself ([[40]](#o4)); whether it
  ever sends its own to the OpenSSH client.
- **Limits.** A cap on detached SSH Sessions per Device Session or on the
  koko server's total replay memory.
- **API calls across a drop.** Whether the Device Mux holds new calls while
  it reconnects, and which operations a command retries after a failed
  call (writes may not be safe to repeat). With
  [CLI command tree](https://github.com/cieons/koko/issues/12) (#12).
- **The web UI after a drop.** `koko web`'s browser connections close; any
  long-lived request the web UI makes must reconnect. Handed to
  [P0 web UI scope](https://github.com/cieons/koko/issues/13) (#13).
- **Surviving a koko server upgrade.** Whether a later release keeps the old
  process serving its SSH Sessions, as Teleport's handover does
  ([[11]](#t11)). Handed to
  [Install and run the koko server](https://github.com/cieons/koko/issues/14)
  (#14).

## Not verified

- No real network drop, Wi-Fi change or laptop sleep was tested; the
  experiments gate bytes on loopback ([[46]](#l1)–[[48]](#l3)). Nothing ran
  on Linux.
- Teleport's front-end transport was not read: how a redial is
  authenticated and whether that transport sends keepalives of its own. The
  claim that Teleport's resumable end sits beneath its end-to-end SSH rests
  on `DialHostWithResumption` and `tsh proxy ssh` ([[12]](#t12)).
- ET, mosh, tsshd and Teleport were read, not run.
- That macOS route messages fire for every Wi-Fi change and that the
  wall-clock jump shows every sleep; this rests on Tailscale's code
  ([[45]](#r4)), not on Apple documentation.
- Whether Go's monotonic clock stops during sleep on macOS and Linux in
  particular; Go's documentation says only "on some systems" ([[42]](#r1)).
- MPTCP through Apple's own APIs, and whether MPTCP survives minutes with no
  path ([[34]](#g4)).
- The 1,000 to 1,500 line estimate for koko's implementation, and the cost
  of framing and an extra copy per byte; neither was measured.
- Which Host images set `ClientAliveInterval` by default.
- How middleboxes on the device's networks treat a TCP connection idle
  through a sleep.
- The licence conclusions are a reading of the licence texts and the
  repository history, not legal advice.
