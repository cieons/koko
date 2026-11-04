# Provider details are Attributes, not entities

koko has no Provider or Provider Account records. Where a Host comes from, its
account, region, price and renewal date are Attributes. koko ships Preset
Attributes with fixed types (`expires_at` is a date) so features such as
renewal reminders can rely on them; every other key is free text. Providers
differ too much to model well up front, and Attributes let users record what
they actually track.

## Consequences

Something an account's Hosts share, such as a provider panel password, is a
Secret in a Space rather than a field of an account record.
