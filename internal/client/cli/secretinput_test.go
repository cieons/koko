package cli

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// secretTree holds commands with the secret inputs the specs name.
var secretTree = []Command{
	{
		Noun: "secret", Verb: "add", Tier: Reversible,
		Args:  []Arg{{Name: "secret", Qualifier: "space"}},
		Flags: []Flag{{Name: "from-file", Value: "file"}},
	},
	{
		Noun: "host", Verb: "enroll", Tier: Irreversible,
		Args:  []Arg{{Name: "host"}},
		Flags: []Flag{{Name: "password-stdin", Bool: true}, {Name: "key", Value: "file"}},
	},
}

// The secret inputs of secretTree: a Secret value from stdin or a file, an
// Enroll password from a stdin flag beside the command's other forms, and a
// key's passphrase from the prompt alone.
var (
	secretValue = SecretInput{What: "the value of ops/db-password", Stdin: true, FileFlag: "from-file"}
	password    = SecretInput{
		What: "the password of web-1", StdinFlag: "password-stdin",
		Others: []string{"--key <file>", "--secret <space>/<secret>"},
	}
	passphrase = SecretInput{What: "the passphrase of id_ed25519"}
)

// fakeTerminal is a terminal on stdin that a person types into.
type fakeTerminal struct {
	// typed is what the person types, line ending included; "" is Ctrl-D.
	typed string
	// reads counts the lines read from it, every one with echo off.
	reads int
}

func (f *fakeTerminal) ReadNoEcho(context.Context) ([]byte, error) {
	f.reads++
	line, _, ok := strings.Cut(f.typed, "\n")
	if !ok && line == "" {
		return nil, io.EOF
	}
	return []byte(line), nil
}

// untouchedStdin fails the test when it is read: a secret input leaves stdin
// alone unless a stdin form reads it.
type untouchedStdin struct{ t *testing.T }

func (s untouchedStdin) Read([]byte) (int, error) {
	s.t.Error("stdin was read")
	return 0, io.EOF
}

// readSecret reads s as the command line argv of secretTree would, with
// stdin, and with term on stdin when it is not nil. It returns the value,
// what was written to stderr, and the error.
func readSecret(t *testing.T, s SecretInput, stdin io.Reader, term *fakeTerminal, argv ...string) ([]byte, string, error) {
	t.Helper()
	in := inputFor(t, secretTree, argv...)
	var stderr bytes.Buffer
	in.inv.Stdin, in.inv.Stderr = stdin, &stderr
	if term != nil {
		in.runner.Terminal = term
	}
	value, err := in.ReadSecret(t.Context(), s)
	return value, stderr.String(), err
}

func TestSecretInputFromAFile(t *testing.T) {
	want := []byte("-----BEGIN KEY-----\n\x00\xff binary\n-----END KEY-----\n")
	file := filepath.Join(t.TempDir(), "key")
	if err := os.WriteFile(file, want, 0o600); err != nil {
		t.Fatal(err)
	}
	term := &fakeTerminal{typed: "typed\n"}
	got, stderr, err := readSecret(t, secretValue, untouchedStdin{t}, term, "secret", "add", "ops/db-password", "--from-file", file)
	if err != nil || !bytes.Equal(got, want) {
		t.Errorf("read %q, %v; want %q byte for byte", got, err, want)
	}
	if stderr != "" || term.reads != 0 {
		t.Errorf("stderr %q after %d terminal reads, want no prompt", stderr, term.reads)
	}
}

func TestSecretInputFromAFileThatCannotBeRead(t *testing.T) {
	file := filepath.Join(t.TempDir(), "missing")
	_, _, err := readSecret(t, secretValue, untouchedStdin{t}, nil, "secret", "add", "ops/db-password", "--from-file", file)
	wantRefused(t, err, "cannot read --from-file "+file+": no such file or directory")
}

// A Secret value is read from stdin whenever stdin is not a terminal: a
// pipe, or a file as here, byte for byte with its trailing newline.
func TestSecretInputFromStdin(t *testing.T) {
	want := []byte("s3cret\n")
	file := filepath.Join(t.TempDir(), "value")
	if err := os.WriteFile(file, want, 0o600); err != nil {
		t.Fatal(err)
	}
	stdin, err := os.Open(file)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stdin.Close() })
	got, stderr, err := readSecret(t, secretValue, stdin, nil, "secret", "add", "ops/db-password")
	if err != nil || !bytes.Equal(got, want) || stderr != "" {
		t.Errorf("read %q, %v, stderr %q; want %q and no prompt", got, err, stderr, want)
	}
}

