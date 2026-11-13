package vt

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"flag"
	"fmt"
	"os"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

var update = flag.Bool("update", false, "rewrite testdata/snapshot.golden")

func TestTheModuleIsThePinnedRelease(t *testing.T) {
	if err := checkPin(wasm); err != nil {
		t.Fatalf("%v: run make vt-wasm", err)
	}
	changed := bytes.Clone(wasm)
	changed[len(changed)/2] ^= 1
	if err := checkPin(changed); err == nil {
		t.Error("a module one bit off the pinned release passed its check")
	}
}

// The tests' machines all run wazero's compiler: CI's Linux and Windows
// amd64 and macOS arm64 runners, as kokod's linux/amd64 and linux/arm64.
func TestTheCompilerRunsTheModule(t *testing.T) {
	kind, err := Engine(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if kind != "compiler" {
		t.Errorf("the engine is %q, want the compiler", kind)
	}
}

// A Terminal opened while the engine compiles waits for the compile and
// then works, and one whose context ends first gives up with its cause,
// leaving the compile to finish for the next (#222).
func TestATerminalWaitsForTheEngineToCompile(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	held := newCompilation(func() (*engine, error) {
		<-release
		return engines.wait(context.Background())
	})

	gone := errors.New("the page left")
	ctx, cancel := context.WithCancelCause(t.Context())
	cancel(gone)
	if _, err := held.newTerminal(ctx, 80, 24); !errors.Is(err, gone) {
		t.Errorf("a Terminal whose context ended returned %v, want its cause", err)
	}

	waited := make(chan error, 1)
	go func() {
		term, err := held.newTerminal(t.Context(), 80, 24)
		if err == nil {
			_, err = term.Write([]byte("ready"))
			_ = term.Close()
		}
		waited <- err
	}()
	select {
	case err := <-waited:
		t.Fatalf("a Terminal returned %v before the compile ended", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	if err := <-waited; err != nil {
		t.Fatalf("a Terminal opened during the compile: %v", err)
	}
}

// snapshotCase is output a shell might write, and what the terminal shows
// after it.
type snapshotCase struct {
	name       string
	cols, rows int
	output     string
	// then is written after the snapshot, into both the terminal and the
	// one the snapshot drew, which must still agree: what the program
	// does next, as leaving vim.
	then string
}

func historyOutput() string {
	var b strings.Builder
	for i := range 1500 {
		fmt.Fprintf(&b, "line %04d \x1b[3%dmcoloured\x1b[0m\r\n", i+1, i%8)
	}
	b.WriteString("deploy@web1:~$ ")
	return b.String()
}

var snapshotCases = []snapshotCase{
	{
		name: "a prompt with 1,500 lines of history, the oldest gone",
		cols: 200, rows: 10,
		output: historyOutput(),
	},
	{
		name: "vim on the alternate screen over a shell screen",
		cols: 40, rows: 10,
		output: strings.Repeat("history behind the shell screen\r\n", 1000) + "deploy@web1:~$ ls\r\n\x1b[1;34mbin\x1b[0m  notes.txt\r\n\x1b[42mdeploy@web1\x1b[0m:~$ \x1b[5 q\x1b[>1uvim notes.txt" +
			"\x1b[?1049h\x1b[2 q\x1b[?1h\x1b=\x1b[H\x1b[2J" +
			"first line of notes\r\n\x1b[94m~\x1b[0m\r\n\x1b[94m~\x1b[0m" +
			"\x1b[10;1H\x1b[44m\x1b[K\"notes.txt\" 1L, 20B\x1b[0m" +
			"\x1b[1;6H",
		then: "\x1b[?1l\x1b>\x1b[?1049l\r\ndeploy@web1:~$ ",
	},
	{
		name: "256 colours and true colour, bold and underline styles",
		cols: 40, rows: 10,
		output: "\x1b[31mred\x1b[0m \x1b[92mbright\x1b[0m \x1b[38;5;196m196\x1b[48;5;21mon21\x1b[0m\r\n" +
			"\x1b[38;2;10;20;30mrgb\x1b[48;2;200;100;0mon\x1b[0m \x1b[1mbold\x1b[22m \x1b[3mitalic\x1b[0m\r\n" +
			"\x1b[4msingle\x1b[4:2mdouble\x1b[4:3mcurly\x1b[58;5;9mred-curly\x1b[0m \x1b[9mstrike\x1b[0m \x1b[7minv\x1b[0m\r\n" +
			"\x1b]4;1;rgb:12/34/56\x1b\\\x1b[31mset red\x1b[0m \x1b[2mfaint\x1b[0m\r\n" +
			"\x1b[1;35;48;5;236m",
		then: "the pen carries on",
	},
	{
		name: "CJK and wide characters at the right edge",
		cols: 40, rows: 10,
		output: strings.Repeat("a", 39) + "中文 wide\r\n" +
			strings.Repeat("b", 38) + "日本\r\n" +
			"é combining, 🚀 emoji, 한국어\r\n" +
			strings.Repeat("c", 40),
	},
	{
		name: "a scroll region and tab stops",
		cols: 40, rows: 10,
		output: "header\r\n\x1b[3g\x1b[5G\x1bH\x1b[20G\x1bH\x1b[3;8r\x1b[8;1H" +
			"one\r\ntwo\r\nthree\r\nfour\r\nfive\r\nsix\r\nseven\r\n\x1b[10;1Hfooter\x1b[5;3H",
		then: "\x1b[8;1H\nscrolled\tto a stop\tand the next",
	},
	{
		name: "application cursor keys, bracketed paste and mouse modes",
		cols: 40, rows: 10,
		output: "\x1b[?1h\x1b=\x1b[?2004h\x1b[?1002h\x1b[?1006h\x1b[?1004h\x1b[>5u\x1b[4h" +
			"\x1b[5 q\x1b[?25lmodes set\r\n",
	},
	{
		name: "a wrapped line with autowrap turned off after it",
		cols: 40, rows: 10,
		output: strings.Repeat("0123456789", 5) + "\r\ndeploy@web1:~$ \x1b[?7l",
		then:   strings.Repeat("x", 50),
	},
	{
		name: "an escape sequence cut by the end of the output",
		cols: 40, rows: 10,
		output: "\x1b[?12ldeploy@web1:~$ make\r\n\x1b[3",
		then:   "1mfailed\x1b[0m: exit 2\r\n",
	},
	{
		name: "the title and OSC 7",
		cols: 40, rows: 10,
		output: "\x1b]2;deploy@web1: ~/srv\x07\x1b]7;file://web1/srv/app\x1b\\deploy@web1:/srv/app$ ",
	},
	{
		name: "a saved cursor and pen, restored after",
		cols: 40, rows: 10,
		output: "\x1b[3;5H\x1b[1;31m\x1b7\x1b[0m\x1b[8;1Hmoved",
		then:   "\x1b8restored",
	},
	{
		name: "the colours a program set",
		cols: 40, rows: 10,
		output: "\x1b]10;rgb:11/22/33\x1b\\\x1b]11;rgb:44/55/66\x07\x1b]12;rgb:77/88/99\x07themed",
	},
	{
		name: "synchronised output under way",
		cols: 40, rows: 10,
		output: "before\r\n\x1b[?2026hhalf a fra",
		then:   "me\x1b[?2026l",
	},
	{
		name: "the alternate screen entered with 1047",
		cols: 40, rows: 10,
		output: "shell$ less\r\n\x1b7\x1b[?1047h\x1b[Hpage one\x1b[5;3H",
		then:   "\x1b[?1047l\x1b8shell$ ",
	},
	{
		name: "the alternate screen entered with 47",
		cols: 40, rows: 10,
		output: "shell$ old\r\n\x1b[?47h\x1b[2J\x1b[Hold program\x1b[4;7H",
		then:   "\x1b[?47lshell$ ",
	},
}

func newTerminal(t *testing.T, cols, rows int) *Terminal {
	t.Helper()
	term, err := New(t.Context(), cols, rows)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := term.Close(); err != nil {
			t.Error(err)
		}
	})
	return term
}

func write(t *testing.T, term *Terminal, s string) {
	t.Helper()
	if _, err := term.Write([]byte(s)); err != nil {
		t.Fatal(err)
	}
}

// drawn is a case written into one core, and its snapshot written into a
// fresh core of the same size.
func drawn(t *testing.T, c snapshotCase) (original, restored *Terminal, snapshot []byte) {
	t.Helper()
	original = newTerminal(t, c.cols, c.rows)
	write(t, original, c.output)
	snapshot, err := original.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	restored = newTerminal(t, c.cols, c.rows)
	write(t, restored, string(snapshot))
	return original, restored, snapshot
}

func TestASnapshotDrawsTheSameTerminal(t *testing.T) {
	t.Parallel()
	for _, c := range snapshotCases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			original, restored, _ := drawn(t, c)
			want, got := inspect(t, original), inspect(t, restored)
			sameTerminal(t, want, got)
			if c.name == snapshotCases[0].name {
				oldestGone(t, want)
				oldestGone(t, got)
			}
			if c.then != "" {
				write(t, original, c.then)
				write(t, restored, c.then)
				sameTerminal(t, inspect(t, original), inspect(t, restored))
			}
		})
	}
}

