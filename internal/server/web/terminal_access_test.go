package web_test

import (
	"encoding/json/v2"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/shared/api"
	"github.com/cieons/koko/internal/shared/timing"
	"github.com/cieons/koko/internal/shared/wire"
)

// openLiveAndDetached opens two terminals to the Host named hostName from
// the browser b: one it keeps on its WebSocket, and one whose WebSocket
// drops, which the Gateway holds Detached.
func openLiveAndDetached(t *testing.T, h *harness.Harness, b *harness.Browser, hostName string) (live, detached *harness.WebTerminal) {
	t.Helper()
	live = b.TerminalPage().Open(hostName, 80, 24)
	live.Type([]byte("echo ready; exec cat\n"))
	live.WaitOutput("ready\n")
	detached = b.TerminalPage().Open(hostName, 80, 24)
	detached.Drop()
	waitLog(t, h, `msg="ssh session detached" tunnel=`+detached.ID+` `)
	return live, detached
}

// wantNotice fails the test unless the live terminal ended with notice, on a
// line of its own and as its stream's close, and the Detached one ended
// too, each logged with reason.
func wantNotice(t *testing.T, h *harness.Harness, live, detached *harness.WebTerminal, notice, reason string) {
	t.Helper()
	wantPeerClosed(t, live.WaitEnd(), notice)
	if out := string(live.Output()); !strings.HasSuffix(out, "\r\nkoko: "+notice+"\r\n") {
		t.Errorf("the live terminal got %q, want it to end with the notice on a line of its own", out)
	}
	for _, w := range []*harness.WebTerminal{live, detached} {
		waitLog(t, h, `msg="ssh session ended" tunnel=`+w.ID+` client=browser .* reason="`+regexp.QuoteMeta(reason)+`"\n`)
	}
}

// wantKept fails the test unless the terminal w still echoes what is typed.
func wantKept(t *testing.T, w *harness.WebTerminal, line string) {
	t.Helper()
	w.Type([]byte(line + "\n"))
	w.WaitOutput(line + "\n")
}

// signedOut is the web front's refusal of a page whose Sign-in has ended,
// which comes before the Gateway reads the first message.
const signedOut = "this browser is not signed in, or its Sign-in has ended: sign in again"

// Each way a Sign-in ends ends its terminals at once, the live one with its
// notice and the Detached one alike, and a later resume of the Detached one
// is refused: the page is no longer signed in. Changing the Password keeps
// the terminal of the Sign-in that changed it.
func TestEndingASignInEndsItsTerminals(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name string
		// end ends bob's Sign-in in b, and returns any terminal of his
		// that must go on.
		end            func(t *testing.T, h *harness.Harness, b *harness.Browser) (kept *harness.WebTerminal)
		notice, reason string
	}{
		{
			name: "sign-out",
			end: func(t *testing.T, _ *harness.Harness, b *harness.Browser) *harness.WebTerminal {
				if a := b.Do(http.MethodPost, api.AuthSignOutPath, "{}"); a.StatusCode != http.StatusOK {
					t.Fatalf("signing out answered %s", a.Status)
				}
				return nil
			},
			notice: "this browser signed out; this SSH Session is closed.",
			reason: "signed out",
		},
		{
			name: "revoke",
			end: func(t *testing.T, h *harness.Harness, _ *harness.Browser) *harness.WebTerminal {
				h.Koko("signin", "revoke", signInID(t, h, "bob")).WantSuccess(t)
				return nil
			},
			notice: "access to web1 was withdrawn: this browser's Sign-in was revoked; this SSH Session is closed.",
			reason: "signin revoked",
		},
		{
			name: "password changed",
			end: func(t *testing.T, h *harness.Harness, _ *harness.Browser) *harness.WebTerminal {
				other := signInAgain(t, h, "bob")
				own := other.TerminalPage().Open("web1", 80, 24)
				own.Type([]byte("exec cat\n"))
				status, body := other.Post(api.AuthPasswordPath, api.AuthPasswordRequest{Current: harness.Password, Password: "the changed password of bob"})
				if status != http.StatusOK {
					t.Fatalf("changing the Password: %d %s", status, body)
				}
				return own
			},
			notice: "access to web1 was withdrawn: the Password was changed; this SSH Session is closed.",
			reason: "password changed",
		},
		{
			name: "password removed",
			end: func(t *testing.T, h *harness.Harness, _ *harness.Browser) *harness.WebTerminal {
				h.Koko("password", "rm", "bob").WantSuccess(t)
				return nil
			},
			notice: "access to web1 was withdrawn: the Password was removed; this SSH Session is closed.",
			reason: "password removed",
		},
		{
			name: "user removed",
			end: func(t *testing.T, h *harness.Harness, _ *harness.Browser) *harness.WebTerminal {
				h.Koko("user", "remove", "bob").WantSuccess(t)
				return nil
			},
			notice: "access to web1 was withdrawn: bob was Removed; this SSH Session is closed.",
			reason: "user removed",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			h := startTerminals(t)
			terminalHost(t, h, "web1", []string{"space=bob"})
			b := h.SignIn("bob")
			live, detached := openLiveAndDetached(t, h, b, "web1")

			kept := c.end(t, h, b)
			wantNotice(t, h, live, detached, c.notice, c.reason)
			wantRefused(t, b.TerminalPage().Resume(detached), api.CodeSignInRequired.Name, signedOut)
			if kept != nil {
				wantKept(t, kept, "kept")
			}
		})
	}
}

