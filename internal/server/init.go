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

// initialise is server.init. In one transaction it runs initialisation's
// steps (initialiseSteps), then issues the Owner's first Join Code with
// issuer, as device.add issues one over the Unix socket, naming the port it
// listens on; a refusal of either leaves the koko server as it was (P0
// spec: install and run).
func initialise(rootKey *seal.RootKey, issuer *device.Issuer, tcp *tcpListener, log *slog.Logger) op.Func[api.ServerInitRequest, api.ServerInitResponse] {
	return func(ctx context.Context, tx store.Tx, caller op.Caller, req api.ServerInitRequest) (api.ServerInitResponse, error) {
		keys, err := initialiseSteps(ctx, tx, rootKey, tcp, log, req)
		if err != nil {
			return api.ServerInitResponse{}, err
		}
		code, err := issuer.Issue(ctx, tx, caller, api.DeviceAddRequest{})
		if err != nil {
			return api.ServerInitResponse{}, err
		}
		return api.ServerInitResponse{Address: req.Address, HostKeyFingerprint: keys.HostKeyFingerprint(), JoinCode: code}, nil
	}
}

// initialiseSteps runs initialisation's steps inside tx, which server.init
// and a fresh koko server's first start share (ADR 0038): it makes the koko
// key and the host key, sealed under rootKey, stores the Server Address,
// creates the Owner with their Personal Space and starts listening on TCP.
// It issues no Join Code. So that a refused or failed initialisation leaves
// the koko server as it was, the listener serves only once tx commits, and
// closes when it rolls back. A second initialisation is refused, so nothing
// replaces the Owner or the keys.
func initialiseSteps(ctx context.Context, tx store.Tx, rootKey *seal.RootKey, tcp *tcpListener, log *slog.Logger, req api.ServerInitRequest) (*deployment.Keys, error) {
	initialised, err := deployment.Initialised(ctx, tx)
	if err != nil {
		return nil, fmt.Errorf("initialise the koko server: %w", err)
	}
	if initialised {
		return nil, api.CodeAlreadyInitialised.Errorf("the koko server is initialised already; its Owner and its keys stay as they are")
	}
	keys, err := deployment.Create(ctx, tx, rootKey, req.Address)
	if err != nil {
		return nil, err
	}
	if err := user.AddOwner(ctx, tx, req.Owner); err != nil {
		return nil, err
	}
	if err := tcp.bind(ctx); err != nil {
		return nil, err
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
	return keys, nil
}
