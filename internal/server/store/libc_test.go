package store_test

import (
	"bytes"
	"encoding/json/v2"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// modernc.org/sqlite builds only against the exact modernc.org/libc its own
// go.mod names, so koko's go.mod must select that version: a libc bumped on
// its own has broken builds before. Upgrade the two together.
func TestLibcIsTheVersionSQLitePins(t *testing.T) {
	sqlite := goListModule(t, "modernc.org/sqlite")
	libc := goListModule(t, "modernc.org/libc")
	gomod, err := os.ReadFile(sqlite.GoMod)
	if err != nil {
		t.Fatalf("read modernc.org/sqlite's go.mod: %v", err)
	}
	pinned := ""
	for line := range strings.Lines(string(gomod)) {
		fields := strings.Fields(strings.TrimPrefix(strings.TrimSpace(line), "require "))
		if len(fields) >= 2 && fields[0] == "modernc.org/libc" {
			pinned = fields[1]
		}
	}
	if pinned == "" {
		t.Fatalf("modernc.org/sqlite %s's go.mod names no modernc.org/libc", sqlite.Version)
	}
	if libc.Version != pinned {
		t.Errorf("go.mod selects modernc.org/libc %s, but modernc.org/sqlite %s pins %s", libc.Version, sqlite.Version, pinned)
	}
}

type goModule struct {
	Version string
	GoMod   string
}

func goListModule(t *testing.T, path string) goModule {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "go", "list", "-m", "-json=Version,GoMod", path)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list -m %s: %v\n%s", path, err, stderr.String())
	}
	var m goModule
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatalf("read go list -m %s: %v", path, err)
	}
	return m
}