// oldestGone checks that a terminal written 1,500 lines kept at least
// Scrollback of them, and not the first.
func oldestGone(t *testing.T, s state) {
	t.Helper()
	if s.History < Scrollback || s.History >= 1500 {
		t.Errorf("history = %d rows, want from %d to under 1,500", s.History, Scrollback)
	}
	if first := s.screenTextOf(0); !strings.HasPrefix(first, "line ") || first <= "line 0001" {
		t.Errorf("the oldest row kept is %q, want a line after the first", first)
	}
}

// sameTerminal compares two cores' states. History is page-granular, so
// the restored core may keep fewer of the oldest rows: rows are compared
// from the bottom, and the restored core must keep at least Scrollback.
func sameTerminal(t *testing.T, want, got state) {
	t.Helper()
	if want.History > 0 && got.History < min(want.History, Scrollback) {
		t.Errorf("history = %d rows, want at least %d", got.History, min(want.History, Scrollback))
	}
	n := min(len(want.Lines), len(got.Lines))
	for i := 1; i <= n; i++ {
		w, g := want.Lines[len(want.Lines)-i], got.Lines[len(got.Lines)-i]
		if !reflect.DeepEqual(w, g) {
			t.Errorf("row %d from the bottom differs:\n got %+v\nwant %+v", i, g, w)
			break
		}
	}
	want.Lines, got.Lines, want.History, got.History = nil, nil, 0, 0
	if !reflect.DeepEqual(want, got) {
		t.Errorf("state differs:\n got %+v\nwant %+v", got, want)
	}
}

