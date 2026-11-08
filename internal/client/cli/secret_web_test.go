package cli_test

import (
	"encoding/base64"
	"net/http"
	"strings"
	"testing"
)

// The web UI's Secrets page reveals a value through secret.show over
// web@koko, and the answer that carries it says Cache-Control: no-store, so
// the browser keeps no copy of it in its cache. A refused secret.show says
// the same.
func TestSecretShowOverWebIsNeverCached(t *testing.T) {
	t.Parallel()
	h := startWithOps(t)
	h.KokoStdin("s3cret\n", "secret", "add", "ops/db-password").WantSuccess(t)
	w := startWeb(t, h)
	b := newBrowser(t, w.port)
	b.open(w)

	shown := b.postJSON("/v1/secret.show", `{"space":"ops","name":"db-password"}`)
	if want := base64.StdEncoding.EncodeToString([]byte("s3cret\n")); shown.StatusCode != http.StatusOK || !strings.Contains(shown.body, want) {
		t.Fatalf("secret.show over web@koko answered %s %q, want the value as %s", shown.Status, shown.body, want)
	}
	unknown := b.postJSON("/v1/secret.show", `{"space":"ops","name":"nope"}`)
	wantAPIError(t, unknown, http.StatusNotFound, "unknown_secret")
	for _, a := range []*answer{shown, unknown} {
		if got := a.Header.Get("Cache-Control"); got != "no-store" {
			t.Errorf("secret.show answered %s with Cache-Control %q, want no-store", a.Status, got)
		}
	}
}
