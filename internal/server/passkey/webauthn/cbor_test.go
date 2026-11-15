package webauthn

import (
	"bytes"
	"encoding/hex"
	"runtime"
	"strings"
	"testing"
)

// cborCases are encodings, in hex, the reader takes or refuses.
var cborCases = []struct {
	name string
	hex  string
	ok   bool
}{
	{"0", "00", true},
	{"23", "17", true},
	{"24", "1818", true},
	{"an integer in eight bytes", "1bffffffffffffffff", true},
	{"an integer not in its shortest form", "1801", true},
	{"-1", "20", true},
	{"the least negative integer", "3bffffffffffffffff", true},
	{"a byte string", "43010203", true},
	{"an empty text string", "60", true},
	{"a text string", "63666d74", true},
	{"an array", "83010203", true},
	{"a map", "a201020304", true},
	{"a map keyed by text", "a163666d74646e6f6e65", true},
	{"false, true, null and undefined", "84f4f5f6f7", true},
	{"a simple value", "f820", true},
	{"an unassigned simple value", "f0", true},
	{"a half float", "f93c00", true},
	{"a float", "fa3f800000", true},
	{"a double", "fb3ff0000000000000", true},
	{"5 levels of arrays", strings.Repeat("81", 5) + "00", true},
	{"5 levels of maps", strings.Repeat("a100", 5) + "00", true},

	{"nothing", "", false},
	{"a trailing byte", "0000", false},
	{"an argument cut short", "19ff", false},
	{"a reserved argument (28)", "1c", false},
	{"a reserved argument (30)", "1e", false},
	{"an indefinite integer", "1f", false},
	{"an indefinite byte string", "5f4101ff", false},
	{"an indefinite text string", "7f6161ff", false},
	{"an indefinite array", "9f01ff", false},
	{"an indefinite map", "bf0102ff", false},
	{"a break", "ff", false},
	{"a tag", "c11a514b67b0", false},
	{"a tagged byte string", "d8184100", false},
	{"a byte string cut short", "4301", false},
	{"a byte string longer than the input", "5bffffffffffffffff00", false},
	{"invalid UTF-8", "62c328", false},
	{"an array cut short", "830102", false},
	{"an array longer than the input", "9bffffffffffffffff00", false},
	{"a map longer than the input", "bbffffffffffffffff00", false},
	{"a map whose count overflows when doubled", "bb800000000000000000", false},
	{"a map far longer than the input", "bb7fffffffffffffff00", false},
	{"a map cut short", "a201", false},
	{"a duplicate integer key", "a201000100", false},
	{"a duplicate integer key in a longer form", "a20100180100", false},
	{"a duplicate text key", "a2616100616100", false},
	{"a duplicate negative key", "a2200020 00", false},
	{"a simple value in two bytes below 32", "f818", false},
	{"6 levels of arrays", strings.Repeat("81", 6) + "00", false},
	{"6 levels, the last empty", strings.Repeat("81", 5) + "80", false},
	{"6 levels of maps", strings.Repeat("a100", 6) + "00", false},
	{"a byte string key", "a1410100", false},
	{"a null key", "a1f600", false},
	{"a float key", "a1f93c0000", false},
	{"an array key", "a1810000", false},
	{"a map key", "a1a000", false},
	{"a nested map with an array key", "a16161a1810000", false},
	{"1,024 items", "9903ff" + strings.Repeat("00", 1023), true},
	{"1,025 items", "9a00000400" + strings.Repeat("00", 1024), false},
	{"1,025 items in nested arrays", "82" + "9901ff" + strings.Repeat("00", 511) + "9902" + "00" + strings.Repeat("00", 512), false},
}

func TestTheCBORReader(t *testing.T) {
	t.Parallel()
	for _, tc := range cborCases {
		data, err := hex.DecodeString(strings.ReplaceAll(tc.hex, " ", ""))
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		it, err := decodeCBOR(data)
		if tc.ok != (err == nil) {
			t.Errorf("%s: got %v, want ok %v", tc.name, err, tc.ok)
			continue
		}
		if err == nil && !bytes.Equal(it.raw, data) {
			t.Errorf("%s: read %x of %x", tc.name, it.raw, data)
		}
	}
}

// hostile returns inputs of about 48 KiB, the most a passkey answer's 64 KiB
// JSON carries, that ask the reader for the most (#230's review): a flat
// array of one-byte items, a map of distinct integer keys, and map heads
// nested 5 deep, each declaring as many pairs as what follows could hold.
func hostile() [][]byte {
	const size = 48 << 10
	array := append([]byte{0x99, 0xbf, 0xfb}, make([]byte, 0xbffb)...)
	m := []byte{0xb9, 0x1f, 0xff}
	for k := range 0x1fff {
		m = append(m, 0x19, byte(k>>8), byte(k), 0x00)
	}
	var nested []byte
	for range 5 {
		nested = append(nested, 0xb9, 0x5f, 0xff, 0x00)
	}
	for k := 1; len(nested) < size; k++ {
		nested = append(nested, 0x19, byte(k>>8), byte(k), 0x00)
	}
	return [][]byte{array, m, nested}
}

