package space_test

import (
	"context"
	"encoding/json/v2"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/server/op"
	"github.com/cieons/koko/internal/server/seal"
	"github.com/cieons/koko/internal/server/space"
	"github.com/cieons/koko/internal/server/store"
	"github.com/cieons/koko/internal/server/user"
	"github.com/cieons/koko/internal/shared/api"
)

// addOwner records the Owner, alice, so the Local caller acts as someone.
var addOwner = api.Op[struct{}, struct{}]{Name: "test.owner", Write: true}

// holdings plays the areas whose records live in a Space: each counts its
// own records, by Space id, as the test sets them.
type holdings struct {
	mu      sync.Mutex
	hosts   map[string]int
	secrets map[string]int
}

func (h *holdings) set(spaceID string, hosts, secrets int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.hosts[spaceID], h.secrets[spaceID] = hosts, secrets
}

func (h *holdings) countHosts(_ context.Context, _ store.Tx, spaceID string, c *api.SpaceCounts) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	c.Hosts = h.hosts[spaceID]
	return nil
}

func (h *holdings) countSecrets(_ context.Context, _ store.Tx, spaceID string, c *api.SpaceCounts) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	c.Secrets = h.secrets[spaceID]
	return nil
}

// call calls the operation at path with body as the Local caller, with an
// Idempotency-Key of its own, and returns the status and the response body.
func call(t *testing.T, a *op.API, path, body string) (int, string) {
	t.Helper()
	req := httptest.NewRequestWithContext(op.WithCaller(t.Context(), op.Caller{Local: true}), http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(api.IdempotencyKeyHeader, api.NewIdempotencyKey())
	rec := httptest.NewRecorder()
	a.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

// space show counts what each area holds in the Space, through the
// Holdings it was given, and space rm refuses a Space while any area holds
// a record in it, naming what is left; once nothing is left, it removes it.
func TestASpaceThatHoldsRecordsIsNotRemoved(t *testing.T) {
	st, err := store.Open(t.Context(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	rootKey, err := seal.Load(harness.NewRootKeyFile(t))
	if err != nil {
		t.Fatal(err)
	}
	a := op.New(st, rootKey, slog.New(slog.DiscardHandler),
		func(context.Context, store.Tx) (bool, error) { return true, nil }, time.Now)
	h := &holdings{hosts: map[string]int{}, secrets: map[string]int{}}
	space.Register(a, user.ByName, []space.Holding{h.countHosts, h.countSecrets})
	a.Handle(addOwner, func(ctx context.Context, tx store.Tx, _ op.Caller, _ struct{}) (struct{}, error) {
		return struct{}{}, user.AddOwner(ctx, tx, "alice")
	})
	if status, body := call(t, a, addOwner.Path(), `{}`); status != http.StatusOK {
		t.Fatalf("record the Owner: %d %s", status, body)
	}
	status, body := call(t, a, api.SpaceAdd.Path(), `{"name": "ops"}`)
	var added api.SpaceAddResponse
	if err := json.Unmarshal([]byte(body), &added); status != http.StatusOK || err != nil {
		t.Fatalf("space.add ops: %d %s", status, body)
	}
	id := added.Space.ID

	h.set(id, 2, 1)
	status, body = call(t, a, api.SpaceShow.Path(), `{"name": "ops"}`)
	var shown api.SpaceShowResponse
	if err := json.Unmarshal([]byte(body), &shown); status != http.StatusOK || err != nil {
		t.Fatalf("space.show ops: %d %s", status, body)
	}
	if want := (api.SpaceCounts{Hosts: 2, Secrets: 1}); shown.Space.Counts != want {
		t.Errorf("space.show ops counts %+v, want %+v", shown.Space.Counts, want)
	}
	for _, tc := range []struct {
		hosts, secrets int
		left           string
	}{
		{2, 1, "2 Hosts and 1 Secret"},
		{1, 0, "1 Host"},
		{0, 3, "3 Secrets"},
	} {
		h.set(id, tc.hosts, tc.secrets)
		status, body := call(t, a, api.SpaceRemove.Path(), `{"name": "ops"}`)
		if want := "the Space ops still holds " + tc.left + ": move or remove them first"; status != api.CodeSpaceNotEmpty.Status ||
			!strings.Contains(body, `"code":"space_not_empty"`) || !strings.Contains(body, want) {
			t.Errorf("space.remove of ops holding %d Hosts and %d Secrets = %d %s, want space_not_empty saying %q", tc.hosts, tc.secrets, status, body, want)
		}
	}
	if status, body := call(t, a, api.SpaceShow.Path(), `{"name": "ops"}`); status != http.StatusOK {
		t.Errorf("space.show ops after the refusals = %d %s, want it still there", status, body)
	}

	h.set(id, 0, 0)
	if status, body := call(t, a, api.SpaceRemove.Path(), `{"name": "ops"}`); status != http.StatusOK || !strings.Contains(body, id) {
		t.Errorf("space.remove of the empty ops = %d %s, want it removed", status, body)
	}
	if status, _ := call(t, a, api.SpaceShow.Path(), `{"name": "ops"}`); status != api.CodeUnknownSpace.Status {
		t.Errorf("space.show ops after space.remove = %d, want unknown_space", status)
	}
}
