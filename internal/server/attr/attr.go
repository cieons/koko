// Package attr is the attr area of the koko server: attr.list, the Preset
// Attributes with their types and every Attribute key in use on what the
// caller can see (P0 spec: the record). Each area whose records hold
// Attributes lists its keys through a Keys, so this area reads no other
// area's records.
package attr

import (
	"context"
	"maps"
	"slices"

	"github.com/cieons/koko/internal/server/access"
	"github.com/cieons/koko/internal/server/op"
	"github.com/cieons/koko/internal/server/store"
	"github.com/cieons/koko/internal/shared/api"
	"github.com/cieons/koko/internal/shared/record"
)

// Keys lists the Attribute keys in use on one area's records, such as its
// Hosts, in the Spaces scope holds. Each area whose records hold Attributes
// gives one, through the attrKeys list in package server.
type Keys func(ctx context.Context, tx store.Tx, scope access.Scope) ([]string, error)

// Register adds attr.list to the API; keys list the keys in use on each
// area's records.
func Register(a *op.API, keys []Keys) {
	a.Handle(api.AttrList, func(ctx context.Context, tx store.Tx, caller op.Caller, _ api.AttrListRequest) (api.AttrListResponse, error) {
		return list(ctx, tx, caller, keys)
	})
}

// list is attr.list: every Preset in the order of the Preset table, then
// every other key in use in the Spaces the caller can act in, by key. A key
// held only in a Space the caller cannot act in is answered as if nothing
// held it.
func list(ctx context.Context, tx store.Tx, caller op.Caller, keys []Keys) (api.AttrListResponse, error) {
	who, err := access.Who(ctx, tx, caller)
	if err != nil {
		return api.AttrListResponse{}, err
	}
	scope, err := who.Spaces(ctx, tx)
	if err != nil {
		return api.AttrListResponse{}, err
	}
	inUse := map[string]bool{}
	for _, k := range keys {
		held, err := k(ctx, tx, scope)
		if err != nil {
			return api.AttrListResponse{}, err
		}
		for _, key := range held {
			inUse[key] = true
		}
	}
	resp := api.AttrListResponse{Attributes: []api.AttrKey{}}
	for _, p := range record.Presets() {
		k := api.AttrKey{Key: p.Key, Type: string(p.Type.Kind()), Values: p.Type.Values(), Preset: true, InUse: inUse[p.Key]}
		if v, ok := p.Type.Min(); ok {
			k.Min = &v
		}
		resp.Attributes = append(resp.Attributes, k)
	}
	for _, key := range slices.Sorted(maps.Keys(inUse)) {
		if !record.IsPreset(key) {
			resp.Attributes = append(resp.Attributes, api.AttrKey{Key: key, Type: string(record.KindText), InUse: true})
		}
	}
	return resp, nil
}
