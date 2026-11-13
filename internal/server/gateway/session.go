package gateway

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cieons/koko/internal/server/op"
	"github.com/cieons/koko/internal/server/vt"
	"github.com/cieons/koko/internal/shared/resumable"
	"github.com/cieons/koko/internal/shared/wire"
)

// A Workspace's SSH Session is held by the Gateway apart from any page
// (spec #213, "The SSH Session's loop and fan-out"; ADR 0057). One
// goroutine, its loop, owns its emulator, a vt.Terminal: it reads the
// Host's output, writes it into the emulator and appends it to the queue
// of each page attached, and takes, in turn with the output, attaches,
// detaches, the pages' records and the shell's end. It never waits for a
// page, the Host's stdin or the database. Each attachment has a writer of
// its own that drains its queue into its resumable stream, and a page
// that falls behind gets a reset and a fresh snapshot in place of what it
// missed. An ending never goes through the loop, which may be inside a
// call into the emulator: it closes what the pages and the Host wait on
// first (LESSONS.md, "An ending never waits for a lock that a blocked
// write holds").

const (
	// maxAttachments is how many pages one SSH Session takes at once; the
	// next one displaces the least recently active.
	maxAttachments = 8
	// behindLimit is the most output waiting in one attachment's queue: a
	// page past it has fallen behind, and gets a snapshot in its place.
	behindLimit = 256 << 10
	// stdinLimit is the most bytes waiting for the shell's stdin before the
	// loop stops taking the pages' records, so that a shell that reads
	// nothing holds the pages back, as SSH's window holds back a client.
	stdinLimit = 64 << 10
)

// emulator is an SSH Session's terminal emulator in the koko server: a
// vt.Terminal, or a fake in tests.
type emulator interface {
	Write(p []byte) ([]byte, error)
	Resize(cols, rows int) ([]byte, error)
	SetColors(fg, bg vt.RGB) error
	Snapshot() ([]byte, error)
	Memory() int
	Close() error
}

// newVT makes a vt.Terminal as an emulator.
func newVT(ctx context.Context, cols, rows int) (emulator, error) {
	return vt.New(ctx, cols, rows)
}

// hostShell is the shell on the Host as an SSH Session's loop uses it: a
// terminalShell, or a fake in tests.
type hostShell interface {
	// outputs are the shell's stdout and stderr.
	outputs() []io.Reader
	// Write writes to the shell's stdin.
	Write(p []byte) (int, error)
	// windowChange tells the Host the pty's new size.
	windowChange(cols, rows int)
	// requests serves the Host's requests until they end, keeping how the
	// shell ended.
	requests()
	// ending is why the SSH Session ends once the output and the requests
	// have: the shell ended, or the Host's connection did.
	ending() *ending
	// exitRecords are the terminal records of how the shell ended.
	exitRecords() []byte
	// exit returns the log fields of how the shell ended.
	exit() []any
	// exitState is how the shell ended, for its tab.
	exitState() (*uint32, string)
}

// Why a Workspace's SSH Session ends, besides those a Tunnel shares.
var (
	// errShellEnded: the shell ended, and the Host closed its session
	// channel.
	errShellEnded = &ending{reason: "shell ended"}
	// errRunEnded: the run of koko web the Local caller's page came
	// through ended.
	errRunEnded = &ending{reason: "koko web ended"}
	// errIdle: no page was attached for timing.WorkspaceIdle.
	errIdle = &ending{reason: "idle"}
	// errEmulatorFailed: the emulator failed, and a new one could not be
	// made.
	errEmulatorFailed = &ending{
		reason: "emulator failed",
		notice: "the koko server could not keep this terminal's screen, so this SSH Session is closed; its log says why.",
		why:    "the koko server could not keep this terminal's screen",
	}
)

// tabClosed ends an SSH Session whose tab a page closed: closedBy names the
// caller who closed it, as a log line names who acted.
func tabClosed(closedBy op.Caller) *ending {
	by := deviceSession(closedBy)
	if closedBy.SignIn != "" {
		by = closedBy.SignIn
	}
	return &ending{reason: "tab closed", attrs: []any{"closed_by", by}}
}

