package gateway

import (
	"cmp"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cieons/koko/internal/server/access"
	"github.com/cieons/koko/internal/server/store"
	"github.com/cieons/koko/internal/shared/api"
	"github.com/cieons/koko/internal/shared/wire"
)

// Each User has one Workspace: the web terminal's tabs, held by the koko
// server and the same in every page of the web UI the User has open (spec
// #213, "The Workspace and its record"; ADR 0057). The Workspaces are the
// one reader and writer of the table workspace_tabs, which keeps each
// tab's Host, label and order across a restart; a tab's shell, screen and
// scrollback live in memory alone. A Workspace takes its pages' requests
// one at a time, in the order they arrive from any page: each change is
// one transaction, then the whole Workspace to every page, the asking
// page's answer after it.

// maxTabs is how many tabs one Workspace holds, and so how many web
// terminal SSH Sessions one User holds at once.
const maxTabs = 16

// pageQueueLimit is the most messages waiting for one page's Workspace
// socket: a page that reads none of them is cut, and gets the Workspace
// whole once it reconnects.
const pageQueueLimit = 64

// WorkspaceSocket is a page's Workspace socket, once the web front has read
// its first message.
type WorkspaceSocket interface {
	// Refuse sends a refusal of the first message, then closes the
	// WebSocket with status 1008.
	Refuse(wire.TerminalAnswer)
	// Read reads the page's next message, at most
	// wire.WorkspaceMaxMessage bytes of text.
	Read() ([]byte, error)
	// Write sends one text message, waiting a bounded time for a page that
	// reads nothing.
	Write([]byte) error
	// Close closes the WebSocket with its closing handshake: with status
	// 1008 for a page that broke the protocol, else 1000.
	Close(policyViolation bool)
	// Cut closes the WebSocket at once.
	Cut()
}

// Workspaces holds each User's Workspace that a page has asked for since
// the koko server started.
type Workspaces struct {
	g  *Gateway
	mu sync.Mutex
	of map[string]*workspace // by the User's id
}

// workspace is one User's Workspace.
type workspace struct {
	ws     *Workspaces
	userID string

	// mu is held by each change, which it applies in turn.
	mu     sync.Mutex
	loaded bool
	// gone is set once the User was Removed: the Workspace takes no page
	// and no request.
	gone  bool
	tabs  []*wsTab
	pages map[*wsPage]struct{}
}

// wsTab is one tab of a Workspace.
type wsTab struct {
	id, hostID, label, openedAt string
	state                       string
	ended                       *wire.TabEnded
	refused                     *wire.TabRefused
	// session is the tab's current SSH Session, or its last.
	session *termSession
}

// storedEnd is a tab's ended column: how its SSH Session ended, or why it
// did not start.
type storedEnd struct {
	Ended   *wire.TabEnded   `json:"ended,omitzero"`
	Refused *wire.TabRefused `json:"refused,omitzero"`
}

// Delete deletes the Workspace of the User whose id is userID in tx, for
// user.remove to call in its transaction. Their SSH Sessions end once it
// commits, through the access-change signal, which drops the Workspace held
// in memory then (drop), since the transaction may yet roll back.
func (ws *Workspaces) Delete(ctx context.Context, tx store.Tx, userID string) error {
	if _, err := tx.ExecContext(ctx, "DELETE FROM workspace_tabs WHERE user_id = ?", userID); err != nil {
		return fmt.Errorf("delete the Workspace of %s: %w", userID, err)
	}
	return nil
}

// drop forgets the Workspace of the User whose id is userID, once
// user.remove's Delete has committed: its pages are cut, and it takes no
// request, so no later load or page sees its tabs. A tab an open added
// after the Delete, from a request read before the removal, is deleted
// too.
func (ws *Workspaces) drop(userID string) {
	ws.mu.Lock()
	w := ws.of[userID]
	delete(ws.of, userID)
	ws.mu.Unlock()
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.gone = true
	for p := range w.pages {
		p.cut("user removed")
	}
	ctx := context.Background()
	err := ws.g.store.Write(ctx, func(tx store.Tx) error { return ws.Delete(ctx, tx, userID) })
	if err != nil {
		ws.g.log.Error("drop the Workspace of a User Removed", "user", userID, "err", err)
	}
}

// workspace returns the Workspace of the User whose id is userID.
func (ws *Workspaces) workspace(userID string) *workspace {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	w := ws.of[userID]
	if w == nil {
		w = &workspace{ws: ws, userID: userID, pages: map[*wsPage]struct{}{}}
		ws.of[userID] = w
	}
	return w
}

