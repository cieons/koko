package space_test

import (
	"context"
	"encoding/json/v2"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/server/access"
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
	retired map[string]int
	secrets map[string]int
}

func (h *holdings) set(spaceID string, hosts, retired, secrets int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.hosts[spaceID], h.retired[spaceID], h.secrets[spaceID] = hosts, retired, secrets
}

func (h *holdings) countHosts(_ context.Context, _ store.Tx, spaceID string, c *api.SpaceCounts) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	c.Hosts, c.RetiredHosts = h.hosts[spaceID], h.retired[spaceID]
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

// newSpaces serves the space operations over a new store, with the Owner,
// alice, recorded, and returns the stand-in Holdings it counts through.
func newSpaces(t *testing.T) (*op.API, *holdings) {
	t.Helper()
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
	h := &holdings{hosts: map[string]int{}, retired: map[string]int{}, secrets: map[string]int{}}
	space.Register(a, user.ByName, []space.Holding{h.countHosts, h.countSecrets}, &access.Signal{})
	a.Handle(addOwner, func(ctx context.Context, tx store.Tx, _ op.Caller, _ struct{}) (struct{}, error) {
		return struct{}{}, user.AddOwner(ctx, tx, "alice")
	})
	if status, body := call(t, a, addOwner.Path(), `{}`); status != http.StatusOK {
		t.Fatalf("record the Owner: %d %s", status, body)
	}
	return a, h
}

// space show counts what each area holds in the Space, through the
// Holdings it was given, and space rm refuses a Space while any area holds
// a record in it, naming what is left; once nothing is left, it removes it.
func TestASpaceThatHoldsRecordsIsNotRemoved(t *testing.T) {
	a, h := newSpaces(t)
	id := addSpace(t, a, "ops")

	h.set(id, 2, 1, 1)
	status, body := call(t, a, api.SpaceShow.Path(), `{"name": "ops"}`)
	var shown api.SpaceShowResponse
	if err := json.Unmarshal([]byte(body), &shown); status != http.StatusOK || err != nil {
		t.Fatalf("space.show ops: %d %s", status, body)
	}
	if want := (api.SpaceCounts{Hosts: 2, RetiredHosts: 1, Secrets: 1}); shown.Space.Counts != want {
		t.Errorf("space.show ops counts %+v, want %+v", shown.Space.Counts, want)
	}
	for _, tc := range []struct {
		hosts, retired, secrets int
		left                    string
	}{
		{2, 0, 1, "2 Hosts and 1 Secret"},
		{1, 0, 0, "1 Host"},
		{0, 0, 3, "3 Secrets"},
		{0, 1, 0, "1 Retired Host"},
		{1, 2, 0, "1 Host and 2 Retired Hosts"},
	} {
		h.set(id, tc.hosts, tc.retired, tc.secrets)
		status, body := call(t, a, api.SpaceRemove.Path(), `{"name": "ops"}`)
		if want := "the Space ops still holds " + tc.left + ": move or remove them first"; status != api.CodeSpaceNotEmpty.Status ||
			!strings.Contains(body, `"code":"space_not_empty"`) || !strings.Contains(body, want) {
			t.Errorf("space.remove of ops holding %d Hosts, %d Retired Hosts and %d Secrets = %d %s, want space_not_empty saying %q",
				tc.hosts, tc.retired, tc.secrets, status, body, want)
		}
	}
	if status, body := call(t, a, api.SpaceShow.Path(), `{"name": "ops"}`); status != http.StatusOK {
		t.Errorf("space.show ops after the refusals = %d %s, want it still there", status, body)
	}

	h.set(id, 0, 0, 0)
	if status, body := call(t, a, api.SpaceRemove.Path(), `{"name": "ops"}`); status != http.StatusOK || !strings.Contains(body, id) {
		t.Errorf("space.remove of the empty ops = %d %s, want it removed", status, body)
	}
	if status, _ := call(t, a, api.SpaceShow.Path(), `{"name": "ops"}`); status != api.CodeUnknownSpace.Status {
		t.Errorf("space.show ops after space.remove = %d, want unknown_space", status)
	}
}

