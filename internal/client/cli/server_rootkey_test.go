package cli_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/shared/api"
)

// initialise starts a koko server on dataDir with the Root Key file keyFile,
// initialises it, stops it, and returns the host key fingerprint that
// initialisation gave.
func initialise(t *testing.T, dataDir, keyFile string) string {
	t.Helper()
	h := harness.Start(t, harness.NoInit(), harness.DataDir(dataDir), harness.RootKeyFile(keyFile))
	resp, err := h.Call(api.ServerInit, api.ServerInitRequest{Owner: harness.Owner, Address: harness.Address})
	if err != nil {
		t.Fatalf("server.init: %v", err)
	}
	h.Stop()
	return resp.HostKeyFingerprint
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

// A restart with the Root Key the data was sealed under opens the koko
// server's keys: it logs the host key initialisation made.
func TestServerRunOpensTheSealedKeysWithTheirRootKey(t *testing.T) {
	root := t.TempDir()
	dataDir, keyFile := filepath.Join(root, "data"), filepath.Join(root, "etc", "root.key")
	newKeyFile(t, keyFile)
	fingerprint := initialise(t, dataDir, keyFile)

	r := serverRun(t, dataDir, keyFile)
	if r.Status != 0 {
		t.Fatalf("exit status %d, stderr %q", r.Status, r.Stderr)
	}
	if !strings.Contains(r.Stderr, "initialised=true host_key="+fingerprint) {
		t.Errorf("the start-up log does not name the host key %s:\n%s", fingerprint, r.Stderr)
	}
}

// Before initialisation the start-up log says so, and names no host key.
func TestServerRunBeforeInitialisation(t *testing.T) {
	keyFile := filepath.Join(t.TempDir(), "root.key")
	newKeyFile(t, keyFile)
	r := serverRun(t, t.TempDir(), keyFile)
	if r.Status != 0 || !strings.Contains(r.Stderr, "initialised=false") || strings.Contains(r.Stderr, "host_key") {
		t.Errorf("exit status %d, stderr %q", r.Status, r.Stderr)
	}
}

// Over sealed data, a restart without the Root Key the data was sealed under
// is refused: the refusal names the path and the Root Key id the sealed data
// needs, and nothing is written, in the data directory or beside the Root
// Key file (P0 spec: Secrets, stories 43 and 44).
func TestServerRunRefusesAnotherRootKeyOverSealedData(t *testing.T) {
	for _, tc := range []struct {
		name string
		// replace changes the Root Key file at path and returns the key
		// that no output may show, and what the refusal says before the id
		// the sealed data needs.
		replace func(t *testing.T, path string) (key, says string)
	}{
		{"another Root Key", func(t *testing.T, path string) (string, string) {
			id, key := newKeyFile(t, path)
			return key, "the Root Key file " + path + " names " + id + ", but the sealed data needs "
		}},
		{"a missing file", func(t *testing.T, path string) (string, string) {
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			return "", "the Root Key file " + path + " is missing; the sealed data needs "
		}},
		{"a file that does not parse", func(t *testing.T, path string) (string, string) {
			_, key := newKeyFile(t, path)
			if err := os.WriteFile(path, []byte(key+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			return key, "the Root Key file " + path + " does not parse"
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			dataDir, keyFile := filepath.Join(root, "data"), filepath.Join(root, "etc", "root.key")
			id, _ := newKeyFile(t, keyFile)
			initialise(t, dataDir, keyFile)
			key, says := tc.replace(t, keyFile)
			before := contents(t, root)

			r := serverRun(t, dataDir, keyFile)
			refused(t, r, key, says)
			if !strings.Contains(r.Stderr, "the sealed data needs "+id) {
				t.Errorf("stderr %q does not name %s, the Root Key the sealed data needs", r.Stderr, id)
			}
			if c := changed(before, contents(t, root)); len(c) > 0 {
				t.Errorf("the refusal changed %v", c)
			}
		})
	}
}

// changed names the files that differ between two reads of contents, and
// none of their content, which may hold a key.
func changed(before, after map[string]string) []string {
	var names []string
	for name, content := range before {
		if a, ok := after[name]; !ok || a != content {
			names = append(names, name)
		}
	}
	for name := range after {
		if _, ok := before[name]; !ok {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return names
}
