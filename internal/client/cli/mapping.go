package cli

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
)

// mapping says how one API response type reaches the CLI's own --json, which
// is mapped from it by hand (ADR 0016). Each field of api, by JSON name, shows
// under the same name in cli or is listed in omitted with the reason it is
// left out on purpose. A response type the CLI unwraps, such as a list's
// wrapper, has no cli type: its fields' types are mapped instead.
//
// The mapping test fails for an API response field in neither, so a change
// that adds a field to the API also decides whether the CLI shows it.
type mapping struct {
	api     reflect.Type
	cli     reflect.Type
	omitted map[string]string
}

// mappings lists every area's mappings; an area's first ticket adds its line.
var mappings = [][]mapping{
	spaceMappings,
	deviceMappings,
	userMappings,
	webMappings,
}

// checkMappings returns one error for each API response field, reachable
// from the operations the commands call, that no mapping accounts for.
func checkMappings(cmds []Command, ms []mapping) []error {
	var errs []error
	seen := map[reflect.Type]bool{}
	var walk func(t reflect.Type, from string)
	walk = func(t reflect.Type, from string) {
		for t.Kind() == reflect.Pointer || t.Kind() == reflect.Slice || t.Kind() == reflect.Array || t.Kind() == reflect.Map {
			t = t.Elem()
		}
		if t.Kind() != reflect.Struct || seen[t] {
			return
		}
		seen[t] = true
		i := slices.IndexFunc(ms, func(m mapping) bool { return m.api == t })
		if i < 0 {
			errs = append(errs, fmt.Errorf("API type %v, in %s, has no mapping into the CLI's --json", t, from))
			return
		}
		m := ms[i]
		for f := range t.Fields() {
			name, ok := jsonName(f)
			if !ok || m.omitted[name] != "" {
				continue
			}
			at := fmt.Sprintf("%v field %q", t, name)
			if m.cli == nil {
				walk(f.Type, at)
				continue
			}
			if !hasJSONField(m.cli, name) {
				errs = append(errs, fmt.Errorf("%s is neither in the CLI's %v nor left out on purpose", at, m.cli))
			}
			walk(f.Type, at)
		}
	}
	for _, c := range cmds {
		if c.Op != nil {
			info := c.Op.Info()
			walk(info.Response, "the response of "+info.Name)
		}
	}
	return errs
}

// jsonName is the JSON member name of a struct field, as encoding/json/v2
// names it; ok is false for a field JSON leaves out.
func jsonName(f reflect.StructField) (name string, ok bool) {
	if !f.IsExported() {
		return "", false
	}
	tag := f.Tag.Get("json")
	if tag == "-" {
		return "", false
	}
	if name, _, _ := strings.Cut(tag, ","); name != "" {
		return name, true
	}
	return f.Name, true
}

func hasJSONField(t reflect.Type, name string) bool {
	for f := range t.Fields() {
		if n, ok := jsonName(f); ok && n == name {
			return true
		}
	}
	return false
}