func TestSecretInputFromAStdinFlag(t *testing.T) {
	got, _, err := readSecret(t, password, strings.NewReader("hunter2\n"), nil, "host", "enroll", "web-1", "--password-stdin")
	if err != nil || string(got) != "hunter2\n" {
		t.Errorf("read %q, %v; want %q", got, err, "hunter2\n")
	}
}

// A stdin flag given with a terminal on stdin would echo what is typed.
func TestSecretInputStdinFlagRefusesATerminal(t *testing.T) {
	term := &fakeTerminal{typed: "hunter2\n"}
	_, _, err := readSecret(t, password, untouchedStdin{t}, term, "host", "enroll", "web-1", "--password-stdin")
	wantRefused(t, err, "--password-stdin reads stdin, which is a terminal: pipe the password of web-1 in, "+
		"or leave out --password-stdin to type it with echo off")
	if term.reads != 0 {
		t.Errorf("the terminal was read %d times", term.reads)
	}
}

// With no form given and stdin a terminal, the reader prompts on stderr and
// reads one line with echo off, and never reads stdin as a pipe.
func TestSecretInputPromptsOnATerminal(t *testing.T) {
	for _, tc := range []struct {
		s    SecretInput
		argv []string
	}{
		{secretValue, []string{"secret", "add", "ops/db-password"}},
		{password, []string{"host", "enroll", "web-1"}},
		{passphrase, []string{"host", "enroll", "web-1", "--key", "id_ed25519"}},
	} {
		term := &fakeTerminal{typed: "s3cret\nnext line\n"}
		got, stderr, err := readSecret(t, tc.s, untouchedStdin{t}, term, tc.argv...)
		if err != nil || string(got) != "s3cret" {
			t.Errorf("%s: read %q, %v; want the line typed", tc.s.What, got, err)
		}
		if want := "Enter " + tc.s.What + ": \n"; stderr != want {
			t.Errorf("%s: stderr %q, want the prompt %q and nothing typed", tc.s.What, stderr, want)
		}
		if term.reads != 1 {
			t.Errorf("%s: %d lines read with echo off, want 1", tc.s.What, term.reads)
		}
	}
}

// Without a terminal, a secret input not given fails at once with exit 2,
// naming the forms that give it. Empty stdin, such as an agent's /dev/null,
// gives nothing.
func TestSecretInputWithoutATerminalFailsAtOnce(t *testing.T) {
	devNull, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = devNull.Close() })
	for _, tc := range []struct {
		s       SecretInput
		stdin   io.Reader
		argv    []string
		message string
	}{
		{
			secretValue, devNull,
			[]string{"secret", "add", "ops/db-password"},
			"the value of ops/db-password is missing: give it on stdin or with --from-file <file>; " +
				"koko prompts for it only when stdin is a terminal",
		},
		{
			password,
			untouchedStdin{t},
			[]string{"host", "enroll", "web-1"},
			"the password of web-1 is missing: give it with --password-stdin, --key <file> or --secret <space>/<secret>; " +
				"koko prompts for it only when stdin is a terminal",
		},
		{
			passphrase,
			untouchedStdin{t},
			[]string{"host", "enroll", "web-1", "--key", "id_ed25519"},
			"the passphrase of id_ed25519 is missing, and koko prompts for it only when stdin is a terminal",
		},
	} {
		_, stderr, err := readSecret(t, tc.s, tc.stdin, nil, tc.argv...)
		wantRefused(t, err, tc.message)
		if stderr != "" {
			t.Errorf("%s: stderr %q, want no prompt", tc.s.What, stderr)
		}
	}
}

// An empty secret input is refused from every form, so a pipe from a command
// that failed never stores or sends an empty value.
func TestSecretInputIsNeverEmpty(t *testing.T) {
	empty := filepath.Join(t.TempDir(), "empty")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, err := readSecret(t, secretValue, untouchedStdin{t}, nil, "secret", "add", "ops/db-password", "--from-file", empty)
	wantRefused(t, err, "the value of ops/db-password cannot be empty: --from-file "+empty+" is empty")

	_, _, err = readSecret(t, password, strings.NewReader(""), nil, "host", "enroll", "web-1", "--password-stdin")
	wantRefused(t, err, "the password of web-1 cannot be empty: stdin is empty")

	for _, typed := range []string{"\n", ""} {
		_, _, err = readSecret(t, passphrase, untouchedStdin{t}, &fakeTerminal{typed: typed}, "host", "enroll", "web-1", "--key", "k")
		wantRefused(t, err, "the passphrase of id_ed25519 cannot be empty")
	}
}
