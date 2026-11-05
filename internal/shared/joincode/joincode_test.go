package joincode_test

import (
	"encoding/base32"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"strings"
	"testing"

	"github.com/cieons/koko/internal/shared/joincode"
)

// known is a Join Code whose string was worked out independently of this
// package (Python's base64.b32encode and zlib.crc32), so that the version-1
// format cannot drift unnoticed: its byte layout, the CRC-32 variant and its
// byte order, and the alphabet.
var known = struct {
	code   joincode.Code
	string string
}{
	code: joincode.Code{
		Address:     "koko.example.com:443",
		Fingerprint: [32]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31},
		Secret:      [16]byte{0xa0, 0xa1, 0xa2, 0xa3, 0xa4, 0xa5, 0xa6, 0xa7, 0xa8, 0xa9, 0xaa, 0xab, 0xac, 0xad, 0xae, 0xaf},
	},
	string: "koko_aekgw33ln4xgk6dbnvygyzjomnxw2orugqzqaaicamcakbqhbaequcymbuha6earcijrifiwc4mbsgq3dqor4h5augrkhjffu2t2rknkvowk3lvpjnol3ty",
}

func TestEncodeWritesTheVersion1Format(t *testing.T) {
	got, err := known.code.Encode()
	if err != nil || got != known.string {
		t.Errorf("Encode() = %q, %v\nwant %q", got, err, known.string)
	}
	decoded, err := joincode.Decode(known.string)
	if err != nil || decoded != known.code {
		t.Errorf("Decode(the known string) = %+v, %v, want the known code", decoded, err)
	}
}

func TestDecodeReadsWhatEncodeWrote(t *testing.T) {
	for _, address := range []string{
		"koko.example.com:443",
		"Koko.Example.COM:8443",
		"203.0.113.7:65535",
		"[2001:db8::1]:443",
		strings.Repeat(strings.Repeat("a", 62)+".", 3) + strings.Repeat("a", 60) + ":65535", // 255 bytes
	} {
		c := joincode.Code{Address: address}
		for i := range c.Fingerprint {
			c.Fingerprint[i] = byte(255 - i)
		}
		for i := range c.Secret {
			c.Secret[i] = byte(i * 7)
		}
		s, err := c.Encode()
		if err != nil {
			t.Errorf("Encode() with %q: %v", address, err)
			continue
		}
		if !strings.HasPrefix(s, "koko_") || strings.Trim(s[len("koko_"):], "abcdefghijklmnopqrstuvwxyz234567") != "" {
			t.Errorf("Encode() with %q = %q, want koko_ and lowercase base32 without padding", address, s)
		}
		got, err := joincode.Decode(s)
		if err != nil || got != c {
			t.Errorf("Decode(Encode()) with %q = %+v, %v", address, got, err)
		}
	}
}

// A Join Code always writes its port, and its address fits its one length
// byte.
func TestEncodeRefusesAnAddressAJoinCodeCannotCarry(t *testing.T) {
	for _, address := range []string{
		"",
		"koko.example.com",
		"[2001:db8::1]",
		"koko.example.com:0",
		"https://koko.example.com:443",
		strings.Repeat(strings.Repeat("a", 62)+".", 3) + strings.Repeat("a", 61) + ":65535", // 256 bytes
	} {
		if s, err := (joincode.Code{Address: address}).Encode(); err == nil {
			t.Errorf("Encode() with %q = %q, want an error", address, s)
		}
	}
}

// rawString encodes bytes as a Join Code string, with no checks, for codes
// that are well formed on the outside only.
func rawString(b []byte) string {
	return "koko_" + strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b))
}

// withChecksum appends the CRC-32 a Join Code ends in.
func withChecksum(b []byte) []byte {
	return binary.BigEndian.AppendUint32(b, crc32.ChecksumIEEE(b))
}

// body is the known code's bytes before its checksum, changed by edit.
func body(edit func([]byte) []byte) []byte {
	raw, _ := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(known.string[len("koko_"):]))
	return edit(raw[:len(raw)-4])
}