// A count is bounded by what the input holds, and a decode's items by
// maxItems, before anything is made for them, so an input cannot make the
// reader allocate much. It counts the process's allocations, so it runs
// before the parallel tests.
func TestTheCBORReaderAllocatesForWhatTheInputHolds(t *testing.T) {
	// A map of 2^22 entries, and an array of as many, then nothing.
	for _, data := range append([][]byte{{0xba, 0, 0x40, 0, 0, 0}, {0x9a, 0, 0x40, 0, 0, 0}}, hostile()...) {
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		if _, err := decodeCBOR(data); err == nil {
			t.Fatalf("%x…: took it", data[:min(len(data), 8)])
		}
		runtime.ReadMemStats(&after)
		if n := after.TotalAlloc - before.TotalAlloc; n > 256<<10 {
			t.Fatalf("%x…: allocated %d bytes", data[:min(len(data), 8)], n)
		}
	}
}

func TestTheCBORReaderReadsValues(t *testing.T) {
	t.Parallel()
	data, _ := hex.DecodeString("a40102033903ff20430102036163") // {1: 2, 3: -1024, -1: h'010203', 'c' ...}
	data = append(data, 0x80)
	it, err := decodeCBOR(data)
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := intParam(it, 1); !ok || v != 2 {
		t.Fatalf("1: got %d, %v", v, ok)
	}
	if v, ok := intParam(it, 3); !ok || v != -1024 {
		t.Fatalf("3: got %d, %v", v, ok)
	}
	if v, ok := bytesParam(it, -1); !ok || !bytes.Equal(v, []byte{1, 2, 3}) {
		t.Fatalf("-1: got %x, %v", v, ok)
	}
	if v, ok := it.value(textKey("c")); !ok || v.major != majorArray {
		t.Fatalf("c: got %+v, %v", v, ok)
	}
	if _, ok := intParam(it, 2); ok {
		t.Fatal("2: found a key the map does not hold")
	}
	big, _ := decodeCBOR([]byte{0x3b, 0x80, 0, 0, 0, 0, 0, 0, 0})
	if _, ok := big.int(); ok {
		t.Fatal("an integer outside int64 read as one")
	}
}

// FuzzCBOR checks that the reader never panics, reads nothing past its
// input, and gives back only what the subset allows.
func FuzzCBOR(f *testing.F) {
	for _, tc := range cborCases {
		data, err := hex.DecodeString(strings.ReplaceAll(tc.hex, " ", ""))
		if err != nil {
			f.Fatal(err)
		}
		f.Add(data)
	}
	// A COSE_Key and an attestation object, as authenticators write them.
	for _, h := range []string{
		"a5010203262001215820afefa16f97ca9b2d23eb86ccb64098d20db90856062eb249c33a9b672f26df61225820930a56b87a2fca66334b03458abf879717c12cc68ed73290af2e2664796b9220",
		"a363666d74646e6f6e656761747453746d74a068617574684461746158250000000000000000000000000000000000000000000000000000000000000000050000000a",
	} {
		data, _ := hex.DecodeString(h)
		f.Add(data)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		// The reader is given a slice with room past its end, which it must
		// not read into.
		buf := append(bytes.Clone(data), 0xff, 0xff)
		in := buf[:len(data)]
		it, rest, err := decodeFirst(in)
		if err != nil {
			if _, err := decodeCBOR(in); err == nil {
				t.Fatal("decodeCBOR took what decodeFirst refused")
			}
			return
		}
		if len(it.raw)+len(rest) != len(in) || !bytes.Equal(it.raw, in[:len(it.raw)]) {
			t.Fatal("the item and what follows are not the input")
		}
		if _, err := decodeCBOR(in); (err == nil) != (len(rest) == 0) {
			t.Fatalf("decodeCBOR: %v, with %d bytes after the item", err, len(rest))
		}
		items := 0
		checkItem(t, in, it, 0, &items)
		if items > maxItems {
			t.Fatalf("%d items were taken", items)
		}
	})
}

// checkItem checks that it, inside depth containers, lies within in and
// keeps to the subset, and counts it and the items it holds into items.
func checkItem(t *testing.T, in []byte, it item, depth int, items *int) {
	t.Helper()
	*items++
	if (it.major == majorArray || it.major == majorMap) && depth+1 > maxDepth {
		t.Fatal("nested past the limit")
	}
	within := func(s []byte) bool {
		if len(s) == 0 {
			return true
		}
		start := cap(in) - cap(s)
		return start >= 0 && start+len(s) <= len(in)
	}
	if !within(it.raw) || !within(it.str) {
		t.Fatal("an item reaches past the input")
	}
	switch it.major {
	case majorTag:
		t.Fatal("a tag was taken")
	case majorBytes, majorText:
		if uint64(len(it.str)) != it.arg {
			t.Fatal("a string's length is not its argument")
		}
	case majorArray, majorMap:
		n := it.arg
		if it.major == majorMap {
			n *= 2
		}
		if uint64(len(it.items)) != n {
			t.Fatal("a container's count is not its argument")
		}
		keys := map[string]bool{}
		for i, child := range it.items {
			if it.major == majorMap && i%2 == 0 {
				if child.major != majorUint && child.major != majorNeg && child.major != majorText {
					t.Fatal("a key that is no integer or text was taken")
				}
				if keys[child.key()] {
					t.Fatal("a duplicate key was taken")
				}
				keys[child.key()] = true
			}
			checkItem(t, in, child, depth+1, items)
		}
	}
	if it.raw[0]&0x1f >= 28 {
		t.Fatal("a reserved or indefinite argument was taken")
	}
	again, err := decodeCBOR(it.raw)
	if err != nil {
		t.Fatalf("its own encoding does not read back: %v", err)
	}
	if !bytes.Equal(again.raw, it.raw) {
		t.Fatal("its own encoding reads back otherwise")
	}
}
