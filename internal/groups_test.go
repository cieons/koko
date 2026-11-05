package internal_test

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
)

const module = "github.com/cieons/koko"

// group says which package group an import path is in (docs/subsystems/code-layout.md).
func group(path string) string {
	rel, ok := strings.CutPrefix(path, module+"/")
	if !ok {
		return ""
	}
	for _, g := range []string{"internal/shared", "internal/server", "internal/client", "internal/harness"} {
		if rel == g || strings.HasPrefix(rel, g+"/") {
			return strings.TrimPrefix(g, "internal/")
		}
	}
	if strings.HasPrefix(rel, "cmd/") {
		return "cmd"
	}
	return "none"
}

// mayNotUse lists, for each group, the groups its packages may not depend on,
// directly or not. Only cmd/koko and the harness depend on both the server and
// the client groups, and no product code depends on the harness.
var mayNotUse = map[string][]string{
	"shared": {"server", "client", "harness", "cmd"},
	"server": {"client", "harness", "cmd"},
	"client": {"server", "harness", "cmd"},
	"cmd":    {"harness"},
}

// The server and the CLI stay separable: a test (not a linter) fails when a
// client package depends on a server package or the reverse, on every OS koko
// builds for. Test files may import the harness, so only non-test
// dependencies count.
func TestPackageGroupsStaySeparate(t *testing.T) {
	for _, goos := range []string{"linux", "darwin", "windows"} {
		t.Run(goos, func(t *testing.T) {
			for _, pkg := range listPackages(t, goos) {
				if len(pkg.GoFiles) == 0 {
					continue // only tests here, or no file for this OS
				}
				g := group(pkg.ImportPath)
				if g == "none" {
					t.Errorf("%s is in no package group: put it under internal/shared, internal/server or internal/client", pkg.ImportPath)
					continue
				}
				for _, dep := range pkg.Deps {
					for _, banned := range mayNotUse[g] {
						if group(dep) == banned {
							t.Errorf("%s (%s) depends on %s (%s)", pkg.ImportPath, g, dep, banned)
						}
					}
				}
			}
		})
	}
}

type goPackage struct {
	ImportPath string
	GoFiles    []string
	Deps       []string
}

// listPackages runs go list for one OS and returns koko's packages with their
// dependencies.
func listPackages(t *testing.T, goos string) []goPackage {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "go", "list", "-json=ImportPath,GoFiles,Deps", module+"/...")
	cmd.Env = append(os.Environ(), "GOOS="+goos, "GOARCH=amd64", "CGO_ENABLED=0")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list for %s: %v\n%s", goos, err, stderr.String())
	}
	dec := jsontext.NewDecoder(bytes.NewReader(out))
	var pkgs []goPackage
	for {
		var p goPackage
		if err := json.UnmarshalDecode(dec, &p); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			t.Fatalf("read go list output: %v", err)
		}
		pkgs = append(pkgs, p)
	}
	if len(pkgs) == 0 {
		t.Fatal("go list found no packages")
	}
	return pkgs
}
