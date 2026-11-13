package gateway

import (
	"context"
	"io"
	"log/slog"
	"net"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cieons/koko/internal/server/access"
	"github.com/cieons/koko/internal/server/op"
	"github.com/cieons/koko/internal/server/store"
	"github.com/cieons/koko/internal/shared/api"
	"github.com/cieons/koko/internal/shared/resumable"
	"github.com/cieons/koko/internal/shared/timing"
	"github.com/cieons/koko/internal/shared/wire"
)

// A caller that ends between the read of it and the registration of its
// page: each way a page comes in reads it again once registered, and one
// that has ended gets nothing and changes nothing.

var (
	phone   = op.Caller{SignIn: "signin_phone"}
	desktop = op.Caller{SignIn: "signin_desktop"}
)

// storeGateway is a Gateway on a store holding bob, whose Sign-ins phone and
// desktop are live, with one SSH Session the desktop opened running in his
// Workspace's tab, on a fake shell.
type storeGateway struct {
	*testGateway
	st  *store.Store
	s   *termSession
	sh  *fakeShell
	w   *workspace
	log *syncBuffer
}

func newStoreGateway(t *testing.T) *storeGateway {
	st, err := store.Open(t.Context(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	now := time.Now().UTC()
	at := func(d time.Duration) string { return now.Add(d).Format(time.RFC3339) }
	err = st.Write(t.Context(), func(tx store.Tx) error {
		if _, err := tx.ExecContext(context.Background(), "INSERT INTO users (id, name, role, created_at) VALUES ('user_bob', 'bob', 'member', ?)", at(0)); err != nil {
			return err
		}
		for i, id := range []string{phone.SignIn, desktop.SignIn} {
			if _, err := tx.ExecContext(context.Background(), `INSERT INTO signins (id, user_id, token_hash, started_address, address, started_at, last_used_at, expires_at)
				VALUES (?, 'user_bob', ?, 'a', 'a', ?, ?, ?)`, id, []byte{byte(i)}, at(0), at(0), at(30*24*time.Hour)); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	log := &syncBuffer{}
	g := New(st, nil, time.Now, timing.Timings{}, 0, slog.New(slog.NewTextHandler(log, &slog.HandlerOptions{Level: slog.LevelDebug})))
	g.newEmulator = func(ctx context.Context, _, _ int) (emulator, error) { return &fakeEmulator{ctx: ctx}, nil }
	t.Cleanup(g.Close)
	w := g.workspaces.workspace("user_bob")
	tun := g.newTunnel(desktop, clientBrowser, nil)
	tun.userID = "user_bob"
	s := g.newSession(tun, w, "tab", admission{}, callerKey{}, 80, 24)
	sh := newFakeShell()
	if err := s.start(sh); err != nil {
		t.Fatal(err)
	}
	g.wg.Go(func() { _ = s.awaitEnd() })
	t.Cleanup(func() {
		tun.end(errEnded)
		sh.end()
	})
	w.mu.Lock()
	w.loaded = true
	w.tabs = []*wsTab{{id: "tab", state: wire.TabStateRunning, session: s}}
	w.mu.Unlock()
	return &storeGateway{testGateway: &testGateway{Gateway: g, log: &log.b}, st: st, s: s, sh: sh, w: w, log: log}
}

// revokeInStore records the Sign-in id revoked, as signin.revoke does,
// without telling the Gateway.
func (sg *storeGateway) revokeInStore(t *testing.T, id string) {
	t.Helper()
	err := sg.st.Write(t.Context(), func(tx store.Tx) error {
		_, err := tx.ExecContext(context.Background(), "UPDATE signins SET ended_at = ?, end_reason = 'revoked' WHERE id = ?", time.Now().UTC().Format(time.RFC3339), id)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

// revoke revokes the Sign-in id, and has the Gateway take the change as
// the access-change signal hands it over.
func (sg *storeGateway) revoke(t *testing.T, id string) {
	t.Helper()
	sg.revokeInStore(t, id)
	sg.Withdraw(access.Change{SignIns: []string{id}, SignInEnd: api.EndRevoked})
}

// fakeTerminalSocket is a page's terminal socket on conn, whose Answer
// waits for hold, once answered is closed.
type fakeTerminalSocket struct {
	conn     net.Conn
	hold     chan struct{}
	answered chan struct{}

	mu       sync.Mutex
	answers  []wire.TerminalAnswer
	refusals []wire.TerminalAnswer
	cut      bool
}

func newFakeTerminalSocket(conn net.Conn) *fakeTerminalSocket {
	return &fakeTerminalSocket{conn: conn, hold: make(chan struct{}), answered: make(chan struct{})}
}

func (s *fakeTerminalSocket) Answer(a wire.TerminalAnswer) error {
	close(s.answered)
	<-s.hold
	s.mu.Lock()
	defer s.mu.Unlock()
	s.answers = append(s.answers, a)
	return nil
}

func (s *fakeTerminalSocket) Refuse(a wire.TerminalAnswer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refusals = append(s.refusals, a)
}

func (s *fakeTerminalSocket) Conn() net.Conn { return s.conn }

func (s *fakeTerminalSocket) Cut() {
	s.mu.Lock()
	s.cut = true
	s.mu.Unlock()
	_ = s.conn.Close()
}

// An attach whose caller is revoked while its answer is on its way never
// reaches the SSH Session: the page gets no screen, what it types reaches
// no shell, and its WebSocket is cut, the log saying why.
func TestAnAttachAcrossItsCallersEndNeverReachesTheShell(t *testing.T) {
	sg := newStoreGateway(t)
	gw, pg := net.Pipe()
	sock := newFakeTerminalSocket(gw)
	page := resumable.New(pg)
	go func() {
		// The page types at once, as one does into a screen it expects.
		rec, _ := wire.AppendTerminalRecord(nil, wire.TerminalRecord{Type: wire.TerminalData, Data: []byte("typed")})
		_, _ = page.Write(rec)
		_, _ = io.Copy(io.Discard, page)
	}()
	done := make(chan struct{})
	go func() {
		defer close(done)
		sg.attachTerminal(TerminalCaller{Caller: phone}, sock, sg.s.t.id, 80, 24)
	}()
	<-sock.answered
	sg.revoke(t, phone.SignIn)
	close(sock.hold)
	<-done

	log := sg.log.String()
	if !regexp.MustCompile(`msg="ssh session attach refused" .*signin=signin_phone .*reason="signin revoked"\n`).MatchString(log) {
		t.Errorf("the log says no attach refused for the revoked caller:\n%s", log)
	}
	if strings.Contains(log, `msg="ssh session attached"`) {
		t.Errorf("the revoked caller's page was attached:\n%s", log)
	}
	sock.mu.Lock()
	cut := sock.cut
	sock.mu.Unlock()
	if !cut {
		t.Error("the revoked caller's terminal socket was not cut")
	}
}

// A Workspace socket whose caller ended after it was read and before its
// page was registered gets nothing, not even the Workspace, and is cut. A
// message queued before the cut is written only when the page's writer
// runs first, so the straddle is tried 100 times.
func TestAWorkspaceSocketAcrossItsCallersEndGetsNothing(t *testing.T) {
	sg := newStoreGateway(t)
	seen := sg.withdrawalsTaken()
	sg.revoke(t, phone.SignIn)
	for range 100 {
		sock := &fakeWorkspaceSocket{cut: make(chan struct{})}
		sg.openWorkspace(TerminalCaller{Caller: phone}, sock, "user_bob", seen)
		select {
		case <-sock.cut:
		default:
			t.Fatal("the revoked caller's Workspace socket was not cut")
		}
		if n := sock.writes.Load(); n != 0 {
			t.Fatalf("the revoked caller's page got %d messages", n)
		}
	}
}

// A Workspace request whose caller ended after it was read changes
// nothing: the tab it closes stays, and its page is cut.
func TestARequestAcrossItsCallersEndChangesNothing(t *testing.T) {
	sg := newStoreGateway(t)
	sock := &fakeWorkspaceSocket{cut: make(chan struct{})}
	p := &wsPage{w: sg.w, c: TerminalCaller{Caller: phone}, s: sock, wake: make(chan struct{}, 1), done: make(chan struct{}), start: time.Now()}
	seen := sg.withdrawalsTaken()
	sg.revoke(t, phone.SignIn)
	sg.w.handle(p, wire.WorkspaceRequest{Request: 1, Close: "tab"}, seen)
	sg.w.mu.Lock()
	tabs := len(sg.w.tabs)
	sg.w.mu.Unlock()
	if tabs != 1 {
		t.Error("the revoked caller's request closed the tab")
	}
	if !p.isClosing() {
		t.Error("the revoked caller's page was not cut")
	}
}

// A resume reads its caller: one revoked since its attachment was made,
// whose attachment no change has detached yet, is refused.
func TestAResumeReadsItsCaller(t *testing.T) {
	sg := newStoreGateway(t)
	pg := sg.attachAs(sg.s, phone)
	waitFor(t, "snapshot", func() bool { return pg.has(1, "") })
	sg.revokeInStore(t, phone.SignIn)
	gw, other := net.Pipe()
	t.Cleanup(func() { _ = other.Close() })
	sock := newFakeTerminalSocket(gw)
	close(sock.hold)
	sg.resumeTerminal(TerminalCaller{Caller: phone}, sock, pg.a.id, 0)
	sock.mu.Lock()
	defer sock.mu.Unlock()
	if len(sock.answers) != 0 || len(sock.refusals) != 1 || sock.refusals[0].Refused != api.CodeSignInRequired.Name {
		t.Errorf("the revoked caller's resume was answered %+v and refused %+v, want signin_required", sock.answers, sock.refusals)
	}
}
