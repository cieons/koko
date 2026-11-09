package webui_test

import (
	"bytes"
	"compress/gzip"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
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

// The web UI's size budget (ADR 0045): kokod embeds the build uncompressed,
// so all of it stays within 3 MiB on disk, and what a browser loads to show
// the first page, gzipped as the Owner's reverse proxy sends it, within
// 450 KiB.
const (
	budgetOnDisk    = 3 << 20
	budgetFirstLoad = 450 << 10
)

func TestTheBuiltWebUIStaysWithinItsSizeBudget(t *testing.T) {
	if !webui.Built() {
		t.Skip("this binary carries the placeholder page; make webui builds the web UI")
	}
	onDisk, err := sizeOnDisk(webui.FS())
	if err != nil {
		t.Fatal(err)
	}
	files, err := firstLoad(webui.FS())
	if err != nil {
		t.Fatal(err)
	}
	gzipped := 0
	for _, name := range files {
		n, err := gzippedSize(webui.FS(), name)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("first load: %s, %d bytes gzipped", name, n)
		gzipped += n
	}
	t.Logf("the build: %d bytes on disk; its first load: %d files, %d bytes gzipped", onDisk, len(files), gzipped)
	if onDisk > budgetOnDisk {
		t.Errorf("the build is %d bytes on disk, over its budget of %d", onDisk, budgetOnDisk)
	}
	if gzipped > budgetFirstLoad {
		t.Errorf("the first load is %d bytes gzipped, over its budget of %d", gzipped, budgetFirstLoad)
	}
}

// sizeOnDisk is the size of every file of fsys.
func sizeOnDisk(fsys fs.FS) (int64, error) {
	var total int64
	err := fs.WalkDir(fsys, ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		total += info.Size()
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("walk the build: %w", err)
	}
	return total, nil
}

var (
	// htmlRef is a file index.html loads: a script's src, or a link's href
	// (the stylesheet and the modules Vite preloads).
	htmlRef = regexp.MustCompile(`<(?:script|link)\b[^>]*?\s(?:src|href)="([^"]+)"`)
	// cssRef is a file a stylesheet loads, such as a font.
	cssRef = regexp.MustCompile(`url\(\s*["']?([^"')]+)["']?\s*\)`)
)

// firstLoad lists the files a browser loads to show the first page:
// index.html, the files it names, and the files their stylesheets name. A
// file it names that the build lacks is an error, not a smaller load.
func firstLoad(fsys fs.FS) ([]string, error) {
	index, err := fs.ReadFile(fsys, "index.html")
	if err != nil {
		return nil, fmt.Errorf("read index.html: %w", err)
	}
	files := []string{"index.html"}
	for _, m := range htmlRef.FindAllSubmatch(index, -1) {
		name, err := local(fsys, ".", string(m[1]))
		if err != nil {
			return nil, fmt.Errorf("index.html: %w", err)
		}
		files = append(files, name)
		if path.Ext(name) != ".css" {
			continue
		}
		css, err := fs.ReadFile(fsys, name)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", name, err)
		}
		for _, m := range cssRef.FindAllSubmatch(css, -1) {
			// A data: URL loads nothing more, and #id names an element.
			if strings.HasPrefix(string(m[1]), "data:") || strings.HasPrefix(string(m[1]), "#") {
				continue
			}
			ref, err := local(fsys, path.Dir(name), string(m[1]))
			if err != nil {
				return nil, fmt.Errorf("%s: %w", name, err)
			}
			files = append(files, ref)
		}
	}
	slices.Sort(files)
	return slices.Compact(files), nil
}

// local is the file of fsys that ref, a URL in a file of dir, names, its
// query and fragment aside.
func local(fsys fs.FS, dir, ref string) (string, error) {
	p, _, _ := strings.Cut(ref, "#")
	p, _, _ = strings.Cut(p, "?")
	name := path.Clean(path.Join(dir, p))
	if after, ok := strings.CutPrefix(p, "/"); ok {
		name = path.Clean(after)
	}
	if strings.Contains(p, "//") || !fs.ValidPath(name) {
		return "", fmt.Errorf("%q names no file of the build", ref)
	}
	info, err := fs.Stat(fsys, name)
	if err != nil {
		return "", fmt.Errorf("%q names no file of the build: %w", ref, err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("%q names a directory of the build, not a file", ref)
	}
	return name, nil
}

// gzippedSize is the size of the file name of fsys once gzipped.
func gzippedSize(fsys fs.FS, name string) (int, error) {
	data, err := fs.ReadFile(fsys, name)
	if err != nil {
		return 0, fmt.Errorf("read %s: %w", name, err)
	}
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(data); err != nil {
		return 0, fmt.Errorf("gzip %s: %w", name, err)
	}
	if err := zw.Close(); err != nil {
		return 0, fmt.Errorf("gzip %s: %w", name, err)
	}
	return buf.Len(), nil
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