// held returns the Workspace of the User whose id is userID, if one is
// held, without making one.
func (ws *Workspaces) held(userID string) *workspace {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	return ws.of[userID]
}

// all returns every Workspace held.
func (ws *Workspaces) all() []*workspace {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	var all []*workspace
	for _, w := range ws.of {
		all = append(all, w)
	}
	return all
}

// loadLocked reads the Workspace's tabs, once. A tab whose SSH Session was
// live when the koko server stopped, cleanly or not, is restarted. w.mu is
// held.
func (w *workspace) loadLocked(ctx context.Context) error {
	if w.loaded {
		return nil
	}
	var tabs []*wsTab
	err := w.ws.g.store.Read(ctx, func(tx store.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT id, host_id, label, opened_at, ended
			FROM workspace_tabs WHERE user_id = ? ORDER BY position`, w.userID)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			tab := &wsTab{}
			var ended sql.NullString
			if err := rows.Scan(&tab.id, &tab.hostID, &tab.label, &tab.openedAt, &ended); err != nil {
				return err
			}
			tab.state = wire.TabStateRestarted
			if ended.Valid {
				var e storedEnd
				if err := json.Unmarshal([]byte(ended.String), &e); err != nil {
					return fmt.Errorf("read how the tab %s ended: %w", tab.id, err)
				}
				tab.ended, tab.refused = e.Ended, e.Refused
				tab.state = wire.TabStateEnded
				if e.Refused != nil {
					tab.state = wire.TabStateRefused
				}
			}
			tabs = append(tabs, tab)
		}
		return rows.Err()
	})
	if err != nil {
		return fmt.Errorf("read the Workspace of %s: %w", w.userID, err)
	}
	w.tabs, w.loaded = tabs, true
	return nil
}

// view is the Workspace as its pages get it. A tab's host is its Host's
// name now, read here, so that a renamed Host's tab reopens to the same
// Host; a Host the User can no longer see is given by the tab's label. A
// read that fails gives every tab's label. w.mu is held.
func (w *workspace) view(ctx context.Context) wire.Workspace {
	names := map[string]string{}
	err := w.ws.g.store.Read(ctx, func(tx store.Tx) error {
		var err error
		names, err = visibleHosts(ctx, tx, w.userID, w.hostIDs())
		return err
	})
	if err != nil {
		w.ws.g.log.Error("read the Hosts of a Workspace", "user", w.userID, "err", err)
	}
	v := wire.Workspace{Tabs: make([]wire.WorkspaceTab, 0, len(w.tabs))}
	for _, tab := range w.tabs {
		wt := wire.WorkspaceTab{
			ID: tab.id, Host: names[tab.hostID], Label: tab.label, State: tab.state, OpenedAt: tab.openedAt,
			Ended: tab.ended, Refused: tab.refused,
		}
		if wt.Host == "" {
			wt.Host = tab.label
		}
		if tab.state == wire.TabStateRunning {
			wt.Session = tab.session.t.id.String()
		}
		v.Tabs = append(v.Tabs, wt)
	}
	return v
}

func (w *workspace) hostIDs() []string {
	var ids []string
	for _, tab := range w.tabs {
		if !slices.Contains(ids, tab.hostID) {
			ids = append(ids, tab.hostID)
		}
	}
	return ids
}

// visibleHosts returns the names of the Hosts among ids that the User
// whose id is userID can see: those not Retired, in a Space they act in.
func visibleHosts(ctx context.Context, tx store.Tx, userID string, ids []string) (map[string]string, error) {
	names := map[string]string{}
	if len(ids) == 0 {
		return names, nil
	}
	who, err := access.OfUser(ctx, tx, userID)
	if err != nil {
		return nil, err
	}
	scope, err := who.Spaces(ctx, tx)
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		var name, space, state string
		err := tx.QueryRowContext(ctx, "SELECT name, space_id, state FROM hosts WHERE id = ?", id).Scan(&name, &space, &state)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			continue
		case err != nil:
			return nil, fmt.Errorf("read the Host %s: %w", id, err)
		}
		if state != api.HostRetired && scope.Has(space) {
			names[id] = name
		}
	}
	return names, nil
}

// broadcastLocked sends the Workspace to every page of it. w.mu is held.
func (w *workspace) broadcastLocked(ctx context.Context) {
	msg, err := json.Marshal(w.view(ctx))
	if err != nil {
		w.ws.g.log.Error("send a Workspace", "err", err)
		return
	}
	for p := range w.pages {
		p.send(msg)
	}
}

// find returns the tab whose id is id, and its place, or nil and -1.
func (w *workspace) find(id string) (*wsTab, int) {
	i := slices.IndexFunc(w.tabs, func(t *wsTab) bool { return t.id == id })
	if i < 0 {
		return nil, -1
	}
	return w.tabs[i], i
}

// label is the label of a new tab to the Host named name: the name, or
// the lowest "name (n)" from 2 that no tab holds.
func (w *workspace) label(name string) string {
	held := func(l string) bool { return slices.ContainsFunc(w.tabs, func(t *wsTab) bool { return t.label == l }) }
	if !held(name) {
		return name
	}
	for n := 2; ; n++ {
		if l := name + " (" + strconv.Itoa(n) + ")"; !held(l) {
			return l
		}
	}
}

// newID returns 128 random bits in lowercase hex, a tab's or an
// attachment's id.
func newID() string {
	var b [wire.TunnelIDSize]byte
	_, _ = rand.Read(b[:]) // never fails
	return hex.EncodeToString(b[:])
}

// openedBy is the opened_by column of a tab opened by the caller c.
func openedBy(c TerminalCaller) any {
	if id := c.Caller.ID(); id != "" {
		return id
	}
	return nil
}

// handle applies the page p's request r, one at a time with every other
// change of the Workspace, and answers it after the Workspace it changed.
// Its caller was read live once the access-change signal had been taken
// seen times: one that has ended since changes nothing, and its page is
// cut.
func (w *workspace) handle(p *wsPage, r wire.WorkspaceRequest, seen uint64) {
	ctx := context.Background()
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.gone || p.isClosing() {
		// Detached, or its User Removed: its socket is cut.
		return
	}
	if reason := w.ws.g.endedSince(p.c, seen); reason != "" {
		p.cut(reason)
		return
	}
	var a wire.WorkspaceAnswer
	switch {
	case r.Open != "":
		a = w.open(ctx, p, r)
	case r.Reopen != "":
		a = w.reopen(ctx, p, r)
	default:
		a = w.closeTab(ctx, p, r)
	}
	a.Request = r.Request
	if msg, err := json.Marshal(a); err == nil {
		p.send(msg)
	}
}

// refuseRequest answers a request with code and message.
func refuseRequest(code, message string) wire.WorkspaceAnswer {
	return wire.WorkspaceAnswer{Refused: code, Message: &message}
}

// openFailed refuses an SSH Session the koko server failed to open, which
// its log says why.
var openFailed = &refusal{code: api.CodeInternal.Name, banner: "koko: the koko server could not open this SSH Session; its log says why.\n"}

// refuseWith answers a request with the refusal r.
func refuseWith(r *refusal) wire.WorkspaceAnswer { return refuseRequest(r.code, r.message()) }

// open adds a tab to the Host r names at the end of the Workspace and
// starts its SSH Session. A refusal of the admission adds none; one of the
// Host or the shell, found later, leaves the tab refused.
func (w *workspace) open(ctx context.Context, p *wsPage, r wire.WorkspaceRequest) wire.WorkspaceAnswer {
	g := w.ws.g
	if len(w.tabs) >= maxTabs {
		limit := &refusal{banner: fmt.Sprintf("koko: this Workspace holds %d tabs; close one first\n", maxTabs), code: wire.TerminalSSHSessionLimit, reason: "ssh session limit"}
		g.logRefused(g.newTunnel(p.c.Caller, clientBrowser, p.c.Addr), r.Open, limit)
		return refuseWith(limit)
	}
	t, a, refused := g.admitTab(p.c, r.Open, r.Open)
	if refused != nil {
		return refuseWith(refused)
	}
	tab := &wsTab{id: newID(), hostID: a.host.ID, label: w.label(a.host.Name), openedAt: g.clock().UTC().Format(time.RFC3339)}
	err := g.store.Write(ctx, func(tx store.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO workspace_tabs (id, user_id, position, host_id, label, opened_by, opened_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)`, tab.id, w.userID, len(w.tabs), tab.hostID, tab.label, openedBy(p.c), tab.openedAt)
		return err
	})
	if err != nil {
		g.log.Error("open a web terminal", slices.Concat(t.named(), []any{"name", r.Open}, callerAttrs(t.caller), []any{"err", err})...)
		return refuseWith(openFailed)
	}
	w.tabs = append(w.tabs, tab)
	g.log.Debug("workspace tab opened", slices.Concat([]any{"user", w.userID, "tab", tab.id, "host", a.host.Name}, callerAttrs(p.c.Caller))...)
	w.startLocked(p.c, tab, t, a, int(r.Cols), int(r.Rows), false)
	w.broadcastLocked(ctx)
	return wire.WorkspaceAnswer{Tab: tab.id}
}

