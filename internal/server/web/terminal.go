package web

import (
	"context"
	"encoding/json/v2"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/coder/websocket"

	"github.com/cieons/koko/internal/server/gateway"
	"github.com/cieons/koko/internal/server/op"
	"github.com/cieons/koko/internal/server/transport"
	"github.com/cieons/koko/internal/shared/api"
	"github.com/cieons/koko/internal/shared/wire"
)

// TerminalPath is the web terminal's terminal socket, on the web listener
// and on web@koko alike (P1 spec: a terminal in the web UI, "The terminal
// endpoint"; ADR 0053): the page attaches to one of its Workspace's SSH
// Sessions on each (spec #213). The page at /terminal opens them.
const TerminalPath = "/terminal/socket"

// WorkspacePath is the Workspace socket, on the web listener and on
// web@koko alike, which carries the page's Workspace and its requests
// (spec #213, "The Workspace socket").
const WorkspacePath = "/terminal/workspace"

// terminalReadLimit is the most bytes a web terminal's page may send in one
// WebSocket message; a frame of the resumable stream takes at most a little
// over half of it.
const terminalReadLimit = 64 << 10

// terminalWay is how a web terminal's WebSocket reached the web front: the
// Origin its page must have, and who the page is once its first message has
// been read, or the refusal of a page that is no one.
type terminalWay struct {
	// origin tells whether an Origin header names a page of this way in.
	origin func(string) bool
	// page answers a refusal before the upgrade, as this way's pages read.
	page pageWriter
	// caller tells who the page is, or refuses it with an answer.
	caller func(ctx context.Context) (gateway.TerminalCaller, *wire.TerminalAnswer)
}

// listenerTerminal is the way in of a WebSocket on the web listener, whose
// Web URL is webURL: its Origin must be the Web URL, and its page is the
// live Sign-in its cookie names, which the check moves the Last used time
// of.
func (f *Front) listenerTerminal(r *http.Request, webURL string) terminalWay {
	return terminalWay{
		origin: func(origin string) bool { return origin == webURL },
		page:   listenerPage,
		caller: func(ctx context.Context) (gateway.TerminalCaller, *wire.TerminalAnswer) {
			id, err := f.signins.Check(ctx, signInToken(r), attempt(r))
			if err != nil {
				return gateway.TerminalCaller{}, f.terminalRefusal(r, err)
			}
			return gateway.TerminalCaller{Caller: op.Caller{SignIn: id}, Addr: pageAddr(clientAddress(r)), WebURL: webURL}, nil
		},
	}
}

// kokoWebTerminal is the way in of a WebSocket on a web@koko channel of the
// connection conn, koko web's on port: its Origin must be koko web's own
// address, and its page is the connection's Device Session, or the run of
// koko web on the Local caller's connection, whose cookie it carries.
func (f *Front) kokoWebTerminal(r *http.Request, conn *transport.Connection, port int) terminalWay {
	return terminalWay{
		origin: func(origin string) bool { return localOrigin(origin, port) },
		page:   refusePage,
		caller: func(ctx context.Context) (gateway.TerminalCaller, *wire.TerminalAnswer) {
			sub, state := f.runs.subject(conn)
			if state == subjectRevoked {
				return gateway.TerminalCaller{}, new(wire.TerminalRefusal(api.CodeRevoked.Name, "this device's Device Session was revoked, and with it the web UI's cookie: koko web has stopped, and this device must join the koko server again"))
			}
			valid := false
			if state == subjectFound {
				var err error
				if valid, err = f.checkCookie(ctx, r, cookieName(port), sub); err != nil {
					return gateway.TerminalCaller{}, f.terminalRefusal(r, err)
				}
			}
			if !valid {
				return gateway.TerminalCaller{}, new(wire.TerminalRefusal(api.CodeCookieRequired.Name, "this browser holds no cookie of this koko web: open the address koko web printed, or run koko web again for a new one"))
			}
			c := gateway.TerminalCaller{Caller: conn.Caller(), Addr: conn.RemoteAddr()}
			if c.Caller.DeviceSession == "" {
				// A run of koko web is its connection, and ends with it.
				c.Run, c.RunEnded = string(sub), conn.Context().Done()
			}
			return c, nil
		},
	}
}

