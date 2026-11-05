package server

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/cieons/koko/internal/server/deployment"
	"github.com/cieons/koko/internal/server/device"
	"github.com/cieons/koko/internal/server/op"
	"github.com/cieons/koko/internal/server/seal"
	"github.com/cieons/koko/internal/server/store"
	"github.com/cieons/koko/internal/server/user"
	"github.com/cieons/koko/internal/shared/api"
)

// registerServer registers the server area's operations, which act on the
// koko server itself, with the Root Key it loaded, the issuer of its Join
// Codes and its TCP listener.
func registerServer(a *op.API, rootKey *seal.RootKey, issuer *device.Issuer, tcp *tcpListener, log *slog.Logger) {
	a.Handle(api.ServerInit, initialise(rootKey, issuer, tcp, log), op.LocalOnly, op.BeforeInit)
}

// initialise is server.init. In one transaction it makes the koko key and
// the host key, sealed under rootKey, creates the Owner with their Personal
// Space, stores the Server Address, starts listening on TCP and issues the
// Owner's first Join Code with issuer, as device.add issues one over the
// Unix socket, naming the port it listens on; so that a refused or failed
// initialisation leaves the koko server as it was (P0 spec: install and
// run), the listener serves only once the transaction commits, and closes
// when it rolls back. A second initialisation is refused, so nothing replaces
// the Owner or the keys.
func initialise(rootKey *seal.RootKey, issuer *device.Issuer, tcp *tcpListener, log *slog.Logger) op.Func[api.ServerInitRequest, api.ServerInitResponse] {
	return func(ctx context.Context, tx store.Tx, caller op.Caller, req api.ServerInitRequest) (api.ServerInitResponse, error) {
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
		if err := tcp.bind(ctx); err != nil {
			return api.ServerInitResponse{}, err
		}
		op.AfterTx(ctx, func(committed bool) {
			if !committed {
				tcp.unbind()
				return
			}
			if err := tcp.serve(keys.Host); err != nil {
				log.Error("serve Device Connections after initialisation", "err", err)
				return
			}
			log.Info("koko server initialised", "host_key", keys.HostKeyFingerprint(), "listen", tcp.address())
		})
		code, err := issuer.Issue(ctx, tx, caller, api.DeviceAddRequest{})
		if err != nil {
			return api.ServerInitResponse{}, err
		}
		return api.ServerInitResponse{Address: req.Address, HostKeyFingerprint: keys.HostKeyFingerprint(), JoinCode: code}, nil
	}
}
