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
	"strings"
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
	a.Handle(api.SecretSet, s.set)
	a.Handle(api.SecretRename, rename)
	a.Handle(api.SecretMove, move)
	a.Handle(api.SecretRemove, remove)
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
	_, sec, err := target(ctx, tx, caller, req.Space, req.Name)
	if err != nil {
		return api.SecretShowResponse{}, err
	}
	// The Seal's errors never quote a value.
	value, err := s.rootKey.Open(sec.ID, sec.sealed)
	if err != nil {
		return api.SecretShowResponse{}, api.CodeSecretUnreadable.Errorf("the Secret %s cannot be shown: %v", qualified(sec.Secret), err)
	}
	return api.SecretShowResponse{Secret: sec.Secret, Value: value}, nil
}

// add is secret.add: a new Secret in a Space, its value sealed for its own
// id.
func (s *area) add(ctx context.Context, tx store.Tx, caller op.Caller, req api.SecretAddRequest) (api.SecretAddResponse, error) {
	who, err := access.Who(ctx, tx, caller)
	if err != nil {
		return api.SecretAddResponse{}, err
	}
	sp, sec, err := resolveName(ctx, tx, who, req.Space, req.Name)
	if err != nil {
		return api.SecretAddResponse{}, err
	}
	sec.ID = store.NewID(idPrefix)
	sec.CreatedAt = op.Now(ctx).Format(time.RFC3339)
	sec.UpdatedAt = sec.CreatedAt
	if err := checkValue(sec, req.Value); err != nil {
		return api.SecretAddResponse{}, err
	}
	if err := checkFree(ctx, tx, sp.ID, sec); err != nil {
		return api.SecretAddResponse{}, err
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO secrets (id, space_id, name, sealed, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)",
		sec.ID, sp.ID, sec.Name, s.rootKey.Seal(sec.ID, req.Value), sec.CreatedAt, sec.UpdatedAt)
	if err != nil {
		return api.SecretAddResponse{}, fmt.Errorf("store the Secret %s: %w", qualified(sec), err)
	}
	return api.SecretAddResponse{Secret: sec}, nil
}

// set is secret.set: the Secret takes a new value, sealed afresh for its
// id. P0 keeps no revision, so the last call wins.
func (s *area) set(ctx context.Context, tx store.Tx, caller op.Caller, req api.SecretSetRequest) (api.SecretSetResponse, error) {
	_, sec, err := target(ctx, tx, caller, req.Space, req.Name)
	if err != nil {
		return api.SecretSetResponse{}, err
	}
	if err := checkValue(sec.Secret, req.Value); err != nil {
		return api.SecretSetResponse{}, err
	}
	sec.UpdatedAt = op.Now(ctx).Format(time.RFC3339)
	_, err = tx.ExecContext(ctx, "UPDATE secrets SET sealed = ?, updated_at = ? WHERE id = ?",
		s.rootKey.Seal(sec.ID, req.Value), sec.UpdatedAt, sec.ID)
	if err != nil {
		return api.SecretSetResponse{}, fmt.Errorf("set the value of the Secret %s: %w", qualified(sec.Secret), err)
	}
	return api.SecretSetResponse{Secret: sec.Secret}, nil
}

// rename is secret.rename: the Secret takes a new name within its Space.
// Its value is sealed for its id, which stays, so it is not resealed.
func rename(ctx context.Context, tx store.Tx, caller op.Caller, req api.SecretRenameRequest) (api.SecretRenameResponse, error) {
	_, sec, err := target(ctx, tx, caller, req.Space, req.Name)
	if err != nil {
		return api.SecretRenameResponse{}, err
	}
	renamed := sec.Secret
	renamed.Name = req.NewName
	if err := checkName(renamed); err != nil {
		return api.SecretRenameResponse{}, err
	}
	if err := checkFree(ctx, tx, sec.spaceID, renamed); err != nil {
		return api.SecretRenameResponse{}, err
	}
	renamed.UpdatedAt = op.Now(ctx).Format(time.RFC3339)
	_, err = tx.ExecContext(ctx, "UPDATE secrets SET name = ?, updated_at = ? WHERE id = ?", renamed.Name, renamed.UpdatedAt, sec.ID)
	if err != nil {
		return api.SecretRenameResponse{}, fmt.Errorf("rename the Secret %s to %s: %w", qualified(sec.Secret), qualified(renamed), err)
	}
	return api.SecretRenameResponse{Secret: renamed}, nil
}

// move is secret.move: the Secret, or each Secret of a batch, goes to
// another Space, keeping its name. The caller must be able to act in both
// Spaces. Its value is sealed for its id, which stays, so it is not
// resealed. Two Secrets of a batch under one name that would land in the
// Space together are each at fault, as a name the Space holds is.
func move(ctx context.Context, tx store.Tx, caller op.Caller, req api.SecretMoveRequest) (api.SecretMoveResponse, error) {
	who, err := access.Who(ctx, tx, caller)
	if err != nil {
		return api.SecretMoveResponse{}, err
	}
	n, err := findNamed(ctx, tx, who, req.Space, req.Name, req.Secrets)
	if err != nil {
		return api.SecretMoveResponse{}, err
	}
	to, err := space.ByName(ctx, tx, who, req.NewSpace)
	if err != nil {
		return api.SecretMoveResponse{}, err
	}
	landing := map[string][]string{}
	for i, sec := range n.secrets {
		if !n.batch.Faulted(i) && sec.spaceID != to.ID {
			landing[sec.Name] = append(landing[sec.Name], fmt.Sprintf("[%d]", i))
		}
	}
	secrets, err := n.each("moved", func(sec stored) (api.Secret, error) {
		if sec.spaceID == to.ID {
			return sec.Secret, nil
		}
		moved := sec.Secret
		moved.Space = to.Name
		if same := landing[sec.Name]; len(same) > 1 {
			return api.Secret{}, api.CodeNameTaken.Errorf("the Secrets %s of this batch would all be %s, and a Space holds one Secret of a name",
				strings.Join(same, ", "), qualified(moved))
		}
		if err := checkFree(ctx, tx, to.ID, moved); err != nil {
			return api.Secret{}, err
		}
		moved.UpdatedAt = op.Now(ctx).Format(time.RFC3339)
		_, err = tx.ExecContext(ctx, "UPDATE secrets SET space_id = ?, updated_at = ? WHERE id = ?", to.ID, moved.UpdatedAt, sec.ID)
		if err != nil {
			return api.Secret{}, fmt.Errorf("move the Secret %s to %s: %w", qualified(sec.Secret), qualified(moved), err)
		}
		return moved, nil
	})
	switch {
	case err != nil:
		return api.SecretMoveResponse{}, err
	case n.several:
		return api.SecretMoveResponse{Secrets: secrets}, nil
	}
	return api.SecretMoveResponse{Secret: secrets[0]}, nil
}

