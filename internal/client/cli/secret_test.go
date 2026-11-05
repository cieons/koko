package cli_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/cieons/koko/internal/harness"
)

// secretMaxSize is the longest value a Secret holds: 64 KiB.
const secretMaxSize = 64 << 10

// secretShown is a Secret as secret show --json prints it.
type secretShown struct {
	ID          string `json:"id"`
	Space       string `json:"space"`
	Name        string `json:"name"`
	ValueBase64 []byte `json:"value_base64"`
}

// startWithOps starts a koko server holding the shared Space ops.
func startWithOps(t *testing.T) *harness.Harness {
	t.Helper()
	h := harness.Start(t)
	h.Koko("space", "add", "ops").WantSuccess(t)
	return h
}

// secretNames runs secret ls --json, with args after it, on the device run
// runs commands on, and returns the Secrets it lists as <space>/<secret>.
func secretNames(t *testing.T, run func(args ...string) harness.Result, args ...string) []string {
	t.Helper()
	r := run(append([]string{"secret", "ls", "--json"}, args...)...)
	r.WantSuccess(t)
	var listed []struct {
		Space string `json:"space"`
		Name  string `json:"name"`
	}
	if err := json.Unmarshal([]byte(r.Stdout), &listed); err != nil {
		t.Fatalf("secret ls --json printed %q: %v", r.Stdout, err)
	}
	names := []string{}
	for _, s := range listed {
		names = append(names, s.Space+"/"+s.Name)
	}
	return names
}

// secret add stores a value piped to it, and secret show prints it back on
// stdout as stored, byte for byte, its trailing newline included, and
// nothing else.
func TestSecretAddFromStdinThenShow(t *testing.T) {
	h := startWithOps(t)
	h.KokoStdin("s3cret\n", "secret", "add", "ops/db-password").WantSuccess(t)
	r := h.Koko("secret", "show", "ops/db-password")
	r.WantSuccess(t)
	if r.Stdout != "s3cret\n" {
		t.Errorf("secret show printed %q, want the value as stored", r.Stdout)
	}
}

// Any bytes up to 64 KiB go in from stdin or from a file and come out of
// secret show the same: binary bytes, a multi-line key, exactly 64 KiB.
func TestSecretValuesRoundTripByteForByte(t *testing.T) {
	h := startWithOps(t)
	binary := []byte{0, 1, 2, 0xfe, 0xff, '\n', 0x80, 'k', 0}
	key := "-----BEGIN OPENSSH PRIVATE KEY-----\nb3BlbnNzaC1rZXktdjEAAAAA\r\nBG5vbmUAAAAEbm9uZQ==\n-----END OPENSSH PRIVATE KEY-----\n"
	full := bytes.Repeat([]byte("0123456789abcdef"), secretMaxSize/16)
	dir := t.TempDir()
	for _, tc := range []struct {
		name  string
		value []byte
	}{
		{"binary", binary},
		{"multi-line-key", []byte(key)},
		{"full", full},
	} {
		h.KokoStdin(string(tc.value), "secret", "add", "ops/"+tc.name+"-stdin").WantSuccess(t)
		file := filepath.Join(dir, tc.name)
		if err := os.WriteFile(file, tc.value, 0o600); err != nil {
			t.Fatal(err)
		}
		h.Koko("secret", "add", "ops/"+tc.name+"-file", "--from-file", file).WantSuccess(t)
		for _, from := range []string{"stdin", "file"} {
			r := h.Koko("secret", "show", "ops/"+tc.name+"-"+from)
			r.WantSuccess(t)
			if r.Stdout != string(tc.value) {
				t.Errorf("%s from %s: secret show printed %d bytes, want the %d bytes stored", tc.name, from, len(r.Stdout), len(tc.value))
			}
		}
	}
}

