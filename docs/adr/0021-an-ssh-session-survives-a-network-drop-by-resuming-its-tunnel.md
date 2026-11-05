# An SSH Session survives a network drop by resuming its Tunnel

A live SSH Session survives a drop of the device's network, because each
Tunnel carries a resumable byte stream beneath the SSH Session: its bytes are
framed with acknowledgements, and each end keeps what the other has not yet
acknowledged, up to 2 MiB per direction. The device end lives in the process
that opened the Tunnel, `koko tunnel` or `koko exec`. The other end lives in
the Gateway, which keeps the SSH Session and its connection to the Host
while the Tunnel is Detached, for a grace period of one minute. When the
Device Mux finds its Device Connection dead, it opens a new one with the
device's key, and each Tunnel Resumes on it with its id and the offset it
has received. This is the layering Teleport ships under its OpenSSH
ProxyCommand. Teleport's code is AGPL-3.0, so koko's implementation may
read it as a reference, but is written independently and never copied or
translated from it line by line.

## Considered Options

- **Importing or forking Teleport's `lib/resumption`**: it is AGPL-3.0 in
  every release that contains it, and its module cannot be imported on its
  own. No other Go library fits.
- **A resumable layer beneath the Device Connection**, Eternal Terminal's
  layering: the API and `koko web` channels would survive too. But it needs
  a token handshake of its own beneath SSH and a third first-bytes prelude
  on port 443, and every later transport (WSS in P1, QUIC in P2) would have
  to carry it.
- **The device end in the Device Mux**: `koko tunnel` would stay a stateless
  pipe. But a Device Mux crash would end every SSH Session on the device,
  and a command on the direct path (ADR 0017) could not resume.
- **A grace of minutes or days**, as Eternal Terminal and mosh keep: it
  covers a longer sleep, but holds Host connections and memory longer. Long
  work on a Host belongs in tmux or screen there.

## Consequences

- Only the device's leg resumes. When the Gateway's connection to a Host or
  a Relay drops, the Host's sshd has already ended the session, and the
  Gateway says which leg broke.
- Revocation needs nothing new: a Revoked key cannot open a Device
  Connection. A Tunnel Resumes only on a Device Connection of the Device
  Session that opened it, from any address, and a Detached SSH Session ends
  at once when its access goes, as an attached one does.
- A koko server restart or upgrade ends every SSH Session. Keeping them
  across a restart is P2.
- API calls do not resume. Every write carries an `Idempotency-Key`, whose
  result the koko server stores in the write's own transaction for ten
  minutes, so the CLI retries any call once after a drop and never reports
  an unknown outcome. `koko web`'s browser connections reopen.
- Every timing is a constant in P0, the grace included. koko's `Host *.koko`
  block sets `ServerAliveInterval 30` and `ServerAliveCountMax 4`, so ssh
  waits 150 s: it never ends an SSH Session that its Tunnel could still
  Resume, yet still ends one whose `koko tunnel` hangs. That value moves
  with the grace.
- An ssh that exits while its Tunnel is Detached leaves its SSH Session on
  the Gateway until the grace ends.
