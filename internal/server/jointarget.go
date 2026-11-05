package server

import (
	"context"
	"crypto/sha256"

	"github.com/cieons/koko/internal/server/deployment"
	"github.com/cieons/koko/internal/server/device"
	"github.com/cieons/koko/internal/server/seal"
	"github.com/cieons/koko/internal/server/store"
	"github.com/cieons/koko/internal/shared/serveraddr"
)

// listenPort is the port the koko server listens on, its listen address
// being :443 (P0 spec: install and run). A Server Address without a port
// names it.
const listenPort = 443

// joinTarget returns what the koko server's Join Codes name, read inside
// each call's transaction so that a code issued during initialisation names
// what it made: the Server Address, with the listen port written when it
// names none, and the host key's fingerprint, opened with rootKey.
func joinTarget(rootKey *seal.RootKey) func(context.Context, store.Tx) (device.Target, error) {
	return func(ctx context.Context, tx store.Tx) (device.Target, error) {
		address, err := deployment.Address(ctx, tx)
		if err != nil {
			return device.Target{}, err
		}
		hostKey, err := deployment.HostKey(ctx, tx, rootKey)
		if err != nil {
			return device.Target{}, err
		}
		return device.Target{
			Address:     serveraddr.WithPort(address, listenPort),
			Fingerprint: sha256.Sum256(hostKey.Marshal()),
		}, nil
	}
}