// errOverBudget fails the largest emulator once the emulators together
// pass the Gateway's budget (vtBudget).
var errOverBudget = errors.New("the web terminals' emulators passed the koko server's memory budget")

// restartLine is a reopened tab's first output after a restart, before the
// shell's.
const restartLine = "koko: the koko server restarted; this is a new SSH Session.\r\n"

// droppedLine is written into a new emulator that took a failed one's
// place, before the shell's output goes on.
const droppedLine = "\r\nkoko: this terminal's screen and history were dropped; the shell goes on.\r\n"

// termSession is one SSH Session of a Workspace: its tunnel in the
// registry, its tab, its shell, its emulator and its pages.
type termSession struct {
	g   *Gateway
	t   *tunnel
	w   *workspace
	tab string
	a   admission
	// opener is whose page becomes the first active page: the caller who
	// opened or reopened it.
	opener callerKey
	// restarted writes restartLine before the shell's output.
	restarted bool
	// cols and rows are the pty's size: the open's, then the active
	// page's. The loop owns them once it runs.
	cols, rows int

	sh    hostShell
	stdin *stdinQueue

	// The loop's inbox.
	output    chan []byte
	attaches  chan *attachment
	detaches  chan *attachment
	input     chan pageInput
	drained   chan *attachment
	stdinRoom chan struct{}

	// read and written count the bytes of terminal records from and to
	// the pages.
	read, written atomic.Int64
	// snapshots counts the bytes of the snapshots that pages hold, queued
	// or being written, which the budget counts with the emulator.
	snapshots atomic.Int64

	mu sync.Mutex
	// all is every attachment made to the SSH Session that has not ended,
	// which its ending finishes without the loop.
	all map[*attachment]struct{}
	// ended is set once the SSH Session's ending has finished its
	// attachments: no attachment is added after.
	ended bool

	// The loop's own.
	emu emulator
	// emuCtx is the emulator's context, which emuCancel ends: the budget
	// does, on the largest.
	emuCtx    context.Context
	emuCancel context.CancelCauseFunc
	// failing is set while failEmulator replaces the emulator, and
	// failedAt is when it last logged a failure.
	failing  bool
	failedAt time.Time
	// sizeDue is set once the active page or its box changed, for the
	// loop to size the pty once the pages' records waiting are taken.
	sizeDue    bool
	atts       []*attachment
	active     *attachment
	everActive bool
}

// pageInput is a record a page sent, for the loop.
type pageInput struct {
	a   *attachment
	rec wire.TerminalRecord
}

func (g *Gateway) newSession(t *tunnel, w *workspace, tab string, a admission, opener callerKey, cols, rows int) *termSession {
	s := &termSession{
		g: g, t: t, w: w, tab: tab, a: a, opener: opener,
		cols: min(cols, vt.MaxCols), rows: min(rows, vt.MaxRows),
		output:    make(chan []byte),
		attaches:  make(chan *attachment),
		detaches:  make(chan *attachment),
		input:     make(chan pageInput),
		drained:   make(chan *attachment),
		stdinRoom: make(chan struct{}, 1),
		all:       map[*attachment]struct{}{},
	}
	t.session = s
	return s
}

// named are the fields that start a log line about the SSH Session.
func (s *termSession) named() []any {
	return append(s.t.named(), "tab", s.tab)
}

// start makes the emulator, starts the shell's readers and the stdin's
// writer, and runs the loop, each in a goroutine of its own, once the
// shell has started on sh.
func (s *termSession) start(sh hostShell) error {
	if err := s.newEmulator(); err != nil {
		return err
	}
	s.sh = sh
	s.stdin = &stdinQueue{wake: make(chan struct{}, 1), room: s.stdinRoom}
	g := s.g
	g.wg.Go(func() { s.stdin.run(s.t.ctx, sh) })
	var outputs sync.WaitGroup
	for _, r := range sh.outputs() {
		outputs.Go(func() { s.readOutput(r) })
	}
	outputs.Go(sh.requests)
	g.wg.Go(func() {
		outputs.Wait()
		close(s.output)
	})
	// Apart from the Gateway's other goroutines: a call inside the core
	// cannot be stopped, and the koko server's stop waits for none
	// (Gateway.Close).
	g.loops.Go(s.loop)
	return nil
}

