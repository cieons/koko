-- Passkeys, Linked Identities, the Identity Provider and a Sign-in's method:
-- the three ways a browser signs in to the web UI (ADR 0060). Times are RFC
-- 3339 in UTC to the second.

-- passkeys: each Passkey a User made for the Web URL's host (rp_id) (ADR
-- 0060). public_key is its COSE_Key as the authenticator gave it, algorithm
-- its COSE algorithm (-8 EdDSA, -7 ES256, -257 RS256); neither is secret.
-- sign_count is the authenticator's last signature counter, 0 for one that
-- keeps none. backup_eligible is the authenticator's backup eligibility (BE)
-- when the passkey was made; a sign-in whose BE flag differs is refused
-- (WebAuthn Level 3 section 7.2). backed_up is its backup state (BS) at its
-- last use. added_with is the Sign-in or the Sign-in Link it was made
-- through. A removed Passkey's row is deleted; the log keeps it.
CREATE TABLE passkeys (
    id              TEXT PRIMARY KEY,
    user_id         TEXT NOT NULL REFERENCES users (id),
    credential_id   BLOB NOT NULL UNIQUE,
    public_key      BLOB NOT NULL,
    algorithm       INTEGER NOT NULL,
    rp_id           TEXT NOT NULL,
    name            TEXT NOT NULL,
    sign_count      INTEGER NOT NULL,
    backup_eligible INTEGER NOT NULL,
    backed_up       INTEGER NOT NULL,
    added_with      TEXT NOT NULL,
    added_at        TEXT NOT NULL,
    last_used_at    TEXT
) STRICT;
CREATE INDEX passkeys_by_user ON passkeys (user_id);

-- identities: each Linked Identity, known by (issuer, subject) alone (ADR
-- 0060). label is what the provider called the account when it was linked
-- (preferred_username, else email, else name), shown and never matched.
-- linked_with is the Sign-in or the Sign-in Link it was linked through. An
-- unlinked identity's row is deleted.
CREATE TABLE identities (
    id           TEXT PRIMARY KEY,
    user_id      TEXT NOT NULL UNIQUE REFERENCES users (id),
    issuer       TEXT NOT NULL,
    subject      TEXT NOT NULL,
    label        TEXT NOT NULL,
    linked_with  TEXT NOT NULL,
    linked_at    TEXT NOT NULL,
    last_used_at TEXT,
    UNIQUE (issuer, subject)
) STRICT;

-- identity_provider: the one OpenID Connect provider, or no row. secret is
-- its client secret sealed under the Root Key for the row's id (ADR 0018);
-- every change writes a new id. set_by is the Device Session or Sign-in that
-- set it, or NULL for the Unix socket.
CREATE TABLE identity_provider (
    one       INTEGER PRIMARY KEY CHECK (one = 1),
    id        TEXT NOT NULL UNIQUE,
    issuer    TEXT NOT NULL,
    client_id TEXT NOT NULL,
    name      TEXT NOT NULL,
    secret    BLOB NOT NULL,
    set_at    TEXT NOT NULL,
    set_by    TEXT
) STRICT;

-- How a Sign-in was made: 'password', 'passkey' or 'identity'; every earlier
-- Sign-in was made with a Password. credential is the passkeys or identities
-- row, NULL for a Password.
ALTER TABLE signins ADD COLUMN method TEXT NOT NULL DEFAULT 'password';
ALTER TABLE signins ADD COLUMN credential TEXT;
