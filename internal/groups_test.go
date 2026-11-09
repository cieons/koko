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

// group says which package group an import path is in
// (docs/subsystems/code-layout.md): a program under cmd/ is a group of its
// own, named by its path, such as cmd/koko.
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
		return rel
	}
	return "none"
}

// mayNotUse lists, for each group, the groups its packages may not depend on,
// directly or not; "cmd" stands for every program. Only the harness depends
// on both the server and the client groups, and no product code depends on
// the harness. Each program has a row of its own: koko is the CLI and kokod
// the koko server (ADR 0040).
var mayNotUse = map[string][]string{
	"shared":    {"server", "client", "harness", "cmd"},
	"server":    {"client", "harness", "cmd"},
	"client":    {"server", "harness", "cmd"},
	"cmd/koko":  {"server", "harness", "cmd"},
	"cmd/kokod": {"client", "harness", "cmd"},
	"harness":   {"cmd"},
}

// inGroup tells whether the group g is banned or, for "cmd", one of the
// programs.
func inGroup(g, banned string) bool {
	return g == banned || banned == "cmd" && strings.HasPrefix(g, "cmd/")
}

// The server and the CLI stay apart: a test (not a linter) fails when a
// client package or koko depends on a server package, when a server package
// or kokod depends on a client package, or when a program has no row in
// mayNotUse, on every OS koko builds for. Test files may import the harness,
// so only non-test dependencies count.
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
				banned, ok := mayNotUse[g]
				if !ok {
					t.Errorf("the program %s has no row in mayNotUse: give it one, saying which groups it may not depend on", pkg.ImportPath)
					continue
				}
				for _, dep := range pkg.Deps {
					for _, b := range banned {
						if inGroup(group(dep), b) {
							t.Errorf("%s (%s) depends on %s (%s)", pkg.ImportPath, g, dep, group(dep))
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
