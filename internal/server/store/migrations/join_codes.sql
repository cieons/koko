-- Join Codes: each one-time code issued for one of a User's devices. The
-- code itself is never kept, only the SHA-256 of its secret, so a copy of
-- the database lets nobody join. device is the device name bound to the code.
-- issued_by is the Device Session that issued it, or NULL for the Unix
-- socket. expires_at, used_at and voided_at are RFC 3339 in UTC to the
-- second; a code is valid while it is neither used nor voided and has not
-- expired. Spent codes are kept, for the P1 audit log.
CREATE TABLE join_codes (
    id          TEXT PRIMARY KEY,
    user_id     TEXT NOT NULL REFERENCES users (id),
    secret_hash BLOB NOT NULL UNIQUE,
    device      TEXT NOT NULL,
    issued_by   TEXT,
    expires_at  TEXT NOT NULL,
    used_at     TEXT,
    voided_at   TEXT
) STRICT;

CREATE INDEX join_codes_by_user ON join_codes (user_id, device);
