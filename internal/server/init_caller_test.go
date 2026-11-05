package server

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cieons/koko/internal/server/deployment"
	"github.com/cieons/koko/internal/server/device"
	"github.com/cieons/koko/internal/server/op"
	"github.com/cieons/koko/internal/server/seal"
	"github.com/cieons/koko/internal/server/store"
	"github.com/cieons/koko/internal/shared/api"
)

// server.init answers only the Local caller, over the Unix socket: any other
// caller, such as a Device Session once the koko server listens on TCP, is
// refused with local_only and initialises nothing. The S1 harness reaches
// the koko server only as the Local caller, so this calls the API as the
// caller it names.
func TestInitAnswersOnlyTheLocalCaller(t *testing.T) {
	st, err := store.Open(t.Context(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	keyFile := filepath.Join(t.TempDir(), "root.key")
	if err := os.WriteFile(keyFile, seal.NewRootKey(), 0o600); err != nil {
		t.Fatal(err)
	}
	rootKey, err := seal.Load(keyFile)
	if err != nil {
		t.Fatal(err)
	}
	a := op.New(st, slog.New(slog.DiscardHandler), deployment.Initialised, time.Now)
	registerServer(a, rootKey, device.NewIssuer(joinTarget(rootKey)))

	call := func(caller op.Caller) *httptest.ResponseRecorder {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, api.ServerInit.Path(),
			strings.NewReader(`{"owner": "alice", "address": "koko.test"}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		a.ServeHTTP(rec, req.WithContext(op.WithCaller(req.Context(), caller)))
		return rec
	}
	if rec := call(op.Caller{}); rec.Code != api.CodeLocalOnly.Status || !strings.Contains(rec.Body.String(), `"code":"local_only"`) {
		t.Errorf("server.init as a caller that is not the Local caller = %d %s, want local_only", rec.Code, rec.Body)
	}
	var initialised bool
	err = st.Read(t.Context(), func(tx store.Tx) (err error) {
		initialised, err = deployment.Initialised(t.Context(), tx)
		return err
	})
	if err != nil || initialised {
		t.Errorf("after the refusal, initialised = %v, %v; want false", initialised, err)
	}
	if rec := call(op.Caller{Local: true}); rec.Code != http.StatusOK {
		t.Errorf("server.init as the Local caller = %d %s", rec.Code, rec.Body)
	}
}
