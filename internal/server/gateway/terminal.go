package gateway

import (
	"context"
	"crypto/rand"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"maps"
	"net"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/cieons/koko/internal/server/host"
	"github.com/cieons/koko/internal/server/hostconn"
	"github.com/cieons/koko/internal/server/op"
	"github.com/cieons/koko/internal/shared/liveness"
	"github.com/cieons/koko/internal/shared/resumable"
	"github.com/cieons/koko/internal/shared/wire"
)

// A web terminal's SSH Session (P1 spec: a terminal in the web UI, "The
// Gateway's terminal SSH Session"; ADR 0053, refined by ADR 0057) belongs
// to its User's Workspace (workspace.go), which opens it in a tab: the
// Gateway admits it as it admits a Tunnel's, dials the Host, opens one
// session channel there with a pty and a shell, and holds it, apart from
// any page, until it ends (session.go). It is a tunnel in the registry,
// of the client clientBrowser, so Close, Withdraw, recheck and keepEnding
// serve it as they serve a Tunnel's; it carries no stream of its own and
// counts against no caller's limit, the Workspace's 16 tabs bounding it.
// A page attaches to it on a terminal socket the web front hands over
// (AttachTerminal), and gets a resumable stream of its own carrying
// terminal records both ways (wire.TerminalRecord), which the page resumes
// within timing.Grace of a drop (ResumeTerminal).

// TerminalCaller is who a web terminal's page is, as the web front found
// them.
type TerminalCaller struct {
	// Caller is the live Sign-in the cookie names on the web listener; on
	// web@koko, the Device Session or the Local caller koko web runs as.
	Caller op.Caller
	// Run, for the Local caller, names the run of koko web the page came
	// through, which alone may resume its attachments, and RunEnded is
	// closed once that run ends, which detaches its pages; the SSH
	// Sessions it opened go on.
	Run      string
	RunEnded <-chan struct{}
	// Addr is where the page came from: on the web listener, the client's
	// address the listener counts; through koko web, its connection's.
	Addr net.Addr
	// WebURL, on the web listener, is the Web URL the web front checked the
	// page's Host and Origin against; a Web URL changed since refuses an
	// open, as the change ends the terminals already open. It is empty
	// through koko web.
	WebURL string
}

// TerminalSocket is a web terminal's terminal socket, once the web front
// has read its first message: the Gateway answers that message on it,
// then runs the attachment's resumable stream on its binary messages.
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

// key is whose the caller c's page is, as an attachment's resume must show.
func (c TerminalCaller) key() callerKey { return callerKey{id: c.Caller.ID(), run: c.Run} }

// terminalsOpen tells whether the Gateway takes new web terminals' pages:
// not once CloseTerminals has ended them.
func (g *Gateway) terminalsOpen() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return !g.closed && !g.terminalsClosed
}

// startSession starts the SSH Session s on the shell sh, and says whether
// it did. A start that failed as the SSH Session ended meanwhile, such as
// one whose emulator waited for the engine's compile, records the ending
// in its tab, as an SSH Session that ended before its shell started does;
// any other failure refuses its tab.
func (g *Gateway) startSession(s *termSession, sh hostShell, hostName string) bool {
	err := s.start(sh)
	switch {
	case err == nil:
		return true
	case s.t.ctx.Err() != nil:
		g.endedBeforeStart(s, hostName)
	default:
		t := s.t
		g.log.Error("open a web terminal", slices.Concat(s.named(), []any{"name", hostName}, callerAttrs(t.caller), []any{"addr", t.from, "err", err})...)
		s.w.refused(s, openFailed)
	}
	return false
}

// endedBeforeStart records the ending of the SSH Session s, which ended
// while it opened: its tab closed, the koko server stops, or its access
// was withdrawn, which its tab tells.
func (g *Gateway) endedBeforeStart(s *termSession, hostName string) {
	t := s.t
	e := endingOf(context.Cause(t.ctx))
	g.log.Debug("ssh session ended before it started", slices.Concat(s.named(), []any{"name", hostName}, callerAttrs(t.caller), []any{"addr", t.from, "err", e})...)
	s.w.ended(s, e, nil, "")
}

