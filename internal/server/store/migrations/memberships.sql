-- Memberships: a User who is a Member of a shared Space, with no role
-- (read-only membership is P2). A Personal Space has none while its User
-- exists: it belongs to that User, which is not a membership. A membership
-- goes with its Space when the Space is removed, and with its User when a
-- User who never joined is deleted.
CREATE TABLE memberships (
    space_id TEXT NOT NULL REFERENCES spaces (id) ON DELETE CASCADE,
    user_id  TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    PRIMARY KEY (space_id, user_id)
) STRICT;

CREATE INDEX memberships_by_user ON memberships (user_id);
