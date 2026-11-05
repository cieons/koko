package cli

import (
	"cmp"
	"encoding/json/v2"
	"reflect"
	"strings"

	"github.com/cieons/koko/internal/shared/record"
)

// shapeNotation says how koko skill writes a --json shape.
const shapeNotation = "`string`, `number` and `boolean`; `A | B` is either, and `T | null` may be null; " +
	"`[T]` is an array of T; `{\"key\": T}` is an object; `\"key\"?: T` is a member that may be left out; " +
	"`{<key>: T}` is an object of any keys"

// shaped is a --json type that marshals itself, which says the shape it
// writes, since its fields do not.
type shaped interface {
	jsonShape() string
}

// shape writes t, the type of a command's --json, in the notation of
// shapeNotation, as encoding/json/v2 marshals it. A type it has no notation
// for, such as one that marshals itself and is not shaped, is written any,
// which a test refuses.
func shape(t reflect.Type) string {
	if t == reflect.TypeFor[record.Value]() {
		return "string | number"
	}
	if t.Kind() == reflect.Pointer {
		s := shape(t.Elem())
		if strings.HasSuffix(s, " | null") {
			return s
		}
		return s + " | null"
	}
	if s, ok := reflect.Zero(t).Interface().(shaped); ok {
		return s.jsonShape()
	}
	if marshalsItself(t) {
		return "any"
	}
	switch t.Kind() {
	case reflect.String:
		return "string"
	case reflect.Bool:
		return "boolean"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Float32, reflect.Float64:
		return "number"
	case reflect.Slice:
		if t.Elem().Kind() == reflect.Uint8 {
			// A byte slice is a string of its bytes in base64.
			return "string"
		}
		return "[" + shape(t.Elem()) + "]"
	case reflect.Map:
		return "{<key>: " + shape(t.Elem()) + "}"
	case reflect.Struct:
		var members []string
		for _, f := range jsonFields(t) {
			name := `"` + f.name + `"`
			if f.optional {
				name += "?"
			}
			members = append(members, name+": "+shape(f.typ))
		}
		return "{" + strings.Join(members, ", ") + "}"
	}
	return "any"
}

// marshalsItself tells whether encoding/json/v2 marshals a t by a method of
// its own, whose output its fields do not say.
func marshalsItself(t reflect.Type) bool {
	for _, m := range []reflect.Type{reflect.TypeFor[json.MarshalerTo](), reflect.TypeFor[json.Marshaler]()} {
		if t.Implements(m) || reflect.PointerTo(t).Implements(m) {
			return true
		}
	}
	return false
}

// jsonField is one member of a struct's JSON.
type jsonField struct {
	name string
	typ  reflect.Type
	// optional says the member is left out when its value is zero or empty.
	optional bool
}

// jsonFields returns the members of struct t's JSON, in order, as
// encoding/json/v2 writes them: by their json tag's name or the field's,
// skipping unexported fields and those tagged "-", and taking the members of
// an embedded struct without a name as its own.
func jsonFields(t reflect.Type) []jsonField {
	var fields []jsonField
	for i := range t.NumField() {
		f := t.Field(i)
		tag := f.Tag.Get("json")
		if tag == "-" {
			continue
		}
		name, opts, _ := strings.Cut(tag, ",")
		if f.Anonymous && name == "" && f.Type.Kind() == reflect.Struct {
			fields = append(fields, jsonFields(f.Type)...)
			continue
		}
		if !f.IsExported() {
			continue
		}
		optional := false
		for opt := range strings.SplitSeq(opts, ",") {
			optional = optional || opt == "omitzero" || opt == "omitempty"
		}
		fields = append(fields, jsonField{name: cmp.Or(name, f.Name), typ: f.Type, optional: optional})
	}
	return fields
}
