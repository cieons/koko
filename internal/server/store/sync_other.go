//go:build !unix

package store

import (
	"errors"
	"fmt"
	"os"
)

// syncPath flushes the file at path to disk, and does nothing for a
// directory. The koko server runs on Linux only; this is for the CLI's
// tests on Windows, which start it in the test process (ADR 0051), and
// Windows flushes only a file opened for writing, and no directory.
func syncPath(path string) error {
	fi, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("sync: %w", err)
	}
	if fi.IsDir() {
		return nil
	}
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("sync: %w", err)
	}
	if err := errors.Join(f.Sync(), f.Close()); err != nil {
		return fmt.Errorf("sync: %w", err)
	}
	return nil
}
