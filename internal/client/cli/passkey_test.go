package cli_test

import (
	"encoding/json/v2"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/cieons/koko/internal/client/cli"
	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/server/transport"
	"github.com/cieons/koko/internal/shared/api"
	"github.com/cieons/koko/internal/shared/compat"
	"github.com/cieons/koko/internal/shared/wire"
)

// addPasskey makes a passkey from b, through link or from b's Sign-in
// when link is "", named name, and returns it.
func addPasskey(t *testing.T, b *harness.Browser, link, name string) api.Passkey {
	t.Helper()
	status, body := b.AddPasskey(harness.NewAuthenticator(harness.WebURL), link, name)
	var resp api.AuthPasskeyAddResponse
	if err := json.Unmarshal([]byte(body), &resp); status != http.StatusOK || err != nil {
		t.Fatalf("make a passkey: %d %s", status, body)
	}
	return resp.Passkey
}

// bareIDs replaces each bare record id in text, which the goldens' quoted
// ids do not cover, with its kind and its order of first appearance.
func bareIDs(text string) string {
	seen := map[string]string{}
	counts := map[string]int{}
	return regexp.MustCompile(`\b(passkey|user|signin)_[0-9a-hjkmnp-tv-z]{8}\b`).ReplaceAllStringFunc(text, func(id string) string {
		if p, ok := seen[id]; ok {
			return p
		}
		kind, _, _ := strings.Cut(id, "_")
		counts[kind]++
		seen[id] = kind + "_<" + strings.Repeat("i", counts[kind]) + ">"
		return seen[id]
	})
}

// passkey ls lists a User's passkeys, each with its host, whether it is
// synced, who issued the link it came through and when it was last used;
// passkey rm removes one and says how many Sign-ins it ended; user ls says
// how each User signs in.
func TestPasskeysFromTheCLI(t *testing.T) {
	t.Parallel()
	h := startWebListener(t)
	h.Koko("user", "invite", "carol").WantSuccess(t)
	link := h.Koko("signin", "link", "bob", "--json")
	link.WantSuccess(t)
	var issued struct {
		SigninLink string `json:"signin_link"`
	}
	if err := json.Unmarshal([]byte(link.Stdout), &issued); err != nil {
		t.Fatal(err)
	}
	b := h.Browser()
	phone := addPasskey(t, b, issued.SigninLink, "")
	addPasskey(t, b, "", "Laptop")
	h.SignIn("carol")

	r := h.Koko("passkey", "ls", "--user", "bob", "--json")
	r.WantSuccess(t)
	harness.Golden(t, "passkey-ls.json", r.Stdout)
	r = h.Koko("passkey", "ls", "--user", "bob")
	r.WantSuccess(t)
	harness.Golden(t, "passkey-ls.txt", bareIDs(r.Stdout))
	r = h.Koko("passkey", "ls")
	r.WantSuccess(t)
	harness.Golden(t, "passkey-ls-none.txt", r.Stdout)

	r = h.Koko("user", "ls", "--json")
	r.WantSuccess(t)
	harness.Golden(t, "user-ls-passkeys.json", r.Stdout)
	r = h.Koko("user", "ls")
	r.WantSuccess(t)
	harness.Golden(t, "user-ls-passkeys.txt", bareIDs(r.Stdout))

	r = h.Koko("passkey", "rm", phone.ID, "--json")
	r.WantSuccess(t)
	harness.Golden(t, "passkey-rm.json", r.Stdout)
	r = h.Koko("passkey", "rm", phone.ID, "--json")
	r.WantError(t, 1, "unknown_passkey")
	others := passkeysOf(t, h, "bob")
	if len(others) != 1 {
		t.Fatalf("bob has %+v, want the Laptop left", others)
	}
	r = h.Koko("passkey", "rm", others[0].ID)
	r.WantSuccess(t)
	harness.Golden(t, "passkey-rm.txt", bareIDs(r.Stdout))
}

// passkeysOf lists user's passkeys over the Unix socket.
func passkeysOf(t *testing.T, h *harness.Harness, user string) []api.Passkey {
	t.Helper()
	resp, err := h.Call(api.PasskeyList, api.PasskeyListRequest{User: user})
	if err != nil {
		t.Fatalf("passkey.list of %s: %v", user, err)
	}
	return resp.Passkeys
}

// server set web_url= says how many passkeys belong to another host than
// the new Web URL's, and a change of port alone counts none.
func TestServerSetCountsThePasskeysOfAnotherHost(t *testing.T) {
	t.Parallel()
	h := startWebListener(t)
	addPasskey(t, h.SignIn("bob"), "", "Laptop")
	r := h.Koko("server", "set", "web_url=https://koko.example.com:8443")
	r.WantSuccess(t)
	if strings.Contains(r.Stdout, "passkey") {
		t.Errorf("a change of port printed:\n%s\nwant no passkey counted", r.Stdout)
	}
	r = h.Koko("server", "set", "web_url=https://other.example.com")
	r.WantSuccess(t)
	harness.Golden(t, "server-set-other-host.txt", r.Stdout)
}

// serverBeforePasskeys serves on a socket of its own a koko server from
// before passkeys: it lists no passkey operation, and user.list answers
// Users without passkeys or a Linked Identity. It returns the socket's
// path.
func serverBeforePasskeys(t *testing.T) string {
	t.Helper()
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/v1/"+api.UserList.Name {
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"error": {"code": "unknown_operation", "message": "no operation here"}}`)
			return
		}
		_, _ = io.WriteString(w, `{"users": [{"id": "user_3f9k2m7q", "name": "bob", "role": "member",
			"created_at": "2026-11-15T00:00:00Z", "password_set_at": "2026-11-15T00:00:00Z"}]}`)
	})
	path := filepath.Join(harness.ShortTempDir(t), "koko.sock")
	report := wire.Report{Version: compat.Oldest, Operations: []string{api.UserList.Name}}
	s := transport.New(h, h, func() wire.Report { return report }, slog.New(slog.DiscardHandler))
	if err := s.ListenSocket(t.Context(), path); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return path
}

// Against a koko server from before passkeys, passkey ls and rm fail with
// server_too_old, sending nothing, and user ls says the Password alone.
func TestPasskeysAgainstAServerBeforeThem(t *testing.T) {
	t.Parallel()
	path := serverBeforePasskeys(t)
	for _, args := range [][]string{{"passkey", "ls", "--json"}, {"passkey", "rm", "passkey_3f9k2m7q", "--json"}} {
		r := harness.Run(t, cli.Runner{SocketPath: path}, nil, args...)
		r.WantError(t, 1, "server_too_old")
	}
	r := harness.Run(t, cli.Runner{SocketPath: path}, nil, "user", "ls", "--json")
	r.WantSuccess(t)
	if !strings.Contains(r.Stdout, `"passkeys": 0`) || !strings.Contains(r.Stdout, `"identity_linked_at": null`) {
		t.Errorf("user ls printed %s, want no passkey and no Linked Identity", r.Stdout)
	}
	r = harness.Run(t, cli.Runner{SocketPath: path}, nil, "user", "ls")
	r.WantSuccess(t)
	if !strings.Contains(r.Stdout, "SIGN-IN") || !strings.Contains(r.Stdout, "2026-11-15T00:00:00Z  password\n") {
		t.Errorf("user ls printed:\n%s\nwant password under SIGN-IN", r.Stdout)
	}
}
