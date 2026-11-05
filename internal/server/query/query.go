// Package query filters and sorts the records a list returns (P0 spec: the
// record), fields and Attributes alike: a list names its fields and their
// types, and every other key is an Attribute key, of its Preset's type or
// free text. Each comparison is the key's type's (package record), so the
// koko server and the CLI never disagree on one.
package query

import (
	"slices"

	"github.com/cieons/koko/internal/shared/api"
	"github.com/cieons/koko/internal/shared/record"
)

// Lookup returns a record's value of key, in the form koko stores
// (record.Value.String), and whether the record has one.
type Lookup func(key string) (string, bool)

// Query is a list's filter terms and sort keys, checked.
type Query struct {
	terms []term
	sort  []sortKey
}

type term struct {
	key, op string
	typ     record.Type
	// value is what the term compares with, in the form koko stores.
	value string
}

type sortKey struct {
	key  string
	typ  record.Type
	desc bool
}

// New checks a list's filter terms and sort keys against the keys it takes:
// its fields, by key with their types, and the Attribute keys. list names
// the list for messages, such as host.list. A term or sort key that is
// malformed or names a key the list does not take is refused with
// CodeInvalidRequest, and a value that is not of its key's type, such as a
// relative date, with CodeInvalidValue.
func New(list string, fields map[string]record.Type, filters []api.Filter, sort []api.SortKey) (Query, error) {
	var q Query
	for _, f := range filters {
		typ, err := keyType(list, fields, f.Key)
		if err != nil {
			return Query{}, err
		}
		t := term{key: f.Key, op: f.Op, typ: typ}
		switch f.Op {
		case api.FilterHas, api.FilterLacks:
			if f.Value != nil {
				return Query{}, api.CodeInvalidRequest.Errorf("the term %s %s takes no value", f.Key, f.Op)
			}
		case api.FilterEq, api.FilterNe, api.FilterLt, api.FilterLe, api.FilterGt, api.FilterGe:
			if f.Value == nil {
				return Query{}, api.CodeInvalidRequest.Errorf("the term %s %s needs a value", f.Key, f.Op)
			}
			v, err := typ.Check(*f.Value)
			if err != nil {
				return Query{}, api.CodeInvalidValue.Errorf("the term %s %s: %v", f.Key, f.Op, err)
			}
			t.value = v.String()
		default:
			return Query{}, api.CodeInvalidRequest.Errorf("the term on %s has the operator %q, which is none of eq, ne, lt, le, gt, ge, has and lacks", f.Key, f.Op)
		}
		q.terms = append(q.terms, t)
	}
	for _, s := range sort {
		typ, err := keyType(list, fields, s.Key)
		if err != nil {
			return Query{}, err
		}
		if slices.ContainsFunc(q.sort, func(k sortKey) bool { return k.key == s.Key }) {
			return Query{}, api.CodeInvalidRequest.Errorf("%s sorts by %s twice", list, s.Key)
		}
		q.sort = append(q.sort, sortKey{key: s.Key, typ: typ, desc: s.Desc})
	}
	return q, nil
}

// keyType returns the type of key's values: the field's, or an Attribute
// key's. A field the list does not take, such as the Note, is refused.
func keyType(list string, fields map[string]record.Type, key string) (record.Type, error) {
	t, err := record.KeyType(fields, key, list)
	if err != nil {
		return record.Type{}, api.CodeInvalidRequest.Errorf("%v", err)
	}
	return t, nil
}

// Apply returns the items that match every term, sorted by the sort keys in
// turn; items that tie keep the order they came in. lookup gives an item's
// values.
func (q Query) Apply[T any](items []T, lookup func(T) Lookup) []T {
	var out []T
	for _, item := range items {
		if q.match(lookup(item)) {
			out = append(out, item)
		}
	}
	slices.SortStableFunc(out, func(a, b T) int { return q.compare(lookup(a), lookup(b)) })
	return out
}

// match tells whether a record matches every term. One that lacks the key
// matches only ne and lacks.
func (q Query) match(lookup Lookup) bool {
	for _, t := range q.terms {
		v, ok := lookup(t.key)
		if !t.match(v, ok) {
			return false
		}
	}
	return true
}

func (t term) match(v string, ok bool) bool {
	switch t.op {
	case api.FilterHas:
		return ok
	case api.FilterLacks:
		return !ok
	case api.FilterNe:
		return !ok || t.typ.Compare(v, t.value) != 0
	}
	if !ok {
		return false
	}
	c := t.typ.Compare(v, t.value)
	switch t.op {
	case api.FilterEq:
		return c == 0
	case api.FilterLt:
		return c < 0
	case api.FilterLe:
		return c <= 0
	case api.FilterGt:
		return c > 0
	}
	return c >= 0 // FilterGe, the one left that New lets through
}

// compare orders two records by the sort keys in turn. A record that lacks
// a key sorts after one that has it, in either direction.
func (q Query) compare(a, b Lookup) int {
	for _, k := range q.sort {
		va, okA := a(k.key)
		vb, okB := b(k.key)
		switch {
		case !okA && !okB:
			continue
		case !okA:
			return 1
		case !okB:
			return -1
		}
		c := k.typ.Compare(va, vb)
		if k.desc {
			c = -c
		}
		if c != 0 {
			return c
		}
	}
	return 0
}
