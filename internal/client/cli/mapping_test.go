package cli

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/cieons/koko/internal/shared/api"
)

// Every field of every API response the CLI reads is mapped into the CLI's
// own --json or left out on purpose (ADR 0016).
func TestEveryAPIResponseFieldIsMappedOrLeftOut(t *testing.T) {
	for _, err := range checkMappings(Tree(), slices.Concat(mappings...)) {
		t.Error(err)
	}
}

type widget struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Secret string `json:"secret"`
}

type widgetList struct {
	Widgets []widget `json:"widgets"`
}

type widgetJSON struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

var widgetCommands = []Command{{
	Noun: "widget", Verb: "ls", Tier: Read,
	Op: api.Op[struct{}, widgetList]{Name: "widget.list"},
}}

// The mapping test fails when the API gains a field the CLI neither shows nor
// leaves out, and when a response type has no mapping at all.
func TestMappingCheckFailsForAnUnmappedField(t *testing.T) {
	ms := []mapping{
		{api: reflect.TypeFor[widgetList]()},
		{api: reflect.TypeFor[widget](), cli: reflect.TypeFor[widgetJSON]()},
	}
	errs := checkMappings(widgetCommands, ms)
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), `field "secret" is neither in the CLI's`) {
		t.Errorf("errors = %v, want one naming the secret field", errs)
	}

	ms[1].omitted = map[string]string{"secret": "a credential is never printed by a list"}
	if errs := checkMappings(widgetCommands, ms); len(errs) != 0 {
		t.Errorf("errors = %v, want none once the field is left out on purpose", errs)
	}

	errs = checkMappings(widgetCommands, ms[:1])
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "has no mapping") {
		t.Errorf("errors = %v, want one for the widget type with no mapping", errs)
	}
}
