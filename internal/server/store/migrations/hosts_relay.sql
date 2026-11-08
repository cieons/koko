-- A Host's Relay: the id of the Host the Gateway passes through to reach it,
-- NULL for none. It links by id, so renaming the Relay keeps it, and moving
-- either Host leaves it. A Relay is always a Host that was Managed, which is
-- never deleted, so the link never dangles. hosts_by_relay finds the Hosts
-- that use a Relay, for the rules that a Relay has no Relay itself and is not
-- Retired while a Host that is not Retired uses it.
ALTER TABLE hosts ADD COLUMN relay_id TEXT REFERENCES hosts (id);

CREATE INDEX hosts_by_relay ON hosts (relay_id) WHERE relay_id IS NOT NULL;
