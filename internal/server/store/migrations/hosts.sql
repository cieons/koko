-- Hosts: the machines koko records, each in one Space. name is the Host's
-- SSH Name, unique among the Hosts that are not Retired, whatever Space each
-- is in (ADR 0014); a Retired Host keeps the name it had but no longer holds
-- it. address is an IP address or a DNS name and port is 1 to 65535, neither
-- unique. account is the Host Account. state is set only by koko. host_key
-- is the pinned host key in SSH wire format, NULL until Enroll pins one.
-- note is the Note, NULL for none. created_at and updated_at are RFC 3339 in
-- UTC to the second.
CREATE TABLE hosts (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL,
    space_id   TEXT NOT NULL REFERENCES spaces (id),
    address    TEXT NOT NULL,
    port       INTEGER NOT NULL CHECK (port BETWEEN 1 AND 65535),
    account    TEXT NOT NULL,
    state      TEXT NOT NULL CHECK (state IN ('registered', 'managed', 'retired')),
    host_key   BLOB,
    note       TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
) STRICT;

CREATE UNIQUE INDEX hosts_live_name ON hosts (name) WHERE state <> 'retired';

CREATE INDEX hosts_by_space ON hosts (space_id);

-- Host Attributes: at most one value per key on a Host, written as text.
-- They go with their Host.
CREATE TABLE host_attributes (
    host_id TEXT NOT NULL REFERENCES hosts (id) ON DELETE CASCADE,
    key     TEXT NOT NULL,
    value   TEXT NOT NULL,
    PRIMARY KEY (host_id, key)
) STRICT;
