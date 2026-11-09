package web_test

import (
	"bufio"
	"encoding/json/v2"
	"io"
	"net"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/cieons/koko/internal/harness"
)

// webURL is the Web URL these tests set, as a reverse proxy would serve it.
const webURL = "https://koko.example.com"

// startListening starts a koko server with its web listener on a loopback
// port, and, unless webURL is empty, sets its Web URL.
func startListening(t *testing.T, webURL string, opts ...harness.Option) *harness.Harness {
	t.Helper()
	h := harness.Start(t, append([]harness.Option{harness.WebListen("127.0.0.1:0")}, opts...)...)
	if h.WebAddress() == "" {
		t.Fatal("the web listener is off though WebListen turned it on")
	}
	if webURL != "" {
		h.Koko("server", "set", "web_url="+webURL).WantSuccess(t)
	}
	return h
}

// answer is one answer of the web listener, its body read.
type answer struct {
	*http.Response
	body string
}

// request is one request a reverse proxy passes on to the web listener.
type request struct {
	method, path, host, origin, body string
	// forwardedFor is the X-Forwarded-For header, when set.
	forwardedFor string
}

// send sends r to the web listener of h, as the reverse proxy would, and
// reads the answer.
func send(t *testing.T, h *harness.Harness, r request) answer {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), r.method, "http://"+h.WebAddress()+r.path, strings.NewReader(r.body))
	if err != nil {
		t.Fatal(err)
	}
	req.Host = r.host
	if r.origin != "" {
		req.Header.Set("Origin", r.origin)
	}
	if r.forwardedFor != "" {
		req.Header.Set("X-Forwarded-For", r.forwardedFor)
	}
	if r.method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
	}
	tr := &http.Transport{}
	defer tr.CloseIdleConnections()
	resp, err := (&http.Client{Transport: tr}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return answer{Response: resp, body: string(b)}
}

// get is a GET of path with the Host header host.
func get(t *testing.T, h *harness.Harness, host, path string) answer {
	t.Helper()
	return send(t, h, request{method: http.MethodGet, path: path, host: host})
}

// call is an API call, a POST of a JSON body, from origin.
func call(t *testing.T, h *harness.Harness, operation, origin string) answer {
	t.Helper()
	return send(t, h, request{method: http.MethodPost, path: "/v1/" + operation, host: "koko.example.com", origin: origin, body: "{}"})
}

// wantPage fails the test unless a is a page with status that says each of
// says, which no cache keeps and which sets no cookie.
func wantPage(t *testing.T, a answer, status int, says ...string) {
	t.Helper()
	if a.StatusCode != status {
		t.Errorf("answered %s %q, want %d", a.Status, a.body, status)
	}
	if ct := a.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("Content-Type = %q, want a page of text", ct)
	}
	for _, s := range says {
		if !strings.Contains(a.body, s) {
			t.Errorf("the page %q does not say %q", a.body, s)
		}
	}
	wantRefusalHeaders(t, a)
}

// wantAPIError fails the test unless a is the API's error object with
// status and code.
func wantAPIError(t *testing.T, a answer, status int, code string) {
	t.Helper()
	var e struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(a.body), &e); err != nil || a.StatusCode != status || e.Error.Code != code {
		t.Errorf("answered %s %q, want %d with the code %s", a.Status, a.body, status, code)
	}
	wantRefusalHeaders(t, a)
}

// wantRefusalHeaders fails the test unless a, a refusal, carries every
// answer's security headers and no-store, and sets no cookie.
func wantRefusalHeaders(t *testing.T, a answer) {
	t.Helper()
	wantSecurityHeaders(t, a.Header)
	if cc := a.Header.Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", cc)
	}
	if c := a.Header.Values("Set-Cookie"); len(c) > 0 {
		t.Errorf("the refusal sets a cookie: %q", c)
	}
}

