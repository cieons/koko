package store

import (
	"io/fs"
	"slices"
	"testing"
)

// Every step file is in the list once, and every listed step has its file, so
// a step added without its line (or the reverse) fails here, not at start-up.
func TestStepsListEveryMigrationFileOnce(t *testing.T) {
	files, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil {
		t.Fatal(err)
	}
	listed := make([]string, len(steps))
	for i, name := range steps {
		listed[i] = "migrations/" + name
	}
	if dup := len(listed) - len(slices.Compact(slices.Sorted(slices.Values(listed)))); dup != 0 {
		t.Errorf("steps lists %d file(s) more than once", dup)
	}
	for _, f := range files {
		if !slices.Contains(listed, f) {
			t.Errorf("%s is not in steps: add its line at the end of the list", f)
		}
	}
	for _, f := range listed {
		if !slices.Contains(files, f) {
			t.Errorf("steps lists %s, which does not exist", f)
		}
	}
}
