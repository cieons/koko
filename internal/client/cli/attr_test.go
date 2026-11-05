package cli_test

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"slices"
	"strings"
	"testing"

	"github.com/cieons/koko/internal/harness"
)

// printedAttr is an Attribute key as attr ls --json prints it.
type printedAttr struct {
	Key    string         `json:"key"`
	Type   string         `json:"type"`
	Values []string       `json:"values"`
	Min    jsontext.Value `json:"min"`
	Preset bool           `json:"preset"`
	InUse  bool           `json:"in_use"`
}

// attrKeys runs attr ls --json on the device run runs commands on and
// returns what it printed.
func attrKeys(t *testing.T, run func(args ...string) harness.Result) []printedAttr {
	t.Helper()
	r := run("attr", "ls", "--json")
	r.WantSuccess(t)
	var keys []printedAttr
	if err := json.Unmarshal([]byte(r.Stdout), &keys, json.RejectUnknownMembers(true)); err != nil {
		t.Fatalf("attr ls --json printed %q: %v", r.Stdout, err)
	}
	return keys
}

// attr ls prints the eight Presets with their types, in the order of the
// Preset table, then every other key in use, by key, each saying whether
// something has it.
func TestAttrLsPrintsThePresetsAndTheKeysInUse(t *testing.T) {
	h := harness.Start(t)
	recordHost(t, h.Koko, "web1", "price=10", "os=debian", "cores=8")
	recordHost(t, h.Koko, "web2", "cores=16")
	r := h.Koko("attr", "ls", "--json")
	r.WantSuccess(t)
	harness.Golden(t, "attr-ls.json", r.Stdout)

	keys := attrKeys(t, h.Koko)
	var got []string
	for _, k := range keys {
		got = append(got, k.Key)
	}
	want := []string{"provider", "provider_account", "region", "price", "currency", "billing_cycle", "expires_at", "auto_renew", "cores", "os"}
	if !slices.Equal(got, want) {
		t.Errorf("attr ls lists %v, want %v", got, want)
	}
	for _, k := range keys {
		if k.Key == "billing_cycle" && !slices.Equal(k.Values, []string{"monthly", "quarterly", "semiannual", "yearly", "biennial", "triennial"}) {
			t.Errorf("billing_cycle's values are %v", k.Values)
		}
		if k.Key == "currency" && (len(k.Values) != 178 || !slices.Contains(k.Values, "EUR") || !slices.IsSorted(k.Values)) {
			t.Errorf("currency's values are %v, want the 178 ISO 4217 codes, sorted", k.Values)
		}
		if inUse := slices.Contains([]string{"price", "cores", "os"}, k.Key); k.InUse != inUse {
			t.Errorf("attr ls says %s is in use: %t, want %t", k.Key, k.InUse, inUse)
		}
		if want := map[bool]string{true: "0", false: "null"}[k.Key == "price"]; string(k.Min) != want {
			t.Errorf("attr ls gives %s the least value %s, want %s", k.Key, k.Min, want)
		}
	}

	out := h.Koko("attr", "ls").Stdout
	var lines []string
	for line := range strings.Lines(out) {
		lines = append(lines, strings.Join(strings.Fields(line), " "))
	}
	for _, line := range []string{
		"KEY TYPE PRESET IN USE",
		"price number, at least 0 yes yes",
		"currency enum: 178 values, AED to ZWG yes no",
		"auto_renew enum: no, yes yes no",
		"cores text no yes",
	} {
		if !slices.Contains(lines, line) {
			t.Errorf("attr ls printed no line of the words %q:\n%s", line, out)
		}
	}
}

// A key in use only in a Space the caller cannot act in is answered as if
// nothing held it; the Owner sees every Space's.
func TestAttrLsListsTheKeysInUseOnWhatTheCallerCanSee(t *testing.T) {
	h := harness.Start(t)
	addSpaceWithMembers(t, h, "ops", "bob")
	h.Koko("user", "invite", "carol").WantSuccess(t)
	bob, carol := h.Join("bob"), h.Join("carol")
	recordHost(t, h.Koko, "web1", "space=ops", "rack=r7", "price=10")
	recordHost(t, carol.Koko, "web2", "os=debian")

	inUse := func(keys []printedAttr) []string {
		var used []string
		for _, k := range keys {
			if k.InUse {
				used = append(used, k.Key)
			}
		}
		return used
	}
	for _, tc := range []struct {
		who  string
		run  func(args ...string) harness.Result
		want []string
	}{
		{"the Owner", h.Koko, []string{"price", "os", "rack"}},
		{"bob, in ops", bob.Koko, []string{"price", "rack"}},
		{"carol, outside ops", carol.Koko, []string{"os"}},
	} {
		if got := inUse(attrKeys(t, tc.run)); !slices.Equal(got, tc.want) {
			t.Errorf("for %s attr ls has in use %v, want %v", tc.who, got, tc.want)
		}
	}
}
