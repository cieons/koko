//go:build !windows

package devicefiles

import (
	"fmt"
	"io/fs"
	"os"
)

// restrict sets path's permission bits to mode, such as 0700 for koko's own
// directory, so only this OS user reads the device's key.
func restrict(path string, mode fs.FileMode) error {
	if err := os.Chmod(path, mode); err != nil {
		return fmt.Errorf("make %s private to this user: %w", path, err)
	}
	return nil
}

// writable tells whether the user's file at path, whose Lstat is fi, is
// one koko may rewrite: one whose mode lets someone write it, which this
// process can open for writing, so neither a file made read-only on purpose
// nor one owned by another user is replaced. Opening it changes nothing.
func writable(path string, fi fs.FileInfo) bool {
	if fi.Mode().Perm()&0o222 == 0 {
		return false
	}
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return false
	}
	_ = f.Close()
	return true
}
