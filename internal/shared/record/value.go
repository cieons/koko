package record

import (
	"cmp"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

// Kind is one of the four kinds of Attribute values.
type Kind string

const (
	// KindText is free text: 1 byte to 1 KiB of UTF-8 on one line,
	// compared in natural order.
	KindText Kind = "text"
	// KindNumber is an exact decimal (ParseDecimal), compared by value.
	KindNumber Kind = "number"
	// KindDate is a calendar date, YYYY-MM-DD, compared by the calendar.
	KindDate Kind = "date"
	// KindEnum is one of a fixed set of values, compared in the order they
	// are declared in.
	KindEnum Kind = "enum"
)

// Type is the type of a key's values: a Preset's, a field's, or free text
// for every other Attribute key. It parses a value, refusing one that does
// not parse, and compares two values.
type Type struct {
	kind Kind
	// values are an enum's values, in their declared order.
	values []string
	// about names an enum's values for people, when they are too many to
	// list, such as the ISO 4217 codes.
	about string
	// nonNegative makes a number at least 0.
	nonNegative bool
}

// The types of keys that take any value of their kind.
var (
	Text   = Type{kind: KindText}
	Number = Type{kind: KindNumber}
	Date   = Type{kind: KindDate}
)

// Enum is the type whose values are values, ordered as given.
func Enum(values ...string) Type {
	return Type{kind: KindEnum, values: values}
}

// Kind is the kind of the type's values.
func (t Type) Kind() Kind {
	return t.kind
}

// Values are an enum's values in their declared order, which is the order
// they sort in; nil for the other kinds.
func (t Type) Values() []string {
	return slices.Clone(t.values)
}

// Min returns the least value of a number type that has one, such as
// price's 0, and whether it has one.
func (t Type) Min() (Value, bool) {
	if t.nonNegative {
		return Value{text: "0", number: true}, true
	}
	return Value{}, false
}

// KeyType returns the type of the values of key on a list whose fields, by
// key, have the types fields gives: the field's, or an Attribute key's, its
// Preset's or Text. It refuses the name of another field, such as the
// Note's, which no Attribute key can be, and a key that breaks the key
// rule; list names the list for the refusal, such as koko host ls. The CLI
// asks it before any call, and the koko server on every call.
func KeyType(fields map[string]Type, key, list string) (Type, error) {
	if t, ok := fields[key]; ok {
		return t, nil
	}
	if Reserved(key) {
		return Type{}, fmt.Errorf("%s does not filter or sort on the field %s", list, key)
	}
	if err := CheckKey(key); err != nil {
		return Type{}, err
	}
	return TypeOf(key), nil
}

// Value is one value of a field or an Attribute, as koko stores it and as
// JSON carries it: its text in the one form koko stores (String), and
// whether JSON writes it as a number. A number is a JSON number written
// exactly as stored, so no part of the path decodes it through binary
// floating point; every other value is a JSON string. A Value is checked
// only when a Type parses it.
type Value struct {
	text   string
	number bool
}

// StringValue is s as a JSON string, as text, dates and enums are written,
// unchecked: Type.Check judges it.
func StringValue(s string) Value {
	return Value{text: s}
}

// String returns the value's text, in the form koko stores: for a number,
// its exact decimal.
func (v Value) String() string {
	return v.text
}

// MarshalJSONTo writes the value as a JSON number, its decimal exactly as
// stored, or as a JSON string.
func (v Value) MarshalJSONTo(enc *jsontext.Encoder) error {
	if v.number {
		return enc.WriteValue(jsontext.Value(v.text))
	}
	return enc.WriteToken(jsontext.String(v.text))
}

// UnmarshalJSONFrom reads a JSON string, or a JSON number as the text it is
// written in, never decoding it into a binary floating-point type.
func (v *Value) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	tok, err := dec.ReadToken()
	if err != nil {
		return err
	}
	switch tok.Kind() {
	case '"':
		*v = Value{text: tok.String()}
	case '0':
		*v = Value{text: tok.String(), number: true}
	default:
		return fmt.Errorf("an Attribute value is a JSON string or number, not %s", tok.Kind())
	}
	return nil
}