// A value of 64 KiB and one byte is refused before any call, from stdin and
// from a file, and nothing is stored.
func TestSecretAddRefusesAValueOver64KiB(t *testing.T) {
	h := startWithOps(t)
	over := strings.Repeat("x", secretMaxSize+1)
	file := filepath.Join(t.TempDir(), "over")
	if err := os.WriteFile(file, []byte(over), 0o600); err != nil {
		t.Fatal(err)
	}
	calls := h.Calls()
	r := h.KokoStdin(over, "secret", "add", "ops/big", "--json")
	msg := r.WantError(t, 2, "invalid_usage")
	harness.Golden(t, "secret-add-too-long.json", r.Stderr)
	if !strings.Contains(msg, "the value of ops/big is longer than 65536 bytes") {
		t.Errorf("message %q does not say the value is over the limit", msg)
	}
	h.Koko("secret", "add", "ops/big", "--from-file", file, "--json").WantError(t, 2, "invalid_usage")
	if n := h.Calls() - calls; n != 0 {
		t.Errorf("the refusals made %d call(s)", n)
	}
	if got := secretNames(t, h.Koko); len(got) != 0 {
		t.Errorf("secret ls = %q, want nothing stored", got)
	}
}

// On a terminal, secret add given neither stdin nor a file prompts for the
// value on stderr, reads it with echo off, and stores what was typed; the
// value never shows.
func TestSecretAddPromptsOnATerminal(t *testing.T) {
	h := startWithOps(t)
	term := harness.NewTerminal("typed-s3cret")
	r := h.KokoTerminal(term, "secret", "add", "ops/db-password", "--json")
	if r.Status != 0 || term.Reads() != 1 {
		t.Fatalf("exit status %d after %d reads with echo off, stderr %q", r.Status, term.Reads(), r.Stderr)
	}
	if r.Stderr != "Enter the value of ops/db-password: \n" {
		t.Errorf("stderr = %q, want the prompt alone", r.Stderr)
	}
	if strings.Contains(r.Stdout, "typed-s3cret") {
		t.Errorf("stdout shows the value typed: %q", r.Stdout)
	}
	if got := h.Koko("secret", "show", "ops/db-password").Stdout; got != "typed-s3cret" {
		t.Errorf("secret show printed %q, want the line typed", got)
	}
}

// Without a terminal and with nothing on stdin, secret add never waits: it
// exits 2 at once, naming the stdin and flag forms, and makes no call.
func TestSecretAddWithoutATerminalNamesTheForms(t *testing.T) {
	h := startWithOps(t)
	calls := h.Calls()
	r := h.Koko("secret", "add", "ops/db-password", "--json")
	msg := r.WantError(t, 2, "invalid_usage")
	harness.Golden(t, "secret-add-missing.json", r.Stderr)
	if !strings.Contains(msg, "give it on stdin or with --from-file <file>") {
		t.Errorf("message %q does not name the forms", msg)
	}
	if n := h.Calls() - calls; n != 0 {
		t.Errorf("the refusal made %d call(s)", n)
	}
}

// secret add --json prints the Secret it stored, without its value.
func TestSecretAddPrintsTheSecret(t *testing.T) {
	h := startWithOps(t)
	r := h.KokoStdin("s3cret\n", "secret", "add", "ops/db-password", "--json")
	r.WantSuccess(t)
	harness.Golden(t, "secret-add.json", r.Stdout)
}

func TestSecretAddForPeople(t *testing.T) {
	h := startWithOps(t)
	r := h.KokoStdin("s3cret\n", "secret", "add", "ops/db-password")
	r.WantSuccess(t)
	if !strings.HasPrefix(r.Stdout, "Added Secret ops/db-password (secret_") {
		t.Errorf("stdout = %q", r.Stdout)
	}
}

// secret add under a name the Space holds fails and leaves the value as it
// was; the same name in another Space is another Secret.
func TestSecretAddRefusesATakenName(t *testing.T) {
	h := startWithOps(t)
	h.KokoStdin("first", "secret", "add", "ops/db-password").WantSuccess(t)
	r := h.KokoStdin("second", "secret", "add", "ops/db-password", "--json")
	r.WantError(t, 1, "name_taken")
	harness.Golden(t, "secret-add-taken.json", r.Stderr)
	if got := h.Koko("secret", "show", "ops/db-password").Stdout; got != "first" {
		t.Errorf("secret show printed %q after the refusal, want the first value", got)
	}

	h.KokoStdin("mine", "secret", "add", harness.Owner+"/db-password").WantSuccess(t)
	if got := h.Koko("secret", "show", harness.Owner+"/db-password").Stdout; got != "mine" {
		t.Errorf("secret show %s/db-password printed %q", harness.Owner, got)
	}
}

