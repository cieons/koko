package gateway

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/cieons/koko/internal/server/op"
	"github.com/cieons/koko/internal/server/vt"
	"github.com/cieons/koko/internal/shared/resumable"
	"github.com/cieons/koko/internal/shared/timing"
	"github.com/cieons/koko/internal/shared/wire"
)

// fakeEmulator stands in for vt.Terminal: its screen is what was written,
// and its snapshot that screen. While block is set, a Write waits for it to
// close, its context or not, as a call inside the core would; failNext
// fails the next Write.
type fakeEmulator struct {
	ctx context.Context

	mu       sync.Mutex
	screen   []byte
	memory   int
	block    chan struct{}
	entered  chan struct{}
	failNext error
	// replies is what each Write answers, as to a query.
	replies []byte
}

func (e *fakeEmulator) Write(p []byte) ([]byte, error) {
	e.mu.Lock()
	block, entered, fail := e.block, e.entered, e.failNext
	e.failNext = nil
	e.mu.Unlock()
	if block != nil {
		close(entered)
		<-block
	}
	if err := cmpErr(fail, e.ctx.Err()); err != nil {
		return nil, err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.screen = append(e.screen, p...)
	return bytes.Clone(e.replies), nil
}

func cmpErr(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

func (e *fakeEmulator) Resize(int, int) ([]byte, error) { return nil, e.ctx.Err() }
func (e *fakeEmulator) SetColors(vt.RGB, vt.RGB) error  { return e.ctx.Err() }

func (e *fakeEmulator) Snapshot() ([]byte, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return bytes.Clone(e.screen), e.ctx.Err()
}

func (e *fakeEmulator) Memory() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.memory
}

func (e *fakeEmulator) Close() error { return nil }

// fakeShell is a shell on the Host: what out writes is its output, and
// what the pages type goes to stdin.
type fakeShell struct {
	output *io.PipeReader
	out    *io.PipeWriter
	done   chan struct{}
	// deaf, once closed, holds every write to stdin until done, as a
	// program that reads nothing does.
	deaf  chan struct{}
	ended sync.Once

	mu    sync.Mutex
	stdin []byte
	pty   [2]int
}

func newFakeShell() *fakeShell {
	r, w := io.Pipe()
	return &fakeShell{output: r, out: w, done: make(chan struct{}), deaf: make(chan struct{})}
}

func (sh *fakeShell) outputs() []io.Reader { return []io.Reader{sh.output} }

func (sh *fakeShell) Write(p []byte) (int, error) {
	select {
	case <-sh.deaf:
		<-sh.done
		return 0, io.ErrClosedPipe
	default:
	}
	sh.mu.Lock()
	defer sh.mu.Unlock()
	sh.stdin = append(sh.stdin, p...)
	return len(p), nil
}

func (sh *fakeShell) windowChange(cols, rows int) {
	sh.mu.Lock()
	defer sh.mu.Unlock()
	sh.pty = [2]int{cols, rows}
}
func (sh *fakeShell) requests()                    { <-sh.done }
func (sh *fakeShell) ending() *ending              { return errShellEnded }
func (sh *fakeShell) exitRecords() []byte          { return nil }
func (sh *fakeShell) exit() []any                  { return nil }
func (sh *fakeShell) exitState() (*uint32, string) { return nil, "" }

// end ends the shell, once: its output and its requests.
func (sh *fakeShell) end() {
	sh.ended.Do(func() {
		_ = sh.out.Close()
		close(sh.done)
	})
}

// testGateway is a Gateway without a store whose emulators are fakes, each
// kept in emulators as it is made, and whose emulators together hold at
// most budget bytes.
type testGateway struct {
	*Gateway
	mu        sync.Mutex
	emulators []*fakeEmulator
	log       *bytes.Buffer
}

func newTestGateway(t *testing.T, budget int) *testGateway {
	var log syncBuffer
	tg := &testGateway{log: &log.b}
	tg.Gateway = New(nil, nil, time.Now, timing.Timings{}, budget, slog.New(slog.NewTextHandler(&log, nil)))
	// A stop's wait for a page or a loop that never comes is shortened.
	tg.timings.StopNotice = 200 * time.Millisecond
	tg.newEmulator = func(ctx context.Context, _, _ int) (emulator, error) {
		e := &fakeEmulator{ctx: ctx}
		tg.mu.Lock()
		tg.emulators = append(tg.emulators, e)
		tg.mu.Unlock()
		return e, nil
	}
	return tg
}

// syncBuffer is a log that goroutines write at once.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

// start starts an SSH Session of the Local caller on a fake shell, which
// finishes its pages as it ends, as runSession does, and is ended with the
// test.
func (tg *testGateway) start(t *testing.T) (*termSession, *fakeShell) {
	tun := tg.newTunnel(op.Caller{Local: true}, clientBrowser, nil)
	s := tg.newSession(tun, nil, "tab", admission{}, callerKey{}, 80, 24)
	sh := newFakeShell()
	if err := s.start(sh); err != nil {
		t.Fatal(err)
	}
	tg.wg.Go(func() { _ = s.awaitEnd() })
	t.Cleanup(func() {
		tun.end(errEnded)
		sh.end()
	})
	return s, sh
}

// page is a page attached to an SSH Session over a pipe: the records it
// got, in order.
type page struct {
	a      *attachment
	stream *resumable.Stream
	mu     sync.Mutex
	recs   []wire.TerminalRecord
	data   []byte
	err    error
	ended  chan struct{}
	// held, while not nil, holds the page's reading until it is closed.
	held chan struct{}
}

// attach attaches a page to s.
func (tg *testGateway) attach(s *termSession) *page {
	gw, pg := net.Pipe()
	a := &attachment{s: s, written: make(chan struct{}), wake: make(chan struct{}, 1), cols: 80, rows: 24}
	a.ctx, a.cancel = context.WithCancelCause(s.t.ctx)
	p := &page{a: a, stream: resumable.New(pg), ended: make(chan struct{})}
	go p.read()
	tg.addAttachment(a, gw, func() {})
	return p
}

func (p *page) read() {
	r := wire.NewTerminalReader(p.stream)
	for {
		p.mu.Lock()
		held := p.held
		p.mu.Unlock()
		if held != nil {
			<-held
		}
		rec, err := r.Next()
		p.mu.Lock()
		if err != nil {
			p.err = p.stream.Err()
			p.mu.Unlock()
			close(p.ended)
			return
		}
		rec.Data = bytes.Clone(rec.Data)
		if rec.Type == wire.TerminalReset {
			p.data = nil
		}
		if rec.Type == wire.TerminalData {
			p.data = append(p.data, rec.Data...)
		}
		p.recs = append(p.recs, rec)
		p.mu.Unlock()
	}
}

// has tells whether the page holds want since its last reset, after n
// resets at least.
func (p *page) has(resets int, want string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	n := 0
	for _, r := range p.recs {
		if r.Type == wire.TerminalReset {
			n++
		}
	}
	return n >= resets && strings.Contains(string(p.data), want)
}

// waitFor waits up to 10 s for cond.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("no %s within 10 s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// In a synctest bubble, an SSH Session with no page attached ends with
// idle once timing.WorkspaceIdle has passed since its start, and one a page
// attached to on day 6 goes on until a week after that page detached.
func TestAnSSHSessionWithNoPageEndsAfterAWeek(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tg := newTestGateway(t, 0)
		t.Cleanup(tg.Close)
		alone, _ := tg.start(t)
		watched, _ := tg.start(t)

		time.Sleep(6 * 24 * time.Hour)
		p := tg.attach(watched)
		synctest.Wait()
		time.Sleep(timing.WorkspaceIdle - 6*24*time.Hour - time.Second)
		synctest.Wait()
		if err := alone.t.ctx.Err(); err != nil {
			t.Fatalf("an SSH Session with no page ended before its week: %v", context.Cause(alone.t.ctx))
		}
		time.Sleep(2 * time.Second)
		synctest.Wait()
		if cause := context.Cause(alone.t.ctx); !errors.Is(cause, errIdle) {
			t.Errorf("an SSH Session with no page for a week ended with %v, want idle", cause)
		}

		time.Sleep(3 * 24 * time.Hour)
		synctest.Wait()
		if watched.t.ctx.Err() != nil {
			t.Fatalf("an SSH Session with a page attached ended: %v", context.Cause(watched.t.ctx))
		}
		_ = p.stream.Close()
		synctest.Wait()
		time.Sleep(timing.WorkspaceIdle - time.Second)
		synctest.Wait()
		if watched.t.ctx.Err() != nil {
			t.Fatal("an SSH Session ended before a week passed since its page detached")
		}
		time.Sleep(2 * time.Second)
		synctest.Wait()
		if cause := context.Cause(watched.t.ctx); !errors.Is(cause, errIdle) {
			t.Errorf("a week after its page detached the SSH Session ended with %v, want idle", cause)
		}
	})
}

