// Package vt keeps the screen of a web terminal's SSH Session in the koko
// server: Ghostty's VT core, libghostty-vt's WebAssembly module, run by
// wazero, so kokod stays one binary without cgo (ADR 0058). A Terminal
// takes the shell's output, answers the queries in it, and gives a
// Snapshot that draws the terminal on a page's cleared emulator.
package vt

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/experimental"
)

// Scrollback is the lines of history a Terminal keeps at least, beyond its
// screen.
const Scrollback = 1000

// MaxCols and MaxRows are the largest grid a Terminal takes: New and
// Resize hold a larger size to them, and the pty is held to the same
// (spec #213, maxGrid).
const (
	MaxCols = 500
	MaxRows = 200
)

// memoryBound is the most linear memory a core may hold: the most ordinary
// output measured at MaxCols by MaxRows held, colours in every cell and a
// resize and a snapshot over vim after it, about 81 MiB, with a margin
// (docs/subsystems/vt.md). A core past it has failed: a flood of combining
// marks or of long hyperlinks, which ordinary output never is, reaches it
// in about a second.
const memoryBound = 96 << 20

// pageBytes is more than one page of Ghostty's grid holds, about 210 KiB
// (page.zig, std_capacity), for historyLimit.
const pageBytes = 256 << 10

// historyLimit is the line limit that keeps at least Scrollback lines of
// history at cols columns. Ghostty drops history a whole page at a time,
// once it passes the limit, and a page holds more rows the narrower the
// grid, so the limit is Scrollback and two pages' rows more: about two
// pages of memory, under 0.5 MiB.
func historyLimit(cols int) uint32 {
	const cellBytes = 8 // a Cell, and a Row's header per row
	return uint32(Scrollback + 2*pageBytes/(cellBytes*(cols+1)))
}

// RGB is a colour, as OSC 10 and 11 answer with.
type RGB struct{ R, G, B uint8 }

// Terminal is one SSH Session's emulator: an instance of Ghostty's module
// holding one Ghostty terminal. One goroutine uses it; another may only
// cancel the context it was made with, which ends a Write within its next
// 4 KiB and any other call at its next step into the core.
//
// A call that fails, a cancelled context or a core past memoryBound among
// them, closes the instance: every later call returns the same error, and
// the caller makes a new Terminal.
type Terminal struct {
	ctx     context.Context // carries the Terminal to the callbacks
	ghostty api.Module
	bridge  api.Module
	mem     api.Memory
	err     error // why the Terminal failed, once it has

	handle    uint32 // the GhosttyTerminal
	callbacks uint32 // the bridge's first index in the function table
	scratch   uint32 // scratchSize bytes for arguments and results
	input     uint32 // inputSize bytes that Write copies output through

	cols, rows int

	functions map[string]api.Function // looked up once each

	replies []byte // what write_pty gave during a call
	sink    []byte // what a GhosttyWriter gave during a call
	source  []byte // what a GhosttyReader gives during a call

	// primary is the primary screen behind the alternate one as Snapshot
	// last wrote it, kept while the alternate screen stays, since copying
	// it takes long (primaryBehind). Output that may leave the alternate
	// screen drops it, as a resize does; tail is the end of the last
	// output, for a sequence cut between two.
	primary []byte
	tail    []byte
	// savesCursor is set once the output has saved a cursor (DECSC), which
	// only a copy of the terminal can read back for Snapshot. Copying takes
	// long for a screen full of combining marks, so Snapshot copies only
	// for a terminal whose output saved one.
	savesCursor bool
}

// scratch holds a call's arguments and results from its start, and the
// writer's and reader's structs at these offsets, which no call's
// arguments reach.
const (
	scratchWriter = 16
	scratchReader = 24
)

const (
	scratchSize = 1024
	// inputSize is the most output one call into the core takes: a call
	// cannot be stopped once it runs, and 4 KiB of the slowest output
	// seen, screen alignment tests (DECALN) at the largest grid, takes
	// about 0.15 s.
	inputSize = 4 << 10
	// continuationMax is the longest unfinished sequence Ghostty keeps for
	// Snapshot: an OSC 52 copy of a large selection fits.
	continuationMax = 1 << 20
)