// reopen starts a new SSH Session in an ended, refused or restarted tab,
// to its Host, which it reads by its id, so that a renamed Host's tab
// reopens to the same Host and never to another that took its name. A tab
// starting or running is answered as it is, so two pages that reopen it
// at once start one. The page sends the next reopen of a restart's tabs
// once this one's tab has left starting, so that they dial one by one.
func (w *workspace) reopen(ctx context.Context, p *wsPage, r wire.WorkspaceRequest) wire.WorkspaceAnswer {
	g := w.ws.g
	tab, _ := w.find(r.Reopen)
	switch {
	case tab == nil:
		return refuseRequest(wire.WorkspaceUnknownTab, "this tab was closed")
	case tab.state == wire.TabStateStarting || tab.state == wire.TabStateRunning:
		return wire.WorkspaceAnswer{Tab: tab.id}
	}
	var name string
	err := g.store.Read(ctx, func(tx store.Tx) error {
		names, err := visibleHosts(ctx, tx, w.userID, []string{tab.hostID})
		name = names[tab.hostID]
		return err
	})
	if err != nil {
		g.log.Error("open a web terminal", slices.Concat([]any{"tab", tab.id}, callerAttrs(p.c.Caller), []any{"err", err})...)
		return refuseWith(openFailed)
	}
	if name == "" {
		r := &refusal{banner: unknownHost(tab.label), code: wire.TerminalUnknownHost, reason: "unknown host"}
		w.refuseLocked(ctx, tab, r)
		return refuseWith(r)
	}
	t, a, refused := g.admitTab(p.c, tab.hostID, name)
	if refused != nil {
		// A refusal of the caller's own, as of its credential, or a
		// failure of the koko server's, is the asking page's alone; one of
		// the Host is the tab's, which every page shows.
		if refused.code != wire.TerminalAccessRefused && refused != openFailed {
			w.refuseLocked(ctx, tab, refused)
		}
		return refuseWith(refused)
	}
	openedAt := g.clock().UTC().Format(time.RFC3339)
	err = g.store.Write(ctx, func(tx store.Tx) error {
		_, err := tx.ExecContext(ctx, "UPDATE workspace_tabs SET opened_by = ?, opened_at = ?, ended = NULL WHERE id = ?",
			openedBy(p.c), openedAt, tab.id)
		return err
	})
	if err != nil {
		g.log.Error("open a web terminal", slices.Concat(t.named(), []any{"tab", tab.id}, callerAttrs(t.caller), []any{"err", err})...)
		return refuseWith(openFailed)
	}
	tab.openedAt = openedAt
	w.startLocked(p.c, tab, t, a, int(r.Cols), int(r.Rows), tab.state == wire.TabStateRestarted)
	w.broadcastLocked(ctx)
	return wire.WorkspaceAnswer{Tab: tab.id}
}