// TestASnapshotChangesNothing takes a snapshot of each case twice and
// finds the terminal as it was: the queries Snapshot makes leave no trace,
// nor the copy of the primary screen.
func TestASnapshotChangesNothing(t *testing.T) {
	t.Parallel()
	for _, c := range snapshotCases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			term := newTerminal(t, c.cols, c.rows)
			write(t, term, c.output)
			before := inspectRows(t, term, c.rows)
			first, err := term.Snapshot()
			if err != nil {
				t.Fatal(err)
			}
			second, err := term.Snapshot()
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(first, second) {
				t.Error("a second snapshot differs from the first")
			}
			if after := inspectRows(t, term, c.rows); !reflect.DeepEqual(before, after) {
				t.Errorf("the terminal changed:\n got %+v\nwant %+v", after, before)
			}
		})
	}
}

// TestAtLeastScrollbackLinesAreKept writes far more than Scrollback lines
// at a narrow, a usual and the widest grid, and finds at least Scrollback
// of them kept throughout, though Ghostty drops a whole page at a time.
func TestAtLeastScrollbackLinesAreKept(t *testing.T) {
	for _, cols := range []int{20, 120, 500} {
		term := newTerminal(t, cols, 10)
		least, most := 0, 0
		for range 40 {
			write(t, term, strings.Repeat("line \x1b[31mred\x1b[0m\r\n", 100))
			h := int(getU32(t, term, dataScrollbackRows))
			if most >= Scrollback {
				least = min(least, h)
			} else {
				least = h
			}
			most = max(most, h)
		}
		if least < Scrollback {
			t.Errorf("at %d columns the history fell to %d rows, want at least %d", cols, least, Scrollback)
		}
	}
}

func (s state) screenTextOf(y int) string {
	all := state{Rows: len(s.Lines), Lines: s.Lines}
	return all.screenText()[y]
}

