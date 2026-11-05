-- The koko server's own record, one row, which initialisation writes: the
-- koko server is initialised once it exists. address is the Server Address
-- as the Owner gave it, host or host:port.
CREATE TABLE deployment (
    id      INTEGER PRIMARY KEY CHECK (id = 1),
    address TEXT NOT NULL
) STRICT;

-- The private keys the koko server makes for itself, such as the koko key
-- and its host key: each one PKCS #8, sealed under the Root Key for its row's
-- id (ADR 0018).
CREATE TABLE own_keys (
    id     TEXT PRIMARY KEY,
    kind   TEXT NOT NULL UNIQUE,
    sealed BLOB NOT NULL
) STRICT;
