package web_test

import (
	"net"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"golang.org/x/crypto/ssh"

	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/harness/sshhost"
	"github.com/cieons/koko/internal/server/vt"
	"github.com/cieons/koko/internal/shared/timing"
	"github.com/cieons/koko/internal/shared/wire"
)

// labels are the labels of tabs, in order.
func labels(tabs []wire.WorkspaceTab) []string {
	var l []string
	for _, tab := range tabs {
		l = append(l, tab.Label)
	}
	return l
}

// workspaces counts the Workspace messages among msgs.
func workspaces(msgs []string) int {
	n := 0
	for _, m := range msgs {
		if strings.HasPrefix(m, `{"tabs":`) {
			n++
		}
	}
	return n
}

// Two Sign-ins of one User share their Workspace: a tab opened in one page
// appears in the other, and in its own before the answer; labels run
// "web1 (2)"; a tab closed from either goes from both; two pages closing
// the same tab at once make one change and get two answers; two opening
// the 16th at once get one tab and one ssh_session_limit; a page joining
// late gets the Workspace whole; another User sees none of it, and cannot
// attach to it.
func TestTheWorkspaceIsTheSameInEveryPage(t *testing.T) {
	t.Parallel()
	h := startTerminals(t)
	terminalHost(t, h, "web1", nil)
	terminalHost(t, h, "db2", nil)
	a := h.SignIn(harness.Owner).TerminalPage().Workspace()
	b := signInAgain(t, h, harness.Owner).TerminalPage().Workspace()

	first := a.Open("web1", 80, 24)
	msgs := a.Messages()
	answer := slices.IndexFunc(msgs, func(m string) bool { return strings.HasPrefix(m, `{"request":1,`) })
	shown := slices.IndexFunc(msgs, func(m string) bool { return strings.HasPrefix(m, `{"tabs":`) && strings.Contains(m, first.Tab) })
	if shown < 0 || shown > answer {
		t.Errorf("the opening page got %q, want the Workspace with the tab before the answer", msgs)
	}
	b.WaitTab(first.Tab, "shown", func(wire.WorkspaceTab) bool { return true })
	second := b.Open("web1", 80, 24)
	third := a.Open("db2", 80, 24)
	for _, ws := range []*harness.WorkspacePage{a, b} {
		got := ws.WaitTabs("three tabs", func(tabs []wire.WorkspaceTab) bool { return len(tabs) == 3 })
		if l := labels(got); !slices.Equal(l, []string{"web1", "web1 (2)", "db2"}) {
			t.Errorf("a page holds the tabs %q, want web1, web1 (2) and db2 in order", l)
		}
	}

	if a := a.CloseTab(second.Tab); a.Refused != "" {
		t.Fatalf("the close was refused: %+v", a)
	}
	for _, ws := range []*harness.WorkspacePage{a, b} {
		ws.WaitTabs("web1 and db2", func(tabs []wire.WorkspaceTab) bool { return slices.Equal(labels(tabs), []string{"web1", "db2"}) })
	}

	// Each tab's start changes the Workspace by itself, so the count waits
	// until both run.
	bothRun := func(tabs []wire.WorkspaceTab) bool {
		return len(tabs) == 2 && !slices.ContainsFunc(tabs, func(t wire.WorkspaceTab) bool { return t.State != wire.TabStateRunning })
	}
	a.WaitTabs("two tabs running", bothRun)
	b.WaitTabs("two tabs running", bothRun)
	beforeA, beforeB := len(a.Messages()), len(b.Messages())
	na, nb := a.Send(wire.WorkspaceRequest{Close: third.Tab}), b.Send(wire.WorkspaceRequest{Close: third.Tab})
	for _, ws := range []struct {
		page   *harness.WorkspacePage
		n      uint64
		before int
	}{{a, na, beforeA}, {b, nb, beforeB}} {
		if got := ws.page.Answer(ws.n); got.Refused != "" {
			t.Errorf("a page closing the tab at once with another was answered %+v", got)
		}
		if n := workspaces(ws.page.Messages()[ws.before:]); n != 1 {
			t.Errorf("two pages closing one tab at once sent a page %d Workspaces, want 1", n)
		}
	}

	for range 14 {
		a.Open("web1", 80, 24)
	}
	na, nb = a.Send(wire.WorkspaceRequest{Open: "web1", Cols: 80, Rows: 24}), b.Send(wire.WorkspaceRequest{Open: "web1", Cols: 80, Rows: 24})
	answers := []wire.WorkspaceAnswer{a.Answer(na), b.Answer(nb)}
	if !slices.ContainsFunc(answers, func(x wire.WorkspaceAnswer) bool { return x.Tab != "" }) ||
		!slices.ContainsFunc(answers, func(x wire.WorkspaceAnswer) bool { return x.Refused == wire.TerminalSSHSessionLimit }) {
		t.Errorf("two pages opening the 16th tab at once were answered %+v, want one tab and one ssh_session_limit", answers)
	}
	whole := a.WaitTabs("16 tabs", func(tabs []wire.WorkspaceTab) bool { return len(tabs) == 16 })

	late := signInAgain(t, h, harness.Owner).TerminalPage().Workspace()
	if got := late.Tabs(); !slices.EqualFunc(got, whole, func(x, y wire.WorkspaceTab) bool { return x.ID == y.ID && x.Label == y.Label }) {
		t.Errorf("a page joining late holds %q, want the Workspace whole, %q", labels(got), labels(whole))
	}

	bob := h.SignIn("bob").TerminalPage()
	if tabs := bob.Workspace().Tabs(); len(tabs) != 0 {
		t.Errorf("another User's page holds the tabs %q, want none", labels(tabs))
	}
	running := a.WaitTab(first.Tab, "running", func(tab wire.WorkspaceTab) bool { return tab.State == wire.TabStateRunning })
	_, refused := bob.TryAttach(running.Session, 80, 24)
	wantRefused(t, refused, wire.TerminalUnknownTunnel, "this User has no open SSH Session with this id")
}

