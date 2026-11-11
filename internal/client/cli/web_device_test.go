package cli_test

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/shared/api"
	"github.com/cieons/koko/internal/shared/timing"
)

// webDevice is a koko server reached through a forwarder, on a data
// directory and Root Key that a second koko server can start on, and the
// Owner's device joined to it.
type webDevice struct {
	h                *harness.Harness
	fwd              *harness.Forwarder
	dataDir, rootKey string
	d                *harness.Device
}

// startWebDevice starts a webDevice whose device goes through a Device Mux
// when mux is true, and direct otherwise.
func startWebDevice(t *testing.T, mux bool) *webDevice {
	t.Helper()
	s := &webDevice{dataDir: t.TempDir(), rootKey: harness.NewRootKeyFile(t)}
	s.fwd = harness.Forward(t, s.start(t))
	init, err := s.h.Call(api.ServerInit, api.ServerInitRequest{Owner: harness.Owner, Address: s.fwd.Addr()})
	if err != nil {
		t.Fatalf("server.init: %v", err)
	}
	if mux {
		s.d = harness.NewMuxDevice(t, timing.Timings{})
	} else {
		s.d = harness.NewDevice(t)
	}
	s.d.Koko("join", init.JoinCode.Code).WantSuccess(t)
	return s
}

// restart stops the koko server and starts another on its data directory,
// which the forwarder then leads to, as an upgrade's restart does.
func (s *webDevice) restart(t *testing.T) {
	t.Helper()
	s.h.Stop()
	s.fwd.To(s.start(t))
}

// start starts a koko server on the data directory and returns the address
// it listens on.
func (s *webDevice) start(t *testing.T) string {
	t.Helper()
	listen := harness.FreeAddr(t)
	s.h = harness.Start(t, harness.NoInit(), harness.Listen(listen), harness.DataDir(s.dataDir), harness.RootKeyFile(s.rootKey))
	return listen
}

// wantServed fails the test unless the browser, with the cookie it holds and
// no token, gets the web UI and an API answer within 10 s, while what it
// goes through may still be reconnecting.
func wantServed(t *testing.T, b *browser, after string) {
	t.Helper()
	eventually(t, "the cookie gets the web UI "+after, func() bool {
		a, err := b.do(b.request(http.MethodGet, "/", nil))
		return err == nil && a.StatusCode == http.StatusOK
	})
	if a := b.postJSON("/v1/space.list", "{}"); a.StatusCode != http.StatusOK {
		t.Errorf("the cookie's API call %s answered %s %q", after, a.Status, a.body)
	}
}

// On a joined device the cookie is bound to the Device Session: a browser
// that holds it opens the web UI with no token after koko web restarts on
// its port, after a Device Connection drop, its browser connections
// reopening as new channels, and after a koko server restart on the same
// data directory. koko web runs on through the drop and the restart.
func TestWebOnAJoinedDeviceKeepsItsCookie(t *testing.T) {
	t.Parallel()
	s := startWebDevice(t, true)
	port := freePort(t)
	w := startWebThrough(t, s.d.Background, port)
	b := newBrowser(t, port)
	b.open(w)
	wantServed(t, b, "it was given")
	token, cookie := w.token, b.cookie()

	first := w.Stop()
	if first.Status != 0 {
		t.Fatalf("koko web exited %d at Ctrl-C: %q", first.Status, first.Stderr)
	}
	w = startWebThrough(t, s.d.Background, port)
	wantServed(t, b, "after koko web restarted")

	s.fwd.CutWhen(func() bool { return true })
	_, _ = b.do(b.request(http.MethodGet, "/", nil)) // its answer falls to the cut
	waitLogs(t, s.d, `msg="device connection opened"`, 2)
	wantServed(t, b, "after a Device Connection drop")

	// The Device Mux reconnects to the new koko server once the browser's
	// next request waits for it.
	before := s.h
	s.restart(t)
	wantServed(t, b, "after a koko server restart")
	waitLogs(t, s.d, `msg="device connection opened"`, 3)

	r := w.Stop()
	if r.Status != 0 {
		t.Errorf("koko web exited %d at Ctrl-C, after the drop and the restart: %q", r.Status, r.Stderr)
	}
	for where, text := range map[string]string{
		"koko web's stderr": first.Stderr + r.Stderr, "the second koko web's stdout": r.Stdout,
		"the Device Mux's log": s.d.MuxLog(), "the koko servers' logs": before.Log() + s.h.Log(),
	} {
		if strings.Contains(text, token) || strings.Contains(text, cookie) {
			t.Errorf("%s holds the token or the cookie:\n%s", where, text)
		}
	}
}