// remove is secret.remove: the Secret, or each Secret of a batch, goes, and
// its value with it. P0 keeps no value history, so nothing brings it back.
func remove(ctx context.Context, tx store.Tx, caller op.Caller, req api.SecretRemoveRequest) (api.SecretRemoveResponse, error) {
	who, err := access.Who(ctx, tx, caller)
	if err != nil {
		return api.SecretRemoveResponse{}, err
	}
	n, err := findNamed(ctx, tx, who, req.Space, req.Name, req.Secrets)
	if err != nil {
		return api.SecretRemoveResponse{}, err
	}
	secrets, err := n.each("removed", func(sec stored) (api.Secret, error) {
		if _, err := tx.ExecContext(ctx, "DELETE FROM secrets WHERE id = ?", sec.ID); err != nil {
			return api.Secret{}, fmt.Errorf("delete the Secret %s: %w", qualified(sec.Secret), err)
		}
		return sec.Secret, nil
	})
	switch {
	case err != nil:
		return api.SecretRemoveResponse{}, err
	case n.several:
		return api.SecretRemoveResponse{Secrets: secrets}, nil
	}
	return api.SecretRemoveResponse{Secret: secrets[0]}, nil
}

// resolveName resolves the qualified name spaceName/secretName as who gives
// it: the Space as they see it (one they cannot act in answers as one that
// does not exist), and the Secret as far as its name goes, once the name
// passes the name rule. It does not say whether the Secret exists.
func resolveName(ctx context.Context, tx store.Tx, who access.Identity, spaceName, secretName string) (api.Space, api.Secret, error) {
	sp, err := space.ByName(ctx, tx, who, spaceName)
	if err != nil {
		return api.Space{}, api.Secret{}, err
	}
	sec := api.Secret{Space: sp.Name, Name: secretName}
	if err := checkName(sec); err != nil {
		return api.Space{}, api.Secret{}, err
	}
	return sp, sec, nil
}

// stored is a Secret its Space holds, as its row has it.
type stored struct {
	api.Secret
	// spaceID is the id of the Space that holds it.
	spaceID string
	// sealed is its value, sealed for its id.
	sealed []byte
}

// target returns who is calling and the Secret spaceName/secretName as they
// see it, through resolveName: what every operation on an existing Secret
// starts with. A name the Space does not hold is refused with
// CodeUnknownSecret.
func target(ctx context.Context, tx store.Tx, caller op.Caller, spaceName, secretName string) (access.Identity, stored, error) {
	who, err := access.Who(ctx, tx, caller)
	if err != nil {
		return access.Identity{}, stored{}, err
	}
	st, err := find(ctx, tx, who, spaceName, secretName)
	return who, st, err
}

// find returns the Secret spaceName/secretName as who sees it, as target
// does.
func find(ctx context.Context, tx store.Tx, who access.Identity, spaceName, secretName string) (stored, error) {
	sp, sec, err := resolveName(ctx, tx, who, spaceName, secretName)
	if err != nil {
		return stored{}, err
	}
	st := stored{Secret: sec, spaceID: sp.ID}
	err = tx.QueryRowContext(ctx, "SELECT id, sealed, created_at, updated_at FROM secrets WHERE space_id = ? AND name = ?",
		sp.ID, secretName).Scan(&st.ID, &st.sealed, &st.CreatedAt, &st.UpdatedAt)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return stored{}, api.CodeUnknownSecret.Errorf("no Secret is named %s", qualified(sec))
	case err != nil:
		return stored{}, fmt.Errorf("read the Secret %s: %w", qualified(sec), err)
	}
	return st, nil
}

// checkName refuses sec's name unless it passes the name rule, naming sec.
func checkName(sec api.Secret) error {
	if err := name.CheckSecret(sec.Name); err != nil {
		return api.CodeInvalidName.Errorf("invalid Secret name %s: %v", qualified(sec), err)
	}
	return nil
}

// checkFree refuses sec's name when the Space whose id is spaceID holds it
// already, naming sec as it would be called there.
func checkFree(ctx context.Context, tx store.Tx, spaceID string, sec api.Secret) error {
	var taken bool
	err := tx.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM secrets WHERE space_id = ? AND name = ?)", spaceID, sec.Name).Scan(&taken)
	if err != nil {
		return fmt.Errorf("check whether %s exists: %w", qualified(sec), err)
	}
	if taken {
		return api.CodeNameTaken.Errorf("the Secret %s already exists", qualified(sec))
	}
	return nil
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