// New starts a terminal of cols by rows, held to MaxCols by MaxRows, with
// Scrollback lines of history. Cancelling ctx ends the call under way, as
// soon as it next steps into the core, and fails the Terminal.
func New(ctx context.Context, cols, rows int) (*Terminal, error) {
	if cols < 1 || rows < 1 {
		return nil, fmt.Errorf("new terminal: size %dx%d out of range", cols, rows)
	}
	t, err := instantiate(ctx, min(cols, MaxCols), min(rows, MaxRows))
	if err != nil {
		return nil, fmt.Errorf("new terminal: %w", err)
	}
	if err := t.start(); err != nil {
		_ = t.Close()
		return nil, fmt.Errorf("new terminal: %w", err)
	}
	return t, nil
}

// instantiate makes an instance of Ghostty's module with the bridge's
// callbacks in its table, and no terminal in it yet.
func instantiate(ctx context.Context, cols, rows int) (*Terminal, error) {
	e, err := loadEngine()
	if err != nil {
		return nil, err
	}
	t := &Terminal{cols: cols, rows: rows}
	t.ctx = context.WithValue(ctx, terminalKey{}, t)
	if err := t.instantiate(e); err != nil {
		_ = t.Close()
		return nil, err
	}
	return t, nil
}

func (t *Terminal) instantiate(e *engine) error {
	var err error
	if t.ghostty, err = e.runtime.InstantiateModule(t.ctx, e.ghostty, wazero.NewModuleConfig().WithName("")); err != nil {
		return fmt.Errorf("instantiate libghostty-vt: %w", err)
	}
	t.mem = t.ghostty.Memory()
	// The bridge imports the table of this instance, which has no name of
	// its own, as every Terminal's instance would otherwise share one.
	resolve := experimental.WithImportResolver(t.ctx, func(name string) api.Module {
		if name == "ghostty" {
			return t.ghostty
		}
		return nil
	})
	if t.bridge, err = e.runtime.InstantiateModule(resolve, e.bridge, wazero.NewModuleConfig().WithName("")); err != nil {
		return fmt.Errorf("instantiate the bridge: %w", err)
	}
	base, err := t.bridge.ExportedFunction("install").Call(t.ctx)
	if err != nil {
		return fmt.Errorf("install the callbacks: %w", err)
	}
	if t.callbacks = uint32(base[0]); int32(t.callbacks) < 0 {
		return errors.New("install the callbacks: the function table did not grow")
	}
	if t.scratch, err = t.alloc(scratchSize); err != nil {
		return err
	}
	t.input, err = t.alloc(inputSize)
	return err
}

// start makes the instance's terminal.
func (t *Terminal) start() error {
	if err := t.check("ghostty_terminal_new", 0, uint64(t.scratch), uint64(t.cols), uint64(t.rows)); err != nil {
		return err
	}
	t.handle = t.u32(t.scratch)
	return t.configure()
}

// configure sets what every core of koko's has: its callbacks, its
// history, no Kitty images, and the unfinished sequence kept.
func (t *Terminal) configure() error {
	if err := t.setCallbacks(); err != nil {
		return err
	}
	for _, o := range []struct {
		opt   int
		value []byte
	}{
		// The line limit alone bounds the history; the byte limit's
		// default of one page would otherwise keep far fewer.
		{optScrollbackMaxBytes, u32(0xffffffff)},
		{optScrollbackMaxLines, u32(historyLimit(t.cols))},
		// An APC sequence is buffered up to this, not the 65 MiB default,
		// so a program cannot grow a core with one.
		{optAPCMaxBytes, u32(64 << 10)},
		// Ghostty keeps an escape sequence the output left unfinished, up
		// to this, for Snapshot to end with, so a page's emulator takes
		// the rest of it as the core does.
		{optContinuationMax, u32(continuationMax)},
	} {
		if err := t.set(o.opt, o.value); err != nil {
			return err
		}
	}
	// The page draws text alone, so a core keeps no images, and the
	// queries for them are answered so. The released module may leave
	// Kitty graphics out, which keeps none either.
	if err := t.set(optKittyImageLimit, binary.LittleEndian.AppendUint64(nil, 0)); err != nil && !errors.Is(err, resultError(resultUnsupported)) {
		return err
	}
	return nil
}