// A message on the Workspace socket that is no request closes the socket
// with status 1008, one over 4 KiB with 1009, and none changes anything.
func TestAMalformedWorkspaceMessageClosesItsSocket(t *testing.T) {
	t.Parallel()
	h := startTerminals(t)
	terminalHost(t, h, "web1", nil)
	p := h.SignIn(harness.Owner).TerminalPage()
	for name, msg := range map[string]string{
		"not JSON":         "open web1",
		"no number":        `{"open": "web1", "cols": 80, "rows": 24}`,
		"two things":       `{"request": 1, "open": "web1", "close": "` + strings.Repeat("0", 32) + `", "cols": 80, "rows": 24}`,
		"a size of 0":      `{"request": 1, "open": "web1", "cols": 0, "rows": 24}`,
		"a tab not its id": `{"request": 1, "close": "web1"}`,
		"over 4 KiB":       `{"request": 1, "open": "` + strings.Repeat("w", 5000) + `", "cols": 80, "rows": 24}`,
		"a reopen unsized": `{"request": 1, "reopen": "` + strings.Repeat("0", 32) + `"}`,
	} {
		ws, _, _ := p.DialWorkspace(wire.TerminalProtocol)
		ws.SendRaw([]byte(msg))
		select {
		case <-ws.Ended():
		case <-time.After(10 * time.Second):
			t.Fatalf("%s: the Workspace socket stayed open", name)
		}
		want := websocket.StatusPolicyViolation
		if len(msg) > wire.WorkspaceMaxMessage {
			want = websocket.StatusMessageTooBig
		}
		if got := websocket.CloseStatus(ws.Err()); got != want {
			t.Errorf("%s: the Workspace socket closed with %d, want %d", name, got, want)
		}
	}
	if tabs := p.Workspace().Tabs(); len(tabs) != 0 {
		t.Errorf("the malformed messages opened the tabs %q", labels(tabs))
	}
}