// A Secret's name is 1 to 63 of a-z, 0-9, '-' and '_', starting with a
// letter or a digit; the koko server refuses any other.
func TestSecretNamesFollowTheNameRule(t *testing.T) {
	h := startWithOps(t)
	for _, n := range []string{"DB-password", "-db", "_db", "db.password", "db password", strings.Repeat("a", 64)} {
		r := h.KokoStdin("v", "secret", "add", "ops/"+n, "--json")
		r.WantError(t, 1, "invalid_name")
		h.Koko("secret", "show", "ops/"+n, "--json").WantError(t, 1, "invalid_name")
	}
	for _, n := range []string{"db_password", "9lives", "key-", strings.Repeat("a", 63)} {
		h.KokoStdin("v", "secret", "add", "ops/"+n).WantSuccess(t)
	}
	if got := secretNames(t, h.Koko, "ops"); len(got) != 4 {
		t.Errorf("secret ls ops = %q, want the 4 valid names only", got)
	}
}

// A Secret is always named with its Space: a name without one, or with an
// empty part, is refused with exit 2 before any call.
func TestASecretNameWithoutItsSpaceIsRefused(t *testing.T) {
	h := startWithOps(t)
	calls := h.Calls()
	r := h.Koko("secret", "show", "db-password", "--json")
	r.WantError(t, 2, "invalid_usage")
	harness.Golden(t, "secret-show-bare.json", r.Stderr)
	for _, args := range [][]string{
		{"secret", "add", "db-password"},
		{"secret", "add", "ops/"},
		{"secret", "show", "/db-password"},
		{"secret", "show", "ops/db/password"},
	} {
		h.KokoStdin("v", append(args, "--json")...).WantError(t, 2, "invalid_usage")
	}
	if n := h.Calls() - calls; n != 0 {
		t.Errorf("the refusals made %d call(s)", n)
	}
}

// A flag written before the command path is refused, so a harness rule for
// koko secret ls never matches koko --json secret show.
func TestSecretShowRefusesAFlagBeforeThePath(t *testing.T) {
	h := startWithOps(t)
	h.KokoStdin("s3cret", "secret", "add", "ops/db-password").WantSuccess(t)
	for _, args := range [][]string{
		{"--json", "secret", "show", "ops/db-password"},
		{"secret", "--json", "show", "ops/db-password"},
	} {
		r := h.Koko(args...)
		r.WantError(t, 2, "invalid_usage")
		if strings.Contains(r.Stderr, "s3cret") {
			t.Errorf("koko %s printed the value on stderr", strings.Join(args, " "))
		}
	}
}

// secret show --json prints the Secret with its value in base64.
func TestSecretShowPrintsTheValueInBase64(t *testing.T) {
	h := startWithOps(t)
	value := []byte("s3cret\n\x00\xff")
	h.KokoStdin(string(value), "secret", "add", "ops/db-password").WantSuccess(t)
	r := h.Koko("secret", "show", "ops/db-password", "--json")
	r.WantSuccess(t)
	harness.Golden(t, "secret-show.json", r.Stdout)
	var s secretShown
	if err := json.Unmarshal([]byte(r.Stdout), &s); err != nil {
		t.Fatal(err)
	}
	if s.Space != "ops" || s.Name != "db-password" || !bytes.Equal(s.ValueBase64, value) {
		t.Errorf("secret show --json = %+v, want ops/db-password with its value", s)
	}
}

// secret show of a name the Space does not hold fails with unknown_secret.
func TestSecretShowRefusesAnUnknownSecret(t *testing.T) {
	h := startWithOps(t)
	r := h.Koko("secret", "show", "ops/db-password", "--json")
	r.WantError(t, 1, "unknown_secret")
	harness.Golden(t, "secret-show-unknown.json", r.Stderr)
	h.Koko("secret", "show", "data/db-password", "--json").WantError(t, 1, "unknown_space")
}

