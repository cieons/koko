package store

import "crypto/rand"

// idAlphabet is Crockford's base32 in lowercase: no i, l, o or u, so an id
// reads back without ambiguity.
const idAlphabet = "0123456789abcdefghjkmnpqrstvwxyz"

// NewID returns a new record id: the record's prefix, '_' and 8 random
// characters (40 bits), as in host_7k2m9q4x. No name holds '_', so an id is
// never mistaken for a name. A table's primary key refuses the rare repeat.
func NewID(prefix string) string {
	var b [5]byte
	_, _ = rand.Read(b[:]) // crypto/rand.Read never fails
	n := uint64(b[0])<<32 | uint64(b[1])<<24 | uint64(b[2])<<16 | uint64(b[3])<<8 | uint64(b[4])
	id := make([]byte, 0, len(prefix)+9)
	id = append(id, prefix...)
	id = append(id, '_')
	for i := 7; i >= 0; i-- {
		id = append(id, idAlphabet[n>>(5*uint(i))&31])
	}
	return string(id)
}
