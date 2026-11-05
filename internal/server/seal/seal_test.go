package seal_test

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/cieons/koko/internal/server/seal"
)

// idPattern is the form of a Root Key id: koko's id form, with the kind
// rootkey (docs/subsystems/seal.md).
var idPattern = regexp.MustCompile(`^rootkey_[0-9a-hjkmnp-tv-z]{8}$`)

// writeKey writes text as a Root Key file in a temporary directory and
// returns its path.
func writeKey(t *testing.T, text []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "root.key")
	if err := os.WriteFile(path, text, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// newKey makes a Root Key file and loads it.
func newKey(t *testing.T) *seal.RootKey {
	t.Helper()
	k, err := seal.Load(writeKey(t, seal.NewRootKey()))
	if err != nil {
		t.Fatalf("load a new Root Key: %v", err)
	}
	return k
}

func TestANewRootKeyIsOneLineThatLoadsWithItsID(t *testing.T) {
	text := seal.NewRootKey()
	if bytes.Count(text, []byte("\n")) != 1 || !bytes.HasSuffix(text, []byte("\n")) {
		t.Fatalf("a new Root Key file is %d bytes and not one line ending in a newline", len(text))
	}
	path := writeKey(t, text)
	first, err := seal.Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !idPattern.MatchString(first.ID()) {
		t.Errorf("id %q is not of the form rootkey_<8 characters>", first.ID())
	}
	again, err := seal.Load(path)
	if err != nil {
		t.Fatalf("load again: %v", err)
	}
	if again.ID() != first.ID() {
		t.Errorf("the same file loads as %s, then as %s", first.ID(), again.ID())
	}
	if other := newKey(t); other.ID() == first.ID() {
		t.Errorf("two new Root Keys share the id %s", first.ID())
	}
}

// line is a new Root Key file's line without its newline, split into its
// id and its key.
func line(t *testing.T) (text, id, key string) {
	t.Helper()
	text = strings.TrimSuffix(string(seal.NewRootKey()), "\n")
	id, key, ok := strings.Cut(text, ":")
	if !ok {
		t.Fatalf("a new Root Key file has no ':' between its id and its key")
	}
	return text, id, key
}

func TestLoadReadsTheLineHoweverItEnds(t *testing.T) {
	text, id, _ := line(t)
	for name, content := range map[string]string{
		"no newline":   text,
		"CRLF":         text + "\r\n",
		"blank around": "  " + text + " \n\n",
	} {
		t.Run(name, func(t *testing.T) {
			k, err := seal.Load(writeKey(t, []byte(content)))
			if err != nil {
				t.Fatalf("load: %v", err)
			}
			if k.ID() != id {
				t.Errorf("loaded %s, want the id the line names, %s", k.ID(), id)
			}
		})
	}
}

func TestLoadSaysTheFileIsMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "root.key")
	_, err := seal.Load(path)
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("err = %v, want it to match fs.ErrNotExist", err)
	}
	if want := "the Root Key file " + path + " is missing"; !strings.Contains(err.Error(), want) {
		t.Errorf("err = %q, want it to say %q", err, want)
	}
	if _, statErr := os.Lstat(path); !errors.Is(statErr, fs.ErrNotExist) {
		t.Errorf("Load left something at the missing file's path: %v", statErr)
	}
}

func TestLoadSaysTheFileCannotBeRead(t *testing.T) {
	path := t.TempDir() // a directory reads as no file
	_, err := seal.Load(path)
	if err == nil || errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("err = %v, want a read failure that is not a missing file", err)
	}
	if want := "cannot read the Root Key file " + path; !strings.Contains(err.Error(), want) {
		t.Errorf("err = %q, want it to say %q", err, want)
	}
}

