// Package deployment is the koko server's own record, which initialisation
// makes once: the Server Address, and the koko server's own private keys,
// the koko key and the host key, each sealed under the Root Key (ADR 0018).
// The koko server is initialised once this record exists, and opens its keys
// at every start after that.
package deployment

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"database/sql"
	"errors"
	"fmt"

	"golang.org/x/crypto/ssh"

	"github.com/cieons/koko/internal/server/seal"
	"github.com/cieons/koko/internal/server/store"
	"github.com/cieons/koko/internal/shared/api"
	"github.com/cieons/koko/internal/shared/serveraddr"
)

// keyIDPrefix starts the id of each of the koko server's own keys, as in
// key_3f9k2m7q; the key is sealed for that id.
const keyIDPrefix = "key"

// The kinds of key the koko server makes for itself, as own_keys names
// them.
const (
	kindKoko = "koko"
	kindHost = "host"
)

// Keys are the koko server's own private keys, opened. Neither ever leaves
// the koko server unsealed.
type Keys struct {
	// Koko is the koko key, which logs in to every Managed Host.
	Koko ed25519.PrivateKey
	// Host is the koko server's SSH host key, which devices pin from their
	// Join Codes.
	Host ed25519.PrivateKey
}

// HostKeyFingerprint is the SHA-256 fingerprint of the host key, as OpenSSH
// prints it: "SHA256:" and 43 characters of base64.
func (k *Keys) HostKeyFingerprint() string {
	pub, err := ssh.NewPublicKey(k.Host.Public())
	if err != nil {
		panic(fmt.Sprintf("deployment: an ed25519 public key is an SSH key: %v", err))
	}
	return ssh.FingerprintSHA256(pub)
}

// Initialised tells whether the koko server is initialised.
func Initialised(ctx context.Context, tx store.Tx) (bool, error) {
	var ok bool
	if err := tx.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM deployment)").Scan(&ok); err != nil {
		return false, fmt.Errorf("read the koko server's own record: %w", err)
	}
	return ok, nil
}

// Create makes the koko server's record on a koko server not initialised
// yet: it makes the koko key and the host key (ed25519), seals each under
// rootKey for its own record, and stores them with the Server Address
// address, which it checks first.
func Create(ctx context.Context, tx store.Tx, rootKey *seal.RootKey, address string) (*Keys, error) {
	if err := serveraddr.Check(address); err != nil {
		return nil, api.CodeInvalidAddress.Errorf("invalid Server Address: %v", err)
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO deployment (id, address) VALUES (1, ?)", address); err != nil {
		return nil, fmt.Errorf("store the Server Address: %w", err)
	}
	keys := &Keys{Koko: newKey(), Host: newKey()}
	for kind, key := range keys.byKind() {
		der, err := x509.MarshalPKCS8PrivateKey(*key)
		if err != nil {
			return nil, fmt.Errorf("encode the %s key: %w", kind, err)
		}
		id := store.NewID(keyIDPrefix)
		_, err = tx.ExecContext(ctx, "INSERT INTO own_keys (id, kind, sealed) VALUES (?, ?, ?)", id, kind, rootKey.Seal(id, der))
		if err != nil {
			return nil, fmt.Errorf("store the %s key: %w", kind, err)
		}
	}
	return keys, nil
}

// SealedBy returns the id of the Root Key the koko server's sealed keys
// need, read without opening them, or "" before initialisation.
func SealedBy(ctx context.Context, tx store.Tx) (string, error) {
	var sealed []byte
	err := tx.QueryRowContext(ctx, "SELECT sealed FROM own_keys WHERE kind = ?", kindHost).Scan(&sealed)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return "", nil
	case err != nil:
		return "", fmt.Errorf("read the host key: %w", err)
	}
	id, err := seal.SealedBy(sealed)
	if err != nil {
		return "", fmt.Errorf("read the host key: %w", err)
	}
	return id, nil
}

// Open opens the koko server's keys with rootKey, or returns nil before
// initialisation. It fails, naming the key, when one is missing or does not
// open.
func Open(ctx context.Context, tx store.Tx, rootKey *seal.RootKey) (*Keys, error) {
	if ok, err := Initialised(ctx, tx); err != nil || !ok {
		return nil, err
	}
	keys := &Keys{}
	for kind, key := range keys.byKind() {
		var id string
		var sealed []byte
		err := tx.QueryRowContext(ctx, "SELECT id, sealed FROM own_keys WHERE kind = ?", kind).Scan(&id, &sealed)
		if err != nil {
			return nil, fmt.Errorf("read the %s key: %w", kind, err)
		}
		der, err := rootKey.Open(id, sealed)
		if err != nil {
			return nil, fmt.Errorf("open the %s key (%s): %w", kind, id, err)
		}
		parsed, err := x509.ParsePKCS8PrivateKey(der)
		if err != nil {
			return nil, fmt.Errorf("parse the %s key (%s): %w", kind, id, err)
		}
		priv, ok := parsed.(ed25519.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("parse the %s key (%s): it is a %T, not an ed25519 private key", kind, id, parsed)
		}
		*key = priv
	}
	return keys, nil
}

// byKind gives each of the keys under the kind own_keys names it by.
func (k *Keys) byKind() map[string]*ed25519.PrivateKey {
	return map[string]*ed25519.PrivateKey{kindKoko: &k.Koko, kindHost: &k.Host}
}

// newKey makes a new ed25519 private key.
func newKey() ed25519.PrivateKey {
	_, priv, _ := ed25519.GenerateKey(rand.Reader) // crypto/rand never fails
	return priv
}
