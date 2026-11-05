package harness

import (
	"context"
	"io"
	"sync"
)

// Terminal is the fake terminal of S1 tests: a terminal on koko's stdin, set
// through the fake-terminal hook (cli.Runner.Terminal), where a person types
// one line at each prompt. koko reads it only with echo off, the one way a
// cli.Terminal reads.
type Terminal struct {
	mu sync.Mutex
	// lines are what the person types, one per prompt, in order.
	lines []string
	reads int
}

// NewTerminal returns a terminal where a person types lines, one at each
// prompt, without their line endings. Past the last one, a prompt reads end
// of file, as after Ctrl-D.
func NewTerminal(lines ...string) *Terminal {
	return &Terminal{lines: lines}
}

// ReadNoEcho reads the next line typed, with echo off.
func (t *Terminal) ReadNoEcho(context.Context) ([]byte, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.reads++
	if len(t.lines) == 0 {
		return nil, io.EOF
	}
	line := t.lines[0]
	t.lines = t.lines[1:]
	return []byte(line), nil
}

// Reads counts the lines koko has read from the terminal, every one with
// echo off.
func (t *Terminal) Reads() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.reads
}
