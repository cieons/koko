package server

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/cieons/koko/internal/server/access"
	"github.com/cieons/koko/internal/server/hostlist"
	"github.com/cieons/koko/internal/server/op"
	"github.com/cieons/koko/internal/server/store"
	"github.com/cieons/koko/internal/shared/api"
)

// tellHostsChanged acts on a change to the Hosts some Users see, once it
// has committed: it tells each Device Connection whose User it concerns
// (transport.Server.TellHostsChanged), so that the device's Device Mux
// rewrites its bare names at once. Who that is it reads beside the call
// that made the change, which waits for no read. Close waits for it.
func (s *Server) tellHostsChanged(c hostlist.Change) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing {
		return
	}
	s.telling.Go(func() {
		ids, err := s.concerned(context.Background(), c)
		if err != nil {
			s.log.Warn("cannot tell devices the Hosts they see changed", "err", err)
			return
		}
		if len(ids) == 0 {
			return
		}
		s.transport.TellHostsChanged(ids...)
		s.log.Debug("told devices the Hosts they see changed", "device_sessions", ids)
	})
}

// concerned returns the Device Sessions holding a Device Connection now
// whose User the change concerns: one it names, or one who acts in a Space
// it names, by the access check's rule, read after the change. A Device
// Session Revoked meanwhile is told nothing.
func (s *Server) concerned(ctx context.Context, c hostlist.Change) ([]string, error) {
	connected := s.transport.DeviceSessions()
	var ids []string
	err := s.store.Read(ctx, func(tx store.Tx) error {
		ids = nil
		sees := map[string]bool{}
		for _, id := range connected {
			who, err := access.Who(ctx, tx, op.Caller{DeviceSession: id})
			if e, ok := errors.AsType[*api.Error](err); ok && e.Code == api.CodeRevoked.Name {
				continue
			}
			if err != nil {
				return err
			}
			u := who.User.ID
			if _, ok := sees[u]; !ok {
				if sees[u], err = concerns(ctx, tx, who, c); err != nil {
					return err
				}
			}
			if sees[u] {
				ids = append(ids, id)
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("read whom a change to the Hosts concerns: %w", err)
	}
	return ids, nil
}

// concerns tells whether the change concerns the User who calls as who.
func concerns(ctx context.Context, tx store.Tx, who access.Identity, c hostlist.Change) (bool, error) {
	if slices.Contains(c.Users, who.User.ID) {
		return true, nil
	}
	if len(c.Spaces) == 0 {
		return false, nil
	}
	scope, err := who.Spaces(ctx, tx)
	if err != nil {
		return false, err
	}
	return slices.ContainsFunc(c.Spaces, scope.Has), nil
}
