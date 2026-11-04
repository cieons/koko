# Access is decided by Space membership alone

Every Host and every Secret lives in one Space. A Member can do everything
inside the Spaces they belong to and sees nothing outside them; the Owner sees
every Space. Attributes never decide access.

## Considered Options

- **Grants as Attribute selectors (`region=cn`), with Secrets visible by
  selector**: access would follow new Hosts automatically, but anyone allowed
  to edit an Attribute could widen their own access, and each new case needed
  one more rule to close the gap.
- **Full RBAC with custom roles**: more than a fleet of this size needs.

## Consequences

Every User has a Personal Space where what they record goes by default, and a
departing Member's Personal Space passes to the Owner. Moving a Host between
Spaces needs membership in both. A Host's Relay must be a Host its editor can
see. Read-only membership is not part of the first release.