// addSpace adds the shared Space name and returns its id.
func addSpace(t *testing.T, a *op.API, name string) string {
	t.Helper()
	status, body := call(t, a, api.SpaceAdd.Path(), `{"name": "`+name+`"}`)
	var added api.SpaceAddResponse
	if err := json.Unmarshal([]byte(body), &added); status != http.StatusOK || err != nil {
		t.Fatalf("space.add %s: %d %s", name, status, body)
	}
	return added.Space.ID
}

// space.list answers each Space with its Members and counts, as space.show
// answers one, so a page of the Spaces and their Members takes one call.
func TestSpaceListAnswersEachSpaceWithItsMembersAndCounts(t *testing.T) {
	a, h := newSpaces(t)
	h.set(addSpace(t, a, "ops"), 3, 1, 2)
	h.set(addSpace(t, a, "data"), 0, 0, 5)

	status, listBody := call(t, a, api.SpaceList.Path(), `{}`)
	var listed api.SpaceListResponse
	if err := json.Unmarshal([]byte(listBody), &listed); status != http.StatusOK || err != nil {
		t.Fatalf("space.list: %d %s", status, listBody)
	}
	want := map[string]api.SpaceCounts{
		"alice": {},
		"data":  {Secrets: 5},
		"ops":   {Hosts: 3, RetiredHosts: 1, Secrets: 2},
	}
	var names []string
	for _, sp := range listed.Spaces {
		names = append(names, sp.Name)
		if sp.Counts != want[sp.Name] {
			t.Errorf("space.list counts %s as %+v, want %+v", sp.Name, sp.Counts, want[sp.Name])
		}
		status, showBody := call(t, a, api.SpaceShow.Path(), `{"name": "`+sp.Name+`"}`)
		var shown api.SpaceShowResponse
		if err := json.Unmarshal([]byte(showBody), &shown); status != http.StatusOK || err != nil {
			t.Fatalf("space.show %s: %d %s", sp.Name, status, showBody)
		}
		if !reflect.DeepEqual(sp, shown.Space) {
			t.Errorf("space.list answers %s as %+v, space.show as %+v; want the same", sp.Name, sp, shown.Space)
		}
	}
	if !slices.Equal(names, []string{"alice", "data", "ops"}) {
		t.Errorf("space.list lists %q, want the Owner's Personal Space, data and ops, by name", names)
	}
	if !strings.Contains(listBody, `"members":[]`) {
		t.Errorf("space.list answered %s, want a Space with no Members to list them as []", listBody)
	}
}

// A Member's space.list, over their own Device Connection, answers their
// Personal Space and the Spaces they are a Member of, each with all its
// Members, and nothing of a Space they are not in, nor of its Members.
func TestSpaceListAnswersAMemberTheirOwnSpacesWithTheirMembers(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	for _, u := range []string{"bob", "carol", "dave"} {
		h.Koko("user", "invite", u).WantSuccess(t)
	}
	for _, s := range []string{"ops", "data"} {
		h.Koko("space", "add", s).WantSuccess(t)
	}
	h.Koko("space", "member", "add", "ops", "bob", "carol").WantSuccess(t)
	h.Koko("space", "member", "add", "data", "dave").WantSuccess(t)

	status, body := h.Join("bob").Send(api.SpaceList, []byte(`{}`))
	var listed api.SpaceListResponse
	if err := json.Unmarshal(body, &listed); status != http.StatusOK || err != nil {
		t.Fatalf("bob's space.list: %d %s", status, body)
	}
	var got []string
	for _, sp := range listed.Spaces {
		var members []string
		for _, m := range sp.Members {
			members = append(members, m.Name)
		}
		got = append(got, sp.Name+":"+strings.Join(members, ","))
	}
	if want := []string{"bob:", "ops:bob,carol"}; !slices.Equal(got, want) {
		t.Errorf("bob's space.list answers %q, want %q: his Personal Space, and ops with both its Members", got, want)
	}
}
