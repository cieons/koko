package op_test

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cieons/koko/internal/server/op"
	"github.com/cieons/koko/internal/server/store"
	"github.com/cieons/koko/internal/shared/api"
)

type noteAddRequest struct {
	Text string `json:"text"`
	Fail string `json:"fail"`
}

type noteAddResponse struct {
	Text string `json:"text"`
}

type noteListRequest struct{}

type noteListResponse struct {
	Notes []string `json:"notes"`
	Local bool     `json:"local"`
}

// Test operations over a table of their own, so these tests depend on no area.
var (
	noteAdd  = api.Op[noteAddRequest, noteAddResponse]{Name: "note.add", Write: true}
	noteList = api.Op[noteListRequest, noteListResponse]{Name: "note.list"}
)

type fixture struct {
	api   *op.API
	store *store.Store
	log   *bytes.Buffer
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	s, err := store.Open(t.Context(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	err = s.Write(t.Context(), func(tx store.Tx) error {
		_, err := tx.ExecContext(t.Context(), "CREATE TABLE notes (text TEXT NOT NULL) STRICT")
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	var log bytes.Buffer
	a := op.New(s, slog.New(slog.NewTextHandler(&log, &slog.HandlerOptions{Level: slog.LevelDebug})))
	a.Handle(noteAdd, func(ctx context.Context, tx store.Tx, _ op.Caller, req noteAddRequest) (noteAddResponse, error) {
		if _, err := tx.ExecContext(ctx, "INSERT INTO notes (text) VALUES (?)", req.Text); err != nil {
			return noteAddResponse{}, err
		}
		switch req.Fail {
		case "taken":
			return noteAddResponse{}, api.CodeNameTaken.Errorf("the note %q is taken", req.Text)
		case "unexpected":
			return noteAddResponse{}, errors.New("disk on fire")
		}
		return noteAddResponse{Text: req.Text}, nil
	})
	a.Handle(noteList, func(ctx context.Context, tx store.Tx, c op.Caller, _ noteListRequest) (noteListResponse, error) {
		rows, err := tx.QueryContext(ctx, "SELECT text FROM notes ORDER BY text")
		if err != nil {
			return noteListResponse{}, err
		}
		defer func() { _ = rows.Close() }()
		resp := noteListResponse{Local: c.Local}
		for rows.Next() {
			var text string
			if err := rows.Scan(&text); err != nil {
				return noteListResponse{}, err
			}
			resp.Notes = append(resp.Notes, text)
		}
		return resp, rows.Err()
	})
	return &fixture{api: a, store: s, log: &log}
}

// call sends one request as the Local caller and returns the status and body.
func (f *fixture) call(t *testing.T, method, path, contentType, body string) (int, string) {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), method, path, strings.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req = req.WithContext(op.WithCaller(req.Context(), op.Caller{Local: true}))
	rec := httptest.NewRecorder()
	f.api.ServeHTTP(rec, req)
	b, err := io.ReadAll(rec.Result().Body)
	if err != nil {
		t.Fatal(err)
	}
	return rec.Code, string(b)
}

func (f *fixture) post(t *testing.T, path, body string) (int, string) {
	t.Helper()
	return f.call(t, http.MethodPost, path, "application/json", body)
}

// wantError checks a failed call's status and its error object.
func wantError(t *testing.T, status int, body string, code api.Code) api.Error {
	t.Helper()
	if status != code.Status {
		t.Errorf("status = %d, want %d", status, code.Status)
	}
	var resp api.ErrorResponse
	if err := json.Unmarshal([]byte(body), &resp, json.RejectUnknownMembers(true)); err != nil {
		t.Fatalf("error body %q is not {\"error\": {...}}: %v", body, err)
	}
	if resp.Error.Code != code.Name {
		t.Errorf("code = %q, want %q (message %q)", resp.Error.Code, code.Name, resp.Error.Message)
	}
	if resp.Error.Message == "" {
		t.Error("the error has no message")
	}
	return resp.Error
}

func TestOperationAnswersJSON(t *testing.T) {
	f := newFixture(t)
	status, body := f.post(t, "/v1/note.add", `{"text": "hello"}`)
	if status != http.StatusOK || body != `{"text":"hello"}` {
		t.Fatalf("note.add = %d %s", status, body)
	}
	status, body = f.post(t, "/v1/note.list", `{}`)
	if status != http.StatusOK || body != `{"notes":["hello"],"local":true}` {
		t.Errorf("note.list = %d %s", status, body)
	}
}

// A list is wrapped in an object, and an empty list is [] rather than null.
func TestEmptyListIsAnEmptyArray(t *testing.T) {
	f := newFixture(t)
	status, body := f.post(t, "/v1/note.list", `{}`)
	if status != http.StatusOK || body != `{"notes":[],"local":true}` {
		t.Errorf("note.list = %d %s", status, body)
	}
}

func TestUnknownRequestFieldIsRefused(t *testing.T) {
	f := newFixture(t)
	status, body := f.post(t, "/v1/note.add", `{"text": "hello", "pinned": true}`)
	e := wantError(t, status, body, api.CodeUnknownField)
	if !strings.Contains(e.Message, "pinned") {
		t.Errorf("message %q does not name the field", e.Message)
	}
	if _, list := f.post(t, "/v1/note.list", `{}`); strings.Contains(list, "hello") {
		t.Error("the refused call was applied")
	}
}

func TestMalformedRequestsAreRefused(t *testing.T) {
	f := newFixture(t)
	for _, tc := range []struct {
		name                      string
		method, path, ctype, body string
		code                      api.Code
	}{
		{"unknown operation", http.MethodPost, "/v1/note.frob", "application/json", `{}`, api.CodeUnknownOperation},
		{"no version", http.MethodPost, "/note.add", "application/json", `{}`, api.CodeUnknownOperation},
		{"GET", http.MethodGet, "/v1/note.list", "", "", api.CodeMethodNotAllowed},
		{"form body", http.MethodPost, "/v1/note.list", "text/plain", `{}`, api.CodeUnsupportedMediaType},
		{"no content type", http.MethodPost, "/v1/note.list", "", `{}`, api.CodeUnsupportedMediaType},
		{"empty body", http.MethodPost, "/v1/note.list", "application/json", ``, api.CodeInvalidRequest},
		{"not an object", http.MethodPost, "/v1/note.add", "application/json", `["hello"]`, api.CodeInvalidRequest},
		{"wrong type", http.MethodPost, "/v1/note.add", "application/json", `{"text": 1}`, api.CodeInvalidRequest},
		{"trailing data", http.MethodPost, "/v1/note.add", "application/json", `{"text": "a"} {}`, api.CodeInvalidRequest},
		{"duplicate field", http.MethodPost, "/v1/note.add", "application/json", `{"text": "a", "text": "b"}`, api.CodeInvalidRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, body := f.call(t, tc.method, tc.path, tc.ctype, tc.body)
			wantError(t, status, body, tc.code)
		})
	}
}

func TestJSONWithCharsetIsAccepted(t *testing.T) {
	f := newFixture(t)
	status, body := f.call(t, http.MethodPost, "/v1/note.list", "application/json; charset=utf-8", `{}`)
	if status != http.StatusOK {
		t.Errorf("note.list = %d %s", status, body)
	}
}

// A coded error keeps its code and status, and the write it ended is rolled back.
func TestCodedErrorRollsBackTheWrite(t *testing.T) {
	f := newFixture(t)
	status, body := f.post(t, "/v1/note.add", `{"text": "hello", "fail": "taken"}`)
	e := wantError(t, status, body, api.CodeNameTaken)
	if e.Message != `the note "hello" is taken` {
		t.Errorf("message = %q", e.Message)
	}
	if _, list := f.post(t, "/v1/note.list", `{}`); strings.Contains(list, "hello") {
		t.Errorf("the failed write was committed: %s", list)
	}
}

// An unexpected error reaches the log, not the caller, and is rolled back too.
func TestUnexpectedErrorIsInternalAndLogged(t *testing.T) {
	f := newFixture(t)
	status, body := f.post(t, "/v1/note.add", `{"text": "hello", "fail": "unexpected"}`)
	e := wantError(t, status, body, api.CodeInternal)
	if strings.Contains(e.Message, "disk on fire") {
		t.Errorf("message %q leaks the internal error", e.Message)
	}
	if !strings.Contains(f.log.String(), "disk on fire") {
		t.Errorf("log does not hold the internal error:\n%s", f.log)
	}
	if _, list := f.post(t, "/v1/note.list", `{}`); strings.Contains(list, "hello") {
		t.Errorf("the failed write was committed: %s", list)
	}
}

// An error code no area registered is a bug: the caller gets internal_error.
func TestUnregisteredCodeIsInternal(t *testing.T) {
	f := newFixture(t)
	bogus := api.Op[noteListRequest, noteListResponse]{Name: "note.bogus"}
	f.api.Handle(bogus, func(context.Context, store.Tx, op.Caller, noteListRequest) (noteListResponse, error) {
		return noteListResponse{}, api.Code{Name: "never_registered", Status: http.StatusTeapot}.Errorf("no")
	})
	status, body := f.post(t, "/v1/note.bogus", `{}`)
	wantError(t, status, body, api.CodeInternal)
}

// The API never serves a call that reached it without a caller.
func TestCallWithoutCallerIsRefused(t *testing.T) {
	f := newFixture(t)
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/note.list", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	f.api.ServeHTTP(rec, req)
	b, _ := io.ReadAll(rec.Result().Body)
	wantError(t, rec.Code, string(b), api.CodeInternal)
}

func TestRegisteringAnOperationTwicePanics(t *testing.T) {
	f := newFixture(t)
	defer func() {
		if recover() == nil {
			t.Error("a second handler for note.list did not panic")
		}
	}()
	f.api.Handle(noteList, func(context.Context, store.Tx, op.Caller, noteListRequest) (noteListResponse, error) {
		return noteListResponse{}, nil
	})
}
