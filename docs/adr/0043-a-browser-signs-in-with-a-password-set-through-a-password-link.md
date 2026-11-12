---
status: its "never reaches a Host through the Gateway", superseded by ADR-0053
---

# A browser signs in with a password set through a Password Link

A person signs in to the web UI at the Web URL (ADR 0042) with their User
name and a password, the Owner's choice of 2026-11-09. A User has no password
until they set one through a Password Link: a one-time link the Owner alone
issues, valid for 15 minutes unless the Owner chooses longer (ADR 0044), with
its secret in the URL's fragment. `koko user invite <name> --web` creates a
User with one instead of a Join Code. `koko password link <user>` issues one
to an existing User, which is both the reset and the way a CLI User gets a
browser password. Each signed-in browser is a Sign-in, a record of its own:
listed on `/signins` and by `koko signin ls`, revoked there by its User or
by the Owner, ended by signing out, after 7 days unused or 30 days in all, by
a password change, and by Removing the User. A Sign-in acts as its User,
with exactly the access Space membership gives (ADR 0011). It is not a
Device Session, opens no Device Connection and never reaches a Host through
the Gateway.

## Considered Options

- **Passkeys**: nothing secret on the koko server and nothing to phish, but
  an unfamiliar step for the person this is for, a teammate who only uses a
  browser. Passkeys may come later beside the password.
- **A passkey or a password as a kind of Device Session, joined with a Join
  Code**: one revocation list for everything, but a browser would look like
  a device that can reach Hosts, and a Join Code would gain a second meaning.
- **A sign-in link issued from the CLI for every sign-in**: no password at
  all, but the person would depend on the Owner every time their browser
  signs out.
- **Resets by email**: no Owner in the loop, but koko sends no email.

## Consequences

- A password, unlike a device's key, can be guessed or phished. The guards:
  at least 15 characters with no composition rule (NIST SP 800-63B-4 for a
  sole factor); Argon2id (19 MiB, 2 passes, 1 lane) in a PHC string, sealed
  under the Root Key (ADR 0018), so a copy of the data directory offers no
  hash to guess against; at most 10 failures per 15 minutes from one
  address; and, after 10 failures in a row for one name, a 15-minute
  lockout of that name that leaves open Sign-ins working. A wrong name and
  a wrong password get the same answer.
- The Owner is the only way back from a lost password, by issuing a new
  Password Link. Setting a password ends the User's other Sign-ins and voids
  their unused link.
- Signing in, setting a password and signing out are the web front's own
  endpoints, not API operations, since the API never serves an
  unauthenticated caller (ADR 0015). Everything else a Sign-in does goes
  through the API as any caller's calls do.
- The Sign-in cookie is `__Host-`, `Secure`, `HttpOnly` and `SameSite=Lax`,
  so a link into the web UI opens signed in; every `POST` must carry the Web
  URL's Origin.
- TOTP fits as a second step after the password: a secret per User sealed
  beside the password, asked for at sign-in under the same limits, and
  cleared by a Password Link.
