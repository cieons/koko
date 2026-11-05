package cli_test

import (
	"strings"
	"testing"

	"github.com/cieons/koko/internal/harness"
)

// Each refusal of the command line exits 2 with the error on stderr only, and
// the koko server sees no call.
func TestCommandLineRefusals(t *testing.T) {
	h := harness.Start(t)
	for _, tc := range []struct {
		name    string
		args    []string
		message string
	}{
		{"flag before the path", []string{"--json", "space", "ls"}, "write koko space ls --json"},
		{"flag inside the path", []string{"space", "--json", "add", "ops"}, "write koko space add ops --json"},
		{"value flag before the path", []string{"--json", "--data-dir", "/srv/k", "server", "run"}, "write koko server run --json --data-dir /srv/k"},
		{"value that needs quoting", []string{"--data-dir=/srv/my k", "server", "run", "--json"}, "write koko server run --json '--data-dir=/srv/my k'"},
		{"no command", []string{"--json"}, "give a command"},
		{"unknown noun", []string{"spaces", "ls", "--json"}, "koko spaces is not a command"},
		{"unknown verb", []string{"space", "frob", "--json"}, "koko space frob is not a command; koko space takes add, ls"},
		{"noun alone", []string{"space", "--json"}, "koko space needs one of add, ls"},
		{"unknown flag", []string{"space", "ls", "--all", "--json"}, "unknown flag --all for koko space ls"},
		{"single-dash flag", []string{"space", "ls", "-a", "--json"}, "unknown flag -a for koko space ls"},
		{"missing argument", []string{"space", "add", "--json"}, "koko space add needs <space>"},
		{"empty argument", []string{"space", "add", "", "--json"}, "<space> cannot be empty"},
		{"extra argument", []string{"space", "ls", "ops", "--json"}, `unexpected argument "ops": koko space ls takes no arguments`},
		{"--json twice", []string{"space", "ls", "--json", "--json"}, "--json is given twice"},
		{"--json with a value", []string{"space", "ls", "--json=false"}, "--json takes no value"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := h.Koko(tc.args...)
			msg := r.WantError(t, 2, "invalid_usage")
			if !strings.Contains(msg, tc.message) {
				t.Errorf("message %q does not say %q", msg, tc.message)
			}
		})
	}
	if n := h.Calls(); n != 0 {
		t.Errorf("the koko server answered %d call(s) to refused command lines", n)
	}
}

func TestNoCommandLineAtAll(t *testing.T) {
	h := harness.Start(t)
	r := h.Koko()
	if r.Status != 2 || r.Stdout != "" || !strings.HasPrefix(r.Stderr, "koko: give a command") {
		t.Errorf("status %d, stdout %q, stderr %q", r.Status, r.Stdout, r.Stderr)
	}
}

// Without --json a refusal is one line for people on stderr.
func TestRefusalForPeople(t *testing.T) {
	h := harness.Start(t)
	r := h.Koko("space", "ls", "--all")
	if r.Status != 2 || r.Stdout != "" || r.Stderr != "koko: unknown flag --all for koko space ls\n" {
		t.Errorf("status %d, stdout %q, stderr %q", r.Status, r.Stdout, r.Stderr)
	}
}

// Flags and arguments after the path come in any order.
func TestFlagsAfterThePathInAnyOrder(t *testing.T) {
	h := harness.Start(t)
	h.Koko("space", "add", "--json", "ops").WantSuccess(t)
	h.Koko("space", "add", "data", "--json").WantSuccess(t)
	// The call count the refusal tests rely on sees real calls.
	if n := h.Calls(); n != 2 {
		t.Errorf("the koko server answered %d calls, want 2", n)
	}
}
