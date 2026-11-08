package op_test

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/server/op"
	"github.com/cieons/koko/internal/server/seal"
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

type noteTimeResponse struct {
	Time string `json:"time"`
}

type noteListResponse struct {
	Notes []string `json:"notes"`
	Local bool     `json:"local"`
}

// noteCountResponse is what note.count answers: how many notes there are
// once it has added its own, which differs each time it runs.
type noteCountResponse struct {
	Text  string `json:"text"`
	Notes int    `json:"notes"`
}

// Test operations over a table of their own, so these tests depend on no area.
var (
	noteAdd   = api.Op[noteAddRequest, noteAddResponse]{Name: "note.add", Write: true}
	noteList  = api.Op[noteListRequest, noteListResponse]{Name: "note.list"}
	noteTime  = api.Op[noteListRequest, noteTimeResponse]{Name: "note.time"}
	noteCount = api.Op[noteAddRequest, noteCountResponse]{Name: "note.count", Write: true}
)

type fixture struct {
	api     *op.API
	store   *store.Store
	dataDir string
	rootKey *seal.RootKey
	log     *syncBuffer
	// uninitialised plays a koko server not initialised yet.
	uninitialised atomic.Bool
	// clock is the koko server's clock.
	clock atomic.Pointer[time.Time]
	// refuse makes note.count fail with name_taken once it has added its
	// note.
	refuse atomic.Bool
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{dataDir: t.TempDir(), log: &syncBuffer{}}
	s, err := store.Open(t.Context(), f.dataDir)
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
	if f.rootKey, err = seal.Load(harness.NewRootKeyFile(t)); err != nil {
		t.Fatal(err)
	}
	f.clock.Store(new(time.Now()))
	f.serve(s)
	return f
}

// serve makes the fixture's API anew over s, with its test operations, as a
// koko server that starts on s does.
func (f *fixture) serve(s *store.Store) {
	f.store = s
	a := op.New(s, f.rootKey, slog.New(slog.NewTextHandler(f.log, &slog.HandlerOptions{Level: slog.LevelDebug})),
		func(context.Context, store.Tx) (bool, error) { return !f.uninitialised.Load(), nil },
		func() time.Time { return *f.clock.Load() })
	f.api = a
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
	a.Handle(noteTime, func(ctx context.Context, _ store.Tx, _ op.Caller, _ noteListRequest) (noteTimeResponse, error) {
		first := op.Now(ctx)
		f.clock.Store(new(first.Add(time.Hour))) // the clock moves on during the call
		if !op.Now(ctx).Equal(first) {
			return noteTimeResponse{}, errors.New("op.Now changed during the call")
		}
		return noteTimeResponse{Time: first.Format(time.RFC3339Nano)}, nil
	})
	a.Handle(noteCount, func(ctx context.Context, tx store.Tx, _ op.Caller, req noteAddRequest) (noteCountResponse, error) {
		if _, err := tx.ExecContext(ctx, "INSERT INTO notes (text) VALUES (?)", req.Text); err != nil {
			return noteCountResponse{}, err
		}
		if f.refuse.Load() {
			return noteCountResponse{}, api.CodeNameTaken.Errorf("the note %q is taken", req.Text)
		}
		resp := noteCountResponse{Text: req.Text}
		return resp, tx.QueryRowContext(ctx, "SELECT count(*) FROM notes").Scan(&resp.Notes)
	})
}

// call sends one request as the Local caller and returns the status and body.
func (f *fixture) call(t *testing.T, method, path, contentType, body string) (int, string) {
	t.Helper()
	return f.callAs(t, op.Caller{Local: true}, method, path, contentType, body)
}

// callAs sends one request as caller, with an Idempotency-Key of its own,
// and returns the status and body.
func (f *fixture) callAs(t *testing.T, caller op.Caller, method, path, contentType, body string) (int, string) {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), method, path, strings.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.Header.Set(api.IdempotencyKeyHeader, api.NewIdempotencyKey())
	return f.serveRequest(t, caller, req)
}