func TestAQueryIsAnsweredOnce(t *testing.T) {
	term := newTerminal(t, 80, 24)
	for _, q := range []struct{ query, reply string }{
		{"\x1b[c", "\x1b[?62;22c"},
		{"hello\x1b[6n", "\x1b[1;6R"},
		{"\x1b[14t", "\x1b[4;0;0t"},
		{"\x1b[18t", "\x1b[8;24;80t"},
		{"\x1b[?2004h\x1b[?2004$p", "\x1b[?2004;1$y"},
	} {
		got, err := term.Write([]byte(q.query))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != q.reply {
			t.Errorf("Write(%q) replied %q, want %q", q.query, got, q.reply)
		}
	}
	if got, _ := term.Write([]byte("\x1b]11;?\x1b\\")); len(got) != 0 {
		t.Errorf("OSC 11 before SetColors replied %q, want nothing", got)
	}
	if err := term.SetColors(RGB{0xee, 0xee, 0xee}, RGB{0x10, 0x20, 0x30}); err != nil {
		t.Fatal(err)
	}
	for _, q := range []struct{ query, reply string }{
		{"\x1b]11;?\x1b\\", "\x1b]11;rgb:1010/2020/3030\x1b\\"},
		{"\x1b]10;?\x07", "\x1b]10;rgb:eeee/eeee/eeee\x07"},
	} {
		if got, _ := term.Write([]byte(q.query)); string(got) != q.reply {
			t.Errorf("Write(%q) replied %q, want %q", q.query, got, q.reply)
		}
	}
	// A snapshot holds no query, so drawing it asks nothing again.
	snapshot, err := term.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	fresh := newTerminal(t, 80, 24)
	if got, _ := fresh.Write(snapshot); len(got) != 0 {
		t.Errorf("writing a snapshot replied %q, want nothing", got)
	}
}

func TestResizeAnswersTheSizeReportAProgramAskedFor(t *testing.T) {
	term := newTerminal(t, 80, 24)
	write(t, term, "\x1b[?2048h")
	got, err := term.Resize(100, 30)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "\x1b[48;30;100;") {
		t.Errorf("Resize replied %q, want an in-band size report of 30 rows by 100 columns", got)
	}
	if got, _ := term.Write([]byte("\x1b[18t")); string(got) != "\x1b[8;30;100t" {
		t.Errorf("CSI 18 t after Resize replied %q", got)
	}
}

func TestASnapshotKeepsPaletteIndices(t *testing.T) {
	_, _, snapshot := drawn(t, snapshotCases[2])
	for _, want := range []string{"\x1b[38;5;1m", "\x1b[38;5;196m", "\x1b[48;5;21m", "\x1b]4;1;rgb:12/34/56\x1b\\"} {
		if !bytes.Contains(snapshot, []byte(want)) {
			t.Errorf("the snapshot has no %q", want)
		}
	}
	if n := bytes.Count(snapshot, []byte("\x1b]4;")); n != 1 {
		t.Errorf("the snapshot sets %d palette entries, want the 1 the program set", n)
	}
}

// goldenCase is one case of testdata/snapshot.golden, which
// web/src/terminal/emulator/wterm.test.ts writes into wterm.
type goldenCase struct {
	Name      string   `json:"name"`
	Cols      int      `json:"cols"`
	Rows      int      `json:"rows"`
	Output    string   `json:"output"`
	Snapshot  string   `json:"snapshot"`
	Then      string   `json:"then,omitempty"`
	Screen    []string `json:"screen"`
	CursorRow uint32   `json:"cursorRow"`
	CursorCol uint32   `json:"cursorCol"`
	Alternate bool     `json:"alternate"`
	Title     string   `json:"title"`
	Pwd       string   `json:"pwd"`
}

func TestTheSnapshotGolden(t *testing.T) {
	var cases []goldenCase
	for _, c := range snapshotCases {
		_, restored, snapshot := drawn(t, c)
		s := inspectRows(t, restored, c.rows)
		cases = append(cases, goldenCase{
			Name: c.name, Cols: c.cols, Rows: c.rows,
			Output: c.output, Snapshot: string(snapshot), Then: c.then,
			Screen: s.screenText(), CursorRow: s.CursorY, CursorCol: s.CursorX,
			Alternate: s.Alternate, Title: s.Title, Pwd: s.Pwd,
		})
	}
	// One case to a line, so a change to one shows as that line.
	got := []byte("[\n")
	for i, c := range cases {
		line, err := json.Marshal(c, json.Deterministic(true))
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, line...)
		if i < len(cases)-1 {
			got = append(got, ',')
		}
		got = append(got, '\n')
	}
	got = append(got, "]\n"...)
	const path = "testdata/snapshot.golden"
	if *update {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s is out of date: run go test ./internal/server/vt -run TestTheSnapshotGolden -update", path)
	}
}

