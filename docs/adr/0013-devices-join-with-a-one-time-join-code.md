# Devices join with a one-time Join Code

A device joins a koko server with a one-time Join Code, which carries the
koko server's address and host key fingerprint, so the device verifies its
first connection instead of trusting it. The joining device generates its own
key, which never leaves it. The registered key is its Device Session: every
Device Connection authenticates with it, and revoking it closes them. An
Invite is a Join Code for a new User. A User adds another device by running
`koko device add` on one that has already joined, and the Owner can also run
it on the koko server over the Unix socket (ADR 0008). The Gateway accepts
SSH `none` authentication for an SSH Session that arrives through a Tunnel,
because its Device Connection already says who is on it. This supersedes ADR
0005 and keeps its principle.

## Considered Options

- **The OAuth device-authorization flow (RFC 8628)**: a signed-in browser
  confirms each new device, and P0 has no web sign-in (ADR 0012).

## Consequences

- Users manage no SSH keys or certificates by hand: the CLI makes and keeps
  each device's key.
- An Owner who loses every device logs in to the koko server's machine with
  their own existing access and issues a Join Code over the Unix socket.
