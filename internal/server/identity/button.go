package identity

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/cieons/koko/internal/server/store"
	"github.com/cieons/koko/internal/shared/api"
)

// Button returns the Identity Provider as Sign in shows it, by the name on
// its button, or nil while none is configured: what /auth/methods answers
// any page, before anyone signs in.
func Button(ctx context.Context, tx store.Tx) (*api.AuthProvider, error) {
	var name string
	err := tx.QueryRowContext(ctx, "SELECT name FROM identity_provider").Scan(&name)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil, nil
	case err != nil:
		return nil, fmt.Errorf("read the Identity Provider: %w", err)
	}
	return &api.AuthProvider{Name: name}, nil
}
