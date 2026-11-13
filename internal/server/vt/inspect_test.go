package vt

import (
	"fmt"
	"strings"
	"testing"
)

// The tests compare two cores cell by cell through Ghostty's grid and
// terminal getters, not through the formatter, which would pass anything
// the formatter drops on both sides.

// cellState is one cell as the core holds it.
type cellState struct {
	Text  string   // the cell's grapheme, "" for an empty cell
	Wide  uint32   // GhosttyCellWide: narrow, wide, spacer tail or head
	Style [16]byte // GhosttyStyle: colours with palette and RGB kept apart, and the flags
}

// rowState is one row: its cells and whether it wraps onto the next.
type rowState struct {
	Wrap  bool
	Cells []cellState
}

// state is what a core holds that a snapshot must bring back.
type state struct {
	Cols, Rows  int
	History     int // rows of scrollback
	Lines       []rowState
	CursorX     uint32
	CursorY     uint32
	PendingWrap bool
	Alternate   bool
	Modes       map[string]bool
	KittyFlags  uint32
	Title, Pwd  string
	Shape       int
	Palette     string
	Colors      string // the foreground, background and cursor colours a program set
}

// checkedModes are the modes compared, ANSI ones marked "a".
var checkedModes = []struct {
	n    uint16
	ansi bool
}{
	{4, true},
	{20, true},
	{1, false},
	{6, false},
	{7, false},
	{12, false},
	{25, false},
	{45, false},
	{47, false},
	{66, false},
	{69, false},
	{1000, false},
	{1002, false},
	{1003, false},
	{1004, false},
	{1005, false},
	{1006, false},
	{1007, false},
	{1015, false},
	{1016, false},
	{1047, false},
	{1049, false},
	{2004, false},
	{2026, false},
	{2027, false},
	{2048, false},
}

// inspect reads a terminal's state, its history included.
func inspect(tb testing.TB, t *Terminal) state {
	tb.Helper()
	return inspectRows(tb, t, int(getU32(tb, t, dataTotalRows)))
}

// inspectRows reads a terminal's state with the given number of its
// newest rows.
func inspectRows(tb testing.TB, t *Terminal, rows int) state {
	tb.Helper()
	s := state{Cols: t.cols, Rows: t.rows, Modes: map[string]bool{}}
	s.History = int(getU32(tb, t, dataScrollbackRows))
	total := int(getU32(tb, t, dataTotalRows))
	for y := total - min(rows, total); y < total; y++ {
		s.Lines = append(s.Lines, inspectRow(tb, t, y))
	}
	s.CursorX = getU32(tb, t, dataCursorX) & 0xffff
	s.CursorY = getU32(tb, t, dataCursorY) & 0xffff
	s.PendingWrap = getU32(tb, t, dataCursorPendingWrap)&0xff != 0
	s.Alternate = getU32(tb, t, dataActiveScreen) == screenAlternate
	for _, m := range checkedModes {
		v := m.n
		name := fmt.Sprint("?", m.n)
		if m.ansi {
			v |= 1 << 15
			name = fmt.Sprint(m.n)
		}
		set, err := t.mode(v)
		if err != nil {
			tb.Fatal(err)
		}
		s.Modes[name] = set
	}
	s.KittyFlags = getU32(tb, t, dataKittyKeyboardFlags) & 0xff
	var err error
	if s.Title, err = t.getString(dataTitle); err != nil {
		tb.Fatal(err)
	}
	if s.Pwd, err = t.getString(dataPwd); err != nil {
		tb.Fatal(err)
	}
	if s.Shape, err = t.cursorShape(); err != nil {
		tb.Fatal(err)
	}
	palette, err := t.palette(dataColorPalette)
	if err != nil {
		tb.Fatal(err)
	}
	s.Palette = fmt.Sprintf("%x", palette)
	for _, data := range []int{dataColorForeground, dataColorBackground, dataColorCursor} {
		c, err := t.color(data)
		if err != nil {
			tb.Fatal(err)
		}
		s.Colors += fmt.Sprintf("%x;", c)
	}
	return s
}

func getU32(tb testing.TB, t *Terminal, data int) uint32 {
	tb.Helper()
	t.write(t.scratch, make([]byte, 8))
	r, err := t.get(data)
	if err != nil || r != resultSuccess {
		tb.Fatalf("get data %d: %d %v", data, r, err)
	}
	return t.u32(t.scratch)
}

// inspectRow reads row y of the screen, history included, counted from
// the oldest row kept.
func inspectRow(tb testing.TB, t *Terminal, y int) rowState {
	tb.Helper()
	point, ref, cell, style, buf := t.scratch+64, t.scratch+96, t.scratch+112, t.scratch+128, t.scratch+256
	var row rowState
	t.write(point, make([]byte, 24))
	t.write(point, u32(2)) // GHOSTTY_POINT_TAG_SCREEN
	t.mem.WriteUint32Le(point+12, uint32(y))
	mustCheck(tb, t, "ghostty_terminal_grid_ref", uint64(t.handle), uint64(point), uint64(ref))
	mustCheck(tb, t, "ghostty_grid_ref_row", uint64(ref), uint64(cell))
	r, _ := t.mem.ReadUint64Le(cell)
	mustCheck(tb, t, "ghostty_row_get", r, 1, uint64(buf)) // GHOSTTY_ROW_DATA_WRAP
	w, _ := t.mem.ReadByte(buf)
	row.Wrap = w != 0
	for x := range t.cols {
		// A GhosttyGridRef is the row's page, then x and y in it.
		t.mem.WriteUint16Le(ref+4, uint16(x))
		mustCheck(tb, t, "ghostty_grid_ref_cell", uint64(ref), uint64(cell))
		c, _ := t.mem.ReadUint64Le(cell)
		var cs cellState
		mustCheck(tb, t, "ghostty_cell_get", c, 3, uint64(buf)) // GHOSTTY_CELL_DATA_WIDE
		cs.Wide = t.u32(buf)
		mustCheck(tb, t, "ghostty_grid_ref_style", uint64(ref), uint64(style))
		copy(cs.Style[:], t.read(style, 16))
		t.mem.WriteUint32Le(buf+64, 0)
		mustCheck(tb, t, "ghostty_grid_ref_graphemes", uint64(ref), uint64(buf), 16, uint64(buf+64))
		var text strings.Builder
		for i := range t.u32(buf + 64) {
			text.WriteRune(rune(t.u32(buf + 4*i)))
		}
		cs.Text = text.String()
		row.Cells = append(row.Cells, cs)
	}
	return row
}

func mustCheck(tb testing.TB, t *Terminal, name string, args ...uint64) {
	tb.Helper()
	if err := t.check(name, args...); err != nil {
		tb.Fatal(err)
	}
}

// screenText is the screen's rows as text, without trailing blanks.
func (s state) screenText() []string {
	var rows []string
	for _, l := range s.Lines[len(s.Lines)-s.Rows:] {
		var b strings.Builder
		for _, c := range l.Cells {
			switch {
			case c.Wide >= 2: // a spacer
			case c.Text == "":
				b.WriteByte(' ')
			default:
				b.WriteString(c.Text)
			}
		}
		rows = append(rows, strings.TrimRight(b.String(), " "))
	}
	return rows
}