// acceptSocket accepts a request for one of the web terminal's WebSockets,
// which the checks every request gets have let through, and reads its first
// message, in this order: a GET that is no WebSocket upgrade gets a 426
// page, and a WebSocket from another Origin, or none, a 403 page
// (bad_origin), before anything else is read. Then the WebSocket is
// accepted, with compression off and no subprotocol, and its first message
// read (readFirst). It returns the WebSocket and the message, or false once
// it has answered.
func (f *Front) acceptSocket(w http.ResponseWriter, r *http.Request, way terminalWay) (*websocket.Conn, []byte, bool) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		way.page(w, http.StatusMethodNotAllowed, "the web terminal's endpoint takes a WebSocket's GET alone")
		return nil, nil, false
	}
	if !headerHas(r.Header, "Connection", "upgrade") || !headerHas(r.Header, "Upgrade", "websocket") {
		w.Header().Set("Connection", "Upgrade")
		w.Header().Set("Upgrade", "websocket")
		way.page(w, http.StatusUpgradeRequired, "this is the web terminal's WebSocket, which only a page of the web UI opens")
		return nil, nil, false
	}
	if !way.origin(r.Header.Get("Origin")) {
		f.log.Debug("web terminal refused", "reason", "origin", "client", clientAddress(r))
		way.page(w, api.CodeBadOrigin.Status, "this WebSocket came from no page of this koko server, so it was refused")
		return nil, nil, false
	}
	// The Origin is checked above, more strictly than Accept's own check,
	// which compares it with the Host alone.
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true, CompressionMode: websocket.CompressionDisabled})
	if err != nil {
		f.log.Debug("web terminal refused", "reason", "handshake", "client", clientAddress(r), "err", err)
		return nil, nil, false
	}
	c.SetReadLimit(terminalReadLimit)
	first, err := f.readFirst(c)
	if err != nil {
		f.log.Debug("web terminal refused", "reason", "first message", "client", clientAddress(r), "err", err)
		return nil, nil, false
	}
	return c, first, true
}

// admitPage refuses, in its first answer through refuse, a page out of
// date, whose first message named another protocol (reload_page), then one
// that is no one (way.caller). It returns who the page is, or false once
// it has refused.
func (f *Front) admitPage(r *http.Request, way terminalWay, protocol int, refuse func(wire.TerminalAnswer)) (gateway.TerminalCaller, bool) {
	if protocol != wire.TerminalProtocol {
		f.log.Debug("web terminal refused", "reason", "protocol", "client", clientAddress(r), "protocol", protocol)
		refuse(wire.TerminalRefusal(wire.TerminalReloadPage, "this page is out of date: reload it"))
		return gateway.TerminalCaller{}, false
	}
	caller, refused := way.caller(r.Context())
	if refused != nil {
		f.log.Debug("web terminal refused", "reason", refused.Refused, "client", clientAddress(r))
		refuse(*refused)
		return gateway.TerminalCaller{}, false
	}
	return caller, true
}

// serveTerminal answers a request for the terminal socket (acceptSocket).
// Every refusal after the upgrade is its first answer, followed by a close
// with status 1008, since a browser shows a page none of a refused upgrade
// (admitPage); a first message that asks for no attach or resume is closed
// so. An accepted message goes to the Gateway, which answers it.
func (f *Front) serveTerminal(w http.ResponseWriter, r *http.Request, way terminalWay) {
	c, body, ok := f.acceptSocket(w, r, way)
	if !ok {
		return
	}
	s := &terminalSocket{c: c}
	var first wire.TerminalFirst
	if err := json.Unmarshal(body, &first); err != nil {
		f.log.Debug("web terminal refused", "reason", "first message", "client", clientAddress(r), "err", err)
		_ = c.Close(websocket.StatusPolicyViolation, "")
		return
	}
	caller, ok := f.admitPage(r, way, first.Protocol, s.Refuse)
	if !ok {
		return
	}
	if err := first.Check(); err != nil {
		f.log.Debug("web terminal refused", "reason", "first message", "client", clientAddress(r), "err", err)
		_ = c.Close(websocket.StatusPolicyViolation, "")
		return
	}
	if first.Attach != "" {
		id, _ := wire.ParseTerminalID(first.Attach) // checked above
		f.gateway.AttachTerminal(caller, s, id, first.Cols, first.Rows)
		return
	}
	id, _ := wire.ParseTerminalID(first.Resume) // checked above
	f.gateway.ResumeTerminal(caller, s, id, first.Offset)
}

// serveWorkspace answers a request for the Workspace socket, with the
// terminal socket's checks in their order (acceptSocket, admitPage), and
// hands an accepted page to the Gateway, which sends it the Workspace.
func (f *Front) serveWorkspace(w http.ResponseWriter, r *http.Request, way terminalWay) {
	c, body, ok := f.acceptSocket(w, r, way)
	if !ok {
		return
	}
	c.SetReadLimit(wire.WorkspaceMaxMessage)
	s := &workspaceSocket{terminalSocket{c: c}}
	var first wire.WorkspaceFirst
	if err := json.Unmarshal(body, &first); err != nil {
		f.log.Debug("web terminal refused", "reason", "first message", "client", clientAddress(r), "err", err)
		_ = c.Close(websocket.StatusPolicyViolation, "")
		return
	}
	caller, ok := f.admitPage(r, way, first.Protocol, s.Refuse)
	if !ok {
		return
	}
	f.gateway.ServeWorkspace(caller, s)
}

// readFirst reads a web terminal's first message: one text message, at
// most wire.TerminalMaxFirst bytes, within timing.FirstMessage of the
// upgrade. Without one, it closes the WebSocket with status 1008.
func (f *Front) readFirst(c *websocket.Conn) ([]byte, error) {
	// A timeout of the read's own would close the connection without a
	// status: the timer closes it with one instead, which ends the read.
	late := time.AfterFunc(f.timings.FirstMessage, func() {
		_ = c.Close(websocket.StatusPolicyViolation, "no first message came")
	})
	first, err := readFirstMessage(c)
	if !late.Stop() {
		return first, errors.New("no first message came in time")
	}
	if err != nil {
		_ = c.Close(websocket.StatusPolicyViolation, "")
	}
	return first, err
}

