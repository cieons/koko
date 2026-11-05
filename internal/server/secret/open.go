package secret

import (
	"context"

	"github.com/cieons/koko/internal/server/op"
	"github.com/cieons/koko/internal/server/seal"
	"github.com/cieons/koko/internal/server/store"
	"github.com/cieons/koko/internal/shared/api"
)

// Open is Enroll's read of the Secret ref for koko host enroll --secret:
// secret.show without the API, so caller gets the same access check and
// refusals, and the value, opened with rootKey, never leaves the koko server.
func Open(ctx context.Context, tx store.Tx, caller op.Caller, rootKey *seal.RootKey, ref api.SecretRef) (api.SecretValue, error) {
	shown, err := (&area{rootKey: rootKey}).show(ctx, tx, caller, api.SecretShowRequest(ref))
	if err != nil {
		return nil, err
	}
	return shown.Value, nil
}