// readOutput hands what r, the shell's stdout or stderr, sends to the
// loop, a record's worth at a time, until it ends or the SSH Session does.
func (s *termSession) readOutput(r io.Reader) {
	buf := make([]byte, wire.TerminalMaxRecord)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			select {
			case s.output <- bytes.Clone(buf[:n]):
			case <-s.t.ctx.Done():
				return
			}
		}
		if err != nil {
			return
		}
	}
}

// newEmulator makes the SSH Session's emulator at the pty's size, on a
// context of its own beneath the SSH Session's, which the budget cancels
// to fail it.
func (s *termSession) newEmulator() error {
	ctx, cancel := context.WithCancelCause(s.t.ctx)
	emu, err := s.g.newEmulator(ctx, s.cols, s.rows)
	if err != nil {
		cancel(err)
		return err
	}
	s.emu, s.emuCtx, s.emuCancel = emu, ctx, cancel
	if s.active != nil && s.active.colors != nil {
		_ = s.emu.SetColors(s.active.colors[0], s.active.colors[1]) // a failure shows at the next call
	}
	s.account()
	return nil
}

// failEmulator replaces the emulator that failed with err by a new one at
// the pty's size, which says the screen was dropped, and sends every page
// a reset and its snapshot. The shell and the pages go on. Should a new
// one fail too, the SSH Session ends.
func (s *termSession) failEmulator(err error) {
	if s.t.ctx.Err() != nil {
		return
	}
	if s.failing {
		// The new emulator failed as it took the old one's place, as its
		// snapshot: it would fail again.
		s.g.log.Error("web terminal emulator failed", append(s.named(), "err", err)...)
		s.t.end(errEmulatorFailed)
		return
	}
	s.failing = true
	defer func() { s.failing = false }()
	// A Host whose output fails every new emulator fails it again and
	// again: its failures are logged once a minute.
	level := slog.LevelWarn
	if time.Since(s.failedAt) < time.Minute {
		level = slog.LevelDebug
	} else {
		s.failedAt = time.Now()
	}
	s.g.log.Log(context.Background(), level, "web terminal emulator failed", append(s.named(), "err", err)...)
	_ = s.emu.Close()
	s.g.budget.forget(s)
	if err := s.newEmulator(); err != nil {
		s.g.log.Error("web terminal emulator failed", append(s.named(), "err", err)...)
		s.t.end(errEmulatorFailed)
		return
	}
	replies, err := s.emu.Write([]byte(droppedLine))
	s.stdin.reply(replies)
	if err != nil {
		// A new emulator that fails at once would fail again.
		s.g.log.Error("web terminal emulator failed", append(s.named(), "err", err)...)
		s.t.end(errEmulatorFailed)
		return
	}
	s.resetAll()
}

// account tells the budget how much the emulator and the snapshots that
// pages hold take now, and fails the
// emulator the budget says to, which its loop replaces: the one measured,
// not one made since.
func (s *termSession) account() {
	if fail := s.g.budget.note(s, s.emu.Memory()+int(s.snapshots.Load()), s.emuCancel); fail != nil {
		fail(errOverBudget)
	}
}

