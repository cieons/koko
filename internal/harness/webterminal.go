package harness

import (
	"context"
	"encoding/json/v2"
	"errors"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/cieons/koko/internal/server/web"
	"github.com/cieons/koko/internal/shared/resumable"
	"github.com/cieons/koko/internal/shared/wire"
)

// TerminalPage is a page of the web UI as it opens the web terminal's
// WebSocket (web.TerminalPath): it dials Addr with the Host, Origin and
// cookie a browser sends, and with X-Forwarded-For as a reverse proxy adds
// it, then speaks the terminal protocol, as the page's TypeScript end does,
// with the Go end of the resumable stream and the records on
// websocket.NetConn.
type TerminalPage struct {
	t testing.TB
	// Addr is the TCP address dialled: the web listener's, koko web's, or a
	// Forwarder's in front of either.
	Addr string
	// Host and Origin are the headers sent, and Cookie the cookie, if any;
	// ForwardedFor, if not empty, is sent as X-Forwarded-For.
	Host, Origin, ForwardedFor string
	Cookie                     *http.Cookie
	// Probe is how often a WebTerminal of the page sends an
	// acknowledgement-only frame, and its Workspace socket {}, as the page
	// does after 15 s without sending; 0 sends none.
	Probe time.Duration

	// ws is the page's Workspace socket, once Workspace opened it.
	ws *WorkspacePage
}

// TerminalPage returns the page of b, on its koko server's web listener as
// the reverse proxy passes it on: Host and Origin naming WebURL, and b's
// Sign-in cookie, if any, and address.
func (b *Browser) TerminalPage() *TerminalPage {
	p := &TerminalPage{
		t: b.t, Addr: b.h.web, Host: strings.TrimPrefix(WebURL, "https://"), Origin: WebURL,
		ForwardedFor: b.Address, Probe: 15 * time.Second,
	}
	if b.Cookie != "" {
		p.Cookie = &http.Cookie{Name: "__Host-koko", Value: b.Cookie}
	}
	return p
}

// KokoWebTerminalPage returns a page of the koko web listening on port,
// holding its cookie, as a browser opens koko web's address.
func KokoWebTerminalPage(t testing.TB, port int, cookie *http.Cookie) *TerminalPage {
	at := "localhost:" + strconv.Itoa(port)
	return &TerminalPage{t: t, Addr: "127.0.0.1:" + strconv.Itoa(port), Host: at, Origin: "http://" + at, Cookie: cookie, Probe: 15 * time.Second}
}

// Dial opens the terminal socket. When the upgrade is refused, it returns
// the answer, its body read, beside the error.
func (p *TerminalPage) Dial() (*websocket.Conn, Answer, error) {
	return p.dial(web.TerminalPath)
}

// dial opens the WebSocket at path.
func (p *TerminalPage) dial(path string) (*websocket.Conn, Answer, error) {
	h := http.Header{}
	h.Set("Host", p.Host)
	if p.Origin != "" {
		h.Set("Origin", p.Origin)
	}
	if p.ForwardedFor != "" {
		h.Set("X-Forwarded-For", p.ForwardedFor)
	}
	if p.Cookie != nil {
		h.Set("Cookie", p.Cookie.String())
	}
	tr := &http.Transport{}
	ctx, cancel := context.WithTimeout(p.t.Context(), 10*time.Second)
	defer cancel()
	c, resp, err := websocket.Dial(ctx, "ws://"+p.Addr+path, &websocket.DialOptions{
		HTTPClient: &http.Client{Transport: tr}, HTTPHeader: h, Host: p.Host,
	})
	if err != nil {
		defer tr.CloseIdleConnections()
		var a Answer
		if resp != nil {
			body, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			a = Answer{Response: resp, Body: string(body)}
		}
		return nil, a, err
	}
	p.t.Cleanup(func() { _ = c.CloseNow() })
	return c, Answer{Response: resp}, nil
}

// Exchange dials the terminal socket, sends first as the first message and
// reads its answer. For a refused message, it then reads the WebSocket's
// close, whose status it returns; -1 for an answer that is no refusal.
func (p *TerminalPage) Exchange(first wire.TerminalFirst) (*websocket.Conn, wire.TerminalAnswer, websocket.StatusCode) {
	p.t.Helper()
	c, _, err := p.Dial()
	if err != nil {
		p.t.Fatalf("open the web terminal's WebSocket: %v", err)
	}
	return p.exchangeOn(c, first)
}

