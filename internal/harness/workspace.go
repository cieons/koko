package harness

import (
	"context"
	"encoding/json/v2"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/cieons/koko/internal/server/vt"
	"github.com/cieons/koko/internal/server/web"
	"github.com/cieons/koko/internal/shared/wire"
)

// WorkspacePage is a page's Workspace socket (web.WorkspacePath), as the
// page's TypeScript end holds it: every message the koko server sent, in
// order, the Workspace each carried and the answers to the page's
// requests.
type WorkspacePage struct {
	t    *TerminalPage
	c    *websocket.Conn
	next uint64

	mu sync.Mutex
	// messages are the koko server's messages in order; workspace is the
	// last Workspace among them; answers are by request; stopping is the
	// stopping message's text, if one came; err is why the socket ended.
	messages  []string
	workspace wire.Workspace
	answers   map[uint64]wire.WorkspaceAnswer
	stopping  string
	err       error
	ended     chan struct{}
	changed   chan struct{}
}

// Workspace returns the page's Workspace socket, opening it the first
// time, and fails the test when it is refused.
func (p *TerminalPage) Workspace() *WorkspacePage {
	p.t.Helper()
	if p.ws == nil {
		ws, a, _ := p.DialWorkspace(wire.TerminalProtocol)
		if ws == nil {
			p.t.Fatalf("the Workspace socket was refused: %s: %s", a.Refused, deref(a.Message))
		}
		p.ws = ws
	}
	return p.ws
}

// DialWorkspaceSocket opens the Workspace socket and sends nothing. When
// the upgrade is refused, it returns the answer, its body read, beside the
// error.
func (p *TerminalPage) DialWorkspaceSocket() (*websocket.Conn, Answer, error) {
	return p.dial(web.WorkspacePath)
}

// DialWorkspace opens a Workspace socket of the page, sending protocol in
// its first message, and returns it once the Workspace came, or nil, the
// refusal and the WebSocket's close status.
func (p *TerminalPage) DialWorkspace(protocol int) (*WorkspacePage, wire.TerminalAnswer, websocket.StatusCode) {
	p.t.Helper()
	c, _, err := p.dial(web.WorkspacePath)
	if err != nil {
		p.t.Fatalf("open the Workspace socket: %v", err)
	}
	body, err := json.Marshal(wire.WorkspaceFirst{Protocol: protocol})
	if err != nil {
		p.t.Fatal(err)
	}
	var first struct {
		wire.TerminalAnswer
		wire.Workspace
	}
	if status := p.send(c, body, &first); status != -1 || first.Refused != "" {
		return nil, first.TerminalAnswer, status
	}
	ws := &WorkspacePage{
		t: p, c: c, workspace: first.Workspace, answers: map[uint64]wire.WorkspaceAnswer{},
		ended: make(chan struct{}), changed: make(chan struct{}),
	}
	raw, _ := json.Marshal(first.Workspace)
	ws.messages = []string{string(raw)}
	go ws.read()
	ws.probe()
	return ws, wire.TerminalAnswer{}, -1
}

// read reads the koko server's messages until the socket ends.
func (ws *WorkspacePage) read() {
	for {
		_, msg, err := ws.c.Read(context.Background())
		ws.mu.Lock()
		if err != nil {
			ws.err = err
			close(ws.ended)
			close(ws.changed)
			ws.mu.Unlock()
			return
		}
		ws.messages = append(ws.messages, string(msg))
		var m struct {
			Tabs     *[]wire.WorkspaceTab `json:"tabs"`
			Request  *uint64              `json:"request"`
			Stopping string               `json:"stopping"`
		}
		if json.Unmarshal(msg, &m) == nil {
			switch {
			case m.Tabs != nil:
				ws.workspace = wire.Workspace{Tabs: *m.Tabs}
			case m.Request != nil:
				var a wire.WorkspaceAnswer
				_ = json.Unmarshal(msg, &a)
				ws.answers[a.Request] = a
			case m.Stopping != "":
				ws.stopping = m.Stopping
			}
		}
		close(ws.changed)
		ws.changed = make(chan struct{})
		ws.mu.Unlock()
	}
}

// probe sends {} each Probe of the page, as the page does after 15 s
// without sending, until the socket ends.
func (ws *WorkspacePage) probe() {
	if ws.t.Probe == 0 {
		return
	}
	go func() {
		t := time.NewTicker(ws.t.Probe)
		defer t.Stop()
		for {
			select {
			case <-ws.ended:
				return
			case <-t.C:
				_ = ws.c.Write(context.Background(), websocket.MessageText, []byte("{}"))
			}
		}
	}()
}

// Send sends r, numbered next unless it carries a number, and returns its
// number.
func (ws *WorkspacePage) Send(r wire.WorkspaceRequest) uint64 {
	ws.t.t.Helper()
	ws.mu.Lock()
	if r.Request == 0 {
		ws.next++
		r.Request = ws.next
	}
	ws.mu.Unlock()
	body, err := json.Marshal(r)
	if err != nil {
		ws.t.t.Fatal(err)
	}
	ws.SendRaw(body)
	return r.Request
}

// TrySend sends r as Send does, and returns the error of a socket the koko
// server may have cut already, rather than failing the test.
func (ws *WorkspacePage) TrySend(r wire.WorkspaceRequest) error {
	ws.mu.Lock()
	if r.Request == 0 {
		ws.next++
		r.Request = ws.next
	}
	ws.mu.Unlock()
	body, err := json.Marshal(r)
	if err != nil {
		return err
	}
	return ws.c.Write(ws.t.t.Context(), websocket.MessageText, body)
}

// SendRaw sends body as one text message.
func (ws *WorkspacePage) SendRaw(body []byte) {
	ws.t.t.Helper()
	if err := ws.c.Write(ws.t.t.Context(), websocket.MessageText, body); err != nil {
		ws.t.t.Fatalf("send a Workspace message: %v", err)
	}
}

