# The Host connector

The koko server's one way to log in to a Host, used by Enroll and the Gateway,
in [`internal/server/hostconn`](../../internal/server/hostconn). Server-only.

## Connecting

[`Connector.Connect`](../../internal/server/hostconn/hostconn.go) takes the
Host as a `Target` (address, port, Host Account, `Login` and `Pin`) and its
chain of Relays as a list of `Target`s in dial order, empty for a Host with
no Relay ([ADR 0055](../adr/0055-a-relay-may-have-a-relay-of-its-own.md)).
Each Relay must have a pin, since a Relay is always Managed; one without is
a `*RelayError` before any dial.

It makes one leg after another, each its own SSH connection: it logs in to
the first Relay over TCP, then to each next Relay, and to the Host last,
through a `direct-tcpip` channel on the leg before, so each Relay resolves
the next hop's address on its own network and sees only the encrypted
connection to it. Each leg checks its own pin, logs in as its own Host
Account, gets a leg's retries and its own keepalive, and its connection
goes through `sshconn.Wrap`. `Connect` returns a `Conn`: the Host's
`*ssh.Client`, the host key the Host offered, and `Close`, which closes the
Host's leg and then each Relay's, the last dialled first (the embedded
client's own `Close` does not).

The whole `Connect`, every leg and every wait, is bounded by
`timing.HostConnect` (45 s): a web terminal's page hears nothing while the
Gateway dials, and a proxy cuts a quiet connection at 60 s. A leg the bound
cuts fails as `ErrUnreachable`, even once its login was sent. A dial that
fails on one leg after quick ones spends at most a leg's 33 s and never
meets the bound.

A `Login` is a private key (`KeyLogin`: the koko key, or a key supplied to
Enroll) or a password (`PasswordLogin`). A password answers the `password`
method and each no-echo question of `keyboard-interactive`, so an sshd that
asks through PAM gets it too; once one of them is refused the login ends, so
a Host that offers both counts one failure, not two. The secret lives only
inside the SSH authentication methods, so printing a `Target` never shows it.

## The host key

The connector checks the host key during the key exchange, before it sends
anything to log in with. A `Pin` is a whole key (`PinKey`, as Enroll stores
it) or only its SHA256 fingerprint (`PinFingerprint`, as `--host-key` gives
it). A key other than the pinned one ends the attempt with a
`HostKeyMismatchError` holding both `SHA256:` fingerprints. The zero `Pin`
(Enroll's first sight) accepts the key offered and returns it.

It asks for host key algorithms in OpenSSH's order: `ssh-ed25519`, ECDSA
(nistp256, 384, 521), then RSA with SHA-2 (`rsa-sha2-512`, `rsa-sha2-256`).
A pinned key's own algorithms move to the front, so a Host holding several
host keys offers the pinned one; a Host that no longer holds its type offers
another, which is refused as a mismatch rather than failing the negotiation.
A fingerprint-only pin does not say its key's type, so a Host that offers
another key is asked again without that key's type, until it offers the
pinned key or has no other (the negotiation finds no common algorithm); a
mismatch then names the key it offered first.
Certificates are not asked for, since koko pins keys, nor DSA and SHA-1 RSA,
which OpenSSH no longer accepts by default.

## Retries and failures

Each leg, each Relay's and the Host's, is tried up to 3 times. An attempt is
TCP (or the channel on the leg before) and the SSH handshake, login included,
bounded by `timing.HostConnectAttempt` (10 s); the waits before the second
and third attempts are `timing.HostConnectWait` (1 s) and twice that. Only a
network error met before the login is retried: refused, unreachable, timed
out, or reset or closed by the other end, whether met by the koko server or
reported by the Relay before as a failed `direct-tcpip` open. Once the host
key has passed, the login has been sent, so nothing after it is retried: a
key or a password reaches each Host, Relay or not, at most once per
`Connect`. A leg beneath that ends fails the attempt as that leg's, and it
is not tried again. A caller that gives up stops the waits at once.

| Error | Meaning | Retried |
| --- | --- | --- |
| `*HostKeyMismatchError` | the host key is not the pinned one; nothing was sent to log in with | never |
| `ErrLoginRefused` | the host key passed, and the Host rejected the key or password or disconnected during the login | never |
| `ErrUnreachable` | 3 attempts each ended by a network error before the login, wrapping the last; or a leg cut by `timing.HostConnect` | (spent) |
| anything else | for example the connection lost during the login, a name that does not resolve, or a Relay that forbids forwarding | never |

A failure on a Relay's leg comes wrapped in a `*RelayError`, whose `Hop` is
that Relay's place in the chain, from 0 for the first dialled:

- a failure while logging in to a Relay's leg is that Relay's: its pin, its
  refusal of the koko key, or a network error, a refused `direct-tcpip` open
  on the leg before included, which is the next hop's network error;
- the leg before ending while the connector dials through it is the leg
  before's: the first leg, nearest the koko server, found ended;
- a failure on the Host's own leg is the Host's, with no `*RelayError`.

An error's text names addresses and
the Host Account: it is for the log, and for Enroll's own caller, who can
act on the Host ([Enroll](enroll.md)); never for a message to anyone else.

## Keeping a connection alive

Once a leg, a Relay's or the Host's, is logged in, the connector watches
the bytes that arrive on it with
[`liveness`](../../internal/shared/liveness/liveness.go), as both ends of a
Device Connection do. After `timing.HostProbe` (15 s) without one, it sends
a `keepalive@openssh.com` wanting a reply; any byte that arrives, a reply
or a refusal included, counts. After `timing.HostDead` (60 s) without any,
it ends the connection.

[`Conn.Broken`](../../internal/server/hostconn/hostconn.go) says why a
connection ended, unless `Close` ended it: a `*liveness.DeadError`, its
`Quiet` the 60 s, when the connector ended it, or the error the network
or the other end ended it with, such as `io.EOF` for a Host that closed it.
A Relay's leg that ends takes every leg after it with it, so `Broken`
reports the leg nearest the koko server whose connection ended: a
`*RelayError` with its `Hop`, or, when every Relay's leg lives, the Host's.
It is nil while the connection lives and after `Close`. Each leg's end is
set before the SSH client above it learns of it, so a Relay's end is set
before the leg after it reads its own, and a channel that ends because its
connection did already sees it: the Gateway relies on that to tell a broken
leg from a channel the Host closed
([the Gateway](gateway.md#when-an-ssh-session-ends)). A Relay that stops
answering silences every leg after it too, and one of those may be found
dead first, but its close passes through the silent Relay, which never
answers it, so the Host's connection ends only with the Relay's own leg, and
`Broken`, read then, names the Relay.

## Timings in tests

The connect and keepalive timings come through the timing-constants hook,
[`timing.Timings`](../../internal/shared/timing/timing.go), set as
`Connector.Timings`; its zero value is the product. See
[code layout](code-layout.md#test-only-hooks).

## Verifying

[`hostconn_test.go`](../../internal/server/hostconn/hostconn_test.go) drives
`Connect` against in-process Hosts
([`sshhost`](../../internal/harness/sshhost/sshhost.go)), each behaviour seen
from the Host's side: what logins it received and how often it was connected
to, the keepalives a quiet Host receives, and `Broken` for a Host that stops
answering, one that closes the connection, and a connection `Close` closed.
Chains of 1, 2 and 4 Relays show each Relay's one login and its one
`direct-tcpip` to the next hop; each failure at each hop of a chain of 3
carries that hop, and `Broken` names the dropped Relay nearest the koko
server, and a Relay that stops answering at either hop of a chain of 2, even
after the Host behind it did.
[`retryable_test.go`](../../internal/server/hostconn/retryable_test.go)
covers the network errors loopback cannot cause, such as an unreachable host.