// closeTab closes the tab r names, which ends its SSH Session, and closes
// up its place. A tab already gone is answered the same.
func (w *workspace) closeTab(ctx context.Context, p *wsPage, r wire.WorkspaceRequest) wire.WorkspaceAnswer {
	g := w.ws.g
	tab, at := w.find(r.Close)
	if tab == nil {
		return wire.WorkspaceAnswer{}
	}
	err := g.store.Write(ctx, func(tx store.Tx) error {
		if _, err := tx.ExecContext(ctx, "DELETE FROM workspace_tabs WHERE id = ?", tab.id); err != nil {
			return err
		}
		// From the closed tab's place on, each moves into the place freed
		// before it, which keeps (user_id, position) unique throughout.
		for i, later := range w.tabs[at+1:] {
			if _, err := tx.ExecContext(ctx, "UPDATE workspace_tabs SET position = ? WHERE id = ?", at+i, later.id); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		g.log.Error("close a web terminal's tab", slices.Concat([]any{"tab", tab.id}, callerAttrs(p.c.Caller), []any{"err", err})...)
		return refuseRequest(api.CodeInternal.Name, "the koko server could not close this tab; its log says why")
	}
	w.tabs = slices.Delete(w.tabs, at, at+1)
	if s := tab.session; s != nil && (tab.state == wire.TabStateStarting || tab.state == wire.TabStateRunning) {
		s.t.end(tabClosed(p.c.Caller))
	}
	g.log.Debug("workspace tab closed", slices.Concat([]any{"user", w.userID, "tab", tab.id, "label", tab.label}, callerAttrs(p.c.Caller))...)
	w.broadcastLocked(ctx)
	return wire.WorkspaceAnswer{}
}

// admitTab admits a new SSH Session of the caller c to the Host ref names,
// by its name or its id, named hostName, as a Tunnel's is: the Tunnel it
// is to be, not yet in the registry, and the admission, or the refusal.
func (g *Gateway) admitTab(c TerminalCaller, ref, hostName string) (*tunnel, admission, *refusal) {
	t := g.newTunnel(c.Caller, clientBrowser, c.Addr)
	t.run, t.seen = c.Run, g.withdrawalsTaken()
	a, err := g.admitRef(context.Background(), c.Caller, ref, hostName, c.WebURL)
	if err != nil {
		g.log.Error("open a web terminal", slices.Concat([]any{"client", clientBrowser}, callerAttrs(c.Caller), []any{"addr", c.Addr, "name", hostName, "err", err})...)
		return nil, a, openFailed
	}
	if a.refused != nil {
		g.logRefused(t, hostName, a.refused)
		return nil, a, a.refused
	}
	return t, a, nil
}

// startLocked starts the SSH Session t in the tab, opened by the caller c
// with a pty of cols by rows, once registered; restarted writes the
// restart's line first. w.mu is held.
func (w *workspace) startLocked(c TerminalCaller, tab *wsTab, t *tunnel, a admission, cols, rows int, restarted bool) {
	g := w.ws.g
	s := g.newSession(t, w, tab.id, a, c.key(), cols, rows)
	s.restarted = restarted
	tab.session, tab.state, tab.ended, tab.refused = s, wire.TabStateStarting, nil, nil
	if _, ok := g.register(t, a, t.seen); !ok {
		// The koko server stops: the tab comes back restarted.
		tab.state = wire.TabStateRestarted
		return
	}
	g.wg.Go(func() { g.runSession(s) })
}

// sessionOf returns the Workspace's live SSH Session id, or nil, and
// whether id names its tab's last SSH Session, which has ended.
func (w *workspace) sessionOf(id tunnelID) (*termSession, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, tab := range w.tabs {
		if s := tab.session; s != nil && s.t.id == id {
			if tab.state == wire.TabStateRunning && s.t.ctx.Err() == nil {
				return s, false
			}
			return nil, tab.state != wire.TabStateStarting
		}
	}
	return nil, false
}

// running marks the tab of the SSH Session s running.
func (w *workspace) running(s *termSession) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if tab, _ := w.find(s.tab); tab != nil && tab.session == s && tab.state == wire.TabStateStarting {
		tab.state = wire.TabStateRunning
		w.broadcastLocked(context.Background())
	}
}

// refused marks the tab of the SSH Session s refused, as r says.
func (w *workspace) refused(s *termSession, r *refusal) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if tab, _ := w.find(s.tab); tab != nil && tab.session == s {
		w.refuseLocked(context.Background(), tab, r)
	}
}