// runSession runs the SSH Session s of a Workspace, registered, from its
// dial until it ends: it starts the shell at the pty's size, runs the
// loop, and once the SSH Session ends, finishes its pages with how it
// ended, closes the Host connection and records the ending in its tab.
// None of that waits for the loop, which may be inside a call into the
// emulator, or for a page's writer.
func (g *Gateway) runSession(s *termSession) {
	t := s.t
	defer g.unregister(t)
	defer t.end(errEnded)
	hostName := s.a.host.Name
	sh, refused, err := g.startShell(t, s.a, uint32(s.cols), uint32(s.rows))
	if sh != nil {
		defer sh.close()
	}
	switch {
	case t.ctx.Err() != nil:
		g.endedBeforeStart(s, hostName)
		return
	case refused != nil:
		g.logRefused(t, hostName, refused)
		s.w.refused(s, refused)
		return
	case err != nil:
		g.log.Error("open a web terminal", slices.Concat(s.named(), []any{"name", hostName}, callerAttrs(t.caller), []any{"addr", t.from, "err", err})...)
		s.w.refused(s, openFailed)
		return
	}
	// Before the tab runs, which gives the pages the id its files calls
	// name. The files end before the Host connection closes.
	sh.files = g.newTerminalFiles(sh.host)
	defer sh.files.end()
	g.mu.Lock()
	t.files = sh.files
	g.mu.Unlock()
	if !g.startSession(s, sh, hostName) {
		return
	}

	started := time.Now()
	g.log.Info("ssh session started", slices.Concat(startedAttrs(t, s.a), []any{"tab", s.tab})...)
	g.log.Info("ssh session request", "tunnel", t.id, "client", t.client, "host", hostName, "request", "shell")
	s.w.running(s)
	end := s.awaitEnd()
	// The files end while the Host connection lives, so an upload's partial
	// file goes; closing it then ends the reads of the channel and its
	// requests.
	sh.files.end()
	sh.close()
	status, signal := sh.exitState()
	s.w.ended(s, end, status, signal)
	g.log.Info("ssh session ended", slices.Concat(sessionAttrs(t, s.a), []any{
		"tab", s.tab,
		"duration", time.Since(started).Round(time.Millisecond),
		"bytes_from_device", s.read.Load(), "bytes_to_device", s.written.Load(),
	}, sh.exit(), end.attrs, []any{"reason", end.reason})...)
}

// AttachTerminal attaches the page of the caller c, on the terminal socket
// s, to the SSH Session id of the caller's User, with the page's box of
// cols by rows, in a goroutine of its own. While the Gateway stops, it
// cuts s instead.
func (g *Gateway) AttachTerminal(c TerminalCaller, s TerminalSocket, id [wire.TunnelIDSize]byte, cols, rows uint32) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed || g.terminalsClosed {
		s.Cut()
		return
	}
	g.wg.Go(func() { g.attachTerminal(c, s, tunnelID(id), int(cols), int(rows)) })
}

