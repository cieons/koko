//go:build unix

package store

import (
	"errors"
	"fmt"
	"os"
)

// syncPath flushes the file or directory at path to disk.
func syncPath(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("sync: %w", err)
	}
	if err := errors.Join(f.Sync(), f.Close()); err != nil {
		return fmt.Errorf("sync: %w", err)
	}
	return nil
}
