# Host names are unique across the koko server

A Host's name is unique among the koko server's Hosts that are not Retired,
whatever Space each is in, and that name is how `ssh` reaches it: both
`ssh web1` and `ssh web1.koko` work. One name means one machine for everyone
who can see it, and moving a Host between Spaces or renaming a Space never
changes how it is reached.

## Considered Options

- **Unique within a Space, reached as `web1.<space>.koko`**: no Member learns
  anything about other Spaces, but every address is longer, and moving a Host
  or renaming its Space breaks every script that reached it.
- **Unique within a Space, with `web1.koko` resolved among the Hosts the
  caller can see**: short and private, but one name points at different
  machines for different people, and the Owner's address stops working the
  day someone else records a Host with the same name.

## Consequences

- A Member who records or renames a Host to a name a Host in a Space they
  cannot see already holds is refused, and so learns that the name exists.
  The name is all they learn. This is the one place where something outside
  a Member's Spaces shows through (ADR 0011).
- `web1.koko` reaches a Host through the `Host *.koko` block of koko's own
  `~/.ssh/koko/ssh_config`. Bare `web1` works because koko also writes there
  the names of the Hosts the caller can see, and refreshes that list. A Host
  someone else records is reachable as `web1.koko` at once, but as bare
  `web1` only after the next refresh.
- The Include line sits at the top of `~/.ssh/config`, so a koko Host name
  shadows an entry of the same name in the user's own ssh configuration.
- Host names are single lowercase DNS labels with no dots, so `koko tunnel`
  reads the Host from either form by dropping a trailing `.koko`.
