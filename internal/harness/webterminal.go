package harness

import (
	"context"
	"encoding/json/v2"
	"errors"
	"io"
	"net/http"
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
	// acknowledgement-only frame, as the page does after 15 s without
	// sending; 0 sends none.
	Probe time.Duration
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

// Dial opens the WebSocket. When the upgrade is refused, it returns the
// answer, its body read, beside the error.
func (p *TerminalPage) Dial() (*websocket.Conn, Answer, error) {
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
	c, resp, err := websocket.Dial(ctx, "ws://"+p.Addr+web.TerminalPath, &websocket.DialOptions{
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

// Exchange dials, sends first as the first message and reads its answer.
// For a refused message, it then reads the WebSocket's close, whose status
// it returns; -1 for an answer that is no refusal.
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
	a, status := p.send(c, body)
	return c, a, status
}

// send sends body as the first message on c, and reads its answer, and for
// a refusal or an end the close after it.
func (p *TerminalPage) send(c *websocket.Conn, body []byte) (wire.TerminalAnswer, websocket.StatusCode) {
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
	var a wire.TerminalAnswer
	if err := json.Unmarshal(msg, &a); typ != websocket.MessageText || err != nil {
		p.t.Fatalf("the answer to the first message is %s %q, want text of JSON (%v)", typ, msg, err)
	}
	if a.Refused == "" && !a.Ended {
		return a, -1
	}
	return a, CloseStatus(ctx, c)
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

// Open opens an SSH Session to the Host named host at cols by rows, and
// returns it, failing the test unless it is answered with its id.
func (p *TerminalPage) Open(host string, cols, rows uint32) *WebTerminal {
	p.t.Helper()
	w, a := p.TryOpen(host, cols, rows)
	if w == nil {
		p.t.Fatalf("opening a web terminal to %s was refused: %s: %s", host, a.Refused, deref(a.Message))
	}
	return w
}

// TryOpen opens an SSH Session to the Host named host at cols by rows: the
// SSH Session once answered with its id, or nil and the refusal, after the
// WebSocket closed with status 1008.
func (p *TerminalPage) TryOpen(host string, cols, rows uint32) (*WebTerminal, wire.TerminalAnswer) {
	p.t.Helper()
	c, a, status := p.Exchange(wire.TerminalFirst{Protocol: wire.TerminalProtocol, Open: host, Cols: cols, Rows: rows})
	if a.Refused != "" {
		if status != websocket.StatusPolicyViolation {
			p.t.Errorf("the refused open closed with status %d, want %d", status, websocket.StatusPolicyViolation)
		}
		return nil, a
	}
	if len(a.Tunnel) != 32 {
		p.t.Fatalf("the open was answered %+v, want the SSH Session's id", a)
	}
	w := &WebTerminal{t: p.t, page: p, ID: a.Tunnel, ended: make(chan struct{}), changed: make(chan struct{})}
	w.stream = resumable.New(websocket.NetConn(context.Background(), c, websocket.MessageBinary))
	w.c = c
	go w.read()
	w.probe()
	p.t.Cleanup(func() { _ = w.stream.Close() })
	return w, a
}

// Resume Resumes w on a new WebSocket of the page, which may be another
// page than the one that opened it, and returns the answer: once accepted,
// the stream carries on there; a refusal leaves it Detached. Like the page,
// it tries again after a WebSocket that fails to open (redial).
func (p *TerminalPage) Resume(w *WebTerminal) wire.TerminalAnswer {
	p.t.Helper()
	received := w.stream.Detach()
	c, a, _ := p.exchangeOn(p.redial(), wire.TerminalFirst{Protocol: wire.TerminalProtocol, Resume: w.ID, Offset: received})
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

// End sends an end of the SSH Session id from the page, and returns the
// answer and the WebSocket's close status.
func (p *TerminalPage) End(id string) (wire.TerminalAnswer, websocket.StatusCode) {
	p.t.Helper()
	_, a, status := p.Exchange(wire.TerminalFirst{Protocol: wire.TerminalProtocol, End: id})
	return a, status
}

// WebTerminal is a web terminal's SSH Session as its page holds it: the
// resumable stream on the page's WebSocket, the records the koko server sent
// and how it ended.
type WebTerminal struct {
	t    testing.TB
	page *TerminalPage
	// ID is the SSH Session's id, as the open's answer gave it.
	ID     string
	stream *resumable.Stream

	mu sync.Mutex
	c  *websocket.Conn
	// output is the data the koko server sent; changed is closed and
	// replaced at each record.
	output  []byte
	changed chan struct{}
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

// Output returns the data the koko server has sent so far.
func (w *WebTerminal) Output() []byte {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.output
}

// WaitOutput waits until the data the koko server sent holds want, and
// returns it all. It fails the test after 20 s.
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
// WebSocket, as the page does when the person ends the SSH Session.
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
