-- Device Sessions: the key each device registered when it joined, under a
-- device name that never changes. public_key is the key in SSH wire format,
-- stored plain and unique across every Device Session, Revoked ones
-- included, so a Revoked key can never join again. added_by is the Device
-- Session that issued the Join Code the device joined with, or NULL for the
-- Unix socket. created_at and revoked_at are RFC 3339 in UTC to the second;
-- a Device Session is live until it is Revoked, and kept after, for the P1
-- audit log. A device name is unique among a User's live Device Sessions.
CREATE TABLE device_sessions (
    id         TEXT PRIMARY KEY,
    user_id    TEXT NOT NULL REFERENCES users (id),
    device     TEXT NOT NULL,
    public_key BLOB NOT NULL UNIQUE,
    added_by   TEXT REFERENCES device_sessions (id),
    created_at TEXT NOT NULL,
    revoked_at TEXT
) STRICT;

CREATE UNIQUE INDEX device_sessions_live_name ON device_sessions (user_id, device) WHERE revoked_at IS NULL;