// An SSH Session's ending waits for no call into its emulator: while the
// loop is inside a Write that never returns, the ending finishes its page
// with the notice and returns at once.
func TestAnEndingWaitsForNoEmulatorCall(t *testing.T) {
	tg := newTestGateway(t, 0)
	t.Cleanup(tg.Close)
	s, sh := tg.start(t)
	p := tg.attach(s)
	waitFor(t, "snapshot", func() bool { return p.has(1, "") })

	e := tg.emulators[0]
	block, entered := make(chan struct{}), make(chan struct{})
	e.mu.Lock()
	e.block, e.entered = block, entered
	e.mu.Unlock()
	defer close(block)
	go func() { _, _ = sh.out.Write([]byte("output")) }()
	<-entered

	ended := make(chan *ending, 1)
	go func() { ended <- s.awaitEnd() }()
	s.t.end(errStopping)
	select {
	case e := <-ended:
		if e != errStopping {
			t.Errorf("the SSH Session ended with %v, want the stop's ending", e)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the ending waited for the emulator's call")
	}
	select {
	case <-p.ended:
	case <-time.After(2 * time.Second):
		t.Fatal("the page's stream did not close while the emulator's call ran")
	}
	if e, ok := errors.AsType[*resumable.PeerClosedError](p.err); !ok || e.Reason != errStopping.notice {
		t.Errorf("the page's stream ended with %v, want the restart's notice", p.err)
	}
	if !p.has(1, "koko: "+errStopping.notice) {
		t.Errorf("the page got %q, want the notice", p.data)
	}
	// The ending closes the Host connection, which ends the shell.
	sh.end()
	closed := make(chan struct{})
	go func() {
		tg.Close()
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("the Gateway's stop waited for the emulator's call")
	}
	if !strings.Contains(tg.log.String(), `msg="web terminal emulator still in a call as the koko server stops"`) {
		t.Errorf("the log says nothing of the call the stop left:\n%s", tg.log)
	}
}

// The replies to a shell's queries wait for its stdin no further than its
// queue holds: a program that asks in a loop and reads nothing does not
// grow the queue without end.
func TestRepliesToAShellThatReadsNothingAreBounded(t *testing.T) {
	tg := newTestGateway(t, 0)
	t.Cleanup(tg.Close)
	s, sh := tg.start(t)
	close(sh.deaf)
	e := tg.emulators[0]
	e.mu.Lock()
	e.replies = bytes.Repeat([]byte("r"), 1<<10)
	e.mu.Unlock()
	for range 4 * stdinLimit >> 10 {
		if _, err := sh.out.Write([]byte("\x1b[c")); err != nil {
			t.Fatal(err)
		}
	}
	waitFor(t, "the queries taken", func() bool {
		e.mu.Lock()
		defer e.mu.Unlock()
		return len(e.screen) == 4*stdinLimit>>10*3
	})
	s.stdin.mu.Lock()
	size := s.stdin.size
	s.stdin.mu.Unlock()
	if size > stdinLimit+1<<10 {
		t.Errorf("the replies waiting for stdin hold %d bytes, want at most %d", size, stdinLimit+1<<10)
	}
}

// An emulator that fails, its core past its bound or the emulators
// together past the budget, is made anew: the shell and the page go on,
// and the page gets a reset and a snapshot saying the screen was dropped.
// The budget fails the largest emulator, not the one whose call passed it.
func TestAFailedEmulatorIsMadeAnew(t *testing.T) {
	tg := newTestGateway(t, 32<<20)
	t.Cleanup(tg.Close)
	s, sh := tg.start(t)
	p := tg.attach(s)
	waitFor(t, "snapshot", func() bool { return p.has(1, "") })

	e := tg.emulators[0]
	e.mu.Lock()
	e.failNext = errors.New("the core holds more than 96 MiB")
	e.mu.Unlock()
	_, _ = sh.out.Write([]byte("flood"))
	waitFor(t, "a reset saying the screen was dropped", func() bool { return p.has(2, strings.TrimSpace(droppedLine)) })
	_, _ = sh.out.Write([]byte("after"))
	waitFor(t, "output after the new emulator", func() bool { return p.has(2, "after") })

	other, otherShell := tg.start(t)
	otherPage := tg.attach(other)
	waitFor(t, "snapshot", func() bool { return otherPage.has(1, "") })
	tg.mu.Lock()
	large, small := tg.emulators[1], tg.emulators[2]
	tg.mu.Unlock()
	large.mu.Lock()
	large.memory = 30 << 20
	large.mu.Unlock()
	small.mu.Lock()
	small.memory = 10 << 20
	small.mu.Unlock()
	_, _ = sh.out.Write([]byte("grow"))
	waitFor(t, "output", func() bool { return p.has(2, "grow") })
	_, _ = otherShell.out.Write([]byte("passes"))
	waitFor(t, "a reset of the largest saying the screen was dropped", func() bool { return p.has(3, strings.TrimSpace(droppedLine)) })
	if otherPage.has(2, "") {
		t.Error("the budget failed the emulator whose call passed it, not the largest")
	}
	if !strings.Contains(tg.log.String(), `msg="web terminal emulator failed"`) {
		t.Errorf("the log says nothing of the failures:\n%s", tg.log)
	}
}

// A reset's snapshot is one copy shared by every page it goes to, while
// they read nothing, and the budget counts the snapshots the pages hold,
// queued or being written, beside the emulator.
func TestPagesShareTheirSnapshotsWhichTheBudgetCounts(t *testing.T) {
	tg := newTestGateway(t, 0)
	t.Cleanup(tg.Close)
	s, sh := tg.start(t)
	e := tg.emulators[0]
	const size = 3 << 20 // past what a stream's buffer takes
	e.mu.Lock()
	e.screen = bytes.Repeat([]byte("x"), size)
	e.mu.Unlock()
	var pages []*page
	for range maxAttachments {
		p := tg.attach(s)
		waitFor(t, "snapshot", func() bool { return p.has(1, "x") })
		pages = append(pages, p)
	}
	held := make(chan struct{})
	t.Cleanup(func() { close(held) })
	for _, p := range pages {
		p.mu.Lock()
		p.held = held
		p.mu.Unlock()
	}
	for i := range 5 {
		rec, _ := wire.AppendTerminalRecord(nil, wire.TerminalRecord{Type: wire.TerminalResize, Columns: uint32(81 + i), Rows: 24})
		if _, err := pages[0].stream.Write(rec); err != nil {
			t.Fatal(err)
		}
	}
	waitFor(t, "the last size", func() bool {
		sh.mu.Lock()
		defer sh.mu.Unlock()
		return sh.pty == [2]int{85, 24}
	})
	waitFor(t, "the snapshots held", func() bool { return s.snapshots.Load() > 0 })
	queued := map[*snapshot]bool{}
	for _, p := range pages {
		p.a.mu.Lock()
		for _, it := range p.a.queue {
			if it.snap != nil {
				queued[it.snap] = true
			}
		}
		p.a.mu.Unlock()
	}
	if len(queued) != 1 {
		t.Errorf("%d pages that read nothing hold %d snapshots in their queues, want the last one, shared", maxAttachments, len(queued))
	}
	tg.budget.mu.Lock()
	counted := tg.budget.held[s].n
	tg.budget.mu.Unlock()
	if counted < int(s.snapshots.Load()) {
		t.Errorf("the budget counts %d bytes for the SSH Session, want the %d its pages' snapshots hold among them", counted, s.snapshots.Load())
	}
}
