package cli

import (
	"slices"
	"strings"
	"testing"

	"github.com/cieons/koko/internal/shared/api"
)

// The rules every entry of the command tree keeps.
func TestCommandTreeEntries(t *testing.T) {
	var paths []string
	for _, c := range Tree() {
		path := "koko " + c.Path()
		if slices.Contains(paths, path) {
			t.Errorf("%s is in the tree twice", path)
		}
		paths = append(paths, path)
		if c.Verb == "" {
			t.Errorf("%s has no verb", path)
		}
		if c.Tier == tierUnset {
			t.Errorf("%s has no risk tier; every command needs one (ADR 0019)", path)
		}
		if c.Summary == "" {
			t.Errorf("%s has no summary", path)
		}
		if c.Run.run == nil {
			t.Errorf("%s runs nothing", path)
		}
		if c.Op != nil && c.Run.Shape() == nil {
			t.Errorf("%s calls %s but prints nothing", path, c.Op.Info().Name)
		}
		if c.Op != nil && !slices.ContainsFunc(api.Operations(), func(o api.Operation) bool {
			return o.Info().Name == c.Op.Info().Name
		}) {
			t.Errorf("%s calls %s, which is not in api.Operations", path, c.Op.Info().Name)
		}
		if c.Unset && !c.Pairs {
			t.Errorf("%s takes --unset but no key=value pairs", path)
		}
		if c.Filters != nil && c.Pairs {
			t.Errorf("%s takes both filter terms and key=value pairs, which the parser cannot tell apart", path)
		}
		if c.RemoteCommand && (c.many() == nil || len(c.Flags) > 0 || c.Pairs || c.Unset || len(c.Filters) > 0 || c.ReadsSecret || c.Run.Shape() != nil) {
			t.Errorf("%s runs a remote command, which takes the words after its other arguments as typed: it takes no flags, pairs, filters or secret, and prints nothing of its own", path)
		}
		if c.RemoteCommand != c.Run.remote {
			t.Errorf("%s: a command runs a remote command exactly when its Run is a remoteAction, which exits 255 for koko's own failures", path)
		}
		var names []string
		for i, a := range c.Args {
			switch {
			case slices.Contains(names, a.Name):
				t.Errorf("%s has two arguments named %s", path, a.Name)
			case i > 0 && c.Args[i-1].Optional && !a.Optional:
				t.Errorf("%s <%s> is required but follows an optional argument", path, a.Name)
			case strings.Contains(a.Name+a.Qualifier, "/"):
				t.Errorf("%s %s holds /; write a qualified name with Qualifier", path, a.usage())
			case a.Bare && a.Qualifier == "":
				t.Errorf("%s <%s> may be bare but has no qualifier", path, a.Name)
			case a.Many && i != len(c.Args)-1:
				t.Errorf("%s <%s> takes many words but is not the last argument", path, a.Name)
			case a.Many && a.Qualifier != "":
				t.Errorf("%s %s takes many words and is qualified, which the parser does not take", path, a.usage())
			}
			names = append(names, a.Name)
		}
		names = nil
		for _, f := range c.Flags {
			switch {
			case f.Name == "json":
				t.Errorf("%s lists --json, which every command already takes", path)
			case f.Name == unsetFlag.Name:
				t.Errorf("%s lists --unset, which Unset adds", path)
			case f.Name == sortFlag.Name:
				t.Errorf("%s lists --sort, which Filters adds", path)
			case slices.Contains(names, f.Name):
				t.Errorf("%s has two flags named --%s", path, f.Name)
			case f.Env != "" && !strings.HasPrefix(f.Env, "KOKO_"):
				t.Errorf("%s --%s reads %s; a flag's variable starts KOKO_", path, f.Name, f.Env)
			case f.Bool && (f.Value != "" || f.Default != "" || len(f.Choices) > 0 || f.Env != ""):
				t.Errorf("%s --%s is a boolean flag with a value, default, choices or variable", path, f.Name)
			case !f.Bool && f.Value == "":
				t.Errorf("%s --%s takes a value but does not name it", path, f.Name)
			case len(f.Choices) > 0 && f.Default != "" && !slices.Contains(f.Choices, f.Default):
				t.Errorf("%s --%s defaults to %q, which is not one of its choices", path, f.Name, f.Default)
			}
			names = append(names, f.Name)
		}
	}
}
