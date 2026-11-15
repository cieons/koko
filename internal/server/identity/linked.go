package identity

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/cieons/koko/internal/server/access"
	"github.com/cieons/koko/internal/server/op"
	"github.com/cieons/koko/internal/server/store"
	"github.com/cieons/koko/internal/server/user"
	"github.com/cieons/koko/internal/shared/api"
)

// list is identity.list: the caller's own Linked Identity, or that of the
// User req.User names, whom only the Owner and the Administrators see.
func (s *Service) list(ctx context.Context, tx store.Tx, caller op.Caller, req api.IdentityListRequest) (api.IdentityListResponse, error) {
	who, err := access.Who(ctx, tx, caller)
	if err != nil {
		return api.IdentityListResponse{}, err
	}
	of := who.User
	switch {
	case req.User == "" || req.User == who.User.Name:
	case !who.Administers():
		// Another User's identity is none of a Member's: as for a User
		// with none, whether or not the name is a User's.
		return api.IdentityListResponse{Identities: []api.Identity{}}, nil
	default:
		if of, err = user.ByName(ctx, tx, req.User); err != nil {
			return api.IdentityListResponse{}, err
		}
	}
	found, err := readIdentities(ctx, tx, "i.user_id = ?", of.ID)
	if err != nil {
		return api.IdentityListResponse{}, err
	}
	return api.IdentityListResponse{Identities: found}, nil
}

// unlink is identity.unlink: it deletes the Linked Identity of the User
// req.User names, the caller's own when it names none, and ends the
// Sign-ins made through it ("identity_unlinked"). A Member naming another
// User is refused before the name is read.
func (s *Service) unlink(ctx context.Context, tx store.Tx, caller op.Caller, req api.IdentityUnlinkRequest) (api.IdentityUnlinkResponse, error) {
	who, err := access.Who(ctx, tx, caller)
	if err != nil {
		return api.IdentityUnlinkResponse{}, err
	}
	of := who.User
	if req.User != "" && req.User != who.User.Name {
		if !who.Administers() {
			return api.IdentityUnlinkResponse{}, api.CodeOwnerOnly.Errorf("only the Owner or an Administrator unlinks another User's identity")
		}
		if of, err = user.ByName(ctx, tx, req.User); err != nil {
			return api.IdentityUnlinkResponse{}, err
		}
		if err := who.MayActOn(of, "unlinks the identity of"); err != nil {
			return api.IdentityUnlinkResponse{}, err
		}
	}
	found, err := readIdentities(ctx, tx, "i.user_id = ?", of.ID)
	if err != nil {
		return api.IdentityUnlinkResponse{}, err
	}
	if len(found) == 0 {
		return api.IdentityUnlinkResponse{}, api.CodeNoIdentity.Errorf("%s has no Linked Identity", of.Name)
	}
	i := found[0]
	if _, err := tx.ExecContext(ctx, "DELETE FROM identities WHERE id = ?", i.ID); err != nil {
		return api.IdentityUnlinkResponse{}, fmt.Errorf("unlink the identity %s: %w", i.ID, err)
	}
	ended, err := s.signins.EndMadeWith(ctx, tx, of, i.ID, api.EndIdentityUnlinked, who.By())
	if err != nil {
		return api.IdentityUnlinkResponse{}, err
	}
	op.AfterTx(ctx, func(committed bool) {
		if committed {
			s.log.Info("identity unlinked", "user", of.Name, "identity", i.ID, "unlinked_by", who.By(), "signins_ended", ended)
		}
	})
	return api.IdentityUnlinkResponse{Identity: i, SignInsEnded: int(ended)}, nil
}

// readIdentities returns the Linked Identities the SQL condition where
// selects with args, as the API answers them, in the order they were
// linked. The condition reads the identity as i. An identity's linked_by is
// the User who issued the Sign-in Link it was linked through, the Owner for
// a link the Unix socket issued; none for one linked from a Sign-in.
func readIdentities(ctx context.Context, tx store.Tx, where string, args ...any) ([]api.Identity, error) {
	rows, err := tx.QueryContext(ctx, `SELECT i.id, u.name, i.issuer, i.subject, i.label, i.linked_at, i.last_used_at,
			CASE WHEN l.id IS NULL THEN NULL
				WHEN l.issued_by IS NULL THEN (SELECT name FROM users WHERE role = 'owner')
				ELSE iu.name END
		FROM identities i JOIN users u ON u.id = i.user_id
		LEFT JOIN password_links l ON l.id = i.linked_with
		LEFT JOIN (SELECT id, user_id FROM device_sessions UNION ALL SELECT id, user_id FROM signins) b ON b.id = l.issued_by
		LEFT JOIN users iu ON iu.id = b.user_id
		WHERE `+where+` ORDER BY i.linked_at, i.rowid`, args...)
	if err != nil {
		return nil, fmt.Errorf("read the Linked Identities: %w", err)
	}
	defer func() { _ = rows.Close() }()
	found := []api.Identity{}
	for rows.Next() {
		var i api.Identity
		var lastUsed, linkedBy sql.NullString
		if err := rows.Scan(&i.ID, &i.User, &i.Issuer, &i.Subject, &i.Label, &i.LinkedAt, &lastUsed, &linkedBy); err != nil {
			return nil, fmt.Errorf("read the Linked Identities: %w", err)
		}
		if lastUsed.Valid {
			i.LastUsedAt = new(lastUsed.String)
		}
		if linkedBy.Valid {
			i.LinkedBy = new(linkedBy.String)
		}
		found = append(found, i)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read the Linked Identities: %w", err)
	}
	return found, nil
}