// readFirstMessage reads the first message off c.
func readFirstMessage(c *websocket.Conn) ([]byte, error) {
	typ, msg, err := c.Reader(context.Background())
	if err != nil {
		return nil, err
	}
	if typ != websocket.MessageText {
		return nil, errors.New("the first message is not text")
	}
	body, err := io.ReadAll(io.LimitReader(msg, wire.TerminalMaxFirst+1))
	if err != nil {
		return nil, err
	}
	if len(body) > wire.TerminalMaxFirst {
		return nil, errors.New("the first message is over " + strconv.Itoa(wire.TerminalMaxFirst) + " bytes")
	}
	return body, nil
}

// terminalRefusal is the answer refusing a page that the check of who it is
// failed with err: its code when it is the API's, such as signin_required,
// and otherwise internal_error, whose cause the log holds.
func (f *Front) terminalRefusal(r *http.Request, err error) *wire.TerminalAnswer {
	if e, ok := errors.AsType[*api.Error](err); ok {
		if _, registered := api.LookupCode(e.Code); registered {
			return new(wire.TerminalRefusal(e.Code, e.Message))
		}
	}
	f.log.Error("web request failed", "path", loggedPath(r), "err", err)
	return new(wire.TerminalRefusal(api.CodeInternal.Name, "the koko server failed; its log says why"))
}

// localOrigin tells whether origin is koko web's own address on port, as a
// page of it sends: http://localhost or http://127.0.0.1, with the port, or
// without it when it is HTTP's own, 80.
func localOrigin(origin string, port int) bool {
	u, err := url.Parse(origin)
	if err != nil || u.Scheme != "http" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	return hostAllowed(u.Host, port)
}

// headerHas tells whether the header name lists token, without regard to
// case, as Connection and Upgrade carry theirs.
func headerHas(h http.Header, name, token string) bool {
	for _, v := range h.Values(name) {
		for t := range strings.SplitSeq(v, ",") {
			if strings.EqualFold(strings.TrimSpace(t), token) {
				return true
			}
		}
	}
	return false
}

// pageAddr is the address of the client a request on the web listener came
// from (clientAddress), as a log line names a web terminal's.
type pageAddr string

func (pageAddr) Network() string  { return "tcp" }
func (a pageAddr) String() string { return string(a) }

var _ net.Addr = pageAddr("")

// terminalSocket is a web terminal's WebSocket, as the web front hands it to
// the Gateway (gateway.TerminalSocket).
type terminalSocket struct {
	c *websocket.Conn
}

// Answer sends a as one text message, within answerWait, as a page that
// sent its first message reads its answer at once.
func (s *terminalSocket) Answer(a wire.TerminalAnswer) error {
	body, err := json.Marshal(a)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), answerWait)
	defer cancel()
	return s.c.Write(ctx, websocket.MessageText, body)
}

// answerWait bounds how long an answer to a first message waits for a page
// that reads nothing.
const answerWait = 10 * time.Second

func (s *terminalSocket) Refuse(a wire.TerminalAnswer) {
	if s.Answer(a) == nil {
		_ = s.c.Close(websocket.StatusPolicyViolation, "")
		return
	}
	s.Cut()
}

func (s *terminalSocket) Conn() net.Conn {
	nc := websocket.NetConn(context.Background(), s.c, websocket.MessageBinary)
	// NetConn lifts the read limit; the page's messages keep it.
	s.c.SetReadLimit(terminalReadLimit)
	return nc
}

func (s *terminalSocket) Cut() {
	_ = s.c.CloseNow()
}

// workspaceSocket is a page's Workspace socket, as the web front hands it
// to the Gateway (gateway.WorkspaceSocket): text messages of JSON.
type workspaceSocket struct {
	terminalSocket
}

// Read reads the page's next message, which must be text.
func (s *workspaceSocket) Read() ([]byte, error) {
	typ, msg, err := s.c.Read(context.Background())
	if err != nil {
		return nil, err
	}
	if typ != websocket.MessageText {
		return nil, errors.New("a Workspace message is not text")
	}
	return msg, nil
}

// Write sends msg as one text message, within answerWait.
func (s *workspaceSocket) Write(msg []byte) error {
	ctx, cancel := context.WithTimeout(context.Background(), answerWait)
	defer cancel()
	return s.c.Write(ctx, websocket.MessageText, msg)
}

// Close closes the WebSocket with its closing handshake, which a page that
// answers none bounds at 5 s, the library's own.
func (s *workspaceSocket) Close(policyViolation bool) {
	status := websocket.StatusNormalClosure
	if policyViolation {
		status = websocket.StatusPolicyViolation
	}
	_ = s.c.Close(status, "")
}

var (
	_ gateway.TerminalSocket  = (*terminalSocket)(nil)
	_ gateway.WorkspaceSocket = (*workspaceSocket)(nil)
)
