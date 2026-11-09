package cli_test

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/server/webui"
	"github.com/cieons/koko/internal/shared/api"
)

// freePort returns a TCP port on 127.0.0.1 that nothing listens on and no
// other test takes meanwhile (harness.FreeAddr).
func freePort(t *testing.T) int {
	t.Helper()
	_, port, err := net.SplitHostPort(harness.FreeAddr(t))
	if err != nil {
		t.Fatal(err)
	}
	n, err := strconv.Atoi(port)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// kokoWeb is a running koko web, over the harness's Unix socket.
type kokoWeb struct {
	*harness.Process
	port int
	// printed is its stdout, and address the address in it:
	// http://localhost:<port>/?t=<token>.
	printed string
	address string
	token   string
}

// startWeb runs koko web --no-open --json on a free port and waits for its
// address.
func startWeb(t *testing.T, h *harness.Harness) *kokoWeb {
	t.Helper()
	return startWebOn(t, h, freePort(t))
}

// startWebOn runs koko web --no-open --json on port and waits for its
// address.
func startWebOn(t *testing.T, h *harness.Harness, port int) *kokoWeb {
	t.Helper()
	return startWebThrough(t, h.Background, port)
}

// startWebThrough runs koko web --no-open --json on port with background, the
// Local caller's or a device's, and waits for its address.
func startWebThrough(t *testing.T, background func(env []string, args ...string) *harness.Process, port int) *kokoWeb {
	t.Helper()
	p := background(nil, "web", "--port", strconv.Itoa(port), "--no-open", "--json")
	w := printedWeb(t, p.WaitStdout("}\n"))
	w.Process = p
	return w
}

// printedWeb reads what koko web --json printed: its address, and in it the
// port and the token.
func printedWeb(t *testing.T, out string) *kokoWeb {
	t.Helper()
	var printed struct {
		Address string `json:"address"`
	}
	if err := json.Unmarshal([]byte(out), &printed); err != nil {
		t.Fatalf("koko web printed %q, not its --json: %v", out, err)
	}
	u, err := url.Parse(printed.Address)
	if err != nil {
		t.Fatalf("koko web printed the address %q: %v", printed.Address, err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatalf("koko web printed the address %q, without its port: %v", printed.Address, err)
	}
	return &kokoWeb{port: port, printed: out, address: printed.Address, token: u.Query().Get("t")}
}

// browser is a web browser on this machine: it reaches koko web's port on
// 127.0.0.1, keeps the cookies it is given, as a browser's cookie store
// does, and follows no redirect, so a test sees each answer.
type browser struct {
	t      *testing.T
	port   int
	client *http.Client
}

func newBrowser(t *testing.T, port int) *browser {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	var d net.Dialer
	tr := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return d.DialContext(ctx, "tcp4", fmt.Sprintf("127.0.0.1:%d", port))
		},
	}
	t.Cleanup(tr.CloseIdleConnections)
	return &browser{t: t, port: port, client: &http.Client{
		Transport:     tr,
		Jar:           jar,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

// answer is a response with its body read.
type answer struct {
	*http.Response
	body string
}

// do sends a request for target, a path with its query, to
// http://localhost:<port>, and returns the answer, or nil and the error when
// none came.
func (b *browser) do(req *http.Request) (*answer, error) {
	b.t.Helper()
	resp, err := b.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return &answer{Response: resp, body: string(body)}, nil
}

// request makes a request for target, a path with its query, at
// http://localhost:<port>.
func (b *browser) request(method, target string, body io.Reader) *http.Request {
	b.t.Helper()
	req, err := http.NewRequestWithContext(b.t.Context(), method, fmt.Sprintf("http://localhost:%d%s", b.port, target), body)
	if err != nil {
		b.t.Fatal(err)
	}
	return req
}

// get sends GET target and fails the test when no answer comes.
func (b *browser) get(target string) *answer {
	b.t.Helper()
	a, err := b.do(b.request(http.MethodGet, target, nil))
	if err != nil {
		b.t.Fatalf("GET %s: %v", target, err)
	}
	return a
}

// open opens koko web's address, as a browser given it does: the token is
// traded for a cookie and a redirect to the same page without the token.
func (b *browser) open(w *kokoWeb) {
	b.t.Helper()
	a := b.get("/?t=" + w.token)
	if a.StatusCode != http.StatusSeeOther || a.Header.Get("Location") != "/" {
		b.t.Fatalf("opening the address answered %s, Location %q, %q; want 303 to /", a.Status, a.Header.Get("Location"), a.body)
	}
}

// koko web prints its address, with the one-time token, and serves the web
// UI there: the browser trades the token for a cookie that is HttpOnly and
// SameSite=Strict, and is redirected to the page without the token, which is
// the embedded web UI (the placeholder page in a build without it).
func TestWebPrintsItsAddressAndTradesItsTokenForACookie(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	w := startWeb(t, h)
	printed := strings.NewReplacer(w.token, "<token>", strconv.Itoa(w.port), "<port>").Replace(w.printed)
	harness.Golden(t, "web.json", printed)
	if len(w.token) < 26 {
		t.Errorf("the token %q is too short to be unguessable", w.token)
	}

	b := newBrowser(t, w.port)
	a := b.get("/?t=" + w.token)
	if a.StatusCode != http.StatusSeeOther || a.Header.Get("Location") != "/" {
		t.Fatalf("the address answered %s, Location %q; want 303 to /", a.Status, a.Header.Get("Location"))
	}
	cookies := a.Cookies()
	if len(cookies) != 1 {
		t.Fatalf("the trade set %d cookies, want 1: %v", len(cookies), a.Header.Values("Set-Cookie"))
	}
	c := cookies[0]
	if c.Name != fmt.Sprintf("koko_%d", w.port) || !c.HttpOnly || c.SameSite != http.SameSiteStrictMode || c.Path != "/" || c.Value == "" {
		t.Errorf("the cookie is %q, want koko_%d, HttpOnly, SameSite=Strict, Path=/", a.Header.Get("Set-Cookie"), w.port)
	}

	page := b.get("/")
	want, err := fs.ReadFile(webui.FS(), "index.html")
	if err != nil {
		t.Fatal(err)
	}
	if page.StatusCode != http.StatusOK || page.body != string(want) {
		t.Errorf("GET / with the cookie answered %s:\n%s\nwant the embedded web UI's index.html (web UI built: %t)", page.Status, page.body, webui.Built())
	}
	t.Logf("web UI built: %t", webui.Built())
	if !webui.Built() && !strings.Contains(page.body, "without its web UI") {
		t.Errorf("a build without the web UI serves %q, not the placeholder page", page.body)
	}
	// A bookmark of a page of the web UI opens the web UI, which routes it.
	if deep := b.get("/hosts?space=ops"); deep.StatusCode != http.StatusOK || deep.body != string(want) {
		t.Errorf("GET /hosts answered %s, want the web UI's index.html", deep.Status)
	}
}

// wantRefusedPage fails the test unless a is a refusal page with status that
// a person reads, saying what to do.
func wantRefusedPage(t *testing.T, a *answer, status int, says string) {
	t.Helper()
	if a.StatusCode != status || !strings.HasPrefix(a.Header.Get("Content-Type"), "text/plain") || !strings.Contains(a.body, says) {
		t.Errorf("answer %s (%s) %q, want %d saying %q", a.Status, a.Header.Get("Content-Type"), a.body, status, says)
	}
	if len(a.Cookies()) != 0 {
		t.Errorf("a refusal set a cookie: %v", a.Header.Values("Set-Cookie"))
	}
}

// postJSON sends an API call as the web UI does: a JSON POST, carrying an
// Idempotency-Key of its own, which a write needs and a read ignores.
func (b *browser) postJSON(path, body string) *answer {
	b.t.Helper()
	req := b.request(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(api.IdempotencyKeyHeader, api.NewIdempotencyKey())
	a, err := b.do(req)
	if err != nil {
		b.t.Fatalf("POST %s: %v", path, err)
	}
	return a
}

// wantAPIError fails the test unless a is the API's error object with code.
func wantAPIError(t *testing.T, a *answer, status int, code string) {
	t.Helper()
	var e struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(a.body), &e); err != nil || a.StatusCode != status || e.Error.Code != code || e.Error.Message == "" {
		t.Errorf("answer %s %q, want %d with the error code %s", a.Status, a.body, status, code)
	}
}

// A request with neither a valid token nor the cookie is refused: a page
// says to open koko web's address, and an API call gets cookie_required
// without reaching the API.
func TestWebRefusesARequestWithoutTokenOrCookie(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	w := startWeb(t, h)
	b := newBrowser(t, w.port)
	calls := h.Calls()

	wantRefusedPage(t, b.get("/"), http.StatusUnauthorized, "open the address koko web printed")
	wantRefusedPage(t, b.get("/assets/index.js"), http.StatusUnauthorized, "open the address koko web printed")
	wantRefusedPage(t, b.get("/?t=not-a-token"), http.StatusUnauthorized, "run koko web again")
	wantAPIError(t, b.postJSON("/v1/space.list", "{}"), http.StatusUnauthorized, "cookie_required")
	// web.start (calls before) is the only call the koko server answered.
	if n := h.Calls(); n != calls {
		t.Errorf("the koko server answered %d API calls for refused requests", n-calls)
	}

	// The same browser opens the address and is let in.
	b.open(w)
	if a := b.get("/"); a.StatusCode != http.StatusOK {
		t.Errorf("GET / after opening the address answered %s", a.Status)
	}
}

// The token works once. When someone else, such as another OS user who read
// it from the process list, spends it first, the user's own browser is
// refused and told why. A browser holding the cookie already is only sent on
// without the token.
func TestWebTokenWorksOnce(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	w := startWeb(t, h)
	other := newBrowser(t, w.port)
	other.open(w)

	mine := newBrowser(t, w.port)
	wantRefusedPage(t, mine.get("/?t="+w.token), http.StatusForbidden, "another user of this machine opened it first")
	wantRefusedPage(t, mine.get("/"), http.StatusUnauthorized, "open the address koko web printed")

	again := other.get("/?t=" + w.token + "&space=ops")
	if again.StatusCode != http.StatusSeeOther || again.Header.Get("Location") != "/?space=ops" || len(again.Cookies()) != 0 {
		t.Errorf("the browser with the cookie opening the address again got %s, Location %q; want 303 to /?space=ops", again.Status, again.Header.Get("Location"))
	}
	if a := other.get("/"); a.StatusCode != http.StatusOK {
		t.Errorf("the browser that spent the token got %s", a.Status)
	}
}

// The koko server answers only a Host header of localhost or 127.0.0.1 with
// koko web's own port: a DNS-rebound page's request, naming its own site,
// gets no answer at all.
func TestWebAnswersOnlyItsOwnHost(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	w := startWeb(t, h)
	b := newBrowser(t, w.port)
	port := strconv.Itoa(w.port)
	for _, host := range []string{"evil.example:" + port, "localhost", "localhost:1", "127.0.0.1:" + strconv.Itoa(w.port+1), "[::1]:" + port, "127.0.0.2:" + port, "localhost.:" + port} {
		req := b.request(http.MethodGet, "/?t="+w.token, nil)
		req.Host = host
		if a, err := b.do(req); err == nil {
			t.Errorf("Host %q got an answer: %s %q", host, a.Status, a.body)
		}
	}
	// The token is still unspent: 127.0.0.1 with the port is let in.
	req := b.request(http.MethodGet, "/?t="+w.token, nil)
	req.Host = "127.0.0.1:" + port
	if a, err := b.do(req); err != nil || a.StatusCode != http.StatusSeeOther {
		t.Fatalf("Host 127.0.0.1:%s: %v, %v; want the token traded", port, a, err)
	}
}

// Only a JSON POST reaches the API, and nothing sends a CORS header, so
// another site's page fails its preflight and a form it submits is refused.
func TestWebAPITakesOnlyAJSONPost(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	w := startWeb(t, h)
	b := newBrowser(t, w.port)
	b.open(w)
	var answers []*answer

	list := b.postJSON("/v1/space.list", "{}")
	answers = append(answers, list)
	if list.StatusCode != http.StatusOK || !strings.Contains(list.body, `"name":"`+harness.Owner+`"`) {
		t.Errorf("POST /v1/space.list answered %s %q, want the Spaces", list.Status, list.body)
	}

	get := b.get("/v1/space.list")
	answers = append(answers, get)
	wantAPIError(t, get, http.StatusMethodNotAllowed, "method_not_allowed")
	for _, contentType := range []string{"text/plain", "application/x-www-form-urlencoded", "multipart/form-data; boundary=x"} {
		req := b.request(http.MethodPost, "/v1/space.add", strings.NewReader(`{"name":"ops"}`))
		req.Header.Set("Content-Type", contentType)
		a, err := b.do(req)
		if err != nil {
			t.Fatal(err)
		}
		answers = append(answers, a)
		wantAPIError(t, a, http.StatusUnsupportedMediaType, "unsupported_media_type")
	}
	preflight := b.request(http.MethodOptions, "/v1/space.add", nil)
	preflight.Header.Set("Origin", "http://evil.example")
	preflight.Header.Set("Access-Control-Request-Method", "POST")
	preflight.Header.Set("Access-Control-Request-Headers", "content-type")
	a, err := b.do(preflight)
	if err != nil {
		t.Fatal(err)
	}
	answers = append(answers, a, b.get("/"))
	for _, a := range answers {
		for name := range a.Header {
			if strings.HasPrefix(name, "Access-Control-") {
				t.Errorf("%s %s answered with the CORS header %s", a.Request.Method, a.Request.URL.Path, name)
			}
		}
	}
	if r := h.Koko("space", "ls", "--json"); strings.Contains(r.Stdout, `"ops"`) {
		t.Errorf("a refused POST recorded a Space: %s", r.Stdout)
	}
}

// koko web's answers carry the headers the web listener's carry, but
// Strict-Transport-Security, so that both serve the web UI alike: its pages,
// the trade, its refusals and the API's answers.
func TestWebAnswersCarryTheSecurityHeaders(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	w := startWeb(t, h)
	b := newBrowser(t, w.port)
	answers := map[string]*answer{"a refused page": b.get("/"), "a refused call": b.postJSON("/v1/space.list", "{}")}
	answers["the trade"] = b.get("/?t=" + w.token)
	answers["a page"] = b.get("/hosts")
	answers["a call"] = b.postJSON("/v1/space.list", "{}")
	for name, a := range answers {
		for header, want := range map[string]string{
			"Content-Security-Policy":   "default-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'",
			"X-Content-Type-Options":    "nosniff",
			"Referrer-Policy":           "no-referrer",
			"Strict-Transport-Security": "",
		} {
			if got := a.Header.Get(header); got != want {
				t.Errorf("%s (%s): %s = %q, want %q", name, a.Status, header, got, want)
			}
		}
	}
}

// Over the Unix socket the cookie lasts only for that run of koko web: once
// it exits, the next run on the same port refuses the old cookie and the old
// token.
func TestWebCookieEndsWithTheRun(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	first := startWeb(t, h)
	b := newBrowser(t, first.port)
	b.open(first)
	if r := first.Stop(); r.Status != 0 || r.Stderr != "" {
		t.Fatalf("stopping koko web: exit status %d, stderr %q; want 0", r.Status, r.Stderr)
	}

	second := startWebOn(t, h, first.port)
	wantRefusedPage(t, b.get("/"), http.StatusUnauthorized, "open the address koko web printed")
	wantAPIError(t, b.postJSON("/v1/space.list", "{}"), http.StatusUnauthorized, "cookie_required")
	wantRefusedPage(t, newBrowser(t, first.port).get("/?t="+first.token), http.StatusUnauthorized, "comes from another run of koko web")

	b.open(second)
	if a := b.get("/"); a.StatusCode != http.StatusOK {
		t.Errorf("the second run's cookie got %s", a.Status)
	}
}

// A token nobody uses works for 5 minutes on the koko server's clock, as
// koko web's address says nothing of it, and is refused from then on.
func TestWebTokenWorksForFiveMinutes(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	w := startWeb(t, h)
	h.Advance(5*time.Minute - time.Second)
	newBrowser(t, w.port).open(w)

	late := startWeb(t, h)
	h.Advance(5 * time.Minute)
	wantRefusedPage(t, newBrowser(t, late.port).get("/?t="+late.token), http.StatusUnauthorized, "has expired")
}

// A port another process holds is refused, naming that process and
// --port, before any call: koko web never moves to another port. A browser
// may reach localhost on [::1] first, so a port held there only is refused
// too.
func TestWebRefusesATakenPort(t *testing.T) {
	t.Parallel()
	for _, l := range []struct{ network, addr string }{{"tcp4", "127.0.0.1:0"}, {"tcp6", "[::1]:0"}} {
		t.Run(l.network, func(t *testing.T) {
			h := harness.Start(t)
			var lc net.ListenConfig
			ln, err := lc.Listen(t.Context(), l.network, l.addr)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = ln.Close() }()
			port := strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)

			// In the background, so that a koko web that serves anyway fails
			// the test when Wait gives up, instead of running on.
			r := h.Background(nil, "web", "--port", port, "--no-open", "--json").Wait()
			msg := r.WantError(t, 1, "port_taken")
			for _, want := range []string{"port " + port, fmt.Sprintf("pid %d", os.Getpid()), "--port"} {
				if !strings.Contains(msg, want) {
					t.Errorf("message %q does not say %q", msg, want)
				}
			}
			if n := h.Calls(); n != 0 {
				t.Errorf("the koko server answered %d calls for a refused koko web", n)
			}
		})
	}
}

// --port takes a TCP port, and the command line is refused otherwise.
func TestWebPortMustBeATCPPort(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	for _, port := range []string{"0", "65536", "-1", "http", "5656x"} {
		h.Koko("web", "--port="+port, "--no-open", "--json").WantError(t, 2, "invalid_usage")
	}
	if n := h.Calls(); n != 0 {
		t.Errorf("the koko server answered %d calls", n)
	}
}

// Without --no-open, koko web opens its address with the OS's opener found
// on PATH (xdg-open on Linux, open on macOS); with it, it opens nothing.
// koko web waits for the opener before it exits, so each run's opener has
// done its work once the run is stopped.
func TestWebOpensItsAddressUnlessNoOpen(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	bin := t.TempDir()
	opened := filepath.Join(t.TempDir(), "opened")
	for _, opener := range []string{"xdg-open", "open"} {
		script := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' \"$@\" >> %q\n", opened)
		if err := os.WriteFile(filepath.Join(bin, opener), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	env := []string{"PATH=" + bin}

	quiet := h.Background(env, "web", "--port", strconv.Itoa(freePort(t)), "--no-open")
	quiet.WaitStdout("Ctrl-C")
	quiet.Stop()
	if _, err := os.Stat(opened); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("koko web --no-open ran the opener: %v", err)
	}

	p := h.Background(env, "web", "--port", strconv.Itoa(freePort(t)))
	out := p.WaitStdout("Ctrl-C")
	address := regexp.MustCompile(`http://localhost:\d+/\?t=\S+`).FindString(out)
	if address == "" {
		t.Fatalf("koko web printed no address: %q", out)
	}
	if r := p.Stop(); r.Status != 0 || r.Stderr != "" {
		t.Errorf("exit status %d, stderr %q", r.Status, r.Stderr)
	}
	if got, err := os.ReadFile(opened); err != nil || string(got) != address+"\n" {
		t.Errorf("the opener was given %q (%v), want the address %q", got, err, address)
	}
}

// Without an opener, koko web says so and serves on: the address it printed
// still works.
func TestWebServesOnWhenNoBrowserOpens(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	p := h.Background([]string{"PATH=" + t.TempDir()}, "web", "--port", strconv.Itoa(freePort(t)), "--json")
	w := printedWeb(t, p.WaitStdout("}\n"))
	newBrowser(t, w.port).open(w)
	r := p.Stop()
	if r.Status != 0 || !strings.Contains(r.Stderr, "could not open a browser") {
		t.Errorf("exit status %d, stderr %q; want 0 and a warning", r.Status, r.Stderr)
	}
}

// koko web ends, with server_unreachable, when the koko server goes away:
// its cookie no longer works anyway.
func TestWebEndsWhenTheKokoServerStops(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	w := startWeb(t, h)
	h.Stop()
	r := w.Wait()
	if r.Status != 1 || !strings.Contains(r.Stderr, `"server_unreachable"`) || !strings.Contains(r.Stderr, "run koko web again") {
		t.Errorf("exit status %d, stderr %q; want 1 with server_unreachable", r.Status, r.Stderr)
	}
}

// The token and the cookie appear in no log line, error or output, except
// the address koko web prints.
func TestWebKeepsItsTokenAndCookieOutOfOutput(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	w := startWeb(t, h)
	b := newBrowser(t, w.port)
	var bodies []string
	a := b.get("/?t=" + w.token)
	cookie := a.Cookies()[0].Value
	bodies = append(bodies,
		a.body,
		b.get("/?t="+w.token).body,
		newBrowser(t, w.port).get("/?t="+w.token).body,
		newBrowser(t, w.port).get("/").body,
		b.postJSON("/v1/space.list", "{}").body,
		b.postJSON("/v1/space.add", `{"name":"Bad"}`).body,
	)
	r := w.Stop()
	if strings.Count(r.Stdout, w.token) != 1 || strings.Contains(r.Stdout, cookie) {
		t.Errorf("stdout %q holds the token other than once in the address, or the cookie", r.Stdout)
	}
	for where, text := range map[string]string{"koko web's stderr": r.Stderr, "the koko server's log": h.Log(), "the answers": strings.Join(bodies, "\n")} {
		if strings.Contains(text, w.token) || strings.Contains(text, cookie) {
			t.Errorf("%s holds the token or the cookie:\n%s", where, text)
		}
	}
}

// A token works only through the run of koko web that printed it: opened on
// another run's port, it is refused and stays unspent.
func TestWebTokenWorksOnlyThroughItsOwnRun(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	mine, other := startWeb(t, h), startWeb(t, h)
	b := newBrowser(t, other.port)
	wantRefusedPage(t, b.get("/?t="+mine.token), http.StatusUnauthorized, "comes from another run of koko web")
	newBrowser(t, mine.port).open(mine)
}

// A koko built without the web UI's output, as a plain go build is, serves
// the placeholder page over web@koko. make check builds the web UI before the
// tests, so this runs TestWebPrintsItsAddressAndTradesItsTokenForACookie
// again in a build whose overlay hides the build output.
func TestWebServesThePlaceholderInABuildWithoutTheWebUI(t *testing.T) {
	t.Parallel()
	hide := map[string]string{}
	err := filepath.WalkDir("../../server/webui/site/app", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		abs, err := filepath.Abs(path)
		hide[abs] = "" // an empty replacement deletes the file
		return err
	})
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("list the build output: %v", err)
	}
	overlay, err := json.Marshal(map[string]any{"Replace": hide})
	if err != nil {
		t.Fatal(err)
	}
	overlayFile := filepath.Join(t.TempDir(), "overlay.json")
	if err := os.WriteFile(overlayFile, overlay, 0o644); err != nil {
		t.Fatal(err)
	}
	const test = "TestWebPrintsItsAddressAndTradesItsTokenForACookie"
	out, err := exec.CommandContext(t.Context(), "go", "test", "-count=1", "-overlay", overlayFile, "-run", "^"+test+"$", "-v", ".").CombinedOutput()
	if err != nil {
		t.Fatalf("%s in a build without the web UI: %v\n%s", test, err, out)
	}
	if !bytes.Contains(out, []byte("web UI built: false")) || !bytes.Contains(out, []byte("--- PASS: "+test)) {
		t.Errorf("%s did not pass in a build without the web UI:\n%s", test, out)
	}
}
