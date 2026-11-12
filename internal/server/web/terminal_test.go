package web_test

import (
	"bytes"
	"crypto/rand"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"golang.org/x/crypto/ssh"

	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/harness/sshhost"
	"github.com/cieons/koko/internal/shared/resumable"
	"github.com/cieons/koko/internal/shared/timing"
	"github.com/cieons/koko/internal/shared/wire"
)

// hostPassword is the in-process Hosts' password, which Enroll logs in
// with.
const hostPassword = "correct horse battery staple"

// startTerminals starts a koko server with its web listener on and its Web
// URL set to harness.WebURL, with opts, and invites the Member bob.
func startTerminals(t *testing.T, opts ...harness.Option) *harness.Harness {
	t.Helper()
	h := harness.Start(t, append([]harness.Option{harness.WebListen("127.0.0.1:0")}, opts...)...)
	h.Browser() // sets the Web URL
	h.Koko("user", "invite", "bob").WantSuccess(t)
	return h
}

// terminalHost records an in-process Host started with opts as the Host
// named hostName, as the Local caller, with pairs, and Enrolls it, so the
// Gateway logs in to it with the koko key.
func terminalHost(t *testing.T, h *harness.Harness, hostName string, pairs []string, opts ...sshhost.Option) *sshhost.Host {
	t.Helper()
	host := sshhost.Start(t, append([]sshhost.Option{sshhost.Password(hostPassword)}, opts...)...)
	h.Koko(append([]string{"host", "add", hostName, "address=" + host.Address(), "port=" + strconv.Itoa(host.Port()), "account=" + sshhost.Account}, pairs...)...).WantSuccess(t)
	h.KokoStdin(hostPassword, "host", "enroll", hostName, "--password-stdin").WantSuccess(t)
	return host
}

