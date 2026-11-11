-- The update_check setting: 1 while the koko server checks for a new koko
-- release by itself, a few minutes after it starts and every 6 hours after
-- that, 0 once the Owner turns it off with koko server set update_check=off
-- (ADR 0048). On by default, an upgraded koko server's too.
ALTER TABLE deployment ADD COLUMN update_check INTEGER NOT NULL DEFAULT 1 CHECK (update_check IN (0, 1));