// wantSecurityHeaders fails the test unless h holds the headers every
// answer of the web listener carries.
func wantSecurityHeaders(t *testing.T, h http.Header) {
	t.Helper()
	for name, want := range map[string]string{
		"Content-Security-Policy":   "default-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'",
		"X-Content-Type-Options":    "nosniff",
		"Referrer-Policy":           "no-referrer",
		"Strict-Transport-Security": "max-age=31536000",
	} {
		if got := h.Get(name); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
}

// Without --web-listen, the koko server listens on nothing new, and its
// start log says the web listener is off.
func TestTheWebListenerIsOffByDefault(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	if h.WebAddress() != "" {
		t.Errorf("the web listener listens on %s without --web-listen", h.WebAddress())
	}
	if !strings.Contains(h.Log(), `msg="web listener" listen=off`) {
		t.Errorf("the start log does not say the web listener is off:\n%s", h.Log())
	}
}

// Before initialisation, every request is answered 503 with a page that
// says so, on a fresh koko server too; the Host and Origin it carries are
// not looked at, since there is no Web URL to check them against.
func TestTheWebListenerBeforeInitialisation(t *testing.T) {
	t.Parallel()
	h := startListening(t, "", harness.NoInit())
	for _, r := range []request{
		{method: http.MethodGet, path: "/", host: "koko.example.com"},
		{method: http.MethodGet, path: "/hosts", host: "another.example"},
		{method: http.MethodPost, path: "/v1/user.current", host: "koko.example.com", body: "{}"},
	} {
		wantPage(t, send(t, h, r), http.StatusServiceUnavailable, "not initialised")
	}
	if !regexp.MustCompile(`msg="web listener" listen=127\.0\.0\.1:\d+ web_url="none`).MatchString(h.Log()) {
		t.Errorf("the start log does not name the web listener and say there is no Web URL:\n%s", h.Log())
	}
}

// Until the Web URL is set, every request is answered 503 with a page
// naming the command that sets it; once it is cleared, again.
func TestTheWebListenerWithoutAWebURL(t *testing.T) {
	t.Parallel()
	h := startListening(t, "")
	says := []string{"no Web URL", "koko server set web_url=https://"}
	wantPage(t, get(t, h, "koko.example.com", "/"), http.StatusServiceUnavailable, says...)
	wantPage(t, call(t, h, "user.current", webURL), http.StatusServiceUnavailable, says...)

	h.Koko("server", "set", "web_url="+webURL).WantSuccess(t)
	if a := get(t, h, "koko.example.com", "/"); a.StatusCode != http.StatusOK {
		t.Fatalf("with a Web URL, / answered %s", a.Status)
	}
	h.Koko("server", "set", "--unset", "web_url").WantSuccess(t)
	wantPage(t, get(t, h, "koko.example.com", "/"), http.StatusServiceUnavailable, says...)
}

// A request must name the Web URL's host, with its port, 443 written or
// not; any other is answered 421 with a page naming the Web URL and never
// the Host the request carried. A change of the Web URL takes effect on the
// next request.
func TestTheWebListenerChecksTheHost(t *testing.T) {
	t.Parallel()
	h := startListening(t, webURL)
	for _, host := range []string{"koko.example.com", "koko.example.com:443", "Koko.Example.COM"} {
		a := get(t, h, host, "/")
		if a.StatusCode != http.StatusOK || !strings.Contains(a.body, "<") {
			t.Errorf("Host %s: answered %s %q, want the web UI", host, a.Status, a.body)
		}
		wantSecurityHeaders(t, a.Header)
	}
	for _, host := range []string{"koko.example.com:8443", "attacker.example", "127.0.0.1", "koko.example.com.attacker.example"} {
		a := get(t, h, host, "/")
		wantPage(t, a, http.StatusMisdirectedRequest, webURL, "Host header")
		if strings.Contains(a.body, host) && host != "koko.example.com:8443" {
			t.Errorf("Host %s: the page names the Host the request carried: %q", host, a.body)
		}
	}

	h.Koko("server", "set", "web_url=https://koko.example.com:8443").WantSuccess(t)
	wantPage(t, get(t, h, "koko.example.com", "/"), http.StatusMisdirectedRequest, "https://koko.example.com:8443")
	if a := get(t, h, "koko.example.com:8443", "/"); a.StatusCode != http.StatusOK {
		t.Errorf("Host koko.example.com:8443 under its Web URL answered %s", a.Status)
	}
}

// Every POST must carry an Origin header equal to the Web URL, on /v1/, on
// each /auth/ endpoint and on any other path; another, or none, is refused
// with bad_origin before the API is reached or anything is counted. With
// the Web URL's Origin, a call without a Sign-in is answered
// signin_required, without reaching the API.
func TestTheWebListenerChecksTheOriginOfEveryPost(t *testing.T) {
	t.Parallel()
	h := startListening(t, webURL)
	calls := h.Calls()
	for _, origin := range []string{"", "https://attacker.example", "http://koko.example.com", "https://koko.example.com:8443", "null"} {
		wantAPIError(t, call(t, h, "user.current", origin), http.StatusForbidden, "bad_origin")
		for _, path := range []string{"/auth/signin", "/auth/link", "/auth/password", "/auth/signout"} {
			a := send(t, h, request{method: http.MethodPost, path: path, host: "koko.example.com", origin: origin, body: "{}"})
			wantAPIError(t, a, http.StatusForbidden, "bad_origin")
		}
		a := send(t, h, request{method: http.MethodPost, path: "/hosts", host: "koko.example.com", origin: origin, body: "{}"})
		wantPage(t, a, http.StatusForbidden, "no page of "+webURL)
	}
	// Refused before anything was counted: a sign-in from the same address
	// is not limited.
	a := send(t, h, request{
		method: http.MethodPost, path: "/auth/signin", host: "koko.example.com", origin: webURL,
		body: `{"name": "alice", "password": "not the password at all"}`,
	})
	wantAPIError(t, a, http.StatusUnauthorized, "wrong_password")
	wantAPIError(t, call(t, h, "user.current", webURL), http.StatusUnauthorized, "signin_required")
	wantAPIError(t, call(t, h, "server.show", webURL), http.StatusUnauthorized, "signin_required")
	if n := h.Calls() - calls; n != 0 {
		t.Errorf("the web listener made %d API calls, want none", n)
	}
}

// The Origin check refuses a POST from its headers alone: the answer comes
// though the body never does.
func TestTheWebListenerRefusesABadOriginBeforeReadingTheBody(t *testing.T) {
	t.Parallel()
	h := startListening(t, webURL)
	var d net.Dialer
	conn, err := d.DialContext(t.Context(), "tcp", h.WebAddress())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	_, err = io.WriteString(conn, "POST /v1/user.current HTTP/1.1\r\nHost: koko.example.com\r\nOrigin: https://attacker.example\r\n"+
		"Content-Type: application/json\r\nContent-Length: 1000000\r\n\r\n{")
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatal(err)
	}
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatalf("no answer while the body was still coming: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("answered %s, want 403", resp.Status)
	}
}

