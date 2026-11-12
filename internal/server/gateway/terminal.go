package gateway

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/cieons/koko/internal/server/hostconn"
	"github.com/cieons/koko/internal/server/op"
	"github.com/cieons/koko/internal/shared/api"
	"github.com/cieons/koko/internal/shared/liveness"
	"github.com/cieons/koko/internal/shared/resumable"
	"github.com/cieons/koko/internal/shared/wire"
)

// A web terminal's SSH Session (P1 spec: a terminal in the web UI, "The
// Gateway's terminal SSH Session"; ADR 0053) is a second way into the
// Gateway beside Open. The web front reads a page's first message on a
// WebSocket and hands the WebSocket over (TerminalSocket). The Gateway
// admits the SSH Session as it admits a Tunnel's, counts it against the
// caller's limit, dials the Host, opens one session channel there with a pty
// and a shell, answers the page with the SSH Session's id, and then runs the
// resumable stream on the WebSocket's binary messages, carrying terminal
// records (wire.TerminalRecord) both ways. It is a tunnel in the registry,
// of the client clientBrowser, so Close, Withdraw, recheck, keepEnding and
// the limit serve it as they serve a Tunnel; its grace is
// timing.BrowserGrace, since the leg that drops is a browser's.

// TerminalCaller is who opens a web terminal's SSH Session, as the web front
// found them.
type TerminalCaller struct {
	// Caller is the live Sign-in the cookie names on the web listener; on
	// web@koko, the Device Session or the Local caller koko web runs as.
	Caller op.Caller
	// Run, for the Local caller, names the run of koko web the page came
	// through.
	Run string
	// Addr is where the page came from: on the web listener, the client's
	// address the listener counts; through koko web, its connection's.
	Addr net.Addr
}

// TerminalSocket is a web terminal's WebSocket, once the web front has read
// its first message: the Gateway answers that message on it, then runs the
// resumable stream on its binary messages.
type TerminalSocket interface {
	// Answer sends the answer to the first message, one text message of
	// JSON.
	Answer(wire.TerminalAnswer) error
	// Refuse sends a refusal, then closes the WebSocket with status 1008.
	Refuse(wire.TerminalAnswer)
	// Conn is the WebSocket's binary messages as one byte stream, for the
	// resumable stream once the answer is sent. Closing it closes the
	// WebSocket.
	Conn() net.Conn
	// Cut closes the WebSocket at once, without its closing handshake, as
	// for a page too stalled to take it, or one the koko server stops for.
	Cut()
}

// Why a web terminal's SSH Session ends, besides those a Tunnel shares.
var (
	// errShellEnded: the shell ended, and the Host closed its session
	// channel.
	errShellEnded = &ending{reason: "shell ended"}
	// errMalformed: the page sent bytes that are no terminal record.
	errMalformed = &ending{reason: "malformed record"}
)

// OpenTerminal opens an SSH Session to the Host named hostName for the
// caller c, with a pty of cols columns and rows rows, on the WebSocket s,
// and serves it in a goroutine of its own. While the Gateway stops, it cuts
// s instead.
func (g *Gateway) OpenTerminal(c TerminalCaller, s TerminalSocket, hostName string, cols, rows uint32) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed || g.terminalsClosed {
		s.Cut()
		return
	}
	g.wg.Go(func() { g.serveTerminal(c, s, hostName, cols, rows) })
}

