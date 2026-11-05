// Package user is the user area of the koko server: the Users, of whom
// exactly one is the Owner and every other a Member, each with a Personal
// Space (P0 spec: people and Spaces).
package user

import (
	"context"
	"fmt"
	"time"

	"github.com/cieons/koko/internal/server/space"
	"github.com/cieons/koko/internal/server/store"
	"github.com/cieons/koko/internal/shared/api"
	"github.com/cieons/koko/internal/shared/name"
)

// idPrefix starts every User's id, as in user_3f9k2m7q.
const idPrefix = "user"

// AddOwner records the Owner, named owner, with their Personal Space, which
// takes the same name. The name is checked as a User name and has no
// default, because a User's name never changes. Initialisation calls it once,
// on a koko server that holds no User and no Space yet.
func AddOwner(ctx context.Context, tx store.Tx, owner string) error {
	if err := name.Check(owner); err != nil {
		return api.CodeInvalidName.Errorf("invalid Owner name: %v", err)
	}
	id := store.NewID(idPrefix)
	created := time.Now().UTC().Format(time.RFC3339)
	_, err := tx.ExecContext(ctx, "INSERT INTO users (id, name, role, created_at) VALUES (?, ?, 'owner', ?)", id, owner, created)
	if err != nil {
		return fmt.Errorf("record the Owner %q: %w", owner, err)
	}
	return space.AddPersonal(ctx, tx, id, owner)
}
