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

	"github.com/cieons/koko/internal/server/op"
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
	a := op.New(st, slog.New(slog.DiscardHandler),
		func(context.Context, store.Tx) (bool, error) { return true, nil }, time.Now)
	space.Register(a)
	failing := true
	user.Register(a, func(context.Context, store.Tx, op.Caller, api.DeviceAddRequest) (api.JoinCode, error) {
		if failing {
			return api.JoinCode{}, errors.New("no Join Code today")
		}
		return api.JoinCode{Device: "device-1"}, nil
	})
	call := func(path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequestWithContext(op.WithCaller(t.Context(), op.Caller{Local: true}), http.MethodPost, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		a.ServeHTTP(rec, req)
		return rec
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