// A file that does not parse is refused as such, never as a missing file,
// and its error names the path but quotes nothing the file holds.
func TestLoadRefusesAFileThatDoesNotParse(t *testing.T) {
	text, id, key := line(t)
	_, otherID, _ := line(t)
	damaged := []byte(key) // one character changed, still base64url
	damaged[10] = 'A'
	if key[10] == 'A' {
		damaged[10] = 'B'
	}
	for name, content := range map[string]string{
		"empty":                     "",
		"the key alone":             key + "\n",
		"key and id swapped":        key + ":" + id + "\n",
		"another key's id":          otherID + ":" + key + "\n",
		"one key character changed": id + ":" + string(damaged) + "\n",
		"two lines":                 text + "\n" + text + "\n",
		"a space inside":            id + ": " + key + "\n",
		"a short key":               id + ":" + key[:22] + "\n",
		"padded key":                id + ":" + key + "=\n",
		"a large file":              text + "\n" + strings.Repeat("#", 2048),
	} {
		t.Run(name, func(t *testing.T) {
			path := writeKey(t, []byte(content))
			_, err := seal.Load(path)
			if err == nil {
				t.Fatal("it loaded")
			}
			if errors.Is(err, fs.ErrNotExist) {
				t.Errorf("err = %v matches fs.ErrNotExist: a file that does not parse is not a missing one", err)
			}
			if want := "the Root Key file " + path + " does not parse"; !strings.Contains(err.Error(), want) {
				t.Errorf("err = %q, want it to say %q", err, want)
			}
			if strings.Contains(err.Error(), key) || strings.Contains(err.Error(), key[:22]) || strings.Contains(err.Error(), string(damaged)) {
				t.Errorf("err = %q quotes the key", err)
			}
			if got, _ := os.ReadFile(path); string(got) != content {
				t.Error("Load changed the file")
			}
		})
	}
}

// A relative path is refused without being named: it may be the key, or
// part of it, pasted where its file's path belongs.
func TestLoadRefusesARelativePathWithoutNamingIt(t *testing.T) {
	text, id, key := line(t)
	for _, path := range []string{text, key, id + ":" + key[:20], "root.key", "./etc/root.key"} {
		_, err := seal.Load(path)
		if err == nil {
			t.Fatalf("Load(%q) loaded", path)
		}
		if strings.Contains(err.Error(), path) {
			t.Error("the error names the relative path it was given")
		}
		if !strings.Contains(err.Error(), "is not absolute") {
			t.Errorf("err = %q, want it to say the path is not absolute", err)
		}
	}
}

func TestASealedValueOpensForItsRecord(t *testing.T) {
	k := newKey(t)
	for name, value := range map[string][]byte{
		"empty":  {},
		"text":   []byte("db-password-for-this-test\n"),
		"binary": {0, 0xff, '\n', 0x80, 0},
		"64 KiB": bytes.Repeat([]byte{0xa5}, 64<<10),
	} {
		t.Run(name, func(t *testing.T) {
			sealed := k.Seal("secret_3f9k2m7q", value)
			if len(value) > 0 && bytes.Contains(sealed, value) {
				t.Error("the sealed value holds the value")
			}
			got, err := k.Open("secret_3f9k2m7q", sealed)
			if err != nil {
				t.Fatalf("open: %v", err)
			}
			if !bytes.Equal(got, value) {
				t.Errorf("opened %d bytes that differ from the %d sealed", len(got), len(value))
			}
		})
	}
}

// Each seal takes a fresh nonce, so sealing a value twice gives two sealed
// values, and both open.
func TestSealingTwiceGivesTwoSealedValues(t *testing.T) {
	k := newKey(t)
	a := k.Seal("secret_3f9k2m7q", []byte("same value"))
	b := k.Seal("secret_3f9k2m7q", []byte("same value"))
	if bytes.Equal(a, b) {
		t.Fatal("the same value sealed twice gave the same bytes")
	}
	for _, sealed := range [][]byte{a, b} {
		if _, err := k.Open("secret_3f9k2m7q", sealed); err != nil {
			t.Errorf("open: %v", err)
		}
	}
}

