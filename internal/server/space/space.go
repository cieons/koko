// Package space is the space area of the koko server: the space operations
// over the spaces table.
package space

import (
	"context"

	"github.com/cieons/koko/internal/server/op"
	"github.com/cieons/koko/internal/server/store"
	"github.com/cieons/koko/internal/shared/api"
	"github.com/cieons/koko/internal/shared/name"
)

// idPrefix starts every Space's id, as in space_3f9k2m7q.
const idPrefix = "space"

// Register adds the space operations to the API.
func Register(a *op.API) {
	a.Handle(api.SpaceAdd, add)
	a.Handle(api.SpaceList, list)
}

func add(ctx context.Context, tx store.Tx, _ op.Caller, req api.SpaceAddRequest) (api.SpaceAddResponse, error) {
	if err := name.Check(req.Name); err != nil {
		return api.SpaceAddResponse{}, api.CodeInvalidName.Errorf("invalid Space name: %v", err)
	}
	var taken bool
	err := tx.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM spaces WHERE name = ?)", req.Name).Scan(&taken)
	if err != nil {
		return api.SpaceAddResponse{}, err
	}
	if taken {
		return api.SpaceAddResponse{}, api.CodeNameTaken.Errorf("a Space named %q already exists", req.Name)
	}
	s := api.Space{ID: store.NewID(idPrefix), Name: req.Name}
	if _, err := tx.ExecContext(ctx, "INSERT INTO spaces (id, name) VALUES (?, ?)", s.ID, s.Name); err != nil {
		return api.SpaceAddResponse{}, err
	}
	return api.SpaceAddResponse{Space: s}, nil
}

func list(ctx context.Context, tx store.Tx, _ op.Caller, _ api.SpaceListRequest) (api.SpaceListResponse, error) {
	rows, err := tx.QueryContext(ctx, "SELECT id, name FROM spaces ORDER BY name")
	if err != nil {
		return api.SpaceListResponse{}, err
	}
	defer func() { _ = rows.Close() }()
	var resp api.SpaceListResponse
	for rows.Next() {
		var s api.Space
		if err := rows.Scan(&s.ID, &s.Name); err != nil {
			return api.SpaceListResponse{}, err
		}
		resp.Spaces = append(resp.Spaces, s)
	}
	return resp, rows.Err()
}