// exchangeOn sends first on c, as Exchange does once it has dialled.
func (p *TerminalPage) exchangeOn(c *websocket.Conn, first wire.TerminalFirst) (*websocket.Conn, wire.TerminalAnswer, websocket.StatusCode) {
	p.t.Helper()
	body, err := json.Marshal(first)
	if err != nil {
		p.t.Fatal(err)
	}
	var a wire.TerminalAnswer
	status := p.send(c, body, &a)
	if a.Refused == "" {
		status = -1
	}
	return c, a, status
}

// send sends body as the first message on c, reads its answer into a, and
// for a refusal reads the close after it, whose status it returns.
func (p *TerminalPage) send(c *websocket.Conn, body []byte, a any) websocket.StatusCode {
	p.t.Helper()
	ctx, cancel := context.WithTimeout(p.t.Context(), 60*time.Second)
	defer cancel()
	if err := c.Write(ctx, websocket.MessageText, body); err != nil {
		p.t.Fatalf("send the first message: %v", err)
	}
	typ, msg, err := c.Read(ctx)
	if err != nil {
		p.t.Fatalf("read the answer to the first message: %v", err)
	}
	var refused struct {
		Refused string `json:"refused"`
	}
	if err := errors.Join(json.Unmarshal(msg, a), json.Unmarshal(msg, &refused)); typ != websocket.MessageText || err != nil {
		p.t.Fatalf("the answer to the first message is %s %q, want text of JSON (%v)", typ, msg, err)
	}
	if refused.Refused == "" {
		return -1
	}
	return CloseStatus(ctx, c)
}

// CloseStatus reads c until it closes, and returns the status the koko
// server closed it with, or -1 for a close without one.
func CloseStatus(ctx context.Context, c *websocket.Conn) websocket.StatusCode {
	for {
		if _, _, err := c.Read(ctx); err != nil {
			return websocket.CloseStatus(err)
		}
	}
}

// Open opens a tab to the Host named host in the page's Workspace, at cols
// by rows, waits for its SSH Session to run and attaches the page to it,
// failing the test unless it does.
func (p *TerminalPage) Open(host string, cols, rows uint32) *WebTerminal {
	p.t.Helper()
	w, a := p.TryOpen(host, cols, rows)
	if w == nil {
		p.t.Fatalf("opening a web terminal to %s was refused: %s: %s", host, a.Refused, deref(a.Message))
	}
	return w
}

// TryOpen opens a tab to the Host named host as Open does: the SSH Session
// once attached, or nil and why not: the open's refusal, the tab's own
// once its SSH Session did not start, or, for one that ended before it
// ran, its ending's reason as the code and its notice as the message.
func (p *TerminalPage) TryOpen(host string, cols, rows uint32) (*WebTerminal, wire.TerminalAnswer) {
	p.t.Helper()
	ws := p.Workspace()
	a := ws.Open(host, cols, rows)
	if a.Refused != "" {
		return nil, wire.TerminalRefusal(a.Refused, deref(a.Message))
	}
	tab := ws.WaitTab(a.Tab, "to start", func(t wire.WorkspaceTab) bool { return t.State != wire.TabStateStarting })
	switch {
	case tab.Refused != nil:
		return nil, wire.TerminalRefusal(tab.Refused.Code, tab.Refused.Message)
	case tab.Ended != nil:
		return nil, wire.TerminalRefusal(tab.Ended.Reason, tab.Ended.Notice)
	}
	w := p.Attach(tab.Session, cols, rows)
	w.Tab = tab.ID
	return w, wire.TerminalAnswer{}
}

// Attach attaches the page to the SSH Session id with its box of cols by
// rows, failing the test unless it is answered with the attachment's id.
func (p *TerminalPage) Attach(id string, cols, rows uint32) *WebTerminal {
	p.t.Helper()
	w, a := p.TryAttach(id, cols, rows)
	if w == nil {
		p.t.Fatalf("attaching to the SSH Session %s was refused: %s: %s", id, a.Refused, deref(a.Message))
	}
	return w
}

