package gateway

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/cieons/koko/internal/server/access"
	"github.com/cieons/koko/internal/server/op"
	"github.com/cieons/koko/internal/server/store"
	"github.com/cieons/koko/internal/shared/api"
	"github.com/cieons/koko/internal/shared/resumable"
	"github.com/cieons/koko/internal/shared/timing"
	"github.com/cieons/koko/internal/shared/wire"
)

// fakeWorkspaceSocket is a page's Workspace socket that reads nothing until
// it is cut or closed, and counts the messages written to it.
type fakeWorkspaceSocket struct {
	once   sync.Once
	cut    chan struct{}
	writes atomic.Int32
}

func (s *fakeWorkspaceSocket) Refuse(wire.TerminalAnswer) {}
func (s *fakeWorkspaceSocket) Read() ([]byte, error) {
	<-s.cut
	return nil, io.EOF
}

func (s *fakeWorkspaceSocket) Write([]byte) error {
	s.writes.Add(1)
	return nil
}
func (s *fakeWorkspaceSocket) Close(bool) { s.Cut() }
func (s *fakeWorkspaceSocket) Cut()       { s.once.Do(func() { close(s.cut) }) }

// In a synctest bubble, the Sign-in sweep finds a page's Sign-in expired, 7
// days after its last use, and detaches its pages: its attachment ends,
// logged with "signin expired", and its Workspace socket is cut. The SSH
// Session it is attached to goes on, as does the page of a Sign-in used
// since.
func TestTheSweepDetachesTheExpiredSignInsPages(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		st, err := store.Open(t.Context(), t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = st.Close() })
		now := time.Now().UTC()
		at := func(d time.Duration) string { return now.Add(d).Format(time.RFC3339) }
		err = st.Write(t.Context(), func(tx store.Tx) error {
			for _, q := range []struct {
				sql  string
				args []any
			}{
				{"INSERT INTO users (id, name, role, created_at) VALUES ('user_bob', 'bob', 'member', ?)", []any{at(0)}},
				{`INSERT INTO signins (id, user_id, token_hash, started_address, address, started_at, last_used_at, expires_at)
					VALUES ('signin_phone', 'user_bob', x'01', 'a', 'a', ?1, ?1, ?2)`, []any{at(0), at(30 * 24 * time.Hour)}},
				{`INSERT INTO signins (id, user_id, token_hash, started_address, address, started_at, last_used_at, expires_at)
					VALUES ('signin_desktop', 'user_bob', x'02', 'a', 'a', ?1, ?2, ?3)`, []any{at(0), at(6 * 24 * time.Hour), at(30 * 24 * time.Hour)}},
			} {
				if _, err := tx.ExecContext(context.Background(), q.sql, q.args...); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}

		var log syncBuffer
		g := New(st, nil, time.Now, timing.Timings{SignInCheck: time.Hour}, 0, slog.New(slog.NewTextHandler(&log, &slog.HandlerOptions{Level: slog.LevelDebug})))
		g.newEmulator = func(ctx context.Context, _, _ int) (emulator, error) { return &fakeEmulator{ctx: ctx}, nil }
		tg := &testGateway{Gateway: g, log: &log.b}
		t.Cleanup(g.Close)
		s, _ := tg.start(t)
		s.t.userID = "user_bob"
		phone := op.Caller{SignIn: "signin_phone"}
		desktop := op.Caller{SignIn: "signin_desktop"}
		phoneAttached := tg.attachAs(s, phone)
		desktopAttached := tg.attachAs(s, desktop)
		w := g.workspaces.workspace("user_bob")
		phoneSocket := &fakeWorkspaceSocket{cut: make(chan struct{})}
		desktopSocket := &fakeWorkspaceSocket{cut: make(chan struct{})}
		w.mu.Lock()
		for c, sock := range map[op.Caller]*fakeWorkspaceSocket{phone: phoneSocket, desktop: desktopSocket} {
			w.pages[&wsPage{w: w, c: TerminalCaller{Caller: c}, s: sock, wake: make(chan struct{}, 1), done: make(chan struct{})}] = struct{}{}
		}
		w.mu.Unlock()

		time.Sleep(7*24*time.Hour - time.Minute)
		synctest.Wait()
		if phoneAttached.a.ctx.Err() != nil {
			t.Fatal("the phone's page detached before its Sign-in expired")
		}
		time.Sleep(time.Hour + time.Minute)
		synctest.Wait()
		if cause := context.Cause(phoneAttached.a.ctx); !errors.Is(cause, errEnded) {
			t.Errorf("the expired Sign-in's attachment is %v, want it ended", cause)
		}
		select {
		case <-phoneSocket.cut:
		default:
			t.Error("the expired Sign-in's Workspace socket was not cut")
		}
		select {
		case <-desktopSocket.cut:
			t.Error("the Workspace socket of the Sign-in used since was cut")
		default:
		}
		if desktopAttached.a.ctx.Err() != nil {
			t.Error("the attachment of the Sign-in used since ended")
		}
		if err := s.t.ctx.Err(); err != nil {
			t.Errorf("the SSH Session ended with its page's Sign-in: %v", context.Cause(s.t.ctx))
		}
		if lines := log.String(); !regexp.MustCompile(`msg="ssh session detached" .* signin=signin_phone .*reason="signin expired"\n`).MatchString(lines) {
			t.Errorf("the log says no detach for the expired Sign-in:\n%s", lines)
		}
	})
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// A Workspace's SSH Session checked again acts as its User, and its opener
// takes back only what a revocation, a Password change or removal, or its
// User's removal takes back: one that signed out or expired, or a live one,
// leaves it to the User, and a Device Session Revoked ends it, as when the
// SSH Session was admitted across that change.
func TestARecheckTakesBackOnlyWhatItsOpenerTookBack(t *testing.T) {
	st, err := store.Open(t.Context(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	now := time.Now().UTC()
	at := func(d time.Duration) string { return now.Add(d).Format(time.RFC3339) }
	month := 30 * 24 * time.Hour
	err = st.Write(t.Context(), func(tx store.Tx) error {
		for _, q := range []struct {
			sql  string
			args []any
		}{
			{"INSERT INTO users (id, name, role, created_at) VALUES ('user_bob', 'bob', 'member', ?)", []any{at(0)}},
			{"INSERT INTO users (id, name, role, created_at, removed_at) VALUES ('user_carol', 'carol', 'member', ?1, ?1)", []any{at(0)}},
			{`INSERT INTO device_sessions (id, user_id, device, public_key, created_at, revoked_at)
				VALUES ('device_laptop', 'user_bob', 'laptop', x'01', ?1, ?1)`, []any{at(0)}},
		} {
			if _, err := tx.ExecContext(context.Background(), q.sql, q.args...); err != nil {
				return err
			}
		}
		for i, si := range []struct {
			id, user, endReason string
			lastUsed            time.Duration
		}{
			{"signin_live", "user_bob", "", 0},
			{"signin_signed_out", "user_bob", "signed_out", 0},
			{"signin_expired", "user_bob", "", -8 * 24 * time.Hour},
			{"signin_revoked", "user_bob", "revoked", 0},
			{"signin_password_changed", "user_bob", "password_changed", 0},
			{"signin_password_removed", "user_bob", "password_removed", 0},
			{"signin_carol", "user_carol", "", 0},
		} {
			var ended, reason any
			if si.endReason != "" {
				ended, reason = at(0), si.endReason
			}
			if _, err := tx.ExecContext(context.Background(), `INSERT INTO signins (id, user_id, token_hash, started_address, address, started_at, last_used_at, expires_at, ended_at, end_reason)
				VALUES (?, ?, ?, 'a', 'a', ?, ?, ?, ?, ?)`, si.id, si.user, []byte{byte(i)}, at(si.lastUsed), at(si.lastUsed), at(month), ended, reason); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, c := range []struct {
		caller op.Caller
		user   string
		// reason is how the SSH Session ends, or "" for it to be checked as
		// its User's.
		reason string
	}{
		{op.Caller{SignIn: "signin_live"}, "user_bob", ""},
		{op.Caller{SignIn: "signin_signed_out"}, "user_bob", ""},
		{op.Caller{SignIn: "signin_expired"}, "user_bob", ""},
		{op.Caller{Local: true}, "user_bob", ""},
		{op.Caller{SignIn: "signin_revoked"}, "user_bob", "signin revoked"},
		{op.Caller{SignIn: "signin_password_changed"}, "user_bob", "password changed"},
		{op.Caller{SignIn: "signin_password_removed"}, "user_bob", "password removed"},
		{op.Caller{DeviceSession: "device_laptop"}, "user_bob", "revoked"},
		{op.Caller{SignIn: "signin_carol"}, "user_carol", "user removed"},
	} {
		tun := &tunnel{caller: c.caller, session: &termSession{}, userID: c.user, user: "bob", hostName: "web1"}
		ctx := op.AtTime(context.Background(), now)
		var who access.Identity
		var end *ending
		err := st.Read(ctx, func(tx store.Tx) (err error) {
			who, end, err = workspaceWho(ctx, tx, tun)
			return err
		})
		switch {
		case err != nil:
			t.Errorf("checking the SSH Session %v opened: %v", c.caller, err)
		case c.reason == "" && (end != nil || who.User.ID != c.user):
			t.Errorf("the SSH Session %v opened ends with %v, as %q, want it checked as its User's", c.caller, end, who.User.ID)
		case c.reason != "" && (end == nil || end.reason != c.reason):
			t.Errorf("the SSH Session %v opened ends with %v, want %q", c.caller, end, c.reason)
		}
	}
}

// Each way a Sign-in ends has its own log reason, and every way but
// signing out and expiring takes back the SSH Sessions it opened, a way
// the Gateway does not know among them: it fails closed.
func TestEverySignInEndTakesBackButLeaving(t *testing.T) {
	for _, reason := range []string{api.EndSignedOut, api.EndExpired, api.EndRevoked, api.EndPasswordChanged, api.EndPasswordRemoved, api.EndUserRemoved, "a way added later"} {
		leaves := reason == api.EndSignedOut || reason == api.EndExpired
		if got := takesBack(reason); got == leaves {
			t.Errorf("takesBack(%q) = %v, want %v", reason, got, !leaves)
		}
		_, known := signInEnds[reason]
		if got := signInReason(reason); known == (got == "access withdrawn") {
			t.Errorf("signInReason(%q) = %q", reason, got)
		}
		tun := &tunnel{user: "bob", hostName: "web1"}
		if e := signInEnding(tun, reason); !leaves && (e == nil || e.notice == "") {
			t.Errorf("signInEnding(%q) = %+v, want an ending with its notice", reason, e)
		}
	}
}

// A revocation tells the koko server to close the Revoked Device Session's
// connections only once the device's pages of its SSH Sessions have been
// sent the notice and the close: the SSH Session's own ending may come
// first, and a page through koko web on that device would otherwise see a
// drop rather than why.
func TestARevocationWaitsForTheDevicesPagesToBeToldWhy(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tg := newTestGateway(t, 0)
		t.Cleanup(tg.Close)
		laptop := op.Caller{DeviceSession: "devsession_laptop"}
		tun := tg.newTunnel(laptop, clientBrowser, nil)
		tun.userID, tun.user, tun.hostID, tun.hostName = "user_bob", "bob", "host_web1", "web1"
		tg.mu.Lock()
		tg.tunnels[tun.id] = tun
		tg.mu.Unlock()
		s := tg.newSession(tun, nil, "tab", admission{}, callerKey{id: laptop.ID()}, 80, 24)
		sh := newFakeShell()
		if err := s.start(sh); err != nil {
			t.Fatal(err)
		}
		// As runSession ends: the pages finished, then the Tunnel gone.
		tg.wg.Go(func() {
			_ = s.awaitEnd()
			sh.end()
			tg.unregister(tun)
		})
		// The page's connection, whose writes from the Gateway wait while
		// held is not nil: a Device Connection that has yet to carry them.
		gw, pg := net.Pipe()
		conn := &heldConn{Conn: gw}
		a := newTestAttachment(s, laptop)
		p := &page{a: a, stream: resumable.New(pg), ended: make(chan struct{})}
		go p.read()
		tg.addAttachment(a, conn, func() {}, nil)
		synctest.Wait()
		if !p.has(1, "") {
			t.Fatal("the page got no snapshot")
		}

		release := conn.hold()
		t.Cleanup(release)
		told := tg.Withdraw(access.Change{Revoked: []string{laptop.DeviceSession}})
		synctest.Wait()
		select {
		case <-tun.done:
		default:
			t.Fatal("the SSH Session did not end")
		}
		select {
		case <-told:
			t.Fatal("the revocation was told done before the device's page was sent its notice")
		default:
		}

		release()
		synctest.Wait()
		select {
		case <-told:
		default:
			t.Fatal("the revocation was not told done once the page had its notice")
		}
		notice := revokedEnding(tun).notice
		if e, ok := errors.AsType[*resumable.PeerClosedError](p.err); !ok || e.Reason != notice {
			t.Errorf("the page's stream ended with %v, want the revocation's notice", p.err)
		}
	})
}

// heldConn is a connection whose writes wait while it is held.
type heldConn struct {
	net.Conn
	mu   sync.Mutex
	held chan struct{}
}

// hold holds the connection's writes, and returns what lets them go on.
func (c *heldConn) hold() func() {
	held := make(chan struct{})
	c.mu.Lock()
	c.held = held
	c.mu.Unlock()
	return sync.OnceFunc(func() {
		c.mu.Lock()
		c.held = nil
		c.mu.Unlock()
		close(held)
	})
}

func (c *heldConn) Write(p []byte) (int, error) {
	c.mu.Lock()
	held := c.held
	c.mu.Unlock()
	if held != nil {
		<-held
	}
	return c.Conn.Write(p)
}

// An SSH Session that ends while its start waits for its emulator, as the
// first one does while the engine compiles, is recorded in its tab as
// ended, with its ending, as one that ended before its shell started is:
// not refused as an open that failed.
func TestAnSSHSessionThatEndsWhileItStartsIsRecordedEnded(t *testing.T) {
	sg := newStoreGateway(t)
	making := make(chan struct{})
	sg.newEmulator = func(ctx context.Context, _, _ int) (emulator, error) {
		close(making)
		<-ctx.Done()
		return nil, fmt.Errorf("wait for libghostty-vt to compile: %w", context.Cause(ctx))
	}
	tun := sg.newTunnel(desktop, clientBrowser, nil)
	tun.userID = "user_bob"
	s := sg.newSession(tun, sg.w, "tab2", admission{}, callerKey{}, 80, 24)
	sg.w.mu.Lock()
	sg.w.tabs = append(sg.w.tabs, &wsTab{id: "tab2", state: wire.TabStateStarting, session: s})
	sg.w.mu.Unlock()
	started := make(chan bool, 1)
	go func() { started <- sg.startSession(s, newFakeShell(), "web1") }()
	<-making
	tun.end(errWebURLChanged)
	if <-started {
		t.Fatal("the SSH Session started")
	}
	sg.w.mu.Lock()
	tab, _ := sg.w.find("tab2")
	state, ended, refused := tab.state, tab.ended, tab.refused
	sg.w.mu.Unlock()
	if state != wire.TabStateEnded || ended == nil || ended.Reason != errWebURLChanged.reason || refused != nil {
		t.Errorf("the tab is %s, ended %+v, refused %+v; want ended with %q", state, ended, refused, errWebURLChanged.reason)
	}
	if log := sg.log.String(); strings.Contains(log, "level=ERROR") {
		t.Errorf("an SSH Session that ended as it started logged an error:\n%s", log)
	}
}