// heapInUse is the Go heap in use after a collection, where wazero keeps
// each instance's memory.
func heapInUse() uint64 {
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.HeapInuse
}

// fill writes Scrollback lines and a screen more, in colour, as busy
// output does, so the history is full.
func fill(t *testing.T, term *Terminal) {
	t.Helper()
	var line strings.Builder
	for c := range term.cols - 1 {
		fmt.Fprintf(&line, "\x1b[38;5;%dm%c", c%256, 'a'+c%26)
	}
	line.WriteString("\x1b[0m\r\n")
	write(t, term, strings.Repeat(line.String(), int(historyLimit(term.cols))+term.rows+100))
}

// TestSixteenTerminalsAreFreedOnClose measures 16 terminals of 120 by 40
// with their history full, as one User's Workspace at its limit, and finds
// their memory freed once they are closed.
func TestSixteenTerminalsAreFreedOnClose(t *testing.T) {
	if _, err := engines.wait(t.Context()); err != nil {
		t.Fatal(err)
	}
	before := heapInUse()
	var terms []*Terminal
	for range 16 {
		term, err := New(t.Context(), 120, 40)
		if err != nil {
			t.Fatal(err)
		}
		fill(t, term)
		write(t, term, "\x1b[?1049hvim")
		if _, err := term.Snapshot(); err != nil {
			t.Fatal(err)
		}
		terms = append(terms, term)
	}
	held := heapInUse()
	counted := 0
	for _, term := range terms {
		counted += term.Memory()
	}
	for _, term := range terms {
		if err := term.Close(); err != nil {
			t.Fatal(err)
		}
		if n := term.Memory(); n != 0 {
			t.Errorf("a closed terminal counts %d bytes, want none", n)
		}
	}
	after := heapInUse()
	const mib = 1 << 20
	t.Logf("16 terminals of 120x40, history full, after a snapshot: %.1f MiB held, %.1f MiB counted by Memory, %.1f MiB after Close",
		float64(held-before)/mib, float64(counted)/mib, float64(int64(after)-int64(before))/mib)
	// Memory counts what the budget across cores bounds: the most of what
	// the cores hold, and no more than they hold.
	if h := int64(held - before); int64(counted) < h/2 || int64(counted) > 2*h {
		t.Errorf("Memory counts %.1f MiB for 16 terminals holding %.1f MiB", float64(counted)/mib, float64(h)/mib)
	}
	if grew := int64(after) - int64(before); grew > 4*mib {
		t.Errorf("after Close the heap is %.1f MiB above where it started, want it freed", float64(grew)/mib)
	}
}

// vimOver is the vim case: a shell screen with history, and vim over it.
func vimOver() snapshotCase { return snapshotCases[1] }

// TestAResizeOverVimDrawsTheSameTerminal resizes a terminal while vim runs,
// which reflows the primary screen behind it, and finds its snapshot at
// the new size, then the shell's screen after leaving, the same.
func TestAResizeOverVimDrawsTheSameTerminal(t *testing.T) {
	c := vimOver()
	original := newTerminal(t, c.cols, c.rows)
	write(t, original, c.output)
	if _, err := original.Snapshot(); err != nil { // keeps the primary screen
		t.Fatal(err)
	}
	if _, err := original.Resize(60, 20); err != nil {
		t.Fatal(err)
	}
	snapshot, err := original.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	restored := newTerminal(t, 60, 20)
	write(t, restored, string(snapshot))
	sameTerminal(t, inspect(t, original), inspect(t, restored))
	write(t, original, c.then)
	write(t, restored, c.then)
	sameTerminal(t, inspect(t, original), inspect(t, restored))
}

// TestTheKeptPrimaryScreenIsDroppedWhenItMayChange writes output that
// leaves vim and comes back, in one Write and cut across two, and finds
// each snapshot the same as one taken without the kept primary screen.
func TestTheKeptPrimaryScreenIsDroppedWhenItMayChange(t *testing.T) {
	c := vimOver()
	for _, writes := range [][]string{
		{"\x1b[3;1Hmore notes"},
		{"\x1b[?1049l\r\nshell output\r\n\x1b[?1049h\x1b[Hvim again"},
		{"\x1b[?104", "9l\r\nshell output\r\n\x1b[?10", "49h\x1b[Hvim again"},
		{"\x1bc\x1b[?1049h\x1b[Hafter a reset"},
	} {
		term := newTerminal(t, c.cols, c.rows)
		write(t, term, c.output)
		if _, err := term.Snapshot(); err != nil {
			t.Fatal(err)
		}
		for _, w := range writes {
			write(t, term, w)
		}
		kept, err := term.Snapshot()
		if err != nil {
			t.Fatal(err)
		}
		term.primary = nil
		fresh, err := term.Snapshot()
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(kept, fresh) {
			t.Errorf("after %q the snapshot with the kept primary screen differs from one without", writes)
		}
	}
}