// Answer waits for the answer to the request n.
func (ws *WorkspacePage) Answer(n uint64) wire.WorkspaceAnswer {
	ws.t.t.Helper()
	var a wire.WorkspaceAnswer
	ws.waitFor("the answer to request "+strconv.FormatUint(n, 10), func() bool {
		var ok bool
		a, ok = ws.answers[n]
		return ok
	})
	return a
}

// Open asks to open a tab to the Host named host at cols by rows, and
// returns the answer, once the emulator has compiled (waitEmulator).
func (ws *WorkspacePage) Open(host string, cols, rows uint32) wire.WorkspaceAnswer {
	ws.t.t.Helper()
	ws.waitEmulator()
	return ws.Answer(ws.Send(wire.WorkspaceRequest{Open: host, Cols: cols, Rows: rows}))
}

// Reopen asks for a new SSH Session in the tab, and returns the answer,
// once the emulator has compiled (waitEmulator).
func (ws *WorkspacePage) Reopen(tab string, cols, rows uint32) wire.WorkspaceAnswer {
	ws.t.t.Helper()
	ws.waitEmulator()
	return ws.Answer(ws.Send(wire.WorkspaceRequest{Reopen: tab, Cols: cols, Rows: rows}))
}

// emulatorCompiles bounds the wait for the web terminals' emulator to
// compile: about 11 s under -race on CI's runner, longer while other test
// processes compile theirs.
const emulatorCompiles = 2 * time.Minute

// waitEmulator waits for the web terminals' emulator to compile, once per
// test process, which an SSH Session's start waits for, so that the 20 s
// waits for a tab and its terminal time those alone (#227). The koko
// server a page reaches runs in the test's process, and so does its
// compile. It fails the test after emulatorCompiles.
func (ws *WorkspacePage) waitEmulator() {
	ws.t.t.Helper()
	ctx, cancel := context.WithTimeout(ws.t.t.Context(), emulatorCompiles)
	defer cancel()
	if _, err := vt.Engine(ctx); err != nil {
		ws.t.t.Fatalf("the web terminals' emulator did not compile: %v", err)
	}
}

// CloseTab asks to close the tab, and returns the answer.
func (ws *WorkspacePage) CloseTab(tab string) wire.WorkspaceAnswer {
	ws.t.t.Helper()
	return ws.Answer(ws.Send(wire.WorkspaceRequest{Close: tab}))
}

// Tabs returns the tabs of the last Workspace the koko server sent.
func (ws *WorkspacePage) Tabs() []wire.WorkspaceTab {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	return slices.Clone(ws.workspace.Tabs)
}

// Messages returns the koko server's messages so far, in order.
func (ws *WorkspacePage) Messages() []string {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	return slices.Clone(ws.messages)
}

// WaitTabs waits until the last Workspace's tabs pass cond, which what
// describes, and returns them.
func (ws *WorkspacePage) WaitTabs(what string, cond func([]wire.WorkspaceTab) bool) []wire.WorkspaceTab {
	ws.t.t.Helper()
	ws.waitFor("a Workspace with "+what, func() bool { return cond(ws.workspace.Tabs) })
	return ws.Tabs()
}

// WaitTab waits until the tab id passes cond, which what describes, and
// returns it.
func (ws *WorkspacePage) WaitTab(id, what string, cond func(wire.WorkspaceTab) bool) wire.WorkspaceTab {
	ws.t.t.Helper()
	var tab wire.WorkspaceTab
	ws.WaitTabs("its tab "+id+" "+what, func(tabs []wire.WorkspaceTab) bool {
		i := slices.IndexFunc(tabs, func(t wire.WorkspaceTab) bool { return t.ID == id })
		if i < 0 {
			return false
		}
		tab = tabs[i]
		return cond(tab)
	})
	return tab
}

// WaitStopping waits for the stopping message and the socket's end, and
// returns the message.
func (ws *WorkspacePage) WaitStopping() string {
	ws.t.t.Helper()
	ws.waitFor("the stopping message", func() bool { return ws.stopping != "" })
	select {
	case <-ws.ended:
	case <-time.After(20 * time.Second):
		ws.t.t.Fatal("the Workspace socket did not close within 20 s of stopping")
	}
	ws.mu.Lock()
	defer ws.mu.Unlock()
	return ws.stopping
}

// Ended is closed once the socket has ended.
func (ws *WorkspacePage) Ended() <-chan struct{} { return ws.ended }

// Err is why the socket ended, or nil while it lives.
func (ws *WorkspacePage) Err() error {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	return ws.err
}

// Close closes the socket, as a page that leaves does.
func (ws *WorkspacePage) Close() {
	_ = ws.c.Close(websocket.StatusNormalClosure, "")
}

func (ws *WorkspacePage) waitFor(what string, cond func() bool) {
	ws.t.t.Helper()
	deadline := time.NewTimer(20 * time.Second)
	defer deadline.Stop()
	for {
		ws.mu.Lock()
		ok, changed, ended := cond(), ws.changed, ws.err != nil
		// The last message that is not a probe's {}, which says what the
		// page last learnt.
		last := ""
		for _, m := range slices.Backward(ws.messages) {
			if m != "{}" {
				last = m
				break
			}
		}
		ws.mu.Unlock()
		if ok {
			return
		}
		if ended {
			ws.t.t.Fatalf("the Workspace socket ended (%v) before %s; its last message but probes was %s", ws.Err(), what, last)
		}
		select {
		case <-changed:
		case <-deadline.C:
			ws.t.t.Fatalf("the Workspace socket did not get %s within 20 s; its last message but probes was %s", what, last)
		}
	}
}
