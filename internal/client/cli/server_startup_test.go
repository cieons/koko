package cli_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/cieons/koko/internal/client/cli"
	"github.com/cieons/koko/internal/harness"
)

// serverRun runs `koko server run` with the koko server's real start-up on
// dataDir and the Root Key file keyFile, as the unit would run it.
func serverRun(t *testing.T, dataDir, keyFile string) harness.Result {
	t.Helper()
	return harness.Run(t, cli.Runner{ServerRun: harness.ServerStart(t)}, nil,
		"server", "run", "--data-dir", dataDir, "--root-key-file", keyFile)
}

// newKeyFile writes a new Root Key file at path, making its directory, and
// returns the id the file names and the key it holds, which no output may
// show.
func newKeyFile(t *testing.T, path string) (id, key string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	text, err := os.ReadFile(harness.NewRootKeyFile(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, text, 0o600); err != nil {
		t.Fatal(err)
	}
	id, key, _ = strings.Cut(strings.TrimSpace(string(text)), ":")
	return id, key
}

// refused checks that `koko server run` refused to start, saying says on
// stderr, and that nothing it printed holds key.
func refused(t *testing.T, r harness.Result, key, says string) {
	t.Helper()
	if r.Status != 1 || r.Stdout != "" {
		t.Errorf("exit status %d, stdout %q, want 1 and nothing", r.Status, r.Stdout)
	}
	if !strings.Contains(r.Stderr, says) {
		t.Errorf("stderr %q does not say %q", r.Stderr, says)
	}
	if key != "" && strings.Contains(r.Stdout+r.Stderr, key) {
		t.Error("koko server run printed the Root Key") // and this test does not print it either
	}
}

// files lists every path under dir, relative to it.
func files(t *testing.T, dir string) []string {
	t.Helper()
	var paths []string
	err := filepath.WalkDir(dir, func(path string, _ fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		paths = append(paths, rel)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return paths
}

// The koko server starts with its Root Key, and its log names the id the
// key's file names, never the key.
func TestServerRunStartsWithItsRootKeyFile(t *testing.T) {
	keyFile := filepath.Join(t.TempDir(), "root.key")
	id, key := newKeyFile(t, keyFile)
	r := serverRun(t, t.TempDir(), keyFile)
	if r.Status != 0 || r.Stdout != "" {
		t.Fatalf("exit status %d, stdout %q, stderr %q", r.Status, r.Stdout, r.Stderr)
	}
	if !strings.Contains(r.Stderr, "msg=\"koko server started\"") || !strings.Contains(r.Stderr, "root_key="+id) {
		t.Errorf("the start-up log does not name the Root Key %s:\n%s", id, strings.ReplaceAll(r.Stderr, key, "<key>"))
	}
	if strings.Contains(r.Stderr, key) {
		t.Error("the log holds the Root Key")
	}
}

// The Root Key's file stays outside the data directory, so that a copy of the
// data directory never holds it (P0 spec: install and run, story 45).
func TestServerRunRefusesARootKeyFileInsideTheDataDirectory(t *testing.T) {
	for _, tc := range []struct {
		name string
		// place makes what the case needs under root and returns the data
		// directory and the Root Key file's path to give.
		place func(t *testing.T, root string) (dataDir, keyFile string)
	}{
		{"in the data directory", func(t *testing.T, root string) (string, string) {
			return filepath.Join(root, "data"), filepath.Join(root, "data", "root.key")
		}},
		{"below it", func(t *testing.T, root string) (string, string) {
			return filepath.Join(root, "data"), filepath.Join(root, "data", "keys", "root.key")
		}},
		{"through ..", func(t *testing.T, root string) (string, string) {
			if err := os.MkdirAll(filepath.Join(root, "etc"), 0o700); err != nil {
				t.Fatal(err)
			}
			return filepath.Join(root, "data"), filepath.Join(root, "etc", "..", "data", "root.key")
		}},
		{"a link out of it that leads in", func(t *testing.T, root string) (string, string) {
			newKeyFile(t, filepath.Join(root, "data", "root.key"))
			if err := os.MkdirAll(filepath.Join(root, "etc"), 0o700); err != nil {
				t.Fatal(err)
			}
			link := filepath.Join(root, "etc", "root.key")
			if err := os.Symlink(filepath.Join(root, "data", "root.key"), link); err != nil {
				t.Fatal(err)
			}
			return filepath.Join(root, "data"), link
		}},
		{"a data directory reached through a link", func(t *testing.T, root string) (string, string) {
			if err := os.MkdirAll(filepath.Join(root, "real"), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(filepath.Join(root, "real"), filepath.Join(root, "data")); err != nil {
				t.Fatal(err)
			}
			return filepath.Join(root, "data"), filepath.Join(root, "real", "root.key")
		}},
		{"the data directory itself", func(t *testing.T, root string) (string, string) {
			if err := os.MkdirAll(filepath.Join(root, "data"), 0o700); err != nil {
				t.Fatal(err)
			}
			return filepath.Join(root, "data"), filepath.Join(root, "data")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			dataDir, keyFile := tc.place(t, root)
			key := ""
			if _, err := os.Stat(keyFile); errors.Is(err, fs.ErrNotExist) {
				_, key = newKeyFile(t, keyFile)
			}
			before := files(t, root)
			r := serverRun(t, dataDir, keyFile)
			refused(t, r, key, "the Root Key file "+keyFile+" is inside the data directory "+dataDir)
			if after := files(t, root); !slices.Equal(after, before) {
				t.Errorf("the refusal changed the files: before %v, after %v", before, after)
			}
		})
	}
}

// A Root Key file beside the data directory, named like it, is outside it.
func TestServerRunTakesARootKeyFileBesideTheDataDirectory(t *testing.T) {
	root := t.TempDir()
	keyFile := filepath.Join(root, "data-root.key")
	newKeyFile(t, keyFile)
	if r := serverRun(t, filepath.Join(root, "data"), keyFile); r.Status != 0 {
		t.Fatalf("exit status %d, stderr %q", r.Status, r.Stderr)
	}
}

// A missing Root Key file stops the start-up, and nothing writes a key in its
// place.
func TestServerRunRefusesAMissingRootKeyFile(t *testing.T) {
	keyDir := t.TempDir()
	keyFile := filepath.Join(keyDir, "root.key")
	r := serverRun(t, t.TempDir(), keyFile)
	refused(t, r, "", "the Root Key file "+keyFile+" is missing")
	if got := files(t, keyDir); !slices.Equal(got, []string{"."}) {
		t.Errorf("the refusal wrote %v where the Root Key file belongs", got)
	}
}

// A Root Key file that does not parse stops the start-up as such: it is never
// taken for a missing file, and nothing writes a key over it or beside it.
func TestServerRunRefusesARootKeyFileThatDoesNotParse(t *testing.T) {
	keyDir := t.TempDir()
	keyFile := filepath.Join(keyDir, "root.key")
	_, key := newKeyFile(t, keyFile)
	content := []byte(key + "\n") // the key without the id its file names
	if err := os.WriteFile(keyFile, content, 0o600); err != nil {
		t.Fatal(err)
	}
	dataDir := t.TempDir()
	r := serverRun(t, dataDir, keyFile)
	refused(t, r, key, "the Root Key file "+keyFile+" does not parse")
	if strings.Contains(r.Stderr, "missing") {
		t.Errorf("stderr %q calls a file that does not parse missing", r.Stderr)
	}
	if got, _ := os.ReadFile(keyFile); string(got) != string(content) {
		t.Error("the refusal changed the Root Key file")
	}
	if got := files(t, keyDir); !slices.Equal(got, []string{".", "root.key"}) {
		t.Errorf("the refusal wrote %v beside the Root Key file", got)
	}
	// The store opens before the key loads, so the data directory may hold
	// koko.db and its WAL files, and nothing else.
	for _, f := range files(t, dataDir) {
		if f != "." && !strings.HasPrefix(f, "koko.db") {
			t.Errorf("the refusal wrote %s in the data directory", f)
		}
	}
}

// A relative path is refused without being printed: it may be the key itself,
// or part of it, pasted where its file's path belongs.
func TestServerRunRefusesARelativeRootKeyFileWithoutPrintingIt(t *testing.T) {
	id, key := newKeyFile(t, filepath.Join(t.TempDir(), "root.key"))
	for _, path := range []string{id + ":" + key, key, "root.key"} {
		r := serverRun(t, t.TempDir(), path)
		refused(t, r, key, "is not absolute")
		if strings.Contains(r.Stderr, path) {
			t.Error("koko server run printed the relative path it was given")
		}
	}
	// With a relative data directory too, the key given as a path would land
	// inside it, and that refusal names the path; this one comes first.
	t.Chdir(t.TempDir())
	r := serverRun(t, ".", id+":"+key)
	refused(t, r, key, "is not absolute")
}