func TestDecodeSaysWhyItCannot(t *testing.T) {
	last := len(known.string) - 1
	for _, tc := range []struct {
		name string
		s    string
		want error
	}{
		{"empty", "", joincode.ErrPrefix},
		{"no prefix", known.string[len("koko_"):], joincode.ErrPrefix},
		{"uppercase prefix", "KOKO_" + known.string[len("koko_"):], joincode.ErrPrefix},
		{"another separator", "koko-" + known.string[len("koko_"):], joincode.ErrPrefix},
		{"prefix only", "koko_", joincode.ErrChecksum},
		{"uppercase after the prefix", known.string[:10] + strings.ToUpper(known.string[10:]), joincode.ErrAlphabet},
		{"a digit base32 lacks", known.string[:20] + "1" + known.string[21:], joincode.ErrAlphabet},
		{"padding", known.string + "====", joincode.ErrAlphabet},
		{"a space", known.string[:20] + " " + known.string[21:], joincode.ErrAlphabet},
		{"a newline after it", known.string + "\n", joincode.ErrAlphabet},
		{"one character changed", known.string[:40] + swap(known.string[40]) + known.string[41:], joincode.ErrChecksum},
		{"the last character changed", known.string[:last] + swap(known.string[last]), joincode.ErrChecksum},
		{"one character cut", known.string[:last], joincode.ErrChecksum},
		{"eight characters cut", known.string[:len(known.string)-8], joincode.ErrChecksum},
		{"cut to its prefix and a few characters", known.string[:12], joincode.ErrChecksum},
		{"a character added", known.string + "a", joincode.ErrChecksum},
		{"version 0", rawString(withChecksum(body(func(b []byte) []byte { b[0] = 0; return b }))), joincode.ErrVersion},
		{"version 2", rawString(withChecksum(body(func(b []byte) []byte { b[0] = 2; return b }))), joincode.ErrVersion},
		{"an address length past the end", rawString(withChecksum(body(func(b []byte) []byte { b[1] = 200; return b }))), joincode.ErrMalformed},
		{"an address length short of it", rawString(withChecksum(body(func(b []byte) []byte { b[1]--; return b }))), joincode.ErrMalformed},
		{"an address without a port", rawString(withChecksum(body(func(b []byte) []byte {
			addr := []byte("koko.example.com")
			return append(append([]byte{1, byte(len(addr))}, addr...), b[2+len("koko.example.com:443"):]...)
		}))), joincode.ErrMalformed},
		{"no bytes after the version", rawString(withChecksum([]byte{1})), joincode.ErrMalformed},
	} {
		got, err := joincode.Decode(tc.s)
		if !errors.Is(err, tc.want) {
			t.Errorf("%s: Decode() = %+v, %v; want %v", tc.name, got, err, tc.want)
			continue
		}
		if got != (joincode.Code{}) {
			t.Errorf("%s: Decode() returned %+v beside its error, want the zero Code", tc.name, got)
		}
		// A damaged code still holds most of its secret, so no error quotes it.
		if s := strings.TrimPrefix(tc.s, "koko_"); len(s) >= 8 && strings.Contains(err.Error(), s[:8]) {
			t.Errorf("%s: the error %q quotes the code", tc.name, err)
		}
	}
}

// swap returns a base32 character other than c.
func swap(c byte) string {
	if c == 'a' {
		return "b"
	}
	return "a"
}

// Printing a Code, as a log line or an error might, never shows its secret.
func TestPrintingACodeHidesItsSecret(t *testing.T) {
	s, _ := known.code.Encode()
	for _, format := range []string{"%v", "%+v", "%#v", "%s"} {
		got := fmt.Sprintf(format, known.code)
		if strings.Contains(got, s[len("koko_"):]) || strings.Contains(got, "160") || strings.Contains(got, "0xa0") ||
			strings.Contains(got, string(known.code.Secret[:])) {
			t.Errorf("%s prints %q, which shows the secret", format, got)
		}
		if !strings.Contains(got, known.code.Address) {
			t.Errorf("%s prints %q, want it to name the address", format, got)
		}
	}
}
