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
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"golang.org/x/crypto/ssh"

	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/harness/sshhost"
	"github.com/cieons/koko/internal/shared/api"
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

	for _, path := range []string{"/terminal/socket", "/terminal/workspace"} {
		a := send(t, h, request{method: http.MethodGet, path: path, host: "koko.example.com", origin: webURL})
		wantPage(t, a, http.StatusUpgradeRequired, "web terminal's WebSocket")
		wantSecurityHeaders(t, a.Header)
		if a.Header.Get("Upgrade") != "websocket" {
			t.Errorf("%s: the 426 answer's Upgrade header is %q, want websocket", path, a.Header.Get("Upgrade"))
		}
	}

	for _, origin := range []string{"https://evil.example", "", "http://koko.example.com", "https://koko.example.com:8443"} {
		p := h.SignIn(harness.Owner).TerminalPage()
		p.Origin = origin
		for name, dial := range map[string]func() (*websocket.Conn, harness.Answer, error){"terminal": p.Dial, "Workspace": p.DialWorkspaceSocket} {
			c, refused, err := dial()
			if err == nil {
				_ = c.CloseNow()
				t.Errorf("a %s socket from the Origin %q was accepted", name, origin)
				continue
			}
			if refused.Response == nil || refused.StatusCode != http.StatusForbidden || refused.Header.Get("Content-Security-Policy") == "" ||
				!strings.Contains(refused.Body, "came from no page of this koko server") {
				t.Errorf("a %s socket from the Origin %q was refused with %+v, want a 403 page with the listener's headers", name, origin, refused)
			}
		}
	}
	if got := host.Conns(); got != conns {
		t.Errorf("the refused WebSockets dialled the Host %d times", got-conns)
	}
}