// serveTerminal serves one web terminal's SSH Session from its admission
// until it ends. Every refusal is the answer to the page's open, with the
// code the page tests and the banner a Tunnel's client would get, so an
// unseen Host gets an unknown name's bytes; the 17th SSH Session of a
// Sign-in, or the 65th of a Device Session, dials no Host.
func (g *Gateway) serveTerminal(c TerminalCaller, s TerminalSocket, hostName string, cols, rows uint32) {
	seen := g.withdrawalsTaken()
	a, err := g.admit(context.Background(), c.Caller, hostName)
	if err != nil {
		g.log.Error("open a web terminal", slices.Concat([]any{"client", clientBrowser}, callerAttrs(c.Caller), []any{"addr", c.Addr, "name", hostName, "err", err})...)
		s.Refuse(wire.TerminalRefusal(api.CodeInternal.Name, "the koko server could not open this SSH Session; its log says why"))
		return
	}
	t := g.newTunnel(c.Caller, clientBrowser, c.Addr)
	t.run, t.holdFor = c.Run, g.timings.BrowserGrace
	limit, ok := g.register(t, a, seen)
	if !ok {
		s.Cut()
		return
	}
	defer g.unregister(t)
	defer t.end(errEnded)
	if limit != nil {
		a.refused = limit
	}
	if a.refused != nil {
		g.logRefused(t, hostName, a.refused)
		s.Refuse(wire.TerminalRefusal(a.refused.code, a.refused.message()))
		return
	}

	shell, refused, err := g.startShell(t, a, cols, rows)
	if shell != nil {
		defer shell.close()
	}
	switch {
	case t.ctx.Err() != nil:
		// Ended while it opened: the koko server stops, or its access was
		// withdrawn, which the page is told.
		e := endingOf(context.Cause(t.ctx))
		g.log.Debug("ssh session ended before it started", slices.Concat(t.named(), []any{"name", hostName}, callerAttrs(t.caller), []any{"addr", t.from, "err", e})...)
		if e.notice == "" || e == errStopping {
			s.Cut()
			return
		}
		s.Refuse(wire.TerminalRefusal(wire.TerminalAccessRefused, e.notice))
		return
	case refused != nil:
		g.logRefused(t, hostName, refused)
		s.Refuse(wire.TerminalRefusal(refused.code, refused.message()))
		return
	case err != nil:
		g.log.Error("open a web terminal", slices.Concat(t.named(), []any{"name", hostName}, callerAttrs(t.caller), []any{"addr", t.from, "err", err})...)
		s.Refuse(wire.TerminalRefusal(api.CodeInternal.Name, "the koko server could not open this SSH Session; its log says why"))
		return
	}
	if err := s.Answer(wire.TerminalAnswer{Tunnel: t.id.String()}); err != nil {
		g.log.Debug("ssh session ended before it started", slices.Concat(t.named(), []any{"name", hostName}, callerAttrs(t.caller), []any{"addr", t.from, "err", err})...)
		return
	}
	sc := newSocketConn(s.Conn())
	g.attachTerminal(t, s, resumable.New(sc), sc)

	started := time.Now()
	who := sessionAttrs(t, a)
	g.log.Info("ssh session started", slices.Concat(who, []any{"addr", t.from})...)
	g.log.Info("ssh session request", "tunnel", t.id, "client", t.client, "host", a.host.Name, "request", "shell")
	shell.stream = t.stream
	end := shell.relay(t.ctx)
	t.end(end)
	g.log.Info("ssh session ended", slices.Concat(who, []any{
		"duration", time.Since(started).Round(time.Millisecond),
		"bytes_from_device", shell.read.Load(), "bytes_to_device", shell.written.Load(),
	}, shell.exit(), []any{"reason", end.reason})...)
}

// ResumeTerminal Resumes the web terminal's SSH Session id on the WebSocket
// s, from offset, the page's received offset, and serves the resume in a
// goroutine of its own, as the device's resume of a Tunnel goes (resume.go):
// it is taken only from the caller c who opened it, the same Sign-in,
// Device Session or run of koko web, and only once its access holds still
// (recheckResume). The Gateway answers with its own received offset and
// moves the stream onto s, off the WebSocket it was on, if any: the last
// attach wins. Any other resume is refused, saying why the SSH Session
// ended when the Gateway knows. While the Gateway stops, it cuts s instead.
func (g *Gateway) ResumeTerminal(c TerminalCaller, s TerminalSocket, id [wire.TunnelIDSize]byte, offset uint64) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed || g.terminalsClosed {
		s.Cut()
		return
	}
	g.wg.Go(func() { g.resumeTerminal(c, s, tunnelID(id), offset) })
}

