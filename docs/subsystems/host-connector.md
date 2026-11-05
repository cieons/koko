# The Host connector

The koko server's one way to log in to a Host, used by Enroll and the Gateway,
in [`internal/server/hostconn`](../../internal/server/hostconn). Server-only.

## Connecting

[`Connector.Connect`](../../internal/server/hostconn/hostconn.go) takes the
Host as a `Target` (address, port, Host Account, `Login` and `Pin`) and, when
the Host has a Relay, the Relay as a second `Target`, which must have a pin
since a Relay is always Managed. It logs in to the Relay first, then opens a `direct-tcpip` channel through it to the Host's address
and port and runs the Host's SSH over that channel, so the Relay resolves the
Host's name on its own network. It returns a `Conn`: the Host's
`*ssh.Client`, the host key the Host offered, and `Close`, which closes the
Host's connection and then the Relay's (the embedded client's own `Close`
does not).

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

Each leg, the Relay's and the Host's, is tried up to 3 times. An attempt is
TCP (or the channel through the Relay) and the SSH handshake, login included,
bounded by `timing.HostConnectAttempt` (10 s); the waits before the second
and third attempts are `timing.HostConnectWait` (1 s) and twice that. Only a
network error met before the login is retried: refused, unreachable, timed
out, or reset or closed by the other end, whether met by the koko server or
reported by the Relay as a failed `direct-tcpip` open. Once the host key has
passed, the login has been sent, so nothing after it is retried: a key or a
password reaches a Host at most once per `Connect`. A caller that gives up
stops the waits at once.

| Error | Meaning | Retried |
| --- | --- | --- |
| `*HostKeyMismatchError` | the host key is not the pinned one; nothing was sent to log in with | never |
| `ErrLoginRefused` | the host key passed, and the Host rejected the key or password or disconnected during the login | never |
| `ErrUnreachable` | 3 attempts each ended by a network error before the login; wraps the last | (spent) |
| anything else | for example the connection lost during the login, a name that does not resolve, or a Relay that forbids forwarding | never |

A failure on the Relay's leg comes wrapped in a `*RelayError`: logging in to
the Relay, or losing the Relay's connection while reaching the Host through
it. Any other error is the Host's leg. An error's text names addresses and
the Host Account: it is for the log, and for Enroll's own caller, who can
act on the Host ([Enroll](enroll.md)); never for a message to anyone else.

## Timings in tests

The connect timings come through the timing-constants hook,
[`timing.Timings`](../../internal/shared/timing/timing.go), set as
`Connector.Timings`; its zero value is the product. See
[code layout](code-layout.md#test-only-hooks).

## Verifying

[`hostconn_test.go`](../../internal/server/hostconn/hostconn_test.go) drives
`Connect` against in-process Hosts
([`sshhost`](../../internal/harness/sshhost/sshhost.go)), each behaviour seen
from the Host's side: what logins it received and how often it was connected
to. [`retryable_test.go`](../../internal/server/hostconn/retryable_test.go)
covers the network errors loopback cannot cause, such as an unreachable host.
