package cli

import (
	"maps"
	"testing"
)

// A tree with shapes the real tree does not have yet: a nested noun, a
// command that is also the start of a longer one, and a boolean flag.
var shapesTree = []Command{
	{Noun: "space member", Verb: "add", Tier: Reversible, Args: []Arg{{Name: "space"}, {Name: "user"}}},
	{Verb: "skill", Tier: Read},
	{Noun: "skill", Verb: "install", Tier: Reversible, Flags: []Flag{{Name: "dir", Value: "dir"}}},
	{Verb: "web", Tier: Irreversible, Flags: []Flag{{Name: "no-open", Bool: true}}},
}

func noEnv(string) string { return "" }

func TestParseShapes(t *testing.T) {
	for _, tc := range []struct {
		argv  []string
		path  string
		args  map[string]string
		flags map[string]string
	}{
		{
			[]string{"space", "member", "add", "ops", "alice"},
			"space member add",
			map[string]string{"space": "ops", "user": "alice"},
			map[string]string{},
		},
		{[]string{"skill"}, "skill", map[string]string{}, map[string]string{}},
		{
			[]string{"skill", "install", "--dir", "/x"},
			"skill install",
			map[string]string{},
			map[string]string{"dir": "/x"},
		},
		{
			[]string{"skill", "install", "--dir=--odd"},
			"skill install",
			map[string]string{},
			map[string]string{"dir": "--odd"},
		},
		{[]string{"web", "--no-open"}, "web", map[string]string{}, map[string]string{"no-open": "true"}},
		{[]string{"web"}, "web", map[string]string{}, map[string]string{}},
	} {
		p, err := parse(shapesTree, tc.argv, noEnv)
		if err != nil {
			t.Errorf("parse %q: %v", tc.argv, err)
			continue
		}
		if p.cmd.Path() != tc.path || !maps.Equal(p.args, tc.args) || !maps.Equal(p.flags, tc.flags) {
			t.Errorf("parse %q = %s %v %v, want %s %v %v", tc.argv, p.cmd.Path(), p.args, p.flags, tc.path, tc.args, tc.flags)
		}
	}
}

func TestParseRefusesShapes(t *testing.T) {
	for _, tc := range []struct {
		argv    []string
		message string
	}{
		{[]string{"space", "--json", "member", "add", "ops", "alice"}, "flags come after the command path: write koko space member add ops alice --json"},
		{[]string{"space", "member", "--json"}, "koko space member needs one of add"},
		{[]string{"web", "--no-open=yes"}, "--no-open takes no value"},
		{[]string{"web", "--no-open", "--no-open"}, "--no-open is given twice"},
		{[]string{"space", "member", "add", "ops"}, "koko space member add needs <user>"},
		{[]string{"skill", "--json", "install"}, "flags come after the command path: write koko skill install --json"},
		{[]string{"skill", "--dir", "/x", "install"}, "flags come after the command path: write koko skill install --dir /x"},
		{[]string{"skill", "--json", "frob"}, `unexpected argument "frob": koko skill takes no arguments`},
	} {
		_, err := parse(shapesTree, tc.argv, noEnv)
		if err == nil || err.Error() != tc.message {
			t.Errorf("parse %q = %v, want %q", tc.argv, err, tc.message)
		}
	}
}
