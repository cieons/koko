package cli_test

import (
	"strings"
	"testing"

	"github.com/cieons/koko/internal/harness"
)

// Each refusal of the command line exits 2 with the error on stderr only, and
// the koko server sees no call.
func TestCommandLineRefusals(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	for _, tc := range []struct {
		name    string
		args    []string
		message string
	}{
		{"flag before the path", []string{"--json", "space", "ls"}, "write koko space ls --json"},
		{"flag inside the path", []string{"space", "--json", "add", "ops"}, "write koko space add ops --json"},
		{"value flag before the path", []string{"--json", "--kokod", "/srv/k", "server", "install"}, "write koko server install --json --kokod /srv/k"},
		{"value that needs quoting", []string{"--kokod=/srv/my k", "server", "install", "--json"}, "write koko server install --json '--kokod=/srv/my k'"},
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

// koko alone, or a group alone, lists the commands as koko help does, and
// calls no koko server.
func TestAGroupAloneListsItsCommands(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	for _, args := range [][]string{{}, {"host"}, {"space", "member"}} {
		r := h.Koko(args...)
		r.WantSuccess(t)
		if want := h.Koko(append([]string{"help"}, args...)...).Stdout; r.Stdout != want {
			t.Errorf("koko %s = %q, want what koko help %[1]s prints, %q", strings.Join(args, " "), r.Stdout, want)
		}
	}
	if n := h.Calls(); n != 0 {
		t.Errorf("the koko server answered %d call(s) to help", n)
	}
}

// Without --json a refusal is a line for people on stderr, then the
// command's usage line and where its help is.
func TestRefusalForPeople(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	for _, tc := range []struct {
		args []string
		says string
	}{
		{[]string{"space", "ls", "--all"}, "koko: unknown flag --all for koko space ls\n" +
			"Usage: koko space ls\n" +
			"Run 'koko space ls --help' for the flags.\n"},
		{[]string{"host", "enroll"}, "koko: koko host enroll needs <host>\n" +
			"Usage: koko host enroll <host> [--password-stdin] [--key <file>] [--secret <space/secret>] [--host-key <fingerprint>] [--replace-host-key]\n" +
			"Run 'koko host enroll --help' for the flags.\n"},
		{[]string{"space", "member", "frob"}, "koko: koko space member frob is not a command; koko space member takes add, rm\n" +
			"Run 'koko space member --help' for its commands.\n"},
	} {
		r := h.Koko(tc.args...)
		if r.Status != 2 || r.Stdout != "" || r.Stderr != tc.says {
			t.Errorf("koko %s: status %d, stdout %q, stderr %q; want 2 and %q", strings.Join(tc.args, " "), r.Status, r.Stdout, r.Stderr, tc.says)
		}
	}
	r := h.Koko("frob")
	if r.Status != 2 || !strings.HasPrefix(r.Stderr, "koko: koko frob is not a command") || !strings.HasSuffix(r.Stderr, "\nRun 'koko --help' for the commands.\n") {
		t.Errorf("koko frob: status %d, stderr %q; want 2, the refusal and the pointer to koko --help", r.Status, r.Stderr)
	}
}

// Flags and arguments after the path come in any order.
func TestFlagsAfterThePathInAnyOrder(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	h.Koko("space", "add", "--json", "ops").WantSuccess(t)
	h.Koko("space", "add", "data", "--json").WantSuccess(t)
	// The call count the refusal tests rely on sees real calls.
	if n := h.Calls(); n != 2 {
		t.Errorf("the koko server answered %d calls, want 2", n)
	}
}
