-- The Web URL: the https:// origin the Owner's reverse proxy serves the web
-- UI at, as weburl.Parse stores it, which every request on the web listener
-- is checked against; NULL until the Owner sets it with koko server set
-- web_url=.
ALTER TABLE deployment ADD COLUMN web_url TEXT;
