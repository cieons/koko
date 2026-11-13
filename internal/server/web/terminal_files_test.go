package web_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json/v2"
	"errors"
	"io"
	"io/fs"
	"net"
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
	"github.com/cieons/koko/internal/shared/timing"
	"github.com/cieons/koko/internal/shared/wire"
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

// Every files call is refused, as JSON, from another Origin or none before
// the cookie is read, without a live Sign-in, and for an SSH Session that is
// not the caller's, or no SSH Session at all; a GET and an unknown endpoint
// are refused too.
func TestFilesCallsAreTheCallersOwn(t *testing.T) {
	t.Parallel()
	h, _, b, w := filesTerminal(t, sshhost.SFTP())
	body := `{"tunnel":"` + w.ID + `","path":""}`
	for _, path := range []string{api.FilesListPath, api.FilesUploadPath, api.FilesChunkPath, api.FilesFinishPath, api.FilesCancelPath, api.FilesDownloadPath} {
		for _, origin := range []string{"", "https://evil.example", "http://koko.example.com"} {
			a := send(t, h, request{method: http.MethodPost, path: path, host: "koko.example.com", origin: origin, body: body})
			wantAPIError(t, a, http.StatusForbidden, "bad_origin")
		}
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

// Files work from any page of the SSH Session's User: a page of the
// Owner's other Sign-in lists the Host and uploads into it, and the log
// names that Sign-in as who moved the file; another User's page gets
// unknown_tunnel, as for an id that names no SSH Session.
func TestFilesFromAnotherSignInOfTheUser(t *testing.T) {
	t.Parallel()
	h, host, _, w := filesTerminal(t, sshhost.SFTP())
	other := signInAgain(t, h, harness.Owner)
	status, body := other.Send(api.UserCurrent, []byte("{}"))
	var current api.UserCurrentResponse
	if err := json.Unmarshal(body, &current); err != nil || status != http.StatusOK || current.SignIn == nil {
		t.Fatalf("user.current of the Owner's other Sign-in: %d %s", status, body)
	}

	wantFiles[api.FilesListResponse](t, list(t, other, w.ID, ""))
	home := host.Home()
	id := startUpload(t, other, w, home, "from-the-phone.txt", 5, false)
	chunk(t, h, other, id, 0, []byte("hello"), 5)
	wantFiles[api.FilesFinishResponse](t, filesPost(t, other, api.FilesFinishPath, api.FilesFinishRequest{Upload: id}))
	if got, err := os.ReadFile(filepath.Join(home, "from-the-phone.txt")); err != nil || string(got) != "hello" {
		t.Errorf("the upload holds %q (%v), want hello", got, err)
	}
	waitLog(t, h, `msg="file transferred" tunnel=`+w.ID+` client=browser user=`+harness.Owner+` signin=`+current.SignIn.ID+` host=web1 .* outcome=done\n`)

	wantAPIError(t, list(t, h.SignIn("bob"), w.ID, ""), http.StatusNotFound, "unknown_tunnel")
}

// Files move while no page is attached to the terminal, and once its SSH
// Session has ended with a notice, a files call is ssh_session_ended.
func TestFilesWhileDetachedAndOnceEnded(t *testing.T) {
	t.Parallel()
	h, _, b, w := filesTerminal(t, sshhost.SFTP())
	w.Close()
	waitLog(t, h, `msg="ssh session detached" tunnel=`+w.ID+` `)
	wantFiles[api.FilesListResponse](t, list(t, b, w.ID, ""))

	h.Koko("host", "retire", "web1").WantSuccess(t)
	waitLog(t, h, `msg="ssh session ended" tunnel=`+w.ID+` .* reason="host retired"\n`)
	wantAPIError(t, list(t, b, w.ID, ""), http.StatusGone, "ssh_session_ended")
}

// startUpload starts an upload of size bytes named name into dir through
// the terminal w, and returns its id.
func startUpload(t *testing.T, b *harness.Browser, w *harness.WebTerminal, dir, name string, size int64, replace bool) string {
	t.Helper()
	return wantFiles[api.FilesUploadResponse](t, filesPost(t, b, api.FilesUploadPath,
		api.FilesUploadRequest{Tunnel: w.ID, Dir: dir, Name: name, Size: size, Replace: replace})).Upload
}

// sendChunk sends data as the chunk of the upload id at offset, as the
// browser b would, with contentType.
func sendChunk(t *testing.T, h *harness.Harness, b *harness.Browser, id string, offset int64, data []byte, contentType string) answer {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost,
		"http://"+h.WebAddress()+api.FilesChunkPath+"?upload="+id+"&offset="+strconv.FormatInt(offset, 10), bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	req.Host = strings.TrimPrefix(harness.WebURL, "https://")
	req.Header.Set("Origin", harness.WebURL)
	req.Header.Set("Content-Type", contentType)
	req.AddCookie(&http.Cookie{Name: "__Host-koko", Value: b.Cookie})
	tr := &http.Transport{}
	defer tr.CloseIdleConnections()
	resp, err := (&http.Client{Transport: tr}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return answer{Response: resp, body: string(body)}
}

// chunk sends data as the chunk of the upload id at offset, and fails the
// test unless it is answered with received.
func chunk(t *testing.T, h *harness.Harness, b *harness.Browser, id string, offset int64, data []byte, received int64) {
	t.Helper()
	if got := wantFiles[api.FilesChunkResponse](t, sendChunk(t, h, b, id, offset, data, "application/octet-stream")); got.Received != received {
		t.Errorf("the chunk at %d was answered with %d received, want %d", offset, got.Received, received)
	}
}

// partials returns the partial files of uploads named name in dir.
func partials(t *testing.T, dir, name string) []string {
	t.Helper()
	found, err := filepath.Glob(filepath.Join(dir, "."+name+".koko-upload-*"))
	if err != nil {
		t.Fatal(err)
	}
	return found
}

// wantGone fails the test unless dir holds neither the file name nor a
// partial file of it.
func wantGone(t *testing.T, dir, name string) {
	t.Helper()
	if p := partials(t, dir, name); len(p) > 0 {
		t.Errorf("the partial files %q are left", p)
	}
	if _, err := os.Lstat(filepath.Join(dir, name)); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("%s is under its real name (%v)", name, err)
	}
}

// An upload is written in chunks into a hidden partial file, private to
// the Host Account, and renamed onto its name once every byte has come,
// byte for byte; a chunk sent again is answered and not written twice, a
// gap and a chunk past the size are refused, as is a finish before the
// last byte; a finish sent again is answered the same; and the file
// moved is logged.
func TestFilesUploadInChunks(t *testing.T) {
	t.Parallel()
	h, host, b, w := filesTerminal(t, sshhost.SFTP())
	home := host.Home()
	data := make([]byte, 2*api.MaxFilesChunk+300<<10)
	_, _ = rand.Read(data)
	size := int64(len(data))
	id := startUpload(t, b, w, home, "data.bin", size, false)

	p := partials(t, home, "data.bin")
	if len(p) != 1 || !regexp.MustCompile(`/\.data\.bin\.koko-upload-[0-9a-f]{8}$`).MatchString(p[0]) {
		t.Fatalf("the partial files are %q, want one .data.bin.koko-upload-<8 hex>", p)
	}
	harness.WantPrivate(t, p[0])
	if _, err := os.Lstat(filepath.Join(home, "data.bin")); err == nil {
		t.Error("data.bin is under its real name before the upload finished")
	}

	const c = api.MaxFilesChunk
	chunk(t, h, b, id, 0, data[:c], c)
	chunk(t, h, b, id, c, data[c:2*c], 2*c)
	// Sent again, with other bytes: answered, and not written.
	chunk(t, h, b, id, 0, make([]byte, c), 2*c)
	wantAPIError(t, sendChunk(t, h, b, id, 2*c+1, data[2*c+1:], "application/octet-stream"), http.StatusConflict, "bad_offset")
	wantAPIError(t, sendChunk(t, h, b, id, 2*c, data[2*c:], "application/json"), http.StatusUnsupportedMediaType, "unsupported_media_type")
	wantAPIError(t, sendChunk(t, h, b, id, 2*c, make([]byte, c+1), "application/octet-stream"), http.StatusRequestEntityTooLarge, "request_too_large")
	wantAPIError(t, filesPost(t, b, api.FilesFinishPath, api.FilesFinishRequest{Upload: id}), http.StatusConflict, "bad_offset")
	chunk(t, h, b, id, 2*c, data[2*c:], size)
	wantAPIError(t, sendChunk(t, h, b, id, size, []byte{1}, "application/octet-stream"), http.StatusBadRequest, "invalid_request")

	want := api.FilesFinishResponse{Path: home + "/data.bin", Size: size}
	for range 2 {
		if got := wantFiles[api.FilesFinishResponse](t, filesPost(t, b, api.FilesFinishPath, api.FilesFinishRequest{Upload: id})); got != want {
			t.Errorf("the finish answered %+v, want %+v", got, want)
		}
	}
	got, err := os.ReadFile(filepath.Join(home, "data.bin"))
	if err != nil || sha256.Sum256(got) != sha256.Sum256(data) {
		t.Errorf("data.bin on the Host holds %d bytes that differ from the %d sent (%v)", len(got), size, err)
	}
	if fi, err := os.Stat(filepath.Join(home, "data.bin")); err != nil || fi.Mode().Perm() != 0o644 {
		t.Errorf("data.bin has the mode %v, want 0644 (%v)", fi.Mode(), err)
	}
	if p := partials(t, home, "data.bin"); len(p) > 0 {
		t.Errorf("the partial files %q are left", p)
	}
	waitLog(t, h, `msg="file transferred" tunnel=`+w.ID+` client=browser user=`+harness.Owner+` signin=signin_\w+ host=web1 host_id=\S+ direction=upload path=`+regexp.QuoteMeta(home)+`/data.bin bytes=`+strconv.FormatInt(size, 10)+` outcome=done\n`)
}

// An upload whose name exists is refused with file_exists unless it
// replaces it, which then keeps the old file's mode; a bad name, or a
// directory that is not absolute, is refused.
func TestFilesUploadAsksBeforeReplacing(t *testing.T) {
	t.Parallel()
	h, host, b, w := filesTerminal(t, sshhost.SFTP())
	home := host.Home()
	writeFile(t, home, "notes.txt", "old")
	if err := os.Chmod(filepath.Join(home, "notes.txt"), 0o640); err != nil {
		t.Fatal(err)
	}
	a := filesPost(t, b, api.FilesUploadPath, api.FilesUploadRequest{Tunnel: w.ID, Dir: home, Name: "notes.txt", Size: 3})
	wantAPIError(t, a, http.StatusConflict, "file_exists")
	wantMessage(t, a, "notes.txt exists in "+home)
	if p := partials(t, home, "notes.txt"); len(p) > 0 {
		t.Errorf("a refused upload left %q", p)
	}

	id := startUpload(t, b, w, home, "notes.txt", 11, true)
	chunk(t, h, b, id, 0, []byte("new content"), 11)
	wantFiles[api.FilesFinishResponse](t, filesPost(t, b, api.FilesFinishPath, api.FilesFinishRequest{Upload: id}))
	if got, err := os.ReadFile(filepath.Join(home, "notes.txt")); err != nil || string(got) != "new content" {
		t.Errorf("notes.txt holds %q, want the upload's (%v)", got, err)
	}
	if fi, err := os.Stat(filepath.Join(home, "notes.txt")); err != nil || fi.Mode().Perm() != 0o640 {
		t.Errorf("the replaced notes.txt has the mode %v, want its old 0640 (%v)", fi.Mode(), err)
	}

	for _, name := range []string{"", ".", "..", "a/b", "/etc"} {
		a := filesPost(t, b, api.FilesUploadPath, api.FilesUploadRequest{Tunnel: w.ID, Dir: home, Name: name, Size: 1})
		wantAPIError(t, a, http.StatusBadRequest, "invalid_name")
	}
	wantAPIError(t, filesPost(t, b, api.FilesUploadPath, api.FilesUploadRequest{Tunnel: w.ID, Dir: "docs", Name: "a", Size: 1}),
		http.StatusBadRequest, "invalid_request")
}

// An SSH Session runs at most four uploads at once. Cancelling one removes
// its partial file, frees its place and is logged; a cancelled upload, or
// another caller's, is unknown.
func TestFilesUploadsAreCancelledAndLimited(t *testing.T) {
	t.Parallel()
	h, host, b, w := filesTerminal(t, sshhost.SFTP())
	home := host.Home()
	var ids []string
	for i := range api.MaxUploads {
		ids = append(ids, startUpload(t, b, w, home, "f"+strconv.Itoa(i), 10, false))
	}
	a := filesPost(t, b, api.FilesUploadPath, api.FilesUploadRequest{Tunnel: w.ID, Dir: home, Name: "f4", Size: 10})
	wantAPIError(t, a, http.StatusTooManyRequests, "too_many_uploads")
	wantGone(t, home, "f4")

	chunk(t, h, b, ids[0], 0, []byte("12345"), 5)
	other := h.SignIn("bob")
	wantAPIError(t, filesPost(t, other, api.FilesCancelPath, api.FilesCancelRequest{Upload: ids[0]}), http.StatusNotFound, "unknown_upload")
	wantFiles[api.FilesCancelResponse](t, filesPost(t, b, api.FilesCancelPath, api.FilesCancelRequest{Upload: ids[0]}))
	wantGone(t, home, "f0")
	waitLog(t, h, `msg="file transferred" tunnel=`+w.ID+` .* direction=upload path=`+regexp.QuoteMeta(home)+`/f0 bytes=5 outcome=cancelled\n`)
	wantAPIError(t, sendChunk(t, h, b, ids[0], 5, []byte("67890"), "application/octet-stream"), http.StatusNotFound, "unknown_upload")
	wantAPIError(t, filesPost(t, b, api.FilesCancelPath, api.FilesCancelRequest{Upload: ids[0]}), http.StatusNotFound, "unknown_upload")
	startUpload(t, b, w, home, "f4", 10, false)
}

// The SSH Session's end removes the partial file of each upload it runs,
// while the Host connection lives, and logs it failed; a later chunk is
// ssh_session_ended.
func TestFilesUploadEndsWithItsSSHSession(t *testing.T) {
	t.Parallel()
	h, host, b, w := filesTerminal(t, sshhost.SFTP())
	home := host.Home()
	id := startUpload(t, b, w, home, "big.bin", 10, false)
	chunk(t, h, b, id, 0, []byte("12345"), 5)

	if a := b.TerminalPage().Workspace().CloseTab(w.Tab); a.Refused != "" {
		t.Fatalf("the close was answered %+v", a)
	}
	waitLog(t, h, `msg="file transferred" tunnel=`+w.ID+` .* direction=upload path=`+regexp.QuoteMeta(home)+`/big.bin bytes=5 outcome=failed err="the SSH Session ended"\n`)
	wantGone(t, home, "big.bin")
	wantAPIError(t, sendChunk(t, h, b, id, 5, []byte("67890"), "application/octet-stream"), http.StatusGone, "ssh_session_ended")
	wantAPIError(t, filesPost(t, b, api.FilesFinishPath, api.FilesFinishRequest{Upload: id}), http.StatusGone, "ssh_session_ended")
}

// An upload that gets no chunk for timing.UploadIdle is given up: its
// partial file goes, and it is logged failed and forgotten.
func TestFilesUploadGivenUpWhenIdle(t *testing.T) {
	t.Parallel()
	h := startTerminals(t, harness.Timings(timing.Timings{UploadIdle: 200 * time.Millisecond}))
	host := terminalHost(t, h, "web1", nil, sshhost.SFTP())
	b := h.SignIn(harness.Owner)
	w := b.TerminalPage().Open("web1", 80, 24)
	id := startUpload(t, b, w, host.Home(), "idle.bin", 10, false)

	waitLog(t, h, `msg="file transferred" tunnel=`+w.ID+` .* path=\S+/idle.bin bytes=0 outcome=failed err="no chunk came for 200ms"\n`)
	wantGone(t, host.Home(), "idle.bin")
	wantAPIError(t, sendChunk(t, h, b, id, 0, []byte("1"), "application/octet-stream"), http.StatusNotFound, "unknown_upload")
}

// A Host that fails writes, as on a full disk, answers a chunk
// host_refused with its message, and a cancel then removes the partial
// file.
func TestFilesUploadToAHostThatFailsWrites(t *testing.T) {
	t.Parallel()
	h, host, b, w := filesTerminal(t, sshhost.SFTPFailingWrites())
	home := host.Home()
	id := startUpload(t, b, w, home, "full.bin", 5, false)
	a := sendChunk(t, h, b, id, 0, []byte("12345"), "application/octet-stream")
	wantAPIError(t, a, http.StatusForbidden, "host_refused")
	wantMessage(t, a, "Failure")
	wantFiles[api.FilesCancelResponse](t, filesPost(t, b, api.FilesCancelPath, api.FilesCancelRequest{Upload: id}))
	wantGone(t, home, "full.bin")
}

// fetch sends GET path to the web listener of h as the browser b, through
// the proxy, and returns the answer, not yet read.
func fetch(t *testing.T, h *harness.Harness, b *harness.Browser, path string) *http.Response {
	t.Helper()
	return fetchOn(t, h, b, path, &http.Transport{})
}

// fetchStalling is fetch from a browser whose connection takes in little
// at a time: once it stops reading, the koko server's writes wait at once.
func fetchStalling(t *testing.T, h *harness.Harness, b *harness.Browser, path string) *http.Response {
	t.Helper()
	var d net.Dialer
	return fetchOn(t, h, b, path, &http.Transport{DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
		c, err := d.DialContext(ctx, network, addr)
		if err == nil {
			err = c.(*net.TCPConn).SetReadBuffer(4 << 10)
		}
		return c, err
	}})
}

func fetchOn(t *testing.T, h *harness.Harness, b *harness.Browser, path string, tr *http.Transport) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://"+h.WebAddress()+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = strings.TrimPrefix(harness.WebURL, "https://")
	req.AddCookie(&http.Cookie{Name: "__Host-koko", Value: b.Cookie})
	t.Cleanup(tr.CloseIdleConnections)
	resp, err := (&http.Client{Transport: tr}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

// fetchAll is fetch with the body read.
func fetchAll(t *testing.T, h *harness.Harness, b *harness.Browser, path string) answer {
	t.Helper()
	resp := fetch(t, h, b, path)
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return answer{Response: resp, body: string(body)}
}

// downloadToken asks for the download of path through the terminal w.
func downloadToken(t *testing.T, b *harness.Browser, w *harness.WebTerminal, path string) api.FilesDownloadResponse {
	t.Helper()
	return wantFiles[api.FilesDownloadResponse](t, filesPost(t, b, api.FilesDownloadPath, api.FilesDownloadRequest{Tunnel: w.ID, Path: path}))
}

// A download's token is answered with the file's name and size, and its
// GET streams the file byte for byte as an attachment, its name in UTF-8
// and as an ASCII stand-in, with its length, kept by no cache; the token
// is spent, and the download logged, the token in no line. A directory is
// not_a_file, and a missing file host_refused.
func TestFilesDownloadStreamsAFile(t *testing.T) {
	t.Parallel()
	h, host, b, w := filesTerminal(t, sshhost.SFTP())
	home := host.Home()
	const name = "报告 \"v2\";\n.txt"
	data := make([]byte, 3*api.MaxFilesChunk+7)
	_, _ = rand.Read(data)
	if err := os.WriteFile(filepath.Join(home, name), data, 0o644); err != nil {
		t.Fatal(err)
	}

	d := downloadToken(t, b, w, home+"/"+name)
	if !regexp.MustCompile(`^[0-9a-f]{32}$`).MatchString(d.Token) || d.Name != name || d.Size != int64(len(data)) {
		t.Fatalf("the download was answered %+v, want a 32 hex token, the name and %d bytes", d, len(data))
	}
	a := fetchAll(t, h, b, api.FilesDownloadPath+"/"+d.Token)
	if a.StatusCode != http.StatusOK || sha256.Sum256([]byte(a.body)) != sha256.Sum256(data) {
		t.Fatalf("the download answered %s with %d bytes that differ from the file's %d", a.Status, len(a.body), len(data))
	}
	wantSecurityHeaders(t, a.Header)
	for header, want := range map[string]string{
		"Content-Type":        "application/octet-stream",
		"Content-Length":      strconv.Itoa(len(data)),
		"Content-Disposition": `attachment; filename="__ _v2_;_.txt"; filename*=UTF-8''%E6%8A%A5%E5%91%8A%20%22v2%22%3B%0A.txt`,
		"Cache-Control":       "no-store",
	} {
		if got := a.Header.Get(header); got != want {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}
	waitLog(t, h, `msg="file transferred" tunnel=`+w.ID+` client=browser user=`+harness.Owner+` signin=signin_\w+ host=web1 host_id=\S+ direction=download path=".+" bytes=`+strconv.Itoa(len(data))+` outcome=done\n`)
	wantAPIError(t, fetchAll(t, h, b, api.FilesDownloadPath+"/"+d.Token), http.StatusNotFound, "unknown_download")
	if strings.Contains(h.Log(), d.Token) {
		t.Error("the log holds the download's token")
	}

	if err := os.Mkdir(filepath.Join(home, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	wantAPIError(t, filesPost(t, b, api.FilesDownloadPath, api.FilesDownloadRequest{Tunnel: w.ID, Path: home + "/docs"}), http.StatusConflict, "not_a_file")
	a = filesPost(t, b, api.FilesDownloadPath, api.FilesDownloadRequest{Tunnel: w.ID, Path: home + "/missing"})
	wantAPIError(t, a, http.StatusForbidden, "host_refused")
	wantMessage(t, a, "No such file")
	wantAPIError(t, filesPost(t, b, api.FilesDownloadPath, api.FilesDownloadRequest{Tunnel: w.ID, Path: "notes.txt"}), http.StatusBadRequest, "invalid_request")
}

// A download's token is good for 60 seconds on the koko server's clock,
// and for its own caller alone, whose token another caller's attempt does
// not spend.
func TestFilesDownloadTokensExpireAndAreTheCallersOwn(t *testing.T) {
	t.Parallel()
	h, host, b, w := filesTerminal(t, sshhost.SFTP())
	writeFile(t, host.Home(), "notes.txt", "notes")

	d := downloadToken(t, b, w, host.Home()+"/notes.txt")
	other := h.SignIn("bob")
	wantAPIError(t, fetchAll(t, h, other, api.FilesDownloadPath+"/"+d.Token), http.StatusNotFound, "unknown_download")
	if a := fetchAll(t, h, b, api.FilesDownloadPath+"/"+d.Token); a.StatusCode != http.StatusOK || a.body != "notes" {
		t.Errorf("the download after another caller's attempt answered %s %q", a.Status, a.body)
	}

	late := downloadToken(t, b, w, host.Home()+"/notes.txt")
	h.Advance(59 * time.Second)
	inTime := downloadToken(t, b, w, host.Home()+"/notes.txt")
	h.Advance(time.Second)
	wantAPIError(t, fetchAll(t, h, b, api.FilesDownloadPath+"/"+late.Token), http.StatusNotFound, "unknown_download")
	if a := fetchAll(t, h, b, api.FilesDownloadPath+"/"+inTime.Token); a.StatusCode != http.StatusOK {
		t.Errorf("a token 1 s old answered %s", a.Status)
	}
}

// sparseFile makes the file name under dir of size bytes, sparse, so
// that a file far larger than the connection's buffers is made at once.
func sparseFile(t *testing.T, dir, name string, size int64) {
	t.Helper()
	f, err := os.Create(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if err := f.Truncate(size); err != nil {
		t.Fatal(err)
	}
}

// A download already streaming is cut once its caller is ended: a page of
// the Owner's other Sign-in downloads from a tab the first one opened, and
// revoking that Sign-in, or its expiry, found by the sweep or at a
// Workspace request of its page, which records it, cuts the answer short,
// though the browser has stopped reading, so the koko server's write waits.
// The SSH Session goes on.
func TestFilesDownloadIsCutWhenItsCallerEnds(t *testing.T) {
	t.Parallel()
	// expire lets 8 days pass, the first Sign-in used on the 6th, so the
	// phone's alone expires.
	expire := func(t *testing.T, h *harness.Harness, b *harness.Browser) {
		h.Advance(6 * 24 * time.Hour)
		signInOf(t, b)
		h.Advance(2 * 24 * time.Hour)
	}
	for _, c := range []struct {
		name    string
		timings timing.Timings
		end     func(t *testing.T, h *harness.Harness, b, phone *harness.Browser, ws *harness.WorkspacePage, tab string)
	}{
		{
			name: "revoked",
			end: func(t *testing.T, h *harness.Harness, _, phone *harness.Browser, _ *harness.WorkspacePage, _ string) {
				h.Koko("signin", "revoke", signInOf(t, phone)).WantSuccess(t)
			},
		},
		{
			name:    "expired, found by the sweep",
			timings: timing.Timings{SignInCheck: 20 * time.Millisecond},
			end: func(t *testing.T, h *harness.Harness, b, _ *harness.Browser, _ *harness.WorkspacePage, _ string) {
				expire(t, h, b)
			},
		},
		{
			name: "expired, found at a Workspace request",
			end: func(t *testing.T, h *harness.Harness, b, _ *harness.Browser, ws *harness.WorkspacePage, tab string) {
				expire(t, h, b)
				ws.Send(wire.WorkspaceRequest{Close: tab})
			},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			h := startTerminals(t, harness.Timings(c.timings))
			host := terminalHost(t, h, "web1", nil, sshhost.SFTP())
			b := h.SignIn(harness.Owner)
			w := b.TerminalPage().Open("web1", 80, 24)
			const size = 256 << 20
			sparseFile(t, host.Home(), "big.img", size)
			phone := signInAgain(t, h, harness.Owner)
			ws := phone.TerminalPage().Workspace()

			d := downloadToken(t, phone, w, host.Home()+"/big.img")
			resp := fetchStalling(t, h, phone, api.FilesDownloadPath+"/"+d.Token)
			defer func() { _ = resp.Body.Close() }()
			if _, err := io.ReadFull(resp.Body, make([]byte, 64<<10)); err != nil {
				t.Fatalf("read the download's start: %v", err)
			}
			c.end(t, h, b, phone, ws, w.Tab)
			m := waitLog(t, h, `msg="file transferred" tunnel=`+w.ID+` .* direction=download path=\S+/big.img bytes=(\d+) outcome=failed err="its caller has ended, and the download with it"\n`)
			if moved, _ := strconv.Atoi(m[1]); moved >= size {
				t.Errorf("the cut download moved %d bytes, the whole file", moved)
			}
			if n, err := io.Copy(io.Discard, resp.Body); err == nil {
				t.Errorf("the cut download's answer ended cleanly after %d more bytes, want it cut short", n)
			}
			wantFiles[api.FilesListResponse](t, list(t, b, w.ID, ""))
		})
	}
}

// A download whose caller is revoked after the web front read it and
// before its first byte, here while the Host is frozen in the download's
// first calls, is refused rather than streamed: it was among the downloads
// a change cuts from the caller's read on.
func TestFilesDownloadStartedAsItsCallerEndsIsRefused(t *testing.T) {
	t.Parallel()
	h, host, _, w := filesTerminal(t, sshhost.SFTP())
	sparseFile(t, host.Home(), "big.img", 8<<20)
	phone := signInAgain(t, h, harness.Owner)
	id := signInOf(t, phone)
	// The web front's read of the caller moves its Last used time, kept to
	// the minute, which tells the test the read is done: the token is taken
	// 30 s after the Sign-in's last use, which it does not move, and spent
	// 31 s later, within its 60 s, which does.
	h.Advance(30 * time.Second)
	d := downloadToken(t, phone, w, host.Home()+"/big.img")
	h.Advance(31 * time.Second)
	lastUsed := func() string {
		resp, err := h.Call(api.SignInList, api.SignInListRequest{User: harness.Owner})
		if err != nil {
			t.Fatal(err)
		}
		for _, si := range resp.SignIns {
			if si.ID == id {
				return si.LastUsedAt
			}
		}
		t.Fatalf("signin.list holds no %s", id)
		return ""
	}
	before := lastUsed()

	host.Freeze()
	type got struct {
		status int
		body   int
	}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://"+h.WebAddress()+api.FilesDownloadPath+"/"+d.Token, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = strings.TrimPrefix(harness.WebURL, "https://")
	req.AddCookie(&http.Cookie{Name: "__Host-koko", Value: phone.Cookie})
	tr := &http.Transport{}
	t.Cleanup(tr.CloseIdleConnections)
	fetched := make(chan got, 1)
	go func() {
		resp, err := (&http.Client{Transport: tr}).Do(req)
		if err != nil {
			fetched <- got{status: -1}
			return
		}
		n, _ := io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		fetched <- got{resp.StatusCode, int(n)}
	}()
	deadline := time.Now().Add(10 * time.Second)
	for lastUsed() == before {
		if time.Now().After(deadline) {
			t.Fatal("the download's caller was not read within 10 s")
		}
		time.Sleep(5 * time.Millisecond)
	}
	h.Koko("signin", "revoke", id).WantSuccess(t)
	host.Thaw()
	switch a := <-fetched; a.status {
	case -1:
		t.Error("the download whose caller was revoked as it started got no answer, want its refusal")
	case http.StatusOK:
		t.Errorf("the download whose caller was revoked as it started answered 200 with %d bytes, want it refused", a.body)
	}
}

// A browser that cancels a download midway closes its request, which ends
// the transfer, logged cancelled with the bytes moved.
func TestFilesDownloadCancelledMidway(t *testing.T) {
	t.Parallel()
	h, host, b, w := filesTerminal(t, sshhost.SFTP())
	const size = 256 << 20
	// Sparse: a file far larger than the connection's buffers, made at once.
	f, err := os.Create(filepath.Join(host.Home(), "big.img"))
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(size); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()

	d := downloadToken(t, b, w, host.Home()+"/big.img")
	resp := fetch(t, h, b, api.FilesDownloadPath+"/"+d.Token)
	if _, err := io.ReadFull(resp.Body, make([]byte, 64<<10)); err != nil {
		t.Fatalf("read the download's start: %v", err)
	}
	_ = resp.Body.Close()
	m := waitLog(t, h, `msg="file transferred" tunnel=`+w.ID+` .* direction=download path=\S+/big.img bytes=(\d+) outcome=cancelled\n`)
	if moved, _ := strconv.Atoi(m[1]); moved >= size {
		t.Errorf("the cancelled download moved %d bytes, the whole file", moved)
	}
}
