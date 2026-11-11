package api_test

import (
	"cmp"
	"encoding"
	"encoding/json/v2"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/cieons/koko/internal/harness/frozen"
	"github.com/cieons/koko/internal/shared/api"
)

// requestField is one field of an operation's request: its path, as
// api.Op.Added names it, and its JSON tag's options.
type requestField struct {
	path string
	// omitted says the field is left out of a request when unset.
	omitted bool
}

// requestFields returns every field of the request type t, in the order
// declared, fields of nested objects after their own. A field of a type
// that encodes itself, such as record.Value, a map's values included, is
// one field.
func requestFields(t *testing.T, typ reflect.Type) []requestField {
	t.Helper()
	var fields []requestField
	var walk func(typ reflect.Type, prefix string, seen []reflect.Type)
	walk = func(typ reflect.Type, prefix string, seen []reflect.Type) {
		for i := range typ.NumField() {
			f := typ.Field(i)
			name, opts, _ := strings.Cut(f.Tag.Get("json"), ",")
			if !f.IsExported() || name == "-" {
				continue
			}
			if f.Anonymous {
				t.Fatalf("%s holds the embedded field %s: teach requestFields how JSON shapes it", typ, f.Name)
			}
			path := prefix + cmp.Or(name, f.Name)
			omitted := slices.ContainsFunc(strings.Split(opts, ","), func(o string) bool { return o == "omitzero" || o == "omitempty" })
			fields = append(fields, requestField{path: path, omitted: omitted})
			inner, list := nested(f.Type)
			switch {
			case inner == nil:
			case slices.Contains(seen, inner):
				t.Fatalf("%s holds itself: teach requestFields to list a recursive request", inner)
			case list:
				walk(inner, path+"[].", append(seen, inner))
			default:
				walk(inner, path+".", append(seen, inner))
			}
		}
	}
	walk(typ, "", []reflect.Type{typ})
	return fields
}

// nested returns the object type whose fields a field of type typ holds,
// and whether in a list's items; nil for a field that holds none.
func nested(typ reflect.Type) (inner reflect.Type, list bool) {
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ.Kind() == reflect.Slice || typ.Kind() == reflect.Array {
		inner, _ := nested(typ.Elem())
		return inner, inner != nil
	}
	if typ.Kind() == reflect.Map {
		if inner, _ := nested(typ.Elem()); inner != nil {
			panic(fmt.Sprintf("a map of %s: teach requestFields and api.Op.Added to name its fields", typ.Elem()))
		}
		return nil, false
	}
	if typ.Kind() != reflect.Struct || encodesItself(typ) {
		return nil, false
	}
	return typ, false
}

// encodesItself tells whether values of typ encode themselves in JSON.
func encodesItself(typ reflect.Type) bool {
	for _, i := range []reflect.Type{
		reflect.TypeFor[json.Marshaler](),
		reflect.TypeFor[json.MarshalerTo](),
		reflect.TypeFor[encoding.TextMarshaler](),
	} {
		if typ.Implements(i) || reflect.PointerTo(typ).Implements(i) {
			return true
		}
	}
	return false
}

// Every revision of every operation is recorded with the request fields
// it takes and the revision that added each, and a recorded revision never
// changes: a field added, removed or renamed without a raise of the
// operation's revision fails here, and -update records only a new
// revision (ADR 0049).
func TestEveryOperationsRequestFieldsAreRecordedByRevision(t *testing.T) {
	var sections []frozen.Section
	for _, o := range api.Operations() {
		info := o.Info()
		fields := requestFields(t, info.Request)
		for rev := 1; rev <= info.Revision; rev++ {
			var lines []string
			for _, f := range fields {
				if added := max(info.Added[f.path], 1); added <= rev {
					lines = append(lines, fmt.Sprintf("%s %d", f.path, added))
				}
			}
			sections = append(sections, frozen.Section{Name: fmt.Sprintf("%s %d", info.Name, rev), Text: strings.Join(lines, "\n")})
		}
	}
	frozen.Check(t, "requests.golden", sections, *update)
}

// An operation names in Added only fields its request has, each added at a
// revision from 2 to its own, and left out of a request when unset, so a
// CLI sends one only when an option sets it.
func TestEveryAddedFieldIsOneTheRequestLeavesOutUnset(t *testing.T) {
	for _, o := range api.Operations() {
		info := o.Info()
		fields := requestFields(t, info.Request)
		for path, added := range info.Added {
			i := slices.IndexFunc(fields, func(f requestField) bool { return f.path == path })
			switch {
			case i < 0:
				t.Errorf("%s: Added names %s, which its request %s has not", info.Name, path, info.Request)
			case added < 2 || added > info.Revision:
				t.Errorf("%s: Added gives %s revision %d, want 2 to the operation's, %d", info.Name, path, added, info.Revision)
			case !fields[i].omitted:
				t.Errorf("%s: %s, added at revision %d, is sent unset: tag it omitzero or omitempty", info.Name, path, added)
			}
		}
	}
}

// A request sets a field when it holds its name, in any of a list's
// items for a field of the items; a field added at or before the koko
// server's revision is not after it.
func TestFieldAfterFindsAFieldTheRequestSets(t *testing.T) {
	op := api.Op[api.HostAddRequest, api.HostAddResponse]{
		Name: "host.add", Revision: 3, Added: map[string]int{"hosts[].relay": 2, "hosts[].note": 3},
	}.Info()
	for _, tc := range []struct {
		name     string
		body     string
		revision int
		want     string
	}{
		{"none set", `{"hosts": [{"name": "web1"}, {"name": "web2"}]}`, 1, ""},
		{"set in a later item", `{"hosts": [{"name": "web1"}, {"name": "web2", "relay": "gw"}]}`, 1, "hosts[].relay"},
		{"the first by path", `{"hosts": [{"name": "web1", "relay": "gw", "note": "x"}]}`, 1, "hosts[].note"},
		{"taken", `{"hosts": [{"name": "web1", "relay": "gw"}]}`, 2, ""},
		{"one still later", `{"hosts": [{"name": "web1", "relay": "gw", "note": "x"}]}`, 2, "hosts[].note"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			field, ok, err := op.FieldAfter([]byte(tc.body), tc.revision)
			if err != nil {
				t.Fatal(err)
			}
			if field != tc.want || ok != (tc.want != "") {
				t.Errorf("FieldAfter(%s, %d) = %q, %v; want %q", tc.body, tc.revision, field, ok, tc.want)
			}
		})
	}
}
