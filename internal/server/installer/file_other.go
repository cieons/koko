//go:build !unix

package installer

import (
	"errors"
	"io/fs"
	"os"
)

// errNotUnix is why the upgrade helper's file checks fail where the koko
// server never runs.
var errNotUnix = errors.New("the upgrade helper runs only on Linux")

// fileUID fails: the koko server runs only on Linux.
func fileUID(fs.FileInfo) (uint32, error) {
	return 0, errNotUnix
}

// openRequest fails: the koko server runs only on Linux.
func openRequest(string) (*os.File, error) {
	return nil, errNotUnix
}
