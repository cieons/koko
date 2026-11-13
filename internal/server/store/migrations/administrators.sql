-- Administrators: the Members the Owner has made Administrators. users.role
-- stays 'owner' or 'member', the Owner being exactly one; an Administrator
-- is a 'member' with a row here. promoted_by is the Device Session's or the
-- Sign-in's id that promoted them, or NULL for the Unix socket.
CREATE TABLE administrators (
    user_id     TEXT PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    promoted_at TEXT NOT NULL,
    promoted_by TEXT
) STRICT;
