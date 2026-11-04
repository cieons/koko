# Every SSH Session ends at the Gateway

People and agents never connect to a Host directly. The Gateway ends each
incoming SSH connection, decides access, and opens its own connection to the
Host with the koko key, so nothing that can log in to a Host leaves the koko
server. The one deliberate exception: the Owner can export the koko key, for
example to move the koko server.

## Considered Options

- **ProxyJump through a bastion**: SSH stays end-to-end, so every user must
  hold a credential the Host trusts, and revoking someone means waiting for
  that credential to expire or removing it from every Host.
- **Short-lived certificates issued to users who connect directly**: the same
  problem, and every Host must also be reachable from every user.

## Consequences

Removing a User takes effect on their next connection, and the Gateway can end
their live SSH Sessions. The Gateway is a single point of failure and the most
valuable target in the system.
