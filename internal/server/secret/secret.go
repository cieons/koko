// Package secret is the secret area of the koko server: named values of up
// to 64 KiB, each held in one Space and sealed under the Root Key for its own
// record (P0 spec: Secrets, ADR 0018). Whoever can act in a Space acts on
// its Secrets, and nobody else sees them; the access check decides (package
// access). Only secret.show answers with a value, and no error or log line
// holds one.
package secret

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/cieons/koko/internal/server/access"
	"github.com/cieons/koko/internal/server/op"
	"github.com/cieons/koko/internal/server/seal"
	"github.com/cieons/koko/internal/server/space"
	"github.com/cieons/koko/internal/server/store"
	"github.com/cieons/koko/internal/shared/api"
	"github.com/cieons/koko/internal/shared/name"
)

// idPrefix starts every Secret's id, as in secret_3f9k2m7q. A value is
// sealed for its Secret's id.
const idPrefix = "secret"

// area serves the secret operations.
type area struct {
	// rootKey seals and opens every value.
	rootKey *seal.RootKey
}

// Register adds the secret operations to the API; rootKey is the Root Key
// the koko server loaded, which seals and opens every value.
func Register(a *op.API, rootKey *seal.RootKey) {
	s := &area{rootKey: rootKey}
	a.Handle(api.SecretList, list)
	a.Handle(api.SecretShow, s.show)
	a.Handle(api.SecretAdd, s.add)
}

// Holding counts the Secrets the Space whose id is spaceID holds, so that
// space show counts them and space rm refuses the Space while it holds one.
func Holding(ctx context.Context, tx store.Tx, spaceID string, c *api.SpaceCounts) error {
	err := tx.QueryRowContext(ctx, "SELECT count(*) FROM secrets WHERE space_id = ?", spaceID).Scan(&c.Secrets)
	if err != nil {
		return fmt.Errorf("count the Secrets: %w", err)
	}
	return nil
}

