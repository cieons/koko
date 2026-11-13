-- workspace_tabs: each User's web terminal tabs, in order. A tab names its
-- Host by id; its label is the Host's name when it was opened, numbered
-- "web1 (2)" from the second tab to the same Host. opened_by is the
-- Sign-in's or Device Session's id that opened its current SSH Session,
-- or NULL for the Unix socket. ended is NULL while its SSH Session is
-- live, else how it ended, as the page shows it.
CREATE TABLE workspace_tabs (
    id         TEXT PRIMARY KEY,
    user_id    TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    position   INTEGER NOT NULL,
    host_id    TEXT NOT NULL,
    label      TEXT NOT NULL,
    opened_by  TEXT,
    opened_at  TEXT NOT NULL,
    ended      TEXT,
    UNIQUE (user_id, position)
) STRICT;