// refuseLocked records that the tab's SSH Session did not start, as r
// says, and sends the Workspace. w.mu is held.
func (w *workspace) refuseLocked(ctx context.Context, tab *wsTab, r *refusal) {
	tab.state, tab.ended, tab.refused = wire.TabStateRefused, nil, &wire.TabRefused{Code: r.code, Message: r.message()}
	w.storeEndLocked(ctx, tab, storedEnd{Refused: tab.refused})
	w.broadcastLocked(ctx)
}

// ended records how the SSH Session s ended in its tab, unless the tab is
// gone or holds another: closed, or reopened since. A stop records
// nothing, so the tab comes back restarted.
func (w *workspace) ended(s *termSession, e *ending, status *uint32, signal string) {
	if e == errStopping {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	tab, _ := w.find(s.tab)
	if tab == nil || tab.session != s {
		return
	}
	ctx := context.Background()
	tab.state, tab.refused = wire.TabStateEnded, nil
	tab.ended = &wire.TabEnded{Reason: e.reason, Notice: e.notice, ExitStatus: status, ExitSignal: signal}
	w.storeEndLocked(ctx, tab, storedEnd{Ended: tab.ended})
	w.broadcastLocked(ctx)
}

// storeEndLocked writes how the tab's SSH Session ended. w.mu is held.
func (w *workspace) storeEndLocked(ctx context.Context, tab *wsTab, e storedEnd) {
	g := w.ws.g
	b, err := json.Marshal(e)
	if err == nil {
		err = g.store.Write(ctx, func(tx store.Tx) error {
			_, err := tx.ExecContext(ctx, "UPDATE workspace_tabs SET ended = ? WHERE id = ?", string(b), tab.id)
			return err
		})
	}
	if err != nil {
		g.log.Error("record how a web terminal's tab ended", "tab", tab.id, "err", err)
	}
}

// ServeWorkspace serves the page's Workspace socket s for the caller c, in
// a goroutine of its own: the page gets its User's Workspace, then every
// change of it, and its requests are applied in turn with every other
// page's. While the Gateway stops, it cuts s instead.
func (g *Gateway) ServeWorkspace(c TerminalCaller, s WorkspaceSocket) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed || g.terminalsClosed {
		s.Cut()
		return
	}
	g.wg.Go(func() { g.serveWorkspace(c, s) })
}

