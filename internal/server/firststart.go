package server

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/cieons/koko/internal/server/access"
	"github.com/cieons/koko/internal/server/deployment"
	"github.com/cieons/koko/internal/server/filesystem"
	"github.com/cieons/koko/internal/server/op"
	"github.com/cieons/koko/internal/server/seal"
	"github.com/cieons/koko/internal/server/store"
	"github.com/cieons/koko/internal/shared/api"
)

// Initialisation at first start (ADR 0038): a fresh koko server given the
// Owner and the Server Address makes its Root Key when the file is missing,
// then initialises itself before it serves anything, issuing no Join Code.

// rootKeyMissing tells whether nothing is at the Root Key's path. Anything
// there, or a path that cannot be looked at, is left to loading, which
// reuses or refuses it as it is.
func rootKeyMissing(path string) bool {
	_, err := os.Lstat(path)
	return errors.Is(err, fs.ErrNotExist)
}

// checkRootKeyDir refuses, before anything is written, a Root Key that
// start-up must make in a directory it cannot write: when the file is
// missing and the data directory holds no koko.db yet, so nothing can be
// sealed. Over an existing koko.db, makeRootKey checks once it knows.
func checkRootKeyDir(path, dataDir string) error {
	if !rootKeyMissing(path) {
		return nil
	}
	if _, err := os.Stat(filepath.Join(dataDir, store.FileName)); !errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return probeRootKeyDir(path)
}

// probeRootKeyDir refuses a directory this process cannot write where it
// would make the Root Key file at path, or the nearest one above it while
// that is missing.
func probeRootKeyDir(path string) error {
	dir, err := filesystem.Nearest(filepath.Dir(path))
	if err != nil {
		return fmt.Errorf("find the directory of the Root Key file %s: %w", path, err)
	}
	if err := probeWrite(dir); err != nil {
		return cannotWrite(fmt.Sprintf("%s, where it would make the Root Key file %s", dir, path), err)
	}
	return nil
}

// makeRootKey writes a new Root Key at path when the file is missing and
// nothing in st is sealed yet, and tells whether it did. It never writes
// over sealed data, which loading refuses, naming the Root Key it needs.
func makeRootKey(ctx context.Context, st *store.Store, path string) (made bool, err error) {
	if !rootKeyMissing(path) {
		return false, nil
	}
	var sealedBy string
	err = st.Read(ctx, func(tx store.Tx) (err error) {
		sealedBy, err = deployment.SealedBy(ctx, tx)
		return err
	})
	switch {
	case err != nil:
		return false, fmt.Errorf("read whether the koko server's data is sealed: %w", err)
	case sealedBy != "":
		return false, nil
	}
	if err := probeRootKeyDir(path); err != nil {
		return false, err
	}
	if err := writeKeyFile(path, seal.NewRootKey()); err != nil {
		return false, fmt.Errorf("write the Root Key file %s: %w", path, err)
	}
	return true, nil
}

// writeKeyFile writes content as the file at path, 0600 and the process's
// own, through a temporary file beside it that is synced, then renamed into
// place, then syncs the directory, which it makes, 0700, when missing.
func writeKeyFile(path string, content []byte) (err error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*") // mode 0600
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
		}
	}()
	if _, err := tmp.Write(content); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return err
	}
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	return errors.Join(d.Sync(), d.Close())
}

// initialiseAtStart runs initialisation's steps with req, as server.init
// does, in one write of the koko server's own, and issues no Join Code; so
// it refuses itself the address server.init refuses when it issues one.
// logAttrs go on the koko server initialised line.
func initialiseAtStart(ctx context.Context, a *op.API, rootKey *seal.RootKey, tcp *tcpListener, log *slog.Logger, req api.ServerInitRequest, logAttrs ...any) (*deployment.Keys, error) {
	var keys *deployment.Keys
	err := a.Do(ctx, func(ctx context.Context, tx store.Tx) (err error) {
		if keys, err = initialiseSteps(ctx, tx, rootKey, tcp, log, req, logAttrs...); err != nil {
			return err
		}
		return checkJoinCodeAddress(req.Address, tcp.port())
	})
	if err != nil {
		return nil, fmt.Errorf("initialise the koko server with the Owner %q and the Server Address %q: %w", req.Owner, req.Address, err)
	}
	return keys, nil
}

// warnUnused logs a warning when an initialised koko server is given an
// Owner or a Server Address, in req, that differs from its record, which
// stays as it is; equal values, or none, log nothing.
func warnUnused(ctx context.Context, st *store.Store, req api.ServerInitRequest, log *slog.Logger) error {
	if req == (api.ServerInitRequest{}) {
		return nil
	}
	var owner, address string
	err := st.Read(ctx, func(tx store.Tx) error {
		// The Local caller acts as the Owner (ADR 0008).
		who, err := access.Who(ctx, tx, op.Caller{Local: true})
		if err != nil {
			return err
		}
		owner = who.User.Name
		address, err = deployment.Address(ctx, tx)
		return err
	})
	if err != nil {
		return fmt.Errorf("read the koko server's Owner and Server Address: %w", err)
	}
	var differs []any
	if req.Owner != owner {
		differs = append(differs, "owner", req.Owner, "recorded_owner", owner)
	}
	if req.Address != address {
		differs = append(differs, "address", req.Address, "recorded_address", address)
	}
	if differs != nil {
		log.Warn("the koko server is initialised, so it keeps its record and does not use KOKO_OWNER (--owner) or KOKO_ADDRESS (--address), "+
			"which only a fresh koko server reads; to move the Server Address, run koko server set address=<host[:port]>", differs...)
	}
	return nil
}
