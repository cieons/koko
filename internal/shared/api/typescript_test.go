package api_test

// The web UI's TypeScript types are generated from the API's Go types (ADR
// 0015, ADR 0023) by the generator in tsgen_test.go. The files are committed:
// TestTypeScriptTypes fails when one is out of date, and `make generate`
// (go test -update) rewrites them.

import (
	"flag"
	"maps"
	"reflect"
	"slices"
	"testing"
	"time"
)

// tsDir is where the generated files live, from this package's directory.
const tsDir = "../../../web/src/api"

var update = flag.Bool("update", false, "rewrite web/src/api from the API's Go types")

func TestTypeScriptTypes(t *testing.T) {
	want, err := generateTypeScript(".")
	if err != nil {
		t.Fatalf("generate the TypeScript types: %v", err)
	}
	if *update {
		if err := writeTypeScript(tsDir, want); err != nil {
			t.Fatal(err)
		}
		return
	}
	got, err := readTypeScript(tsDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range slices.Sorted(maps.Keys(want)) {
		if content, ok := got[name]; !ok {
			t.Errorf("web/src/api/%s is missing: run make generate", name)
		} else if content != want[name] {
			t.Errorf("web/src/api/%s is out of date with internal/shared/api: run make generate", name)
		}
	}
	for _, name := range slices.Sorted(maps.Keys(got)) {
		if _, ok := want[name]; !ok {
			t.Errorf("web/src/api/%s is not generated from internal/shared/api: run make generate to remove it", name)
		}
	}
}

func TestTypeScriptTypeMapping(t *testing.T) {
	type record struct{}
	for _, tc := range []struct {
		value any
		want  string
	}{
		{"", "string"},
		{false, "boolean"},
		{0, "number"},
		{uint8(0), "number"},
		{0.5, "number"},
		{time.Time{}, "string"},
		{record{}, "record"},
		{[]record{}, "record[]"},
		{[]*string{}, "(string | null)[]"},
		{map[string]int{}, "Record<string, number>"},
		{new(record), "record | null"},
	} {
		typ := reflect.TypeOf(tc.value)
		got, err := tsType(typ, nameRef)
		if err != nil || got != tc.want {
			t.Errorf("%s: got %q, %v; want %q", typ, got, err, tc.want)
		}
	}
}

func TestTypeScriptFieldMapping(t *testing.T) {
	type fields struct {
		Plain      string   `json:"plain"`
		Untagged   int      // keeps its Go name
		Zero       string   `json:"zero,omitzero"`
		Empty      []string `json:"empty,omitempty"`
		Pointer    *string  `json:"pointer"`
		Optional   *string  `json:"optional,omitzero"`
		Ignored    string   `json:"-"`
		unexported string
	}
	got, err := tsFields(reflect.TypeFor[fields](), nameRef)
	if err != nil {
		t.Fatal(err)
	}
	want := []tsField{
		{"Plain", "plain", "string", false},
		{"Untagged", "Untagged", "number", false},
		{"Zero", "zero", "string", true},
		{"Empty", "empty", "string[]", true},
		{"Pointer", "pointer", "string | null", false},
		{"Optional", "optional", "string", true},
	}
	if !slices.Equal(got, want) {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}
	_ = fields{}.unexported
}

// A shape the generator cannot write is refused, naming it, rather than
// written wrong.
func TestTypeScriptRefusesWhatItCannotMap(t *testing.T) {
	for name, typ := range map[string]reflect.Type{
		"any":              reflect.TypeFor[any](),
		"bytes":            reflect.TypeFor[[]byte](),
		"int keys":         reflect.TypeFor[map[int]string](),
		"anonymous struct": reflect.TypeFor[struct{ A string }](),
		"duration":         reflect.TypeFor[time.Duration](),
		"marshaler":        reflect.TypeFor[textual](),
	} {
		if _, err := tsType(typ, nameRef); err == nil {
			t.Errorf("type %s (%s): no error", name, typ)
		}
	}
	type embedded struct{}
	for name, typ := range map[string]reflect.Type{
		"embedded field": reflect.TypeFor[struct{ embedded }](),
		"string option": reflect.TypeFor[struct {
			N int `json:"n,string"`
		}](),
		"case option": reflect.TypeFor[struct {
			N int `json:"n,case:ignore"`
		}](),
		"quoted name": reflect.TypeFor[struct {
			N int `json:"'n'"`
		}](),
	} {
		if _, err := tsFields(typ, nameRef); err == nil {
			t.Errorf("field %s (%s): no error", name, typ)
		}
	}
}

// nameRef names a struct type by its Go name, as the generator does for one
// it may write.
func nameRef(t reflect.Type) (string, error) {
	return t.Name(), nil
}

// textual has a text form of its own.
type textual struct{}

func (textual) MarshalText() ([]byte, error) { return []byte("text"), nil }
