package cli

import (
	"reflect"
	"testing"

	"github.com/cieons/koko/internal/shared/api"
)

// A command whose operation answers with a Join Code prints a credential, so
// it is in the irreversible tier and an agent's harness asks a person before
// running it (ADR 0019).
func TestCommandsThatPrintAJoinCodeAreIrreversible(t *testing.T) {
	printing := 0
	for _, c := range Tree() {
		if c.Op == nil || !holds(c.Op.Info().Response, reflect.TypeFor[api.JoinCode](), map[reflect.Type]bool{}) {
			continue
		}
		printing++
		if c.Tier != Irreversible {
			t.Errorf("koko %s prints a Join Code but is in the %s tier, want irreversible", c.Path(), c.Tier)
		}
	}
	if printing < 2 {
		t.Errorf("%d commands print a Join Code, want at least device add and user invite", printing)
	}
}

// holds tells whether a value of type t can hold a want.
func holds(t, want reflect.Type, seen map[reflect.Type]bool) bool {
	for t.Kind() == reflect.Pointer || t.Kind() == reflect.Slice || t.Kind() == reflect.Array || t.Kind() == reflect.Map {
		t = t.Elem()
	}
	if t == want {
		return true
	}
	if t.Kind() != reflect.Struct || seen[t] {
		return false
	}
	seen[t] = true
	for f := range t.Fields() {
		if holds(f.Type, want, seen) {
			return true
		}
	}
	return false
}