// secret ls lists names only, sorted by Space and name: those of one Space,
// or of every Space the caller can see.
func TestSecretLsListsNamesOnly(t *testing.T) {
	h := startWithOps(t)
	h.Koko("space", "add", "data").WantSuccess(t)
	for _, n := range []string{"ops/token", "ops/db-password", "data/db-password", harness.Owner + "/note-key"} {
		h.KokoStdin("value-of-"+n, "secret", "add", n).WantSuccess(t)
	}
	r := h.Koko("secret", "ls", "--json")
	r.WantSuccess(t)
	harness.Golden(t, "secret-ls.json", r.Stdout)
	if strings.Contains(r.Stdout, "value-of-") || strings.Contains(r.Stdout, "value_base64") {
		t.Errorf("secret ls printed a value: %s", r.Stdout)
	}
	want := []string{harness.Owner + "/note-key", "data/db-password", "ops/db-password", "ops/token"}
	if got := secretNames(t, h.Koko); !slices.Equal(got, want) {
		t.Errorf("secret ls = %q, want %q", got, want)
	}
	r = h.Koko("secret", "ls", "ops", "--json")
	r.WantSuccess(t)
	harness.Golden(t, "secret-ls-space.json", r.Stdout)
	if got := secretNames(t, h.Koko, "ops"); !slices.Equal(got, []string{"ops/db-password", "ops/token"}) {
		t.Errorf("secret ls ops = %q", got)
	}
}

