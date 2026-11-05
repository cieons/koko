package deployment

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"

	"github.com/cieons/koko/internal/server/seal"
	"github.com/cieons/koko/internal/server/store"
)

// kindCookie is own_keys' kind for the key the web front signs cookies with.
const kindCookie = "cookie"

// CookieKeySize is the length of the cookie key, an HMAC-SHA256 key.
const CookieKeySize = 32

// CookieKey returns the key the web front signs koko web's cookies with,
// making it on first use: CookieKeySize random bytes, sealed under rootKey
// for its own row of own_keys, as every key the koko server makes for itself
// is (ADR 0018). Call it in a write transaction, which the first call writes
// the key in.
func CookieKey(ctx context.Context, tx store.Tx, rootKey *seal.RootKey) ([]byte, error) {
	var id string
	var sealed []byte
	err := tx.QueryRowContext(ctx, "SELECT id, sealed FROM own_keys WHERE kind = ?", kindCookie).Scan(&id, &sealed)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		key := make([]byte, CookieKeySize)
		_, _ = rand.Read(key) // crypto/rand never fails
		id := store.NewID(keyIDPrefix)
		_, err := tx.ExecContext(ctx, "INSERT INTO own_keys (id, kind, sealed) VALUES (?, ?, ?)", id, kindCookie, rootKey.Seal(id, key))
		if err != nil {
			return nil, fmt.Errorf("store the cookie key: %w", err)
		}
		return key, nil
	case err != nil:
		return nil, fmt.Errorf("read the cookie key: %w", err)
	}
	key, err := rootKey.Open(id, sealed)
	if err != nil {
		return nil, fmt.Errorf("open the cookie key (%s): %w", id, err)
	}
	if len(key) != CookieKeySize {
		return nil, fmt.Errorf("open the cookie key (%s): it holds %d bytes, not %d", id, len(key), CookieKeySize)
	}
	return key, nil
}
