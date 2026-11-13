package vt

import (
	"bytes"
	"errors"
	"fmt"
)

// The formatter's extras for the active screen: every piece of state it
// restores but the palette, which would write the core's own colours over
// the page's theme, and the working directory, which Snapshot writes
// itself.
const activeExtras = extraModes | extraScrollingRegion | extraTabstops | extraKeyboard |
	extraCursor | extraStyle | extraHyperlink | extraProtection | extraKittyKeyboard | extraCharsets

// primaryExtras are the extras for the primary screen behind the
// alternate one: its cursor and pen, which switching to the alternate
// screen saves and leaving it restores, and its Kitty keyboard flags,
// which each screen keeps apart.
const primaryExtras = extraCursor | extraStyle | extraHyperlink | extraProtection | extraCharsets | extraKittyKeyboard

// savedExtras are the extras for a screen's saved cursor (DECSC), formatted
// from a copy that restored it: its place and pen.
const savedExtras = extraCursor | extraStyle

// Snapshot returns VT bytes that draw the terminal on a cleared emulator
// of its size, scrollback included: the primary screen with its history,
// then, while the alternate screen is active, the switch to it and its
// screen; then the modes, the scroll region, the tab stops, the cursor's
// place and the pen, from Ghostty's formatter; then what the formatter
// leaves out: the palette entries and colours a program set (OSC 4, 10, 11
// and 12), the cursor's shape, the working directory (OSC 7), the title
// (OSC 2) and an unfinished sequence. Each screen's saved cursor (DECSC)
// is restored before its content. Colours stay palette indices, so the
// page's theme still colours them.
func (t *Terminal) Snapshot() ([]byte, error) {
	var out bytes.Buffer
	err := t.guard("snapshot", func() error { return t.snapshot(&out) })
	if err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func (t *Terminal) snapshot(out *bytes.Buffer) error {
	r, err := t.get(dataActiveScreen)
	if err != nil {
		return err
	}
	if r != resultSuccess {
		return fmt.Errorf("get the active screen: %w", resultError(r))
	}
	alternate := t.u32(t.scratch) == screenAlternate
	var altModes []uint16 // the alternate screen's modes set, the leaving one first
	if alternate {
		for _, m := range altScreenModes {
			set, err := t.mode(decMode(m))
			if err != nil {
				return err
			}
			if set {
				altModes = append(altModes, m)
			}
		}
	} else {
		t.primary = nil
	}
	var saved, primary []byte
	if alternate && t.primary != nil {
		primary = t.primary
	}
	if t.savesCursor || (alternate && t.primary == nil) {
		encoded, err := t.encode()
		if err != nil {
			return fmt.Errorf("copy the terminal: %w", err)
		}
		// What a copy gives is left out if the copy fails, so a page still
		// attaches; the copy is an instance of its own, and the
		// terminal's state does not depend on it.
		saved, primary, _ = t.fromCopy(encoded, alternate, altModes)
	}
	out.Write(primary)
	// Formatting the alternate screen with its modes cleared leaves the
	// switch to it out of the formatter's modes, so the saved cursor goes
	// between the switch and the screen.
	hold := map[uint16]bool{decMode(7): true}
	for i, m := range altModes {
		fmt.Fprintf(out, "\x1b[?%dh", altModes[len(altModes)-1-i])
		hold[decMode(m)] = false
	}
	out.Write(saved)
	// The formatter writes the modes before the screen's content, then the
	// rest after it. With autowrap off a wrapped line, written whole, would
	// replay cut, so autowrap is on while the formatter runs, and turned
	// off after the content.
	wraps, err := t.mode(decMode(7))
	if err != nil {
		return err
	}
	if err := t.holding(hold, func() error { return t.format(out, t.handle, activeExtras, false) }); err != nil {
		return err
	}
	if !wraps {
		out.WriteString("\x1b[?7l")
	}
	if err := t.formatterLeavesOut(out); err != nil {
		return err
	}
	return t.continuation(out)
}

// holding runs f with the terminal's modes set as given, as no sequence
// would set them, and sets them back after.
func (t *Terminal) holding(modes map[uint16]bool, f func() error) error {
	was := map[uint16]bool{}
	for m, v := range modes {
		set, err := t.mode(m)
		if err != nil {
			return err
		}
		was[m] = set
		if set != v {
			if err := t.setMode(m, v); err != nil {
				return err
			}
		}
	}
	err := f()
	for m, v := range was {
		if v != modes[m] {
			err = errors.Join(err, t.setMode(m, v))
		}
	}
	return err
}

// continuation writes the escape sequence or UTF-8 character the shell's
// output left unfinished, so the page's emulator takes the rest of it, in
// the output that follows, as the core does. One longer than
// continuationMax was not kept, and is left out.
func (t *Terminal) continuation(out *bytes.Buffer) error {
	writer := t.startWriter()
	r, err := t.call("ghostty_terminal_continuation_write", uint64(t.handle), uint64(writer))
	unfinished := t.takeSink()
	if err != nil {
		return err
	}
	switch r := int32(r); r {
	case resultSuccess:
		out.Write(unfinished)
	case resultInvalidValue: // too long to have been kept
	default:
		return fmt.Errorf("get the unfinished sequence: %w", resultError(r))
	}
	return nil
}

// restoreSaved leaves a copy's screen blank, with the default pen, then
// restores the saved cursor (DECRC), so the formatter writes the saved
// cursor's place and pen alone. The ESC that starts it ends any sequence
// the copy's parser was in.
const restoreSaved = "\x1b[0m\x1b[H\x1b[2J\x1b[3J\x1b8"

// afterSaved saves the cursor drawn before it (DECSC), then puts the
// cursor and the pen back where a screen's content starts.
const afterSaved = "\x1b7\x1b[H\x1b[0m"

// fromCopy reads, from copies of the terminal in an instance of their own,
// the active screen's saved cursor, once the output has saved one, and,
// while the alternate screen is active, the primary screen behind it,
// which Ghostty's formatter cannot reach. Each comes ready to replay, or
// empty when the copy fails, as the error says.
//
// The saved cursor needs only the screens, which Ghostty's snapshot decoder
// gives first (READY), in well under a millisecond for ordinary output.
// The primary screen needs its history too, so it is kept in t.primary
// while the alternate screen stays. Ghostty's decoder is slow on combining
// marks: a screen of them at the largest grid takes seconds.
func (t *Terminal) fromCopy(encoded []byte, alternate bool, altModes []uint16) (saved, primary []byte, err error) {
	c, err := instantiate(t.ctx, t.cols, t.rows)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = c.Close() }()
	err = c.guard("copy the terminal", func() error {
		if t.savesCursor {
			screens, err := c.decodeScreens(encoded)
			if err != nil {
				return err
			}
			if saved, err = c.savedCursor(screens); err != nil {
				return err
			}
		}
		if alternate && t.primary == nil {
			if t.primary, err = c.primaryBehind(encoded, altModes); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	if alternate {
		primary = t.primary
	}
	return saved, primary, nil
}

// savedCursor formats a copy's active screen's saved cursor, and ends it
// with afterSaved.
func (t *Terminal) savedCursor(handle uint32) ([]byte, error) {
	if err := t.writeTo(handle, []byte(restoreSaved)); err != nil {
		return nil, err
	}
	var b bytes.Buffer
	if err := t.format(&b, handle, savedExtras, true); err != nil {
		return nil, err
	}
	b.WriteString(afterSaved)
	return b.Bytes(), nil
}

// primaryBehind makes a full copy of the terminal in the instance, leaves
// the alternate screen in it as the program would, and formats the copy's
// primary screen with what leaving restored: the cursor, the pen, the Kitty
// keyboard flags and the cursor's shape. Its saved cursor goes first.
func (t *Terminal) primaryBehind(encoded []byte, altModes []uint16) ([]byte, error) {
	copied, err := t.decode(encoded)
	if err != nil {
		return nil, err
	}
	t.handle = copied
	if err := t.setCallbacks(); err != nil {
		return nil, err
	}
	leave := uint16(47)
	if len(altModes) > 0 {
		leave = altModes[0]
	}
	// The copy's parser may be in the middle of a sequence, which the ESC
	// ends, as it ends any.
	if err := t.writeTo(copied, fmt.Appendf(nil, "\x1b[?%dl", leave)); err != nil {
		return nil, err
	}
	var screen bytes.Buffer
	if err := t.format(&screen, copied, primaryExtras, false); err != nil {
		return nil, err
	}
	// The cursor's shape is the screen's own too, and comes back on
	// leaving the alternate screen.
	shape, err := t.cursorShape()
	if err != nil {
		return nil, err
	}
	if shape != 0 {
		fmt.Fprintf(&screen, "\x1b[%d q", shape)
	}
	saved, err := t.savedCursor(copied)
	if err != nil {
		return nil, err
	}
	return append(saved, screen.Bytes()...), nil
}

// encode writes the terminal in Ghostty's snapshot format, streamed out.
func (t *Terminal) encode() ([]byte, error) {
	writer := t.startWriter()
	err := t.check("ghostty_snapshot_encode", uint64(t.handle), uint64(writer))
	encoded := t.takeSink()
	if err != nil {
		return nil, err
	}
	return encoded, nil
}

// decode makes a terminal in the instance from what encode wrote.
func (t *Terminal) decode(encoded []byte) (uint32, error) {
	t.source = encoded
	defer func() { t.source = nil }()
	reader := t.scratch + scratchReader
	t.write(reader, cat(u32(t.callbacks+callbackRead), u32(0)))
	if err := t.check("ghostty_snapshot_decoder_new", 0, uint64(t.scratch), uint64(reader)); err != nil {
		return 0, err
	}
	decoder := t.u32(t.scratch)
	defer func() { _, _ = t.call("ghostty_snapshot_decoder_free", uint64(decoder)) }()
	if err := t.check("ghostty_snapshot_decoder_decode", uint64(decoder), uint64(t.scratch)); err != nil {
		return 0, err
	}
	return t.u32(t.scratch), nil
}

// decodeScreens makes a terminal in the instance from what encode wrote,
// with its screens and saved cursors and none of its history beyond them:
// the decoder's READY step alone.
func (t *Terminal) decodeScreens(encoded []byte) (uint32, error) {
	t.source = encoded
	defer func() { t.source = nil }()
	reader := t.scratch + scratchReader
	t.write(reader, cat(u32(t.callbacks+callbackRead), u32(0)))
	if err := t.check("ghostty_snapshot_decoder_new", 0, uint64(t.scratch), uint64(reader)); err != nil {
		return 0, err
	}
	decoder := t.u32(t.scratch)
	defer func() { _, _ = t.call("ghostty_snapshot_decoder_free", uint64(decoder)) }()
	if err := t.check("ghostty_snapshot_decoder_ready", uint64(decoder), uint64(t.scratch)); err != nil {
		return 0, err
	}
	return t.u32(t.scratch), nil
}

// format writes a terminal's active screen, history included, as VT with
// the extras given. Unless trim is set every row is written, blank ones
// too, so the replay fills the screen as the terminal does; a wrapped line
// is written whole, so the replay wraps it as the terminal did.
func (t *Terminal) format(out *bytes.Buffer, handle uint32, extras uint32, trim bool) error {
	if err := t.check("ghostty_formatter_terminal_new", 0, uint64(t.scratch), uint64(handle)); err != nil {
		return err
	}
	formatter := t.u32(t.scratch)
	defer func() { _, _ = t.call("ghostty_formatter_free", uint64(formatter)) }()
	for _, o := range []struct {
		opt   int
		value []byte
	}{
		{formatterOptEmit, u32(formatVT)},
		{formatterOptTrim, []byte{boolByte(trim)}},
		{formatterOptUnwrap, []byte{1}},
		{formatterOptExtras, u32(extras)},
	} {
		t.write(t.scratch, o.value)
		if err := t.check("ghostty_formatter_set", uint64(formatter), uint64(o.opt), uint64(t.scratch)); err != nil {
			return fmt.Errorf("set formatter option %d: %w", o.opt, err)
		}
	}
	writer := t.startWriter()
	err := t.check("ghostty_formatter_format", uint64(formatter), uint64(writer))
	formatted := t.takeSink()
	if err != nil {
		return err
	}
	out.Write(formatted)
	return nil
}

// formatterLeavesOut writes the state the formatter does not: the palette
// entries and the colours a program set, apart from the defaults
// SetColors gave, the cursor's shape, the working directory and the title.
func (t *Terminal) formatterLeavesOut(out *bytes.Buffer) error {
	palette, err := t.palette(dataColorPalette)
	if err != nil {
		return err
	}
	defaults, err := t.palette(dataColorPaletteDefault)
	if err != nil {
		return err
	}
	for i := range 256 {
		if c := palette[3*i : 3*i+3]; !bytes.Equal(c, defaults[3*i:3*i+3]) {
			fmt.Fprintf(out, "\x1b]4;%d;rgb:%02x/%02x/%02x\x1b\\", i, c[0], c[1], c[2])
		}
	}
	for _, c := range []struct{ current, def, osc int }{
		{dataColorForeground, dataColorForegroundDefault, 10},
		{dataColorBackground, dataColorBackgroundDefault, 11},
		{dataColorCursor, dataColorCursorDefault, 12},
	} {
		current, err := t.color(c.current)
		if err != nil {
			return err
		}
		def, err := t.color(c.def)
		if err != nil {
			return err
		}
		if current != nil && !bytes.Equal(current, def) {
			fmt.Fprintf(out, "\x1b]%d;rgb:%02x/%02x/%02x\x1b\\", c.osc, current[0], current[1], current[2])
		}
	}
	shape, err := t.cursorShape()
	if err != nil {
		return err
	}
	if shape != 0 {
		fmt.Fprintf(out, "\x1b[%d q", shape)
	}
	// The formatter's own OSC 7 carries a NUL before its terminator.
	pwd, err := t.getString(dataPwd)
	if err != nil {
		return err
	}
	if pwd != "" {
		fmt.Fprintf(out, "\x1b]7;%s\x1b\\", pwd)
	}
	title, err := t.getString(dataTitle)
	if err != nil {
		return err
	}
	if title != "" {
		fmt.Fprintf(out, "\x1b]2;%s\x1b\\", title)
	}
	return nil
}

// color reads a colour datum, or nil when it has none.
func (t *Terminal) color(data int) ([]byte, error) {
	r, err := t.get(data)
	if err != nil || r == resultNoValue {
		return nil, err
	}
	if r != resultSuccess {
		return nil, fmt.Errorf("get colour %d: %w", data, resultError(r))
	}
	return t.read(t.scratch, 3), nil
}

func boolByte(b bool) byte {
	if b {
		return 1
	}
	return 0
}

func (t *Terminal) palette(data int) ([]byte, error) {
	r, err := t.get(data)
	if err != nil {
		return nil, err
	}
	if r != resultSuccess {
		return nil, fmt.Errorf("get the palette: %w", resultError(r))
	}
	return t.read(t.scratch, 3*256), nil
}

// cursorShape is the DECSCUSR parameter that restores the cursor shape a
// program set, or 0 when none did, so the page's own default stands.
// Ghostty reports the shape to a DECRQSS query, and a cursor that follows
// the default changes with it, so the core is asked twice with two
// defaults: a shape that stays is one a program set. The queries go in
// only where the shell's output is at ground, between two sequences;
// elsewhere the shape is left out.
func (t *Terminal) cursorShape() (int, error) {
	r, err := t.get(dataVTGround)
	if err != nil {
		return 0, err
	}
	if r != resultSuccess {
		return 0, fmt.Errorf("get whether the output is at ground: %w", resultError(r))
	}
	if ground, _ := t.mem.ReadByte(t.scratch); ground == 0 {
		return 0, nil
	}
	// Changing the default sets the blinking mode of a cursor that
	// follows it, which a program may have set apart. The default ends as
	// block, Ghostty's, which every core has.
	blinking, err := t.mode(decMode(12))
	if err != nil {
		return 0, err
	}
	shown := t.replies
	var answers [2]string
	err = func() error {
		for i, style := range []uint32{cursorBar, cursorBlock} {
			if err := t.set(optDefaultCursorStyle, u32(style)); err != nil {
				return err
			}
			t.replies = nil
			if err := t.writeTo(t.handle, []byte("\x1bP$q q\x1b\\")); err != nil {
				return err
			}
			answers[i] = string(t.replies)
		}
		return nil
	}()
	t.replies = shown
	if err := errors.Join(err, t.setMode(decMode(12), blinking)); err != nil {
		return 0, err
	}
	if answers[0] != answers[1] {
		return 0, nil
	}
	var shape int
	if _, err := fmt.Sscanf(answers[0], "\x1bP1$r%d q\x1b\\", &shape); err != nil {
		return 0, fmt.Errorf("read the cursor shape from %q: %w", answers[0], err)
	}
	return shape, nil
}
