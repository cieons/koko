package web_test

import (
	"encoding/json/v2"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/harness/sshhost"
	"github.com/cieons/koko/internal/shared/api"
)

// filesPost posts req, as JSON, to the files endpoint path from the browser
// b, and returns the answer.
func filesPost(t *testing.T, b *harness.Browser, path string, req any) answer {
	t.Helper()
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	a := b.Do(http.MethodPost, path, string(body))
	return answer{Response: a.Response, body: a.Body}
}

// wantFiles fails the test unless a answered 200 with JSON, which no cache
// keeps, and returns it decoded.
func wantFiles[Resp any](t *testing.T, a answer) Resp {
	t.Helper()
	var resp Resp
	if a.StatusCode != http.StatusOK {
		t.Fatalf("answered %s %q, want 200", a.Status, a.body)
	}
	if err := json.Unmarshal([]byte(a.body), &resp); err != nil {
		t.Fatalf("the answer %q is no JSON of its type: %v", a.body, err)
	}
	if cc := a.Header.Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", cc)
	}
	return resp
}

// wantMessage fails the test unless a's error object says message.
func wantMessage(t *testing.T, a answer, message string) {
	t.Helper()
	var e api.ErrorResponse
	if err := json.Unmarshal([]byte(a.body), &e); err != nil || e.Error.Message != message {
		t.Errorf("answered %q, want the message %q", a.body, message)
	}
}

// filesTerminal starts a koko server with its web listener, the Host web1
// serving SFTP with opts, and a terminal to it from the Owner's browser,
// which it returns with the Host.
func filesTerminal(t *testing.T, opts ...sshhost.Option) (*harness.Harness, *sshhost.Host, *harness.Browser, *harness.WebTerminal) {
	t.Helper()
	h := startTerminals(t)
	host := terminalHost(t, h, "web1", nil, opts...)
	b := h.SignIn(harness.Owner)
	return h, host, b, b.TerminalPage().Open("web1", 80, 24)
}

func list(t *testing.T, b *harness.Browser, tunnel, path string) answer {
	t.Helper()
	return filesPost(t, b, api.FilesListPath, api.FilesListRequest{Tunnel: tunnel, Path: path})
}

// writeFile writes content into the file name under dir, failing the test
// otherwise.
func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The listing opens on the Host Account's home for an empty path, lists
// directories first, then the rest, each by name, with its kind, size and
// date; ".." goes up and a link to a directory is entered, while a file
// named as a directory and an unreadable directory are refused with the
// Host's message. Past 5,000 entries, the rest are counted, not listed.
// The sftp subsystem is opened once, and logged once.
func TestFilesListADirectoryOnTheHost(t *testing.T) {
	t.Parallel()
	h, host, b, w := filesTerminal(t, sshhost.SFTP())
	home := host.Home()
	for _, dir := range []string{"docs", "many", "locked"} {
		if err := os.Mkdir(filepath.Join(home, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, home, "notes.txt", "twelve bytes")
	writeFile(t, filepath.Join(home, "docs"), "a.txt", "a")
	if err := os.Symlink("docs", filepath.Join(home, "docs-link")); err != nil {
		t.Fatal(err)
	}
	for i := range api.MaxListed + 1 {
		writeFile(t, filepath.Join(home, "many"), strconv.Itoa(i), "")
	}
	modified := time.Date(2026, 11, 2, 9, 30, 0, 0, time.UTC)
	if err := os.Chtimes(filepath.Join(home, "notes.txt"), modified, modified); err != nil {
		t.Fatal(err)
	}

	got := wantFiles[api.FilesListResponse](t, list(t, b, w.ID, ""))
	if got.Path != home {
		t.Errorf("the home listing names %q, want the Host Account's home %q", got.Path, home)
	}
	var names []string
	for _, e := range got.Entries {
		names = append(names, e.Name+" "+e.Kind)
	}
	// The home holds what the in-process Host keeps there too, such as
	// .ssh: compare what the test made, in the order listed.
	names = slices.DeleteFunc(names, func(n string) bool { return strings.HasPrefix(n, ".") })
	if want := []string{"docs dir", "locked dir", "many dir", "docs-link link", "notes.txt file"}; !slices.Equal(names, want) {
		t.Errorf("the home lists %q, want %q", names, want)
	}
	if i := slices.IndexFunc(got.Entries, func(e api.FileEntry) bool { return e.Name == "notes.txt" }); i < 0 ||
		got.Entries[i].Size != 12 || got.Entries[i].Modified != "2026-11-02T09:30:00Z" {
		t.Errorf("notes.txt is listed as %+v, want 12 bytes modified 2026-11-02T09:30:00Z", got.Entries)
	}

	if up := wantFiles[api.FilesListResponse](t, list(t, b, w.ID, home+"/docs/..")); up.Path != home {
		t.Errorf("docs/.. lists %q, want %q", up.Path, home)
	}
	in := wantFiles[api.FilesListResponse](t, list(t, b, w.ID, home+"/docs-link"))
	if len(in.Entries) != 1 || in.Entries[0].Name != "a.txt" || in.Entries[0].Kind != "file" {
		t.Errorf("the link to docs lists %+v, want docs' a.txt", in.Entries)
	}
	wantAPIError(t, list(t, b, w.ID, home+"/notes.txt"), http.StatusForbidden, "host_refused")
	if os.Geteuid() != 0 {
		// root reads any directory.
		if err := os.Chmod(filepath.Join(home, "locked"), 0); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(filepath.Join(home, "locked"), 0o755) })
		a := list(t, b, w.ID, home+"/locked")
		wantAPIError(t, a, http.StatusForbidden, "host_refused")
		wantMessage(t, a, "Permission denied")
	}
	many := wantFiles[api.FilesListResponse](t, list(t, b, w.ID, home+"/many"))
	if len(many.Entries) != api.MaxListed || many.More != 1 {
		t.Errorf("a directory of %d files lists %d of them with %d more, want %d with 1 more", api.MaxListed+1, len(many.Entries), many.More, api.MaxListed)
	}
	wantAPIError(t, list(t, b, w.ID, "docs"), http.StatusBadRequest, "invalid_request")

	waitLog(t, h, `msg="ssh session request" tunnel=`+w.ID+` client=browser host=web1 request=subsystem subsystem=sftp\n`)
	if n := len(regexp.MustCompile(`request=subsystem subsystem=sftp`).FindAllString(h.Log(), -1)); n != 1 {
		t.Errorf("the log tells the sftp subsystem %d times, want once", n)
	}
}

