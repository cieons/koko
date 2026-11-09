-- Passwords, Password Links and Sign-ins: how a browser signs in to the web
-- UI on the web listener (ADR 0043). Every time is RFC 3339 in UTC to the
-- second.
--
-- passwords: a User's one Password, or no row. sealed is its Argon2id hash
-- as a PHC string, sealed under the Root Key for the row's id (ADR 0018), so
-- a copy of the data directory holds no hash to guess against offline.
-- set_how is how it was last set: 'link' (through a Password Link) or
-- 'change' (by the current Password).
CREATE TABLE passwords (
    id      TEXT PRIMARY KEY,
    user_id TEXT NOT NULL UNIQUE REFERENCES users (id),
    sealed  BLOB NOT NULL,
    set_at  TEXT NOT NULL,
    set_how TEXT NOT NULL
) STRICT;

-- password_links: each one-time link issued for a User to set their
-- Password. The secret is never kept, only its SHA-256, so a copy of the
-- database sets nobody's Password. issued_by is the Device Session or the
-- Sign-in that issued it, or NULL for the Unix socket. A link is valid while
-- it is neither used nor voided and has not expired; a User holds at most
-- one valid link. Spent links are kept, for the P1 audit log.
CREATE TABLE password_links (
    id          TEXT PRIMARY KEY,
    user_id     TEXT NOT NULL REFERENCES users (id),
    secret_hash BLOB NOT NULL UNIQUE,
    issued_by   TEXT,
    issued_at   TEXT NOT NULL,
    expires_at  TEXT NOT NULL,
    used_at     TEXT,
    voided_at   TEXT
) STRICT;

CREATE INDEX password_links_by_user ON password_links (user_id);

-- signins: each browser signed in with a User's Password. The cookie's
-- token is never kept, only its SHA-256. client is a short label made from
-- the User-Agent at sign-in. started_address and address are the client's
-- address at sign-in and when last used. expires_at is 30 days after
-- started_at; a Sign-in also ends 7 days after last_used_at, which moves at
-- most once a minute. ended_at, end_reason ('signed_out', 'expired',
-- 'revoked', 'password_changed', 'password_removed', 'user_removed') and
-- ended_by (a Device Session's or a Sign-in's id, or 'unix socket') are set
-- once it ends. Ended Sign-ins are kept, for the P1 audit log.
CREATE TABLE signins (
    id              TEXT PRIMARY KEY,
    user_id         TEXT NOT NULL REFERENCES users (id),
    token_hash      BLOB NOT NULL UNIQUE,
    client          TEXT NOT NULL,
    started_address TEXT NOT NULL,
    address         TEXT NOT NULL,
    started_at      TEXT NOT NULL,
    last_used_at    TEXT NOT NULL,
    expires_at      TEXT NOT NULL,
    ended_at        TEXT,
    end_reason      TEXT,
    ended_by        TEXT
) STRICT;

CREATE INDEX signins_by_user ON signins (user_id);
