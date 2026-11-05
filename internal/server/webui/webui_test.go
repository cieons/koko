package webui_test

import (
	"bytes"
	"encoding/json/v2"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/cieons/koko/internal/server/webui"
)

// The binary embeds the web UI make built into site/app, every file of it,
// or the placeholder page when the build found none.
func TestEmbedsTheBuildOutputOnDiskOrThePlaceholder(t *testing.T) {
	app := os.DirFS("site/app")
	_, err := fs.Stat(app, "index.html")
	built := err == nil
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("read the build output: %v", err)
	}
	if webui.Built() != built {
		t.Errorf("Built() = %v, but site/app/index.html on disk exists = %v", webui.Built(), built)
	}
	if !built {
		want, err := os.ReadFile("site/placeholder/index.html")
		if err != nil {
			t.Fatal(err)
		}
		if got, err := fs.ReadFile(webui.FS(), "index.html"); err != nil || !bytes.Equal(got, want) {
			t.Errorf("index.html is not the placeholder: %v\n%s", err, got)
		}
		return
	}
	err = fs.WalkDir(app, ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		want, err := fs.ReadFile(app, name)
		if err != nil {
			return err
		}
		if got, err := fs.ReadFile(webui.FS(), name); err != nil || !bytes.Equal(got, want) {
			t.Errorf("%s is not embedded as built: %v", name, err)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk the build output: %v", err)
	}
}

// A plain go build, which never ran make webui, carries the placeholder page.
// The build output is hidden from go run with an overlay, so this holds even
// after make has built the web UI.
func TestAPlainGoBuildCarriesThePlaceholder(t *testing.T) {
	hide := map[string]string{}
	err := filepath.WalkDir("site/app", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		abs, err := filepath.Abs(path)
		hide[abs] = "" // an empty replacement deletes the file
		return err
	})
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("list the build output: %v", err)
	}
	overlay, err := json.Marshal(map[string]any{"Replace": hide})
	if err != nil {
		t.Fatal(err)
	}
	overlayFile := filepath.Join(t.TempDir(), "overlay.json")
	if err := os.WriteFile(overlayFile, overlay, 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), "go", "run", "-overlay", overlayFile, "./testdata/page")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go run ./testdata/page without the build output: %v\n%s", err, stderr.String())
	}
	placeholder, err := os.ReadFile("site/placeholder/index.html")
	if err != nil {
		t.Fatal(err)
	}
	if want := "built=false\n" + string(placeholder); string(out) != want {
		t.Errorf("a build without site/app serves:\n%s\nwant the placeholder:\n%s", out, want)
	}
}

// Someone who built koko with go build learns from the page how to get the web UI.
func TestThePlaceholderSaysHowToBuildTheWebUI(t *testing.T) {
	page, err := os.ReadFile("site/placeholder/index.html")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"without its web UI", "make build", "Node"} {
		if !bytes.Contains(page, []byte(want)) {
			t.Errorf("the placeholder does not say %q", want)
		}
	}
}
