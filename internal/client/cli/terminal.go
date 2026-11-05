package cli

import (
	"context"
	"os"

	"golang.org/x/term"
)

// Terminal is a terminal on koko's stdin, which a secret input's prompt
// reads. Stdin alone decides whether koko is on a terminal: the prompt reads
// its answer there, and a terminal elsewhere, such as the controlling
// terminal of an agent's harness, has nobody typing into it.
type Terminal interface {
	// ReadNoEcho reads one line typed with echo off and returns it without
	// its line ending. It returns when ctx ends, with ctx's error.
	ReadNoEcho(ctx context.Context) ([]byte, error)
}

// terminal returns the terminal on stdin: the fake-terminal hook when it is
// set, else stdin itself when it is a terminal, else nil.
func (in *Input) terminal() Terminal {
	if in.runner.Terminal != nil {
		return in.runner.Terminal
	}
	if f, ok := in.inv.Stdin.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		return stdinTerminal{fd: int(f.Fd())}
	}
	return nil
}

// stdinTerminal is the terminal on stdin's file descriptor. x/term keeps the
// per-OS code (termios on Linux and macOS, the console mode on Windows).
type stdinTerminal struct {
	fd int
}

func (t stdinTerminal) ReadNoEcho(ctx context.Context) ([]byte, error) {
	state, err := term.GetState(t.fd)
	if err != nil {
		return nil, err
	}
	type read struct {
		line []byte
		err  error
	}
	done := make(chan read, 1)
	go func() {
		line, err := term.ReadPassword(t.fd)
		done <- read{line, err}
	}()
	select {
	case r := <-done:
		return r.line, r.err
	case <-ctx.Done():
		// Ctrl-C ends ctx (cmd/koko catches the signal), but the read cannot
		// be interrupted: turn echo back on and leave the read to end with
		// the process.
		_ = term.Restore(t.fd, state)
		return nil, ctx.Err()
	}
}
