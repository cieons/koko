-- When a User was Removed, as RFC 3339 in UTC to the second; NULL while they
-- belong to the koko server. A Removed User keeps their row, so their name is
-- never used again; they hold no live Device Session, no valid Join Code and
-- no membership, and no Space is their Personal Space any more.
ALTER TABLE users ADD COLUMN removed_at TEXT;
