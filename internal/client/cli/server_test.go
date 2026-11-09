package cli_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/cieons/koko/internal/client/cli"
	"github.com/cieons/koko/internal/harness"
)

// koko server run left the CLI for kokod run (ADR 0040): it is refused with
// exit 2, naming kokod run, with or without the flags it took, and help
// names it no more.
func TestServerRunIsKokodRunNow(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{
		{"server", "run"},
		{"server", "run", "--json", "--data-dir", "/srv/koko", "--listen", ":7822"},
		{"help", "server", "run", "--json"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			r := harness.Run(t, cli.Runner{}, nil, args...)
			if !slices.Contains(args, "--json") {
				if want := "koko: the koko server is kokod now: run kokod run, with the same flags\n"; r.Status != 2 || r.Stdout != "" || r.Stderr != want {
					t.Errorf("status %d, stdout %q, stderr %q; want 2 and %q", r.Status, r.Stdout, r.Stderr, want)
				}
				return
			}
			if msg := r.WantError(t, 2, "invalid_usage"); msg != "the koko server is kokod now: run kokod run, with the same flags" {
				t.Errorf("message %q", msg)
			}
		})
	}
	if slices.ContainsFunc(cli.Tree(), func(c cli.Command) bool { return c.Path() == "server run" }) {
		t.Error("the command tree still has koko server run")
	}
}

// koko help server lists the server commands koko keeps, and says that the
// koko server's process is kokod run.
func TestHelpServerNamesKokodRun(t *testing.T) {
	t.Parallel()
	r := harness.Run(t, cli.Runner{}, nil, "help", "server")
	r.WantSuccess(t)
	for _, want := range []string{"server install", "server uninstall", "server show", "server set", "\nThe koko server's process is kokod run"} {
		if !strings.Contains(r.Stdout, want) {
			t.Errorf("koko help server printed\n%s\nwhich does not say %q", r.Stdout, want)
		}
	}
	if strings.Contains(r.Stdout, "server run") {
		t.Errorf("koko help server lists server run:\n%s", r.Stdout)
	}
	if all := harness.Run(t, cli.Runner{}, nil, "help"); strings.Contains(all.Stdout, "kokod run") {
		t.Errorf("koko help says what only koko help server says:\n%s", all.Stdout)
	}
}

// contents reads every file under dir, by path relative to it.
func contents(t *testing.T, dir string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(path)
		rel, _ := filepath.Rel(dir, path)
		files[rel] = string(b)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}
