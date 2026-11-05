-- The response of each write, kept for its Idempotency-Key, so that a retry
-- of the write gets the response instead of applying the write again. A key
-- belongs to its caller: caller is a Device Session's id, or 'local' for
-- the Local caller. idempotency_key is the key as the write carried it.
-- sealed holds the request's digest and the response, sealed under the Root
-- Key for the caller and the key (ADR 0018), since some responses carry a
-- Join Code. A row is written in its write's own transaction, and forgotten
-- at expires_at, 10 minutes later, RFC 3339 in UTC to the second.
CREATE TABLE idempotency_keys (
    caller          TEXT NOT NULL,
    idempotency_key TEXT NOT NULL,
    expires_at      TEXT NOT NULL,
    sealed          BLOB NOT NULL,
    PRIMARY KEY (caller, idempotency_key)
) STRICT, WITHOUT ROWID;

CREATE INDEX idempotency_keys_by_expiry ON idempotency_keys (expires_at);
