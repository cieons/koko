package web_test

import (
	"context"
	"encoding/json/v2"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/server/store"
	"github.com/cieons/koko/internal/shared/api"
	"github.com/cieons/koko/internal/shared/timing"
	"github.com/cieons/koko/internal/shared/wire"
)

// What ends a Workspace's SSH Session, and what only detaches a page (spec
// #213): a credential taken back ends what it opened, leaving only detaches,
// and access is checked as the User's, whoever opened the SSH Session.

// openCat opens a tab to the Host named hostName from the page p, whose
// shell runs cat, and returns the page's attachment to it.
func openCat(t *testing.T, p *harness.TerminalPage, hostName string) *harness.WebTerminal {
	t.Helper()
	w := p.Open(hostName, 80, 24)
	w.Type([]byte("echo ready; exec cat\n"))
	w.WaitOutput("ready\n")
	return w
}

// pages are two Sign-ins of one User, a phone and a desktop, each with its
// page of the web terminal: each opened a tab to a Host and attached to the
// other's too.
type pages struct {
	phone, desktop         *harness.Browser
	phonePage, desktopPage *harness.TerminalPage
	// phoneTab is the phone's attachment to the tab it opened, and
	// desktopOnPhone the desktop's to it; desktopTab and phoneOnDesktop
	// likewise for the desktop's.
	phoneTab, desktopOnPhone, desktopTab, phoneOnDesktop *harness.WebTerminal
}

// openPages signs user in on a phone and a desktop, and opens their pages
// to the Host named hostName.
func openPages(t *testing.T, h *harness.Harness, user, hostName string) *pages {
	t.Helper()
	p := &pages{phone: h.SignIn(user), desktop: signInAgain(t, h, user)}
	p.phonePage, p.desktopPage = p.phone.TerminalPage(), p.desktop.TerminalPage()
	p.phoneTab = openCat(t, p.phonePage, hostName)
	p.desktopTab = openCat(t, p.desktopPage, hostName)
	p.desktopOnPhone = p.desktopPage.Attach(p.phoneTab.ID, 80, 24)
	p.phoneOnDesktop = p.phonePage.Attach(p.desktopTab.ID, 80, 24)
	return p
}

// signInOf returns the id of the browser b's Sign-in.
func signInOf(t *testing.T, b *harness.Browser) string {
	t.Helper()
	status, body := b.Send(api.UserCurrent, []byte("{}"))
	var current api.UserCurrentResponse
	if err := json.Unmarshal(body, &current); err != nil || status != http.StatusOK || current.SignIn == nil {
		t.Fatalf("user.current: %d %s", status, body)
	}
	return current.SignIn.ID
}

// signOut signs the browser b out.
func signOut(t *testing.T, b *harness.Browser) {
	t.Helper()
	if a := b.Do(http.MethodPost, api.AuthSignOutPath, "{}"); a.StatusCode != http.StatusOK {
		t.Fatalf("signing out answered %s", a.Status)
	}
}

// wantEnded fails the test unless the attachment w ended with notice, on a
// line of its own and as its stream's close, and its SSH Session's ended
// line says reason.
func wantEnded(t *testing.T, h *harness.Harness, w *harness.WebTerminal, notice, reason string) {
	t.Helper()
	wantPeerClosed(t, w.WaitEnd(), notice)
	if out := string(w.Output()); !strings.HasSuffix(out, "\r\nkoko: "+notice+"\r\n") {
		t.Errorf("the terminal got %q, want it to end with the notice on a line of its own", out)
	}
	waitLog(t, h, `msg="ssh session ended" tunnel=`+w.ID+` client=browser .* reason="`+regexp.QuoteMeta(reason)+`"\n`)
}

// wantTabEnded fails the test unless the Workspace socket ws holds the tab
// ended, keeping notice for the pages that attach later.
func wantTabEnded(t *testing.T, ws *harness.WorkspacePage, tab, notice string) {
	t.Helper()
	ws.WaitTab(tab, "ended with its notice", func(tab wire.WorkspaceTab) bool {
		return tab.State == wire.TabStateEnded && tab.Ended != nil && tab.Ended.Notice == notice
	})
}

