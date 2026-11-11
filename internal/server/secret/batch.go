package secret

import (
	"context"

	"github.com/cieons/koko/internal/server/access"
	"github.com/cieons/koko/internal/server/op"
	"github.com/cieons/koko/internal/server/store"
	"github.com/cieons/koko/internal/shared/api"
)

// named is the Secrets a write names: the one its space and name name, or
// the several in its secrets, a batch, applied all or none (ADR 0050).
type named struct {
	// several says the request named secrets, so the write answers secrets.
	several bool
	// secrets are the Secrets found, in the order named; one at fault is
	// the zero stored.
	secrets []stored
	batch   *op.Batch
}

// findNamed finds each Secret a request names, by spaceName and secretName
// or in several, as find finds one. It refuses a request that names both,
// or an empty secrets, with CodeInvalidRequest. A Secret it does not find,
// and one named again later in the batch, are at fault in a batch of
// several, and refuse the request at once otherwise, so one Secret is
// refused as before.
func findNamed(ctx context.Context, tx store.Tx, who access.Identity, spaceName, secretName string, several []api.SecretRef) (*named, error) {
	refs := []api.SecretRef{{Space: spaceName, Name: secretName}}
	switch {
	case several != nil && (spaceName != "" || secretName != ""):
		return nil, api.CodeInvalidRequest.Errorf("name one Secret in space and name or several in secrets, not both")
	case several != nil && len(several) == 0:
		return nil, api.CodeInvalidRequest.Errorf("name at least one Secret in secrets")
	case several != nil:
		refs = several
	}
	n := &named{several: several != nil, secrets: make([]stored, len(refs)), batch: op.NewBatch(len(refs))}
	first := map[string]int{}
	for i, ref := range refs {
		sec, err := find(ctx, tx, who, ref.Space, ref.Name)
		if j, ok := first[sec.ID]; err == nil && ok {
			err = api.CodeInvalidRequest.Errorf("the Secret [%d] of this batch is %s too", j, qualified(sec.Secret))
		}
		if err != nil {
			if err := n.batch.Fault(i, err); err != nil {
				return nil, err
			}
			continue
		}
		first[sec.ID] = i
		n.secrets[i] = sec
	}
	return n, nil
}

// each runs write on each Secret found, as op.Each does; done says what
// the write does, such as "moved", for the refusal.
func (n *named) each(done string, write func(sec stored) (api.Secret, error)) ([]api.Secret, error) {
	return op.Each(n.batch, n.secrets, "Secrets", done, write)
}