// Parse reads s, a value written as text, as on a command line or as
// stored, and returns it in the one form koko stores, or why it is not a
// value of t. It never falls back to text.
func (t Type) Parse(s string) (Value, error) {
	if s == "" {
		return Value{}, errors.New("a value cannot be empty")
	}
	switch t.kind {
	case KindNumber:
		d, err := ParseDecimal(s)
		if err != nil {
			return Value{}, err
		}
		if t.nonNegative && strings.HasPrefix(d, "-") {
			return Value{}, fmt.Errorf("%s is below 0", s)
		}
		return Value{text: d, number: true}, nil
	case KindDate:
		d, err := ParseDate(s)
		return Value{text: d}, err
	case KindEnum:
		if !slices.Contains(t.values, s) {
			return Value{}, fmt.Errorf("%q is not one of %s", s, t.describeValues())
		}
		return Value{text: s}, nil
	}
	return Value{text: s}, checkText(s)
}

// Check judges v, as JSON carried it, as a value of t: a number must come as
// a JSON number and every other value as a JSON string. It returns v in the
// one form koko stores.
func (t Type) Check(v Value) (Value, error) {
	switch {
	case t.kind == KindNumber && !v.number:
		return Value{}, fmt.Errorf("%q is a JSON string, where a number is written as a JSON number", v.text)
	case t.kind != KindNumber && v.number:
		return Value{}, fmt.Errorf("%s is a JSON number, where %s is written as a JSON string", v.text, t.kind)
	}
	return t.Parse(v.text)
}

// Compare compares two values of t, each in the form koko stores, as
// Value.String gives it: numbers by value, dates by the calendar, enum
// values in their declared order and text in natural order, where runs of
// digits compare as numbers and the rest byte by byte, so case matters. It
// is 0 only for equal values.
func (t Type) Compare(a, b string) int {
	switch t.kind {
	case KindNumber:
		return compareDecimal(a, b)
	case KindDate:
		return strings.Compare(a, b)
	case KindEnum:
		return cmp.Compare(slices.Index(t.values, a), slices.Index(t.values, b))
	}
	return compareNatural(a, b)
}

// ParseDate returns s if it is a calendar date written YYYY-MM-DD, from
// 0001-01-01 to 9999-12-31, or why it is not one. A date has no time and no
// time zone; a client resolves a relative date, such as +30d, to one before
// any call.
func ParseDate(s string) (string, error) {
	d, err := time.Parse(time.DateOnly, s)
	if err != nil || d.Format(time.DateOnly) != s || d.Year() < 1 {
		return "", fmt.Errorf("%q is not a calendar date written YYYY-MM-DD, such as 2026-12-01", s)
	}
	return s, nil
}

// describeValues names an enum's values: all of them, or what they are when
// they are too many to list.
func (t Type) describeValues() string {
	return cmp.Or(t.about, strings.Join(t.values, ", "))
}

// compareNatural compares a and b in natural order: a run of ASCII digits
// against a run of digits by its value, anything else byte by byte, and a
// shorter string first when one is a prefix of the other. Strings that
// differ only in leading zeros, such as "a01" and "a1", tie there and are
// told apart byte by byte, so the order is total.
func compareNatural(a, b string) int {
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		if isDigit(a[i]) && isDigit(b[j]) {
			ei, ej := digitsEnd(a, i), digitsEnd(b, j)
			if c := compareDigits(a[i:ei], b[j:ej]); c != 0 {
				return c
			}
			i, j = ei, ej
			continue
		}
		if c := cmp.Compare(a[i], b[j]); c != 0 {
			return c
		}
		i, j = i+1, j+1
	}
	if c := cmp.Compare(len(a)-i, len(b)-j); c != 0 {
		return c
	}
	return strings.Compare(a, b)
}

// isDigit tells whether c is an ASCII digit, which natural order and the
// decimals read.
func isDigit(c byte) bool {
	return '0' <= c && c <= '9'
}

// digitsEnd returns where the run of digits that starts at i in s ends.
func digitsEnd(s string, i int) int {
	for i < len(s) && isDigit(s[i]) {
		i++
	}
	return i
}

// compareDigits compares two runs of digits by the whole numbers they
// write, of any length.
func compareDigits(a, b string) int {
	a, b = strings.TrimLeft(a, "0"), strings.TrimLeft(b, "0")
	return cmp.Or(cmp.Compare(len(a), len(b)), strings.Compare(a, b))
}
