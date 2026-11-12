//go:build !windows

package devicefiles

import (
	"fmt"
	"io/fs"
	"os"
	"syscall"
)

// HomeVar names the environment variable that holds the home directory.
const HomeVar = "HOME"

// restrict sets path's permission bits to mode, such as 0700 for koko's own
// directory, so only this OS user reads the device's key.
func restrict(path string, mode fs.FileMode) error {
	if err := os.Chmod(path, mode); err != nil {
		return fmt.Errorf("make %s private to this user: %w", path, err)
	}
	return nil
}

// isPrivate tells whether the file fi describes is as restrict leaves one
// of koko's files: mode 0600.
func isPrivate(_ string, fi fs.FileInfo) bool {
	return fi.Mode().Perm() == 0o600
}

// keepAccess gives the new file tmp the access the user's file at path has,
// whose Lstat is fi, which tmp is about to replace: its mode.
func keepAccess(tmp, _ string, fi fs.FileInfo) error {
	return os.Chmod(tmp, fi.Mode().Perm())
}

// ownedByMe tells whether this process's user owns the file fi describes.
func ownedByMe(_ string, fi fs.FileInfo) bool {
	st, ok := fi.Sys().(*syscall.Stat_t)
	return ok && int(st.Uid) == os.Geteuid()
}
