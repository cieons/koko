# A Tunnel's Device Session authenticates its SSH Session

A device signs in once with the OAuth device-authorization flow and holds a
Device Session. A Tunnel is opened with that Device Session, so the Gateway
already knows who is on it and accepts SSH `none` authentication for
connections that arrive through a Tunnel. Users manage no SSH keys or
certificates, and revoking a Device Session cuts its Tunnels.

## Consequences

An SSH connection that does not arrive through a Tunnel, such as a direct
Gateway port if one is ever exposed, must authenticate with a registered
public key instead.
