# SSH end to end; the web UI through the CLI

The CLI reaches the koko server over SSH, and P0 has no TLS anywhere: HTTPS
made a certificate an install step, and a domain with ACME the only way to
get one without warnings. The koko server listens for SSH on one port, 443 by
default and configurable, because 443 passes firewalls that filter by port
and port 22 stays with the machine's own sshd. It generates its host key at
first start, and every device pins that key from its Join Code (ADR 0013).
One Device Connection carries everything a device does: the API, and the
Tunnels that `ssh`, `scp` and `rsync` open through `koko tunnel` to the
Gateway, which still ends every SSH Session (ADR 0002). The web UI is not
exposed on the network: `koko web` forwards a localhost port on the device
over its Device Connection, and being able to run it is the sign-in. This
supersedes ADR 0004.

## Considered Options

- **HTTPS with automatic certificates** (ACME for a domain, then a Let's
  Encrypt IP certificate, then self-signed with the CLI pinning it):
  certificates stay an install step, and web passkeys need a domain anyway.
  WebAuthn rejects an IP address as the RP ID and Chrome blocks it on a
  certificate error, so a bare-IP install would still have no web sign-in.
- **sslip.io-style hostnames as a free domain**: passkeys would be bound to a
  third party's domain.
- **A layer-3 VPN (TUN device, routes, DNS)**: many times the complexity, and
  the job of Tailscale and WireGuard rather than koko.

## Consequences

- Networks that pass only HTTP(S) proxies, or that intercept TLS, cannot
  connect until the P1 fallback, SSH over WSS, exists. It is the one path
  that will need TLS, and it can share port 443, told apart by the client's
  first bytes: an SSH banner or a TLS ClientHello. QUIC beneath SSH stays P2.
- Some networks throttle SSH more readily than TLS.
- The web UI needs the CLI installed first, phones have no access in P0, and
  an Invite is accepted in the CLI. P0 has no passkeys and no passwords.
- The `ssh_config` block that runs `koko tunnel` does not change; only the
  transport under `koko tunnel` does.
