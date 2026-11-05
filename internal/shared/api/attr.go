package api

import "github.com/cieons/koko/internal/shared/record"

// The operators of a filter term, as Filter.Op names them.
const (
	// FilterEq matches a record whose value equals the term's.
	FilterEq = "eq"
	// FilterNe matches a record whose value differs from the term's, and
	// one that lacks the key.
	FilterNe = "ne"
	// FilterLt, FilterLe, FilterGt and FilterGe match a record whose value
	// is less than, at most, greater than or at least the term's.
	FilterLt = "lt"
	FilterLe = "le"
	FilterGt = "gt"
	FilterGe = "ge"
	// FilterHas matches a record that has the key, and FilterLacks one that
	// lacks it.
	FilterHas   = "has"
	FilterLacks = "lacks"
)

// Filter is one term of a list's filter. A list returns the records that
// match every term: terms are ANDed. A term compares by the key's type
// (package record): numbers by value, dates by the calendar, enum values in
// their declared order, and text in natural order, case-sensitive. A
// record that lacks the key matches only ne and lacks.
type Filter struct {
	// Key is a field the list filters on, such as space or state, or an
	// Attribute key.
	Key string `json:"key"`
	// Op is eq, ne, lt, le, gt, ge, has or lacks (FilterEq to FilterLacks).
	Op string `json:"op"`
	// Value is what the term compares with, in the JSON type of the key's
	// values: a number as a JSON number, and every other value, a date
	// always written YYYY-MM-DD, as a JSON string. FilterHas and FilterLacks
	// take none.
	Value *record.Value `json:"value,omitzero"`
}

// SortKey is one key a list sorts by: a field the list filters on or an
// Attribute key, compared as Filter compares it. A record that lacks the key
// sorts last in either direction.
type SortKey struct {
	Key string `json:"key"`
	// Desc sorts from the greatest value down.
	Desc bool `json:"desc,omitzero"`
}

// AttrListRequest asks for the Attribute keys koko attr ls prints.
type AttrListRequest struct{}

// AttrKey is one Attribute key: a Preset, or a key in use.
type AttrKey struct {
	Key string `json:"key"`
	// Type is the kind of the key's values: text, number, date or enum.
	Type string `json:"type"`
	// Values are an enum's values in their declared order, which is the
	// order they sort in; left out for the other types.
	Values []string `json:"values,omitempty"`
	// Min is the least value of a number that has one, such as price's 0;
	// left out for any other key.
	Min *record.Value `json:"min,omitzero"`
	// Preset tells whether the key is a Preset Attribute's.
	Preset bool `json:"preset"`
	// InUse tells whether a Host or a Service the caller can see has the
	// key.
	InUse bool `json:"in_use"`
}

// AttrListResponse is every Preset, in the order of the Preset table, then
// every other key in use on the Hosts and Services the caller can see, by
// key.
type AttrListResponse struct {
	Attributes []AttrKey `json:"attributes"`
}

// AttrList lists the Preset Attributes with their types, and every other
// Attribute key in use on what the caller can see: in a Space the caller
// cannot act in, a key is answered as if nothing held it.
var AttrList = Op[AttrListRequest, AttrListResponse]{Name: "attr.list"}

var attrOperations = []Operation{
	AttrList,
}