// attachTerminal attaches a page to the SSH Session id: any live caller of
// its User may. It answers with the attachment's id, starts its stream on
// the socket, and hands it to the loop, which sends it a reset and the
// snapshot, then the live output. An id that names no live SSH Session of
// the User is refused with unknown_tunnel, one that ended with
// ssh_session_ended. The attachment detaches as its caller ends.
func (g *Gateway) attachTerminal(c TerminalCaller, sock TerminalSocket, id tunnelID, cols, rows int) {
	seen := g.withdrawalsTaken()
	u, refused := g.userOf(c)
	if refused != nil {
		sock.Refuse(*refused)
		return
	}
	s, ended := g.workspaces.workspace(u.ID).sessionOf(id)
	if s == nil {
		code, message := wire.TerminalUnknownTunnel, "this User has no open SSH Session with this id"
		if ended {
			code, message = wire.TerminalSSHSessionEnded, "this SSH Session has ended"
		}
		g.log.Debug("ssh session attach refused", slices.Concat([]any{"tunnel", id, "client", clientBrowser}, callerAttrs(c.Caller), []any{"addr", c.Addr, "reason", code})...)
		sock.Refuse(wire.TerminalRefusal(code, message))
		return
	}
	a := &attachment{
		s: s, caller: c.Caller, key: c.key(), from: c.Addr,
		cols: cols, rows: rows, socket: sock,
		written: make(chan struct{}), wake: make(chan struct{}, 1),
	}
	_, _ = rand.Read(a.id[:]) // never fails
	a.ctx, a.cancel = context.WithCancelCause(s.t.ctx)
	if err := sock.Answer(wire.TerminalAnswer{Attachment: a.id.String()}); err != nil {
		a.cancel(err)
		g.log.Debug("ssh session attach failed", append(a.attrs(), "err", err)...)
		return
	}
	sc := newSocketConn(sock.Conn())
	callerEnded := func() string { return g.endedSince(c, seen) }
	if g.addAttachment(a, sc, func() { g.watchSocket(a.stream, sock, sc, a.attrs()) }, callerEnded) {
		g.whenRunEnds(c, a.ctx.Done(), func() { a.detach("koko web ended") })
	}
}

// addAttachment starts the attachment a's stream on conn, its writer and
// watch, and watchConn, unless the SSH Session has ended or its caller
// has, and hands a to the loop; only then does it read the page's records,
// so that none comes before it. It says whether it did. ended, unless nil,
// tells why the caller has ended since it was read, once a is in the
// Gateway's attachments, where a change from then on finds it to detach:
// a caller that has ended gets no snapshot and sends nothing to the shell.
func (g *Gateway) addAttachment(a *attachment, conn net.Conn, watchConn func(), ended func() string) bool {
	s := a.s
	a.stream = resumable.New(conn)
	// In the Gateway's first, so that the SSH Session's ending, which takes
	// it out of both, finds it in the Gateway's once it is in its own.
	g.mu.Lock()
	g.attachments[a.id] = a
	g.mu.Unlock()
	if ended != nil {
		if reason := ended(); reason != "" {
			a.cancel(errEnded)
			g.forgetAttachment(a, nil)
			a.unstarted()
			a.cut()
			_ = a.stream.Close()
			g.log.Debug("ssh session attach refused", append(a.attrs(), "reason", reason)...)
			return false
		}
	}
	s.mu.Lock()
	if s.ended {
		s.mu.Unlock()
		a.cancel(errEnded)
		g.forgetAttachment(a, nil)
		a.unstarted()
		_ = a.stream.CloseWithReason(endingOf(context.Cause(s.t.ctx)).notice)
		return false
	}
	s.all[a] = struct{}{}
	s.mu.Unlock()
	g.wg.Go(a.run)
	a.watch()
	g.wg.Go(watchConn)
	select {
	case s.attaches <- a:
		g.wg.Go(a.read)
	case <-a.ctx.Done():
	}
	return true
}

// unstarted closes written for the attachment a, which addAttachment gave
// up before its writer ran: a stop that found a among the Gateway's
// attachments waits for written, which only the writer closes otherwise.
func (a *attachment) unstarted() { close(a.written) }

// forgetAttachment takes the ended attachment a out of the Gateway's. When
// its SSH Session ended with end, which has a notice, it keeps the ending
// for the grace, so that a resume of a then is refused saying why.
func (g *Gateway) forgetAttachment(a *attachment, end *ending) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.attachments[a.id] == a {
		delete(g.attachments, a.id)
	}
	if end != nil && end.why != "" {
		g.ended[a.id] = endedTunnel{key: a.key, client: clientBrowser, ending: end, until: time.Now().Add(g.timings.Grace)}
	}
}