// Size is the terminal's columns and rows, as New and Resize held them.
func (t *Terminal) Size() (cols, rows int) { return t.cols, t.rows }

// Memory is the bytes the terminal holds: its core's linear memory and the
// primary screen Snapshot keeps behind the alternate one. A failed or
// closed Terminal holds none.
func (t *Terminal) Memory() int {
	if t.err != nil || t.mem == nil {
		return 0
	}
	return int(t.mem.Size()) + len(t.primary)
}

// Write takes the shell's output and returns the replies to the queries
// it held, for the shell's stdin: device attributes, the cursor's place,
// mode reports, the colours SetColors gave and the size. On an error it
// returns the replies to the output taken before it.
func (t *Terminal) Write(p []byte) ([]byte, error) {
	var replies []byte
	err := t.guard("write", func() error {
		t.note(p)
		t.replies = nil
		err := t.writeTo(t.handle, p)
		replies, t.replies = t.replies, nil
		return err
	})
	return replies, err
}

// note looks through output for what Snapshot keeps track of, matching
// more than it needs to, across the end of the last output too. Output
// that may leave the alternate screen, which takes 47, 1047 or 1049 in a
// sequence, or a full reset (RIS), drops the primary screen Snapshot
// kept. Output that may save a cursor, with DECSC (ESC 7), SCOSC (CSI s)
// or mode 1048, sets savesCursor.
func (t *Terminal) note(p []byte) {
	across := append(bytes.Clone(t.tail), p[:min(len(p), 3)]...)
	if t.primary != nil && (leaves(across) || leaves(p)) {
		t.primary = nil
	}
	if !t.savesCursor && (savesCursor(across) || savesCursor(p)) {
		t.savesCursor = true
	}
	last := append(t.tail, p[max(0, len(p)-3):]...)
	t.tail = bytes.Clone(last[max(0, len(last)-3):])
}

func leaves(p []byte) bool {
	return bytes.Contains(p, []byte("47")) || bytes.Contains(p, []byte("1049")) || bytes.Contains(p, []byte("\x1bc"))
}

func savesCursor(p []byte) bool {
	return bytes.Contains(p, []byte("\x1b7")) || bytes.Contains(p, []byte("\x1b[s")) || bytes.Contains(p, []byte("1048"))
}

// Resize sets the terminal's size, held to MaxCols by MaxRows, reflowing
// the primary screen as the page's core does, and returns any reply it
// gives, such as the in-band size report a program asked for (mode 2048).
func (t *Terminal) Resize(cols, rows int) ([]byte, error) {
	if cols < 1 || rows < 1 {
		return nil, fmt.Errorf("resize terminal: size %dx%d out of range", cols, rows)
	}
	var replies []byte
	err := t.guard("resize", func() error {
		cols, rows = min(cols, MaxCols), min(rows, MaxRows)
		t.primary = nil
		t.replies = nil
		err := t.check("ghostty_terminal_resize", uint64(t.handle), uint64(cols), uint64(rows), 0, 0)
		if err == nil {
			t.cols, t.rows = cols, rows
			err = t.set(optScrollbackMaxLines, u32(historyLimit(cols)))
		}
		replies, t.replies = t.replies, nil
		return err
	})
	return replies, err
}

// SetColors sets the foreground and background that OSC 10 and 11 answer
// with: the theme of the page most recently active.
func (t *Terminal) SetColors(fg, bg RGB) error {
	return t.guard("set colours", func() error {
		if err := t.set(optColorForeground, []byte{fg.R, fg.G, fg.B}); err != nil {
			return err
		}
		return t.set(optColorBackground, []byte{bg.R, bg.G, bg.B})
	})
}

// Close frees the terminal's instance and its memory. A failed Terminal's
// is freed already.
func (t *Terminal) Close() error {
	if t.err == nil {
		t.err = errClosed
	}
	return t.closeModules()
}