// wantDetached fails the test unless the attachment w's WebSocket was cut
// without its SSH Session ending, and the log says it detached for reason.
func wantDetached(t *testing.T, h *harness.Harness, w *harness.WebTerminal, reason string) {
	t.Helper()
	w.WaitDropped()
	if err := w.Err(); err != nil {
		t.Errorf("the detached terminal's stream ended with %v, want its WebSocket cut", err)
	}
	waitLog(t, h, `msg="ssh session detached" tunnel=`+w.ID+` client=browser tab=\S+ attachment=`+w.Attachment+` .* reason="?`+regexp.QuoteMeta(reason)+`"?\n`)
}

// wantCut fails the test unless the Workspace socket ws ends.
func wantCut(t *testing.T, ws *harness.WorkspacePage) {
	t.Helper()
	select {
	case <-ws.Ended():
	case <-time.After(10 * time.Second):
		t.Fatal("the page's Workspace socket stayed open")
	}
}

// wantKept fails the test unless the terminal w still echoes what is typed.
func wantKept(t *testing.T, w *harness.WebTerminal, line string) {
	t.Helper()
	w.Type([]byte(line + "\n"))
	w.WaitOutput(line + "\n")
}

// wantNoEnd fails the test if the log says an SSH Session ended.
func wantNoEnd(t *testing.T, h *harness.Harness) {
	t.Helper()
	if log := h.Log(); strings.Contains(log, `msg="ssh session ended"`) {
		t.Errorf("the log says an SSH Session ended:\n%s", log)
	}
}

// signedOut is the web front's refusal of a page whose Sign-in has ended,
// which comes before the Gateway reads the first message.
const signedOut = "this browser is not signed in, or its Sign-in has ended: sign in again"

// Leaving only detaches: a phone that signs out, or whose Sign-in expires,
// found by the sweep, at a call or at a Workspace request, has its pages
// detached, its attachments cut and its Workspace socket closed, and
// attaches no more, while every SSH Session goes on, those it opened
// among them, and the desktop's page stays attached to them all. A request
// from the expired page changes nothing.
func TestLeavingOnlyDetaches(t *testing.T) {
	t.Parallel()
	// expire lets 8 days pass, the desktop used on the 6th, so the
	// phone's Sign-in alone expires.
	expire := func(t *testing.T, h *harness.Harness, p *pages) {
		h.Advance(6 * 24 * time.Hour)
		signInOf(t, p.desktop)
		h.Advance(2 * 24 * time.Hour)
	}
	for _, c := range []struct {
		name    string
		timings timing.Timings
		leave   func(t *testing.T, h *harness.Harness, p *pages)
		reason  string
	}{
		{
			name:   "signed out",
			leave:  func(t *testing.T, _ *harness.Harness, p *pages) { signOut(t, p.phone) },
			reason: "signed out",
		},
		{
			name:    "expired, found by the sweep",
			timings: timing.Timings{SignInCheck: 20 * time.Millisecond},
			leave:   expire,
			reason:  "signin expired",
		},
		{
			name: "expired, found at a call",
			leave: func(t *testing.T, h *harness.Harness, p *pages) {
				expire(t, h, p)
				if status, body := p.phone.Send(api.UserCurrent, []byte("{}")); status != http.StatusUnauthorized {
					t.Fatalf("user.current of the expired Sign-in: %d %s", status, body)
				}
			},
			reason: "signin expired",
		},
		{
			name: "expired, found at a Workspace request",
			leave: func(t *testing.T, h *harness.Harness, p *pages) {
				expire(t, h, p)
				p.phonePage.Workspace().Send(wire.WorkspaceRequest{Close: p.desktopTab.Tab})
			},
			reason: "signin expired",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			h := startTerminals(t, harness.Timings(c.timings))
			terminalHost(t, h, "web1", []string{"space=bob"})
			p := openPages(t, h, "bob", "web1")
			phoneWS, desktopWS := p.phonePage.Workspace(), p.desktopPage.Workspace()

			c.leave(t, h, p)
			wantDetached(t, h, p.phoneTab, c.reason)
			wantDetached(t, h, p.phoneOnDesktop, c.reason)
			wantCut(t, phoneWS)
			for _, w := range []*harness.WebTerminal{p.desktopTab, p.desktopOnPhone} {
				wantKept(t, w, "still here")
			}
			if tabs := desktopWS.Tabs(); len(tabs) != 2 || tabs[0].State != wire.TabStateRunning || tabs[1].State != wire.TabStateRunning {
				t.Errorf("the desktop's Workspace holds %+v, want both tabs running", tabs)
			}
			_, a := p.phone.TerminalPage().TryAttach(p.desktopTab.ID, 80, 24)
			wantRefused(t, a, api.CodeSignInRequired.Name, signedOut)
			wantNoEnd(t, h)
		})
	}
}

