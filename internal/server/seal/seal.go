// Package seal is the Seal: it owns the Root Key and every value the koko
// server stores sealed (ADR 0018). Every module that stores a secret value
// calls it, and none touches AES-GCM itself.
//
// It hides the algorithm, the nonce, the sealed value's layout, the Root Key
// file's text and how the Root Key's id is derived; docs/subsystems/seal.md
// records them. It is server-only: no client package imports it.
package seal

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// keySize is the Root Key's length: AES-256.
const keySize = 32

// idPrefix starts every Root Key id, as in rootkey_3f9k2m7q.
const idPrefix = "rootkey_"

// idSize is how many bytes of the key's hash make its id: 40 bits, which
// tells a koko server's few Root Keys apart. The id is a name, not a check:
// AES-GCM still refuses a value sealed under another key that shares it.
const idSize = 5

// idLabel separates the hash that names a key from any other use of the key.
const idLabel = "koko Root Key id\x00"

// idEncoding writes an id in the alphabet of record ids (store.NewID):
// lowercase Crockford base32, so an id reads back without ambiguity. The
// Seal keeps its own copy rather than depend on the store.
var idEncoding = base32.NewEncoding("0123456789abcdefghjkmnpqrstvwxyz").WithPadding(base32.NoPadding)

// keyEncoding writes the key in its file: base64url without padding, 43
// characters, refusing a non-canonical encoding.
var keyEncoding = base64.RawURLEncoding.Strict()

// maxFileSize bounds what Load reads, far above the one line a Root Key file
// holds, so a path to a large file fails as text that does not parse.
const maxFileSize = 1024

// RootKey is a loaded Root Key: it seals and opens values, and reports its
// id. It never shows the key itself.
type RootKey struct {
	id string
	// header starts every value this key seals: the format version and the
	// bytes behind id.
	header []byte
	aead   cipher.AEAD
}

// NewRootKey makes a new random Root Key and returns its file's content: one
// line, <id>:<key>, ending in a newline. Install writes it; the koko server
// never does.
func NewRootKey() []byte {
	key := make([]byte, keySize)
	_, _ = rand.Read(key) // crypto/rand.Read never fails
	return []byte(encodeID(keyID(key)) + ":" + keyEncoding.EncodeToString(key) + "\n")
}

// Load reads the Root Key from its file at path, which must be absolute. It
// fails when the file is missing (the error matches fs.ErrNotExist), cannot
// be read, or does not parse. A file that does not parse is never taken for a
// missing one. No error holds the file's content.
func Load(path string) (*RootKey, error) {
	if err := CheckPath(path); err != nil {
		return nil, err
	}
	text, err := readFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil, &missingError{path: path}
	case err != nil:
		return nil, fmt.Errorf("cannot read the Root Key file %s: %w", path, err)
	}
	key, err := parse(text)
	if err != nil {
		return nil, fmt.Errorf("the Root Key file %s does not parse: %w", path, err)
	}
	return newRootKey(key), nil
}

// CheckPath refuses a Root Key file path that is not absolute, without
// naming it. Every Root Key file has an absolute path (systemd's credentials
// directory, /etc/koko), and no Root Key line starts with '/', so a path that
// passes is never the key pasted in its place, and an error may name it.
func CheckPath(path string) error {
	if !filepath.IsAbs(path) {
		return errors.New("the Root Key file's path is not absolute; give its absolute path (koko does not repeat what was given, in case it is the key)")
	}
	return nil
}

// missingError says the Root Key file does not exist. It matches
// fs.ErrNotExist, so a caller can tell a missing file from one that does not
// parse.
type missingError struct {
	path string
}

func (e *missingError) Error() string {
	return "the Root Key file " + e.path + " is missing"
}

func (e *missingError) Unwrap() error {
	return fs.ErrNotExist
}

// readFile reads at most maxFileSize+1 bytes of the file at path, giving the
// cause alone (such as fs.ErrNotExist) when it cannot.
func readFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", unwrapPath(err)
	}
	defer func() { _ = f.Close() }()
	b, err := io.ReadAll(io.LimitReader(f, maxFileSize+1))
	if err != nil {
		return "", unwrapPath(err)
	}
	return string(b), nil
}

// unwrapPath drops the operation and path a *fs.PathError adds, which Load's
// own message already names.
func unwrapPath(err error) error {
	if pe, ok := errors.AsType[*fs.PathError](err); ok {
		return pe.Err
	}
	return err
}