func (g *Gateway) resumeTerminal(c TerminalCaller, s TerminalSocket, id tunnelID, offset uint64) {
	attrs := slices.Concat([]any{"tunnel", id, "client", clientBrowser}, callerAttrs(c.Caller), []any{"addr", c.Addr})
	t, ended := g.resumable(id, callerKey{id: callerID(c.Caller), run: c.Run}, clientBrowser)
	if t != nil {
		ended = g.recheckResume(t)
	}
	if t == nil || ended != nil {
		reason, why := "unknown tunnel", ""
		if ended != nil {
			reason, why = ended.reason, ended.why
		}
		g.log.Info("ssh session resume refused", append(attrs, "reason", reason)...)
		s.Refuse(wire.TerminalRefusal(wire.TerminalResumeRefused, why))
		return
	}
	t.mu.Lock()
	t.resuming++
	t.mu.Unlock()
	sc := newSocketConn(s.Conn())
	err := t.stream.AcceptResumeAnswering(sc, offset, func(own uint64) error {
		return s.Answer(wire.TerminalAnswer{Offset: &own})
	})
	t.mu.Lock()
	t.resuming--
	if err != nil {
		// The stream stays Detached, unless the page's offset was outside
		// its buffer, which ends it and its SSH Session.
		g.holdLocked(t)
	} else {
		t.stopGraceLocked()
		t.socket = s
	}
	t.mu.Unlock()
	if err != nil {
		g.log.Info("ssh session resume failed", append(attrs, "err", err)...)
		return
	}
	g.log.Info("ssh session resumed", attrs...)
	g.watch(t)
	g.wg.Go(func() { g.watchSocket(t, s, sc) })
}

// EndTerminal ends the web terminal's SSH Session id of the caller c as the
// client's close, should the Gateway hold it, and returns once it has
// ended, so that a page reloaded, which has lost its streams, ends the SSH
// Sessions of its tabs before it opens new ones, which they would otherwise
// count against its limit. The page that held it is gone, so the WebSocket
// it may still seem to be on is cut rather than waited for.
func (g *Gateway) EndTerminal(c TerminalCaller, id [wire.TunnelIDSize]byte) {
	// Whether or not its stream is attached yet, or it has ended already.
	g.mu.Lock()
	t := g.ownLocked(tunnelID(id), callerKey{id: callerID(c.Caller), run: c.Run}, clientBrowser)
	g.mu.Unlock()
	if t == nil {
		return
	}
	t.end(clientClosed)
	t.cut()
	<-t.done
}

// attachTerminal puts the web terminal t's stream, new on sc, the WebSocket
// s, in its Tunnel, where a resume finds it, has the Tunnel Detached once
// the stream comes off s, and watches s's liveness.
func (g *Gateway) attachTerminal(t *tunnel, s TerminalSocket, stream *resumable.Stream, sc *socketConn) {
	g.mu.Lock()
	t.stream = stream
	g.mu.Unlock()
	t.mu.Lock()
	t.socket = s
	t.mu.Unlock()
	g.watch(t)
	g.wg.Go(func() { g.watchSocket(t, s, sc) })
}

// socketConn is a web terminal's WebSocket as its stream runs on it: it
// notes when bytes last arrived (liveness.Conn) and when the stream last
// sent, for watchSocket, and tells when the stream has come off it.
type socketConn struct {
	*liveness.Conn
	start time.Time
	// sent is when the stream last wrote, as time since start.
	sent      atomic.Int64
	closed    chan struct{}
	closeOnce sync.Once
}

func newSocketConn(nc net.Conn) *socketConn {
	return &socketConn{Conn: liveness.NewConn(nc), start: time.Now(), closed: make(chan struct{})}
}

func (c *socketConn) Write(p []byte) (int, error) {
	n, err := c.Conn.Write(p)
	if n > 0 {
		c.sent.Store(int64(time.Since(c.start)))
	}
	return n, err
}