// loop is the SSH Session's loop, from its shell's start until it ends.
func (s *termSession) loop() {
	defer func() {
		_ = s.emu.Close()
		s.g.budget.forget(s)
	}()
	idle := time.NewTimer(s.g.timings.WorkspaceIdle)
	defer idle.Stop()
	if s.restarted {
		s.write([]byte(restartLine))
	}
	for {
		input := s.input
		if s.stdin.full() {
			input = nil
		}
		if s.sizeDue {
			// The pages' records already waiting first, so that a burst of
			// resizes sizes the pty once, at the last.
			select {
			case in := <-input:
				s.take(in)
				continue
			default:
			}
			s.sizeDue = false
			if s.active != nil {
				s.resize(s.active.cols, s.active.rows)
			}
		}
		select {
		case <-s.t.ctx.Done():
			return
		case <-s.emuCtx.Done():
			s.failEmulator(context.Cause(s.emuCtx))
		case p, ok := <-s.output:
			if !ok {
				s.t.end(s.sh.ending())
				return
			}
			s.write(p)
		case a := <-s.attaches:
			if s.attach(a) {
				idle.Stop()
			}
		case a := <-s.detaches:
			if s.detach(a) && len(s.atts) == 0 {
				idle.Reset(s.g.timings.WorkspaceIdle)
			}
		case in := <-input:
			s.take(in)
		case a := <-s.drained:
			if a.registered && a.isBehind() {
				s.reset(a)
			}
		case <-s.stdinRoom:
		case <-idle.C:
			s.t.end(errIdle)
			return
		}
	}
}

// write takes output for the pty, the shell's or the Gateway's own: into
// the emulator, whose replies go to the shell's stdin, then into each
// page's queue.
func (s *termSession) write(p []byte) {
	replies, err := s.emu.Write(p)
	s.stdin.reply(replies)
	if err != nil {
		s.failEmulator(err)
		return
	}
	s.account()
	for chunk := range slices.Chunk(p, wire.TerminalMaxRecord) {
		rec, _ := wire.AppendTerminalRecord(nil, wire.TerminalRecord{Type: wire.TerminalData, Data: chunk}) // within its size
		for _, a := range s.atts {
			a.push(rec)
		}
	}
}

// attach registers the page a, unless it has ended, and says whether it
// did: it displaces the least recently active page other than the active
// one when maxAttachments are attached already, becomes the active page
// when it is the opener's first, and gets a reset and its snapshot. A page
// counts as active from its attach until it acts.
func (s *termSession) attach(a *attachment) bool {
	if a.ctx.Err() != nil {
		return false
	}
	if len(s.atts) >= maxAttachments {
		others := slices.DeleteFunc(slices.Clone(s.atts), func(x *attachment) bool { return x == s.active })
		lru := slices.MinFunc(others, func(x, y *attachment) int { return x.lastActive.Compare(y.lastActive) })
		s.detach(lru)
		displaced, _ := wire.AppendTerminalRecord(nil, wire.TerminalRecord{Type: wire.TerminalDisplaced}) // no payload
		lru.end("displaced", displaced, "")
	}
	a.registered, a.lastActive = true, time.Now()
	s.atts = append(s.atts, a)
	s.g.log.Info("ssh session attached", a.attrs()...)
	if !s.everActive && a.key == s.opener {
		s.activate(a)
	}
	s.reset(a)
	return true
}

// detach takes the page a off the SSH Session, and says whether it was
// on. A page that leaves leaves the size as it was.
func (s *termSession) detach(a *attachment) bool {
	i := slices.Index(s.atts, a)
	if i < 0 {
		return false
	}
	s.atts = slices.Delete(s.atts, i, i+1)
	a.registered = false
	if s.active == a {
		s.active = nil
	}
	return true
}

// take takes a record the page in sent: data for the shell's stdin, which
// makes the page active as active does; a resize of the page's box, which
// sizes the pty while the page is active; its theme's colours.
func (s *termSession) take(in pageInput) {
	a, rec := in.a, in.rec
	if !a.registered {
		return
	}
	switch rec.Type {
	case wire.TerminalData:
		s.activate(a)
		s.stdin.put(rec.Data)
	case wire.TerminalActive:
		s.activate(a)
	case wire.TerminalResize:
		a.cols, a.rows = int(rec.Columns), int(rec.Rows)
		s.sizeDue = s.sizeDue || s.active == a
	case wire.TerminalColors:
		a.colors = &[2]vt.RGB{rgb(rec.Foreground), rgb(rec.Background)}
		if s.active == a {
			s.setColors(a)
		}
	}
}

