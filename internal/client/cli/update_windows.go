package cli

import (
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

// kokoFile is the name of koko's file in the directory it is installed in.
const kokoFile = "koko.exe"

// replaceable names the koko koko update replaces, for its refusal of
// another.
const replaceable = `the koko install.ps1 put in ~\.local\bin`

// The name of a koko file renamed aside: .koko.old-<random>.exe, beside
// koko.exe (ADR 0052).
const (
	oldPrefix = ".koko.old-"
	oldSuffix = ".exe"
)

// replaceFile puts the checked file in place of target, a file in the same
// directory's filesystem. Windows does not let a running program's file be
// replaced or deleted, but lets it be renamed within its directory, so
// target is renamed aside, where a koko that runs from it keeps running,
// and checked is renamed to target. When that fails the old file is renamed
// back, and the error says that nothing was changed. Between the two
// renames there is no target. Afterwards every file renamed aside that no
// process runs any more is deleted; one that still runs is hidden and left
// for the next call.
func replaceFile(checked, target string) error {
	dir := filepath.Dir(target)
	old := filepath.Join(dir, oldPrefix+strings.ToLower(rand.Text())+oldSuffix)
	if err := os.Rename(target, old); err != nil {
		return fmt.Errorf("%w%s", err, nothingChanged)
	}
	if err := os.Rename(checked, target); err != nil {
		if back := os.Rename(old, target); back != nil {
			return fmt.Errorf("%w; the koko that was there is now %s and could not be renamed back (%w): rename it to %s",
				err, old, back, filepath.Base(target))
		}
		return fmt.Errorf("%w%s", err, nothingChanged)
	}
	removeOld(dir)
	return nil
}

// removeOld deletes every koko file renamed aside in dir that can be
// deleted, and hides one that cannot, which a process still runs.
func removeOld(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, oldPrefix) || !strings.HasSuffix(name, oldSuffix) {
			continue
		}
		file := filepath.Join(dir, name)
		if os.Remove(file) == nil {
			continue
		}
		p, err := windows.UTF16PtrFromString(file)
		if err != nil {
			continue
		}
		if attrs, err := windows.GetFileAttributes(p); err == nil {
			_ = windows.SetFileAttributes(p, attrs|windows.FILE_ATTRIBUTE_HIDDEN)
		}
	}
}
