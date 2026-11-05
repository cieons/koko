-- When each Device Session was last used: the last time a Device Connection
-- it authenticated opened a channel, kept to the minute, as RFC 3339 in UTC
-- to the second. NULL until it first opens one.
ALTER TABLE device_sessions ADD COLUMN last_used_at TEXT;