func rgb(c [3]byte) vt.RGB { return vt.RGB{R: c[0], G: c[1], B: c[2]} }

// setColors gives the emulator the active page a's theme, which OSC 10 and
// 11 answer with.
func (s *termSession) setColors(a *attachment) {
	if err := s.emu.SetColors(a.colors[0], a.colors[1]); err != nil {
		s.failEmulator(err)
	}
}

// activate makes the page a the active page, telling it and the page that
// was, and has the pty sized to a's box.
func (s *termSession) activate(a *attachment) {
	a.lastActive = time.Now()
	if s.active == a {
		return
	}
	old := s.active
	s.active, s.everActive, s.sizeDue = a, true, true
	if old != nil {
		old.push(activeRecord(false))
	}
	if a.colors != nil {
		s.setColors(a)
	}
	a.push(activeRecord(true))
}

// activeRecord tells a page whether it is the active page.
func activeRecord(active bool) []byte {
	act := wire.TerminalNoLongerActive
	if active {
		act = wire.TerminalNowActive
	}
	rec, _ := wire.AppendTerminalRecord(nil, wire.TerminalRecord{Type: wire.TerminalActive, Activity: act}) // in range
	return rec
}

// resize sizes the pty to cols by rows, held to vt.MaxCols by vt.MaxRows:
// a window-change on the Host, the emulator's resize, then a reset and a
// snapshot for every page, so that no page reflows by itself. It says
// whether the size changed.
func (s *termSession) resize(cols, rows int) bool {
	cols, rows = min(max(cols, 1), vt.MaxCols), min(max(rows, 1), vt.MaxRows)
	if cols == s.cols && rows == s.rows {
		return false
	}
	s.cols, s.rows = cols, rows
	s.stdin.resize(cols, rows)
	replies, err := s.emu.Resize(cols, rows)
	s.stdin.reply(replies)
	if err != nil {
		s.failEmulator(err)
		return true
	}
	s.account()
	s.resetAll()
	return true
}

// reset hands the page a a reset and its snapshot in place of what waits
// in its queue.
func (s *termSession) reset(a *attachment) {
	if snap := s.snapshot(); snap != nil {
		a.replace(snap, activeRecord(s.active == a))
		s.account()
	}
}

// resetAll hands every page a reset and the snapshot, one copy of it
// shared by them all.
func (s *termSession) resetAll() {
	snap := s.snapshot()
	if snap == nil {
		return
	}
	for _, a := range s.atts {
		a.replace(snap, activeRecord(s.active == a))
	}
	s.account()
}

// snapshot takes the emulator's snapshot, as the pages get it. An
// emulator that fails is replaced, which resets every page itself, so it
// returns nil.
func (s *termSession) snapshot() *snapshot {
	snap, err := s.emu.Snapshot()
	if err != nil {
		s.failEmulator(err)
		return nil
	}
	b, _ := wire.AppendTerminalRecord(nil, wire.TerminalRecord{Type: wire.TerminalReset, Columns: uint32(s.cols), Rows: uint32(s.rows)}) // within maxGrid
	for chunk := range slices.Chunk(snap, wire.TerminalMaxRecord) {
		b, _ = wire.AppendTerminalRecord(b, wire.TerminalRecord{Type: wire.TerminalData, Data: chunk})
	}
	return &snapshot{b: b, s: s}
}

// snapshot is what a page gets on attach, on a change of the pty's size
// and once it has fallen behind, the active record aside: a reset at the
// pty's size and the emulator's snapshot as data records, encoded once
// and shared by every page it goes to. While any page holds it, queued or
// being written, the SSH Session counts its bytes for the budget: at
// 500 by 200 one is tens of MiB.
type snapshot struct {
	b       []byte
	s       *termSession
	holders atomic.Int32
}

func (p *snapshot) hold() {
	if p.holders.Add(1) == 1 {
		p.s.snapshots.Add(int64(len(p.b)))
	}
}