// parse reads a Root Key file's text, one line <id>:<key>, and returns the
// key. The id must be the key's, so a copy names itself and a damaged copy
// does not parse. Its errors never quote the text.
func parse(text string) ([]byte, error) {
	if len(text) > maxFileSize {
		return nil, errors.New("it is longer than the one line a Root Key file holds")
	}
	line := strings.TrimSpace(text)
	id, encoded, ok := strings.Cut(line, ":")
	if !ok || strings.ContainsAny(line, " \t\r\n") {
		return nil, errors.New("it is not one line " + idPrefix + "<id>:<key>")
	}
	key, err := keyEncoding.DecodeString(encoded)
	if err != nil || len(key) != keySize {
		return nil, fmt.Errorf("its key is not %d bytes of base64url", keySize)
	}
	if id != encodeID(keyID(key)) {
		return nil, errors.New("the id it names is not its key's; the copy is damaged")
	}
	return key, nil
}

// newRootKey makes the RootKey for a 32-byte key. AES-256 and GCM refuse
// only a key or block of another size, so an error here is a bug, and panics.
func newRootKey(key []byte) *RootKey {
	block, err := aes.NewCipher(key)
	if err != nil {
		panic(fmt.Sprintf("seal: make AES-256 from a %d-byte key: %v", len(key), err))
	}
	aead, err := cipher.NewGCMWithRandomNonce(block)
	if err != nil {
		panic(fmt.Sprintf("seal: make AES-256-GCM: %v", err))
	}
	raw := keyID(key)
	return &RootKey{id: encodeID(raw), header: append([]byte{version1}, raw...), aead: aead}
}

// keyID gives the bytes behind a key's id: the first idSize bytes of SHA-256
// over idLabel and the key.
func keyID(key []byte) []byte {
	sum := sha256.Sum256(append([]byte(idLabel), key...))
	return sum[:idSize]
}

// encodeID writes the bytes behind an id as the id, rootkey_<8 characters>.
func encodeID(b []byte) string {
	return idPrefix + idEncoding.EncodeToString(b)
}

// ID is the Root Key's id, which its file names and every value sealed under
// it carries.
func (k *RootKey) ID() string {
	return k.id
}

// String names the Root Key by its id, so that logging one shows no key.
func (k *RootKey) String() string {
	return "Root Key " + k.id
}

// The sealed form, version 1: the format version, the Root Key's id, then
// what AES-256-GCM with a random nonce gives: the nonce, the ciphertext and
// the tag. The additional data is the header (version and key id) followed by
// the record's id, so a sealed value opens only for its record, and a changed
// header byte fails like any other.
const (
	version1   = 1
	headerSize = 1 + idSize
)

// Seal seals value for the record whose immutable id is recordID, with a
// fresh random nonce. The sealed value opens only under this Root Key and for
// that record id. An empty recordID is a programming error, and panics: it
// would let one sealed value open as another's.
func (k *RootKey) Seal(recordID string, value []byte) []byte {
	if recordID == "" {
		panic("seal: a value is sealed for a record id, and none was given")
	}
	sealed := make([]byte, 0, headerSize+len(value)+k.aead.Overhead())
	sealed = append(sealed, k.header...)
	return k.aead.Seal(sealed, nil, value, additionalData(k.header, recordID))
}

// Open opens a value sealed for the record whose id is recordID. It fails
// closed, giving no value, when the format version is unknown, the value was
// sealed under another Root Key, it was sealed for another record, or any
// byte has changed. Its errors never quote a value; the caller names the
// record.
func (k *RootKey) Open(recordID string, sealed []byte) ([]byte, error) {
	id, err := SealedBy(sealed)
	if err != nil {
		return nil, err
	}
	if id != k.id {
		return nil, fmt.Errorf("it was sealed under %s, not %s, the Root Key loaded", id, k.id)
	}
	value, err := k.aead.Open(nil, nil, sealed[headerSize:], additionalData(k.header, recordID))
	if err != nil {
		return nil, fmt.Errorf("it does not open under %s: it was sealed for another record, or it has changed", k.id)
	}
	return value, nil
}

// SealedBy returns the id of the Root Key a sealed value needs, without
// opening it, so that a refusal can name the key it expects.
func SealedBy(sealed []byte) (string, error) {
	switch {
	case len(sealed) == 0:
		return "", errors.New("the sealed value is empty")
	case sealed[0] != version1:
		return "", fmt.Errorf("it is sealed in format version %d, which this koko does not know", sealed[0])
	case len(sealed) < headerSize:
		return "", errors.New("the sealed value is cut short")
	}
	return encodeID(sealed[1:headerSize]), nil
}

// additionalData binds a sealed value to its header and its record. The
// header's fixed size keeps the two apart.
func additionalData(header []byte, recordID string) []byte {
	return append(bytes.Clone(header), recordID...)
}
