package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"
)

// SecretInput is one secret input of a command: a Secret value, an Enroll
// password or a key's passphrase. It is never an argument, so it never lands
// in shell history or ps (ADR 0019).
type SecretInput struct {
	// What names the input in its prompt and its refusals, such as "the value
	// of ops/db-password".
	What string
	// FileFlag, when set, is the value flag naming a file that holds the
	// input, such as "from-file".
	FileFlag string
	// StdinFlag, when set, is the boolean flag saying stdin holds the input,
	// such as "password-stdin".
	StdinFlag string
	// Stdin says stdin holds the input whenever it is not a terminal, as
	// `koko secret add` reads a value piped to it: stdin is then read to its
	// end, and empty stdin gives nothing.
	Stdin bool
	// Others are forms the command reads itself instead, such as "--secret
	// <space>/<secret>" beside an Enroll password, for the refusal to name.
	Others []string
	// Max, when above 0, is the longest input taken, in bytes: a longer one
	// is refused, never cut short. No file or stdin is read past one byte
	// more; a typed line is checked once it is read.
	Max int
}

// ReadSecret returns s byte for byte from the first form given: the file,
// the stdin flag, then stdin. With none it prompts with echo off when stdin is
// a terminal, and otherwise refuses at once with exit 2, naming the forms. It
// refuses an empty value, and one longer than s.Max. A command calls it
// before its first call, so that its refusals make none.
func (in *Input) ReadSecret(ctx context.Context, s SecretInput) ([]byte, error) {
	term := in.terminal()
	switch {
	case s.FileFlag != "" && in.Flag(s.FileFlag) != "":
		path := in.Flag(s.FileFlag)
		value, err := readFile(path, s.Max)
		if err != nil {
			if pe, ok := errors.AsType[*fs.PathError](err); ok {
				err = pe.Err
			}
			return nil, usagef("cannot read --%s %s: %v", s.FileFlag, path, err)
		}
		return s.check(value, "--"+s.FileFlag+" "+path)
	case s.StdinFlag != "" && in.Bool(s.StdinFlag):
		if term != nil {
			return nil, usagef("--%s reads stdin, which is a terminal: pipe %s in, or leave out --%s to type it with echo off",
				s.StdinFlag, s.What, s.StdinFlag)
		}
		value, err := in.readStdin(s.Max)
		if err != nil {
			return nil, err
		}
		return s.check(value, "stdin")
	case s.Stdin && term == nil:
		value, err := in.readStdin(s.Max)
		if err != nil {
			return nil, err
		}
		if len(value) > 0 {
			return s.check(value, "stdin")
		}
	case term != nil:
		return in.prompt(ctx, term, s)
	}
	if forms := s.forms(); forms != "" {
		return nil, usagef("%s is missing: give it %s; koko prompts for it only when stdin is a terminal", s.What, forms)
	}
	return nil, usagef("%s is missing, and koko prompts for it only when stdin is a terminal", s.What)
}

// prompt asks for s on stderr and reads one line from the terminal with echo
// off, then ends the prompt's line, which the Enter typed did not.
func (in *Input) prompt(ctx context.Context, term Terminal, s SecretInput) ([]byte, error) {
	_, _ = fmt.Fprintf(in.inv.Stderr, "Enter %s: ", s.What)
	value, err := term.ReadNoEcho(ctx)
	_, _ = fmt.Fprintln(in.inv.Stderr)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("read %s from the terminal: %w", s.What, err)
	}
	if len(value) == 0 {
		return nil, usagef("%s cannot be empty", s.What)
	}
	return s.check(value, "what was typed")
}

// readStdin reads stdin to its end, or to one byte past limit when limit is
// above 0.
func (in *Input) readStdin(limit int) ([]byte, error) {
	if in.inv.Stdin == nil {
		return nil, nil
	}
	value, err := readUpTo(in.inv.Stdin, limit)
	if err != nil {
		return nil, fmt.Errorf("read stdin: %w", err)
	}
	return value, nil
}

// readFile reads the file at path, as readUpTo reads.
func readFile(path string, limit int) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return readUpTo(f, limit)
}

// readUpTo reads r to its end, or, when limit is above 0, to one byte past
// limit, which is enough to refuse what is too long without reading an
// endless stream to its end.
func readUpTo(r io.Reader, limit int) ([]byte, error) {
	if limit > 0 {
		r = io.LimitReader(r, int64(limit)+1)
	}
	return io.ReadAll(r)
}

// check refuses a value read from source that is empty or longer than
// s.Max.
func (s SecretInput) check(value []byte, source string) ([]byte, error) {
	switch {
	case len(value) == 0:
		return nil, usagef("%s cannot be empty: %s is empty", s.What, source)
	case s.Max > 0 && len(value) > s.Max:
		return nil, usagef("%s is longer than %d bytes, the most it may be", s.What, s.Max)
	}
	return value, nil
}

// forms names the forms that give s, such as "on stdin or with --from-file
// <file>".
func (s SecretInput) forms() string {
	var flags []string
	if s.StdinFlag != "" {
		flags = append(flags, "--"+s.StdinFlag)
	}
	if s.FileFlag != "" {
		flags = append(flags, "--"+s.FileFlag+" <file>")
	}
	flags = append(flags, s.Others...)
	var forms []string
	if s.Stdin {
		forms = append(forms, "on stdin")
	}
	if len(flags) > 0 {
		forms = append(forms, "with "+joinOr(flags))
	}
	return strings.Join(forms, " or ")
}

// joinOr joins words as "a, b or c".
func joinOr(words []string) string {
	if len(words) < 2 {
		return strings.Join(words, "")
	}
	return strings.Join(words[:len(words)-1], ", ") + " or " + words[len(words)-1]
}