// A credential taken back ends the SSH Sessions it opened, with the notice
// in every page attached and kept on the tab, and detaches its pages; what
// another Sign-in opened goes on, and so does what a Sign-in that had
// signed out before opened, which it takes back no more. Revoking the
// phone's Sign-in or changing the Password from the desktop ends the
// phone's; removing the Password ends what each live Sign-in opened;
// removing the User ends every one and deletes the Workspace.
func TestTakingBackASignInEndsWhatItOpened(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name string
		take func(t *testing.T, h *harness.Harness, p *pages)
		// desktopEnds says the desktop's own tab ends too; laptopEnds, the
		// tab the signed-out laptop opened.
		desktopEnds, laptopEnds bool
		why, reason             string
	}{
		{
			name: "revoked",
			take: func(t *testing.T, h *harness.Harness, p *pages) {
				h.Koko("signin", "revoke", signInOf(t, p.phone)).WantSuccess(t)
			},
			why:    "this browser's Sign-in was revoked",
			reason: "signin revoked",
		},
		{
			name: "password changed",
			take: func(t *testing.T, _ *harness.Harness, p *pages) {
				status, body := p.desktop.Post(api.AuthPasswordPath, api.AuthPasswordRequest{Current: harness.Password, Password: "the changed password of bob"})
				if status != http.StatusOK {
					t.Fatalf("changing the Password: %d %s", status, body)
				}
			},
			why:    "the Password was changed",
			reason: "password changed",
		},
		{
			name:        "password removed",
			take:        func(t *testing.T, h *harness.Harness, _ *pages) { h.Koko("password", "rm", "bob").WantSuccess(t) },
			desktopEnds: true,
			why:         "the Password was removed",
			reason:      "password removed",
		},
		{
			name:        "user removed",
			take:        func(t *testing.T, h *harness.Harness, _ *pages) { h.Koko("user", "remove", "bob").WantSuccess(t) },
			desktopEnds: true, laptopEnds: true,
			why:    "bob was Removed",
			reason: "user removed",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			h := startTerminals(t, harness.DataDir(dir))
			terminalHost(t, h, "web1", []string{"space=bob"})
			p := openPages(t, h, "bob", "web1")
			laptop := signInAgain(t, h, "bob")
			laptopTab := openCat(t, laptop.TerminalPage(), "web1")
			desktopOnLaptop := p.desktopPage.Attach(laptopTab.ID, 80, 24)
			signOut(t, laptop)
			wantDetached(t, h, laptopTab, "signed out")
			phoneWS, desktopWS := p.phonePage.Workspace(), p.desktopPage.Workspace()
			p.phoneOnDesktop.Drop()

			c.take(t, h, p)
			notice := "access to web1 was withdrawn: " + c.why + "; this SSH Session is closed."
			for _, w := range []*harness.WebTerminal{p.phoneTab, p.desktopOnPhone} {
				wantEnded(t, h, w, notice, c.reason)
			}
			wantCut(t, phoneWS)
			wantRefused(t, p.phone.TerminalPage().Resume(p.phoneOnDesktop), api.CodeSignInRequired.Name, signedOut)
			if c.desktopEnds {
				wantEnded(t, h, p.desktopTab, notice, c.reason)
				wantCut(t, desktopWS)
			} else {
				wantTabEnded(t, desktopWS, p.phoneTab.Tab, notice)
				wantKept(t, p.desktopTab, "still here")
			}
			switch {
			case c.laptopEnds:
				// user.remove names the Sign-ins it ended before the User:
				// the desktop's page may detach from the laptop's SSH
				// Session before the second change ends it, or get its
				// notice; it is off it either way.
				waitLog(t, h, `msg="ssh session ended" tunnel=`+laptopTab.ID+` client=browser .* reason="user removed"\n`)
				desktopOnLaptop.WaitDropped()
			case c.desktopEnds:
				wantDetached(t, h, desktopOnLaptop, c.reason)
			default:
				wantKept(t, desktopOnLaptop, "the laptop's still here")
			}
			if !c.laptopEnds && strings.Contains(h.Log(), `msg="ssh session ended" tunnel=`+laptopTab.ID+` `) {
				t.Error("the SSH Session the signed-out laptop opened ended")
			}

			if c.reason == "user removed" {
				h.Stop()
				st, err := store.Open(t.Context(), dir)
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = st.Close() }()
				var n int
				if err := st.Read(t.Context(), func(tx store.Tx) error {
					return tx.QueryRowContext(context.Background(), "SELECT count(*) FROM workspace_tabs").Scan(&n)
				}); err != nil || n != 0 {
					t.Errorf("after user.remove the store holds %d workspace_tabs rows (%v), want none", n, err)
				}
			}
		})
	}
}

