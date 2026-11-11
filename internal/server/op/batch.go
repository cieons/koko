package op

import (
	"cmp"
	"errors"
	"slices"

	"github.com/cieons/koko/internal/shared/api"
)

// Batch gathers the items at fault of a write that names several records
// and applies them all or none (ADR 0030, ADR 0050): the operation checks
// every item, records each one at fault, and refuses the whole batch when
// any is, which rolls back what it applied before.
type Batch struct {
	n       int
	faults  []api.ItemError
	faulted map[int]bool
}

// NewBatch begins a batch of n records.
func NewBatch(n int) *Batch {
	return &Batch{n: n, faulted: map[int]bool{}}
}

// Fault takes err, the failure of the item at index i, if any. In a batch
// of several, an *api.Error makes the item one at fault and Fault returns
// nil, so the operation goes on to the next item. It returns any other
// error, which ends the call, and the error of the one item of a batch of
// one, which is refused with its own error and no items.
func (b *Batch) Fault(i int, err error) error {
	if e, ok := errors.AsType[*api.Error](err); ok && b.n > 1 {
		b.faults = append(b.faults, api.ItemError{Index: i, Code: e.Code, Message: e.Message})
		b.faulted[i] = true
		return nil
	}
	return err
}

// Faulted tells whether the item at index i is at fault.
func (b *Batch) Faulted(i int) bool {
	return b.faulted[i]
}

// Each runs write on each item of a batch b, in order, skipping those at
// fault already, inside the call's transaction, and returns what write
// answered of each. An item write refuses is at fault in a batch of
// several, and every other item is still checked; then the batch is
// refused, listing each item at fault, as Err does, and returning the
// error rolls back what the others wrote.
func Each[T, R any](b *Batch, items []T, records, done string, write func(T) (R, error)) ([]R, error) {
	answered := make([]R, 0, len(items))
	for i, it := range items {
		if b.Faulted(i) {
			continue
		}
		a, err := write(it)
		if err != nil {
			if err := b.Fault(i, err); err != nil {
				return nil, err
			}
			continue
		}
		answered = append(answered, a)
	}
	if err := b.Err(records, done); err != nil {
		return nil, err
	}
	return answered, nil
}

// Err returns the refusal of the batch, listing every item at fault in the
// batch's order, or nil when none is. records names the records, such as
// "Hosts", and done what the batch would have done, such as "moved".
func (b *Batch) Err(records, done string) error {
	if len(b.faults) == 0 {
		return nil
	}
	slices.SortStableFunc(b.faults, func(x, y api.ItemError) int { return cmp.Compare(x.Index, y.Index) })
	return api.BatchError(b.n, records, done, b.faults)
}