func (p *snapshot) release() {
	if p.holders.Add(-1) == 0 {
		p.s.snapshots.Add(-int64(len(p.b)))
	}
}

// awaitEnd waits for the SSH Session to end, then finishes its pages with
// how it ended: the exit's records after the shell's last output, or the
// ending's notice. It returns the ending, and waits for neither the loop
// nor any writer.
func (s *termSession) awaitEnd() *ending {
	<-s.t.ctx.Done()
	end := endingOf(context.Cause(s.t.ctx))
	var last []byte
	reason := ""
	switch {
	case end == errShellEnded:
		last = s.sh.exitRecords()
	case end.notice != "":
		last, reason = noticeRecord(end.notice), end.notice
	}
	s.finishAll(end, last, reason)
	return end
}

// finishAll ends every attachment as the SSH Session ends, as end says:
// last, the exit's records or the notice, goes after what each has queued,
// and its stream then closes with reason. A page whose stream is off its
// WebSocket is given up at once, and nothing here waits for a writer.
func (s *termSession) finishAll(end *ending, last []byte, reason string) {
	s.mu.Lock()
	s.ended = true
	all := s.all
	s.all = nil
	s.mu.Unlock()
	for a := range all {
		a.endOnce.Do(func() {
			a.cancel(errEnded)
			a.finish(last, reason)
			s.g.forgetAttachment(a, end)
		})
	}
}

// stdinQueue holds what goes to the shell's stdin, the pages' keys and the
// emulator's replies, and the pty's sizes, in the loop's order, for a
// writer of its own: a shell that reads nothing fills SSH's window, and
// the loop must not wait for it.
type stdinQueue struct {
	mu    sync.Mutex
	items []stdinItem
	size  int
	// pty is the pty's size to send, the last asked for, ahead of the
	// data waiting; nil when none is due.
	pty  *[2]int
	wake chan struct{}
	// room hears, once the queue was full, that it is not.
	room chan<- struct{}
}

// stdinItem is bytes for stdin.
type stdinItem struct {
	data []byte
}

func (q *stdinQueue) put(p []byte) {
	if len(p) > 0 {
		q.add(stdinItem{data: p})
	}
}

// reply queues the emulator's replies to the shell's queries, unless the
// queue is full: a program that asks in a loop and reads nothing would
// otherwise grow it without end, and one that reads nothing needs no
// answer.
func (q *stdinQueue) reply(p []byte) {
	if len(p) > 0 && !q.full() {
		q.add(stdinItem{data: p})
	}
}

// resize has the pty's size sent ahead of the data waiting, in place of
// any not yet sent: a shell that reads nothing holds its stdin, and the
// sizes would otherwise pile up behind it.
func (q *stdinQueue) resize(cols, rows int) {
	q.mu.Lock()
	q.pty = &[2]int{cols, rows}
	q.mu.Unlock()
	signal(q.wake)
}

func (q *stdinQueue) add(it stdinItem) {
	q.mu.Lock()
	q.items = append(q.items, it)
	q.size += len(it.data)
	q.mu.Unlock()
	signal(q.wake)
}

// full tells whether the queue holds stdinLimit bytes or more.
func (q *stdinQueue) full() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.size >= stdinLimit
}

// run writes the queue to sh until ctx ends. A Host that has closed the
// channel fails the writes, which drop what they carry.
func (q *stdinQueue) run(ctx context.Context, sh hostShell) {
	for {
		q.mu.Lock()
		if pty := q.pty; pty != nil {
			q.pty = nil
			q.mu.Unlock()
			sh.windowChange(pty[0], pty[1])
			continue
		}
		if len(q.items) == 0 {
			q.mu.Unlock()
			select {
			case <-q.wake:
				continue
			case <-ctx.Done():
				return
			}
		}
		it := q.items[0]
		q.items[0] = stdinItem{}
		q.items = q.items[1:]
		q.mu.Unlock()
		_, _ = sh.Write(it.data)
		q.mu.Lock()
		wasFull := q.size >= stdinLimit
		q.size -= len(it.data)
		roomNow := wasFull && q.size < stdinLimit
		q.mu.Unlock()
		if roomNow {
			signal(q.room)
		}
	}
}