// ResumeTerminal resumes the attachment id on the terminal socket s, from
// offset, the page's received offset, in a goroutine of its own: it is
// taken only from the caller c who made it, the same Sign-in, Device
// Session or run of koko web, while it lives, within timing.Grace of its
// stream's drop, and only once its SSH Session's access holds still
// (recheckResume). The Gateway answers with its own received offset and
// moves the stream onto s, off the WebSocket it was on, if any: the last
// attach wins. Any other resume is refused. While the Gateway stops, it
// cuts s instead.
func (g *Gateway) ResumeTerminal(c TerminalCaller, s TerminalSocket, id [wire.TunnelIDSize]byte, offset uint64) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed || g.terminalsClosed {
		s.Cut()
		return
	}
	g.wg.Go(func() { g.resumeTerminal(c, s, tunnelID(id), offset) })
}

func (g *Gateway) resumeTerminal(c TerminalCaller, sock TerminalSocket, id tunnelID, offset uint64) {
	seen := g.withdrawalsTaken()
	if _, refused := g.userOf(c); refused != nil {
		sock.Refuse(*refused)
		return
	}
	key := c.key()
	g.mu.Lock()
	a := g.attachments[id]
	if a != nil && (a.key != key || a.ctx.Err() != nil) {
		a = nil
	}
	var ended *ending
	if e, ok := g.ended[id]; a == nil && ok && e.key == key && e.client == clientBrowser && time.Now().Before(e.until) {
		ended = e.ending
	}
	g.mu.Unlock()
	if a != nil {
		ended = g.recheckResume(a.s.t)
	}
	if reason := ""; a != nil && ended == nil {
		// Its caller, read above, may have ended since, by a change that
		// detached its attachments before this one was found.
		if reason = g.endedSince(c, seen); reason != "" {
			a.detach(reason)
			ended = &ending{reason: reason}
		}
	}
	if a == nil || ended != nil {
		reason, why := "unknown attachment", ""
		attrs := slices.Concat([]any{"client", clientBrowser, "attachment", id}, callerAttrs(c.Caller), []any{"addr", c.Addr})
		if ended != nil {
			reason, why = ended.reason, ended.why
		}
		g.log.Info("ssh session resume refused", append(attrs, "reason", reason)...)
		sock.Refuse(wire.TerminalRefusal(wire.TerminalResumeRefused, why))
		return
	}
	attrs := slices.Concat(a.s.named(), []any{"attachment", a.id}, callerAttrs(c.Caller), []any{"addr", c.Addr})
	sc := newSocketConn(sock.Conn())
	a.mu.Lock()
	a.resuming++
	a.mu.Unlock()
	err := a.stream.AcceptResumeAnswering(sc, offset, func(own uint64) error {
		return sock.Answer(wire.TerminalAnswer{Offset: &own})
	})
	a.mu.Lock()
	a.resuming--
	if err != nil {
		a.holdLocked()
	} else {
		a.stopGraceLocked()
		a.socket = sock
	}
	a.mu.Unlock()
	if err != nil {
		g.log.Info("ssh session resume failed", append(attrs, "err", err)...)
		return
	}
	g.log.Info("ssh session resumed", attrs...)
	a.watch()
	g.wg.Go(func() { g.watchSocket(a.stream, sock, sc, attrs) })
}

// watch watches the WebSocket the attachment's stream is on now for its
// end, unless it is watched already: the attachment is then held for
// timing.Grace, for the page to resume it.
func (a *attachment) watch() {
	a.mu.Lock()
	defer a.mu.Unlock()
	attached := a.stream.Detached()
	if attached == a.watched {
		return
	}
	a.watched = attached
	a.s.g.wg.Go(func() {
		select {
		case <-attached:
		case <-a.ctx.Done():
			return
		}
		a.mu.Lock()
		defer a.mu.Unlock()
		a.holdLocked()
	})
}

