package user_test

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/server/access"
	"github.com/cieons/koko/internal/server/hostlist"
	"github.com/cieons/koko/internal/server/op"
	"github.com/cieons/koko/internal/server/seal"
	"github.com/cieons/koko/internal/server/space"
	"github.com/cieons/koko/internal/server/store"
	"github.com/cieons/koko/internal/server/user"
	"github.com/cieons/koko/internal/shared/api"
)

// The User, their Personal Space and their first Join Code are made in one
// transaction: when the Join Code cannot be issued, neither the User nor the
// Personal Space is left behind, and the name stays free.
func TestUserInviteIsOneTransaction(t *testing.T) {
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
	space.Register(a, user.ByName, nil, &access.Signal{}, &hostlist.Signal{})
	// Only the Owner invites, so the Local caller needs an Owner to act as.
	addOwner := api.Op[struct{}, struct{}]{Name: "test.owner", Write: true}
	a.Handle(addOwner, func(ctx context.Context, tx store.Tx, _ op.Caller, _ struct{}) (struct{}, error) {
		return struct{}{}, user.AddOwner(ctx, tx, "alice")
	})
	failing := true
	user.Register(a, func(context.Context, store.Tx, op.Caller, api.DeviceAddRequest) (api.JoinCode, error) {
		if failing {
			return api.JoinCode{}, errors.New("no Join Code today")
		}
		return api.JoinCode{Device: "device-1"}, nil
	}, nil, nil, nil, &access.Signal{}, &hostlist.Signal{}, slog.New(slog.DiscardHandler))
	call := func(path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequestWithContext(op.WithCaller(t.Context(), op.Caller{Local: true}), http.MethodPost, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(api.IdempotencyKeyHeader, api.NewIdempotencyKey())
		rec := httptest.NewRecorder()
		a.ServeHTTP(rec, req)
		return rec
	}

	if rec := call(addOwner.Path(), `{}`); rec.Code != http.StatusOK {
		t.Fatalf("record the Owner: %d %s", rec.Code, rec.Body)
	}
	if rec := call(api.UserInvite.Path(), `{"name": "bob"}`); rec.Code != api.CodeInternal.Status {
		t.Fatalf("user.invite with a failing Join Code = %d %s, want internal_error", rec.Code, rec.Body)
	}
	if rec := call(api.SpaceList.Path(), `{}`); strings.Contains(rec.Body.String(), "bob") {
		t.Errorf("space.list = %s: the failed Invite left bob's Personal Space", rec.Body)
	}
	failing = false
	if rec := call(api.UserInvite.Path(), `{"name": "bob"}`); rec.Code != http.StatusOK {
		t.Errorf("user.invite again = %d %s, want bob invited: the failed Invite left the User", rec.Code, rec.Body)
	}
}

// usedBrowsers stands in for the signin area: every User has used a
// browser, so user.remove Removes them, and they hold nothing to end.
type usedBrowsers struct{ user.Browsers }

func (usedBrowsers) Used(context.Context, store.Tx, api.User) (bool, error) { return true, nil }
func (usedBrowsers) Remove(context.Context, store.Tx, api.User, string) (int64, int64, error) {
	return 0, 0, nil
}

func (usedBrowsers) VoidLinksIssuedBy(context.Context, store.Tx, api.User) (int64, error) {
	return 0, nil
}

// deletingWorkspaces deletes a User's tabs in the transaction it is handed,
// then fails when fail is set, as a later step of the call would.
type deletingWorkspaces struct{ fail bool }

func (w *deletingWorkspaces) Delete(ctx context.Context, tx store.Tx, userID string) error {
	if _, err := tx.ExecContext(ctx, "DELETE FROM workspace_tabs WHERE user_id = ?", userID); err != nil {
		return err
	}
	if w.fail {
		return errors.New("the call fails after the delete")
	}
	return nil
}

// user.remove deletes the Removed User's Workspace in its own transaction:
// a call that fails after the delete keeps every tab, and one that
// succeeds leaves none.
func TestUserRemoveDeletesTheWorkspaceInItsTransaction(t *testing.T) {
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
	space.Register(a, user.ByName, nil, &access.Signal{}, &hostlist.Signal{})
	setUp := api.Op[struct{}, struct{}]{Name: "test.setup", Write: true}
	a.Handle(setUp, func(ctx context.Context, tx store.Tx, _ op.Caller, _ struct{}) (struct{}, error) {
		return struct{}{}, user.AddOwner(ctx, tx, "alice")
	})
	addTabs := api.Op[struct{}, struct{}]{Name: "test.tabs", Write: true}
	a.Handle(addTabs, func(ctx context.Context, tx store.Tx, _ op.Caller, _ struct{}) (struct{}, error) {
		bob, err := user.ByName(ctx, tx, "bob")
		if err != nil {
			return struct{}{}, err
		}
		for i := range 2 {
			_, err := tx.ExecContext(ctx, `INSERT INTO workspace_tabs (id, user_id, position, host_id, label, opened_at)
				VALUES (?, ?, ?, 'host_web1', 'web1', '2026-11-13T00:00:00Z')`, strings.Repeat(string(rune('a'+i)), 32), bob.ID, i)
			if err != nil {
				return struct{}{}, err
			}
		}
		return struct{}{}, nil
	})
	workspaces := &deletingWorkspaces{fail: true}
	user.Register(a, func(context.Context, store.Tx, op.Caller, api.DeviceAddRequest) (api.JoinCode, error) {
		return api.JoinCode{Device: "device-1"}, nil
	}, usedBrowsers{}, workspaces, nil, &access.Signal{}, &hostlist.Signal{}, slog.New(slog.DiscardHandler))
	call := func(path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequestWithContext(op.WithCaller(t.Context(), op.Caller{Local: true}), http.MethodPost, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(api.IdempotencyKeyHeader, api.NewIdempotencyKey())
		rec := httptest.NewRecorder()
		a.ServeHTTP(rec, req)
		return rec
	}
	tabs := func() int {
		var n int
		if err := st.Read(t.Context(), func(tx store.Tx) error {
			return tx.QueryRowContext(t.Context(), "SELECT count(*) FROM workspace_tabs").Scan(&n)
		}); err != nil {
			t.Fatal(err)
		}
		return n
	}
	for _, c := range []struct{ path, body string }{{setUp.Path(), `{}`}, {api.UserInvite.Path(), `{"name": "bob"}`}, {addTabs.Path(), `{}`}} {
		if rec := call(c.path, c.body); rec.Code != http.StatusOK {
			t.Fatalf("%s = %d %s", c.path, rec.Code, rec.Body)
		}
	}

	if rec := call(api.UserRemove.Path(), `{"name": "bob"}`); rec.Code != api.CodeInternal.Status {
		t.Fatalf("user.remove failing after the delete = %d %s, want internal_error", rec.Code, rec.Body)
	}
	if n := tabs(); n != 2 {
		t.Errorf("after a user.remove that failed the store holds %d tabs, want both kept", n)
	}
	workspaces.fail = false
	if rec := call(api.UserRemove.Path(), `{"name": "bob"}`); rec.Code != http.StatusOK {
		t.Fatalf("user.remove = %d %s", rec.Code, rec.Body)
	}
	if n := tabs(); n != 0 {
		t.Errorf("after user.remove the store holds %d tabs, want none", n)
	}
}
