package server

import (
	"context"
	"fmt"

	"github.com/cieons/koko/internal/server/deployment"
	"github.com/cieons/koko/internal/server/op"
	"github.com/cieons/koko/internal/server/seal"
	"github.com/cieons/koko/internal/server/store"
	"github.com/cieons/koko/internal/server/user"
	"github.com/cieons/koko/internal/shared/api"
)

// registerServer registers the server area's operations, which act on the
// koko server itself, with the Root Key it loaded.
func registerServer(a *op.API, rootKey *seal.RootKey) {
	a.Handle(api.ServerInit, initialise(rootKey), op.LocalOnly, op.BeforeInit)
}

// initialise is server.init. In one transaction it makes the koko key and
// the host key, sealed under rootKey, creates the Owner with their Personal
// Space, and stores the Server Address, so that a refused or failed
// initialisation leaves the koko server as it was (P0 spec: install and run).
// A second initialisation is refused, so nothing replaces the Owner or the
// keys.
func initialise(rootKey *seal.RootKey) op.Func[api.ServerInitRequest, api.ServerInitResponse] {
	return func(ctx context.Context, tx store.Tx, _ op.Caller, req api.ServerInitRequest) (api.ServerInitResponse, error) {
		initialised, err := deployment.Initialised(ctx, tx)
		if err != nil {
			return api.ServerInitResponse{}, fmt.Errorf("initialise the koko server: %w", err)
		}
		if initialised {
			return api.ServerInitResponse{}, api.CodeAlreadyInitialised.Errorf("the koko server is initialised already; its Owner and its keys stay as they are")
		}
		keys, err := deployment.Create(ctx, tx, rootKey, req.Address)
		if err != nil {
			return api.ServerInitResponse{}, err
		}
		if err := user.AddOwner(ctx, tx, req.Owner); err != nil {
			return api.ServerInitResponse{}, err
		}
		return api.ServerInitResponse{Address: req.Address, HostKeyFingerprint: keys.HostKeyFingerprint()}, nil
	}
}
