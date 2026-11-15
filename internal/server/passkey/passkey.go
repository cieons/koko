// Package passkey is the passkey area of the koko server: the Passkeys a
// User's devices and password managers make for the Web URL's host, each of
// which signs them in to the web UI there in place of a Password (ADR 0060).
// It serves passkey.list and passkey.remove, and the work of the web front's
// passkey endpoints: the two ceremonies, making a passkey and signing in
// with one (ceremony.go), each begun and answered in two requests through a
// flow held in memory. koko's own verifier (package webauthn) checks each
// answer; the signin area starts and ends the Sign-ins, spends the Sign-in
// Links and keeps the rate limits.
package passkey

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/cieons/koko/internal/server/access"
	"github.com/cieons/koko/internal/server/flow"
	"github.com/cieons/koko/internal/server/op"
	"github.com/cieons/koko/internal/server/signin"
	"github.com/cieons/koko/internal/server/store"
	"github.com/cieons/koko/internal/server/user"
	"github.com/cieons/koko/internal/shared/api"
)

// idPrefix starts every Passkey's id, as in passkey_3f9k2m7q.
const idPrefix = "passkey"

// flowLifetime is how long a ceremony's flow lasts, as long as the browser
// waits for the person (api.PasskeyTimeout).
const flowLifetime = api.PasskeyTimeout * time.Millisecond

// Service is the passkey area: its operations and the ceremonies.
type Service struct {
	store   *store.Store
	signins *signin.Service
	// clock is the koko server's clock, which a ceremony's request reads its
	// time on.
	clock func() time.Time
	log   *slog.Logger
	flows *flow.Store[ceremony]
	// api runs the ceremonies' writes, as writes of the koko server's own
	// (op.API.Do); Register sets it.
	api *op.API
}

// New returns the passkey area, whose records live in st, whose Sign-ins
// signins starts and ends, which reads the time of each request on clock
// and logs to log.
func New(st *store.Store, signins *signin.Service, clock func() time.Time, log *slog.Logger) *Service {
	return &Service{store: st, signins: signins, clock: clock, log: log, flows: flow.New[ceremony](flowLifetime, clock)}
}

// Register adds the passkey operations to the API, which also runs the
// ceremonies' writes.
func (s *Service) Register(a *op.API) {
	s.api = a
	a.Handle(api.PasskeyList, s.list)
	a.Handle(api.PasskeyRemove, s.remove)
}

// list is passkey.list: the caller's own Passkeys, or the User req.User
// names, whom only the Owner and the Administrators see.
func (s *Service) list(ctx context.Context, tx store.Tx, caller op.Caller, req api.PasskeyListRequest) (api.PasskeyListResponse, error) {
	who, err := access.Who(ctx, tx, caller)
	if err != nil {
		return api.PasskeyListResponse{}, err
	}
	of := who.User
	switch {
	case req.User == "" || req.User == who.User.Name:
	case !who.Administers():
		// Another User's Passkeys are none of a Member's: as for a User with
		// none, whether or not the name is a User's.
		return api.PasskeyListResponse{Passkeys: []api.Passkey{}}, nil
	default:
		if of, err = user.ByName(ctx, tx, req.User); err != nil {
			return api.PasskeyListResponse{}, err
		}
	}
	found, err := read(ctx, tx, "p.user_id = ?", of.ID)
	if err != nil {
		return api.PasskeyListResponse{}, err
	}
	return api.PasskeyListResponse{Passkeys: found}, nil
}

