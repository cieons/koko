//go:build unix

package cli_test

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cieons/koko/internal/harness"
)

// fakeOpener gives koko web a fake of this OS's opener: xdg-open and open
// on a PATH of their own, which the returned variables set, each writing
// the address it is given to a file. opened returns the addresses opened so
// far. koko web waits for the opener before it exits, so each run's opener
// has done its work once the run is stopped.
func fakeOpener(t *testing.T, _ *harness.Harness) (env []string, opened func() []string) {
	t.Helper()
	bin := t.TempDir()
	log := filepath.Join(t.TempDir(), "opened")
	for _, opener := range []string{"xdg-open", "open"} {
		script := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' \"$@\" >> %q\n", log)
		if err := os.WriteFile(filepath.Join(bin, opener), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return []string{"PATH=" + bin}, func() []string {
		t.Helper()
		got, err := os.ReadFile(log)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			t.Fatal(err)
		}
		return strings.Fields(string(got))
	}
}

// noOpener gives koko web a machine without an opener: a PATH that holds
// none, which the returned variables set.
func noOpener(t *testing.T, _ *harness.Harness) (env []string) {
	t.Helper()
	return []string{"PATH=" + t.TempDir()}
}