// holdLocked starts the attachment's grace, which ends it with "page left"
// unless a resume comes first. It does nothing while the stream is on a
// WebSocket or a resume moves it onto one, once the attachment has ended,
// or while its grace runs already. a.mu is held.
func (a *attachment) holdLocked() {
	if a.resuming > 0 || a.grace != nil || a.ctx.Err() != nil || a.stream.Err() != nil {
		return
	}
	select {
	case <-a.stream.Detached():
	default:
		return
	}
	a.s.g.log.Debug("web terminal dropped", a.attrs()...)
	a.grace = time.AfterFunc(a.s.g.timings.Grace, func() { a.end("page left", nil, "") })
}

// stopGraceLocked stops the attachment's grace, if it runs. a.mu is held.
func (a *attachment) stopGraceLocked() {
	if a.grace != nil {
		a.grace.Stop()
		a.grace = nil
	}
}

// cut closes the WebSocket the attachment was last on at once.
func (a *attachment) cut() {
	a.mu.Lock()
	s := a.socket
	a.mu.Unlock()
	if s != nil {
		s.Cut()
	}
}

// socketConn is a web terminal's WebSocket as a stream runs on it: it
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

// watchSocket watches the WebSocket s, sc beneath stream, an attachment's,
// until the stream comes off it. A page can neither send nor see a
// WebSocket's pings, so liveness is in the stream: after
// timing.ServerProbe without sending, the Gateway asks the stream for an
// acknowledgement-only frame (resumable.Stream.Probe), which also keeps a
// proxy from cutting a quiet terminal; after timing.ServerDead without a
// byte from the page, it cuts the WebSocket, which starts the
// attachment's grace. The page does the same with the same values. attrs
// name the attachment in the log.
func (g *Gateway) watchSocket(stream *resumable.Stream, s TerminalSocket, sc *socketConn, attrs []any) {
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
			g.log.Debug("web terminal found dead", append(attrs, "quiet", quiet.Round(time.Millisecond))...)
			s.Cut()
			return
		}
		next := probe - sc.silent()
		if next <= 0 {
			stream.Probe()
			next = probe
		}
		timer.Reset(min(next, dead-quiet))
	}
}

// CloseTerminals ends every web terminal's SSH Session, telling each page
// that the koko server is restarting, sends stopping on every Workspace
// socket, and refuses new pages. server.Close calls it before the web
// listener closes, whose shutdown does not track a WebSocket. It waits for
// each SSH Session to end and each page to have its notice, and once
// timing.StopNotice has passed it cuts the WebSockets of those still at
// it, which a page too stalled to take the notice holds. Once a further
// StopNotice has passed after the cuts it gives up, logging each wait
// still outstanding, so that one whose owner was lost holds up no stop.
// The tabs keep no ending, so they come back restarted.
func (g *Gateway) CloseTerminals() {
	g.mu.Lock()
	g.terminalsClosed = true
	var waits []closeWait
	for _, t := range g.tunnels {
		if t.client == clientBrowser {
			t.end(errStopping)
			waits = append(waits, closeWait{t.done, append([]any{"waiting_for", "ssh session"}, t.named()...)})
		}
	}
	atts := slices.Collect(maps.Values(g.attachments))
	g.mu.Unlock()
	for _, a := range atts {
		waits = append(waits, closeWait{a.written, append([]any{"waiting_for", "attachment"}, a.attrs()...)})
	}
	stopping, _ := json.Marshal(wire.WorkspaceStopping{Stopping: "the koko server is restarting"}) // plain
	var pages []*wsPage
	for _, w := range g.workspaces.all() {
		w.mu.Lock()
		for p := range w.pages {
			p.stop(stopping)
			pages = append(pages, p)
			waits = append(waits, closeWait{p.done, slices.Concat([]any{"waiting_for", "workspace socket", "user", w.userID}, callerAttrs(p.c.Caller), []any{"addr", p.c.Addr})})
		}
		w.mu.Unlock()
	}
	late := time.NewTimer(g.timings.StopNotice)
	defer late.Stop()
	if waits = awaitAll(waits, late.C); len(waits) == 0 {
		return
	}
	for _, a := range atts {
		a.cut()
	}
	for _, p := range pages {
		p.s.Cut()
	}
	late.Reset(g.timings.StopNotice)
	for _, w := range awaitAll(waits, late.C) {
		g.log.Error("web terminals' stop gave up waiting", w.attrs...)
	}
}

