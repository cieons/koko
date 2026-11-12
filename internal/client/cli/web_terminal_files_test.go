package cli_test

import (
	"encoding/json/v2"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/harness/sshhost"
	"github.com/cieons/koko/internal/shared/api"
)

// Through koko web, every files call is a POST from koko web's own address
// alone: another Origin, or none, is refused with bad_origin, even with
// the cookie, where an API call needs no Origin; a page without the cookie
// gets cookie_required; and a page of the run lists the Host's files as
// the Local caller.
func TestFilesThroughKokoWeb(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	host := tunnelHost(t, h, "web1", sshhost.SFTP())
	w := startWeb(t, h)
	p := webTerminalPage(t, w)
	term := p.Open("web1", 80, 24)
	b := newBrowser(t, w.port)
	post := func(path, origin string, cookie *http.Cookie) *answer {
		t.Helper()
		req := b.request(http.MethodPost, path, strings.NewReader(`{"tunnel":"`+term.ID+`","path":""}`))
		req.Header.Set("Content-Type", "application/json")
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		if cookie != nil {
			req.AddCookie(cookie)
		}
		a, err := b.do(req)
		if err != nil {
			t.Fatalf("POST %s: %v", path, err)
		}
		return a
	}

	for _, path := range []string{api.FilesListPath, api.FilesUploadPath, api.FilesChunkPath, api.FilesFinishPath, api.FilesCancelPath, api.FilesDownloadPath} {
		for _, origin := range []string{"", "http://localhost:1", "https://koko.example.com"} {
			wantAPIError(t, post(path, origin, p.Cookie), http.StatusForbidden, "bad_origin")
		}
	}
	own := fmt.Sprintf("http://localhost:%d", w.port)
	wantAPIError(t, post(api.FilesListPath, own, nil), http.StatusUnauthorized, "cookie_required")
	// The home is named by its SFTP path, which on Windows is not the
	// OS's: the listing is known by what it holds.
	if err := os.WriteFile(filepath.Join(host.Home(), "marker.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	a := post(api.FilesListPath, own, p.Cookie)
	var got api.FilesListResponse
	err := json.Unmarshal([]byte(a.body), &got)
	if a.StatusCode != http.StatusOK || err != nil || !slices.ContainsFunc(got.Entries, func(e api.FileEntry) bool { return e.Name == "marker.txt" }) {
		t.Errorf("the listing through koko web answered %s %q, want the Host Account's home, holding marker.txt", a.Status, a.body)
	}
}