// After the upgrade, the first message's answer refuses a page out of date
// (reload_page), a protocol 1 page among them, a browser not signed in and
// one whose Sign-in has ended (signin_required), on the terminal socket
// and the Workspace socket alike, then the WebSocket closes with status
// 1008; a malformed first message closes it with 1008 alone, protocol 1's
// open and end among them. No Host is dialled for any of them.
func TestTheTerminalRefusesInItsFirstAnswer(t *testing.T) {
	t.Parallel()
	h := startTerminals(t)
	host := terminalHost(t, h, "web1", nil)
	conns := host.Conns()
	b := h.SignIn(harness.Owner)
	attach := wire.TerminalFirst{Protocol: wire.TerminalProtocol, Attach: strings.Repeat("0", 32), Cols: 80, Rows: 24}

	for _, protocol := range []int{1, wire.TerminalProtocol + 1} {
		stale := attach
		stale.Protocol = protocol
		_, a, status := b.TerminalPage().Exchange(stale)
		wantRefused(t, a, wire.TerminalReloadPage, "this page is out of date: reload it")
		if status != websocket.StatusPolicyViolation {
			t.Errorf("reload_page closed the terminal socket with %d, want 1008", status)
		}
		ws, a, status := b.TerminalPage().DialWorkspace(protocol)
		if ws != nil {
			t.Fatalf("a Workspace socket of protocol %d was answered with the Workspace", protocol)
		}
		wantRefused(t, a, wire.TerminalReloadPage, "this page is out of date: reload it")
		if status != websocket.StatusPolicyViolation {
			t.Errorf("reload_page closed the Workspace socket with %d, want 1008", status)
		}
	}

	out := signInAgain(t, h, harness.Owner)
	page := out.TerminalPage()
	out.Do(http.MethodPost, "/auth/signout", "{}")
	for _, p := range []*harness.TerminalPage{h.Browser().TerminalPage(), page} {
		_, a, status := p.Exchange(attach)
		wantRefused(t, a, "signin_required", "this browser is not signed in, or its Sign-in has ended: sign in again")
		if status != websocket.StatusPolicyViolation {
			t.Errorf("signin_required closed with %d, want 1008", status)
		}
		_, a, _ = p.DialWorkspace(wire.TerminalProtocol)
		wantRefused(t, a, "signin_required", "this browser is not signed in, or its Sign-in has ended: sign in again")
	}

	for name, msg := range map[string]string{
		"not JSON":          "attach web1",
		"two things":        `{"protocol": 2, "attach": "` + strings.Repeat("0", 32) + `", "cols": 80, "rows": 24, "resume": "` + strings.Repeat("0", 32) + `"}`,
		"a size of 0":       `{"protocol": 2, "attach": "` + strings.Repeat("0", 32) + `", "cols": 0, "rows": 24}`,
		"an id not hex":     `{"protocol": 2, "resume": "` + strings.Repeat("g", 32) + `", "offset": 0}`,
		"over 4 KiB":        `{"protocol": 2, "attach": "` + strings.Repeat("w", 5000) + `", "cols": 80, "rows": 24}`,
		"binary, not text":  "",
		"an id of 31 chars": `{"protocol": 2, "attach": "` + strings.Repeat("0", 31) + `", "cols": 80, "rows": 24}`,
		"an open":           `{"protocol": 2, "open": "web1", "cols": 80, "rows": 24}`,
		"an end":            `{"protocol": 2, "end": "` + strings.Repeat("0", 32) + `"}`,
	} {
		c, _, err := b.TerminalPage().Dial()
		if err != nil {
			t.Fatal(err)
		}
		if msg == "" {
			err = c.Write(t.Context(), websocket.MessageBinary, []byte(`{"protocol": 2, "attach": "`+strings.Repeat("0", 32)+`", "cols": 80, "rows": 24}`))
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
	p := h.SignIn(harness.Owner).TerminalPage()
	for name, dial := range map[string]func() (*websocket.Conn, harness.Answer, error){"terminal": p.Dial, "Workspace": p.DialWorkspaceSocket} {
		c, _, err := dial()
		if err != nil {
			t.Fatal(err)
		}
		if got := harness.CloseStatus(t.Context(), c); got != websocket.StatusPolicyViolation {
			t.Errorf("a %s socket without a first message closed with %d, want 1008", name, got)
		}
	}
}

// An open runs a shell on the Host as its Host Account, logged in with the
// koko key, on a session channel with a pty: TERM xterm-256color, the
// page's size, pixel sizes 0 and IUTF8 on. 1 MiB passes each way byte for
// byte, within what the stream's buffer holds so the page never falls
// behind, a resize of the active page becomes a window-change, and the
// shell's exit status, or the signal that ended it, comes as its record
// before the stream's close.
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
	data := make([]byte, 1<<20)
	_, _ = rand.Read(data)
	for i, c := range data {
		// No query for the koko server's emulator to answer into the
		// shell's stdin, which cat would echo: no escape, no ENQ, no C1.
		if c &= 0x7f; c == 0x1b || c == 0x05 {
			c = 'x'
		}
		data[i] = c
	}
	typed := make(chan error, 1)
	go func() {
		_, err := w.Write(data)
		typed <- err
	}()
	w.WaitOutputLen(len("ready\n") + len(data))
	if out := w.Live(); !bytes.Equal(out[len("ready\n"):], data) {
		t.Error("the 1 MiB cat echoed are not those sent")
	}
	if err := <-typed; err != nil {
		t.Errorf("send 1 MiB: %v", err)
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
	if status, signal := exits.Exit(); status == nil || *status != 3 || signal != "" || string(exits.Live()) != "bye\n" {
		t.Errorf("the shell that exited 3 sent %q, then the status %v and the signal %q", exits.Live(), status, signal)
	}
	waitLog(t, h, `msg="ssh session ended" tunnel=`+exits.ID+` client=browser .* exit_status=3 reason="shell ended"\n`)
	tab := p.Workspace().WaitTab(exits.Tab, "ended", func(tab wire.WorkspaceTab) bool { return tab.State == wire.TabStateEnded })
	if e := tab.Ended; e == nil || e.Reason != "shell ended" || e.ExitStatus == nil || *e.ExitStatus != 3 || e.Notice != "" {
		t.Errorf("the tab of the shell that exited 3 is %+v, want it ended with the exit status", tab)
	}

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

// A web terminal reaches a Host behind a chain of two Relays, web1 then
// db1, and its Files panel lists the Host's files through it, for the Owner
// and for bob, who sees web1 but not db1. A refusal at either hop has the
// code it has at one Relay and the banner's text, which names the Relay
// only to a caller who can see it and every Relay between it and the Host:
// to bob, both are "app1's Relay". The log names the Relay and its hop.
func TestATerminalReachesAHostThroughAChainOfRelays(t *testing.T) {
	t.Parallel()
	h := startTerminals(t, harness.Timings(timing.Timings{HostConnectWait: time.Millisecond}))
	for _, space := range []string{"apps", "edge"} {
		h.Koko("space", "add", space).WantSuccess(t)
		h.Koko("space", "member", "add", space, "bob").WantSuccess(t)
	}
	web1 := terminalHost(t, h, "web1", []string{"space=edge"})
	terminalHost(t, h, "db1", []string{"relay=web1"})
	app1 := terminalHost(t, h, "app1", []string{"space=apps", "relay=db1"}, sshhost.SFTP())
	writeFile(t, app1.Home(), "behind-two.txt", "deep")

	owner, bob := h.SignIn(harness.Owner), h.SignIn("bob")
	for _, b := range []*harness.Browser{owner, bob} {
		w := b.TerminalPage().Open("app1", 80, 24)
		w.Type([]byte("echo $((6 * 7))\n"))
		w.WaitOutput("42")
		listed := wantFiles[api.FilesListResponse](t, list(t, b, w.ID, ""))
		if !slices.ContainsFunc(listed.Entries, func(e api.FileEntry) bool { return e.Name == "behind-two.txt" }) {
			t.Errorf("the Files panel lists %+v, want app1's behind-two.txt", listed.Entries)
		}
	}

	refuse := func(b *harness.Browser, code, message string) {
		t.Helper()
		w, a := b.TerminalPage().TryOpen("app1", 80, 24)
		if w != nil {
			t.Error("opening app1 was answered with an SSH Session")
			return
		}
		wantRefused(t, a, code, message)
	}
	h.Koko("host", "set", "db1", "account=ops").WantSuccess(t)
	refuse(owner, wire.TerminalRelayRegistered, "app1's Relay db1 is Registered: koko passes through it only once it is Enrolled, with koko host enroll db1.")
	refuse(bob, wire.TerminalRelayRegistered, "app1's Relay is Registered: koko passes through it only once it is Enrolled.")
	waitLog(t, h, `msg="ssh session refused" .* client=browser name=app1 .* reason="relay registered" relay=db1 relay_id=host_\w+ hop=2\n`)
	h.Koko("host", "set", "db1", "account="+sshhost.Account).WantSuccess(t)
	h.KokoStdin(hostPassword, "host", "enroll", "db1", "--password-stdin").WantSuccess(t)

	_, deadPort, err := net.SplitHostPort(harness.FreeAddr(t))
	if err != nil {
		t.Fatal(err)
	}
	h.Koko("host", "set", "web1", "port="+deadPort).WantSuccess(t)
	refuse(owner, wire.TerminalHostUnreachable, "app1's Relay web1 is down or unreachable: koko could not connect to it; this SSH Session did not start.")
	refuse(bob, wire.TerminalHostUnreachable, "app1's Relay is down or unreachable: koko could not connect to it; this SSH Session did not start.")
	waitLog(t, h, `msg="ssh session refused" .* client=browser name=app1 .* reason="relay unreachable" relay=web1 relay_id=host_\w+ hop=1 err=`)
	h.Koko("host", "set", "web1", "port="+strconv.Itoa(web1.Port())).WantSuccess(t)

	if err := os.WriteFile(filepath.Join(web1.Home(), ".ssh", "authorized_keys"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	refuse(owner, wire.TerminalLoginRefused, `app1's Relay web1 refused the koko key for its Host Account "deploy"; this SSH Session did not start.`)
	refuse(bob, wire.TerminalLoginRefused, "app1's Relay refused the koko key; this SSH Session did not start.")
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

// A Workspace holds at most 16 tabs, whichever page of the User opens
// them: the 17th is refused without a connection to the Host, until one
// is closed. Pages of another Sign-in of the same User share the 16.
func TestAWorkspaceHoldsAtMost16Tabs(t *testing.T) {
	t.Parallel()
	h := startTerminals(t)
	host := terminalHost(t, h, "web1", nil)
	p := h.SignIn(harness.Owner).TerminalPage()
	other := signInAgain(t, h, harness.Owner).TerminalPage()
	open := make([]*harness.WebTerminal, 16)
	for i := range open {
		page := p
		if i%2 == 1 {
			page = other
		}
		open[i] = page.Open("web1", 80, 24)
	}
	conns := host.Conns()
	for _, page := range []*harness.TerminalPage{p, other} {
		_, a := page.TryOpen("web1", 80, 24)
		wantRefused(t, a, wire.TerminalSSHSessionLimit, "this Workspace holds 16 tabs; close one first")
	}
	if host.Conns() != conns {
		t.Error("the 17th tab dialled the Host")
	}
	waitLog(t, h, `msg="ssh session refused" tunnel=[0-9a-f]{32} client=browser name=web1 signin=signin_\w+ addr=192\.0\.2\.10 reason="ssh session limit"\n`)
	if n := len(p.Workspace().Tabs()); n != 16 {
		t.Errorf("the Workspace holds %d tabs, want 16", n)
	}

	// A page that leaves frees nothing; closing a tab does.
	open[0].Close()
	_, a := p.TryOpen("web1", 80, 24)
	wantRefused(t, a, wire.TerminalSSHSessionLimit, "this Workspace holds 16 tabs; close one first")
	if a := other.Workspace().CloseTab(open[0].Tab); a.Refused != "" {
		t.Fatalf("the close was refused: %+v", a)
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

// A resume from another address moves an attachment off a WebSocket the
// koko server still holds, which a stalled path keeps: the last attach
// wins. An attachment whose WebSocket dropped resumes on a new one within
// the grace. Across both, what the shell sent and the keys typed meanwhile
// arrive once. Another Sign-in's resume of its id is refused with an empty
// message, and an id the koko server never gave alike.
func TestAnAttachmentResumesByteForByte(t *testing.T) {
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
	waitLog(t, h, `msg="ssh session resumed" tunnel=`+w.ID+` client=browser tab=[0-9a-f]{32} attachment=`+w.Attachment+` signin=signin_\w+ addr=198\.51\.100\.7\n`)
	w.Type([]byte("after\n"))
	w.WaitOutput("after\n")
	if out := w.Live(); string(out) != "ready\ntyped while cut off\nafter\n" {
		t.Errorf("across the resume the terminal got %q, want each line once", out)
	}

	w.Drop()
	w.Type([]byte("typed while dropped\n"))
	if a := b.TerminalPage().Resume(w); a.Refused != "" {
		t.Fatalf("the resume was refused: %s", deref(a.Message))
	}
	w.Type([]byte("again\n"))
	w.WaitOutput("again\n")
	if out := w.Live(); string(out) != "ready\ntyped while cut off\nafter\ntyped while dropped\nagain\n" {
		t.Errorf("across the second resume the terminal got %q, want each line once", out)
	}
	if strings.Contains(h.Log(), `msg="ssh session detached"`) {
		t.Error("a resume within the grace detached the page")
	}

	other := signInAgain(t, h, harness.Owner).TerminalPage()
	_, a, status := other.Exchange(wire.TerminalFirst{Protocol: wire.TerminalProtocol, Resume: w.Attachment, Offset: 0})
	wantRefused(t, a, wire.TerminalResumeRefused, "")
	if status != websocket.StatusPolicyViolation {
		t.Errorf("the refused resume closed with %d, want 1008", status)
	}
	waitLog(t, h, `msg="ssh session resume refused" client=browser attachment=`+w.Attachment+` signin=signin_\w+ addr=\S+ reason="unknown attachment"\n`)
	_, a, _ = other.Exchange(wire.TerminalFirst{Protocol: wire.TerminalProtocol, Resume: strings.Repeat("ab", 16), Offset: 0})
	wantRefused(t, a, wire.TerminalResumeRefused, "")
	w.Type([]byte("still\n"))
	w.WaitOutput("still\n")
}

// A page's close of its stream only detaches it, and its SSH Session goes
// on. Closing the tab, from any page of the User, ends it, at once though
// a page attached to a busy shell reads nothing: every page's tab goes,
// and the log names who closed it. A tab already gone is answered alike.
func TestClosingATabEndsItsSSHSession(t *testing.T) {
	t.Parallel()
	// A page that reads nothing never takes its last records, so the wait
	// for it is shortened.
	h := startTerminals(t, harness.Timings(timing.Timings{StopNotice: 200 * time.Millisecond}))
	terminalHost(t, h, "web1", nil)
	b := h.SignIn(harness.Owner)
	p := b.TerminalPage()
	other := signInAgain(t, h, harness.Owner).TerminalPage()

	left := p.Open("web1", 80, 24)
	left.Close()
	waitLog(t, h, `msg="ssh session detached" tunnel=`+left.ID+` client=browser tab=`+left.Tab+` attachment=`+left.Attachment+` signin=signin_\w+ addr=192\.0\.2\.10 reason="page left"\n`)
	again := other.Attach(left.ID, 80, 24)
	again.Type([]byte("echo kept\n"))
	again.WaitOutput("kept\n")

	if a := other.Workspace().CloseTab(left.Tab); a.Refused != "" {
		t.Fatalf("the close was refused: %+v", a)
	}
	wantPeerClosed(t, again.WaitEnd(), "")
	for _, page := range []*harness.TerminalPage{p, other} {
		page.Workspace().WaitTabs("no tab", func(tabs []wire.WorkspaceTab) bool { return len(tabs) == 0 })
	}
	waitLog(t, h, `msg="ssh session ended" tunnel=`+left.ID+` client=browser user=alice signin=signin_\w+ host=web1 host_id=host_\w+ tab=`+left.Tab+` duration=\S+ bytes_from_device=\d+ bytes_to_device=\d+ closed_by=signin_\w+ reason="tab closed"\n`)
	if a := p.Workspace().CloseTab(left.Tab); a.Refused != "" {
		t.Errorf("the close of a tab already gone was answered %+v", a)
	}

	// A shell that keeps writing to a page that reads nothing fills the
	// page's buffer: closing its tab ends it all the same, at once.
	busy := p.Open("web1", 80, 24)
	watcher := other.Attach(busy.ID, 80, 24)
	busy.Type([]byte("yes\n"))
	busy.WaitOutputLen(64 << 10)
	busy.StopReading()
	// Past what the stream's buffer and the queue hold for the page that
	// reads nothing, its writer waits for room.
	watcher.WaitOutputLen(len(watcher.Output()) + resumable.MaxBuffer + 1<<20)
	start := time.Now()
	if a := other.Workspace().CloseTab(busy.Tab); a.Refused != "" {
		t.Fatalf("the close was refused: %+v", a)
	}
	waitLog(t, h, `msg="ssh session ended" tunnel=`+busy.ID+` .* reason="tab closed"\n`)
	if took := time.Since(start); took > 3*time.Second {
		t.Errorf("closing the tab of a page that reads nothing took %v", took)
	}
	// Its stream closed while the page read nothing, which released the
	// writer waiting for room.
	wantPeerClosed(t, busy.WaitStreamEnd(), "")
	busy.ReadAgain()
}

// A page whose WebSocket dropped is detached once the grace has passed,
// a busy shell's too, and its resume is then refused with an empty
// message; the SSH Session goes on, and a new attach gets it.
func TestADroppedPageDetachesAtItsGrace(t *testing.T) {
	t.Parallel()
	// Nothing but the grace detaches these pages, so it is shortened.
	h := startTerminals(t, harness.Timings(timing.Timings{Grace: 200 * time.Millisecond}))
	terminalHost(t, h, "web1", nil)
	b := h.SignIn(harness.Owner)

	held := b.TerminalPage().Open("web1", 80, 24)
	held.Drop()
	waitLog(t, h, `msg="ssh session detached" tunnel=`+held.ID+` .* attachment=`+held.Attachment+` .* reason="page left"\n`)
	wantRefused(t, b.TerminalPage().Resume(held), wire.TerminalResumeRefused, "")
	back := b.TerminalPage().Attach(held.ID, 80, 24)
	back.Type([]byte("echo back\n"))
	back.WaitOutput("back\n")

	graced := detachBusy(t, h, b)
	waitLog(t, h, `msg="ssh session detached" .* attachment=`+graced.Attachment+` .* reason="page left"\n`)
	if strings.Contains(h.Log(), `msg="ssh session ended"`) {
		t.Errorf("a page's drop ended its SSH Session:\n%s", h.Log())
	}
}

// detachBusy opens a terminal on web1 whose shell keeps writing, and drops
// its page once 64 KiB of the output has reached it, while the shell's
// output fills the stream's buffer.
func detachBusy(t *testing.T, h *harness.Harness, b *harness.Browser) *harness.WebTerminal {
	t.Helper()
	w := b.TerminalPage().Open("web1", 80, 24)
	w.Type([]byte("yes\n"))
	w.WaitOutputLen(64 << 10)
	w.Drop()
	return w
}

// The Gateway watches a terminal's WebSocket, as the page does: an idle one
// stays attached, since each end probes the other after a silence, which
// reaches the page; one whose path stalls is found dead ServerDead after
// the last byte that reached the koko server, and dropped; one cut is
// dropped at once. Each attachment resumes byte for byte.
func TestTheGatewayWatchesATerminalsWebSocket(t *testing.T) {
	// Not parallel: it holds a terminal idle for a set time.
	const probe, dead = 100 * time.Millisecond, time.Second
	h := startTerminals(t, harness.Timings(timing.Timings{ServerProbe: probe, ServerDead: dead}))
	terminalHost(t, h, "web1", nil)
	b := h.SignIn(harness.Owner)
	f := harness.Forward(t, h.WebAddress())
	p := b.TerminalPage()
	p.Probe = probe / 2
	p.Workspace() // not through the forwarder, which stalls the terminal socket alone
	p.Addr = f.Addr()
	w := p.Open("web1", 80, 24)
	w.Type([]byte("echo ready; exec cat\n"))
	w.WaitOutput("ready\n")

	time.Sleep(2*dead + dead/2)
	if strings.Contains(h.Log(), "web terminal dropped") {
		t.Errorf("an idle terminal whose page probes was dropped:\n%s", h.Log())
	}
	passed := f.Stall()
	if quiet := time.Since(passed.Back); quiet > dead/2 {
		t.Errorf("the Gateway sent an idle page nothing for %v", quiet)
	}
	waitLog(t, h, `msg="web terminal found dead" tunnel=`+w.ID+` `)
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
		line := regexp.MustCompile(`msg="web terminal dropped" tunnel=` + w.ID + ` `)
		deadline := time.Now().Add(10 * time.Second)
		for len(line.FindAllString(h.Log(), -1)) < n {
			if time.Now().After(deadline) {
				t.Fatalf("the page was not dropped %d times within 10 s:\n%s", n, h.Log())
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
	w.WaitOutput("after\n")
	if out := w.Live(); string(out) != "ready\nwhile stalled\ncut\nafter\n" {
		t.Errorf("across the drops the terminal got %q, want each line once", out)
	}
}

// Stopping the koko server gives each web terminal's page the restart
// notice, as a line and as the stream's close, and each Workspace socket
// stopping, before the web listener closes: while a call in flight is still
// open on it. A page too stalled to take its notice is cut once StopNotice
// has passed, a terminal whose Host sits on its session's open ends, and
// the stop goes on.
func TestStoppingGivesEachTerminalTheRestartNotice(t *testing.T) {
	t.Parallel()
	h := startTerminals(t)
	terminalHost(t, h, "web1", nil)
	b := h.SignIn(harness.Owner)
	page := b.TerminalPage()
	live := page.Open("web1", 80, 24)
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
	if a := page.Workspace().Open("loaded", 80, 24); a.Tab == "" {
		t.Fatalf("the open of a Host that holds its channels was answered %+v, want its tab", a)
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
	if out := string(live.Live()); out != "\r\nkoko: "+notice+"\r\n" {
		t.Errorf("the terminal got %q, want the notice on a line of its own", out)
	}
	if got := page.Workspace().WaitStopping(); got != "the koko server is restarting" {
		t.Errorf("the Workspace socket was told %q as the koko server stopped", got)
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
}

// Bytes from the page that are no terminal record detach that page alone:
// its stream closes saying so, and the SSH Session goes on.
func TestAMalformedRecordDetachesThePage(t *testing.T) {
	t.Parallel()
	h := startTerminals(t)
	terminalHost(t, h, "web1", nil)
	p := h.SignIn(harness.Owner).TerminalPage()
	w := p.Open("web1", 80, 24)
	w.Send([]byte{0x08, 0x00})
	err := w.WaitEnd()
	if e, ok := errors.AsType[*resumable.PeerClosedError](err); !ok || !strings.Contains(e.Reason, "malformed terminal record") {
		t.Errorf("the stream ended with %v, want a close saying the record is malformed", err)
	}
	waitLog(t, h, `msg="ssh session detached" tunnel=`+w.ID+` .* attachment=`+w.Attachment+` .* reason="malformed record"\n`)
	again := p.Attach(w.ID, 80, 24)
	again.Type([]byte("echo on\n"))
	again.WaitOutput("on\n")
}

// The log names a web terminal's SSH Session as the browser's, with its
// tab: client, signin and the proxy's address of the page; each page's
// attach and detach with its attachment; who closed the tab; and no line
// nor answer holds the cookie.
func TestTheTerminalLogLines(t *testing.T) {
	t.Parallel()
	h := startTerminals(t)
	terminalHost(t, h, "web1", nil)
	b := h.SignIn(harness.Owner)
	p := b.TerminalPage()
	w := p.Open("web1", 80, 24)
	w.Type([]byte("echo hi\n"))
	w.WaitOutput("hi\n")
	w.Close()
	waitLog(t, h, `level=INFO msg="ssh session detached" tunnel=`+w.ID+` client=browser tab=`+w.Tab+` attachment=`+w.Attachment+` signin=signin_\w+ addr=192\.0\.2\.10 reason="page left"\n`)
	if a := p.Workspace().CloseTab(w.Tab); a.Refused != "" {
		t.Fatalf("the close was refused: %+v", a)
	}
	waitLog(t, h, `level=DEBUG msg="workspace tab opened" user=user_\w+ tab=`+w.Tab+` host=web1 signin=signin_\w+\n`)
	waitLog(t, h, `level=INFO msg="ssh session started" tunnel=`+w.ID+` client=browser user=alice signin=signin_\w+ host=web1 host_id=host_\w+ addr=192\.0\.2\.10 tab=`+w.Tab+`\n`)
	waitLog(t, h, `level=INFO msg="ssh session request" tunnel=`+w.ID+` client=browser host=web1 request=shell\n`)
	waitLog(t, h, `level=INFO msg="ssh session attached" tunnel=`+w.ID+` client=browser tab=`+w.Tab+` attachment=`+w.Attachment+` signin=signin_\w+ addr=192\.0\.2\.10\n`)
	waitLog(t, h, `level=DEBUG msg="workspace tab closed" user=user_\w+ tab=`+w.Tab+` label=web1 signin=signin_\w+\n`)
	waitLog(t, h, `msg="ssh session ended" tunnel=`+w.ID+` client=browser .* tab=`+w.Tab+` .* closed_by=signin_\w+ reason="tab closed"\n`)
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
