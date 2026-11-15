package cli_test

import (
	"bufio"
	"encoding/json/v2"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/cieons/koko/internal/client/cli"
	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/server/transport"
	"github.com/cieons/koko/internal/shared/api"
	"github.com/cieons/koko/internal/shared/compat"
	"github.com/cieons/koko/internal/shared/wire"
)

// serverBeforeSignInLinks serves on a socket of its own a koko server from
// before the Sign-in Link, as far as the signin area goes: password.link
// answers a /password# link, and signin.list Sign-ins without a method. It
// returns the socket's path.
func serverBeforeSignInLinks(t *testing.T) string {
	t.Helper()
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/" + api.PasswordLinkIssue.Name:
			_, _ = io.WriteString(w, `{"password_link": {"id": "passwordlink_3f9k2m7q", "user": "bob",
				"link": "https://koko.example.com/password#ABCDEFGHIJKLMNOPQRSTUVWXYZ", "expires_at": "2026-11-15T00:15:00Z"}}`)
		case "/v1/" + api.SignInList.Name:
			_, _ = io.WriteString(w, `{"signins": [{"id": "signin_3f9k2m7q", "user": "bob", "client": "Firefox on Linux",
				"address": "192.0.2.10", "started_at": "2026-11-15T00:00:00Z", "last_used_at": "2026-11-15T00:00:00Z",
				"expires_at": "2026-11-22T00:00:00Z"}]}`)
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"error": {"code": "unknown_operation", "message": "no operation here"}}`)
		}
	})
	path := filepath.Join(harness.ShortTempDir(t), "koko.sock")
	report := wire.Report{Version: compat.Oldest, Operations: []string{api.PasswordLinkIssue.Name, api.SignInList.Name}}
	s := transport.New(h, h, func() wire.Report { return report }, slog.New(slog.DiscardHandler))
	if err := s.ListenSocket(t.Context(), path); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return path
}

// This CLI's signin link, against a koko server from before the Sign-in
// Link, prints the /password# link that koko server issues, which the web
// UI still opens; signin ls says no method for its Sign-ins.
func TestSignInLinkAgainstAServerBeforeIt(t *testing.T) {
	t.Parallel()
	path := serverBeforeSignInLinks(t)
	r := harness.Run(t, cli.Runner{SocketPath: path}, nil, "signin", "link", "bob", "--json")
	r.WantSuccess(t)
	var out map[string]string
	if err := json.Unmarshal([]byte(r.Stdout), &out); err != nil || out["signin_link"] != "https://koko.example.com/password#ABCDEFGHIJKLMNOPQRSTUVWXYZ" {
		t.Errorf("signin link printed %s (%v), want the /password# link the koko server issued", r.Stdout, err)
	}

	r = harness.Run(t, cli.Runner{SocketPath: path}, nil, "signin", "ls", "--json")
	r.WantSuccess(t)
	if !strings.Contains(r.Stdout, `"method": null`) {
		t.Errorf("signin ls printed %s, want the method null", r.Stdout)
	}
	r = harness.Run(t, cli.Runner{SocketPath: path}, nil, "signin", "ls")
	r.WantSuccess(t)
	if !strings.Contains(r.Stdout, "bob   Firefox on Linux  -       192.0.2.10") {
		t.Errorf("signin ls printed:\n%s\nwant - under METHOD", r.Stdout)
	}
}

// An older CLI's password link, which sends password.link and reads its
// link from password_link.link, gets a /link# Sign-in Link from this koko
// server, which a browser spends as it spent a Password Link.
func TestAnOlderCLIsPasswordLinkWorks(t *testing.T) {
	t.Parallel()
	h := startWebListener(t)
	c := clientAs(t, h.Socket(), "SSH-2.0-koko "+compat.Oldest)
	ch, reqs, err := c.OpenChannel(wire.ChannelAPI, nil)
	if err != nil {
		t.Fatalf("open %s: %v", wire.ChannelAPI, err)
	}
	go ssh.DiscardRequests(reqs)
	defer func() { _ = ch.Close() }()
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, "http://koko/v1/"+api.PasswordLinkIssue.Name, strings.NewReader(`{"user": "bob"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(api.IdempotencyKeyHeader, api.NewIdempotencyKey())
	if err := req.Write(ch); err != nil {
		t.Fatal(err)
	}
	resp, err := http.ReadResponse(bufio.NewReader(ch), req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var out struct {
		PasswordLink struct {
			User string `json:"user"`
			Link string `json:"link"`
		} `json:"password_link"`
	}
	if err := json.UnmarshalRead(resp.Body, &out); resp.StatusCode != http.StatusOK || err != nil ||
		out.PasswordLink.User != "bob" || !strings.HasPrefix(out.PasswordLink.Link, harness.WebURL+"/link#") {
		t.Fatalf("password.link answered %s %+v (%v), want bob's /link# Sign-in Link", resp.Status, out, err)
	}
	b := h.Browser()
	status, body := b.Post(api.AuthPasswordPath, api.AuthPasswordRequest{Link: harness.LinkSecret(out.PasswordLink.Link), Password: harness.Password})
	if status != http.StatusOK || b.Cookie == "" {
		t.Errorf("spending the link answered %d %s, want bob signed in", status, body)
	}
}