// Opening fails closed: another record id, another Root Key, an unknown
// format version, any changed byte and a cut-short value each give an error
// and no value.
func TestOpenFailsClosed(t *testing.T) {
	k, other := newKey(t), newKey(t)
	sealed := k.Seal("secret_3f9k2m7q", []byte("value-for-this-test"))
	check := func(t *testing.T, k *seal.RootKey, recordID string, sealed []byte, says string) {
		t.Helper()
		got, err := k.Open(recordID, sealed)
		if err == nil || got != nil {
			t.Fatalf("Open gave %q and error %v, want no value and an error", got, err)
		}
		if !strings.Contains(err.Error(), says) {
			t.Errorf("err = %q, want it to say %q", err, says)
		}
		if strings.Contains(err.Error(), "value-for-this-test") {
			t.Errorf("err = %q quotes the value", err)
		}
	}
	t.Run("another record id", func(t *testing.T) {
		check(t, k, "secret_7k2m9q4x", sealed, "does not open")
	})
	t.Run("another Root Key", func(t *testing.T) {
		check(t, other, "secret_3f9k2m7q", sealed, "sealed under "+k.ID()+", not "+other.ID())
	})
	t.Run("unknown format version", func(t *testing.T) {
		changed := bytes.Clone(sealed)
		changed[0] = 2 // the format version is the first byte (docs/subsystems/seal.md)
		check(t, k, "secret_3f9k2m7q", changed, "format version 2")
	})
	t.Run("every byte changed in turn", func(t *testing.T) {
		for i := range sealed {
			changed := bytes.Clone(sealed)
			changed[i] ^= 0x01
			if got, err := k.Open("secret_3f9k2m7q", changed); err == nil || got != nil {
				t.Fatalf("with byte %d changed, Open gave %q and error %v", i, got, err)
			}
		}
	})
	t.Run("cut short", func(t *testing.T) {
		for n := range len(sealed) {
			if got, err := k.Open("secret_3f9k2m7q", sealed[:n]); err == nil || got != nil {
				t.Fatalf("with %d of %d bytes, Open gave %q and error %v", n, len(sealed), got, err)
			}
		}
	})
}

// A refusal can name the Root Key a sealed value needs without opening it,
// under any loaded key or none.
func TestSealedByNamesTheRootKeyWithoutOpening(t *testing.T) {
	k := newKey(t)
	sealed := k.Seal("secret_3f9k2m7q", []byte("value-for-this-test"))
	id, err := seal.SealedBy(sealed)
	if err != nil {
		t.Fatalf("SealedBy: %v", err)
	}
	if id != k.ID() {
		t.Errorf("SealedBy = %s, want %s", id, k.ID())
	}
	changed := bytes.Clone(sealed)
	changed[0] = 2
	for name, b := range map[string][]byte{"empty": nil, "one byte": sealed[:1], "unknown format version": changed} {
		if id, err := seal.SealedBy(b); err == nil {
			t.Errorf("%s: SealedBy = %s, want an error", name, id)
		}
	}
}

// Sealing for no record would let one sealed value open as any other's.
func TestSealRefusesAnEmptyRecordID(t *testing.T) {
	k := newKey(t)
	defer func() {
		if recover() == nil {
			t.Error("Seal sealed a value for an empty record id")
		}
	}()
	k.Seal("", []byte("value"))
}

// Logging a Root Key shows its id, never the key.
func TestARootKeyPrintsOnlyItsID(t *testing.T) {
	text, id, key := line(t)
	k, err := seal.Load(writeKey(t, []byte(text)))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := base64.RawURLEncoding.DecodeString(key)
	if err != nil {
		t.Fatal(err)
	}
	var log bytes.Buffer
	slog.New(slog.NewTextHandler(&log, nil)).Info("loaded", "root_key", k)
	slog.New(slog.NewJSONHandler(&log, nil)).Info("loaded", "root_key", k)
	printed := fmt.Sprintf("%v %+v %#v %s %q %+v %#v", k, k, k, k, k, *k, *k) + log.String()
	if !strings.Contains(printed, id) {
		t.Errorf("printing the Root Key does not show its id %s", id)
	}
	for _, leak := range []string{key, string(raw), fmt.Sprintf("%x", raw), fmt.Sprintf("%d", raw)} {
		if strings.Contains(printed, leak) {
			t.Fatal("printing the Root Key shows the key") // and this test does not print it either
		}
	}
}
