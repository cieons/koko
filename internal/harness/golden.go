package harness

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

var update = flag.Bool("update", false, "rewrite golden files with what the tests see")

// idPattern matches a record id inside a JSON string, as in "space_3f9k2m7q".
var idPattern = regexp.MustCompile(`"([a-z]+)_[0-9a-hjkmnp-tv-z]{8}"`)

// codePattern matches a Join Code, which is made at run time and never lands
// in a fixture.
var codePattern = regexp.MustCompile(`koko_[a-z2-7]{32,}`)

// fingerprintPattern matches a SHA-256 fingerprint as OpenSSH prints one,
// of a key made at run time.
var fingerprintPattern = regexp.MustCompile(`SHA256:[A-Za-z0-9+/]{43}`)

// linkPattern matches the secret of a Password Link, made at run time.
var linkPattern = regexp.MustCompile(`/password#[A-Z2-7]{26}`)

// timePattern matches a time as koko prints one: RFC 3339 in UTC to the
// second.
var timePattern = regexp.MustCompile(`\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z`)

// Golden checks got against testdata/<name> in the test's package, after
// replacing each distinct record id with a numbered placeholder in order of
// first appearance ("space_<1>", "space_<2>"), since ids are random, each
// Join Code with "koko_<code>", each Password Link's secret with
// "/password#<secret>", each SHA-256 fingerprint with
// "SHA256:<fingerprint>" and each time with "<time>". With -update it
// rewrites the file instead. Name one file per command and case, such as
// space-add.json.
func Golden(t testing.TB, name, got string) {
	t.Helper()
	got = normalizeIDs(got)
	got = codePattern.ReplaceAllString(got, "koko_<code>")
	got = linkPattern.ReplaceAllString(got, "/password#<secret>")
	got = fingerprintPattern.ReplaceAllString(got, "SHA256:<fingerprint>")
	got = timePattern.ReplaceAllString(got, "<time>")
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden file (run go test -update to write it): %v", err)
	}
	if got != string(want) {
		t.Errorf("output differs from %s (run go test -update to accept it)\ngot:\n%s\nwant:\n%s", path, got, want)
	}
}

func normalizeIDs(s string) string {
	numbers := map[string]int{}
	return idPattern.ReplaceAllStringFunc(s, func(id string) string {
		n, ok := numbers[id]
		if !ok {
			n = len(numbers) + 1
			numbers[id] = n
		}
		prefix := idPattern.FindStringSubmatch(id)[1]
		return fmt.Sprintf(`"%s_<%d>"`, prefix, n)
	})
}
