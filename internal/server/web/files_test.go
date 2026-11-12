package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

// A file under the build's assets/, whose name carries a hash of its
// content, is cached for a year and never asked for again; a .wasm file is
// served as application/wasm, so a browser compiles it while it downloads.
// index.html, a file outside assets/, and the index.html a path that names
// no file gets, a missing asset's included, carry no caching header: a
// release changes what they hold under the same name.
func TestHashedBuildFilesAreCachedAndTheRestAreNot(t *testing.T) {
	t.Parallel()
	f := &Front{files: fstest.MapFS{
		"index.html":                      {Data: []byte("<!doctype html><title>koko</title>")},
		"favicon.svg":                     {Data: []byte("<svg xmlns='http://www.w3.org/2000/svg'/>")},
		"assets/index-BqX3kT9a.js":        {Data: []byte("export {};")},
		"assets/ghostty-vt-D4f1c2e7.wasm": {Data: []byte("\x00asm\x01\x00\x00\x00")},
	}}
	const long = "public, max-age=31536000, immutable"
	for _, c := range []struct {
		path, cacheControl, contentType string
	}{
		{"/assets/index-BqX3kT9a.js", long, "text/javascript; charset=utf-8"},
		{"/assets/ghostty-vt-D4f1c2e7.wasm", long, "application/wasm"},
		{"/", "", "text/html; charset=utf-8"},
		{"/index.html", "", "text/html; charset=utf-8"},
		{"/hosts", "", "text/html; charset=utf-8"},
		{"/favicon.svg", "", "image/svg+xml"},
		{"/assets/index-0ldH4sh0.js", "", "text/html; charset=utf-8"},
	} {
		w := httptest.NewRecorder()
		f.serveFile(w, httptest.NewRequestWithContext(t.Context(), http.MethodGet, c.path, nil), refusePage)
		if w.Code != http.StatusOK {
			t.Errorf("GET %s: status %d, want 200", c.path, w.Code)
		}
		if got := w.Header().Get("Cache-Control"); got != c.cacheControl {
			t.Errorf("GET %s: Cache-Control = %q, want %q", c.path, got, c.cacheControl)
		}
		if got := w.Header().Get("Content-Type"); got != c.contentType {
			t.Errorf("GET %s: Content-Type = %q, want %q", c.path, got, c.contentType)
		}
	}
}
