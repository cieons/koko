-- Services: what runs on a Host, each named uniquely on its Host. A Service
-- lives in its Host's Space and never moves to another Host, so it is keyed
-- by its Host's id and goes when its Host is deleted. note is the Note, NULL
-- for none. created_at and updated_at are RFC 3339 in UTC to the second.
CREATE TABLE services (
    id         TEXT PRIMARY KEY,
    host_id    TEXT NOT NULL REFERENCES hosts (id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    note       TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE (host_id, name)
) STRICT;

-- The ports a Service listens on: a set, which may be empty, of a port and
-- its protocol. Other Services' ports are not checked.
CREATE TABLE service_ports (
    service_id TEXT NOT NULL REFERENCES services (id) ON DELETE CASCADE,
    port       INTEGER NOT NULL CHECK (port BETWEEN 1 AND 65535),
    protocol   TEXT NOT NULL CHECK (protocol IN ('tcp', 'udp')),
    PRIMARY KEY (service_id, port, protocol)
) STRICT;

-- Service Attributes: at most one value per key on a Service, written as
-- text, as host_attributes are. They go with their Service.
CREATE TABLE service_attributes (
    service_id TEXT NOT NULL REFERENCES services (id) ON DELETE CASCADE,
    key        TEXT NOT NULL,
    value      TEXT NOT NULL,
    PRIMARY KEY (service_id, key)
) STRICT;