// A cookie and a token let in only their own subject. On one port, in turn:
// a Local run's cookie is refused through a device's koko web, and that
// device's cookie through another device's, as when two OS users of a
// machine share a browser; another device's token is refused and stays
// unspent for its own.
func TestWebCookieAndTokenWorkOnlyForTheirOwnDeviceSession(t *testing.T) {
	t.Parallel()
	s := startWebDevice(t, true)
	other := s.h.Join(harness.Owner)
	port := freePort(t)
	b := newBrowser(t, port)

	local := startWebThrough(t, s.h.Background, port)
	b.open(local)
	local.Stop()
	mine := startWebThrough(t, s.d.Background, port)
	wantAPIError(t, b.postJSON("/v1/space.list", "{}"), http.StatusUnauthorized, api.CodeCookieRequired.Name)
	b.open(mine)
	spare := startWebThrough(t, s.d.Background, freePort(t))
	mine.Stop()

	theirs := startWebThrough(t, other.Background, port)
	wantRefusedPage(t, b.get("/"), http.StatusUnauthorized, "open the address koko web printed")
	wantAPIError(t, b.postJSON("/v1/space.list", "{}"), http.StatusUnauthorized, api.CodeCookieRequired.Name)
	wantRefusedPage(t, newBrowser(t, port).get("/?t="+spare.token), http.StatusUnauthorized, "comes from another run of koko web")
	b.open(theirs)
	newBrowser(t, spare.port).open(spare)
}

// cookie returns the value of the one cookie the browser holds.
func (b *browser) cookie() string {
	b.t.Helper()
	cookies := b.client.Jar.Cookies(b.request(http.MethodGet, "/", nil).URL)
	if len(cookies) != 1 {
		b.t.Fatalf("the browser holds %d cookies, want 1", len(cookies))
	}
	return cookies[0].Value
}

// heldCall is an API call through koko web that the koko server is
// answering: it has read the call's headers and waits for its body. A
// Revoked Device Session's Device Connection stays open while a call on it
// is in flight, so a test holds one to reach the koko server after the
// revocation commits, as a browser's request on its way then does. The
// connection stays open for at most the transport's shutdownGrace (5 s),
// far beyond the few requests a test makes meanwhile.
type heldCall struct {
	t    *testing.T
	conn net.Conn
	r    *bufio.Reader
}