// stripVT drops the escape sequences of what a terminal received, leaving
// its text.
var stripVT = regexp.MustCompile(`\x1b\[[0-9;?<>=]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(\x07|\x1b\\)|\x1b[()][0-9A-Za-z]|\x1b[0-9=>A-Za-z]`)

// screenOf draws what a page holds since its last reset in a fresh core of
// cols by rows, and returns that core's snapshot.
func screenOf(t *testing.T, cols, rows int, got ...[]byte) []byte {
	t.Helper()
	term, err := vt.New(t.Context(), cols, rows)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = term.Close() }()
	for _, b := range got {
		if _, err := term.Write(b); err != nil {
			t.Fatal(err)
		}
	}
	snap, err := term.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	return snap
}

// A second page attaching while a full-screen program shows gets a reset
// at the pty's size and a snapshot that draws the first page's screen,
// history and the screen beneath included; keys from both reach the shell
// in order; and a page that attaches while output streams misses no byte
// and gets none twice.
func TestASecondPageAttachesToTheScreenAsItIs(t *testing.T) {
	t.Parallel()
	h := startTerminals(t)
	terminalHost(t, h, "web1", nil)
	p := h.SignIn(harness.Owner).TerminalPage()
	q := signInAgain(t, h, harness.Owner).TerminalPage()

	first := p.Open("web1", 80, 24)
	first.Type([]byte(`seq 1 60; printf '\033[?1049h\033[2J\033[Hvim on top\033[10;5H\033[1;31mred bold\033[0m\033[?2004h'; echo; echo shown; exec cat` + "\n"))
	first.WaitOutput("shown")
	second := q.Attach(first.ID, 80, 24)
	if r := second.WaitResets(1); r[0] != [2]uint32{80, 24} {
		t.Errorf("the second page's reset is at %v, want the pty's 80x24", r[0])
	}
	// The snapshot gives the alternate screen a saved cursor at its
	// origin, where the first page's has none; nothing on the screen
	// shows it, so the comparison leaves it out.
	alternate := strings.NewReplacer("\x1b[?1049h\x1b[1;1H\x1b[0m\x1b7\x1b[H\x1b[0m", "\x1b[?1049h")
	got, want := screenOf(t, 80, 24, second.Snapshot()), screenOf(t, 80, 24, first.Output())
	if alternate.Replace(string(got)) != string(want) {
		t.Errorf("the second page's snapshot draws\n%q\nwant the first page's screen\n%q", got, want)
	}

	for i, w := range []*harness.WebTerminal{first, second, first, second} {
		line := "key " + strconv.Itoa(i) + "\n"
		w.Type([]byte(line))
		for _, page := range []*harness.WebTerminal{first, second} {
			page.WaitOutput(line)
		}
	}
	if out := string(second.Output()); !strings.Contains(out, "key 0\nkey 1\nkey 2\nkey 3\n") {
		t.Errorf("the keys of both pages reached the shell as %q, want in the order typed", out)
	}

	streams := p.Open("web1", 80, 24)
	const last = 200000
	streams.Type([]byte("seq 1 " + strconv.Itoa(last) + "; echo done\n"))
	streams.WaitOutputLen(64 << 10)
	late := q.Attach(streams.ID, 80, 24)
	late.WaitOutput("done")
	text := stripVT.ReplaceAllString(strings.ReplaceAll(string(late.Output()), "\r", ""), "")
	var numbers []int
	for line := range strings.SplitSeq(text, "\n") {
		if n, err := strconv.Atoi(strings.TrimSpace(line)); err == nil {
			numbers = append(numbers, n)
		}
	}
	if len(numbers) < 2 || numbers[len(numbers)-1] != last {
		t.Fatalf("the page that attached as output streamed holds %d numbers, ending %v, want them up to %d", len(numbers), numbers[max(0, len(numbers)-3):], last)
	}
	// The snapshot's oldest row of history may be the wrapped tail of a
	// number whose first row went, so the check starts after it.
	for i := 2; i < len(numbers); i++ {
		if numbers[i] != numbers[i-1]+1 {
			t.Fatalf("the page that attached as output streamed got %d after %d: a byte missed or one twice", numbers[i], numbers[i-1])
		}
	}
}

