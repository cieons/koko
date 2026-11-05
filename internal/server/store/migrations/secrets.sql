-- Secrets: a named value held in one Space (P0 spec: Secrets). A name is
-- unique within its Space only, so a name never tells what a Space the
-- caller cannot see holds. sealed is the value sealed under the Root Key for
-- the row's id (ADR 0018), which a rename or a move never changes, so
-- neither reseals; the value is never stored plain. A Space that holds a
-- Secret cannot be removed. created_at and updated_at are RFC 3339 in UTC to
-- the second.
CREATE TABLE secrets (
    id         TEXT PRIMARY KEY,
    space_id   TEXT NOT NULL REFERENCES spaces (id),
    name       TEXT NOT NULL,
    sealed     BLOB NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE (space_id, name)
) STRICT;