// holdCall starts an API call through koko web on port, with the browser
// b's cookie, and returns once the koko server has asked for its body.
func holdCall(t *testing.T, b *browser, port int) *heldCall {
	t.Helper()
	var d net.Dialer
	conn, err := d.DialContext(t.Context(), "tcp4", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	_, err = fmt.Fprintf(conn, "POST /v1/space.list HTTP/1.1\r\nHost: localhost:%d\r\nCookie: koko_%d=%s\r\n"+
		"Content-Type: application/json\r\nContent-Length: 2\r\nExpect: 100-continue\r\n\r\n", port, port, b.cookie())
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	r := bufio.NewReader(conn)
	if line, err := r.ReadString('\n'); err != nil || !strings.HasPrefix(line, "HTTP/1.1 100 ") {
		t.Fatalf("the held call got %q (%v), want 100 Continue", line, err)
	}
	if _, err := r.ReadString('\n'); err != nil { // the blank line after it
		t.Fatal(err)
	}
	return &heldCall{t: t, conn: conn, r: r}
}

// finish sends the held call's body and reads its answer's status line.
func (c *heldCall) finish() {
	c.t.Helper()
	if _, err := c.conn.Write([]byte("{}")); err != nil {
		c.t.Fatal(err)
	}
	if _, err := c.r.ReadString('\n'); err != nil {
		c.t.Fatalf("the held call got no answer: %v", err)
	}
	_ = c.conn.Close()
}

// Revoking the Device Session koko web runs as, from its own web UI, stops
// the cookie at once, however soon the browser asks next: a page says the
// Device Session was Revoked, and an API call is answered revoked. koko web
// then exits 1 with revoked, and the Device Mux has deleted the device's key
// and client config.
func TestRevokingTheDeviceFromItsWebUIEndsTheCookieAndKokoWeb(t *testing.T) {
	t.Parallel()
	s := startWebDevice(t, true)
	port := freePort(t)
	w := startWebThrough(t, s.d.Background, port)
	b := newBrowser(t, port)
	b.open(w)
	wantServed(t, b, "it was given")
	held := holdCall(t, b, port)

	device := readClientConfig(t, s.d).Device
	if a := b.postJSON("/v1/device.revoke", fmt.Sprintf(`{"device": %q, "last": true}`, device)); a.StatusCode != http.StatusOK {
		t.Fatalf("device.revoke from the web UI answered %s %q", a.Status, a.body)
	}
	wantRefusedPage(t, b.get("/"), http.StatusUnauthorized, "Device Session was revoked")
	wantAPIError(t, b.postJSON("/v1/space.list", "{}"), http.StatusUnauthorized, api.CodeRevoked.Name)
	held.finish()

	r := w.Wait()
	if r.Status != 1 || !strings.Contains(r.Stderr, `"revoked"`) {
		t.Errorf("koko web exited %d, stderr %q; want 1 with revoked", r.Status, r.Stderr)
	}
	wantGone(t, s.d, "device_key", "client.json", "known_hosts")
}

// koko web on the direct path, its own Device Connection beneath it, runs
// the revocation cleanup itself when the koko server revokes its Device
// Session, and exits 1 with revoked, saying the device's key and client
// config are deleted.
func TestWebOnTheDirectPathRunsTheRevocationCleanup(t *testing.T) {
	t.Parallel()
	s := startWebDevice(t, false)
	w := startWebThrough(t, s.d.Background, freePort(t))
	device := readClientConfig(t, s.d).Device

	deviceRevoke(t, s.h.Koko, harness.Owner+"/"+device, "--last")
	r := w.Wait()
	if r.Status != 1 || !strings.Contains(r.Stderr, `"revoked"`) || !strings.Contains(r.Stderr, "deleted this device's key and client config") {
		t.Errorf("koko web exited %d, stderr %q; want 1 with revoked, saying the device's files are deleted", r.Status, r.Stderr)
	}
	wantGone(t, s.d, "device_key", "client.json", "known_hosts")
	if rec := readRevocation(t, s.d); rec.Device != device {
		t.Errorf("the revocation record is %+v, want the device %s", rec, device)
	}
}

// koko disconnect ends a running koko web and counts it among the commands
// it ended; koko web says koko disconnect stopped it, and exits 0, as at
// Ctrl-C.
func TestKokoDisconnectEndsKokoWebAndCountsIt(t *testing.T) {
	t.Parallel()
	_, _, d := muxDevice(t, timing.Timings{})
	w := startWebThrough(t, d.Background, freePort(t))

	if out, _ := kokoDisconnect(t, d); out.CommandsEnded != 1 {
		t.Errorf("koko disconnect ended %d commands, want koko web's 1", out.CommandsEnded)
	}
	if r := w.Wait(); r.Status != 0 || r.Stderr != "koko: koko disconnect stopped koko web.\n" {
		t.Errorf("koko web exited %d, stderr %q; want 0, saying koko disconnect stopped it", r.Status, r.Stderr)
	}
}

// A running koko web keeps the Device Mux from idling out, with no browser
// connection open, and the Device Mux idles out once it stops.
func aRunningKokoWebKeepsTheDeviceMuxRunning(t *testing.T) {
	const idle = 300 * time.Millisecond
	_, fwd, d, held := idlingMuxDevice(t, timing.Timings{DeviceMuxIdle: idle})
	conns, stops := fwd.Conns(), idleOuts(d)
	w := startWebThrough(t, d.Background, freePort(t))
	_ = held.Close()
	time.Sleep(3 * idle)
	if idleOuts(d) != stops {
		t.Fatalf("the Device Mux stopped under a running koko web; it logged:\n%s", d.MuxLog())
	}
	newBrowser(t, w.port).open(w)
	if n := fwd.Conns(); n != conns {
		t.Errorf("the device made %d connections, want the %d it had", n, conns)
	}

	w.Stop()
	waitLogs(t, d, idledOut, stops+1)
}
