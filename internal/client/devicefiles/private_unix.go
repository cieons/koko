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

// MkdirPrivate makes the directory dir, mode 0700, so it is private to this
// OS user from the moment it exists. It fails wrapping fs.ErrExist when
// something is at dir.
func MkdirPrivate(dir string) error {
	return os.Mkdir(dir, 0o700)
}

// closedToOthers says why no OS user but this one may reach into the
// directory fi describes is untrue, or returns nil when it holds: its mode
// is 0700.
func closedToOthers(_ string, fi fs.FileInfo) error {
	if perm := fi.Mode().Perm(); perm != 0o700 {
		return fmt.Errorf("has mode %04o, not 0700", perm)
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