// TryAttach attaches the page to the SSH Session id: the attachment once
// answered with its id, or nil and the refusal, after the WebSocket closed
// with status 1008.
func (p *TerminalPage) TryAttach(id string, cols, rows uint32) (*WebTerminal, wire.TerminalAnswer) {
	p.t.Helper()
	c, a, status := p.Exchange(wire.TerminalFirst{Protocol: wire.TerminalProtocol, Attach: id, Cols: cols, Rows: rows})
	if a.Refused != "" {
		if status != websocket.StatusPolicyViolation {
			p.t.Errorf("the refused attach closed with status %d, want %d", status, websocket.StatusPolicyViolation)
		}
		return nil, a
	}
	if len(a.Attachment) != 32 {
		p.t.Fatalf("the attach was answered %+v, want the attachment's id", a)
	}
	w := &WebTerminal{t: p.t, page: p, ID: id, Attachment: a.Attachment, ended: make(chan struct{}), changed: make(chan struct{})}
	w.stream = resumable.New(websocket.NetConn(context.Background(), c, websocket.MessageBinary))
	w.c = c
	go w.read()
	w.probe()
	p.t.Cleanup(func() { _ = w.stream.Close() })
	return w, a
}

// Resume resumes w's attachment on a new WebSocket of the page, which may
// be another page than the one that made it, and returns the answer: once
// accepted, the stream carries on there; a refusal leaves it Detached.
// Like the page, it tries again after a WebSocket that fails to open
// (redial).
func (p *TerminalPage) Resume(w *WebTerminal) wire.TerminalAnswer {
	p.t.Helper()
	received := w.stream.Detach()
	c, a, _ := p.exchangeOn(p.redial(), wire.TerminalFirst{Protocol: wire.TerminalProtocol, Resume: w.Attachment, Offset: received})
	if a.Refused != "" {
		return a
	}
	if a.Offset == nil {
		p.t.Fatalf("the resume was answered %+v, want the koko server's offset", a)
	}
	if err := w.stream.ResumeAnswered(websocket.NetConn(context.Background(), c, websocket.MessageBinary), received, *a.Offset); err != nil {
		p.t.Fatalf("resume the stream: %v", err)
	}
	w.mu.Lock()
	w.c = c
	w.mu.Unlock()
	return a
}

// redial opens a WebSocket to Resume on as the page does: after one that
// fails to open, such as one koko web closed when the Device Mux could not
// carry its channel across a drop, it tries again after 250 ms, doubling
// the wait up to 5 s (timing.reconnectFirst and timing.reconnectMax in
// web/src/terminal/connection.ts). It fails the test after 20 s.
func (p *TerminalPage) redial() *websocket.Conn {
	p.t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	var wait time.Duration
	for {
		c, _, err := p.Dial()
		if err == nil {
			return c
		}
		if time.Now().After(deadline) {
			p.t.Fatalf("open the web terminal's WebSocket to Resume for 20 s: %v", err)
		}
		wait = min(max(2*wait, 250*time.Millisecond), 5*time.Second)
		time.Sleep(wait)
	}
}

// WebTerminal is a page's attachment to a web terminal's SSH Session, as
// the page holds it: the resumable stream on the page's terminal socket,
// the records the koko server sent and how it ended.
type WebTerminal struct {
	t    testing.TB
	page *TerminalPage
	// ID is the SSH Session's id, Attachment the attachment's, and Tab, for
	// one Open made, the tab's.
	ID, Attachment, Tab string
	stream              *resumable.Stream

	mu sync.Mutex
	c  *websocket.Conn
	// output is the data the koko server sent since its last reset, the
	// snapshot included; live is what came after the snapshot, and
	// snapshot the snapshot itself. resets are the sizes the resets
	// carried. changed is closed and replaced at each record.
	output, live, snapshot []byte
	inSnapshot             bool
	resets                 [][2]uint32
	// active is what the last active record said; displaced, whether a
	// displaced record came. held, while not nil, holds the reading back
	// until it is closed.
	active, displaced bool
	held              chan struct{}
	changed           chan struct{}
	// status and signal are the exit records, if any; err is why the
	// stream ended, once ended is closed.
	status *uint32
	signal string
	err    error
	ended  chan struct{}
}

// read reads the koko server's records until the stream ends.
func (w *WebTerminal) read() {
	r := wire.NewTerminalReader(w.stream)
	for {
		w.mu.Lock()
		held := w.held
		w.mu.Unlock()
		if held != nil {
			<-held
		}
		rec, err := r.Next()
		w.mu.Lock()
		if err != nil {
			if errors.Is(err, io.EOF) {
				err = w.stream.Err()
			}
			w.err = err
			close(w.ended)
			close(w.changed)
			w.mu.Unlock()
			return
		}
		switch rec.Type {
		case wire.TerminalData:
			w.output = append(w.output, rec.Data...)
			if w.inSnapshot {
				w.snapshot = append(w.snapshot, rec.Data...)
			} else {
				w.live = append(w.live, rec.Data...)
			}
		case wire.TerminalReset:
			w.resets = append(w.resets, [2]uint32{rec.Columns, rec.Rows})
			w.output, w.snapshot, w.live, w.inSnapshot = nil, nil, nil, true
		case wire.TerminalActive:
			// The last record of a snapshot, or a change.
			w.active, w.inSnapshot = rec.Activity == wire.TerminalNowActive, false
		case wire.TerminalDisplaced:
			w.displaced = true
		case wire.TerminalExitStatus:
			w.status = new(rec.Status)
		case wire.TerminalExitSignal:
			w.signal = rec.Signal
		}
		close(w.changed)
		w.changed = make(chan struct{})
		w.mu.Unlock()
	}
}

