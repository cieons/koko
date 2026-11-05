package cli

import (
	"maps"
	"slices"
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

// inputFor parses argv on tree into the Input a command's Run would get.
func inputFor(t *testing.T, tree []Command, argv ...string) *Input {
	t.Helper()
	p, err := parse(tree, argv, noEnv)
	if err != nil {
		t.Fatalf("parse %q: %v", argv, err)
	}
	return &Input{inv: &Invocation{Args: argv}, runner: &Runner{}, line: p}
}

// wantRefused fails the test unless err refuses the command line (exit 2,
// invalid_usage) with message.
func wantRefused(t *testing.T, err error, message string) {
	t.Helper()
	if err == nil {
		t.Errorf("no error, want the refusal %q", message)
		return
	}
	status, e := classify(err)
	if status != exitRefused || e.Code != codeInvalidUsage || e.Message != message {
		t.Errorf("exit %d, %s %q; want exit 2, invalid_usage %q", status, e.Code, e.Message, message)
	}
}

// A tree with the write grammar: key=value pairs, --unset, and qualified
// names, bare or not.
var writeTree = []Command{
	{Noun: "host", Verb: "add", Tier: Reversible, Args: []Arg{{Name: "host"}}, Pairs: true},
	{Noun: "host", Verb: "set", Tier: Reversible, Args: []Arg{{Name: "host"}}, Pairs: true, Unset: true},
	{Noun: "secret", Verb: "move", Tier: Reversible, Args: []Arg{{Name: "secret", Qualifier: "space"}, {Name: "space"}}},
	{Noun: "device", Verb: "revoke", Tier: Irreversible, Args: []Arg{{Name: "device", Qualifier: "user", Bare: true}}},
}

func TestParseWrites(t *testing.T) {
	for _, tc := range []struct {
		argv  []string
		pairs []Pair
		unset []string
	}{
		{[]string{"host", "add", "web-1"}, nil, nil},
		{
			[]string{"host", "add", "address=203.0.113.7", "web-1", "account=root"},
			[]Pair{{"address", "203.0.113.7"}, {"account", "root"}},
			nil,
		},
		{[]string{"host", "set", "web-1", "note=a=b"}, []Pair{{"note", "a=b"}}, nil},
		{
			[]string{"host", "set", "web-1", "--unset", "region", "plan=cx22", "--unset=owner"},
			[]Pair{{"plan", "cx22"}},
			[]string{"region", "owner"},
		},
	} {
		in := inputFor(t, writeTree, tc.argv...)
		if in.Arg("host") != "web-1" || !slices.Equal(in.Pairs(), tc.pairs) || !slices.Equal(in.Unset(), tc.unset) {
			t.Errorf("parse %q = <host> %q, pairs %v, unset %v; want web-1, %v, %v",
				tc.argv, in.Arg("host"), in.Pairs(), in.Unset(), tc.pairs, tc.unset)
		}
	}
}

func TestParseRefusesWrites(t *testing.T) {
	for _, tc := range []struct {
		argv    []string
		message string
	}{
		{[]string{"host", "add", "web-1", "region="}, "the value of region= cannot be empty"},
		{[]string{"host", "set", "web-1", "region="}, "the value of region= cannot be empty; to clear region, write --unset region"},
		{[]string{"host", "set", "web-1", "=eu"}, "=eu has no key: write <key>=eu"},
		{[]string{"host", "set", "web-1", "region=eu", "region=us"}, "region= is given twice"},
		{[]string{"host", "set", "web-1", "--unset", "region", "--unset", "region"}, "--unset region is given twice"},
		{[]string{"host", "set", "web-1", "region=eu", "--unset", "region"}, "region= and --unset region are both given"},
		{[]string{"host", "set", "web-1", "--unset"}, "--unset needs a value (write --unset=<key> for one that starts with --)"},
		{[]string{"host", "set", "web-1", "--unset="}, "--unset needs a value; it cannot be empty"},
		{[]string{"host", "add", "web-1", "--unset", "region"}, "unknown flag --unset for koko host add"},
		{[]string{"host", "--unset", "region", "set", "web-1"}, "flags come after the command path: write koko host set web-1 --unset region"},
		{[]string{"host", "set", "region=eu"}, "koko host set needs <host>"},
	} {
		_, err := parse(writeTree, tc.argv, noEnv)
		wantRefused(t, err, tc.message)
	}
}

func TestParseQualifiedNames(t *testing.T) {
	for _, tc := range []struct {
		argv            []string
		arg             string
		qualifier, name string
	}{
		{[]string{"secret", "move", "ops/db-password", "prod"}, "secret", "ops", "db-password"},
		{[]string{"device", "revoke", "alice/laptop"}, "device", "alice", "laptop"},
		{[]string{"device", "revoke", "laptop"}, "device", "", "laptop"},
	} {
		in := inputFor(t, writeTree, tc.argv...)
		if q, n := in.Qualified(tc.arg); q != tc.qualifier || n != tc.name {
			t.Errorf("parse %q: <%s> = %q, %q; want %q, %q", tc.argv, tc.arg, q, n, tc.qualifier, tc.name)
		}
		if in.Arg(tc.arg) != tc.argv[2] {
			t.Errorf("parse %q: <%s> as typed = %q, want %q", tc.argv, tc.arg, in.Arg(tc.arg), tc.argv[2])
		}
	}
}

func TestParseRefusesQualifiedNames(t *testing.T) {
	for _, tc := range []struct {
		argv    []string
		message string
	}{
		{[]string{"secret", "move", "db-password", "prod"}, `"db-password" is not <space>/<secret>: the <space> is missing`},
		{[]string{"secret", "move", "/db-password", "prod"}, `"/db-password" is not <space>/<secret>: the <space> is empty`},
		{[]string{"secret", "move", "ops/", "prod"}, `"ops/" is not <space>/<secret>: the <secret> is empty`},
		{[]string{"secret", "move", "/", "prod"}, `"/" is not <space>/<secret>: the <space> is empty`},
		{[]string{"secret", "move", "ops/db/password", "prod"}, `"ops/db/password" is not <space>/<secret>: a name cannot hold /`},
		{[]string{"secret", "move", "", "prod"}, "<space>/<secret> cannot be empty in koko secret move"},
		{[]string{"secret", "move"}, "koko secret move needs <space>/<secret>"},
		{[]string{"secret", "move", "ops/db-password"}, "koko secret move needs <space>"},
		{[]string{"device", "revoke", "/laptop"}, `"/laptop" is not [<user>/]<device>: the <user> is empty`},
		{[]string{"device", "revoke", "alice/"}, `"alice/" is not [<user>/]<device>: the <device> is empty`},
		{[]string{"device", "revoke", "a/b/c"}, `"a/b/c" is not [<user>/]<device>: a name cannot hold /`},
		{[]string{"device", "revoke", "a", "b"}, `unexpected argument "b": write koko device revoke [<user>/]<device>`},
	} {
		_, err := parse(writeTree, tc.argv, noEnv)
		wantRefused(t, err, tc.message)
	}
}
