# A browser signs in with a passkey, through the Owner's identity provider or with a Password

A browser signs in to the web UI at the Web URL in one of three ways, each a
first factor on its own, the Owner's choice of 2026-11-15 (#228):

- **A Password,** as ADR 0043 set it.
- **A passkey:** a discoverable WebAuthn credential that requires user
  verification. Its RP ID is the Web URL's host and its origin the Web URL
  itself, so it works there alone, never through `koko web`. A User may
  hold several, each named, and no Password at all.
- **The Identity Provider:** one OpenID Connect provider the Owner runs,
  such as Authentik or Keycloak, found by discovery, through the
  Authorization Code flow with PKCE, `state` and `nonce`. A User links
  their own identity there, from a live Sign-in or through a Sign-in Link.
  The identity is the pair (issuer, `sub`), never an email or a name, and
  links to at most one User, who holds at most one. Nobody gets a koko
  User by signing in at the provider: Users are still invited.

The Password Link becomes the **Sign-in Link**: one link that lets its User
create a passkey, link their identity or set a Password, and signs that
browser in. It is spent only when the step chosen ends. `koko signin link`
issues one. `koko password link`, `password.link`, `password_link` and the
`/password#…` links issued before stay, as the CLI's and the API's contracts
require (ADR 0016, ADR 0049).

The Owner alone configures the provider, from any of the Owner's callers,
the web UI included: its issuer, client ID, client secret, sealed under the
Root Key (ADR 0018), and the name on its button. A change of issuer, or the
provider's removal, unlinks every identity. An Administrator does not
configure it, since that would unlink the Owner's identity, which only the
Owner acts on (ADR 0056).

A Sign-in records its method, `password`, `passkey` or `identity`, and the
credential it was made with. Removing a passkey, or unlinking an identity,
ends the Sign-ins made with it and no others. Setting a Password, or
spending a Sign-in Link, ends the User's other Sign-ins, since both are the
way back after a loss. The lifetimes stay 7 days unused and 30 days in all.

This supersedes these lines of ADR 0043:

- "with their User name and a password", now one of three ways;
- "A User has no password until they set one through a Password Link",
  since a Sign-in Link sets any of the three;
- "Passkeys may come later beside the password", which this fulfils;
- "ended … by a password change" as `password.remove` applied it, which now
  ends only the Sign-ins made with the Password;
- "The Owner is the only way back from a lost password", which now holds for
  every credential, through the Owner or an Administrator (ADR 0056).

It refines ADR 0042: a change of the Web URL's host leaves each passkey
recorded for its own host, unusable until the Web URL names that host again,
and moves the redirect URI the Owner registers at the provider.

## Considered Options

- **A passkey as a second step after the Password:** it would guard a
  phished Password, but every User would keep a Password to lose, and the
  person with a passkey gains nothing.
- **Creating a User at their first sign-in through the provider, or matching
  one by email:** no Invite to send, but anyone the provider admits would
  become a User, and an email is neither stable there nor proof of who the
  person is at koko.
- **Several providers:** an Owner's self-hosted provider is one, and it can
  federate others itself. Identities keep their issuer, so more can come
  later without migrating them.
- **The provider configured only with `koko server set` on the koko
  server's machine,** as the Web URL is: consistent, but every change in a
  container needs `docker exec`. The provider decides who signs in, not
  where devices connect, which is what keeps `server.set` local.
- **The name Password Link, kept:** nothing new in the CLI, but the Owner
  would issue a "Password Link" to someone who will make a passkey.
- **Removing any credential ends every Sign-in of the User,** as
  `password.remove` did: simpler, but retiring an old security key would sign
  out every other browser.
- **Asking for the current Password before a passkey is added or an
  identity linked:** a stolen Sign-in can already issue a Join Code for its
  own User, and a User with only passkeys has no Password to give.

## Consequences

- The callback from the provider is the one `/auth/` endpoint that changes
  state on a `GET`. The Origin check covers only `POST`s, so the callback
  is guarded instead by a flow held in memory, which it takes once, and by
  a `__Host-koko-oidc` cookie that binds the flow to the browser that began
  it. The cookie is `SameSite=Lax`, which a cross-site top-level navigation
  carries. A callback that fails the binding counts for no rate limit, since
  any site can make a browser load it.
- `response_mode` is `query`. A `form_post` answer would be a cross-site
  `POST`, which carries no `SameSite=Lax` cookie and which the Origin check
  refuses.
- koko calls the provider for discovery, its keys and the token exchange:
  outside any transaction, bounded in time and in calls at once. A provider
  that does not answer fails only the sign-ins through it.
- A sign-out at the provider ends no Sign-in at koko. A person removed
  there keeps their Sign-ins until they end, are revoked, or their identity
  is unlinked.
- Passkeys and the provider sign in no CLI, no `koko join` and no
  `koko web`; a device still joins with a Join Code (ADR 0013).
- New operations (`passkey.*`, `identity.*`, `server.oidc.*`) need this
  release on both ends. No request changes, so `compat.Oldest` stays.
  `koko password rm` from any CLI now ends only the Password's Sign-ins.