// probe sends an acknowledgement-only frame each time the page has sent
// nothing for its Probe, as the page does, until the stream ends.
func (w *WebTerminal) probe() {
	if w.page.Probe == 0 {
		return
	}
	go func() {
		t := time.NewTicker(w.page.Probe)
		defer t.Stop()
		for {
			select {
			case <-w.ended:
				return
			case <-t.C:
				w.stream.Probe()
			}
		}
	}()
}

// Type sends data as data records, as keys typed or pasted.
func (w *WebTerminal) Type(data []byte) {
	w.t.Helper()
	for len(data) > 0 {
		n := min(len(data), wire.TerminalMaxRecord)
		w.send(wire.TerminalRecord{Type: wire.TerminalData, Data: data[:n]})
		data = data[n:]
	}
}

// Write sends p as data records, as Type does, and returns the error
// instead of failing the test, for a goroutine of the test's to write while
// the test reads.
func (w *WebTerminal) Write(p []byte) (int, error) {
	for sent := 0; sent < len(p); {
		n := min(len(p)-sent, wire.TerminalMaxRecord)
		rec, err := wire.AppendTerminalRecord(nil, wire.TerminalRecord{Type: wire.TerminalData, Data: p[sent : sent+n]})
		if err != nil {
			return sent, err
		}
		if _, err := w.stream.Write(rec); err != nil {
			return sent, err
		}
		sent += n
	}
	return len(p), nil
}

// Resize sends a resize record.
func (w *WebTerminal) Resize(cols, rows uint32) {
	w.t.Helper()
	w.send(wire.TerminalRecord{Type: wire.TerminalResize, Columns: cols, Rows: rows})
}

// Send writes raw bytes into the stream, such as a malformed record.
func (w *WebTerminal) Send(b []byte) {
	w.t.Helper()
	if _, err := w.stream.Write(b); err != nil {
		w.t.Fatalf("write into the web terminal's stream: %v", err)
	}
}

func (w *WebTerminal) send(rec wire.TerminalRecord) {
	w.t.Helper()
	b, err := wire.AppendTerminalRecord(nil, rec)
	if err != nil {
		w.t.Fatal(err)
	}
	w.Send(b)
}

// Output returns the data the koko server has sent since its last reset,
// the snapshot that followed it included: what the page's emulator holds.
func (w *WebTerminal) Output() []byte {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.output
}

// Live returns the data the koko server has sent since its last snapshot.
func (w *WebTerminal) Live() []byte {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.live
}

// Snapshot returns the last snapshot the koko server sent.
func (w *WebTerminal) Snapshot() []byte {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.snapshot
}

// Resets returns the sizes of the resets the koko server has sent.
func (w *WebTerminal) Resets() [][2]uint32 {
	w.mu.Lock()
	defer w.mu.Unlock()
	return slices.Clone(w.resets)
}

// WaitResets waits until the koko server has sent n resets, the last one
// complete with its snapshot, and returns their sizes.
func (w *WebTerminal) WaitResets(n int) [][2]uint32 {
	w.t.Helper()
	w.waitFor(strconv.Itoa(n)+" resets", func() bool { return len(w.resets) >= n && !w.inSnapshot })
	return w.Resets()
}

// Active tells whether the koko server's last active record said the page
// is the active page.
func (w *WebTerminal) Active() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.active
}

// WaitActive waits until the koko server says the page is the active page,
// or, for false, that it is not.
func (w *WebTerminal) WaitActive(active bool) {
	w.t.Helper()
	w.waitFor("an active record of "+strconv.FormatBool(active), func() bool { return w.active == active && !w.inSnapshot && len(w.resets) > 0 })
}

// Displaced tells whether a displaced record came.
func (w *WebTerminal) Displaced() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.displaced
}

// StopReading stops the page reading its stream, as a page in a stalled
// tab does: it acknowledges nothing more, so the koko server's buffer of
// it fills.
func (w *WebTerminal) StopReading() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.held == nil {
		w.held = make(chan struct{})
	}
}

