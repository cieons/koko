# The Owner chooses how long a Join Code or Password Link stays valid

A Join Code and a Password Link (ADR 0043) stay valid for 15 minutes unless
whoever issues one chooses otherwise with `--valid <duration>`, from 1 minute
to 7 days, the Owner's choice of 2026-11-09. `koko user invite`, `koko device
add` and `koko password link` take it, and so do the web UI's Invite and Add
device forms. A link sent over chat to a teammate who opens it hours later
then still works. This refines ADR 0013's "short-lived".

## Considered Options

- **15 minutes always**: the least exposure, but a link opened later fails,
  and the Owner issues it again.
- **A longer default, such as 24 hours**: fewer reissued links, but every
  code, the CLI's included, would live longer than most need.
- **A longer validity only for links spent in a browser**: two rules for one
  kind of credential.

## Consequences

- A Join Code or Password Link that leaks stays usable for as long as its
  issuer chose, up to 7 days, until it is spent. Issuing a new Password Link
  voids the User's unused one; Removing a User voids both kinds.
- A Join Code's form does not change; only the expiry the koko server
  records does.
- The web UI's countdown counts the validity chosen, from when the code or
  link showed, in days and hours when more than an hour is left.