func (t *Terminal) closeModules() error {
	var errs []error
	for _, m := range []api.Module{t.bridge, t.ghostty} {
		if m != nil {
			errs = append(errs, m.Close(context.Background()))
		}
	}
	t.bridge, t.ghostty, t.mem, t.functions, t.primary = nil, nil, nil, nil, nil
	return errors.Join(errs...)
}

// errClosed is what a closed Terminal's calls return.
var errClosed = errors.New("the terminal is closed")

// guard runs a public call's work. It returns at once on a failed or
// closed Terminal, and fails the Terminal on the work's error, or on an
// address past the instance's memory, which the work's reads and writes
// panic with.
func (t *Terminal) guard(what string, work func() error) (err error) {
	if t.err != nil {
		return t.err
	}
	defer func() {
		if r := recover(); r != nil {
			if r != errOutOfRange { //nolint:errorlint // the value panicked with, not a wrapped error
				panic(r)
			}
			err = errOutOfRange
		}
		if err != nil {
			err = t.fail(fmt.Errorf("%s: %w", what, err))
		}
	}()
	return work()
}

// fail marks the Terminal failed with err, which every later call
// returns, and frees its instance: after an error Ghostty's state is not
// to be trusted.
func (t *Terminal) fail(err error) error {
	if t.err == nil {
		t.err = fmt.Errorf("terminal failed: %w", err)
		_ = t.closeModules()
	}
	return t.err
}

// set sets a terminal option to value, copied in through scratch.
func (t *Terminal) set(opt int, value []byte) error {
	t.write(t.scratch, value)
	if err := t.check("ghostty_terminal_set", uint64(t.handle), uint64(opt), uint64(t.scratch)); err != nil {
		return fmt.Errorf("set option %d: %w", opt, err)
	}
	return nil
}

// get reads terminal data into scratch, returning the result code, so a
// caller can tell GHOSTTY_NO_VALUE apart.
func (t *Terminal) get(data int) (int32, error) {
	r, err := t.call("ghostty_terminal_get", uint64(t.handle), uint64(data), uint64(t.scratch))
	return int32(r), err
}

// getString reads a GhosttyString datum, such as the title.
func (t *Terminal) getString(data int) (string, error) {
	r, err := t.get(data)
	if err != nil || r == resultNoValue {
		return "", err
	}
	if r != resultSuccess {
		return "", fmt.Errorf("get data %d: %w", data, resultError(r))
	}
	return string(t.read(t.u32(t.scratch), t.u32(t.scratch+4))), nil
}

// setCallbacks gives the terminal koko's callbacks for replies and the
// size, by value, as each function's index in the table.
func (t *Terminal) setCallbacks() error {
	for _, c := range []struct {
		opt      int
		callback uint32
	}{{optWritePty, callbackWritePty}, {optSize, callbackSize}} {
		if err := t.check("ghostty_terminal_set", uint64(t.handle), uint64(c.opt), uint64(t.callbacks+c.callback)); err != nil {
			return fmt.Errorf("set option %d: %w", c.opt, err)
		}
	}
	return nil
}

// setMode sets a mode's value alone, as no sequence would.
func (t *Terminal) setMode(mode uint16, value bool) error {
	v := []byte{0, 0, 0, 0}
	binary.LittleEndian.PutUint16(v, mode)
	if value {
		v[2] = 1
	}
	t.write(t.scratch, v)
	if err := t.check("ghostty_terminal_set", uint64(t.handle), optMode, uint64(t.scratch)); err != nil {
		return fmt.Errorf("set mode %d: %w", mode, err)
	}
	return nil
}

// mode tells whether a terminal's mode is set.
func (t *Terminal) mode(mode uint16) (bool, error) {
	v := []byte{0, 0, 0, 0}
	binary.LittleEndian.PutUint16(v, mode)
	t.write(t.scratch, v)
	r, err := t.get(dataMode)
	if err != nil {
		return false, err
	}
	if r != resultSuccess {
		return false, fmt.Errorf("get mode %d: %w", mode, resultError(r))
	}
	return t.read(t.scratch+2, 1)[0] != 0, nil
}

func (t *Terminal) alloc(n int) (uint32, error) {
	p, err := t.call("ghostty_wasm_alloc", uint64(n))
	if err == nil && p == 0 {
		err = errors.New("out of memory")
	}
	if err != nil {
		return 0, fmt.Errorf("allocate %d bytes: %w", n, err)
	}
	return uint32(p), nil
}

