package server

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"

	"github.com/cieons/koko/internal/server/deployment"
	"github.com/cieons/koko/internal/server/seal"
	"github.com/cieons/koko/internal/server/store"
)

// loadKeys loads the Root Key from its file at path and, once the koko
// server is initialised, opens the koko server's own keys with it; before
// initialisation it gives no keys. It
// refuses a file that is missing, cannot be read or does not parse, and,
// over sealed data, a Root Key the data was not sealed under. Every refusal
// names the path, and over sealed data the id of the Root Key it needs, so
// the Owner knows which copy to put back; none writes a key (P0 spec:
// Secrets).
func loadKeys(ctx context.Context, st *store.Store, path string) (*seal.RootKey, *deployment.Keys, error) {
	var needs string
	err := st.Read(ctx, func(tx store.Tx) (err error) {
		needs, err = deployment.SealedBy(ctx, tx)
		return err
	})
	if err != nil {
		return nil, nil, fmt.Errorf("read which Root Key the sealed data needs: %w", err)
	}
	key, err := seal.Load(path)
	switch {
	case err != nil && needs != "":
		return nil, nil, fmt.Errorf("%w; the sealed data needs %s", err, needs)
	case err != nil:
		return nil, nil, err
	case needs != "" && key.ID() != needs:
		return nil, nil, fmt.Errorf("the Root Key file %s names %s, but the sealed data needs %s: put back the Root Key file that names %s",
			path, key.ID(), needs, needs)
	}
	var keys *deployment.Keys
	err = st.Read(ctx, func(tx store.Tx) (err error) {
		keys, err = deployment.Open(ctx, tx, key)
		return err
	})
	if err != nil {
		return nil, nil, fmt.Errorf("open the koko server's keys with the Root Key file %s: %w", path, err)
	}
	return key, keys, nil
}

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