// remove is passkey.remove: it deletes the Passkey req.ID names, the
// caller's own or, for the Owner or an Administrator, anyone's whom they may
// act on, and ends the Sign-ins made with it ("passkey_removed"). Another
// User's, for a Member, is refused as an id no Passkey holds.
func (s *Service) remove(ctx context.Context, tx store.Tx, caller op.Caller, req api.PasskeyRemoveRequest) (api.PasskeyRemoveResponse, error) {
	who, err := access.Who(ctx, tx, caller)
	if err != nil {
		return api.PasskeyRemoveResponse{}, err
	}
	found, err := read(ctx, tx, "p.id = ?1 AND (?2 OR p.user_id = ?3)", req.ID, who.Administers(), who.User.ID)
	if err != nil {
		return api.PasskeyRemoveResponse{}, err
	}
	if len(found) == 0 {
		return api.PasskeyRemoveResponse{}, api.CodeUnknownPasskey.Errorf("no passkey is %q", req.ID)
	}
	p := found[0]
	of, err := user.ByName(ctx, tx, p.User)
	if err != nil {
		return api.PasskeyRemoveResponse{}, err
	}
	if err := who.MayActOn(of, "removes the passkeys of"); err != nil {
		return api.PasskeyRemoveResponse{}, err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM passkeys WHERE id = ?", p.ID); err != nil {
		return api.PasskeyRemoveResponse{}, fmt.Errorf("remove the passkey %s: %w", p.ID, err)
	}
	ended, err := s.signins.EndMadeWith(ctx, tx, of, p.ID, api.EndPasskeyRemoved, who.By())
	if err != nil {
		return api.PasskeyRemoveResponse{}, err
	}
	afterCommit(ctx, s.log, "passkey removed", "user", of.Name, "passkey", p.ID, "name", p.Name,
		"removed_by", who.By(), "signins_ended", ended)
	return api.PasskeyRemoveResponse{Passkey: p, SignInsEnded: int(ended)}, nil
}

// read returns the Passkeys the SQL condition where selects with args, as
// the API answers them, in the order they were made. The condition reads the
// Passkey as p. A Passkey's added_by is the User who issued the Sign-in Link
// it was made through, the Owner for a link the Unix socket issued; none for
// one made from a Sign-in.
func read(ctx context.Context, tx store.Tx, where string, args ...any) ([]api.Passkey, error) {
	rows, err := tx.QueryContext(ctx, `SELECT p.id, u.name, p.name, p.rp_id, p.backed_up, p.added_at, p.last_used_at,
			CASE WHEN l.id IS NULL THEN NULL
				WHEN l.issued_by IS NULL THEN (SELECT name FROM users WHERE role = 'owner')
				ELSE iu.name END
		FROM passkeys p JOIN users u ON u.id = p.user_id
		LEFT JOIN password_links l ON l.id = p.added_with
		LEFT JOIN (SELECT id, user_id FROM device_sessions UNION ALL SELECT id, user_id FROM signins) i ON i.id = l.issued_by
		LEFT JOIN users iu ON iu.id = i.user_id
		WHERE `+where+` ORDER BY p.added_at, p.rowid`, args...)
	if err != nil {
		return nil, fmt.Errorf("read passkeys: %w", err)
	}
	defer func() { _ = rows.Close() }()
	found := []api.Passkey{}
	for rows.Next() {
		var p api.Passkey
		var lastUsed, addedBy sql.NullString
		if err := rows.Scan(&p.ID, &p.User, &p.Name, &p.Host, &p.Synced, &p.AddedAt, &lastUsed, &addedBy); err != nil {
			return nil, fmt.Errorf("read passkeys: %w", err)
		}
		if lastUsed.Valid {
			p.LastUsedAt = new(lastUsed.String)
		}
		if addedBy.Valid {
			p.AddedBy = new(addedBy.String)
		}
		found = append(found, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read passkeys: %w", err)
	}
	return found, nil
}

// OtherHost counts the Passkeys made for a host other than host, the Web
// URL's, or every Passkey for "": each is refused until the Web URL names
// its host again.
func OtherHost(ctx context.Context, tx store.Tx, host string) (int, error) {
	var n int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM passkeys WHERE rp_id != ?", host).Scan(&n); err != nil {
		return 0, fmt.Errorf("count the passkeys for another host: %w", err)
	}
	return n, nil
}

// afterCommit logs msg with attrs to log once the call's transaction has
// committed, and never when it rolls back.
func afterCommit(ctx context.Context, log *slog.Logger, msg string, attrs ...any) {
	op.AfterTx(ctx, func(committed bool) {
		if committed {
			log.Info(msg, attrs...)
		}
	})
}
