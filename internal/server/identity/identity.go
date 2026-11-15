// Package identity is the identity area of the koko server: the Identity
// Provider, the one OpenID Connect provider the Owner configures, through
// which a User with a Linked Identity signs in to the web UI (ADR 0060,
// ADR 0061). It serves server.oidc.show, server.oidc.set,
// server.oidc.remove, identity.list and identity.unlink, keeps the
// provider's client secret sealed under the Root Key, and makes every call
// to the provider through one bounded client (client.go), keeping its
// discovery and keys (discovery.go). The Linked Identities' operations are
// in linked.go.
//
// It also does the work of the web front's two endpoints that sign a
// browser in, or link a User's identity, through the provider (flow.go):
// the Authorization Code flow with PKCE, each flow held in memory under
// its state and bound to the browser that began it by a cookie.
//
// A set checks a new or changed issuer's discovery before its write's
// transaction (op.HandlePrepared), so a slow provider holds up no other
// write. A change of issuer, or a removal, unlinks every Linked Identity
// and ends the Sign-ins made through them, which the signin area ends.
package identity

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/cieons/koko/internal/server/access"
	"github.com/cieons/koko/internal/server/deployment"
	"github.com/cieons/koko/internal/server/flow"
	"github.com/cieons/koko/internal/server/op"
	"github.com/cieons/koko/internal/server/seal"
	"github.com/cieons/koko/internal/server/signin"
	"github.com/cieons/koko/internal/server/store"
	"github.com/cieons/koko/internal/shared/api"
	"github.com/cieons/koko/internal/shared/timing"
)

// Service is the identity area.
type Service struct {
	store   *store.Store
	rootKey *seal.RootKey
	// signins starts and ends the Sign-ins, spends the Sign-in Links and
	// keeps the rate limits.
	signins *signin.Service
	// client makes every call to the Identity Provider; exchange is the
	// same client, through the same bounds, for the token exchange, which
	// follows no redirect: one would send its body, which holds the client
	// secret, the code and the verifier, on to wherever it points.
	client, exchange *http.Client
	discoveries      *discoveries
	// clock is the koko server's clock, which a flow's request reads its
	// time on.
	clock func() time.Time
	flows *flow.Store[oidcFlow]
	log   *slog.Logger
	// api runs a flow's outcome, as a write of the koko server's own
	// (op.API.Do); Register sets it.
	api *op.API
}