// signal wakes whoever waits on c, a channel of one, unless woken already.
func signal(c chan<- struct{}) {
	select {
	case c <- struct{}{}:
	default:
	}
}

// attachment is one page attached to an SSH Session: its own resumable
// stream on the terminal socket, which the page resumes within
// timing.Grace of its WebSocket's drop, its queue and its writer.
type attachment struct {
	id     tunnelID
	s      *termSession
	caller op.Caller
	key    callerKey
	from   net.Addr
	stream *resumable.Stream
	// ctx ends with the attachment.
	ctx    context.Context
	cancel context.CancelCauseFunc
	// written is closed once the writer has left.
	written chan struct{}

	// The loop's own: the page's box in cells, its theme, when it was last
	// active (or attached), and whether the loop holds it.
	cols, rows int
	colors     *[2]vt.RGB
	lastActive time.Time
	registered bool

	mu sync.Mutex
	// queue is what waits for the writer; queued counts its output.
	queue  []queuedItem
	queued int
	// behind is set once the queue passed behindLimit, its output dropped,
	// until a snapshot takes its place; asked, once the writer asked the
	// loop for it.
	behind, asked bool
	// final is set once the last records are queued, after which the
	// stream closes with reason.
	final  bool
	reason string
	wake   chan struct{}
	// socket is the WebSocket the stream was last on; watched, the channel
	// watched for its end; resuming counts the resumes moving the stream;
	// grace, while the stream is off its WebSocket, ends the attachment.
	socket   TerminalSocket
	watched  <-chan struct{}
	resuming int
	grace    *time.Timer

	endOnce sync.Once
}

// queuedItem is records for the page: output, which behindLimit counts,
// or a shared snapshot, or records of the koko server's own.
type queuedItem struct {
	b       []byte
	counted bool
	snap    *snapshot
}

// bytes is what the item writes.
func (it queuedItem) bytes() []byte {
	if it.snap != nil {
		return it.snap.b
	}
	return it.b
}

// dropQueueLocked empties the queue, letting go of its snapshots. a.mu is
// held.
func (a *attachment) dropQueueLocked() {
	for _, it := range a.queue {
		if it.snap != nil {
			it.snap.release()
		}
	}
	clear(a.queue)
	a.queue, a.queued = a.queue[:0], 0
}

// attrs are the fields of a log line about the attachment.
func (a *attachment) attrs() []any {
	return slices.Concat(a.s.named(), []any{"attachment", a.id}, callerAttrs(a.caller), []any{"addr", a.from})
}

// push queues output for the page, unless it has fallen behind: output
// past behindLimit drops the queue, and the page gets a snapshot once its
// writer has caught up (loop's drained).
func (a *attachment) push(rec []byte) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.final || a.behind {
		return
	}
	if a.queued+len(rec) > behindLimit {
		a.dropQueueLocked()
		a.behind, a.asked = true, false
		signal(a.wake)
		return
	}
	a.queue = append(a.queue, queuedItem{b: rec, counted: true})
	a.queued += len(rec)
	signal(a.wake)
}

// replace hands the page snap, a reset and a snapshot, and then active,
// whether it is the active page, in place of what its queue holds:
// behindLimit counts neither.
func (a *attachment) replace(snap *snapshot, active []byte) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.final {
		return
	}
	a.dropQueueLocked()
	snap.hold()
	a.queue = append(a.queue, queuedItem{snap: snap}, queuedItem{b: active})
	a.behind, a.asked = false, false
	signal(a.wake)
}

func (a *attachment) isBehind() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.behind
}