func (g *Gateway) serveWorkspace(c TerminalCaller, s WorkspaceSocket) {
	seen := g.withdrawalsTaken()
	u, refused := g.userOf(c)
	if refused != nil {
		s.Refuse(*refused)
		return
	}
	g.openWorkspace(c, s, u.ID, seen)
}

// openWorkspace serves the Workspace socket s of the caller c, read live
// as the User whose id is userID once the access-change signal had been
// taken seen times, until it ends.
func (g *Gateway) openWorkspace(c TerminalCaller, s WorkspaceSocket, userID string, seen uint64) {
	w := g.workspaces.workspace(userID)
	p := &wsPage{w: w, c: c, s: s, wake: make(chan struct{}, 1), done: make(chan struct{}), start: time.Now()}
	w.mu.Lock()
	if err := w.loadLocked(context.Background()); err != nil {
		w.mu.Unlock()
		g.log.Error("open a Workspace", slices.Concat(callerAttrs(c.Caller), []any{"err", err})...)
		s.Refuse(wire.TerminalRefusal(api.CodeInternal.Name, "the koko server could not read this Workspace; its log says why"))
		return
	}
	if !g.terminalsOpen() || w.gone {
		w.mu.Unlock()
		s.Cut()
		return
	}
	// Among the pages first, where a change from now on finds it to
	// detach, then checked against any change since its caller was read,
	// before it gets anything.
	w.pages[p] = struct{}{}
	if reason := g.endedSince(c, seen); reason != "" {
		delete(w.pages, p)
		w.mu.Unlock()
		g.log.Debug("workspace socket closed", append(callerAttrs(c.Caller), "reason", reason)...)
		s.Cut()
		return
	}
	if msg, err := json.Marshal(w.view(context.Background())); err == nil {
		p.send(msg)
	}
	w.mu.Unlock()
	g.wg.Go(p.write)
	g.whenRunEnds(c, p.done, func() { p.cut("koko web ended") })
	p.read()
	w.mu.Lock()
	delete(w.pages, p)
	w.mu.Unlock()
	p.stop(nil)
	<-p.done
}

// userOf reads the User the caller c acts as, or the refusal of a caller
// that is no one now, such as a Sign-in that ended.
func (g *Gateway) userOf(c TerminalCaller) (api.User, *wire.TerminalAnswer) {
	ctx := g.readContext(context.Background())
	var who access.Identity
	err := g.store.Read(ctx, func(tx store.Tx) (err error) {
		who, err = access.Who(ctx, tx, c.Caller)
		return err
	})
	if e, ok := errors.AsType[*api.Error](err); ok {
		return api.User{}, new(wire.TerminalRefusal(e.Code, e.Message))
	}
	if err != nil {
		g.log.Error("read who a web terminal's page is", slices.Concat(callerAttrs(c.Caller), []any{"err", err})...)
		return api.User{}, new(wire.TerminalRefusal(api.CodeInternal.Name, "the koko server failed; its log says why"))
	}
	return who.User, nil
}

