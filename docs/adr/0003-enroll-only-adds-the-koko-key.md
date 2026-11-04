# Enrolling a Host only adds the koko key

koko changes a Host only as far as access needs. Enroll logs in once with the
password or key the user supplies, pins the host key, and appends the koko key
to the Host Account's `authorized_keys`; every existing key, account and sshd
setting stays as it was. The supplied password or key is used once and not
stored, and one koko key serves the whole koko server.

## Considered Options

- **A koko user CA (`TrustedUserCAKeys`)**: short-lived per-session
  certificates, but it rewrites sshd configuration and reloads sshd on machines
  the user may not fully own.
- **Keeping the supplied password or key**: no change to the Host at all, but
  koko loses access the day that password changes.
- **Creating dedicated accounts and hardening sshd**: too intrusive for
  machines that already run things.
- **One koko key per Host**: every private key would sit in the same store
  anyway, and revoking koko from one Host is already one deleted line.

## Consequences

Deleting that one `authorized_keys` line revokes koko from the Host. The user's
own existing access is untouched and is the break-glass path. Host logs show
every SSH Session as the koko key; who did what is recorded by koko, not by
the Host.
