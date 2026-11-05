package devicemux

import (
	"errors"
	"io/fs"
	"os"
	"syscall"
)

// errWindows is why no Device Mux runs on Windows yet: commands there open
// their own Device Connection until the Windows CLI (P1).
var errWindows = errors.New("the Device Mux is not supported on Windows yet")

// ownedByMe would tell whether this OS user owns the file fi describes.
// Windows keeps that in ACLs, which the Windows CLI (P1) will read.
func ownedByMe(fs.FileInfo) bool {
	return false
}

// tryLock would take the Device Mux lock at path.
func tryLock(string) (*os.File, error) {
	return nil, errWindows
}

// detached would start a Device Mux detached from the command's console.
func detached() (*syscall.SysProcAttr, error) {
	return nil, errWindows
}