// closeWait is one wait of CloseTerminals: done is closed once what attrs
// name in the log has ended.
type closeWait struct {
	done  <-chan struct{}
	attrs []any
}

// awaitAll waits for each of waits until expired fires, and returns those
// still outstanding then.
func awaitAll(waits []closeWait, expired <-chan time.Time) []closeWait {
	for i, w := range waits {
		select {
		case <-w.done:
		case <-expired:
			return slices.DeleteFunc(waits[i:], func(w closeWait) bool {
				select {
				case <-w.done:
					return true
				default:
					return false
				}
			})
		}
	}
	return nil
}

// startShell dials the Host a admitted for the web terminal t, through its
// chain of Relays, and starts a shell there on one session channel with a pty
// of cols columns and rows rows, asking for TERM xterm-256color and IUTF8,
// so that a shell's line editor erases a whole Chinese character. It returns
// the refusal of a Host the connector could not log in to, or that refused
// the session, its pty or its shell, and fails on t's end.
func (g *Gateway) startShell(t *tunnel, a admission, cols, rows uint32) (*terminalShell, *refusal, error) {
	conn, err := g.connector.Connect(t.ctx, kokoTarget(a.host, a.kokoKey), kokoTargets(a.host.Relays, a.kokoKey))
	if err != nil {
		return nil, connectRefusal(a.host, err), nil
	}
	sh := &terminalShell{host: conn, hostName: a.host.Name, relays: a.host.Relays}
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
// on the Gateway's connection to the Host named hostName, through its chain
// relays, as an SSH Session's loop uses it (hostShell).
type terminalShell struct {
	host     *hostconn.Conn
	hostName string
	relays   []*host.Relay
	ch       ssh.Channel
	reqs     <-chan *ssh.Request
	// files are the SSH Session's files on the Host (files.go), which end
	// before the Host connection closes; nil before the shell started.
	files *terminalFiles

	mu sync.Mutex
	// exitStatus and exitSignal are what the Host sent to say how the shell
	// ended.
	exitStatus *uint32
	exitSignal string

	closeOnce sync.Once
}

var _ hostShell = (*terminalShell)(nil)

func (sh *terminalShell) outputs() []io.Reader { return []io.Reader{sh.ch, sh.ch.Stderr()} }

func (sh *terminalShell) Write(p []byte) (int, error) { return sh.ch.Write(p) }

// windowChange tells the Host the pty's size, its pixel sizes 0.
func (sh *terminalShell) windowChange(cols, rows int) {
	_, _ = sh.ch.SendRequest("window-change", false, ssh.Marshal(struct {
		Columns, Rows, Width, Height uint32
	}{Columns: uint32(cols), Rows: uint32(rows)}))
}

func (sh *terminalShell) requests() { sh.hostRequests() }

// ending is why the SSH Session ends once the channel's output and
// requests have: a channel the Host's connection took down sees it
// broken.
func (sh *terminalShell) ending() *ending {
	if sh.host.Broken() != nil {
		return hostEnding(sh.host, sh.hostName, sh.relays)
	}
	return errShellEnded
}

func (sh *terminalShell) exitState() (*uint32, string) {
	sh.mu.Lock()
	defer sh.mu.Unlock()
	return sh.exitStatus, sh.exitSignal
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
