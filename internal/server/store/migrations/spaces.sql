-- Spaces: a container of Hosts and Secrets. A name is unique among Spaces.
CREATE TABLE spaces (
    id   TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE
) STRICT;
