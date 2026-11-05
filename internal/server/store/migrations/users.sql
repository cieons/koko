-- Users: the people who belong to the koko server. Exactly one is the Owner,
-- and every other is a Member. A User's name never changes. created_at is
-- RFC 3339 in UTC to the second.
CREATE TABLE users (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL UNIQUE,
    role       TEXT NOT NULL CHECK (role IN ('owner', 'member')),
    created_at TEXT NOT NULL
) STRICT;

CREATE UNIQUE INDEX users_one_owner ON users (role) WHERE role = 'owner';

-- A Personal Space names the User it belongs to, which is not a membership;
-- a shared Space names none.
ALTER TABLE spaces ADD COLUMN user_id TEXT REFERENCES users (id);

CREATE UNIQUE INDEX spaces_one_personal ON spaces (user_id) WHERE user_id IS NOT NULL;