// A Host that offers no SFTP answers every files call sftp_unavailable,
// and its terminal goes on.
func TestFilesOnAHostWithoutSFTP(t *testing.T) {
	t.Parallel()
	_, _, b, w := filesTerminal(t)
	a := list(t, b, w.ID, "")
	wantAPIError(t, a, http.StatusConflict, "sftp_unavailable")
	wantMessage(t, a, "web1 offers no SFTP, which koko moves files through")
	w.Type([]byte("echo still here\n"))
	w.WaitOutput("still here\n")
}

// A files call is refused, as JSON, from another Origin or none before the
// cookie is read, without a live Sign-in, and for an SSH Session that is
// not the caller's, or no SSH Session at all; a GET and an unknown endpoint
// are refused too.
func TestFilesCallsAreTheCallersOwn(t *testing.T) {
	t.Parallel()
	h, _, b, w := filesTerminal(t, sshhost.SFTP())
	body := `{"tunnel":"` + w.ID + `","path":""}`
	for _, origin := range []string{"", "https://evil.example", "http://koko.example.com"} {
		a := send(t, h, request{method: http.MethodPost, path: api.FilesListPath, host: "koko.example.com", origin: origin, body: body})
		wantAPIError(t, a, http.StatusForbidden, "bad_origin")
	}
	wantAPIError(t, send(t, h, request{method: http.MethodPost, path: api.FilesListPath, host: "koko.example.com", origin: harness.WebURL, body: body}),
		http.StatusUnauthorized, "signin_required")

	other := h.SignIn("bob")
	wantAPIError(t, list(t, other, w.ID, ""), http.StatusNotFound, "unknown_tunnel")
	wantAPIError(t, list(t, b, strings.Repeat("ab", 16), ""), http.StatusNotFound, "unknown_tunnel")
	wantAPIError(t, list(t, b, "web1", ""), http.StatusNotFound, "unknown_tunnel")
	wantAPIError(t, filesPost(t, b, api.FilesPrefix+"rename", struct{}{}), http.StatusNotFound, "unknown_operation")
	g := b.Do(http.MethodGet, api.FilesListPath, "")
	wantAPIError(t, answer{Response: g.Response, body: g.Body}, http.StatusMethodNotAllowed, "method_not_allowed")
	big := b.Do(http.MethodPost, api.FilesListPath, `{"tunnel":"`+w.ID+`","path":"/`+strings.Repeat("a", api.MaxFilesBody)+`"}`)
	wantAPIError(t, answer{Response: big.Response, body: big.Body}, http.StatusRequestEntityTooLarge, "request_too_large")
	wantFiles[api.FilesListResponse](t, list(t, b, w.ID, ""))
}

// Files move while the terminal's WebSocket is Detached, and once its SSH
// Session has ended with a notice, a files call is ssh_session_ended.
func TestFilesWhileDetachedAndOnceEnded(t *testing.T) {
	t.Parallel()
	h, _, b, w := filesTerminal(t, sshhost.SFTP())
	w.Drop()
	waitLog(t, h, `msg="ssh session detached" tunnel=`+w.ID+` `)
	wantFiles[api.FilesListResponse](t, list(t, b, w.ID, ""))

	h.Koko("host", "retire", "web1").WantSuccess(t)
	waitLog(t, h, `msg="ssh session ended" tunnel=`+w.ID+` .* reason="host retired"\n`)
	wantAPIError(t, list(t, b, w.ID, ""), http.StatusGone, "ssh_session_ended")
}
