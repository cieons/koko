package host

import (
	"context"

	"github.com/cieons/koko/internal/server/access"
	"github.com/cieons/koko/internal/server/op"
	"github.com/cieons/koko/internal/server/store"
	"github.com/cieons/koko/internal/shared/api"
)

// named is the Hosts a write names: the one in its host, or the several in
// its hosts, a batch, applied all or none (ADR 0050).
type named struct {
	// several says the request named hosts, so the write answers hosts.
	several bool
	// hosts are the Hosts found, in the order named; one at fault is the
	// zero stored.
	hosts []stored
	batch *op.Batch
}

// findNamed finds each Host a request names, in one of host and hosts, as
// find finds one. It refuses a request that names both, or an empty hosts,
// with CodeInvalidRequest. A Host it does not find, and a Host named again
// later in the batch, are at fault in a batch of several, and refuse the
// request at once otherwise, so one Host is refused as before.
func findNamed(ctx context.Context, tx store.Tx, who access.Identity, one string, several []string) (*named, error) {
	refs := []string{one}
	switch {
	case several != nil && one != "":
		return nil, api.CodeInvalidRequest.Errorf("name one Host in host or several in hosts, not both")
	case several != nil && len(several) == 0:
		return nil, api.CodeInvalidRequest.Errorf("name at least one Host in hosts")
	case several != nil:
		refs = several
	}
	n := &named{several: several != nil, hosts: make([]stored, len(refs)), batch: op.NewBatch(len(refs))}
	first := map[string]int{}
	for i, ref := range refs {
		h, err := find(ctx, tx, who, ref)
		if j, ok := first[h.ID]; err == nil && ok {
			err = api.CodeInvalidRequest.Errorf("the Host [%d] of this batch is %s (%s) too", j, h.Name, h.ID)
		}
		if err != nil {
			if err := n.batch.Fault(i, err); err != nil {
				return nil, err
			}
			continue
		}
		first[h.ID] = i
		n.hosts[i] = h
	}
	return n, nil
}

// ids are the ids of the Hosts found.
func (n *named) ids() []string {
	var ids []string
	for i, h := range n.hosts {
		if !n.batch.Faulted(i) {
			ids = append(ids, h.ID)
		}
	}
	return ids
}

// each runs write on each Host found, in the order named, inside the call's
// transaction, and returns what write answered of each. A Host write
// refuses is at fault in a batch of several, and every other Host is still
// checked; then the batch is refused, listing each Host at fault, and
// returning the error rolls back what the others wrote. done says what the
// write does, such as "moved", for the refusal.
func (n *named) each(done string, write func(h stored) (api.Host, error)) ([]api.Host, error) {
	answered := make([]api.Host, 0, len(n.hosts))
	for i, h := range n.hosts {
		if n.batch.Faulted(i) {
			continue
		}
		a, err := write(h)
		if err != nil {
			if err := n.batch.Fault(i, err); err != nil {
				return nil, err
			}
			continue
		}
		answered = append(answered, a)
	}
	if err := n.batch.Err("Hosts", done); err != nil {
		return nil, err
	}
	return answered, nil
}