// errMemoryBound is a core past memoryBound.
var errMemoryBound = fmt.Errorf("the core holds more than %d MiB", memoryBound>>20)

// call calls one of Ghostty's functions. An error is a trap, a cancelled
// context or a core past memoryBound, and fails the Terminal at the
// public call's end. A cancelled context stops the next call, not one
// under way: wazero can end a call under way only by checking its context
// throughout, which made the core write a ninth as fast.
func (t *Terminal) call(name string, args ...uint64) (uint64, error) {
	if t.ghostty == nil {
		return 0, errClosed
	}
	if err := t.ctx.Err(); err != nil {
		return 0, fmt.Errorf("%s: %w", name, err)
	}
	f := t.functions[name]
	if f == nil {
		if f = t.ghostty.ExportedFunction(name); f == nil {
			return 0, fmt.Errorf("libghostty-vt exports no %s", name)
		}
		if t.functions == nil {
			t.functions = map[string]api.Function{}
		}
		t.functions[name] = f
	}
	r, err := f.Call(t.ctx, args...)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", name, err)
	}
	if t.mem.Size() > memoryBound {
		return 0, fmt.Errorf("%s: %w", name, errMemoryBound)
	}
	if len(r) == 0 {
		return 0, nil
	}
	return r[0], nil
}

// check calls a function that returns a GhosttyResult.
func (t *Terminal) check(name string, args ...uint64) error {
	r, err := t.call(name, args...)
	if err != nil {
		return err
	}
	if r := int32(r); r != resultSuccess {
		return fmt.Errorf("%s: %w", name, resultError(r))
	}
	return nil
}

// writeTo writes VT bytes into one of the instance's terminals, through
// input a piece at a time.
func (t *Terminal) writeTo(handle uint32, p []byte) error {
	for len(p) > 0 {
		n := min(len(p), inputSize)
		t.write(t.input, p[:n])
		if _, err := t.call("ghostty_terminal_vt_write", uint64(handle), uint64(t.input), uint64(n)); err != nil {
			return err
		}
		p = p[n:]
	}
	return nil
}

// startWriter readies scratchWriter as a GhosttyWriter whose output the
// write callback collects in sink, and returns its address.
func (t *Terminal) startWriter() uint32 {
	t.write(t.scratch+scratchWriter, cat(u32(t.callbacks+callbackWrite), u32(0)))
	t.sink = nil
	return t.scratch + scratchWriter
}

// takeSink returns what the writer collected, and forgets it.
func (t *Terminal) takeSink() []byte {
	out := t.sink
	t.sink = nil
	return out
}

// resultError is a GhosttyResult other than success.
type resultError int32

func (r resultError) Error() string {
	switch r {
	case resultOutOfMemory:
		return "out of memory"
	case resultInvalidValue:
		return "invalid value"
	case resultOutOfSpace:
		return "out of space"
	case resultNoValue:
		return "no value"
	case resultIOError:
		return "I/O error"
	case resultLimitExceeded:
		return "limit exceeded"
	case resultUnsupported:
		return "unsupported"
	default:
		return fmt.Sprintf("libghostty-vt result %d", int32(r))
	}
}

// errOutOfRange is an address past the instance's memory, which neither
// koko nor Ghostty passes: a host function panics with it, which wazero
// returns as the call's error.
var errOutOfRange = errors.New("an address past the instance's memory")

// read copies n bytes out of the instance's memory.
func (t *Terminal) read(p, n uint32) []byte {
	b, ok := t.mem.Read(p, n)
	if !ok {
		panic(errOutOfRange)
	}
	return bytes.Clone(b)
}

// write copies b into the instance's memory at p.
func (t *Terminal) write(p uint32, b []byte) {
	if !t.mem.Write(p, b) {
		panic(errOutOfRange)
	}
}

func (t *Terminal) u32(p uint32) uint32 { return binary.LittleEndian.Uint32(t.read(p, 4)) }

func u32(v uint32) []byte { return binary.LittleEndian.AppendUint32(nil, v) }
