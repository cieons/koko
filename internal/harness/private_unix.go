//go:build !windows

package harness

import (
	"fmt"
	"io/fs"
	"os"
)

// notPrivate says how the file or directory at path, whose Lstat is fi,
// differs from one private to this OS user, or "" when it is private: a
// directory of mode 0700, or a file of mode 0600.
func notPrivate(_ string, fi fs.FileInfo) string {
	want := fs.FileMode(0o600)
	if fi.IsDir() {
		want = 0o700
	}
	if fi.Mode().Perm() != want {
		return fmt.Sprintf("its mode is %04o, want %04o", fi.Mode().Perm(), want)
	}
	return ""
}

// access describes who may reach the file whose Lstat is fi: its mode.
func access(_ string, fi fs.FileInfo) (string, error) {
	return fi.Mode().String(), nil
}

// letOthersIn lets other users read the file or directory at path, whose
// Lstat is fi, so that it is no longer private to this OS user: its mode
// gains read for the group and for others, and search on a directory.
func letOthersIn(path string, fi fs.FileInfo) error {
	add := fs.FileMode(0o044)
	if fi.IsDir() {
		add = 0o055
	}
	return os.Chmod(path, fi.Mode().Perm()|add)
}
