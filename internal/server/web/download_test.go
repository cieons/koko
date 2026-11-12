package web

import (
	"bytes"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A request that fails on the web front's side logs its path, and a
// download's GET logs it without its token, which would let anyone reading
// the log download the file.
func TestAFailedDownloadLogsNoToken(t *testing.T) {
	t.Parallel()
	var log bytes.Buffer
	f := &Front{log: slog.New(slog.NewTextHandler(&log, nil))}
	const token = "0123456789abcdef0123456789abcdef"
	r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/terminal/files/download/"+token, nil)
	f.fail(httptest.NewRecorder(), r, errors.New("the store failed"), listenerPage)
	if got := log.String(); strings.Contains(got, token) || !strings.Contains(got, "path=/terminal/files/download/<token>") {
		t.Errorf("the failure logged %q, want the path without the token", got)
	}
}
