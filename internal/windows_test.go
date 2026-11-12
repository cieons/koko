package internal_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// goSources calls visit with each Go file under the module's root, by its
// slash-separated path from the root, and its text.
func goSources(t *testing.T, visit func(path, text string)) {
	t.Helper()
	root := ".."
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if name := d.Name(); name == "node_modules" || name == "testdata" || (strings.HasPrefix(name, ".") && path != root) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		visit(filepath.ToSlash(rel), string(b))
		return nil
	})
	if err != nil {
		t.Fatalf("read the module's Go files: %v", err)
	}
}

// The CLI reads the home directory in one place, cli.Invocation's home,
// through the one name devicefiles.HomeVar gives it on each OS: no other
// file of the CLI names HOME as a variable to read, since on Windows the
// home is USERPROFILE and HOME is never read for one (ADR 0051). The one
// other reader is koko status on Windows, which reads HOME to say that ssh
// may read another home than the profile directory koko writes under.
func TestOnlyTheHomeRuleReadsHOME(t *testing.T) {
	t.Parallel()
	const (
		rule          = "internal/client/devicefiles/private_unix.go"
		sshHomeReader = "internal/client/cli/status_windows.go"
	)
	found := false
	goSources(t, func(path, text string) {
		ofTheCLI := strings.HasPrefix(path, "internal/client/") || strings.HasPrefix(path, "cmd/koko/")
		if strings.HasSuffix(path, "_test.go") || !ofTheCLI {
			return
		}
		// Nor does one ask the OS, or read the variable past home.
		if strings.Contains(text, "UserHomeDir(") || path != "internal/client/cli/cli.go" && strings.Contains(text, "getenv(devicefiles.HomeVar)") {
			t.Errorf("%s finds the home directory by itself: read it through cli.Invocation's home", path)
		}
		if !strings.Contains(text, `"HOME"`) || path == sshHomeReader {
			return
		}
		if path == rule {
			found = true
			return
		}
		t.Errorf(`%s names "HOME": read the home directory through cli.Invocation's home, which reads devicefiles.HomeVar`, path)
	})
	if !found {
		t.Errorf(`%s no longer names "HOME": this test must follow the home rule to where it lives`, rule)
	}
}

// leftOutOnWindows is how many call sites of skip.OnWindows there are: the
// tests, or the helpers many tests share, that cannot hold on Windows, each
// saying why. Raising it is a decision to leave a test out there, so it is
// made here, in view.
const leftOutOnWindows = 10

// The tests left out on Windows are counted: each goes through
// skip.OnWindows with its reason, and a new one fails this test until the
// count is raised with it.
func TestTheTestsLeftOutOnWindowsAreCounted(t *testing.T) {
	t.Parallel()
	calls := 0
	goSources(t, func(path, text string) {
		// Wherever it stands on its line; its definition is not one, and
		// this file only speaks of it.
		n := strings.Count(text, "skip.OnWindows(t")
		if path == "internal/windows_test.go" {
			n = 0
		}
		if n > 0 {
			t.Logf("%s: %d", path, n)
		}
		calls += n
	})
	if calls != leftOutOnWindows {
		t.Errorf("skip.OnWindows is called at %d places, want %d: a test left out on Windows is counted in leftOutOnWindows", calls, leftOutOnWindows)
	}
}