// The log names the client by the last address in X-Forwarded-For, the one
// the reverse proxy appended, or else by the TCP peer's.
func TestTheWebListenerNamesTheClientByTheProxysAddress(t *testing.T) {
	t.Parallel()
	h := startListening(t, webURL)
	for forwarded, want := range map[string]string{
		"198.51.100.7":                 "198.51.100.7",
		"198.51.100.7, 203.0.113.9":    "203.0.113.9",
		"spoofed, 2001:db8::7":         "2001:db8::7",
		"198.51.100.7, not-an-address": "127.0.0.1",
		"":                             "127.0.0.1",
	} {
		before := len(h.Log())
		send(t, h, request{method: http.MethodPost, path: "/v1/user.current", host: "koko.example.com", origin: "https://attacker.example", forwardedFor: forwarded, body: "{}"})
		if line := h.Log()[before:]; !strings.Contains(line, `reason=origin client=`+want+"\n") {
			t.Errorf("X-Forwarded-For %q: the log names another client than %s:\n%s", forwarded, want, line)
		}
	}
}

// The start log names the web listener's address and the Web URL.
func TestTheWebListenerStartLogNamesTheWebURL(t *testing.T) {
	t.Parallel()
	dataDir, keyFile := t.TempDir(), harness.NewRootKeyFile(t)
	h := startListening(t, webURL, harness.DataDir(dataDir), harness.RootKeyFile(keyFile))
	h.Stop()
	h = startListening(t, "", harness.NoInit(), harness.DataDir(dataDir), harness.RootKeyFile(keyFile))
	if want := `msg="web listener" listen=` + h.WebAddress() + " web_url=" + webURL + "\n"; !strings.Contains(h.Log(), want) {
		t.Errorf("the start log does not say %q:\n%s", want, h.Log())
	}
}
