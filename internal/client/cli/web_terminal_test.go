package cli_test

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/shared/resumable"
	"github.com/cieons/koko/internal/shared/wire"
)

// webTerminalPage opens koko web's address in a new browser, which trades
// its token for the cookie, and returns the web terminal's page there,
// holding the cookie, with koko web's own address as its Origin.
func webTerminalPage(t *testing.T, w *kokoWeb) *harness.TerminalPage {
	t.Helper()
	b := newBrowser(t, w.port)
	b.open(w)
	u, err := url.Parse(fmt.Sprintf("http://localhost:%d/", w.port))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range b.client.Jar.Cookies(u) {
		if c.Name == fmt.Sprintf("koko_%d", w.port) {
			return harness.KokoWebTerminalPage(t, w.port, &http.Cookie{Name: c.Name, Value: c.Value})
		}
	}
	t.Fatal("the browser holds no cookie of koko web after opening its address")
	return nil
}

// Through koko web over the Unix socket, a page whose Origin is koko web's
// own address, by localhost or 127.0.0.1, opens a web terminal as the Local
// caller: the WebSockets' upgrades and their frames pass through koko web's
// forwarder as they are. Another Origin is refused before the upgrade, and
// a page without the cookie in its first answer. Another run's page and
// the Owner's Sign-in on the web listener join the Owner's Workspace, and a
// tab opened in either shows in the other; another run's page attaches to
// the terminal but cannot resume this run's attachment. The run's
// terminals end with it.
func TestAWebTerminalThroughKokoWeb(t *testing.T) {
	t.Parallel()
	h := harness.Start(t, harness.WebListen("127.0.0.1:0"))
	tunnelHost(t, h, "web1")
	w := startWeb(t, h)
	p := webTerminalPage(t, w)

	term := p.Open("web1", 80, 24)
	term.Type([]byte("echo ready; exec cat\n"))
	term.WaitOutput("ready\n")
	data := randomBytes(256 << 10)
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
		_, err := term.Write(data)
		typed <- err
	}()
	term.WaitOutputLen(len("ready\n") + len(data))
	if out := term.Live(); !bytes.Equal(out[len("ready\n"):], data) {
		t.Error("the bytes cat echoed through koko web are not those sent")
	}
	if err := <-typed; err != nil {
		t.Errorf("send 256 KiB: %v", err)
	}
	waitServerLog(t, h, `msg="ssh session started" tunnel=`+term.ID+` client=browser user=alice device_session="unix socket" host=web1 host_id=host_\w+ addr=\S+ tab=`+term.Tab+`\n`)

	byIP := *p
	byIP.Origin = fmt.Sprintf("http://127.0.0.1:%d", w.port)
	byIP.Open("web1", 80, 24)

	other := *p
	other.Origin = "http://localhost:1"
	if _, refused, err := other.Dial(); err == nil || refused.Response == nil || refused.StatusCode != http.StatusForbidden {
		t.Errorf("a WebSocket from another Origin through koko web was refused with %+v (%v), want 403", refused, err)
	}
	none := *p
	none.Cookie = nil
	if _, a, _ := none.DialWorkspace(wire.TerminalProtocol); a.Refused != "cookie_required" {
		t.Errorf("a page without the cookie was answered %+v, want cookie_required", a)
	}

	another := webTerminalPage(t, startWeb(t, h))
	if _, a, _ := another.Exchange(wire.TerminalFirst{Protocol: wire.TerminalProtocol, Resume: term.Attachment}); a.Refused != wire.TerminalResumeRefused || a.Message == nil || *a.Message != "" {
		t.Errorf("another run of koko web resuming the attachment was answered %+v, want resume_refused with no message", a)
	}
	if tabs := another.Workspace().Tabs(); !slices.ContainsFunc(tabs, func(tab wire.WorkspaceTab) bool { return tab.ID == term.Tab }) {
		t.Errorf("another run's page holds the Workspace %+v, want the Owner's, with the terminal's tab", tabs)
	}
	signedIn := h.SignIn(harness.Owner).TerminalPage().Workspace()
	signedIn.WaitTab(term.Tab, "shown", func(wire.WorkspaceTab) bool { return true })
	opened := signedIn.Open("web1", 80, 24)
	p.Workspace().WaitTab(opened.Tab, "shown", func(wire.WorkspaceTab) bool { return true })
	shown := another.Attach(term.ID, 80, 24)
	term.Type([]byte("still\n"))
	term.WaitOutput("still\n")
	shown.WaitOutput("still\n")

	w.Stop()
	waitServerLog(t, h, `msg="ssh session ended" tunnel=`+term.ID+` client=browser .* reason="koko web ended"\n`)
}

