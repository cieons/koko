package cli_test

import (
	"encoding/json/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cieons/koko/internal/harness"
)

// secretRecord is a Secret as secret ls --json and the writes print it.
type secretRecord struct {
	ID        string `json:"id"`
	Space     string `json:"space"`
	Name      string `json:"name"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// decodeSecret decodes the Secret a write printed with --json.
func decodeSecret(t *testing.T, r harness.Result) secretRecord {
	t.Helper()
	r.WantSuccess(t)
	var s secretRecord
	if err := json.Unmarshal([]byte(r.Stdout), &s); err != nil {
		t.Fatalf("decode %q: %v", r.Stdout, err)
	}
	return s
}

// secret set replaces a Secret's value, from stdin or from a file, and
// secret show then prints the new value as stored.
func TestSecretSetReplacesTheValue(t *testing.T) {
	t.Parallel()
	h := startWithOps(t)
	h.KokoStdin("old-s3cret", "secret", "add", "ops/db-password").WantSuccess(t)
	h.KokoStdin("new-s3cret\n", "secret", "set", "ops/db-password").WantSuccess(t)
	if got := h.Koko("secret", "show", "ops/db-password").Stdout; got != "new-s3cret\n" {
		t.Errorf("secret show printed %q after secret set, want the new value", got)
	}

	key := "-----BEGIN OPENSSH PRIVATE KEY-----\nb3BlbnNzaC1rZXktdjEAAAAA\x00\xff\n-----END OPENSSH PRIVATE KEY-----\n"
	file := filepath.Join(t.TempDir(), "key")
	if err := os.WriteFile(file, []byte(key), 0o600); err != nil {
		t.Fatal(err)
	}
	h.Koko("secret", "set", "ops/db-password", "--from-file", file).WantSuccess(t)
	if got := h.Koko("secret", "show", "ops/db-password").Stdout; got != key {
		t.Errorf("secret show printed %q after secret set --from-file, want the file's bytes", got)
	}
}

// secret set --json prints the Secret it changed, without its value, its id
// and created_at as they were and updated_at moved to the change.
func TestSecretSetPrintsTheSecret(t *testing.T) {
	t.Parallel()
	h := startWithOps(t)
	added := decodeSecret(t, h.KokoStdin("old-s3cret", "secret", "add", "ops/db-password", "--json"))
	h.Advance(time.Hour)
	r := h.KokoStdin("new-s3cret", "secret", "set", "ops/db-password", "--json")
	set := decodeSecret(t, r)
	harness.Golden(t, "secret-set.json", r.Stdout)
	if strings.Contains(r.Stdout, "s3cret") || strings.Contains(r.Stdout, "value") {
		t.Errorf("secret set --json printed a value: %s", r.Stdout)
	}
	if set.ID != added.ID || set.CreatedAt != added.CreatedAt || set.UpdatedAt != h.Now().UTC().Format(time.RFC3339) {
		t.Errorf("secret set printed %+v, want the Secret %+v updated at %s", set, added, h.Now().UTC().Format(time.RFC3339))
	}
}

func TestSecretSetForPeople(t *testing.T) {
	t.Parallel()
	h := startWithOps(t)
	h.KokoStdin("old-s3cret", "secret", "add", "ops/db-password").WantSuccess(t)
	r := h.KokoStdin("new-s3cret", "secret", "set", "ops/db-password")
	r.WantSuccess(t)
	if !strings.HasPrefix(r.Stdout, "Set the value of Secret ops/db-password (secret_") || strings.Contains(r.Stdout, "s3cret") {
		t.Errorf("stdout = %q", r.Stdout)
	}
}

// secret set on a Secret that does not exist fails and adds none, so a typo
// never makes a second Secret.
func TestSecretSetRefusesAnUnknownSecret(t *testing.T) {
	t.Parallel()
	h := startWithOps(t)
	h.KokoStdin("s3cret", "secret", "add", "ops/db-password").WantSuccess(t)
	r := h.KokoStdin("s3cret", "secret", "set", "ops/db-pasword", "--json")
	r.WantError(t, 1, "unknown_secret")
	harness.Golden(t, "secret-set-unknown.json", r.Stderr)
	h.KokoStdin("s3cret", "secret", "set", "data/db-password", "--json").WantError(t, 1, "unknown_space")
	if got := secretNames(t, h.Koko); !slices.Equal(got, []string{"ops/db-password"}) {
		t.Errorf("secret ls = %q, want ops/db-password alone", got)
	}
}

// secret set reads its value as secret add does: a value over 64 KiB, or
// none without a terminal, is refused with exit 2 before any call, and the
// value stays as it was.
func TestSecretSetReadsItsValueAsSecretAddDoes(t *testing.T) {
	t.Parallel()
	h := startWithOps(t)
	h.KokoStdin("s3cret", "secret", "add", "ops/db-password").WantSuccess(t)
	calls := h.Calls()
	r := h.KokoStdin(strings.Repeat("x", secretMaxSize+1), "secret", "set", "ops/db-password", "--json")
	if msg := r.WantError(t, 2, "invalid_usage"); !strings.Contains(msg, "longer than 65536 bytes") {
		t.Errorf("message %q does not say the value is over the limit", msg)
	}
	r = h.Koko("secret", "set", "ops/db-password", "--json")
	if msg := r.WantError(t, 2, "invalid_usage"); !strings.Contains(msg, "give it on stdin or with --from-file <file>") {
		t.Errorf("message %q does not name the forms", msg)
	}
	if n := h.Calls() - calls; n != 0 {
		t.Errorf("the refusals made %d call(s)", n)
	}

	term := harness.NewTerminal("typed-s3cret")
	r = h.KokoTerminal(term, "secret", "set", "ops/db-password")
	if r.Status != 0 || term.Reads() != 1 || r.Stderr != "Enter the value of ops/db-password: \n" {
		t.Fatalf("on a terminal: exit status %d after %d reads with echo off, stderr %q", r.Status, term.Reads(), r.Stderr)
	}
	if got := h.Koko("secret", "show", "ops/db-password").Stdout; got != "typed-s3cret" {
		t.Errorf("secret show printed %q, want the line typed", got)
	}
}

// secret set refuses a value written as an argument with exit 2, before any
// call, without repeating it, and the value stays as it was.
func TestSecretSetRefusesAValueWrittenAsAnArgument(t *testing.T) {
	t.Parallel()
	h := startWithOps(t)
	h.KokoStdin("s3cret", "secret", "add", "ops/db-password").WantSuccess(t)
	calls := h.Calls()
	r := h.Koko("secret", "set", "ops/db-password", "hunter2", "--json")
	r.WantError(t, 2, "invalid_usage")
	harness.Golden(t, "secret-set-argument.json", r.Stderr)
	if strings.Contains(r.Stderr, "hunter2") {
		t.Errorf("the refusal repeats the value: %q", r.Stderr)
	}
	if n := h.Calls() - calls; n != 0 {
		t.Errorf("the refusal made %d call(s)", n)
	}
	if got := h.Koko("secret", "show", "ops/db-password").Stdout; got != "s3cret" {
		t.Errorf("secret show printed %q after the refusal, want the value as it was", got)
	}
}

// The last secret set wins: two callers set one Secret in turn, and it
// holds the later value. P0 has no revision check.
func TestTheLastSecretSetWins(t *testing.T) {
	t.Parallel()
	h := startWithOps(t)
	h.Koko("user", "invite", "bob").WantSuccess(t)
	h.Koko("space", "member", "add", "ops", "bob").WantSuccess(t)
	bob := h.Join("bob")
	h.KokoStdin("first", "secret", "add", "ops/db-password").WantSuccess(t)
	bob.KokoStdin("bob's", "secret", "set", "ops/db-password").WantSuccess(t)
	h.KokoStdin("the owner's", "secret", "set", "ops/db-password").WantSuccess(t)
	if got := bob.Koko("secret", "show", "ops/db-password").Stdout; got != "the owner's" {
		t.Errorf("secret show printed %q, want the later value", got)
	}
}

// secret rename gives a Secret a new name in its Space: it keeps its id and
// its value, and the old name names nothing.
func TestSecretRenameKeepsTheSecret(t *testing.T) {
	t.Parallel()
	h := startWithOps(t)
	added := decodeSecret(t, h.KokoStdin("s3cret", "secret", "add", "ops/db-password", "--json"))
	h.Advance(time.Hour)
	r := h.Koko("secret", "rename", "ops/db-password", "db-pass", "--json")
	renamed := decodeSecret(t, r)
	harness.Golden(t, "secret-rename.json", r.Stdout)
	if renamed.ID != added.ID || renamed.Space != "ops" || renamed.Name != "db-pass" || renamed.CreatedAt != added.CreatedAt ||
		renamed.UpdatedAt != h.Now().UTC().Format(time.RFC3339) {
		t.Errorf("secret rename printed %+v, want %+v as ops/db-pass, updated now", renamed, added)
	}
	if got := h.Koko("secret", "show", "ops/db-pass").Stdout; got != "s3cret" {
		t.Errorf("secret show ops/db-pass printed %q, want the value", got)
	}
	h.Koko("secret", "show", "ops/db-password", "--json").WantError(t, 1, "unknown_secret")
}

func TestSecretRenameForPeople(t *testing.T) {
	t.Parallel()
	h := startWithOps(t)
	h.KokoStdin("s3cret", "secret", "add", "ops/db-password").WantSuccess(t)
	r := h.Koko("secret", "rename", "ops/db-password", "db-pass")
	r.WantSuccess(t)
	if !strings.HasPrefix(r.Stdout, "Renamed Secret ops/db-password to ops/db-pass (secret_") {
		t.Errorf("stdout = %q", r.Stdout)
	}
}

// secret rename refuses a name the Space holds, its own included, a name
// that breaks the name rule, and a Secret that does not exist, and changes
// nothing.
func TestSecretRenameRefusals(t *testing.T) {
	t.Parallel()
	h := startWithOps(t)
	h.KokoStdin("first", "secret", "add", "ops/db-password").WantSuccess(t)
	h.KokoStdin("second", "secret", "add", "ops/token").WantSuccess(t)
	r := h.Koko("secret", "rename", "ops/db-password", "token", "--json")
	r.WantError(t, 1, "name_taken")
	harness.Golden(t, "secret-rename-taken.json", r.Stderr)
	h.Koko("secret", "rename", "ops/db-password", "db-password", "--json").WantError(t, 1, "name_taken")
	h.Koko("secret", "rename", "ops/db-password", "DB", "--json").WantError(t, 1, "invalid_name")
	h.Koko("secret", "rename", "ops/nothing", "something", "--json").WantError(t, 1, "unknown_secret")
	for name, want := range map[string]string{"ops/db-password": "first", "ops/token": "second"} {
		if got := h.Koko("secret", "show", name).Stdout; got != want {
			t.Errorf("secret show %s printed %q after the refusals, want %q", name, got, want)
		}
	}
	if got := secretNames(t, h.Koko); !slices.Equal(got, []string{"ops/db-password", "ops/token"}) {
		t.Errorf("secret ls = %q after the refusals", got)
	}
}

// secret move moves a Secret to another Space under its name: it keeps its
// id and its value, and the old Space no longer holds it. A move to its own
// Space changes nothing.
func TestSecretMoveKeepsTheSecret(t *testing.T) {
	t.Parallel()
	h := startWithOps(t)
	h.Koko("space", "add", "data").WantSuccess(t)
	added := decodeSecret(t, h.KokoStdin("s3cret", "secret", "add", "ops/db-password", "--json"))
	h.Advance(time.Hour)
	r := h.Koko("secret", "move", "ops/db-password", "data", "--json")
	moved := decodeSecret(t, r)
	harness.Golden(t, "secret-move.json", r.Stdout)
	if moved.ID != added.ID || moved.Space != "data" || moved.Name != "db-password" || moved.CreatedAt != added.CreatedAt ||
		moved.UpdatedAt != h.Now().UTC().Format(time.RFC3339) {
		t.Errorf("secret move printed %+v, want %+v in data, updated now", moved, added)
	}
	if got := h.Koko("secret", "show", "data/db-password").Stdout; got != "s3cret" {
		t.Errorf("secret show data/db-password printed %q, want the value", got)
	}
	h.Koko("secret", "show", "ops/db-password", "--json").WantError(t, 1, "unknown_secret")

	h.Advance(time.Hour)
	if same := decodeSecret(t, h.Koko("secret", "move", "data/db-password", "data", "--json")); same != moved {
		t.Errorf("a move to its own Space printed %+v, want the Secret unchanged: %+v", same, moved)
	}
}

func TestSecretMoveForPeople(t *testing.T) {
	t.Parallel()
	h := startWithOps(t)
	h.KokoStdin("s3cret", "secret", "add", "ops/db-password").WantSuccess(t)
	r := h.Koko("secret", "move", "ops/db-password", harness.Owner)
	r.WantSuccess(t)
	if !strings.HasPrefix(r.Stdout, "Moved Secret ops/db-password to "+harness.Owner+"/db-password (secret_") {
		t.Errorf("stdout = %q", r.Stdout)
	}
}

// secret move refuses a target Space that holds the name, and a Secret or a
// Space that does not exist, and changes nothing.
func TestSecretMoveRefusals(t *testing.T) {
	t.Parallel()
	h := startWithOps(t)
	h.Koko("space", "add", "data").WantSuccess(t)
	h.KokoStdin("ops-value", "secret", "add", "ops/db-password").WantSuccess(t)
	h.KokoStdin("data-value", "secret", "add", "data/db-password").WantSuccess(t)
	r := h.Koko("secret", "move", "ops/db-password", "data", "--json")
	r.WantError(t, 1, "name_taken")
	harness.Golden(t, "secret-move-taken.json", r.Stderr)
	h.Koko("secret", "move", "ops/db-password", "nowhere", "--json").WantError(t, 1, "unknown_space")
	h.Koko("secret", "move", "ops/nothing", "data", "--json").WantError(t, 1, "unknown_secret")
	for name, want := range map[string]string{"ops/db-password": "ops-value", "data/db-password": "data-value"} {
		if got := h.Koko("secret", "show", name).Stdout; got != want {
			t.Errorf("secret show %s printed %q after the refusals, want %q", name, got, want)
		}
	}
}

// A move needs the caller to act in both Spaces: a Member who can act in
// the source only, or in the target only, is refused as for a Space that
// does not exist, and the Secret stays; one who can act in both moves it.
func TestSecretMoveNeedsBothSpaces(t *testing.T) {
	t.Parallel()
	h := startWithOps(t)
	h.Koko("space", "add", "data").WantSuccess(t)
	h.Koko("user", "invite", "bob").WantSuccess(t)
	h.Koko("space", "member", "add", "ops", "bob").WantSuccess(t)
	bob := h.Join("bob")
	h.KokoStdin("ops-value", "secret", "add", "ops/db-password").WantSuccess(t)
	h.KokoStdin("data-value", "secret", "add", "data/token").WantSuccess(t)

	r := bob.Koko("secret", "move", "ops/db-password", "data", "--json")
	if msg := r.WantError(t, 1, "unknown_space"); !strings.Contains(msg, `"data"`) {
		t.Errorf("the source only: message %q does not name data", msg)
	}
	r = bob.Koko("secret", "move", "data/token", "ops", "--json")
	if msg := r.WantError(t, 1, "unknown_space"); !strings.Contains(msg, `"data"`) {
		t.Errorf("the target only: message %q does not name data", msg)
	}
	want := []string{"data/token", "ops/db-password"}
	if got := secretNames(t, h.Koko); !slices.Equal(got, want) {
		t.Errorf("after the refusals secret ls = %q, want %q", got, want)
	}

	bob.Koko("secret", "move", "ops/db-password", "bob").WantSuccess(t)
	if got := bob.Koko("secret", "show", "bob/db-password").Stdout; got != "ops-value" {
		t.Errorf("both Spaces: secret show bob/db-password printed %q, want the value", got)
	}
	h.Koko("space", "member", "add", "data", "bob").WantSuccess(t)
	bob.Koko("secret", "move", "data/token", "ops").WantSuccess(t)
	if got := secretNames(t, h.Koko); !slices.Equal(got, []string{"bob/db-password", "ops/token"}) {
		t.Errorf("after the moves secret ls = %q", got)
	}
}

// secret rm deletes the Secret and its value: it names nothing afterwards,
// is listed nowhere, and its Space holds nothing. --json prints the Secret
// it deleted, as it was, without its value.
func TestSecretRmDeletesTheSecret(t *testing.T) {
	t.Parallel()
	h := startWithOps(t)
	added := decodeSecret(t, h.KokoStdin("s3cret", "secret", "add", "ops/db-password", "--json"))
	r := h.Koko("secret", "rm", "ops/db-password", "--json")
	if removed := decodeSecret(t, r); removed != added {
		t.Errorf("secret rm printed %+v, want the Secret as it was: %+v", removed, added)
	}
	harness.Golden(t, "secret-rm.json", r.Stdout)
	if strings.Contains(r.Stdout, "s3cret") || strings.Contains(r.Stdout, "value") {
		t.Errorf("secret rm --json printed a value: %s", r.Stdout)
	}
	h.Koko("secret", "show", "ops/db-password", "--json").WantError(t, 1, "unknown_secret")
	if got := secretNames(t, h.Koko); len(got) != 0 {
		t.Errorf("secret ls = %q after secret rm, want nothing", got)
	}
	if s := spaceShow(t, h.Koko, "ops"); s.Counts.Secrets != 0 {
		t.Errorf("space show ops counts %d Secrets after secret rm", s.Counts.Secrets)
	}
	r = h.Koko("secret", "rm", "ops/db-password", "--json")
	r.WantError(t, 1, "unknown_secret")
	harness.Golden(t, "secret-rm-unknown.json", r.Stderr)
	h.Koko("space", "rm", "ops").WantSuccess(t)
}

func TestSecretRmForPeople(t *testing.T) {
	t.Parallel()
	h := startWithOps(t)
	h.KokoStdin("s3cret", "secret", "add", "ops/db-password").WantSuccess(t)
	r := h.Koko("secret", "rm", "ops/db-password")
	r.WantSuccess(t)
	if !strings.HasPrefix(r.Stdout, "Removed Secret ops/db-password (secret_") {
		t.Errorf("stdout = %q", r.Stdout)
	}
}

// secret rename, move and rm refuse a command line that names a Secret
// without its Space, or misses an argument, with exit 2 before any call.
func TestSecretChangesRefuseABadCommandLine(t *testing.T) {
	t.Parallel()
	h := startWithOps(t)
	calls := h.Calls()
	for _, tc := range []struct {
		golden string
		args   []string
	}{
		{"secret-rename-missing.json", []string{"secret", "rename", "ops/db-password"}},
		{"secret-move-bare.json", []string{"secret", "move", "db-password", "data"}},
		{"secret-rm-bare.json", []string{"secret", "rm", "db-password"}},
	} {
		r := h.Koko(append(tc.args, "--json")...)
		r.WantError(t, 2, "invalid_usage")
		harness.Golden(t, tc.golden, r.Stderr)
	}
	if n := h.Calls() - calls; n != 0 {
		t.Errorf("the refusals made %d call(s)", n)
	}
}

// The Owner sees the Secrets of a Removed User's former Personal Space, and
// moves them out or deletes them; nobody else sees them.
func TestTheOwnerMovesTheSecretsOfARemovedUser(t *testing.T) {
	t.Parallel()
	h := startWithOps(t)
	h.Koko("user", "invite", "bob").WantSuccess(t)
	h.Koko("user", "invite", "carol").WantSuccess(t)
	bob, carol, alice := h.Join("bob"), h.Join("carol"), h.Join(harness.Owner)
	bob.KokoStdin("bob-token", "secret", "add", "bob/token").WantSuccess(t)
	bob.KokoStdin("bob-key", "secret", "add", "bob/key").WantSuccess(t)
	h.Koko("user", "remove", "bob").WantSuccess(t)

	want := []string{"bob/key", "bob/token"}
	for _, tc := range []struct {
		who string
		run func(args ...string) harness.Result
	}{{"the Owner's device", alice.Koko}, {"the Local caller", h.Koko}} {
		if got := secretNames(t, tc.run); !slices.Equal(got, want) {
			t.Errorf("secret ls for %s = %q, want %q", tc.who, got, want)
		}
	}
	if got := secretNames(t, carol.Koko); len(got) != 0 {
		t.Errorf("carol's secret ls = %q, want nothing", got)
	}
	carol.Koko("secret", "move", "bob/token", "carol", "--json").WantError(t, 1, "unknown_space")

	alice.Koko("secret", "move", "bob/token", "ops").WantSuccess(t)
	if got := alice.Koko("secret", "show", "ops/token").Stdout; got != "bob-token" {
		t.Errorf("secret show ops/token printed %q, want the value moved out", got)
	}
	h.Koko("secret", "rm", "bob/key").WantSuccess(t)
	if got := secretNames(t, h.Koko); !slices.Equal(got, []string{"ops/token"}) {
		t.Errorf("secret ls = %q, want ops/token alone", got)
	}
}