// waitLog waits until the koko server's log holds a line matching pattern,
// and returns its submatches. It fails the test after 10 s.
func waitLog(t *testing.T, h *harness.Harness, pattern string) []string {
	t.Helper()
	re := regexp.MustCompile(pattern)
	deadline := time.Now().Add(10 * time.Second)
	for {
		if m := re.FindStringSubmatch(h.Log()); m != nil {
			return m
		}
		if time.Now().After(deadline) {
			t.Fatalf("the log holds no line matching %s within 10 s; it holds:\n%s", pattern, h.Log())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// wantRefused fails the test unless a refuses with code and message.
func wantRefused(t *testing.T, a wire.TerminalAnswer, code, message string) {
	t.Helper()
	if a.Refused != code || a.Message == nil || *a.Message != message {
		got := "<none>"
		if a.Message != nil {
			got = *a.Message
		}
		t.Errorf("answered %q with %q, want %q with %q", a.Refused, got, code, message)
	}
}

// wantPeerClosed fails the test unless err is the koko server's close of
// the stream, with reason.
func wantPeerClosed(t *testing.T, err error, reason string) {
	t.Helper()
	if e, ok := errors.AsType[*resumable.PeerClosedError](err); !ok || e.Reason != reason {
		t.Errorf("the stream ended with %v, want the koko server's close saying %q", err, reason)
	}
}

// Before the upgrade, the web listener refuses a GET that is no WebSocket
// with a 426 page, and a WebSocket from another Origin, from none, or from
// the Web URL on another scheme with a 403 page, before anything else is
// read; no Host is dialled.
func TestTheTerminalRefusesBeforeTheUpgrade(t *testing.T) {
	t.Parallel()
	h := startTerminals(t)
	host := terminalHost(t, h, "web1", nil)
	conns := host.Conns()

	a := send(t, h, request{method: http.MethodGet, path: "/terminal/socket", host: "koko.example.com", origin: webURL})
	wantPage(t, a, http.StatusUpgradeRequired, "web terminal's WebSocket")
	wantSecurityHeaders(t, a.Header)
	if a.Header.Get("Upgrade") != "websocket" {
		t.Errorf("the 426 answer's Upgrade header is %q, want websocket", a.Header.Get("Upgrade"))
	}

	for _, origin := range []string{"https://evil.example", "", "http://koko.example.com", "https://koko.example.com:8443"} {
		p := h.SignIn(harness.Owner).TerminalPage()
		p.Origin = origin
		c, refused, err := p.Dial()
		if err == nil {
			_ = c.CloseNow()
			t.Errorf("a WebSocket from the Origin %q was accepted", origin)
			continue
		}
		if refused.Response == nil || refused.StatusCode != http.StatusForbidden || refused.Header.Get("Content-Security-Policy") == "" ||
			!strings.Contains(refused.Body, "came from no page of this koko server") {
			t.Errorf("a WebSocket from the Origin %q was refused with %+v, want a 403 page with the listener's headers", origin, refused)
		}
	}
	if got := host.Conns(); got != conns {
		t.Errorf("the refused WebSockets dialled the Host %d times", got-conns)
	}
}

// After the upgrade, the first message's answer refuses a page out of date
// (reload_page), a browser not signed in and one whose Sign-in has ended
// (signin_required), then the WebSocket closes with status 1008; a
// malformed first message closes it with 1008 alone. No Host is dialled for
// any of them.
func TestTheTerminalRefusesInItsFirstAnswer(t *testing.T) {
	t.Parallel()
	h := startTerminals(t)
	host := terminalHost(t, h, "web1", nil)
	conns := host.Conns()
	b := h.SignIn(harness.Owner)
	open := wire.TerminalFirst{Protocol: wire.TerminalProtocol, Open: "web1", Cols: 80, Rows: 24}

	stale := open
	stale.Protocol = wire.TerminalProtocol + 1
	_, a, status := b.TerminalPage().Exchange(stale)
	wantRefused(t, a, wire.TerminalReloadPage, "this page is out of date: reload it")
	if status != websocket.StatusPolicyViolation {
		t.Errorf("reload_page closed with %d, want 1008", status)
	}

	_, a, status = h.Browser().TerminalPage().Exchange(open)
	wantRefused(t, a, "signin_required", "this browser is not signed in, or its Sign-in has ended: sign in again")
	if status != websocket.StatusPolicyViolation {
		t.Errorf("signin_required closed with %d, want 1008", status)
	}
	out := signInAgain(t, h, harness.Owner)
	page := out.TerminalPage()
	out.Do(http.MethodPost, "/auth/signout", "{}")
	_, a, _ = page.Exchange(open)
	wantRefused(t, a, "signin_required", "this browser is not signed in, or its Sign-in has ended: sign in again")

	for name, msg := range map[string]string{
		"not JSON":          "open web1",
		"two things":        `{"protocol": 1, "open": "web1", "cols": 80, "rows": 24, "end": "` + strings.Repeat("0", 32) + `"}`,
		"a size of 0":       `{"protocol": 1, "open": "web1", "cols": 0, "rows": 24}`,
		"an id not hex":     `{"protocol": 1, "resume": "` + strings.Repeat("g", 32) + `", "offset": 0}`,
		"over 4 KiB":        `{"protocol": 1, "open": "` + strings.Repeat("w", 5000) + `", "cols": 80, "rows": 24}`,
		"binary, not text":  "",
		"an id of 31 chars": `{"protocol": 1, "end": "` + strings.Repeat("0", 31) + `"}`,
	} {
		c, _, err := b.TerminalPage().Dial()
		if err != nil {
			t.Fatal(err)
		}
		if msg == "" {
			err = c.Write(t.Context(), websocket.MessageBinary, []byte(`{"protocol": 1, "open": "web1", "cols": 80, "rows": 24}`))
		} else {
			err = c.Write(t.Context(), websocket.MessageText, []byte(msg))
		}
		if err != nil {
			t.Fatal(err)
		}
		if got := harness.CloseStatus(t.Context(), c); got != websocket.StatusPolicyViolation {
			t.Errorf("%s: closed with %d, want 1008 and no answer", name, got)
		}
	}
	if got := host.Conns(); got != conns {
		t.Errorf("the refused first messages dialled the Host %d times", got-conns)
	}
}

// A WebSocket that sends no first message within timing.FirstMessage is
// closed with status 1008.
func TestATerminalWithoutAFirstMessageIsClosed(t *testing.T) {
	t.Parallel()
	// The first message never comes, so its wait is shortened.
	h := startTerminals(t, harness.Timings(timing.Timings{FirstMessage: 300 * time.Millisecond}))
	c, _, err := h.SignIn(harness.Owner).TerminalPage().Dial()
	if err != nil {
		t.Fatal(err)
	}
	if got := harness.CloseStatus(t.Context(), c); got != websocket.StatusPolicyViolation {
		t.Errorf("a WebSocket without a first message closed with %d, want 1008", got)
	}
}

// An open runs a shell on the Host as its Host Account, logged in with the
// koko key, on a session channel with a pty: TERM xterm-256color, the
// page's size, pixel sizes 0 and IUTF8 on. 4 MiB pass each way byte for
// byte, a resize becomes a window-change, and the shell's exit status, or
// the signal that ended it, comes as its record before the stream's close.
func TestATerminalRunsAShellOnThePty(t *testing.T) {
	t.Parallel()
	h := startTerminals(t)
	host := terminalHost(t, h, "web1", nil)
	logins := len(host.Logins())
	p := h.SignIn(harness.Owner).TerminalPage()
	w := p.Open("web1", 120, 36)

	if got := host.Logins()[logins:]; len(got) != 1 || got[0].Account != sshhost.Account || got[0].Method != "publickey" || !got[0].OK {
		t.Errorf("the Host got the logins %+v, want one with the koko key as %s", got, sshhost.Account)
	}
	var pty struct {
		Term                         string
		Columns, Rows, Width, Height uint32
		Modes                        string
	}
	var kinds []string
	for _, r := range host.Requests() {
		if !r.Channel {
			kinds = append(kinds, r.Type)
		}
		if r.Type == "pty-req" {
			if err := ssh.Unmarshal(r.Payload, &pty); err != nil {
				t.Fatal(err)
			}
		}
	}
	if !strings.Contains(strings.Join(kinds, " "), "pty-req shell") {
		t.Errorf("the Host got the requests %v, want a pty-req then a shell", kinds)
	}
	if pty.Term != "xterm-256color" || pty.Columns != 120 || pty.Rows != 36 || pty.Width != 0 || pty.Height != 0 || pty.Modes != "\x2a\x00\x00\x00\x01\x00" {
		t.Errorf("the pty-req asked for %+v, want xterm-256color at 120x36, pixel sizes 0 and IUTF8", pty)
	}

	w.Type([]byte("echo ready; exec cat\n"))
	w.WaitOutput("ready\n")
	data := make([]byte, 4<<20)
	_, _ = rand.Read(data)
	typed := make(chan error, 1)
	go func() {
		_, err := w.Write(data)
		typed <- err
	}()
	if out := w.WaitOutputLen(len("ready\n") + len(data)); !bytes.Equal(out[len("ready\n"):], data) {
		t.Error("the 4 MiB cat echoed are not those sent")
	}
	if err := <-typed; err != nil {
		t.Errorf("send 4 MiB: %v", err)
	}

	w.Resize(100, 40)
	var change struct{ Columns, Rows, Width, Height uint32 }
	deadline := time.Now().Add(10 * time.Second)
	for change.Columns == 0 && time.Now().Before(deadline) {
		for _, r := range host.Requests() {
			if r.Type == "window-change" {
				_ = ssh.Unmarshal(r.Payload, &change)
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if change.Columns != 100 || change.Rows != 40 || change.Width != 0 || change.Height != 0 {
		t.Errorf("the resize reached the Host as %+v, want a window-change to 100x40, pixel sizes 0", change)
	}

	exits := p.Open("web1", 80, 24)
	exits.Type([]byte("echo bye; exit 3\n"))
	wantPeerClosed(t, exits.WaitEnd(), "")
	if status, signal := exits.Exit(); status == nil || *status != 3 || signal != "" || string(exits.Output()) != "bye\n" {
		t.Errorf("the shell that exited 3 sent %q, then the status %v and the signal %q", exits.Output(), status, signal)
	}
	waitLog(t, h, `msg="ssh session ended" tunnel=`+exits.ID+` client=browser .* exit_status=3 reason="shell ended"\n`)

	killed := p.Open("web1", 80, 24)
	killed.Type([]byte("kill -KILL $$\n"))
	wantPeerClosed(t, killed.WaitEnd(), "")
	if status, signal := killed.Exit(); status != nil || signal != "KILL" {
		t.Errorf("the shell killed sent the status %v and the signal %q, want KILL alone", status, signal)
	}
}

// Each refusal of an open is its answer, with the code and the banner a
// Tunnel's client gets, without "koko: ": an unseen Host and a Retired one
// get an unknown name's bytes; each is logged with client=browser.
func TestEachRefusalOfAnOpenIsItsAnswer(t *testing.T) {
	t.Parallel()
	h := startTerminals(t, harness.Timings(timing.Timings{HostConnectWait: time.Millisecond}))
	_, deadPort, err := net.SplitHostPort(harness.FreeAddr(t))
	if err != nil {
		t.Fatal(err)
	}
	web1 := terminalHost(t, h, "web1", nil)
	h.Koko("host", "add", "registered", "address=203.0.113.10", "account=deploy").WantSuccess(t)
	terminalHost(t, h, "down", nil)
	h.Koko("host", "set", "down", "port="+deadPort).WantSuccess(t)
	terminalHost(t, h, "rebuilt", nil)
	rebuilt := sshhost.Start(t, sshhost.Password(hostPassword))
	h.Koko("host", "set", "rebuilt", "port="+strconv.Itoa(rebuilt.Port())).WantSuccess(t)
	keyless := terminalHost(t, h, "keyless", nil)
	if err := os.WriteFile(filepath.Join(keyless.Home(), ".ssh", "authorized_keys"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	terminalHost(t, h, "nopty", nil, sshhost.RefuseRequests("pty-req"))
	terminalHost(t, h, "noshell", nil, sshhost.RefuseRequests("shell"))
	terminalHost(t, h, "garbled", nil)
	h.Koko("host", "set", "garbled", "port="+strconv.Itoa(notSSH(t))).WantSuccess(t)
	terminalHost(t, h, "gw", nil)
	terminalHost(t, h, "behind", []string{"relay=gw"})
	h.Koko("host", "set", "gw", "account=ops").WantSuccess(t)
	terminalHost(t, h, "retired", nil)
	h.Koko("host", "retire", "retired").WantSuccess(t)

	owner := h.SignIn(harness.Owner).TerminalPage()
	bob := h.SignIn("bob").TerminalPage()
	refuse := func(p *harness.TerminalPage, host, code, message string) {
		t.Helper()
		w, a := p.TryOpen(host, 80, 24)
		if w != nil {
			t.Errorf("opening %s was answered with an SSH Session", host)
			return
		}
		wantRefused(t, a, code, message)
	}
	refuse(owner, "nosuch", wire.TerminalUnknownHost, `no Host is named "nosuch".`)
	conns := web1.Conns()
	refuse(bob, "web1", wire.TerminalUnknownHost, `no Host is named "web1".`)
	if web1.Conns() != conns {
		t.Error("an unseen Host was dialled")
	}
	refuse(owner, "retired", wire.TerminalUnknownHost, `no Host is named "retired".`)
	refuse(owner, "registered", wire.TerminalHostRegistered, "registered is Registered: koko logs in to it only once it is Enrolled, with koko host enroll registered.")
	refuse(owner, "behind", wire.TerminalRelayRegistered, "behind's Relay gw is Registered: koko passes through it only once it is Enrolled, with koko host enroll gw.")
	refuse(owner, "down", wire.TerminalHostUnreachable, "down is down or unreachable: koko could not connect to it; this SSH Session did not start.")
	if _, a := owner.TryOpen("rebuilt", 80, 24); a.Refused != wire.TerminalHostKeyMismatch || !strings.HasPrefix(*a.Message, "the host key of rebuilt does not match the one pinned at Enroll") {
		t.Errorf("a rebuilt Host's open was refused %+v, want host_key_mismatch", a)
	}
	refuse(owner, "keyless", wire.TerminalLoginRefused, `keyless refused the koko key for its Host Account "deploy"; this SSH Session did not start.`)
	refuse(owner, "nopty", wire.TerminalShellRefused, `nopty refused a shell with a terminal for its Host Account "deploy"; this SSH Session did not start.`)
	refuse(owner, "noshell", wire.TerminalShellRefused, `noshell refused a shell with a terminal for its Host Account "deploy"; this SSH Session did not start.`)
	refuse(owner, "garbled", wire.TerminalHostConnectionFailed, "koko could not connect to garbled; this SSH Session did not start.")

	for _, reason := range []string{"unknown_host", `"host registered"`, `"relay registered"`, `"host unreachable"`, `"host key mismatch"`, `"login refused"`, `"shell refused"`, `"host connection failed"`} {
		waitLog(t, h, `msg="ssh session refused" tunnel=[0-9a-f]{32} client=browser name=\S+ signin=signin_\w+ addr=192\.0\.2\.10 reason=`+reason)
	}
}

// notSSH listens on a loopback port where a server answers SSH's version
// exchange, then sends a packet no SSH client takes, and holds the
// connection until the client leaves, so the client fails on something
// other than the network; it returns the port.
func notSSH(t *testing.T) int {
	t.Helper()
	var lc net.ListenConfig
	ln, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				_, _ = c.Write(append([]byte("SSH-2.0-not-koko\r\n"), bytes.Repeat([]byte{0xff}, 64)...))
				_, _ = io.Copy(io.Discard, c)
				_ = c.Close()
			}()
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port
}

// A Sign-in holds at most 16 SSH Sessions at once: the 17th is refused
// without a connection to the Host, until one of the 16 ends, as a
// reloaded page's end of an old one does before it is answered. Another
// Sign-in of the same User holds its own.
func TestASignInHoldsAtMost16SSHSessions(t *testing.T) {
	t.Parallel()
	h := startTerminals(t)
	host := terminalHost(t, h, "web1", nil)
	p := h.SignIn(harness.Owner).TerminalPage()
	open := make([]*harness.WebTerminal, 16)
	for i := range open {
		open[i] = p.Open("web1", 80, 24)
	}
	conns := host.Conns()
	_, a := p.TryOpen("web1", 80, 24)
	wantRefused(t, a, wire.TerminalSSHSessionLimit, "this Sign-in already has 16 SSH Sessions open, the most koko allows; this SSH Session did not start.")
	if host.Conns() != conns {
		t.Error("the 17th SSH Session dialled the Host")
	}
	waitLog(t, h, `msg="ssh session refused" tunnel=[0-9a-f]{32} client=browser name=web1 signin=signin_\w+ addr=192\.0\.2\.10 reason="ssh session limit"\n`)

	signInAgain(t, h, harness.Owner).TerminalPage().Open("web1", 80, 24).Type([]byte("echo other\n"))

	// Its place is free once it has left the koko server's registry, a
	// moment after its end is logged.
	open[0].Close()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if w, _ := p.TryOpen("web1", 80, 24); w != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no SSH Session opened within 10 s of one of the 16 closing")
		}
		time.Sleep(10 * time.Millisecond)
	}

	if a, _ := p.End(open[1].ID); !a.Ended {
		t.Fatalf("the end answered %+v", a)
	}
	p.Open("web1", 80, 24)
}

// signInAgain signs a new browser in as user, whose Password h.SignIn set,
// so that the User's other Sign-ins go on.
func signInAgain(t *testing.T, h *harness.Harness, user string) *harness.Browser {
	t.Helper()
	b := h.Browser()
	if status, body := signIn(b, user, harness.Password); status != http.StatusOK {
		t.Fatalf("sign in again as %s: %d %s", user, status, body)
	}
	return b
}

// A resume from another address moves an SSH Session off a WebSocket the
// koko server still holds, which a stalled path keeps, and which is never
// Detached: the last attach wins. A Detached SSH Session Resumes on a new
// WebSocket too. Across both, what the shell sent and the keys typed
// meanwhile arrive once. Another Sign-in's resume of its id is refused
// with an empty message, and an id the koko server never gave alike.
func TestATerminalResumesByteForByte(t *testing.T) {
	t.Parallel()
	h := startTerminals(t)
	terminalHost(t, h, "web1", nil)
	b := h.SignIn(harness.Owner)
	f := harness.Forward(t, h.WebAddress())
	p := b.TerminalPage()
	p.Addr = f.Addr()
	w := p.Open("web1", 80, 24)
	w.Type([]byte("echo ready; exec cat\n"))
	w.WaitOutput("ready\n")

	f.Partition()
	w.Type([]byte("typed while cut off\n"))
	b.Address = "198.51.100.7"
	if a := b.TerminalPage().Resume(w); a.Refused != "" {
		t.Fatalf("the resume was refused: %s", deref(a.Message))
	}
	waitLog(t, h, `msg="ssh session resumed" tunnel=`+w.ID+` client=browser signin=signin_\w+ addr=198\.51\.100\.7\n`)
	w.Type([]byte("after\n"))
	if out := w.WaitOutput("after\n"); string(out) != "ready\ntyped while cut off\nafter\n" {
		t.Errorf("across the Resume the terminal got %q, want each line once", out)
	}
	if strings.Contains(h.Log(), `msg="ssh session detached" tunnel=`+w.ID) {
		t.Error("a resume onto another WebSocket Detached the SSH Session")
	}

	w.Drop()
	waitLog(t, h, `msg="ssh session detached" tunnel=`+w.ID+` client=browser signin=signin_\w+\n`)
	w.Type([]byte("typed while Detached\n"))
	if a := b.TerminalPage().Resume(w); a.Refused != "" {
		t.Fatalf("the resume was refused: %s", deref(a.Message))
	}
	w.Type([]byte("again\n"))
	if out := w.WaitOutput("again\n"); string(out) != "ready\ntyped while cut off\nafter\ntyped while Detached\nagain\n" {
		t.Errorf("across the second Resume the terminal got %q, want each line once", out)
	}

	other := signInAgain(t, h, harness.Owner).TerminalPage()
	_, a, status := other.Exchange(wire.TerminalFirst{Protocol: wire.TerminalProtocol, Resume: w.ID, Offset: 0})
	wantRefused(t, a, wire.TerminalResumeRefused, "")
	if status != websocket.StatusPolicyViolation {
		t.Errorf("the refused resume closed with %d, want 1008", status)
	}
	waitLog(t, h, `msg="ssh session resume refused" tunnel=`+w.ID+` client=browser signin=signin_\w+ addr=\S+ reason="unknown tunnel"\n`)
	_, a, _ = other.Exchange(wire.TerminalFirst{Protocol: wire.TerminalProtocol, Resume: strings.Repeat("ab", 16), Offset: 0})
	wantRefused(t, a, wire.TerminalResumeRefused, "")
	w.Type([]byte("still\n"))
	w.WaitOutput("still\n")
}

// The page's close ends the SSH Session as the client's close, and a later
// page's end does too, answered once it has ended, a busy shell's whose page
// is gone included; another Sign-in's end ends nothing, answered alike.
func TestATerminalEnds(t *testing.T) {
	t.Parallel()
	// A call ends each terminal here, so the grace keeps the product's
	// timing: shortened, it could end a Detached one before the call comes
	// (#187).
	h := startTerminals(t)
	terminalHost(t, h, "web1", nil)
	b := h.SignIn(harness.Owner)

	closed := b.TerminalPage().Open("web1", 80, 24)
	closed.Close()
	waitLog(t, h, `msg="ssh session ended" tunnel=`+closed.ID+` client=browser user=alice signin=signin_\w+ host=web1 host_id=host_\w+ duration=\S+ bytes_from_device=\d+ bytes_to_device=\d+ reason="client closed"\n`)

	kept := b.TerminalPage().Open("web1", 80, 24)
	a, status := signInAgain(t, h, harness.Owner).TerminalPage().End(kept.ID)
	if !a.Ended || status != websocket.StatusNormalClosure {
		t.Errorf("another Sign-in's end answered %+v and closed with %d, want ended and 1000", a, status)
	}
	kept.Type([]byte("echo kept\n"))
	kept.WaitOutput("kept\n")

	a, status = b.TerminalPage().End(kept.ID)
	if !a.Ended || status != websocket.StatusNormalClosure {
		t.Errorf("the end answered %+v and closed with %d, want ended and 1000", a, status)
	}
	if !regexp.MustCompile(`msg="ssh session ended" tunnel=` + kept.ID + ` .* reason="client closed"\n`).MatchString(h.Log()) {
		t.Errorf("the end was answered before the SSH Session ended as the client's close:\n%s", h.Log())
	}

	// A shell that keeps writing fills the stream's buffer once its page
	// is gone: an end ends its SSH Session all the same.
	ended := detachBusy(t, h, b)
	if a, _ := b.TerminalPage().End(ended.ID); !a.Ended {
		t.Errorf("the end of a busy terminal answered %+v", a)
	}
	waitLog(t, h, `msg="ssh session ended" tunnel=`+ended.ID+` .* reason="client closed"\n`)
}

// An SSH Session Detached for its grace ends, a busy shell's too, and its
// resume is then refused with an empty message.
func TestADetachedTerminalEndsAtItsGrace(t *testing.T) {
	t.Parallel()
	// Nothing but the grace ends these terminals, so it is shortened.
	h := startTerminals(t, harness.Timings(timing.Timings{BrowserGrace: 200 * time.Millisecond}))
	terminalHost(t, h, "web1", nil)
	b := h.SignIn(harness.Owner)

	held := b.TerminalPage().Open("web1", 80, 24)
	held.Drop()
	waitLog(t, h, `msg="ssh session ended" tunnel=`+held.ID+` .* reason="grace over"\n`)
	wantRefused(t, b.TerminalPage().Resume(held), wire.TerminalResumeRefused, "")

	graced := detachBusy(t, h, b)
	waitLog(t, h, `msg="ssh session ended" tunnel=`+graced.ID+` .* reason="grace over"\n`)
}

// detachBusy opens a terminal on web1 whose shell keeps writing, drops its
// page once 64 KiB of the output has reached it, and returns once the SSH
// Session is Detached, while the shell's output fills the stream's buffer.
func detachBusy(t *testing.T, h *harness.Harness, b *harness.Browser) *harness.WebTerminal {
	t.Helper()
	w := b.TerminalPage().Open("web1", 80, 24)
	w.Type([]byte("yes\n"))
	w.WaitOutputLen(64 << 10)
	w.Drop()
	waitLog(t, h, `msg="ssh session detached" tunnel=`+w.ID+` `)
	return w
}

// The Gateway watches a terminal's WebSocket, as the page does: an idle one
// stays attached, since each end probes the other after a silence, which
// reaches the page; one whose path stalls is found dead ServerDead after
// the last byte that reached the koko server, and Detached; one cut is
// Detached at once. Each Resumes byte for byte.
func TestTheGatewayWatchesATerminalsWebSocket(t *testing.T) {
	// Not parallel: it holds a terminal idle for a set time.
	const probe, dead = 100 * time.Millisecond, time.Second
	h := startTerminals(t, harness.Timings(timing.Timings{ServerProbe: probe, ServerDead: dead}))
	terminalHost(t, h, "web1", nil)
	b := h.SignIn(harness.Owner)
	f := harness.Forward(t, h.WebAddress())
	p := b.TerminalPage()
	p.Addr, p.Probe = f.Addr(), probe/2
	w := p.Open("web1", 80, 24)
	w.Type([]byte("echo ready; exec cat\n"))
	w.WaitOutput("ready\n")

	time.Sleep(2*dead + dead/2)
	if strings.Contains(h.Log(), "ssh session detached") {
		t.Errorf("an idle terminal whose page probes was Detached:\n%s", h.Log())
	}
	passed := f.Stall()
	if quiet := time.Since(passed.Back); quiet > dead/2 {
		t.Errorf("the Gateway sent an idle page nothing for %v", quiet)
	}
	waitLog(t, h, `msg="ssh session detached" tunnel=`+w.ID+` `)
	if after := time.Since(passed.Out); after < dead {
		t.Errorf("the Gateway took the page for dead %v after its last byte, before %v", after, dead)
	}
	w.Type([]byte("while stalled\n"))
	if a := b.TerminalPage().Resume(w); a.Refused != "" {
		t.Fatalf("the resume was refused: %s", deref(a.Message))
	}
	w.WaitOutput("while stalled\n")

	detaches := func(n int) {
		t.Helper()
		line := regexp.MustCompile(`msg="ssh session detached" tunnel=` + w.ID + ` `)
		deadline := time.Now().Add(10 * time.Second)
		for len(line.FindAllString(h.Log(), -1)) < n {
			if time.Now().After(deadline) {
				t.Fatalf("the SSH Session was not Detached %d times within 10 s:\n%s", n, h.Log())
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	w.Drop()
	detaches(2)
	cut := harness.Forward(t, h.WebAddress())
	p.Addr = cut.Addr()
	if a := p.Resume(w); a.Refused != "" {
		t.Fatalf("the resume was refused: %s", deref(a.Message))
	}
	cut.CutWhen(func() bool { return true })
	w.Type([]byte("cut\n"))
	detaches(3)
	if a := b.TerminalPage().Resume(w); a.Refused != "" {
		t.Fatalf("the resume after the cut was refused: %s", deref(a.Message))
	}
	w.Type([]byte("after\n"))
	if out := w.WaitOutput("after\n"); string(out) != "ready\nwhile stalled\ncut\nafter\n" {
		t.Errorf("across the drops the terminal got %q, want each line once", out)
	}
}

// Stopping the koko server gives each web terminal the restart notice, as a
// line and as the stream's close, before the web listener closes: while a
// call in flight is still open on it. A page too stalled to take its notice
// is cut once StopNotice has passed, a terminal whose Host sits on its
// session's open ends, and the stop goes on.
func TestStoppingGivesEachTerminalTheRestartNotice(t *testing.T) {
	t.Parallel()
	h := startTerminals(t)
	terminalHost(t, h, "web1", nil)
	b := h.SignIn(harness.Owner)
	live := b.TerminalPage().Open("web1", 80, 24)
	f := harness.Forward(t, h.WebAddress())
	p := b.TerminalPage()
	p.Addr = f.Addr()
	stalled := p.Open("web1", 80, 24)
	stalled.Type([]byte("yes\n"))
	stalled.WaitOutputLen(1 << 20)
	f.Stall()

	loaded := terminalHost(t, h, "loaded", nil)
	loaded.HoldChannels()
	opens := len(loaded.Requests())
	opening, _, err := b.TerminalPage().Dial()
	if err != nil {
		t.Fatal(err)
	}
	if err := opening.Write(t.Context(), websocket.MessageText, []byte(`{"protocol": 1, "open": "loaded", "cols": 80, "rows": 24}`)); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for len(loaded.Requests()) == opens {
		if time.Now().After(deadline) {
			t.Fatal("the Host that holds its channels got no session open within 10 s")
		}
		time.Sleep(10 * time.Millisecond)
	}

	var d net.Dialer
	held, err := d.DialContext(t.Context(), "tcp", h.WebAddress())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = held.Close() }()
	if _, err := io.WriteString(held, "POST /auth/signin HTTP/1.1\r\nHost: koko.example.com\r\nOrigin: "+webURL+
		"\r\nContent-Type: application/json\r\nContent-Length: 2\r\n\r\n{"); err != nil {
		t.Fatal(err)
	}
	waitLog(t, h, `msg="ssh session request" tunnel=`+stalled.ID+` `)

	stopped := make(chan struct{})
	go func() {
		// A failed stop fails the test there, and this select below.
		h.Stop()
		close(stopped)
	}()
	const notice = "the koko server is restarting; this SSH Session has ended."
	wantPeerClosed(t, live.WaitEnd(), notice)
	if out := string(live.Output()); out != "\r\nkoko: "+notice+"\r\n" {
		t.Errorf("the terminal got %q, want the notice on a line of its own", out)
	}
	_ = held.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
	if _, err := held.Read(make([]byte, 1)); !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Errorf("the call in flight ended (%v) before the terminal had its notice", err)
	}
	if _, err := io.WriteString(held, "}"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-stopped:
	case <-time.After(20 * time.Second):
		t.Fatal("the koko server did not stop within 20 s")
	}
	if !regexp.MustCompile(`msg="ssh session ended" tunnel=` + stalled.ID + ` .* reason="server stopping"\n`).MatchString(h.Log()) {
		t.Errorf("the stalled terminal did not end as the koko server stopped:\n%s", h.Log())
	}
	if _, _, err := opening.Read(t.Context()); err == nil {
		t.Error("the terminal whose Host held its open got an answer as the koko server stopped")
	}
}

// Bytes from the page that are no terminal record end the SSH Session: the
// stream closes saying so.
func TestAMalformedRecordEndsTheTerminal(t *testing.T) {
	t.Parallel()
	h := startTerminals(t)
	terminalHost(t, h, "web1", nil)
	w := h.SignIn(harness.Owner).TerminalPage().Open("web1", 80, 24)
	w.Send([]byte{0x07, 0x00})
	err := w.WaitEnd()
	if e, ok := errors.AsType[*resumable.PeerClosedError](err); !ok || !strings.Contains(e.Reason, "malformed terminal record") {
		t.Errorf("the stream ended with %v, want a close saying the record is malformed", err)
	}
	waitLog(t, h, `msg="ssh session ended" tunnel=`+w.ID+` .* reason="malformed record"\n`)
}

// The log names a web terminal's SSH Session as the browser's: client,
// signin and the proxy's address of the page, and no line nor answer holds
// the cookie.
func TestTheTerminalLogLines(t *testing.T) {
	t.Parallel()
	h := startTerminals(t)
	terminalHost(t, h, "web1", nil)
	b := h.SignIn(harness.Owner)
	w := b.TerminalPage().Open("web1", 80, 24)
	w.Type([]byte("echo hi\n"))
	w.WaitOutput("hi\n")
	w.Close()
	waitLog(t, h, `level=INFO msg="ssh session started" tunnel=`+w.ID+` client=browser user=alice signin=signin_\w+ host=web1 host_id=host_\w+ addr=192\.0\.2\.10\n`)
	waitLog(t, h, `level=INFO msg="ssh session request" tunnel=`+w.ID+` client=browser host=web1 request=shell\n`)
	waitLog(t, h, `msg="ssh session ended" tunnel=`+w.ID+` client=browser .* reason="client closed"\n`)
	if strings.Contains(h.Log(), b.Cookie) || strings.Contains(string(w.Output()), b.Cookie) {
		t.Error("the cookie is in the log or the terminal's output")
	}
}

func deref(s *string) string {
	if s == nil {
		return "<none>"
	}
	return *s
}