// New returns the identity area, whose records live in st, which seals the
// client secret with rootKey, starts and ends Sign-ins through signins,
// calls the provider through base's transport (nil for the system's),
// bounded as timings say (the zero Timings is the product's), reads the
// time on clock, the koko server's, and logs to log.
func New(st *store.Store, rootKey *seal.RootKey, signins *signin.Service, base *http.Client, clock func() time.Time, timings timing.Timings, log *slog.Logger) *Service {
	t := timings.Resolve()
	client := newClient(base, t.ProviderCall, t.ProviderTurn, log)
	exchange := &http.Client{
		Transport:     client.Transport,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	return &Service{
		store: st, rootKey: rootKey, signins: signins, client: client, exchange: exchange,
		discoveries: &discoveries{client: client, clock: clock}, clock: clock,
		flows: flow.New[oidcFlow](flowLifetime, clock), log: log,
	}
}

// Register adds the Identity Provider's operations to the API, which also
// runs the flows' outcomes.
func (s *Service) Register(a *op.API) {
	s.api = a
	a.Handle(api.ServerOIDCShow, s.show)
	a.HandlePrepared(api.ServerOIDCSet, s.prepareSet, s.set)
	a.Handle(api.ServerOIDCRemove, s.remove)
	a.Handle(api.IdentityList, s.list)
	a.Handle(api.IdentityUnlink, s.unlink)
}

// show is server.oidc.show, for the Owner and the Administrators.
func (s *Service) show(ctx context.Context, tx store.Tx, caller op.Caller, _ api.ServerOIDCShowRequest) (api.ServerOIDCShowResponse, error) {
	if _, err := access.Administers(ctx, tx, caller, "sees the Identity Provider"); err != nil {
		return api.ServerOIDCShowResponse{}, err
	}
	p, err := readProvider(ctx, tx, s.rootKey)
	if err != nil {
		return api.ServerOIDCShowResponse{}, err
	}
	webURL, _, err := deployment.WebURL(ctx, tx)
	if err != nil {
		return api.ServerOIDCShowResponse{}, err
	}
	var resp api.ServerOIDCShowResponse
	if p != nil {
		linked, err := countIdentities(ctx, tx)
		if err != nil {
			return api.ServerOIDCShowResponse{}, err
		}
		resp.Provider = new(p.answer(linked))
	}
	if webURL != "" {
		resp.RedirectURI = new(webURL + api.RedirectPath)
	}
	return resp, nil
}

// checked is what prepareSet checked outside the write's transaction: the
// issuer whose discovery it fetched, "" for none, and what it found.
type checked struct {
	issuer string
	found  discovery
}

// prepareSet checks server.oidc.set before its transaction: the Owner
// alone, refusing anyone else before it reads anything; the Web URL set;
// the settings; and, for a new or changed issuer, its discovery, fetched
// outside any transaction.
func (s *Service) prepareSet(ctx context.Context, read op.Read, caller op.Caller, req api.ServerOIDCSetRequest) (checked, error) {
	var current, next *provider
	err := read(func(tx store.Tx) error {
		var err error
		current, next, err = s.planSet(ctx, tx, caller, req)
		return err
	})
	if err != nil {
		return checked{}, err
	}
	if current != nil && next.issuer == current.issuer {
		return checked{}, nil
	}
	found, err := discover(ctx, s.client, next.issuer)
	if err != nil {
		return checked{}, err
	}
	return checked{issuer: next.issuer, found: found}, nil
}

// planSet checks who calls and the Web URL, and returns the Identity
// Provider as it is and as req makes it.
func (s *Service) planSet(ctx context.Context, tx store.Tx, caller op.Caller, req api.ServerOIDCSetRequest) (current, next *provider, err error) {
	if _, err := access.OwnerOnly(ctx, tx, caller, "configures the Identity Provider"); err != nil {
		return nil, nil, err
	}
	if _, err := redirectURI(ctx, tx); err != nil {
		return nil, nil, err
	}
	if current, err = readProvider(ctx, tx, s.rootKey); err != nil {
		return nil, nil, err
	}
	if next, err = merge(current, req); err != nil {
		return nil, nil, err
	}
	return current, next, nil
}

// set is server.oidc.set, once prepareSet has checked it: it checks again
// what it relies on, since the record may have changed meanwhile, then
// writes the provider under a new id, unlinking every identity when the
// issuer changes.
func (s *Service) set(ctx context.Context, tx store.Tx, caller op.Caller, req api.ServerOIDCSetRequest, c checked) (api.ServerOIDCSetResponse, error) {
	current, next, err := s.planSet(ctx, tx, caller, req)
	if err != nil {
		return api.ServerOIDCSetResponse{}, err
	}
	who, err := access.Who(ctx, tx, caller)
	if err != nil {
		return api.ServerOIDCSetResponse{}, err
	}
	if current != nil && *next == *current {
		// Nothing changes: no write, no new id, and no log line.
		linked, err := countIdentities(ctx, tx)
		if err != nil {
			return api.ServerOIDCSetResponse{}, err
		}
		redirect, err := redirectURI(ctx, tx)
		return api.ServerOIDCSetResponse{Provider: current.answer(linked), RedirectURI: redirect}, err
	}
	issuerChanged := current == nil || next.issuer != current.issuer
	if issuerChanged && c.issuer != next.issuer {
		return api.ServerOIDCSetResponse{}, api.CodeInvalidRequest.Errorf("the Identity Provider's issuer changed while this set was checked: set it again")
	}
	unlinked := 0
	if issuerChanged {
		if unlinked, err = s.unlinkAll(ctx, tx, who.By()); err != nil {
			return api.ServerOIDCSetResponse{}, err
		}
	}
	if err := writeProvider(ctx, tx, s.rootKey, next, op.Now(ctx), who.Issuer()); err != nil {
		return api.ServerOIDCSetResponse{}, err
	}
	redirect, err := redirectURI(ctx, tx)
	if err != nil {
		return api.ServerOIDCSetResponse{}, err
	}
	linked, err := countIdentities(ctx, tx)
	if err != nil {
		return api.ServerOIDCSetResponse{}, err
	}
	secretChanged := current == nil || next.secret != current.secret
	op.AfterTx(ctx, func(committed bool) {
		if !committed {
			return
		}
		if issuerChanged {
			s.discoveries.keep(next.issuer, c.found)
		}
		s.log.Info("identity provider set", "user", who.User.Name, "issuer", next.issuer, "client_id", next.clientID,
			"name", next.name, "secret_changed", secretChanged, "set_by", who.By(), "identities_unlinked", unlinked)
	})
	return api.ServerOIDCSetResponse{Provider: next.answer(linked), RedirectURI: redirect, IdentitiesUnlinked: unlinked}, nil
}

// remove is server.oidc.remove, for the Owner alone: it unlinks every
// identity and removes the provider.
func (s *Service) remove(ctx context.Context, tx store.Tx, caller op.Caller, _ api.ServerOIDCRemoveRequest) (api.ServerOIDCRemoveResponse, error) {
	who, err := access.OwnerOnly(ctx, tx, caller, "removes the Identity Provider")
	if err != nil {
		return api.ServerOIDCRemoveResponse{}, err
	}
	p, err := readProvider(ctx, tx, s.rootKey)
	if err != nil {
		return api.ServerOIDCRemoveResponse{}, err
	}
	if p == nil {
		return api.ServerOIDCRemoveResponse{}, api.CodeProviderUnset.Errorf("no Identity Provider is set, so there is none to remove")
	}
	unlinked, err := s.unlinkAll(ctx, tx, who.By())
	if err != nil {
		return api.ServerOIDCRemoveResponse{}, err
	}
	if err := deleteProvider(ctx, tx); err != nil {
		return api.ServerOIDCRemoveResponse{}, err
	}
	op.AfterTx(ctx, func(committed bool) {
		if !committed {
			return
		}
		s.discoveries.forget()
		s.log.Info("identity provider removed", "user", who.User.Name, "issuer", p.issuer, "removed_by", who.By(), "identities_unlinked", unlinked)
	})
	return api.ServerOIDCRemoveResponse{IdentitiesUnlinked: unlinked}, nil
}

// redirectURI is where the provider sends a browser back to: the Web URL
// followed by api.RedirectPath, refused with web_url_unset while the Web
// URL is not set.
func redirectURI(ctx context.Context, tx store.Tx) (string, error) {
	webURL, _, err := deployment.WebURL(ctx, tx)
	if err != nil {
		return "", err
	}
	if webURL == "" {
		return "", api.CodeWebURLUnset.Errorf("the Identity Provider sends a browser back to the Web URL, which is not set: set it with koko server set web_url=https://… on the koko server's machine")
	}
	return webURL + api.RedirectPath, nil
}

// linked is one Linked Identity, with its User.
type linked struct {
	id   string
	user api.User
}

// unlinkAll unlinks every Linked Identity, by being the caller named by:
// it ends the Sign-ins made through each (identity_unlinked) and deletes
// them, logging each once the call commits, and says how many it unlinked.
func (s *Service) unlinkAll(ctx context.Context, tx store.Tx, by string) (int, error) {
	rows, err := tx.QueryContext(ctx, "SELECT i.id, u.id, u.name FROM identities i JOIN users u ON u.id = i.user_id ORDER BY u.name")
	if err != nil {
		return 0, fmt.Errorf("read the Linked Identities: %w", err)
	}
	var all []linked
	for rows.Next() {
		var l linked
		if err := rows.Scan(&l.id, &l.user.ID, &l.user.Name); err != nil {
			_ = rows.Close()
			return 0, fmt.Errorf("read the Linked Identities: %w", err)
		}
		all = append(all, l)
	}
	if err := cmp.Or(rows.Err(), rows.Close()); err != nil {
		return 0, fmt.Errorf("read the Linked Identities: %w", err)
	}
	for _, l := range all {
		ended, err := s.signins.EndMadeWith(ctx, tx, l.user, l.id, api.EndIdentityUnlinked, by)
		if err != nil {
			return 0, err
		}
		op.AfterTx(ctx, func(committed bool) {
			if committed {
				s.log.Info("identity unlinked", "user", l.user.Name, "identity", l.id, "unlinked_by", by, "signins_ended", ended)
			}
		})
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM identities"); err != nil {
		return 0, fmt.Errorf("unlink the Linked Identities: %w", err)
	}
	return len(all), nil
}