// Close closes the WebSocket, as the stream does once it comes off it.
func (c *socketConn) Close() error {
	c.closeOnce.Do(func() { close(c.closed) })
	return c.Conn.Close()
}

// silent returns how long the stream has sent nothing on c.
func (c *socketConn) silent() time.Duration {
	return time.Since(c.start) - time.Duration(c.sent.Load())
}

// watchSocket watches the WebSocket s, sc beneath the web terminal t's
// stream, until the stream comes off it. A page can neither send nor see a
// WebSocket's pings, so liveness is in the stream: after
// timing.ServerProbe without sending, the Gateway asks the stream for an
// acknowledgement-only frame (resumable.Stream.Probe), which also keeps a
// proxy from cutting a quiet terminal; after timing.ServerDead without a
// byte from the page, it cuts the WebSocket, which Detaches the SSH
// Session. The page does the same with the same values.
func (g *Gateway) watchSocket(t *tunnel, s TerminalSocket, sc *socketConn) {
	probe, dead := g.timings.ServerProbe, g.timings.ServerDead
	timer := time.NewTimer(min(probe, dead))
	defer timer.Stop()
	for {
		select {
		case <-sc.closed:
			return
		case <-timer.C:
		}
		quiet := sc.Quiet()
		if quiet >= dead {
			g.log.Debug("web terminal found dead", "tunnel", t.id, "quiet", quiet.Round(time.Millisecond))
			s.Cut()
			return
		}
		next := probe - sc.silent()
		if next <= 0 {
			t.stream.Probe()
			next = probe
		}
		timer.Reset(min(next, dead-quiet))
	}
}

// CloseTerminals ends every web terminal's SSH Session, telling each that
// the koko server is restarting, and refuses new ones. server.Close calls it
// before the web listener closes, whose shutdown does not track a
// WebSocket. It waits for each to give its notice and close its WebSocket,
// and once timing.StopNotice has passed it cuts the WebSockets of those
// still at it, which a page too stalled to take the notice holds.
func (g *Gateway) CloseTerminals() {
	g.mu.Lock()
	g.terminalsClosed = true
	var ts []*tunnel
	for _, t := range g.tunnels {
		if t.client == clientBrowser {
			t.end(errStopping)
			ts = append(ts, t)
		}
	}
	g.mu.Unlock()
	late := time.NewTimer(g.timings.StopNotice)
	defer late.Stop()
	for i, t := range ts {
		select {
		case <-t.done:
		case <-late.C:
			for _, t := range ts[i:] {
				t.cut()
			}
			for _, t := range ts[i:] {
				<-t.done
			}
			return
		}
	}
}

// cut closes the WebSocket the web terminal t was last attached to, if any,
// at once.
func (t *tunnel) cut() {
	t.mu.Lock()
	s := t.socket
	t.mu.Unlock()
	if s != nil {
		s.Cut()
	}
}