// A page that stops reading holds up neither the shell nor the other page:
// the other gets the whole output while it reads nothing, and once it
// reads again it gets a reset and a fresh snapshot in place of what it
// missed.
func TestAPageThatStopsReadingHoldsUpNothing(t *testing.T) {
	t.Parallel()
	h := startTerminals(t)
	terminalHost(t, h, "web1", nil)
	other := h.SignIn(harness.Owner).TerminalPage().Open("web1", 80, 24)
	shared := signInAgain(t, h, harness.Owner).TerminalPage().Attach(other.ID, 80, 24)
	shared.WaitResets(1)
	shared.StopReading()
	other.Type([]byte("head -c 4000000 /dev/zero | tr '\\0' x; echo; echo end-of-output\n"))
	other.WaitOutput("end-of-output")

	shared.ReadAgain()
	shared.WaitOutput("end-of-output")
	if r := shared.Resets(); len(r) < 2 {
		t.Errorf("the page that read again got the resets %v, want a fresh one after the first", r)
	}
}

// windowChanges returns the sizes of the window-changes the Host got.
func windowChanges(host *sshhost.Host) [][2]uint32 {
	var sizes [][2]uint32
	for _, r := range host.Requests() {
		if r.Type == "window-change" {
			var c struct{ Columns, Rows, Width, Height uint32 }
			if ssh.Unmarshal(r.Payload, &c) == nil {
				sizes = append(sizes, [2]uint32{c.Columns, c.Rows})
			}
		}
	}
	return sizes
}

