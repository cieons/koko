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

// Golden checks got against testdata/<name> in the test's package, after
// replacing each distinct record id with a numbered placeholder in order of
// first appearance ("space_<1>", "space_<2>"), since ids are random. With
// -update it rewrites the file instead. Name one file per command and case,
// such as space-add.json.
func Golden(t testing.TB, name, got string) {
	t.Helper()
	got = normalizeIDs(got)
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
