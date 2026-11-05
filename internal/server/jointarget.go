package server

import (
	"context"
	"crypto/sha256"
	"errors"

	"github.com/cieons/koko/internal/server/deployment"
	"github.com/cieons/koko/internal/server/device"
	"github.com/cieons/koko/internal/server/seal"
	"github.com/cieons/koko/internal/server/store"
	"github.com/cieons/koko/internal/shared/serveraddr"
)

// joinTarget returns what the koko server's Join Codes name, read inside
// each call's transaction so that a code issued during initialisation names
// what it made: the Server Address, with the port the koko server listens on,
// listenPort, written when it names none (P0 spec: install and run), and the
// host key's fingerprint, opened with rootKey.
func joinTarget(rootKey *seal.RootKey, listenPort func() int) func(context.Context, store.Tx) (device.Target, error) {
	return func(ctx context.Context, tx store.Tx) (device.Target, error) {
		address, err := deployment.Address(ctx, tx)
		if err != nil {
			return device.Target{}, err
		}
		port := listenPort()
		if port == 0 {
			return device.Target{}, errors.New("the koko server does not listen on TCP, so a Join Code names no port")
		}
		hostKey, err := deployment.HostKey(ctx, tx, rootKey)
		if err != nil {
			return device.Target{}, err
		}
		return device.Target{
			Address:     serveraddr.WithPort(address, port),
			Fingerprint: sha256.Sum256(hostKey.Marshal()),
		}, nil
	}
}
