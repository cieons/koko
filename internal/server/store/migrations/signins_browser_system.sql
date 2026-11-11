-- signins.browser and signins.system: the browser and the system a Sign-in's
-- User-Agent named at sign-in, each one of the names the koko server
-- recognises, or '' for none. They replace signins.client, the English label
-- made of them, which the koko server now makes when it answers a Sign-in,
-- so the web UI can name the browser in the reader's language (#129).
--
-- A Sign-in recorded before keeps what its label said. The koko server made
-- every label from the same table of names: "<browser> on <system>",
-- "<browser>", or "Browser" for no browser it recognised, and no name holds
-- " on ", so the split is exact.
ALTER TABLE signins ADD COLUMN browser TEXT NOT NULL DEFAULT '';
ALTER TABLE signins ADD COLUMN system TEXT NOT NULL DEFAULT '';

UPDATE signins SET
    browser = CASE
        WHEN client = 'Browser' THEN ''
        WHEN instr(client, ' on ') > 0 THEN substr(client, 1, instr(client, ' on ') - 1)
        ELSE client
    END,
    system = CASE
        WHEN instr(client, ' on ') > 0 THEN substr(client, instr(client, ' on ') + 4)
        ELSE ''
    END;

ALTER TABLE signins DROP COLUMN client;