// On a joined device, koko web's web terminal acts as its Device Session,
// but its SSH Sessions count against its User's Workspace, not against the
// Device Session's 64, which counts its Tunnels alone: with 64 open, a
// terminal still opens through koko web, and with it open the 65th Tunnel,
// from koko exec, is refused without a connection to the Host.
func TestKokoWebsTerminalCountsAgainstItsWorkspace(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	host := tunnelHost(t, h, "web1")
	pinned := gatewayHostKey(t, h)
	laptop := h.Join(harness.Owner)
	open := make([]*ssh.Client, 63)
	for i := range open {
		open[i] = sshOver(t, laptop.Proxy(nil, "tunnel", "web1"), "web1", pinned)
	}
	p := webTerminalPage(t, startWebThrough(t, laptop.Background, freePort(t)))
	p.Open("web1", 80, 24)
	sshOver(t, laptop.Proxy(nil, "tunnel", "web1"), "web1", pinned)
	p.Open("web1", 80, 24)
	waitServerLog(t, h, `msg="ssh session started" tunnel=[0-9a-f]{32} client=browser user=alice device_session=\S+ host=web1 `)

	conns := host.Conns()
	r := laptop.Koko("exec", "web1", "true")
	if r.Status == 0 || !strings.Contains(r.Stderr, "this Device Session already has 64 SSH Sessions open, the most koko allows; this SSH Session did not start.") {
		t.Errorf("the 65th Tunnel, from koko exec beside the Workspace, exited %d with %q, want the limit's refusal", r.Status, r.Stderr)
	}
	if host.Conns() != conns {
		t.Error("the 65th Tunnel dialled the Host")
	}
	waitServerLog(t, h, `msg="ssh session refused" tunnel=[0-9a-f]{32} client=ssh name=web1 device_session=\S+ addr=\S+ reason="ssh session limit"\n`)
}

// Revoking a Device Session ends its web terminal through koko web as it
// ends its Tunnels: the notice reaches the page, as a line and as the
// stream's close.
func TestRevokingADeviceSessionEndsItsWebTerminal(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	tunnelHost(t, h, "web1")
	laptop := h.Join(harness.Owner)
	p := webTerminalPage(t, startWebThrough(t, laptop.Background, freePort(t)))
	term := p.Open("web1", 80, 24)
	h.Koko("device", "revoke", harness.Owner+"/"+readClientConfig(t, laptop).Device, "--last").WantSuccess(t)
	const notice = "access to web1 was withdrawn: this device's Device Session was Revoked; this SSH Session is closed."
	if e, ok := errors.AsType[*resumable.PeerClosedError](term.WaitEnd()); !ok || e.Reason != notice {
		t.Errorf("the terminal ended with %v, want the revocation's notice", term.Err())
	}
	if out := string(term.Live()); out != "\r\nkoko: "+notice+"\r\n" {
		t.Errorf("the terminal got %q, want the notice on a line of its own", out)
	}
	waitServerLog(t, h, `msg="ssh session ended" tunnel=`+term.ID+` client=browser .* reason=revoked\n`)
}

// Through koko web on a joined device, a dropped Device Connection ends the
// web terminal's WebSocket on it: after a cut the koko server drops its
// attachment, or a quick resume comes first, after a stall the device's new
// Device Connection wins over the one the koko server still holds. Either
// way the page resumes its attachment through koko web once the Device Mux
// has reconnected, byte for byte.
func aWebTerminalThroughKokoWebResumesAcrossADrop(t *testing.T) {
	for _, f := range faults {
		t.Run(f.name, func(t *testing.T) {
			s := resumeHost(t, resumeServer)
			d := harness.NewMuxDevice(t, resumeDevice)
			d.Koko("join", s.joinCode).WantSuccess(t)
			p := webTerminalPage(t, startWebThrough(t, d.Background, freePort(t)))
			// The page probes as the koko server does, well within its
			// ServerDead.
			p.Probe = resumeServer.ServerProbe
			term := p.Open("web1", 80, 24)
			term.Type([]byte("echo ready; exec cat\n"))
			term.WaitOutput("ready\n")

			f.fall(s.fwd)
			term.Type([]byte("across the drop\n"))
			term.WaitDropped()
			if a := p.Resume(term); a.Refused != "" {
				t.Fatalf("after %s the resume was refused: %s", f.name, *a.Message)
			}
			term.Type([]byte("after\n"))
			term.WaitOutput("after\n")
			if out := term.Live(); string(out) != "ready\nacross the drop\nafter\n" {
				t.Errorf("after %s the terminal got %q, want each line once", f.name, out)
			}
			waitServerLog(t, s.h, `msg="ssh session resumed" tunnel=`+term.ID+` client=browser tab=\S+ attachment=`+term.Attachment+` device_session=\S+ addr=\S+\n`)
			if f.attachWins && strings.Contains(s.h.Log(), `msg="web terminal dropped" tunnel=`+term.ID+` `) {
				t.Errorf("after %s the koko server dropped the attachment, want the resume to take it off the channel it still held:\n%s", f.name, s.h.Log())
			}
		})
	}
}