// TestASizePastTheLargestGridIsHeldToIt starts and resizes terminals past
// MaxCols by MaxRows.
func TestASizePastTheLargestGridIsHeldToIt(t *testing.T) {
	term := newTerminal(t, 4096, 4096)
	if cols, rows := term.Size(); cols != MaxCols || rows != MaxRows {
		t.Errorf("New(4096, 4096) made %dx%d, want %dx%d", cols, rows, MaxCols, MaxRows)
	}
	if _, err := term.Resize(80, 1000); err != nil {
		t.Fatal(err)
	}
	if got, _ := term.Write([]byte("\x1b[18t")); string(got) != fmt.Sprintf("\x1b[8;%d;80t", MaxRows) {
		t.Errorf("CSI 18 t after Resize(80, 1000) replied %q", got)
	}
}

// TestACorePastItsMemoryBoundFails floods a terminal with long hyperlinks
// until its core passes memoryBound, and finds the Terminal failed: the
// call that passed it, and every call after, returns the same error.
func TestACorePastItsMemoryBoundFails(t *testing.T) {
	term := newTerminal(t, 120, 40)
	link := strings.Repeat("a", 2000)
	var err error
	for i := 0; err == nil; i++ {
		var line strings.Builder
		for c := range 119 {
			fmt.Fprintf(&line, "\x1b]8;;https://example.com/%d/%d/%s\x1b\\x", i, c, link)
		}
		line.WriteString("\x1b]8;;\x1b\\\r\n")
		_, err = term.Write([]byte(line.String()))
		if i > 5000 {
			t.Fatal("the flood never reached memoryBound")
		}
	}
	if !errors.Is(err, errMemoryBound) {
		t.Fatalf("the flood ended with %v, want errMemoryBound", err)
	}
	if n := term.Memory(); n != 0 {
		t.Errorf("the failed terminal counts %d bytes, want none, its instance freed", n)
	}
	if _, err2 := term.Resize(119, 40); err2 != err { //nolint:errorlint // the same error, as it says
		t.Errorf("Resize after the failure returned %v, want %v", err2, err)
	}
	if _, err2 := term.Snapshot(); err2 != err { //nolint:errorlint // the same error
		t.Errorf("Snapshot after the failure returned %v, want %v", err2, err)
	}
	if err2 := term.SetColors(RGB{}, RGB{}); err2 != err { //nolint:errorlint // the same error
		t.Errorf("SetColors after the failure returned %v, want %v", err2, err)
	}
}

// TestCancellingEndsAWriteUnderWay cancels a Terminal's context during a
// Write of screen alignment tests (DECALN), which takes seconds at the
// largest grid, and finds the Write ended and the Terminal failed.
func TestCancellingEndsAWriteUnderWay(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	term, err := New(ctx, MaxCols, MaxRows)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = term.Close() })
	started := make(chan struct{})
	go func() {
		<-started
		cancel()
	}()
	close(started)
	if _, err := term.Write([]byte(strings.Repeat("\x1b#8", 16<<10))); err == nil {
		t.Fatal("the Write ran to its end, want it ended by the cancel")
	}
	if _, err := term.Write([]byte("more")); err == nil {
		t.Error("a Write after the cancel succeeded, want the Terminal failed")
	}
}

// BenchmarkWrite writes mixed coloured output, as ls --color or a build's
// log, into a terminal of 120 by 40.
func BenchmarkWrite(b *testing.B) {
	term, err := New(b.Context(), 120, 40)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = term.Close() })
	var out strings.Builder
	for i := range 2000 {
		fmt.Fprintf(&out, "\x1b[3%dm%-12s\x1b[0m %8d  some plain text of a log line, \x1b[1mbold\x1b[22m and more\r\n", i%8, "name", i)
	}
	chunk := []byte(out.String())
	b.SetBytes(int64(len(chunk)))
	for b.Loop() {
		if _, err := term.Write(chunk); err != nil {
			b.Fatal(err)
		}
	}
}