// startShell dials the Host a admitted for the web terminal t, through its
// Relay if any, and starts a shell there on one session channel with a pty
// of cols columns and rows rows, asking for TERM xterm-256color and IUTF8,
// so that a shell's line editor erases a whole Chinese character. It returns
// the refusal of a Host the connector could not log in to, or that refused
// the session, its pty or its shell, and fails on t's end.
func (g *Gateway) startShell(t *tunnel, a admission, cols, rows uint32) (*terminalShell, *refusal, error) {
	var relay *hostconn.Target
	if r := a.host.Relay; r != nil {
		relay = new(kokoTarget(r.Reach, a.kokoKey))
	}
	conn, err := g.connector.Connect(t.ctx, kokoTarget(a.host, a.kokoKey), relay)
	if err != nil {
		return nil, connectRefusal(a.host, err), nil
	}
	sh := &terminalShell{host: conn, hostName: a.host.Name, relayName: relayName(a.host)}
	// A Host that answers neither the open nor a request would hold the
	// SSH Session's end, as when the koko server stops: the end closes the
	// connection, which fails them.
	defer context.AfterFunc(t.ctx, func() { _ = conn.Close() })()
	refused := &refusal{
		banner: "koko: " + a.host.Name + " refused a shell with a terminal for its Host Account \"" + a.host.Account + "\"; this SSH Session did not start.\n",
		code:   wire.TerminalShellRefused, reason: "shell refused",
	}
	ch, reqs, err := conn.OpenChannel("session", nil)
	if e, ok := errors.AsType[*ssh.OpenChannelError](err); ok {
		refused.attrs = []any{"err", e}
		return sh, refused, nil
	}
	if err != nil {
		return sh, nil, fmt.Errorf("open a session channel on %s: %w", a.host.Name, err)
	}
	sh.ch, sh.reqs = ch, reqs
	pty := ssh.Marshal(struct {
		Term                         string
		Columns, Rows, Width, Height uint32
		Modes                        string
	}{Term: "xterm-256color", Columns: cols, Rows: rows, Modes: string(ttyModes)})
	for _, r := range []struct {
		kind    string
		payload []byte
	}{{"pty-req", pty}, {"shell", nil}} {
		ok, err := ch.SendRequest(r.kind, true, r.payload)
		if err != nil {
			return sh, nil, fmt.Errorf("ask %s for a %s: %w", a.host.Name, r.kind, err)
		}
		if !ok {
			refused.attrs = []any{"request", r.kind}
			return sh, refused, nil
		}
	}
	return sh, nil, nil
}

// ttyModes are the terminal modes a web terminal's pty-req asks for, in
// SSH's encoding (RFC 4254, section 8): IUTF8 (opcode 42) on, then
// TTY_OP_END.
var ttyModes = []byte{42, 0, 0, 0, 1, 0}

// terminalShell is a web terminal's shell on the Host: one session channel
// on the Gateway's connection to the Host named hostName, through the Relay
// relayName names, if any, relayed to and from the terminal records of a
// resumable stream.
type terminalShell struct {
	host      *hostconn.Conn
	hostName  string
	relayName string
	ch        ssh.Channel
	reqs      <-chan *ssh.Request
	stream    *resumable.Stream

	// writeMu makes the records written into the stream take turns: the
	// Host's stdout's, its stderr's, the notice and the exit record.
	// closed is set once the last record is written, after which none is.
	writeMu sync.Mutex
	closed  bool

	// read and written count the stream's bytes from and to the page.
	read, written atomic.Int64

	mu sync.Mutex
	// exitStatus and exitSignal are what the Host sent to say how the shell
	// ended.
	exitStatus *uint32
	exitSignal string
	// malformed is why the page's bytes were no record, once they were not.
	malformed error

	closeOnce sync.Once
}

// close closes the session channel, if open, and the connection to the
// Host, once.
func (sh *terminalShell) close() {
	sh.closeOnce.Do(func() {
		if sh.ch != nil {
			_ = sh.ch.Close()
		}
		_ = sh.host.Close()
	})
}