// wsPage is one page's Workspace socket.
type wsPage struct {
	w     *workspace
	c     TerminalCaller
	s     WorkspaceSocket
	start time.Time
	// received is when a message last arrived, as time since start.
	received atomic.Int64

	mu      sync.Mutex
	queue   [][]byte
	closing bool
	wake    chan struct{}
	// done is closed once the writer has left.
	done chan struct{}
}

// send queues msg for the page. A page that has let pageQueueLimit pile
// up is cut.
func (p *wsPage) send(msg []byte) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closing {
		return
	}
	if len(p.queue) >= pageQueueLimit {
		p.closing = true
		p.queue = nil
		// Not here, under the Workspace's lock: a cut through koko web on a
		// stalled Device Connection can take its time.
		p.w.ws.g.wg.Go(p.s.Cut)
		signal(p.wake)
		return
	}
	p.queue = append(p.queue, msg)
	signal(p.wake)
}

// stop queues last, unless nil, as the page's last message, after which
// the writer closes the socket.
func (p *wsPage) stop(last []byte) {
	p.mu.Lock()
	if !p.closing {
		if last != nil {
			p.queue = append(p.queue, last)
		}
		p.closing = true
	}
	p.mu.Unlock()
	signal(p.wake)
}

// write sends the queue to the page, and {} after timing.ServerProbe
// without sending, and cuts the socket after timing.ServerDead without a
// message from the page, until the page leaves or stop has its last
// message sent.
func (p *wsPage) write() {
	defer close(p.done)
	g := p.w.ws.g
	probe, dead := g.timings.ServerProbe, g.timings.ServerDead
	timer := time.NewTimer(min(probe, dead))
	defer timer.Stop()
	sent := time.Now()
	for {
		p.mu.Lock()
		var msg []byte
		if len(p.queue) > 0 {
			msg = p.queue[0]
			p.queue = p.queue[1:]
		}
		closing := p.closing
		p.mu.Unlock()
		switch {
		case msg != nil:
			if p.s.Write(msg) != nil {
				p.s.Cut()
				return
			}
			sent = time.Now()
			continue
		case closing:
			p.s.Close(false)
			return
		}
		select {
		case <-p.wake:
			continue
		case <-timer.C:
		}
		quiet := time.Since(p.start) - time.Duration(p.received.Load())
		if quiet >= dead {
			g.log.Debug("workspace socket found dead", slices.Concat(callerAttrs(p.c.Caller), []any{"quiet", quiet.Round(time.Millisecond)})...)
			p.s.Cut()
			return
		}
		next := probe - time.Since(sent)
		if next <= 0 {
			if p.s.Write([]byte("{}")) != nil {
				p.s.Cut()
				return
			}
			sent, next = time.Now(), probe
		}
		timer.Reset(min(next, dead-quiet))
	}
}

// read applies the page's requests until its socket ends. A message that
// is no request closes the socket with status 1008.
func (p *wsPage) read() {
	for {
		msg, err := p.s.Read()
		if err != nil {
			return
		}
		p.received.Store(int64(time.Since(p.start)))
		var r wire.WorkspaceRequest
		if err = json.Unmarshal(msg, &r); err == nil {
			err = r.Check()
		}
		if err != nil {
			p.w.ws.g.log.Debug("workspace socket closed", slices.Concat(callerAttrs(p.c.Caller), []any{"reason", "malformed message", "err", err})...)
			p.s.Close(true)
			return
		}
		if r.Request == 0 {
			continue // liveness alone
		}
		// A request acts for the caller as it is now: one that has ended,
		// such as a Sign-in found expired, changes nothing, and every page
		// of the caller detaches, to find out why as it reconnects. A read
		// that fails fails this request alone.
		g := p.w.ws.g
		seen := g.withdrawalsTaken()
		u, refused := g.userOf(p.c)
		switch {
		case refused != nil && refused.Refused == api.CodeInternal.Name:
			if msg, err := json.Marshal(wire.WorkspaceAnswer{Request: r.Request, Refused: refused.Refused, Message: refused.Message}); err == nil {
				p.send(msg)
			}
			continue
		case refused != nil || u.ID != p.w.userID:
			reason := cmp.Or(g.callerEnd(p.c), "access withdrawn")
			p.cut(reason)
			g.detachCaller(p.c.Caller, reason)
			return
		}
		p.w.handle(p, r, seen)
	}
}