// ReadAgain has the page read its stream again.
func (w *WebTerminal) ReadAgain() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.held != nil {
		close(w.held)
		w.held = nil
	}
}

// WaitStreamEnd waits until the page's stream has ended, whether or not
// the page has read up to the end, as when the koko server closed it while
// the page read nothing, and returns why. It fails the test after 20 s.
func (w *WebTerminal) WaitStreamEnd() error {
	w.t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for w.stream.Err() == nil {
		if time.Now().After(deadline) {
			w.t.Fatal("the web terminal's stream did not end within 20 s")
		}
		time.Sleep(10 * time.Millisecond)
	}
	return w.stream.Err()
}

// Act sends an active record: a person acted in the page.
func (w *WebTerminal) Act() {
	w.t.Helper()
	w.send(wire.TerminalRecord{Type: wire.TerminalActive, Activity: wire.TerminalActed})
}

// Colors sends the page's theme: its foreground and background.
func (w *WebTerminal) Colors(fg, bg [3]byte) {
	w.t.Helper()
	w.send(wire.TerminalRecord{Type: wire.TerminalColors, Foreground: fg, Background: bg})
}

// WaitOutput waits until the data the koko server sent since its last
// reset holds want, and returns it all. It fails the test after 20 s.
func (w *WebTerminal) WaitOutput(want string) []byte {
	w.t.Helper()
	return w.waitFor("output holding "+strconv.Quote(want), func() bool { return strings.Contains(string(w.output), want) })
}

// WaitOutputLen waits until the koko server has sent n bytes of data.
func (w *WebTerminal) WaitOutputLen(n int) []byte {
	w.t.Helper()
	return w.waitFor(strconv.Itoa(n)+" bytes of output", func() bool { return len(w.output) >= n })
}

func (w *WebTerminal) waitFor(what string, cond func() bool) []byte {
	w.t.Helper()
	deadline := time.NewTimer(20 * time.Second)
	defer deadline.Stop()
	for {
		w.mu.Lock()
		ok, changed, out := cond(), w.changed, w.output
		w.mu.Unlock()
		if ok {
			return out
		}
		select {
		case <-changed:
			select {
			case <-w.ended:
				w.mu.Lock()
				ok = cond()
				w.mu.Unlock()
				if !ok {
					w.t.Fatalf("the web terminal ended (%v) before %s; it got %q", w.Err(), what, tail(w.Output()))
				}
			default:
			}
		case <-deadline.C:
			w.t.Fatalf("the web terminal got no %s within 20 s; it got %q", what, tail(w.Output()))
		}
	}
}

// tail is the end of out, for a message.
func tail(out []byte) []byte {
	return out[max(0, len(out)-200):]
}

// Ended is closed once the stream has ended.
func (w *WebTerminal) Ended() <-chan struct{} {
	return w.ended
}

// WaitEnd waits for the stream to end, and returns why: the
// *resumable.PeerClosedError of the koko server's close frame, with its
// reason. It fails the test after 20 s.
func (w *WebTerminal) WaitEnd() error {
	w.t.Helper()
	select {
	case <-w.ended:
	case <-time.After(20 * time.Second):
		w.t.Fatalf("the web terminal did not end within 20 s; it got %q", tail(w.Output()))
	}
	return w.Err()
}

// Err is why the stream ended, or nil while it lives.
func (w *WebTerminal) Err() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.err
}

// Exit returns the exit status and signal records the koko server sent.
func (w *WebTerminal) Exit() (*uint32, string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.status, w.signal
}

// Close closes the stream from the page, with its close frame, then the
// WebSocket, as the page does when it leaves: the page detaches.
func (w *WebTerminal) Close() {
	_ = w.stream.Close()
}

// WaitDropped waits until the page's WebSocket has closed beneath the
// stream, as when the connection under it drops, and fails the test after
// 20 s.
func (w *WebTerminal) WaitDropped() {
	w.t.Helper()
	select {
	case <-w.stream.Detached():
	case <-time.After(20 * time.Second):
		w.t.Fatal("the web terminal's WebSocket did not close within 20 s")
	}
}

// Drop cuts the page's WebSocket without a close, as a network drop does:
// the stream is Detached.
func (w *WebTerminal) Drop() {
	w.mu.Lock()
	c := w.c
	w.mu.Unlock()
	_ = c.CloseNow()
	<-w.stream.Detached()
}

func deref(s *string) string {
	if s == nil {
		return "<none>"
	}
	return *s
}