// relay relays the shell to and from the stream until the shell ends, the
// Host's connection does, the page closes the stream or sends bytes that
// are no record, or ctx ends with the Tunnel. What the Host sends becomes
// data records, stdout and stderr merged as a pty merges them; data records
// from the page go to the shell's stdin, and resize records become a
// window-change. Once the shell ends it writes its exit record after the
// last output, then closes the stream with no reason. An ending with a
// notice writes it as a data line, then closes the stream with it. It
// returns why the SSH Session ended.
func (sh *terminalShell) relay(ctx context.Context) *ending {
	var wg sync.WaitGroup
	defer wg.Wait()
	ended := make(chan *ending, 2)
	var output sync.WaitGroup
	output.Go(func() { sh.output(sh.ch) })
	output.Go(func() { sh.output(sh.ch.Stderr()) })
	requests := make(chan struct{})
	wg.Go(func() {
		defer close(requests)
		sh.hostRequests()
	})
	wg.Go(func() {
		output.Wait()
		<-requests
		// A channel the Host's connection took down sees it broken.
		if sh.host.Broken() != nil {
			ended <- hostEnding(sh.host, sh.hostName, sh.relayName)
			return
		}
		ended <- errShellEnded
	})
	wg.Go(func() {
		if e := sh.input(); e != nil {
			ended <- e
		}
	})

	var end *ending
	select {
	case end = <-ended:
	case <-ctx.Done():
	}
	if ctx.Err() != nil {
		end = endingOf(context.Cause(ctx))
	}
	switch {
	case end == errShellEnded:
		// A page that takes nothing more, Detached with the buffer full,
		// holds the exit record back until it Resumes, or until the SSH
		// Session ends otherwise, which then says why.
		if !sh.last(sh.exitRecords(), ctx.Done(), &wg) {
			end = endingOf(context.Cause(ctx))
		}
		_ = sh.stream.CloseWithReason(end.notice)
	case end.notice != "":
		sh.last(noticeRecord(end.notice), sh.stream.Detached(), &wg)
		_ = sh.stream.CloseWithReason(end.notice)
	default:
		// Nothing more is written. Closing the stream first ends a write
		// of output waiting for room, which holds the records' turn.
		reason := ""
		if end == errMalformed {
			sh.mu.Lock()
			reason = sh.malformed.Error()
			sh.mu.Unlock()
		}
		_ = sh.stream.CloseWithReason(reason)
	}
	// Ends the reads of the channel, its requests and the stream.
	sh.close()
	return end
}

// output writes what r, the shell's stdout or stderr, sends into the stream
// as data records, until it ends.
func (sh *terminalShell) output(r io.Reader) {
	buf := make([]byte, wire.TerminalMaxRecord)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			rec, _ := wire.AppendTerminalRecord(nil, wire.TerminalRecord{Type: wire.TerminalData, Data: buf[:n]}) // within its size
			if !sh.write(rec) {
				return
			}
		}
		if err != nil {
			return
		}
	}
}

// write writes the encoded records recs into the stream, after any written
// before, unless the last has been, and says whether it did.
func (sh *terminalShell) write(recs []byte) bool {
	sh.writeMu.Lock()
	defer sh.writeMu.Unlock()
	if sh.closed {
		return false
	}
	n, err := sh.stream.Write(recs)
	sh.written.Add(int64(n))
	return err == nil
}

// last writes the encoded records recs as the last the stream carries,
// after any written before, in a goroutine wg waits for, and says once they
// are in the stream. It gives up, saying false, once giveUp is closed
// first: a write of output waiting for room in a full buffer, which a
// Detached or stalled page never frees, holds the records' turn, and only
// the stream's close, which the caller makes then, ends it and drops them.
func (sh *terminalShell) last(recs []byte, giveUp <-chan struct{}, wg *sync.WaitGroup) bool {
	written := make(chan struct{})
	wg.Go(func() {
		defer close(written)
		sh.writeMu.Lock()
		defer sh.writeMu.Unlock()
		if !sh.closed && len(recs) > 0 {
			n, _ := sh.stream.Write(recs)
			sh.written.Add(int64(n))
		}
		sh.closed = true
	})
	select {
	case <-written:
		return true
	case <-giveUp:
		return false
	}
}

// noticeRecord is an ending's notice as a web terminal shows it, in its
// output and its scrollback, as ssh shows a device's on stderr: a data
// record of one line of its own, starting on a new line wherever the
// cursor stood.
func noticeRecord(notice string) []byte {
	rec, _ := wire.AppendTerminalRecord(nil, wire.TerminalRecord{Type: wire.TerminalData, Data: []byte("\r\nkoko: " + notice + "\r\n")}) // a notice is short
	return rec
}

