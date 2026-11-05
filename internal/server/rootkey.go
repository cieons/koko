package server

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
)

// checkRootKeyOutside refuses a Root Key file inside the data directory, or
// the directory itself, so that a copy of the data directory never holds the
// key (P0 spec: install and run). Both paths are compared after resolving
// symlinks, so a link outside that leads in is refused too. A path that
// cannot be resolved is refused, never taken for one outside.
func checkRootKeyOutside(keyPath, dataDir string) error {
	key, keyErr := resolve(keyPath)
	dir, dirErr := resolve(dataDir)
	if err := errors.Join(keyErr, dirErr); err != nil {
		return fmt.Errorf("check that the Root Key file %s is outside the data directory %s: %w", keyPath, dataDir, err)
	}
	if rel, err := filepath.Rel(dir, key); err == nil && filepath.IsLocal(rel) {
		return fmt.Errorf("the Root Key file %s is inside the data directory %s; keep it outside, so that a copy of the data directory never holds it", keyPath, dataDir)
	}
	return nil
}

// resolve makes path absolute and resolves the symlinks in as much of it as
// exists, so that a file not made yet resolves like its directory.
func resolve(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	missing := ""
	for {
		resolved, err := filepath.EvalSymlinks(abs)
		switch {
		case err == nil:
			return filepath.Join(resolved, missing), nil
		case !errors.Is(err, fs.ErrNotExist):
			return "", err
		}
		parent := filepath.Dir(abs)
		if parent == abs {
			return filepath.Join(abs, missing), nil
		}
		missing = filepath.Join(filepath.Base(abs), missing)
		abs = parent
	}
}