// A change that takes a Host out of a Sign-in's reach ends its terminals
// to it as it ends a device's SSH Sessions, the live one with its notice and
// the Detached one alike, and a later resume of the Detached one is refused
// saying why: bob taken out of the Space, the Host moved to a Space he
// cannot act in, Retired, or its Relay sent back to Registered.
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
			b := h.SignIn("bob")
			live, detached := openLiveAndDetached(t, h, b, "web1")

			for _, args := range c.change {
				h.Koko(args...).WantSuccess(t)
			}
			wantNotice(t, h, live, detached, c.notice, c.reason)
			wantRefused(t, b.TerminalPage().Resume(detached), wire.TerminalResumeRefused, strings.TrimSuffix(c.notice, "; this SSH Session is closed."))
		})
	}
}

// A change that keeps a Sign-in's access ends none of its terminals:
// renaming the Host, changing its address, port, Note and Attributes,
// moving it to another Space bob acts in, revoking another of bob's
// Sign-ins and taking another User out of the Space.
func TestAChangeThatKeepsAccessEndsNoTerminal(t *testing.T) {
	t.Parallel()
	h := startTerminals(t)
	h.Koko("user", "invite", "carol").WantSuccess(t)
	for _, s := range []string{"ops", "dev"} {
		h.Koko("space", "add", s).WantSuccess(t)
		h.Koko("space", "member", "add", s, "bob", "carol").WantSuccess(t)
	}
	terminalHost(t, h, "web1", []string{"space=ops"})
	b := h.SignIn("bob")
	other := signInAgain(t, h, "bob")
	status, body := other.Send(api.UserCurrent, []byte("{}"))
	var current api.UserCurrentResponse
	if err := json.Unmarshal(body, &current); err != nil || status != http.StatusOK || current.SignIn == nil {
		t.Fatalf("user.current of bob's other Sign-in: %d %s", status, body)
	}
	h.SignIn("carol")
	w := b.TerminalPage().Open("web1", 80, 24)
	w.Type([]byte("exec cat\n"))
	otherID := current.SignIn.ID

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
	wantKept(t, w, "after the check")
	if log := h.Log(); strings.Contains(log, `msg="ssh session ended"`) {
		t.Errorf("the log says an SSH Session ended:\n%s", log)
	}
}

// A Sign-in that expires, 7 days after it was last used, publishes nothing:
// the Gateway's sweep finds it on the koko server's clock and ends its
// terminals, the live one with its notice and the Detached one alike, and a
// later resume is refused. A Sign-in whose terminal Resumed meanwhile, and
// one that opened a terminal meanwhile, each of which moved its Last used
// time, go on.
func TestAnExpiredSignInsTerminalsEnd(t *testing.T) {
	t.Parallel()
	h := startTerminals(t, harness.Timings(timing.Timings{SignInCheck: 20 * time.Millisecond}))
	terminalHost(t, h, "web1", []string{"space=bob"})
	b := h.SignIn("bob")
	live, detached := openLiveAndDetached(t, h, b, "web1")
	owner := h.SignIn(harness.Owner)
	used := owner.TerminalPage().Open("web1", 80, 24)
	used.Type([]byte("exec cat\n"))
	other := signInAgain(t, h, harness.Owner)

	h.Advance(6 * 24 * time.Hour)
	used.Drop()
	if a := owner.TerminalPage().Resume(used); a.Refused != "" {
		t.Fatalf("the resume was refused: %s", deref(a.Message))
	}
	opened := other.TerminalPage().Open("web1", 80, 24)
	opened.Type([]byte("exec cat\n"))
	h.Advance(2 * 24 * time.Hour)
	wantNotice(t, h, live, detached, "access to web1 was withdrawn: this browser's Sign-in has expired; this SSH Session is closed.", "signin expired")
	// The sweep that ended bob's read the Owner's Sign-ins too.
	wantKept(t, used, "resumed meanwhile")
	wantKept(t, opened, "opened meanwhile")
	wantRefused(t, b.TerminalPage().Resume(detached), api.CodeSignInRequired.Name, signedOut)
}

// A Web URL change ends every terminal that came through the web listener,
// the live one with its notice and the Detached one alike, and a resume at
// the new address is refused saying why; setting the same Web URL again
// ends none.
func TestAWebURLChangeEndsTheListenersTerminals(t *testing.T) {
	t.Parallel()
	h := startTerminals(t)
	terminalHost(t, h, "web1", []string{"space=bob"})
	b := h.SignIn("bob")
	live, detached := openLiveAndDetached(t, h, b, "web1")

	h.Koko("server", "set", "web_url="+harness.WebURL).WantSuccess(t)
	wantKept(t, live, "after the same Web URL")

	// A page at the old address whose WebSocket the web listener took
	// before the change, and which sends its open after it.
	opening, _, err := b.TerminalPage().Dial()
	if err != nil {
		t.Fatal(err)
	}

	const moved = "https://koko.example.org"
	h.Koko("server", "set", "web_url="+moved).WantSuccess(t)
	const notice = "the Web URL changed; this SSH Session is closed. Sign in at the new address."
	wantNotice(t, h, live, detached, notice, "web url changed")
	p := b.TerminalPage()
	p.Host, p.Origin = strings.TrimPrefix(moved, "https://"), moved
	wantRefused(t, p.Resume(detached), wire.TerminalResumeRefused, "the Web URL changed")

	if err := opening.Write(t.Context(), websocket.MessageText, []byte(`{"protocol": 1, "open": "web1", "cols": 80, "rows": 24}`)); err != nil {
		t.Fatal(err)
	}
	_, msg, err := opening.Read(t.Context())
	var a wire.TerminalAnswer
	if err == nil {
		err = json.Unmarshal(msg, &a)
	}
	if err != nil {
		t.Fatalf("read the answer to the open sent after the change: %v", err)
	}
	wantRefused(t, a, wire.TerminalAccessRefused, "the Web URL changed; this SSH Session did not start. Sign in at the new address.")
}