// serveRequest has the API answer req as caller, and returns the status and
// body. It never fails the test, so a goroutine may call it.
func (f *fixture) serveRequest(t *testing.T, caller op.Caller, req *http.Request) (int, string) {
	t.Helper()
	req = req.WithContext(op.WithCaller(req.Context(), caller))
	rec := httptest.NewRecorder()
	f.api.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

// syncBuffer is a log the API writes while a test reads it.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
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

// The koko server reads a request body of up to 16 MiB, and refuses a
// longer one with request_too_large.
func TestARequestBodyOverTheLimitIsRefused(t *testing.T) {
	f := newFixture(t)
	const limit = 16 << 20
	padded := func(n int) string { return "{" + strings.Repeat(" ", n-2) + "}" }
	if status, body := f.post(t, noteList.Path(), padded(limit)); status != http.StatusOK {
		t.Errorf("note.list with a body of 16 MiB = %d %s, want it read", status, body)
	}
	status, body := f.post(t, noteList.Path(), padded(limit+1))
	if e := wantError(t, status, body, api.CodeRequestTooLarge); !strings.Contains(e.Message, "16 MiB") {
		t.Errorf("message %q does not name the limit", e.Message)
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

// Until the koko server is initialised, an operation is refused with
// not_initialised and does not run, unless it is registered to answer before
// initialisation.
func TestOperationsWaitForInitialisation(t *testing.T) {
	f := newFixture(t)
	f.uninitialised.Store(true)
	early := api.Op[noteListRequest, noteListResponse]{Name: "note.early"}
	f.api.Handle(early, func(context.Context, store.Tx, op.Caller, noteListRequest) (noteListResponse, error) {
		return noteListResponse{Notes: []string{"early"}}, nil
	}, op.BeforeInit)

	status, body := f.post(t, "/v1/note.add", `{"text": "hello"}`)
	wantError(t, status, body, api.CodeNotInitialised)
	status, body = f.post(t, "/v1/note.early", `{}`)
	if status != http.StatusOK || body != `{"notes":["early"],"local":false}` {
		t.Errorf("note.early before initialisation = %d %s", status, body)
	}

	f.uninitialised.Store(false)
	if status, body := f.post(t, "/v1/note.list", `{}`); status != http.StatusOK || body != `{"notes":[],"local":true}` {
		t.Errorf("note.list after initialisation = %d %s, want no note from the refused call", status, body)
	}
}

// A Local-caller-only operation refuses every other caller with local_only,
// and does not run for them.
func TestLocalOnlyOperationRefusesOtherCallers(t *testing.T) {
	f := newFixture(t)
	ran := 0
	local := api.Op[noteListRequest, noteListResponse]{Name: "note.local", Write: true}
	f.api.Handle(local, func(context.Context, store.Tx, op.Caller, noteListRequest) (noteListResponse, error) {
		ran++
		return noteListResponse{}, nil
	}, op.LocalOnly)

	status, body := f.callAs(t, op.Caller{}, http.MethodPost, "/v1/note.local", "application/json", `{}`)
	wantError(t, status, body, api.CodeLocalOnly)
	if ran != 0 {
		t.Error("note.local ran for a caller that is not the Local caller")
	}
	if status, body := f.post(t, "/v1/note.local", `{}`); status != http.StatusOK || ran != 1 {
		t.Errorf("note.local as the Local caller = %d %s, ran %d time(s)", status, body, ran)
	}
}

// Every call reads the time once, from the koko server's clock, in UTC to
// the second, so the times an operation records agree.
func TestNowIsTheClockAtTheStartOfTheCall(t *testing.T) {
	f := newFixture(t)
	f.clock.Store(new(time.Date(2026, 11, 5, 16, 3, 7, 900_000_000, time.FixedZone("", 2*60*60))))
	status, body := f.post(t, noteTime.Path(), `{}`)
	if status != http.StatusOK || body != `{"time":"2026-11-05T14:03:07Z"}` {
		t.Errorf("note.time = %d %s, want the clock's time in UTC to the second", status, body)
	}
}

// AfterTx runs once the call's transaction has ended, told whether it
// committed: true for a call that succeeded, false for one whose write was
// rolled back, so a call can start what must outlive it only once its write
// holds, and undo what it began otherwise.
func TestAfterTxIsToldWhetherTheCallCommitted(t *testing.T) {
	f := newFixture(t)
	var ended []bool
	hooked := api.Op[noteAddRequest, noteAddResponse]{Name: "note.hooked", Write: true}
	f.api.Handle(hooked, func(ctx context.Context, tx store.Tx, _ op.Caller, req noteAddRequest) (noteAddResponse, error) {
		op.AfterTx(ctx, func(committed bool) { ended = append(ended, committed) })
		if _, err := tx.ExecContext(ctx, "INSERT INTO notes (text) VALUES (?)", req.Text); err != nil {
			return noteAddResponse{}, err
		}
		if req.Fail != "" {
			return noteAddResponse{}, api.CodeNameTaken.Errorf("the note %q is taken", req.Text)
		}
		if len(ended) != 0 {
			return noteAddResponse{}, errors.New("AfterTx ran inside the transaction")
		}
		return noteAddResponse{Text: req.Text}, nil
	})
	if status, body := f.post(t, hooked.Path(), `{"text": "kept"}`); status != http.StatusOK {
		t.Fatalf("note.hooked = %d %s", status, body)
	}
	status, body := f.post(t, hooked.Path(), `{"text": "dropped", "fail": "yes"}`)
	wantError(t, status, body, api.CodeNameTaken)
	if !slices.Equal(ended, []bool{true, false}) {
		t.Errorf("AfterTx was told %v, want [true false]: committed, then rolled back", ended)
	}
}

// A write of the koko server's own, which no caller asked for, runs in one
// transaction with the call's time and AfterTx, as an operation does: it
// commits when it returns no error, rolls back when it does, and AfterTx
// is told which, once the transaction has ended.
func TestDoRunsAWriteOfTheKokoServersOwn(t *testing.T) {
	f := newFixture(t)
	f.clock.Store(new(time.Date(2026, 11, 8, 9, 30, 15, 500_000_000, time.UTC)))
	var ended []bool
	do := func(text string, fail error) error {
		before := len(ended)
		return f.api.Do(t.Context(), func(ctx context.Context, tx store.Tx) error {
			op.AfterTx(ctx, func(committed bool) { ended = append(ended, committed) })
			if got := op.Now(ctx); !got.Equal(time.Date(2026, 11, 8, 9, 30, 15, 0, time.UTC)) {
				return fmt.Errorf("op.Now = %v, want the clock's time in UTC to the second", got)
			}
			if _, err := tx.ExecContext(ctx, "INSERT INTO notes (text) VALUES (?)", text); err != nil {
				return err
			}
			if len(ended) != before {
				return errors.New("AfterTx ran inside the transaction")
			}
			return fail
		})
	}
	if err := do("kept", nil); err != nil {
		t.Fatal(err)
	}
	refusal := api.CodeNameTaken.Errorf("the note is taken")
	if err := do("dropped", refusal); !errors.Is(err, refusal) {
		t.Errorf("Do returned %v, want the write's own error", err)
	}
	if !slices.Equal(ended, []bool{true, false}) {
		t.Errorf("AfterTx was told %v, want [true false]: committed, then rolled back", ended)
	}
	status, body := f.post(t, noteList.Path(), `{}`)
	if status != http.StatusOK || body != `{"notes":["kept"],"local":true}` {
		t.Errorf("note.list = %d %s, want only the note the committed write added", status, body)
	}
}