// A change that takes a Host out of the User's reach ends the SSH Session
// to it, whoever opened it, here a phone that has signed out since: the
// desktop's live page gets the notice, and its dropped one's resume is
// refused saying why. bob taken out of the Space, the Host moved to a Space
// he cannot act in, Retired, or its Relay sent back to Registered.
func TestLosingAccessEndsATerminal(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name           string
		change         [][]string
		notice, reason string
	}{
		{
			name:   "space.member.remove",
			change: [][]string{{"space", "member", "rm", "ops", "bob"}},
			notice: "access to web1 was withdrawn; this SSH Session is closed.",
			reason: "access withdrawn",
		},
		{
			name:   "host.move",
			change: [][]string{{"space", "add", "vault"}, {"host", "move", "web1", "vault"}},
			notice: "access to web1 was withdrawn; this SSH Session is closed.",
			reason: "access withdrawn",
		},
		{
			name:   "host.retire",
			change: [][]string{{"host", "retire", "web1"}},
			notice: "access to web1 was withdrawn: web1 was Retired; this SSH Session is closed.",
			reason: "host retired",
		},
		{
			name:   "relay registered",
			change: [][]string{{"host", "set", "gw", "account=ops"}},
			notice: "access to web1 was withdrawn: web1's Relay is no longer Managed; this SSH Session is closed.",
			reason: "relay registered",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			h := startTerminals(t)
			h.Koko("space", "add", "ops").WantSuccess(t)
			h.Koko("space", "member", "add", "ops", "bob").WantSuccess(t)
			terminalHost(t, h, "gw", nil)
			terminalHost(t, h, "web1", []string{"space=ops", "relay=gw"})
			phone, desktop := h.SignIn("bob"), signInAgain(t, h, "bob")
			opened := openCat(t, phone.TerminalPage(), "web1")
			live := desktop.TerminalPage().Attach(opened.ID, 80, 24)
			dropped := desktop.TerminalPage().Attach(opened.ID, 80, 24)
			dropped.Drop()
			signOut(t, phone)
			wantDetached(t, h, opened, "signed out")

			for _, args := range c.change {
				h.Koko(args...).WantSuccess(t)
			}
			wantEnded(t, h, live, c.notice, c.reason)
			wantRefused(t, desktop.TerminalPage().Resume(dropped), wire.TerminalResumeRefused, strings.TrimSuffix(c.notice, "; this SSH Session is closed."))
		})
	}
}

// Demoting an Administrator ends their terminals to a Host outside the
// Spaces they still act in, here the Owner's Personal Space, with its
// notice, and keeps their terminal to a Host in a Space they are a Member
// of.
func TestDemotingAnAdministratorEndsTheirTerminalsOutsideTheirSpaces(t *testing.T) {
	t.Parallel()
	h := startTerminals(t)
	h.Koko("user", "promote", "bob").WantSuccess(t)
	h.Koko("space", "add", "ops").WantSuccess(t)
	h.Koko("space", "member", "add", "ops", "bob").WantSuccess(t)
	terminalHost(t, h, "web1", []string{"space=" + harness.Owner})
	terminalHost(t, h, "web2", []string{"space=ops"})
	p := h.SignIn("bob").TerminalPage()
	lost := openCat(t, p, "web1")
	kept := openCat(t, p, "web2")

	h.Koko("user", "demote", "bob").WantSuccess(t)
	wantEnded(t, h, lost, "access to web1 was withdrawn; this SSH Session is closed.", "access withdrawn")
	wantKept(t, kept, "still here")
}

