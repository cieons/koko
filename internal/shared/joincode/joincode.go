// Package joincode is the Join Code codec: pure, with no I/O. The koko
// server encodes the codes it issues with it, and `koko join` decodes the
// code it is given (P0 spec: people and Spaces).
//
// A Join Code is "koko_" followed by these bytes in lowercase RFC 4648
// base32 (a-z, 2-7) without padding:
//
//	1 byte    the format version, 1
//	1 byte    n, the length of the address
//	n bytes   the Server Address as host:port in UTF-8, its port always written
//	32 bytes  the SHA-256 fingerprint of the koko server's host key
//	16 bytes  the secret, random
//	4 bytes   CRC-32 (IEEE) of every byte before it, big-endian
//
// Every format version ends in that same checksum, so a damaged code is told
// from one of a newer format: Decode checks the checksum before the version.
package joincode

import (
	"crypto/sha256"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"strings"

	"github.com/cieons/koko/internal/shared/serveraddr"
)

// Prefix starts every Join Code, so that people and secret scanners
// recognise one.
const Prefix = "koko_"

// Version is the format version this koko writes and reads.
const Version = 1

// SecretSize is the length of a Join Code's secret: 128 bits.
const SecretSize = 16

// MaxAddressLen is the longest address a Join Code carries, in bytes: its
// length fits one byte.
const MaxAddressLen = 255

// fixedLen is the length of every part but the address.
const fixedLen = 1 + 1 + sha256.Size + SecretSize + crc32.Size

// encoding is lowercase base32 without padding; Decode refuses uppercase.
var encoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// Why Decode cannot read a string as a Join Code. No error quotes the string,
// since a damaged code still holds most of its secret.
var (
	ErrPrefix    = errors.New("a Join Code starts with " + Prefix)
	ErrAlphabet  = errors.New("a Join Code holds only " + Prefix + " and the characters a-z and 2-7")
	ErrChecksum  = errors.New("the Join Code is cut short or damaged: copy all of it again")
	ErrVersion   = errors.New("the Join Code is of a format this koko does not read: upgrade koko")
	ErrMalformed = errors.New("the Join Code's checksum holds, but it does not hold a Join Code's parts")
)

// Code is a Join Code's content.
type Code struct {
	// Address is the koko server's Server Address as host:port, its port
	// always written (serveraddr.CheckHostPort).
	Address string
	// Fingerprint is the SHA-256 of the koko server's host key in SSH wire
	// format, which the device checks before it sends anything.
	Fingerprint [sha256.Size]byte
	// Secret is what the device presents to join; the koko server keeps only
	// its hash.
	Secret [SecretSize]byte
}

// Encode writes the code as its koko_ string. It refuses an address a Join
// Code cannot carry: one that is not host:port, or longer than MaxAddressLen.
func (c Code) Encode() (string, error) {
	if err := CheckAddress(c.Address); err != nil {
		return "", err
	}
	b := make([]byte, 0, fixedLen+len(c.Address))
	b = append(b, Version, byte(len(c.Address)))
	b = append(b, c.Address...)
	b = append(b, c.Fingerprint[:]...)
	b = append(b, c.Secret[:]...)
	b = binary.BigEndian.AppendUint32(b, crc32.ChecksumIEEE(b))
	return Prefix + strings.ToLower(encoding.EncodeToString(b)), nil
}

// Decode reads a koko_ string, or says why it cannot with one of the Err
// values: the prefix, the alphabet, the checksum (a code cut short or
// damaged), the version, or parts that do not fit a Join Code.
func Decode(s string) (Code, error) {
	text, ok := strings.CutPrefix(s, Prefix)
	if !ok {
		return Code{}, ErrPrefix
	}
	if strings.Trim(text, "abcdefghijklmnopqrstuvwxyz234567") != "" {
		return Code{}, ErrAlphabet
	}
	b, err := encoding.DecodeString(strings.ToUpper(text))
	// Every character is in the alphabet, so only a length base32 cannot
	// hold fails here: a code cut short.
	if err != nil || len(b) < 1+crc32.Size {
		return Code{}, ErrChecksum
	}
	content, sum := b[:len(b)-crc32.Size], b[len(b)-crc32.Size:]
	if crc32.ChecksumIEEE(content) != binary.BigEndian.Uint32(sum) {
		return Code{}, ErrChecksum
	}
	if content[0] != Version {
		return Code{}, fmt.Errorf("format version %d: %w", content[0], ErrVersion)
	}
	if len(b) < fixedLen || len(b) != fixedLen+int(content[1]) {
		return Code{}, ErrMalformed
	}
	rest := content[2:]
	var c Code
	c.Address, rest = string(rest[:content[1]]), rest[content[1]:]
	rest = rest[copy(c.Fingerprint[:], rest):]
	copy(c.Secret[:], rest)
	if CheckAddress(c.Address) != nil {
		return Code{}, ErrMalformed
	}
	return c, nil
}

// CheckAddress refuses an address a Join Code cannot carry: one that is not
// host:port, or longer than MaxAddressLen.
func CheckAddress(address string) error {
	if len(address) > MaxAddressLen {
		return fmt.Errorf("the address %q is %d bytes; a Join Code carries at most %d", address, len(address), MaxAddressLen)
	}
	if err := serveraddr.CheckHostPort(address); err != nil {
		return fmt.Errorf("a Join Code carries host:port: %w", err)
	}
	return nil
}

// String names the code by its address, so that printing one never shows
// its secret.
func (c Code) String() string {
	return "Join Code for " + c.Address
}

// GoString is String, for %#v.
func (c Code) GoString() string {
	return c.String()
}
