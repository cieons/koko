package webauthn

import (
	"encoding/binary"
	"errors"
	"math"
	"unicode/utf8"
)

// The CBOR (RFC 8949) subset WebAuthn uses: definite-length maps, arrays,
// integers, and byte and text strings. Floats and simple values are read
// past. Indefinite lengths, tags, reserved encodings, map keys other than
// integers and text strings, duplicate map keys, containers nested past
// maxDepth, more than maxItems items and, for a whole value, trailing bytes
// are refused.

// The major types of a CBOR item's first byte.
const (
	majorUint   = 0
	majorNeg    = 1
	majorBytes  = 2
	majorText   = 3
	majorArray  = 4
	majorMap    = 5
	majorTag    = 6
	majorSimple = 7
)

// maxDepth is how deeply arrays and maps may nest: one outside any is at
// depth 1. It is go-webauthn's decoder's limit, so that koko takes no
// nesting the library refuses (ADR 0061); the items in a container at the
// deepest level may still be strings, integers and simple values.
const maxDepth = 5

// maxItems is how many items one decode may hold, the first included: what
// WebAuthn's structures hold is well under it, and it bounds what a few
// bytes can make the reader allocate.
const maxItems = 1024

// errCBOR is every refusal of the CBOR reader. It quotes nothing.
var errCBOR = errors.New("not the CBOR WebAuthn uses")

// An item is one decoded CBOR data item. Its slices point into the input.
type item struct {
	major byte
	// arg is an integer's argument: its value for majorUint, -1-arg for
	// majorNeg.
	arg uint64
	// str is a byte or text string's content.
	str []byte
	// items are an array's elements, or a map's keys and values in turn.
	items []item
	// raw is the item's whole encoding.
	raw []byte
}

// decodeCBOR decodes data as exactly one item, with nothing after it.
func decodeCBOR(data []byte) (item, error) {
	it, rest, err := decodeFirst(data)
	if err != nil {
		return item{}, err
	}
	if len(rest) != 0 {
		return item{}, errCBOR
	}
	return it, nil
}

// decodeFirst decodes the item data starts with, and returns what follows.
func decodeFirst(data []byte) (item, []byte, error) {
	var d decoder
	return d.item(data, 0)
}

// A decoder counts the items one decode has read.
type decoder struct{ items int }

// item decodes the item data starts with, inside depth containers.
func (d *decoder) item(data []byte, depth int) (item, []byte, error) {
	d.items++
	if d.items > maxItems || len(data) == 0 {
		return item{}, nil, errCBOR
	}
	it := item{major: data[0] >> 5}
	info := data[0] & 0x1f
	rest := data[1:]
	switch {
	case info < 24:
		it.arg = uint64(info)
	case info <= 27:
		n := 1 << (info - 24)
		if len(rest) < n {
			return item{}, nil, errCBOR
		}
		var buf [8]byte
		copy(buf[8-n:], rest[:n])
		it.arg = binary.BigEndian.Uint64(buf[:])
		rest = rest[n:]
	default:
		// 28 to 30 are reserved; 31 is an indefinite length or a break.
		return item{}, nil, errCBOR
	}
	switch it.major {
	case majorUint, majorNeg:
	case majorBytes, majorText:
		if it.arg > uint64(len(rest)) {
			return item{}, nil, errCBOR
		}
		it.str, rest = rest[:it.arg], rest[it.arg:]
		if it.major == majorText && !utf8.Valid(it.str) {
			return item{}, nil, errCBOR
		}
	case majorArray, majorMap:
		if depth+1 > maxDepth {
			return item{}, nil, errCBOR
		}
		// Each item takes a byte at least, so a count beyond what is left,
		// or than an item may hold, is refused before anything is read.
		n := it.arg
		if it.major == majorMap {
			if n > math.MaxUint64/2 {
				return item{}, nil, errCBOR
			}
			n *= 2
		}
		if n > uint64(len(rest)) || n > maxItems {
			return item{}, nil, errCBOR
		}
		var seen map[string]bool
		if it.major == majorMap {
			seen = make(map[string]bool, min(it.arg, 16))
		}
		for i := range n {
			var child item
			var err error
			child, rest, err = d.item(rest, depth+1)
			if err != nil {
				return item{}, nil, err
			}
			if seen != nil && i%2 == 0 {
				if child.major != majorUint && child.major != majorNeg && child.major != majorText {
					return item{}, nil, errCBOR
				}
				k := child.key()
				if seen[k] {
					return item{}, nil, errCBOR
				}
				seen[k] = true
			}
			it.items = append(it.items, child)
		}
	case majorTag:
		return item{}, nil, errCBOR
	case majorSimple:
		// A one-byte simple value below 32 is not well-formed (RFC 8949
		// 3.3); floats and the other simple values are read past.
		if info == 24 && it.arg < 32 {
			return item{}, nil, errCBOR
		}
	}
	it.raw = data[:len(data)-len(rest)]
	return it, rest, nil
}

// key is what makes a map key, an integer or a text string, the same as
// another: its value however long its encoding.
func (it item) key() string {
	if it.major == majorText {
		return string(append([]byte{it.major}, it.str...))
	}
	var buf [9]byte
	buf[0] = it.major
	binary.BigEndian.PutUint64(buf[1:], it.arg)
	return string(buf[:])
}

// int returns an integer item's value, and false for any other item or one
// outside int64.
func (it item) int() (int64, bool) {
	if (it.major != majorUint && it.major != majorNeg) || it.arg > math.MaxInt64 {
		return 0, false
	}
	if it.major == majorNeg {
		return -1 - int64(it.arg), true
	}
	return int64(it.arg), true
}

// value returns the value a map item holds under the key that match
// accepts, and false when it is no map or holds no such key.
func (it item) value(match func(item) bool) (item, bool) {
	if it.major != majorMap {
		return item{}, false
	}
	for i := 0; i < len(it.items); i += 2 {
		if match(it.items[i]) {
			return it.items[i+1], true
		}
	}
	return item{}, false
}

// textKey matches the text string name.
func textKey(name string) func(item) bool {
	return func(k item) bool { return k.major == majorText && string(k.str) == name }
}

// intKey matches the integer n.
func intKey(n int64) func(item) bool {
	return func(k item) bool {
		v, ok := k.int()
		return ok && v == n
	}
}
