# One HTTPS port; Tunnels over WSS

The koko server listens on one HTTPS port. The CLI reaches the Gateway through
a Tunnel over WSS, which passes proxies and CDNs that only allow 443 and also
works from a browser. Port forwards travel inside the SSH Session, so a Tunnel
only has to carry one reliable byte stream. Tunnels sit behind a transport
interface so QUIC can be added later for UDP and lossy networks, with WSS kept
as the fallback where UDP is blocked.

## Considered Options

- **Exposing the Gateway on port 22**: a second port to open and protect.
- **A layer-3 VPN (TUN device, routes, DNS)**: many times the complexity, and
  the job of Tailscale and WireGuard rather than koko.
