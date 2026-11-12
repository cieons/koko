package cli_test

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/shared/api"
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
// caller: the WebSocket's upgrade and its frames pass through koko web's
// forwarder as they are. Another Origin is refused before the upgrade, and
// a page without the cookie in its first answer. The run's terminals end
// with it.
func TestAWebTerminalThroughKokoWeb(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	tunnelHost(t, h, "web1")
	w := startWeb(t, h)
	p := webTerminalPage(t, w)

	term := p.Open("web1", 80, 24)
	term.Type([]byte("echo ready; exec cat\n"))
	term.WaitOutput("ready\n")
	data := randomBytes(256 << 10)
	typed := make(chan error, 1)
	go func() {
		_, err := term.Write(data)
		typed <- err
	}()
	if out := term.WaitOutputLen(len("ready\n") + len(data)); !bytes.Equal(out[len("ready\n"):], data) {
		t.Error("the bytes cat echoed through koko web are not those sent")
	}
	if err := <-typed; err != nil {
		t.Errorf("send 256 KiB: %v", err)
	}
	waitServerLog(t, h, `msg="ssh session started" tunnel=`+term.ID+` client=browser user=alice device_session="unix socket" host=web1 host_id=host_\w+ addr=\S+\n`)

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
	_, a := none.TryOpen("web1", 80, 24)
	if a.Refused != "cookie_required" {
		t.Errorf("a page without the cookie was answered %+v, want cookie_required", a)
	}

	another := webTerminalPage(t, startWeb(t, h))
	if _, a, _ := another.Exchange(wire.TerminalFirst{Protocol: wire.TerminalProtocol, Resume: term.ID}); a.Refused != wire.TerminalResumeRefused || a.Message == nil || *a.Message != "" {
		t.Errorf("another run of koko web resuming the terminal was answered %+v, want resume_refused with no message", a)
	}
	if a, _ := another.End(term.ID); !a.Ended {
		t.Errorf("another run's end was answered %+v", a)
	}
	term.Type([]byte("still\n"))
	term.WaitOutput("still\n")

	w.Stop()
	waitServerLog(t, h, `msg="ssh session ended" tunnel=`+term.ID+` client=browser .* reason="koko web ended"\n`)
}

// On a joined device, koko web's web terminal acts as its Device Session,
// which counts it beside the CLI's Tunnels: with 64 open, the next is
// refused through koko web, without a connection to the Host.
func TestKokoWebsTerminalCountsAgainstItsDeviceSession(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	host := tunnelHost(t, h, "web1")
	pinned := gatewayHostKey(t, h)
	laptop := h.Join(harness.Owner)
	open := make([]*ssh.Client, 64)
	for i := range open {
		open[i] = sshOver(t, laptop.Proxy(nil, "tunnel", "web1"), "web1", pinned)
	}
	p := webTerminalPage(t, startWebThrough(t, laptop.Background, freePort(t)))
	conns := host.Conns()
	_, a := p.TryOpen("web1", 80, 24)
	if a.Refused != wire.TerminalSSHSessionLimit || a.Message == nil ||
		*a.Message != "this Device Session already has 64 SSH Sessions open, the most koko allows; this SSH Session did not start." {
		t.Errorf("the 65th SSH Session, through koko web, was answered %+v, want the limit's refusal", a)
	}
	if host.Conns() != conns {
		t.Error("the 65th SSH Session dialled the Host")
	}
	waitServerLog(t, h, `msg="ssh session refused" tunnel=[0-9a-f]{32} client=browser name=web1 device_session=\S+ addr=\S+ reason="ssh session limit"\n`)
	_ = open[0].Close()
	eventually(t, "a terminal opens through koko web once one of the 64 ended", func() bool {
		w, _ := p.TryOpen("web1", 80, 24)
		return w != nil
	})
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
	if out := string(term.Output()); out != "\r\nkoko: "+notice+"\r\n" {
		t.Errorf("the terminal got %q, want the notice on a line of its own", out)
	}
	waitServerLog(t, h, `msg="ssh session ended" tunnel=`+term.ID+` client=browser .* reason=revoked\n`)
}

// A web terminal's grace is its own: with timing.BrowserGrace short, a
// Detached web terminal ends at it, while a Tunnel of the CLI's, Detached
// beside it, is held for its own grace, timing.Grace, and Resumes once its
// device can reconnect.
func aBrowsersGraceLeavesATunnelsAsItIs(t *testing.T) {
	listen := harness.FreeAddr(t)
	timings := resumeServer
	timings.BrowserGrace = 500 * time.Millisecond
	h := harness.Start(t, harness.NoInit(), harness.Listen(listen), harness.WebListen("127.0.0.1:0"), harness.Timings(timings))
	fwd := harness.Forward(t, listen)
	init, err := h.Call(api.ServerInit, api.ServerInitRequest{Owner: harness.Owner, Address: fwd.Addr()})
	if err != nil {
		t.Fatalf("server.init: %v", err)
	}
	tunnelHost(t, h, "web1")
	d := harness.NewMuxDevice(t, resumeDevice)
	d.Koko("join", init.JoinCode.Code).WantSuccess(t)
	say := shell(t, sshOver(t, d.Proxy(nil, "tunnel", "web1.koko"), "web1.koko", gatewayHostKey(t, h)))
	if got := say("echo one"); got != "one\n" {
		t.Fatalf("the shell printed %q, want one", got)
	}
	tunnel := waitServerLog(t, h, `msg="ssh session started" tunnel=(\S+) client=ssh `)[1]
	term := h.SignIn(harness.Owner).TerminalPage().Open("web1", 80, 24)

	// The device finds nothing to reconnect to while the terminal's grace
	// runs out.
	fwd.To(recordTries(t).addr())
	fwd.CutWhen(func() bool { return true })
	waitServerLog(t, h, `msg="ssh session detached" tunnel=`+tunnel+` client=ssh `)
	term.Drop()
	waitServerLog(t, h, `msg="ssh session ended" tunnel=`+term.ID+` client=browser .* reason="grace over"\n`)
	if strings.Contains(h.Log(), `msg="ssh session ended" tunnel=`+tunnel) {
		t.Fatalf("the Tunnel ended at the web terminal's grace:\n%s", h.Log())
	}
	fwd.To(listen)
	if got := say("echo two"); got != "two\n" {
		t.Errorf("once its device reconnected the shell printed %q, want two", got)
	}
	waitServerLog(t, h, `msg="ssh session resumed" tunnel=`+tunnel+` client=ssh `)
}

// Through koko web on a joined device, a dropped Device Connection ends the
// web terminal's WebSocket on it: after a cut the koko server Detaches its
// SSH Session, after a stall the device's new Device Connection wins over
// the one the koko server still holds. Either way the page Resumes through
// koko web once the Device Mux has reconnected, byte for byte.
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
			if out := term.WaitOutput("after\n"); string(out) != "ready\nacross the drop\nafter\n" {
				t.Errorf("after %s the terminal got %q, want each line once", f.name, out)
			}
			waitServerLog(t, s.h, `msg="ssh session resumed" tunnel=`+term.ID+` client=browser device_session=\S+ addr=\S+\n`)
			detached := strings.Contains(s.h.Log(), `msg="ssh session detached" tunnel=`+term.ID+` `)
			if detached == f.attachWins {
				t.Errorf("after %s the SSH Session was Detached: %t, want %t", f.name, detached, !f.attachWins)
			}
		})
	}
}