// list is secret.list: the Secrets of one Space, or of every Space the
// caller can act in, without their values.
func list(ctx context.Context, tx store.Tx, caller op.Caller, req api.SecretListRequest) (api.SecretListResponse, error) {
	who, err := access.Who(ctx, tx, caller)
	if err != nil {
		return api.SecretListResponse{}, err
	}
	// only is the id of the one Space listed, or "" for every Space.
	var only string
	if req.Space != "" {
		sp, err := space.ByName(ctx, tx, who, req.Space)
		if err != nil {
			return api.SecretListResponse{}, err
		}
		only = sp.ID
	}
	scope, err := who.Spaces(ctx, tx)
	if err != nil {
		return api.SecretListResponse{}, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT s.id, s.space_id, sp.name, s.name, s.created_at, s.updated_at
		FROM secrets s JOIN spaces sp ON sp.id = s.space_id
		WHERE ?1 = '' OR s.space_id = ?1 ORDER BY sp.name, s.name`, only)
	if err != nil {
		return api.SecretListResponse{}, fmt.Errorf("list the Secrets: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var resp api.SecretListResponse
	for rows.Next() {
		var s api.Secret
		var spaceID string
		if err := rows.Scan(&s.ID, &spaceID, &s.Space, &s.Name, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return api.SecretListResponse{}, fmt.Errorf("list the Secrets: %w", err)
		}
		if scope.Has(spaceID) {
			resp.Secrets = append(resp.Secrets, s)
		}
	}
	if err := rows.Err(); err != nil {
		return api.SecretListResponse{}, fmt.Errorf("list the Secrets: %w", err)
	}
	return resp, nil
}

// show is secret.show: the Secret with its value, opened with the Root Key
// for its own record.
func (s *area) show(ctx context.Context, tx store.Tx, caller op.Caller, req api.SecretShowRequest) (api.SecretShowResponse, error) {
	sp, sec, err := resolveName(ctx, tx, caller, req.Space, req.Name)
	if err != nil {
		return api.SecretShowResponse{}, err
	}
	var sealed []byte
	err = tx.QueryRowContext(ctx, "SELECT id, sealed, created_at, updated_at FROM secrets WHERE space_id = ? AND name = ?",
		sp.ID, req.Name).Scan(&sec.ID, &sealed, &sec.CreatedAt, &sec.UpdatedAt)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return api.SecretShowResponse{}, api.CodeUnknownSecret.Errorf("no Secret is named %s", qualified(sec))
	case err != nil:
		return api.SecretShowResponse{}, fmt.Errorf("read the Secret %s: %w", qualified(sec), err)
	}
	// The Seal's errors never quote a value.
	value, err := s.rootKey.Open(sec.ID, sealed)
	if err != nil {
		return api.SecretShowResponse{}, api.CodeSecretUnreadable.Errorf("the Secret %s cannot be shown: %v", qualified(sec), err)
	}
	return api.SecretShowResponse{Secret: sec, Value: value}, nil
}

// add is secret.add: a new Secret in a Space, its value sealed for its own
// id.
func (s *area) add(ctx context.Context, tx store.Tx, caller op.Caller, req api.SecretAddRequest) (api.SecretAddResponse, error) {
	sp, sec, err := resolveName(ctx, tx, caller, req.Space, req.Name)
	if err != nil {
		return api.SecretAddResponse{}, err
	}
	sec.ID = store.NewID(idPrefix)
	sec.CreatedAt = op.Now(ctx).Format(time.RFC3339)
	sec.UpdatedAt = sec.CreatedAt
	if err := checkValue(sec, req.Value); err != nil {
		return api.SecretAddResponse{}, err
	}
	var taken bool
	err = tx.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM secrets WHERE space_id = ? AND name = ?)", sp.ID, req.Name).Scan(&taken)
	if err != nil {
		return api.SecretAddResponse{}, fmt.Errorf("check whether %s exists: %w", qualified(sec), err)
	}
	if taken {
		return api.SecretAddResponse{}, api.CodeNameTaken.Errorf("the Secret %s already exists", qualified(sec))
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO secrets (id, space_id, name, sealed, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)",
		sec.ID, sp.ID, sec.Name, s.rootKey.Seal(sec.ID, req.Value), sec.CreatedAt, sec.UpdatedAt)
	if err != nil {
		return api.SecretAddResponse{}, fmt.Errorf("store the Secret %s: %w", qualified(sec), err)
	}
	return api.SecretAddResponse{Secret: sec}, nil
}

// resolveName resolves the qualified name spaceName/secretName as the caller
// gives it: the Space as the caller sees it (one they cannot act in answers
// as one that does not exist), and the Secret as far as its name goes, once
// the name passes the name rule. It does not say whether the Secret exists.
func resolveName(ctx context.Context, tx store.Tx, caller op.Caller, spaceName, secretName string) (api.Space, api.Secret, error) {
	who, err := access.Who(ctx, tx, caller)
	if err != nil {
		return api.Space{}, api.Secret{}, err
	}
	sp, err := space.ByName(ctx, tx, who, spaceName)
	if err != nil {
		return api.Space{}, api.Secret{}, err
	}
	sec := api.Secret{Space: sp.Name, Name: secretName}
	if err := name.CheckSecret(secretName); err != nil {
		return api.Space{}, api.Secret{}, api.CodeInvalidName.Errorf("invalid Secret name %s: %v", qualified(sec), err)
	}
	return sp, sec, nil
}

// checkValue refuses a value for sec that is empty or over the limit, naming
// the Secret and the value's length, never the value.
func checkValue(sec api.Secret, value api.SecretValue) error {
	switch {
	case len(value) == 0:
		return api.CodeInvalidRequest.Errorf("the value of %s cannot be empty", qualified(sec))
	case len(value) > api.SecretMaxSize:
		return api.CodeValueTooLarge.Errorf("the value of %s is %d bytes, over the limit of %d bytes (64 KiB)",
			qualified(sec), len(value), api.SecretMaxSize)
	}
	return nil
}

// qualified names a Secret as <space>/<secret>, as every error does.
func qualified(sec api.Secret) string {
	return sec.Space + "/" + sec.Name
}
