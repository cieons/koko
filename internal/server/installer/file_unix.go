//go:build unix

package installer

import (
	"fmt"
	"io/fs"
	"os"
	"syscall"
)

// fileUID is the uid that owns the file info describes.
func fileUID(info fs.FileInfo) (uint32, error) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, fmt.Errorf("%s has no owner the system tells", info.Name())
	}
	return st.Uid, nil
}

// openRequest opens the file at path for reading: it fails when path is a
// symbolic link rather than follow it, and never waits, as it would for the
// writer of a named pipe.
func openRequest(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
}