// A change that keeps the User's access ends none of their terminals, here
// one a Sign-in opened that has signed out since, which the desktop's page
// shows and resumes: renaming the Host, changing its address, port, Note
// and Attributes, moving it to another Space bob acts in, revoking another
// of bob's Sign-ins and taking another User out of the Space.
func TestAChangeThatKeepsAccessEndsNoTerminal(t *testing.T) {
	t.Parallel()
	h := startTerminals(t)
	h.Koko("user", "invite", "carol").WantSuccess(t)
	for _, s := range []string{"ops", "dev"} {
		h.Koko("space", "add", s).WantSuccess(t)
		h.Koko("space", "member", "add", s, "bob", "carol").WantSuccess(t)
	}
	terminalHost(t, h, "web1", []string{"space=ops"})
	opener, desktop, other := h.SignIn("bob"), signInAgain(t, h, "bob"), signInAgain(t, h, "bob")
	h.SignIn("carol")
	opened := openCat(t, opener.TerminalPage(), "web1")
	w := desktop.TerminalPage().Attach(opened.ID, 80, 24)
	signOut(t, opener)
	wantDetached(t, h, opened, "signed out")
	otherID := signInOf(t, other)

	for _, change := range [][]string{
		{"host", "rename", "web1", "web9"},
		{"host", "set", "web9", "address=localhost", "port=2222", "provider=hetzner"},
		{"host", "set", "web9", "--note-file", "-"},
		{"host", "move", "web9", "dev"},
		{"signin", "revoke", otherID},
		{"space", "member", "rm", "dev", "carol"},
	} {
		h.KokoStdin("keep it", change...).WantSuccess(t)
		wantKept(t, w, "after "+strings.Join(change, " "))
	}
	// The move and carol taken out are the changes the Gateway checks
	// again, after each commits: the check itself, not the echo, says it
	// kept the SSH Session.
	waitLog(t, h, `msg="access checked again" ssh_sessions=1 ended=0\n`)
	w.Drop()
	if a := desktop.TerminalPage().Resume(w); a.Refused != "" {
		t.Fatalf("the resume was refused: %s", deref(a.Message))
	}
	wantKept(t, w, "after the resume")
	wantNoEnd(t, h)
}

// A Web URL change ends every SSH Session a Sign-in opened, through the web
// listener, with its notice, and detaches every page there; a resume at
// the new address is refused saying why, and an open sent after the change
// on a Workspace socket taken before it opens nothing; setting the same
// Web URL again ends nothing.
func TestAWebURLChangeEndsTheListenersTerminals(t *testing.T) {
	t.Parallel()
	h := startTerminals(t)
	terminalHost(t, h, "web1", []string{"space=bob"})
	p := openPages(t, h, "bob", "web1")
	phoneWS := p.phonePage.Workspace()
	p.phoneOnDesktop.Drop()
	// A page at the old address whose Workspace socket the web listener
	// took before the change, and which sends its open after it.
	opening := p.phone.TerminalPage().Workspace()

	h.Koko("server", "set", "web_url="+harness.WebURL).WantSuccess(t)
	wantKept(t, p.phoneTab, "after the same Web URL")

	const moved = "https://koko.example.org"
	h.Koko("server", "set", "web_url="+moved).WantSuccess(t)
	const notice = "the Web URL changed; this SSH Session is closed. Sign in at the new address."
	for _, w := range []*harness.WebTerminal{p.phoneTab, p.desktopTab} {
		wantEnded(t, h, w, notice, "web url changed")
	}
	wantCut(t, phoneWS)
	started := strings.Count(h.Log(), `msg="ssh session started"`)
	_ = opening.TrySend(wire.WorkspaceRequest{Open: "web1", Cols: 80, Rows: 24})
	wantCut(t, opening)
	for _, m := range opening.Messages() {
		if strings.Contains(m, `"tab":`) {
			t.Errorf("the page at the old address was answered %s, want its open to open nothing", m)
		}
	}
	if n := strings.Count(h.Log(), `msg="ssh session started"`); n != started {
		t.Errorf("the open at the old address started %d SSH Sessions", n-started)
	}
	page := p.phone.TerminalPage()
	page.Host, page.Origin = strings.TrimPrefix(moved, "https://"), moved
	wantRefused(t, page.Resume(p.phoneOnDesktop), wire.TerminalResumeRefused, "the Web URL changed")
}