// exitRecords are the records of how the Host said the shell ended: its
// exit status, the signal that ended it, both, or neither.
func (sh *terminalShell) exitRecords() []byte {
	sh.mu.Lock()
	defer sh.mu.Unlock()
	var recs []byte
	if sh.exitStatus != nil {
		recs, _ = wire.AppendTerminalRecord(recs, wire.TerminalRecord{Type: wire.TerminalExitStatus, Status: *sh.exitStatus})
	}
	if sh.exitSignal != "" {
		// A signal's name is short; one past a record's size is dropped.
		recs, _ = wire.AppendTerminalRecord(recs, wire.TerminalRecord{Type: wire.TerminalExitSignal, Signal: sh.exitSignal})
	}
	return recs
}

// exit returns the log fields for how the Host said the shell ended.
func (sh *terminalShell) exit() []any {
	sh.mu.Lock()
	defer sh.mu.Unlock()
	var attrs []any
	if sh.exitStatus != nil {
		attrs = append(attrs, "exit_status", *sh.exitStatus)
	}
	if sh.exitSignal != "" {
		attrs = append(attrs, "exit_signal", sh.exitSignal)
	}
	return attrs
}

// hostRequests answers the requests the Host sends on the session channel
// until it closes, keeping how the shell ended: exit-status and
// exit-signal. It refuses every other one, the Host's keepalive@openssh.com
// among them, which sshd takes as the answer (hostKeepalive).
func (sh *terminalShell) hostRequests() {
	for r := range sh.reqs {
		var status struct{ Status uint32 }
		var signal struct {
			Signal     string
			CoreDumped bool
			Error      string
			Lang       string
		}
		sh.mu.Lock()
		switch {
		case r.Type == "exit-status" && ssh.Unmarshal(r.Payload, &status) == nil:
			sh.exitStatus = &status.Status
		case r.Type == "exit-signal" && ssh.Unmarshal(r.Payload, &signal) == nil:
			sh.exitSignal = signal.Signal
		}
		sh.mu.Unlock()
		if r.WantReply {
			_ = r.Reply(false, nil)
		}
	}
}

// input reads the page's records from the stream and passes them to the
// shell: data to its stdin, a resize as a window-change, the pixel sizes 0.
// The page's half-close passes on as the stdin's EOF. It returns how the
// SSH Session ends when the page closed the stream (clientClosed) or sent
// bytes that are no record (errMalformed), or nil once the stream ends
// otherwise, as when the relay closed it.
func (sh *terminalShell) input() *ending {
	r := wire.NewTerminalReader(countingReader{sh.stream, &sh.read})
	for {
		rec, err := r.Next()
		if errors.Is(err, io.EOF) {
			_ = sh.ch.CloseWrite()
			return nil
		}
		if err == nil && rec.Type != wire.TerminalData && rec.Type != wire.TerminalResize {
			err = wire.ErrTerminalMalformed
		}
		if _, ok := errors.AsType[*resumable.PeerClosedError](err); ok {
			return clientClosed
		}
		if errors.Is(err, wire.ErrTerminalMalformed) || errors.Is(err, io.ErrUnexpectedEOF) {
			sh.mu.Lock()
			sh.malformed = errors.Join(resumable.ErrMalformed, err)
			sh.mu.Unlock()
			return errMalformed
		}
		if err != nil {
			// The stream ended: closed by the relay, or for an offset
			// outside its buffer, which a Tunnel's client sees as a close.
			if errors.Is(err, resumable.ErrOffset) {
				return clientClosed
			}
			return nil
		}
		switch rec.Type {
		case wire.TerminalData:
			// A Host that has closed the channel fails the write; the
			// channel's end tells why.
			_, _ = sh.ch.Write(rec.Data)
		case wire.TerminalResize:
			_, _ = sh.ch.SendRequest("window-change", false, ssh.Marshal(struct {
				Columns, Rows, Width, Height uint32
			}{Columns: rec.Columns, Rows: rec.Rows}))
		}
	}
}

// countingReader counts the bytes read from r.
type countingReader struct {
	r io.Reader
	n *atomic.Int64
}

func (c countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n.Add(int64(n))
	return n, err
}
