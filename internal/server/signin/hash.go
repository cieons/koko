package signin

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync"

	"golang.org/x/crypto/argon2"
)

// params are Argon2id's parameters: memory in KiB, passes and lanes.
type params struct {
	memory  uint32
	time    uint32
	threads uint8
}

// hashParams are the parameters every new hash takes: OWASP's minimum for
// Argon2id, 19 MiB of memory, 2 passes and 1 lane. A hash of other
// parameters is rehashed with these at the next sign-in.
var hashParams = params{memory: 19 * 1024, time: 2, threads: 1}

const (
	saltSize = 16
	keySize  = 32
	// maxHashing is how many hashes run at once, so that a burst of
	// sign-ins cannot exhaust a small machine's memory; a request waits for
	// its turn while its client waits.
	maxHashing = 2
)

// phcPrefix starts every hash koko writes, as the PHC string format names
// Argon2id at version 19 (0x13).
const phcPrefix = "$argon2id$v=19$"

var b64 = base64.RawStdEncoding

// hasher hashes Passwords and checks them against their hashes, at most
// maxHashing at once.
type hasher struct {
	slots chan struct{}
	// derive is argon2.IDKey; a test of how many run at once stands in its
	// own.
	derive func(password, salt []byte, time, memory uint32, threads uint8, keyLen uint32) []byte

	dummyOnce sync.Once
	dummy     string
}

func newHasher() *hasher {
	return &hasher{slots: make(chan struct{}, maxHashing), derive: argon2.IDKey}
}

// hash returns the PHC string of a new hash of password, with hashParams
// and a random salt.
func (h *hasher) hash(ctx context.Context, password string) (string, error) {
	salt := make([]byte, saltSize)
	_, _ = rand.Read(salt) // crypto/rand.Read never fails
	key, err := h.run(ctx, hashParams, password, salt)
	if err != nil {
		return "", err
	}
	return encodePHC(hashParams, salt, key), nil
}

// verify tells whether password matches phc, a PHC string, comparing in
// constant time, and whether phc was made with parameters other than
// hashParams, so the caller rehashes it.
func (h *hasher) verify(ctx context.Context, phc, password string) (match, stale bool, err error) {
	p, salt, want, err := parsePHC(phc)
	if err != nil {
		return false, false, err
	}
	got, err := h.run(ctx, p, password, salt)
	if err != nil {
		return false, false, err
	}
	return subtle.ConstantTimeCompare(got, want) == 1, p != hashParams, nil
}

// spend hashes password once against a dummy hash, for a sign-in that names
// no User, a Removed User or a User without a Password, so that it takes as
// long as one that checks a real hash.
func (h *hasher) spend(ctx context.Context, password string) error {
	h.dummyOnce.Do(func() {
		salt := make([]byte, saltSize)
		_, _ = rand.Read(salt)
		h.dummy = encodePHC(hashParams, salt, make([]byte, keySize))
	})
	_, _, err := h.verify(ctx, h.dummy, password)
	return err
}

// run derives the key of password with p and salt once a slot is free, or
// fails when ctx ends first.
func (h *hasher) run(ctx context.Context, p params, password string, salt []byte) ([]byte, error) {
	select {
	case h.slots <- struct{}{}:
	case <-ctx.Done():
		return nil, fmt.Errorf("wait to hash a Password: %w", ctx.Err())
	}
	defer func() { <-h.slots }()
	return h.derive([]byte(password), salt, p.time, p.memory, p.threads, keySize), nil
}

// encodePHC writes a hash as a PHC string, such as
// $argon2id$v=19$m=19456,t=2,p=1$<salt>$<key>, in unpadded base64.
func encodePHC(p params, salt, key []byte) string {
	return fmt.Sprintf("%sm=%d,t=%d,p=%d$%s$%s", phcPrefix, p.memory, p.time, p.threads, b64.EncodeToString(salt), b64.EncodeToString(key))
}

// errPHC is a stored hash that does not parse.
var errPHC = errors.New("the stored Password hash is not an Argon2id PHC string koko reads")

// parsePHC reads a PHC string encodePHC wrote, of any parameters.
func parsePHC(phc string) (params, []byte, []byte, error) {
	rest, ok := strings.CutPrefix(phc, phcPrefix)
	if !ok {
		return params{}, nil, nil, errPHC
	}
	fields := strings.Split(rest, "$")
	if len(fields) != 3 {
		return params{}, nil, nil, errPHC
	}
	var p params
	if _, err := fmt.Sscanf(fields[0], "m=%d,t=%d,p=%d", &p.memory, &p.time, &p.threads); err != nil || p.time == 0 || p.threads == 0 {
		return params{}, nil, nil, errPHC
	}
	salt, err := b64.DecodeString(fields[1])
	if err != nil {
		return params{}, nil, nil, errPHC
	}
	key, err := b64.DecodeString(fields[2])
	if err != nil || len(key) != keySize {
		return params{}, nil, nil, errPHC
	}
	return p, salt, key, nil
}