func TestSecretLsForPeople(t *testing.T) {
	h := startWithOps(t)
	if r := h.Koko("secret", "ls"); r.Status != 0 || r.Stdout != "No Secrets.\n" {
		t.Errorf("secret ls with none = %d %q", r.Status, r.Stdout)
	}
	h.KokoStdin("s3cret", "secret", "add", "ops/db-password").WantSuccess(t)
	r := h.Koko("secret", "ls")
	r.WantSuccess(t)
	lines := strings.Split(strings.TrimSpace(r.Stdout), "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "NAME") || !strings.HasPrefix(lines[1], "ops/db-password ") ||
		strings.Contains(r.Stdout, "s3cret") {
		t.Errorf("stdout = %q, want a NAME header and a row for ops/db-password", r.Stdout)
	}
}

// No flag of secret ls prints a value: it takes none but --json, which
// prints names only.
func TestNoFlagOfSecretLsPrintsAValue(t *testing.T) {
	h := startWithOps(t)
	h.KokoStdin("s3cret", "secret", "add", "ops/db-password").WantSuccess(t)
	for _, flag := range []string{"--values", "--show", "--reveal", "--all", "--from-file=x"} {
		r := h.Koko("secret", "ls", flag, "--json")
		r.WantError(t, 2, "invalid_usage")
		if strings.Contains(r.Stderr, "s3cret") {
			t.Errorf("secret ls %s printed the value", flag)
		}
	}
}

// secret ls of a Space that does not exist fails with unknown_space.
func TestSecretLsRefusesAnUnknownSpace(t *testing.T) {
	h := startWithOps(t)
	h.Koko("secret", "ls", "data", "--json").WantError(t, 1, "unknown_space")
}

// A Member acts on the Secrets of their Personal Space and of the Spaces
// they belong to, and sees none outside them: a Space they cannot act in
// answers as one that does not exist. The Owner, through a Device Session as
// over the Unix socket, acts on every Space's Secrets.
func TestSecretsFollowSpaceMembership(t *testing.T) {
	h := startWithOps(t)
	for _, u := range []string{"bob", "carol"} {
		h.Koko("user", "invite", u).WantSuccess(t)
	}
	h.Koko("space", "member", "add", "ops", "bob").WantSuccess(t)
	bob, carol, alice := h.Join("bob"), h.Join("carol"), h.Join(harness.Owner)

	bob.KokoStdin("ops-value", "secret", "add", "ops/db-password").WantSuccess(t)
	bob.KokoStdin("bob-value", "secret", "add", "bob/token").WantSuccess(t)
	carol.KokoStdin("carol-value", "secret", "add", "carol/token").WantSuccess(t)

	every := []string{"bob/token", "carol/token", "ops/db-password"}
	for _, tc := range []struct {
		who  string
		run  func(args ...string) harness.Result
		want []string
	}{
		{"bob", bob.Koko, []string{"bob/token", "ops/db-password"}},
		{"carol", carol.Koko, []string{"carol/token"}},
		{"the Owner's device", alice.Koko, every},
		{"the Local caller", h.Koko, every},
	} {
		if got := secretNames(t, tc.run); !slices.Equal(got, tc.want) {
			t.Errorf("secret ls for %s = %q, want %q", tc.who, got, tc.want)
		}
	}
	for _, run := range []func(args ...string) harness.Result{bob.Koko, alice.Koko, h.Koko} {
		if got := run("secret", "show", "ops/db-password").Stdout; got != "ops-value" {
			t.Errorf("secret show ops/db-password printed %q, want the value", got)
		}
	}
	if got := h.Koko("secret", "show", "carol/token").Stdout; got != "carol-value" {
		t.Errorf("the Local caller's secret show carol/token printed %q", got)
	}

	// carol is outside ops, which answers her as a Space that does not
	// exist; the access matrix compares the answers byte for byte.
	carol.Koko("secret", "show", "ops/db-password", "--json").WantError(t, 1, "unknown_space")
	carol.Koko("secret", "ls", "ops", "--json").WantError(t, 1, "unknown_space")
	carol.KokoStdin("v", "secret", "add", "ops/db-password", "--json").WantError(t, 1, "unknown_space")
	carol.KokoStdin("v", "secret", "add", "ops/new", "--json").WantError(t, 1, "unknown_space")
	if got := secretNames(t, h.Koko, "ops"); !slices.Equal(got, []string{"ops/db-password"}) {
		t.Errorf("after carol's refusals secret ls ops = %q", got)
	}
}

// Membership is read afresh on every call: a Member taken out of a Space
// loses its Secrets on their next command, on the same Device Session.
func TestAMemberTakenOutOfASpaceLosesItsSecretsAtOnce(t *testing.T) {
	h := startWithOps(t)
	h.Koko("user", "invite", "bob").WantSuccess(t)
	h.Koko("space", "member", "add", "ops", "bob").WantSuccess(t)
	bob := h.Join("bob")
	bob.KokoStdin("s3cret", "secret", "add", "ops/db-password").WantSuccess(t)
	bob.Koko("secret", "show", "ops/db-password").WantSuccess(t)

	h.Koko("space", "member", "rm", "ops", "bob").WantSuccess(t)
	r := bob.Koko("secret", "show", "ops/db-password", "--json")
	r.WantError(t, 1, "unknown_space")
	if strings.Contains(r.Stderr, "s3cret") {
		t.Errorf("the refusal printed the value: %q", r.Stderr)
	}
	if got := secretNames(t, bob.Koko); len(got) != 0 {
		t.Errorf("bob's secret ls = %q, want nothing", got)
	}
}

// marker is a Secret value made for the marker test, which looks for it in
// everything koko prints and writes.
const marker = "koko-marker-value-7Qz4Xw2Ln9"

// The marker test: a unique value appears only on the stdout of secret
// show. It never appears on any stderr, in any other stdout, in the koko
// server's log, or in any file koko writes under a temporary HOME, from
// every Secret command, with and without --json, and their failures, over
// the Unix socket and over a Device Connection.
func TestASecretValueShowsOnlyOnSecretShowsStdout(t *testing.T) {
	h := startWithOps(t)
	h.Koko("user", "invite", "bob").WantSuccess(t)
	h.Koko("space", "member", "add", "ops", "bob").WantSuccess(t)
	bob := h.Join("bob")
	file := filepath.Join(t.TempDir(), "value")
	if err := os.WriteFile(file, []byte(marker), 0o600); err != nil {
		t.Fatal(err)
	}
	tooLong := marker + strings.Repeat("x", secretMaxSize)
	forms := []string{marker, base64.StdEncoding.EncodeToString([]byte(marker))}

	type ran struct {
		line string
		r    harness.Result
		// show says the command is a secret show that succeeds, whose
		// stdout holds the value.
		show bool
	}
	var runs []ran
	for _, flags := range [][]string{nil, {"--json"}} {
		local := func(stdin string, args ...string) {
			args = append(args, flags...)
			runs = append(runs, ran{line: "local: " + strings.Join(args, " "), r: h.KokoStdin(stdin, args...)})
		}
		device := func(stdin string, args ...string) {
			args = append(args, flags...)
			runs = append(runs, ran{line: "bob: " + strings.Join(args, " "), r: bob.KokoStdin(stdin, args...)})
		}
		suffix := strings.ReplaceAll(strings.Join(flags, ""), "--", "-")
		// The writes, and their failures.
		local(marker, "secret", "add", "ops/stdin"+suffix)
		local("", "secret", "add", "ops/file"+suffix, "--from-file", file)
		runs = append(runs, ran{line: "terminal: secret add", r: h.KokoTerminal(harness.NewTerminal(marker),
			append([]string{"secret", "add", "ops/typed" + suffix}, flags...)...)})
		device(marker, "secret", "add", "ops/device"+suffix)
		local(marker, "secret", "add", "ops/stdin"+suffix)
		device(marker, "secret", "add", "ops/stdin"+suffix)
		local(marker, "secret", "add", "stdin")
		local("", "secret", "add", "ops/argument", marker)
		local("", "secret", "add", "ops/argument", "-"+marker)
		local(marker, "secret", "add", "ops/Bad-name")
		local(marker, "secret", "add", "nowhere/stdin")
		local(tooLong, "secret", "add", "ops/too-long")
		device(marker, "secret", "add", "alice/stdin")
		local(marker, "--json", "secret", "add", "ops/before")
		// The reads, and their failures.
		local("", "secret", "ls")
		local("", "secret", "ls", "ops")
		device("", "secret", "ls")
		local("", "secret", "ls", "ops", "--values")
		local("", "space", "show", "ops")
		local("", "secret", "show", "ops/nothing")
		local("", "secret", "show", "stdin")
		device("", "secret", "show", "alice/stdin")
		local("", "--json", "secret", "show", "ops/stdin")
		for _, run := range []func(args ...string) harness.Result{h.Koko, bob.Koko} {
			args := append([]string{"secret", "show", "ops/stdin" + suffix}, flags...)
			r := run(args...)
			r.WantSuccess(t)
			runs = append(runs, ran{line: strings.Join(args, " "), r: r, show: true})
		}
	}

	shown := 0
	for _, run := range runs {
		for _, form := range forms {
			if strings.Contains(run.r.Stderr, form) {
				t.Errorf("%s: stderr holds the value: %q", run.line, run.r.Stderr)
			}
			if !run.show && strings.Contains(run.r.Stdout, form) {
				t.Errorf("%s: stdout holds the value: %q", run.line, run.r.Stdout)
			}
		}
		if run.show && slices.ContainsFunc(forms, func(f string) bool { return strings.Contains(run.r.Stdout, f) }) {
			shown++
		}
	}
	if shown != 4 {
		t.Errorf("%d secret show runs printed the value, want all 4", shown)
	}
	for _, form := range forms {
		if strings.Contains(h.Log(), form) {
			t.Errorf("the koko server's log holds the value")
		}
	}
	for _, home := range []string{h.Home(), bob.Home()} {
		for path, content := range contents(t, home) {
			for _, form := range forms {
				if strings.Contains(content, form) {
					t.Errorf("%s, which koko wrote under HOME, holds the value", path)
				}
			}
		}
	}
}

// space show counts a Space's Secrets, and space rm refuses a Space while it
// holds one, naming it.
func TestASpaceThatHoldsASecretIsNotRemoved(t *testing.T) {
	h := startWithOps(t)
	h.KokoStdin("s3cret", "secret", "add", "ops/db-password").WantSuccess(t)
	h.KokoStdin("s3cret", "secret", "add", "ops/token").WantSuccess(t)
	if s := spaceShow(t, h.Koko, "ops"); s.Counts.Secrets != 2 || s.Counts.Hosts != 0 {
		t.Errorf("space show ops counts %+v, want 2 Secrets", s.Counts)
	}
	r := h.Koko("space", "rm", "ops", "--json")
	msg := r.WantError(t, 1, "space_not_empty")
	harness.Golden(t, "space-rm-holds-secrets.json", r.Stderr)
	if !strings.Contains(msg, "2 Secrets") {
		t.Errorf("message %q does not name the Secrets left", msg)
	}
	if got := secretNames(t, h.Koko, "ops"); len(got) != 2 {
		t.Errorf("after the refusal secret ls ops = %q", got)
	}
}
