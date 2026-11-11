// Package frozen checks golden files of revisions (ADR 0049): what each
// revision of an operation or a channel takes or carries, one section per
// revision. A section, once written, never changes, since a koko server
// goes on speaking a revision while some koko in the window speaks it: a
// change to what a revision takes or carries fails until it comes with a
// raise, whose new section -update then adds. Deleting the file lets
// -update write every section anew, which only a deliberate change does,
// and its diff shows.
package frozen

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// Section is one revision's section of a golden file.
type Section struct {
	// Name names the revision, such as "host.add 2" or "tunnel@koko 1".
	Name string
	// Text is what the revision takes or carries, a line for each thing.
	Text string
}

// header starts each section's line in the file, before its name.
const header = "== "

// Check checks got against the golden file testdata/<name> in the test's
// package. Every section of the file must be among got, as it is, and
// every section of got in the file; with update, Check adds to the file
// the sections it lacks instead, but still changes and removes none.
func Check(t testing.TB, name string, got []Section, update bool) {
	t.Helper()
	path := filepath.Join("testdata", name)
	b, err := os.ReadFile(path)
	// Only -update writes a golden file that is not there yet.
	if err != nil && (!update || !errors.Is(err, fs.ErrNotExist)) {
		t.Fatalf("read the golden file (go test -update writes it): %v", err)
	}
	want, err := parse(string(b))
	if err != nil {
		t.Fatalf("read the golden file %s: %v", path, err)
	}
	ok := true
	for _, w := range want {
		i := slices.IndexFunc(got, func(g Section) bool { return g.Name == w.Name })
		switch {
		case i < 0:
			t.Errorf("%s: revision %q is in the golden file but no longer produced: a revision, once released, is spoken while the window holds it", path, w.Name)
			ok = false
		case got[i].Text != w.Text:
			t.Errorf("%s: revision %q changed without a raise: raise the revision with the change (ADR 0049); -update adds the new revision's section and changes none\n got:\n%s\nwant:\n%s", path, w.Name, got[i].Text, w.Text)
			ok = false
		}
	}
	var added []string
	for _, g := range got {
		if !slices.ContainsFunc(want, func(w Section) bool { return w.Name == g.Name }) {
			added = append(added, g.Name)
		}
	}
	switch {
	case len(added) == 0:
	case !update:
		t.Errorf("%s: no section for the revision(s) %s: run go test -update to add them", path, strings.Join(added, ", "))
	case ok:
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(format(got)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// parse reads a golden file's sections.
func parse(file string) ([]Section, error) {
	var sections []Section
	for line := range strings.Lines(file) {
		if name, ok := strings.CutPrefix(line, header); ok {
			sections = append(sections, Section{Name: strings.TrimSuffix(name, "\n")})
			continue
		}
		if len(sections) == 0 {
			return nil, fmt.Errorf("the line %q comes before the first section", line)
		}
		sections[len(sections)-1].Text += line
	}
	for i := range sections {
		sections[i].Text = strings.TrimRight(sections[i].Text, "\n")
	}
	return sections, nil
}

// format writes sections as a golden file: each a header line and its
// text, with a blank line between them.
func format(sections []Section) string {
	var b strings.Builder
	for i, s := range sections {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(header + s.Name + "\n")
		if s.Text != "" {
			b.WriteString(s.Text + "\n")
		}
	}
	return b.String()
}
