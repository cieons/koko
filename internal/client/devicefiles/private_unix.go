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