// run drains the queue into the stream until the last records are
// written, then closes the stream with the reason. A write that fails, its
// stream closed, ends it.
func (a *attachment) run() {
	defer close(a.written)
	defer func() {
		// The snapshots of a queue the stream's close left.
		a.mu.Lock()
		a.dropQueueLocked()
		a.mu.Unlock()
	}()
	for {
		a.mu.Lock()
		if len(a.queue) == 0 {
			final, reason := a.final, a.reason
			ask := a.behind && !a.asked && !final
			a.asked = a.asked || ask
			a.mu.Unlock()
			if final {
				_ = a.stream.CloseWithReason(reason)
				return
			}
			if ask {
				select {
				case a.s.drained <- a:
				case <-a.ctx.Done():
				case <-a.s.t.ctx.Done():
				}
			}
			<-a.wake
			continue
		}
		it := a.queue[0]
		a.queue[0] = queuedItem{}
		a.queue = a.queue[1:]
		if it.counted {
			a.queued -= len(it.b)
		}
		a.mu.Unlock()
		n, err := a.stream.Write(it.bytes())
		a.s.written.Add(int64(n))
		if it.snap != nil {
			it.snap.release()
		}
		if err != nil {
			a.mu.Lock()
			a.final = true
			a.dropQueueLocked()
			a.mu.Unlock()
			return
		}
	}
}

// read reads the page's records off the stream and hands them to the
// loop, until the page leaves: its half-close or its close detaches it, as
// do bytes that are no record, which the stream's close names.
func (a *attachment) read() {
	r := wire.NewTerminalReader(countingReader{a.stream, &a.s.read})
	for {
		rec, err := r.Next()
		if err == nil {
			switch {
			case rec.Type == wire.TerminalData:
				rec.Data = bytes.Clone(rec.Data)
			case rec.Type == wire.TerminalResize, rec.Type == wire.TerminalColors,
				rec.Type == wire.TerminalActive && rec.Activity == wire.TerminalActed:
			default:
				err = wire.ErrTerminalMalformed
			}
		}
		switch {
		case err == nil:
		case errors.Is(err, wire.ErrTerminalMalformed) || errors.Is(err, io.ErrUnexpectedEOF):
			a.end("malformed record", nil, errors.Join(resumable.ErrMalformed, err).Error())
			return
		case errors.Is(a.stream.Err(), resumable.ErrClosed):
			// Closed by this end.
			return
		default:
			// Its half-close or close, or an offset outside the buffer.
			a.end("page left", nil, "")
			return
		}
		select {
		case a.s.input <- pageInput{a: a, rec: rec}:
		case <-a.ctx.Done():
			return
		}
	}
}

// end ends the attachment for reason, as the log line says: last, if any,
// is its last records, and closeReason the reason its stream closes with.
// The loop takes it off the SSH Session.
func (a *attachment) end(reason string, last []byte, closeReason string) {
	a.endOnce.Do(func() {
		s := a.s
		a.cancel(errEnded)
		a.finish(last, closeReason)
		s.mu.Lock()
		delete(s.all, a)
		s.mu.Unlock()
		s.g.forgetAttachment(a, nil)
		s.g.log.Info("ssh session detached", append(a.attrs(), "reason", reason)...)
		s.g.wg.Go(func() {
			select {
			case s.detaches <- a:
			case <-s.t.ctx.Done():
			}
		})
	})
}

// finish queues last after what waits, as the attachment's last records,
// and has its stream closed with reason once they are written: at once
// should its stream be off its WebSocket, as a page that has gone never
// takes them, and after timing.StopNotice for a page that reads nothing,
// whose writer waits for room, so that the close releases it.
func (a *attachment) finish(last []byte, reason string) {
	a.mu.Lock()
	if a.final {
		a.mu.Unlock()
		return
	}
	if len(last) > 0 {
		a.queue = append(a.queue, queuedItem{b: last})
	}
	a.final, a.reason = true, reason
	a.mu.Unlock()
	signal(a.wake)
	if a.stream == nil {
		return
	}
	a.s.g.wg.Go(func() {
		late := time.NewTimer(a.s.g.timings.StopNotice)
		defer late.Stop()
		select {
		case <-a.written:
			return
		case <-a.stream.Detached():
		case <-late.C:
		}
		_ = a.stream.CloseWithReason(reason)
	})
}
