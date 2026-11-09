package harness

import (
	"encoding/json/v2"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/cieons/koko/internal/shared/api"
)

// WebURL is the Web URL a Browser's koko server answers at, as its Owner's
// reverse proxy would serve it: the Host and Origin every Browser request
// carries.
const WebURL = "https://koko.example.com"

// Password is the Password SignIn sets.
const Password = "correct horse battery staple"

// Browser is a browser on the web listener of a koko server started with
// WebListen, as the Owner's reverse proxy passes it on: each request names
// WebURL's host, carries its Origin, and X-Forwarded-For with the browser's
// address. It keeps the Sign-in cookie the koko server gives it, and sends
// it back, as a browser does.
type Browser struct {
	t testing.TB
	h *Harness
	// Address is the client's address the proxy passes on in
	// X-Forwarded-For; empty sends none.
	Address string
	// UserAgent is the User-Agent it sends.
	UserAgent string
	// Cookie is the Sign-in cookie it holds: its value, or "" for none.
	Cookie string
}

// Browser returns a new browser on h's web listener, from the address
// 192.0.2.10, holding no cookie. It sets the Web URL to WebURL over the Unix
// socket first.
func (h *Harness) Browser() *Browser {
	h.t.Helper()
	if h.web == "" {
		h.t.Fatal("a Browser needs the web listener: start the koko server with harness.WebListen")
	}
	if _, err := h.Call(api.ServerSet, api.ServerSetRequest{WebURL: new(WebURL)}); err != nil {
		h.t.Fatalf("set the Web URL: %v", err)
	}
	return &Browser{t: h.t, h: h, Address: "192.0.2.10", UserAgent: "Mozilla/5.0 (X11; Linux x86_64; rv:140.0) Gecko/20100101 Firefox/140.0"}
}

// SignIn issues a Password Link for the User named user over the Unix
// socket, and returns a new Browser that spent it, setting Password, so it
// is signed in as user.
func (h *Harness) SignIn(user string) *Browser {
	h.t.Helper()
	b := h.Browser()
	resp, err := h.Call(api.PasswordLinkIssue, api.PasswordLinkRequest{User: user})
	if err != nil {
		h.t.Fatalf("issue a Password Link for %s: %v", user, err)
	}
	status, body := b.Post(api.AuthPasswordPath, api.AuthPasswordRequest{Link: LinkSecret(resp.PasswordLink.Link), Password: Password})
	if status != http.StatusOK || b.Cookie == "" {
		h.t.Fatalf("set %s's Password through their Password Link: %d %s", user, status, body)
	}
	return b
}

// LinkSecret is the secret of a Password Link: the part after #.
func LinkSecret(link string) string {
	_, secret, _ := strings.Cut(link, "#")
	return secret
}

// Answer is one answer of the web listener, its body read.
type Answer struct {
	*http.Response
	Body string
}

// Do sends method to path with body, as the browser would through the
// proxy, and reads the answer, keeping the Sign-in cookie it sets or
// clears.
func (b *Browser) Do(method, path, body string) Answer {
	b.t.Helper()
	req, err := http.NewRequestWithContext(b.t.Context(), method, "http://"+b.h.web+path, strings.NewReader(body))
	if err != nil {
		b.t.Fatal(err)
	}
	req.Host = strings.TrimPrefix(WebURL, "https://")
	req.Header.Set("User-Agent", b.UserAgent)
	if method == http.MethodPost {
		req.Header.Set("Origin", WebURL)
		req.Header.Set("Content-Type", "application/json")
		// As the web UI's client sends it with every call, a write's
		// included.
		if strings.HasPrefix(path, api.PathPrefix) {
			req.Header.Set(api.IdempotencyKeyHeader, api.NewIdempotencyKey())
		}
	}
	if b.Address != "" {
		req.Header.Set("X-Forwarded-For", b.Address)
	}
	if b.Cookie != "" {
		req.AddCookie(&http.Cookie{Name: "__Host-koko", Value: b.Cookie})
	}
	tr := &http.Transport{}
	defer tr.CloseIdleConnections()
	resp, err := (&http.Client{Transport: tr}).Do(req)
	if err != nil {
		b.t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	read, err := io.ReadAll(resp.Body)
	if err != nil {
		b.t.Fatal(err)
	}
	for _, c := range resp.Cookies() {
		if c.Name == "__Host-koko" {
			b.Cookie = c.Value
			if c.MaxAge < 0 {
				b.Cookie = ""
			}
		}
	}
	return Answer{Response: resp, Body: string(read)}
}

// Post posts req, encoded as JSON, to path, such as one of the /auth/
// endpoints, and returns the status and body of the answer.
func (b *Browser) Post(path string, req any) (int, string) {
	b.t.Helper()
	body, err := json.Marshal(req)
	if err != nil {
		b.t.Fatal(err)
	}
	a := b.Do(http.MethodPost, path, string(body))
	return a.StatusCode, a.Body
}

// Send calls the operation o with body as its JSON request, as Harness.Send
// does, from the browser, as its Sign-in: for the access matrix and other
// byte-for-byte checks.
func (b *Browser) Send(o api.Operation, body []byte) (int, []byte) {
	b.t.Helper()
	a := b.Do(http.MethodPost, api.PathPrefix+o.Info().Name, string(body))
	return a.StatusCode, []byte(a.Body)
}