// waitWindowChange waits until the Host's last window-change is size.
func waitWindowChange(t *testing.T, host *sshhost.Host, size [2]uint32) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if c := windowChanges(host); len(c) > 0 && c[len(c)-1] == size {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the Host got the window-changes %v within 10 s, want the last at %v", windowChanges(host), size)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// The pty takes the active page's box: typing in the second page sends the
// Host a window-change to its box and every page a reset at it, and so
// does active alone, each page told whether it is the active one. A page
// that only reconnects and resumes takes nothing, a page detaching leaves
// the size, and a box past maxGrid is held to it.
func TestThePtyTakesTheActivePagesSize(t *testing.T) {
	t.Parallel()
	h := startTerminals(t)
	host := terminalHost(t, h, "web1", nil)
	p := h.SignIn(harness.Owner).TerminalPage()
	q := signInAgain(t, h, harness.Owner).TerminalPage()
	first := p.Open("web1", 80, 24)
	first.Type([]byte("exec cat\n"))
	first.WaitActive(true)

	second := q.Attach(first.ID, 100, 30)
	second.WaitActive(false)
	second.Type([]byte("typed\n"))
	waitWindowChange(t, host, [2]uint32{100, 30})
	waitReset(t, first, [2]uint32{100, 30})
	waitReset(t, second, [2]uint32{100, 30})
	second.WaitActive(true)
	first.WaitActive(false)

	third := q.Attach(first.ID, 90, 20)
	third.WaitResets(1)
	third.Act()
	waitWindowChange(t, host, [2]uint32{90, 20})
	for _, w := range []*harness.WebTerminal{first, second, third} {
		waitReset(t, w, [2]uint32{90, 20})
	}
	third.WaitActive(true)
	second.WaitActive(false)

	changes := len(windowChanges(host))
	second.Drop()
	if a := q.Resume(second); a.Refused != "" {
		t.Fatalf("the resume was refused: %+v", a)
	}
	third.Type([]byte("after the resume\n"))
	third.WaitOutput("after the resume\n")
	if got := windowChanges(host)[changes:]; len(got) != 0 {
		t.Errorf("a page that resumed sent the Host the window-changes %v, want none", got)
	}

	third.Close()
	waitLog(t, h, `msg="ssh session detached" tunnel=`+first.ID+` .* attachment=`+third.Attachment+` `)
	fourth := p.Attach(first.ID, 70, 20)
	if r := fourth.WaitResets(1); r[0] != [2]uint32{90, 20} {
		t.Errorf("after the active page detached a page attached at %v, want the size left at 90x20", r[0])
	}

	huge := p.Attach(first.ID, 1000, 500)
	huge.WaitResets(1)
	huge.Act()
	waitWindowChange(t, host, [2]uint32{vt.MaxCols, vt.MaxRows})
	waitReset(t, huge, [2]uint32{vt.MaxCols, vt.MaxRows})
}

// waitReset waits until the page's last reset is at size.
func waitReset(t *testing.T, w *harness.WebTerminal, size [2]uint32) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if r := w.Resets(); len(r) > 0 && r[len(r)-1] == size {
			w.WaitResets(len(r))
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the page got the resets %v within 10 s, want the last at %v", w.Resets(), size)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// The Workspace socket is watched as the terminal socket is: the koko
// server sends {} after timing.ServerProbe without sending, so a page that
// sends {} stays, and it cuts a page from which nothing came for
// timing.ServerDead.
func TestTheWorkspaceSocketIsWatched(t *testing.T) {
	// Not parallel: it holds a socket idle for a set time.
	const probe, dead = 100 * time.Millisecond, time.Second
	h := startTerminals(t, harness.Timings(timing.Timings{ServerProbe: probe, ServerDead: dead}))
	b := h.SignIn(harness.Owner)
	alive := b.TerminalPage()
	alive.Probe = probe / 2
	ws := alive.Workspace()
	silentPage := b.TerminalPage()
	silentPage.Probe = 0
	opened := time.Now()
	silent := silentPage.Workspace()
	select {
	case <-silent.Ended():
		if took := time.Since(opened); took < dead {
			t.Errorf("a page that sent nothing was cut after %v, before %v", took, dead)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("a page that sent nothing was not cut within 10 s")
	}
	if ws.Err() != nil {
		t.Errorf("a page that sends {} was cut: %v", ws.Err())
	}
	if n := slices.Index(ws.Messages(), "{}"); n < 0 {
		t.Errorf("the koko server sent a quiet page %q, want {} among them", ws.Messages())
	}
}

// A tab reopens to its own Host, by its id: renamed, and its name taken
// by another Host since, the tab's new SSH Session goes to the Host it
// had, under its new name.
func TestAReopenGoesToTheTabsOwnHost(t *testing.T) {
	t.Parallel()
	h := startTerminals(t)
	own := terminalHost(t, h, "web1", nil)
	p := h.SignIn(harness.Owner).TerminalPage()
	w := p.Open("web1", 80, 24)
	w.Type([]byte("exit\n"))
	p.Workspace().WaitTab(w.Tab, "ended", func(tab wire.WorkspaceTab) bool { return tab.State == wire.TabStateEnded })
	h.Koko("host", "rename", "web1", "web9").WantSuccess(t)
	other := terminalHost(t, h, "web1", nil)
	ownLogins, otherLogins := len(own.Logins()), len(other.Logins())

	if a := p.Workspace().Reopen(w.Tab, 80, 24); a.Tab != w.Tab {
		t.Fatalf("the reopen was answered %+v", a)
	}
	tab := p.Workspace().WaitTab(w.Tab, "running", func(tab wire.WorkspaceTab) bool { return tab.State == wire.TabStateRunning })
	if tab.Host != "web9" || tab.Label != "web1" {
		t.Errorf("the reopened tab is %+v, want web9, labelled web1", tab)
	}
	if len(own.Logins()) == ownLogins || len(other.Logins()) != otherLogins {
		t.Error("the reopen logged in to the Host that took the tab's Host's name, not to the tab's own")
	}
}

// A ninth page attaching to one SSH Session displaces the page least
// recently active in it, never the active page, though the active page
// acted before the other seven attached: the displaced page is told so
// before its stream closes.
func TestANinthPageDisplacesTheLeastRecentlyActive(t *testing.T) {
	t.Parallel()
	h := startTerminals(t)
	terminalHost(t, h, "web1", nil)
	p := h.SignIn(harness.Owner).TerminalPage()
	first := p.Open("web1", 80, 24)
	first.Type([]byte("exec cat\n"))
	pages := []*harness.WebTerminal{first}
	for range 7 {
		w := p.Attach(first.ID, 80, 24)
		w.WaitResets(1)
		pages = append(pages, w)
	}
	ninth := p.Attach(first.ID, 80, 24)
	ninth.WaitResets(1)
	wantPeerClosed(t, pages[1].WaitEnd(), "")
	if !pages[1].Displaced() {
		t.Error("the displaced page got no displaced record")
	}
	waitLog(t, h, `msg="ssh session detached" tunnel=`+first.ID+` .* attachment=`+pages[1].Attachment+` .* reason=displaced\n`)
	for _, w := range append(pages[2:], first, ninth) {
		if w.Displaced() {
			t.Errorf("the page %s was displaced too", w.Attachment)
		}
	}
	first.Type([]byte("still\n"))
	ninth.WaitOutput("still\n")
}

// The koko server answers the shell's device attributes query once,
// whether two pages are attached or none: the shell reads, in one read,
// whatever came to its stdin a second after it asked.
func TestTheKokoServerAnswersTheShellsQueriesOnce(t *testing.T) {
	t.Parallel()
	h := startTerminals(t)
	host := terminalHost(t, h, "web1", nil)
	p := h.SignIn(harness.Owner).TerminalPage()
	q := signInAgain(t, h, harness.Owner).TerminalPage()
	const ask = `printf '\033[c'; sleep 1; dd bs=64 count=1 2>/dev/null | od -An -c; echo answered; exec cat` + "\n"
	answers := func(out []byte) int { return strings.Count(stripVT.ReplaceAllString(string(out), ""), "033") }

	two := p.Open("web1", 80, 24)
	q.Attach(two.ID, 80, 24).WaitResets(1)
	two.Type([]byte(ask))
	if out := two.WaitOutput("answered"); answers(out) != 1 {
		t.Errorf("with two pages attached the shell read %q, want one answer", out)
	}

	// The shell asks once no page is attached, which the test tells it with
	// a file, and says it asked with another.
	none := p.Open("web1", 80, 24)
	none.Type([]byte(`until [ -e "$HOME/ask" ]; do sleep 0.05; done; touch "$HOME/asked"; ` + ask))
	none.Close()
	waitLog(t, h, `msg="ssh session detached" tunnel=`+none.ID+` `)
	writeFile(t, host.Home(), "ask", "")
	deadline := time.Now().Add(10 * time.Second)
	for _, err := os.Stat(filepath.Join(host.Home(), "asked")); err != nil; _, err = os.Stat(filepath.Join(host.Home(), "asked")) {
		if time.Now().After(deadline) {
			t.Fatal("the shell did not ask within 10 s")
		}
		time.Sleep(10 * time.Millisecond)
	}
	back := p.Attach(none.ID, 80, 24)
	if out := back.WaitOutput("answered"); answers(out) != 1 {
		t.Errorf("with no page attached the shell read %q, want one answer", out)
	}
}

// copyDataDir copies a running koko server's data directory, as a crash
// leaves it: the database and its write-ahead log as they are.
func copyDataDir(t *testing.T, dir string) string {
	t.Helper()
	to := t.TempDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(to, e.Name()), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return to
}

// A koko server stopped and started on the same store, cleanly or after a
// crash, brings its tabs back restarted, and a tab ended before the stop
// stays ended. The first page's reopens open them one after another, each
// saying the earlier SSH Session ended in the restart; a second page's
// reopen at the same time opens nothing more; a Host no longer in reach
// shows the refusal.
func TestTabsComeBackRestarted(t *testing.T) {
	t.Parallel()
	dir, key := t.TempDir(), harness.NewRootKeyFile(t)
	// The Host no longer in reach is never reached, so the connector's
	// wait between its tries is shortened.
	timings := harness.Timings(timing.Timings{HostConnectWait: time.Millisecond})
	h := startTerminals(t, harness.DataDir(dir), harness.RootKeyFile(key), timings)
	terminalHost(t, h, "web1", nil)
	terminalHost(t, h, "down", nil)
	b := h.SignIn(harness.Owner)
	p := b.TerminalPage()
	p.Open("web1", 80, 24)
	p.Open("web1", 80, 24)
	ended := p.Open("web1", 80, 24)
	ended.Type([]byte("exit 4\n"))
	p.Workspace().WaitTab(ended.Tab, "ended", func(tab wire.WorkspaceTab) bool { return tab.State == wire.TabStateEnded })
	p.Open("down", 80, 24)
	crashed := copyDataDir(t, dir)
	h.Stop()

	_, deadPort, err := net.SplitHostPort(harness.FreeAddr(t))
	if err != nil {
		t.Fatal(err)
	}
	for name, d := range map[string]string{"a clean stop": dir, "a crash": crashed} {
		t.Run(name, func(t *testing.T) {
			again := harness.Start(t, harness.NoInit(), harness.WebListen("127.0.0.1:0"), harness.DataDir(d), harness.RootKeyFile(key), timings)
			again.Koko("host", "set", "down", "port="+deadPort).WantSuccess(t)
			page := func() *harness.TerminalPage {
				br := again.Browser()
				br.Cookie = b.Cookie
				return br.TerminalPage()
			}
			first, second := page().Workspace(), page().Workspace()
			tabs := first.Tabs()
			if got := labels(tabs); !slices.Equal(got, []string{"web1", "web1 (2)", "web1 (3)", "down"}) {
				t.Fatalf("after %s the Workspace holds %q, want its four tabs", name, got)
			}
			for i, want := range []string{wire.TabStateRestarted, wire.TabStateRestarted, wire.TabStateEnded, wire.TabStateRestarted} {
				if tabs[i].State != want {
					t.Errorf("after %s the tab %s is %s, want %s", name, tabs[i].Label, tabs[i].State, want)
				}
			}

			var wg sync.WaitGroup
			var other wire.WorkspaceAnswer
			wg.Go(func() { other = second.Reopen(tabs[0].ID, 80, 24) })
			for _, tab := range []wire.WorkspaceTab{tabs[0], tabs[1], tabs[3]} {
				if a := first.Reopen(tab.ID, 80, 24); a.Tab != tab.ID {
					t.Errorf("the reopen of %s was answered %+v", tab.Label, a)
				}
				first.WaitTab(tab.ID, "started", func(t wire.WorkspaceTab) bool { return t.State != wire.TabStateStarting })
			}
			wg.Wait()
			if other.Tab != tabs[0].ID {
				t.Errorf("the second page's reopen was answered %+v, want the tab as it is", other)
			}
			for _, tab := range tabs[:2] {
				running := first.WaitTab(tab.ID, "running", func(t wire.WorkspaceTab) bool { return t.State == wire.TabStateRunning })
				w := page().Attach(running.Session, 80, 24)
				w.WaitOutput("koko: the koko server restarted; this is a new SSH Session.\r\n")
				if n := len(regexp.MustCompile(`msg="ssh session started" .* tab=`+tab.ID+`\n`).FindAllString(again.Log(), -1)); n != 1 {
					t.Errorf("the tab %s started %d SSH Sessions, want 1", tab.Label, n)
				}
			}
			down := first.WaitTab(tabs[3].ID, "refused", func(t wire.WorkspaceTab) bool { return t.State == wire.TabStateRefused })
			if down.Refused == nil || down.Refused.Code != wire.TerminalHostUnreachable {
				t.Errorf("the tab to a Host no longer in reach is %+v, want refused host_unreachable", down)
			}
			if e := first.Tabs()[2]; e.State != wire.TabStateEnded || e.Ended == nil || e.Ended.ExitStatus == nil || *e.Ended.ExitStatus != 4 {
				t.Errorf("the tab ended before the stop is %+v, want it ended still", e)
			}
		})
	}
}
