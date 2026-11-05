# PROTOTYPE: an SSH Device Connection and a terminating Gateway

Throwaway code for
[Prototype: an SSH Device Connection and terminating Gateway carrying ssh, scp and rsync](https://github.com/cieons/koko/issues/15).
It lives on branch `prototype/ssh-tunnel` and is never merged. It is its
own Go module, so `make check` at the repo root does not see it.

## Question

Does the shape in ADR 0002, 0012 and 0013 work with unchanged OpenSSH
tooling? A koko server authenticates a device by its registered key;
`koko tunnel %h`, the ProxyCommand from
[#6](https://github.com/cieons/koko/issues/6), opens a Device Connection and
a Tunnel on it; the Gateway ends the SSH Session inside the Tunnel with
`none` authentication and opens its own connection to the Host.

## Answer

Yes. `ssh` (shell, pty, exec, exit status, stderr), `scp` (SFTP and legacy
`-O`), `sftp`, `rsync` and `-L`, `-D`, `-R` all ran unchanged through it,
with matching checksums, using the #6 `ssh_config` block word for word.
The OpenSSH client sends one `none` request and nothing else.

What it costs:

- **Per keystroke: nothing measurable.** At 40 ms RTT a keystroke's echo
  took 48.7 ms through koko against 48.1 ms direct. SSH inside SSH adds no
  round trip.
- **Bulk transfer: no loss.** It ran as fast as direct on loopback and at
  40 ms RTT. The Gateway uses 3.5 to 4.4 s of CPU per GiB on an M4 Pro.
- **Per `ssh` invocation: about 5 extra round trips.** At 40 ms RTT,
  `ssh web1.koko true` took 623 ms against 411 ms direct. With one shared
  Device Connection it took 353 ms, faster than direct, because `none` saves
  the two public-key round trips and the Host login happens next to the Host.

## Run it

Everything runs under one scratch directory `$S`. ssh runs with `-F`, a
scratch known_hosts and a scratch key, so `~/.ssh` is never read for config
and never written. Listeners bind 127.0.0.1 only. The Host is a disposable
Alpine sshd in docker.

```sh
docker build -t koko-proto-host host/          # the disposable Host
./up.sh "$S"                                   # keys, Host container (--init), join, ssh config
KOKO_PROTO_HOME=$S/home "$S/bin/kokoproto" server &               # koko server on :47443
"$S/bin/kokoproto" delay -listen 127.0.0.1:47444 -to 127.0.0.1:47443 -one-way 20ms &
"$S/bin/kokoproto" delay -listen 127.0.0.1:47023 -to 127.0.0.1:47022 -one-way 20ms &
./try.sh "$S"                                  # functional checks -> results/try.log
./measure.sh "$S"                              # measurements      -> results/measure.log
docker rm -f koko-proto-web1                   # afterwards
```

Then `KOKO_PROTO_HOME=$S/home ssh -F $S/home/ssh/config web1.koko`.

| File | What it is |
| --- | --- |
| `server.go` | koko server: Device Connections authenticated by registered key, revocation |
| `gateway.go` | Gateway: `none` auth inside the Tunnel, its own connection to the Host, channel and request relay |
| `tunnel.go` | `koko tunnel <name>` |
| `state.go` | the plain files standing in for the record |
| `tools.go` | `join` (stands in for a Join Code), `revoke`, `delay` (WAN emulator), `bench` |
| `host/` | the disposable Host image |
| `results/` | what the runs printed, verbatim |

Shortcuts this prototype takes and the real thing will not: `join` writes the
device key and server files directly instead of redeeming a Join Code; the
record is files, not SQLite; there are no Spaces or access checks; the name
maps to a Host by its first label; revocation is noticed by polling a file
every 500 ms; nothing is logged beyond `log.Printf`.

## Findings

### Unchanged tooling works

All in [results/try.log](results/try.log), against the #6 block as written:

| Check | Result |
| --- | --- |
| `ssh -tt web1.koko 'tty; echo out; echo err >&2; exit 7'` | pty `/dev/pts/0`, stdout, stderr, exit 7 |
| `scp` up and down (SFTP), `scp -O` up, `sftp -b` put and get | all exit 0, 32 MiB, sha256 match |
| `rsync -a -e "ssh -F ..."` up and down (macOS openrsync 2.6.9 to rsync 3.5.0) | exit 0, sha256 match |
| `ssh -N -L 47180:127.0.0.1:8080` to a Service on the Host's loopback | `curl` got the page |
| `ssh -N -D 47181`, `curl --socks5-hostname` | got the page |
| `ssh -R 47182:127.0.0.1:47183` to a server on the device, `wget` on the Host | got the page |
| `ssh -o SendEnv=LANG` | reached the Host, which has `AcceptEnv LANG LC_*` |

The Gateway relays channels and requests generically: `session` and
`direct-tcpip` from the client, `forwarded-tcpip` from the Host,
`tcpip-forward` and `cancel-tcpip-forward` global requests, and every
channel request except `auth-agent-req@openssh.com` and `x11-req`, which it
refuses. Everything else is refused and logged.

### What OpenSSH does with `none`

With the #6 block, `ssh -vvv` shows one `send packet: type 50` (the
`none` request), `receive packet: type 52` (success), and `Authenticated to
web1.koko (via proxy) using "none".` No key is offered, no agent is asked,
nothing prompts. The request carries the SSH user name: the local login
(`cieons`) or `bob` from `bob@web1.koko`. The Gateway logged it and
logged in as the Host Account anyway. ssh still lists its identity files at
start (here the scratch `keys/none`), but never offers one.

The Gateway can refuse `none` with a reason: an `ssh.BannerError` from
`NoClientAuthCallback` becomes a `USERAUTH_BANNER` that ssh prints. The
prototype uses it when the Host is down (`koko: cannot reach web2: ... connection refused`).

### How the Host name travels

ssh lowercases `%h` and drops the user and port: `ssh -p 2222
bob@WEB1.koko` runs `koko tunnel 'web1.koko'`. On the Device Connection
the Tunnel is a channel of type `tunnel@koko` whose payload is that name as
one SSH string. The koko server resolves it before accepting the channel. A
refusal travels as the channel-open failure's description, which `koko
tunnel` prints (`koko: no Host named "web9.koko" in your Spaces`).

The same Tunnel also works as a standard `direct-tcpip` channel with the
name as its host, which is what OpenSSH's own `ssh -W %h:%p` opens. The
`*.kokomux` lines in `up.sh` use that to stand in for a shared Device
Connection, with OpenSSH's ControlMaster on the outer connection.

### What the user sees when something is wrong

ssh adds its own last line whenever `koko tunnel` exits before SSH starts.
koko cannot change that line.

| Situation | What `ssh web1.koko` printed (exit 255 in every case) |
| --- | --- |
| device has not joined (no key) | `koko: this device has not joined a koko server, so it has no Device Session.` / `koko: run \`koko join <Join Code>\` with an Invite, or with a code from \`koko device add\` on a device that has joined.` / `Connection closed by UNKNOWN port 65535` |
| Device Session revoked | `koko: the Device Session "alice/laptop" of this device was revoked.` / `koko: join again with a new Join Code: koko join <Join Code>` / `Connection closed by UNKNOWN port 65535` |
| koko server down | `koko: cannot reach the koko server at 127.0.0.1:47499: dial tcp ...: connection refused` / `Connection closed ...` |
| koko server shows another host key | `koko: Device Connection to 127.0.0.1:47443 failed: ssh: handshake failed: the koko server at ... presented host key SHA256:Hw..., but this device pinned SHA256:AA... when it joined` / `Connection closed ...` |
| name koko does not know | `koko: no Host named "web9.koko" in your Spaces` / `Connection closed ...` |
| Host down | `koko: cannot reach web2: dial tcp 127.0.0.1:47099: connect: connection refused` (the Gateway's banner) / `Connection closed ...` |

The revoked message is sent only after the client has proven it holds the
key (`VerifiedPublicKeyCallback`), so only the holder of a revoked key
learns it was revoked. An unknown key gets `koko: this koko server does not
know this device's key.` To tell the two apart, the server must keep revoked
Device Sessions instead of deleting them.

**Under ControlPersist, `koko tunnel`'s stderr is lost.** ssh sends the
proxy's stderr to `/dev/null` for a ControlPersist master, so with
`-o ControlMaster=auto -o ControlPersist=30` an unknown name printed only
`Connection closed by UNKNOWN port 65535`. The Gateway's banner (Host down)
still showed, because ssh itself prints it
([results/extra.log](results/extra.log)).

### Revocation during an open SSH Session

Within the 500 ms poll, the Gateway wrote to each open session's stderr and
the server closed the Device Connection. ssh printed
([results/extra.log](results/extra.log); [results/try.log](results/try.log),
recorded before `koko tunnel` waited for the Tunnel to drain, has the two
koko lines the other way round):

```
tick 5

koko: the Device Session "alice/laptop" was revoked; this SSH Session is closed.

koko: the Device Connection to 127.0.0.1:47443 closed.
Connection to web1.koko closed by remote host.
Connection to web1.koko closed.
client_loop: send disconnect: Broken pipe
```

The last three lines are ssh's own, and it exits 255. The next `ssh`
gets the revoked message from the table above.

### How `koko tunnel` exits

- **stdin EOF**: it half-closes the Tunnel and keeps relaying until the
  Gateway closes. With `</dev/null` it read the Gateway's 12-byte version
  line `SSH-2.0-Go`, then exited 0 within 3 ms.
- **When ssh exits** it closes the proxy's stdin, then sends SIGHUP 0.1 to
  0.4 ms later. `koko tunnel` exits on whichever comes first: Tunnel EOF,
  SIGHUP, or SIGPIPE when writing to the dead ssh (one in six runs). In
  every run after the first, the Device Connection closed within 1 ms of
  ssh closing its end. In the very first run, one Device Connection stayed open
  16 s. That never happened again in eight runs, and is unexplained.
- **SIGHUP with stdin still open**: closes the Device Connection, exit 0.
- **Ctrl-C without a tty** sends SIGINT to ssh and `koko tunnel` alike,
  since they share a process group. The prototype ignores SIGINT, leaving
  the decision to ssh, which then sends SIGHUP: a clean close. With Go's
  default handling, `koko tunnel` dies at once. ssh exits 255 either way.
- Exit status 0 after a normal close, 1 when it cannot open the Tunnel or
  the Device Connection drops, 2 for bad usage. ssh ignores it.

## Measurements

Apple M4 Pro, macOS 27.0.1, OpenSSH_10.3p1; Host in OrbStack (Alpine
3.22.7, OpenSSH_10.0p2), reached through docker's port publishing on
127.0.0.1. "40 ms RTT" puts `kokoproto delay` (20 ms each way, no bandwidth
limit, no loss) between the device and the koko server, and between the
device and the Host for the direct baseline. The Gateway reaches the Host
over loopback, as a koko server near its Hosts would. The emulator accepts
TCP locally, so a real network adds one more RTT to every row.
Verbatim output: [results/measure.log](results/measure.log) and
[results/extra.log](results/extra.log).

### Setup time each `ssh` invocation pays

`ssh <name> true`, wall clock, p50 (min to max):

| Path | loopback, n=30 | 40 ms RTT, n=15 |
| --- | --- | --- |
| direct to the Host, no koko | 107.6 ms (77-132) | 411.5 ms (397-432) |
| `koko tunnel`: a Device Connection per invocation | 110.5 ms (94-125) | 623.1 ms (607-630) |
| one shared Device Connection (`ssh -W` over ControlMaster) | 115.1 ms (90-128) | 352.7 ms (329-364) |
| the user's own ControlMaster on `*.koko`, after the first | 8.1 ms (6-17) | not run |

Where the 623 ms goes at 40 ms RTT (one run, `koko tunnel` trace and server log):

| Step | ms | RTTs |
| --- | --- | --- |
| Device Connection: version, key exchange, `none` probe, key query, signature | 268 | ~6.7 |
| open the Tunnel channel | 44 | ~1.1 |
| inner SSH to the Gateway, up to `none` accepted | 184 | ~4.6 |
| the session itself (open, exec, exit) and process start | ~127 | |

The Gateway dials the Host while the inner handshake runs, so its 35 ms
Host login was hidden here. Done one after the other, it would add 35 ms
plus the Host's RTT. About 2 RTT of the Device Connection come from
x/crypto's client always sending `none`, then a key query, before the
signature. Skipping those means patching or replacing x/crypto's client
auth; not tried.

### Interactive latency

`ssh -tt ... 'stty raw -echo; cat'`: one byte out, wait for it to come
back, 15 ms apart.

| Path | p50 | p90 | p99 |
| --- | --- | --- | --- |
| direct, loopback (n=300) | 4.63 ms | 6.66 ms | 11.23 ms |
| koko, loopback (n=300) | 3.52 ms | 6.05 ms | 29.78 ms |
| direct, 40 ms RTT (n=150) | 48.13 ms | 50.00 ms | 55.34 ms |
| koko, 40 ms RTT (n=150) | 48.66 ms | 50.80 ms | 57.39 ms |

### Throughput

512 MiB of random data on loopback, p50 of 3 runs, MiB/s:

| | raw up | raw down | scp up | scp down | rsync up |
| --- | --- | --- | --- | --- | --- |
| direct | 398 | 457 | 251 | 277 | 164 |
| koko | 436 | 421 | 279 | 262 | 202 |
| koko, Gateway offering AES-GCM only | 538 | 561 | | | |

At 40 ms RTT (128 MiB, p50 of 2): raw up direct 36, koko 36; raw down
direct 39, koko 36, shared Device Connection 39. Every hop has a 2 MiB
channel window (x/crypto hard-codes it; OpenSSH's default is the same), so
one transfer tops out near 2 MiB per RTT. Nesting did not make it worse.

### CPU

- **koko server**: 4.4 s of CPU per GiB relayed (raw transfers) with
  x/crypto's default ciphers; 6.5 s/GiB over the scp and rsync mix. The
  inner hop negotiates chacha20-poly1305, OpenSSH's first choice, and Go's
  poly1305 has no arm64 assembly. A Gateway that offers only AES-GCM inside
  the Tunnel used 3.5 s/GiB and moved 22 to 30 % more.
- **`koko tunnel` on the device**: 1.07 to 1.12 s of CPU per 512 MiB, its
  second layer of encryption. An `ssh ... true` costs it 10 to 14 ms of CPU.

## Decisions this raises

Each goes to the ticket named. The prototype's own choice comes first.

1. **One Device Connection per `ssh`, or a shared one**
   ([CLI-server API](https://github.com/cieons/koko/issues/18)). One per
   invocation keeps `koko tunnel` stateless and costs about 5 RTT per
   `ssh`. Sharing one needs a long-lived local process and a socket path
   under 104 bytes on macOS. A ControlMaster line in koko's block would be
   cheaper still, but it hides `koko tunnel`'s errors and overrides the
   user's own choice.
2. **Where errors are reported**
   ([Gateway and Tunnel behaviour](https://github.com/cieons/koko/issues/16)).
   Errors on `koko tunnel`'s stderr vanish under ControlPersist; the
   Gateway's banner does not. Errors after the Device Connection is up
   (unknown name, no access, Host down) could all go through the Gateway's
   banner.
3. **Which host key the Gateway presents inside the Tunnel**
   ([Gateway and Tunnel behaviour](https://github.com/cieons/koko/issues/16)).
   The prototype used the koko server's own key: one known_hosts line under
   the `HostKeyAlias`, written from the Join Code, with nothing to fetch.
4. **The Tunnel's channel type**
   ([CLI-server API](https://github.com/cieons/koko/issues/18)). A
   `tunnel@koko` channel can grow fields later. Also accepting
   `direct-tcpip` lets OpenSSH's own `ssh -W` open a Tunnel.
5. **Gateway defaults the prototype suggests**
   ([Gateway and Tunnel behaviour](https://github.com/cieons/koko/issues/16)):
   offer only AES-GCM inside the Tunnel; dial the Host while the inner
   handshake runs (a Host login is then spent even when the client gives
   up); refuse agent and X11 forwarding without a message, since ssh asks
   with want-reply false; ignore the SSH user name and `-p`; keep revoked
   Device Sessions so the holder can be told; `koko tunnel` ignores SIGINT.

## Not verified

- No Linux or Windows client. Only macOS OpenSSH 10.3p1 against a Linux
  OpenSSH 10.0p2 Host.
- No real WAN: the emulator adds delay only, with no loss, jitter or
  bandwidth limit, and does not delay the TCP handshake.
- docker's port publishing sits in both the direct and the koko path; on
  loopback it, not the Gateway, may set the throughput ceiling.
- The 16 s lingering Device Connection in the first run.
- ControlPersist together with revocation; long-lived sessions; many
  concurrent Tunnels on one koko server.
