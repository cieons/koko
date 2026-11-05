package op_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cieons/koko/internal/server/op"
	"github.com/cieons/koko/internal/server/store"
	"github.com/cieons/koko/internal/shared/api"
)

var (
	local   = op.Caller{Local: true}
	deviceA = op.Caller{DeviceSession: "device_3f9k2m7q"}
	deviceB = op.Caller{DeviceSession: "device_7h2k9m3q"}
)

// key is an Idempotency-Key, as a CLI makes one: 128 random bits in 32
// lowercase hex characters.
const key = "0123456789abcdef0123456789abcdef"

// send posts body to path as caller, carrying the Idempotency-Key k, or none
// when k is "", and returns the status and body.
func (f *fixture) send(t *testing.T, caller op.Caller, path, k, body string) (int, string) {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if k != "" {
		req.Header.Set(api.IdempotencyKeyHeader, k)
	}
	return f.serveRequest(t, caller, req)
}

// notes returns the notes the fixture's table holds.
func (f *fixture) notes(t *testing.T) string {
	t.Helper()
	_, body := f.post(t, noteList.Path(), `{}`)
	return body
}

// wantOK fails the test unless a call answered 200 with want.
func wantOK(t *testing.T, status int, body, want string) {
	t.Helper()
	if status != http.StatusOK || body != want {
		t.Fatalf("answered %d %s, want 200 %s", status, body, want)
	}
}

// Every write carries an Idempotency-Key of 128 bits in 32 lowercase hex
// characters: a write without one, or with another form, is refused and does
// nothing. A read needs none.
func TestAWriteWithoutAnIdempotencyKeyIsRefused(t *testing.T) {
	f := newFixture(t)
	for name, k := range map[string]string{
		"none":      "",
		"short":     key[1:],
		"long":      key + "0",
		"uppercase": strings.ToUpper(key),
		"not hex":   "0123456789abcdef0123456789abcdeg",
	} {
		status, body := f.send(t, local, noteCount.Path(), k, `{"text": "a"}`)
		if e := wantError(t, status, body, api.CodeInvalidRequest); !strings.Contains(e.Message, "Idempotency-Key") {
			t.Errorf("%s: message %q does not name the Idempotency-Key", name, e.Message)
		}
	}
	if notes := f.notes(t); notes != `{"notes":[],"local":true}` {
		t.Errorf("notes = %s, want none from the refused writes", notes)
	}
	status, body := f.send(t, local, noteList.Path(), "", `{}`)
	wantOK(t, status, body, `{"notes":[],"local":true}`)
}

// A write retried with its Idempotency-Key gets the response of the first
// attempt, and is not applied again.
func TestARetryReturnsTheStoredResponse(t *testing.T) {
	f := newFixture(t)
	status, body := f.send(t, local, noteCount.Path(), key, `{"text": "a"}`)
	wantOK(t, status, body, `{"text":"a","notes":1}`)
	status, body = f.send(t, local, noteCount.Path(), key, `{"text": "a"}`)
	wantOK(t, status, body, `{"text":"a","notes":1}`)
	if notes := f.notes(t); notes != `{"notes":["a"],"local":true}` {
		t.Errorf("notes = %s, want the one note of the first attempt", notes)
	}
	if !strings.Contains(f.log.String(), `msg="api call" path=/v1/note.count status=200 replayed=true`) {
		t.Errorf("the log does not show the retry answered with the stored response:\n%s", f.log)
	}
}

// The same Idempotency-Key with another request, another body or another
// operation, is refused with idempotency_key_reused, and does nothing.
func TestTheSameKeyWithAnotherRequestIsRefused(t *testing.T) {
	f := newFixture(t)
	status, body := f.send(t, local, noteCount.Path(), key, `{"text": "a"}`)
	wantOK(t, status, body, `{"text":"a","notes":1}`)
	status, body = f.send(t, local, noteCount.Path(), key, `{"text": "b"}`)
	wantError(t, status, body, api.CodeIdempotencyKeyReused)
	status, body = f.send(t, local, noteAdd.Path(), key, `{"text": "a"}`)
	wantError(t, status, body, api.CodeIdempotencyKeyReused)
	if notes := f.notes(t); notes != `{"notes":["a"],"local":true}` {
		t.Errorf("notes = %s, want only the first write's", notes)
	}
}

// An Idempotency-Key belongs to its caller: another caller's write with the
// same key is a write of its own.
func TestKeysArePerCaller(t *testing.T) {
	f := newFixture(t)
	status, body := f.send(t, local, noteCount.Path(), key, `{"text": "a"}`)
	wantOK(t, status, body, `{"text":"a","notes":1}`)
	status, body = f.send(t, deviceA, noteCount.Path(), key, `{"text": "a"}`)
	wantOK(t, status, body, `{"text":"a","notes":2}`)
	status, body = f.send(t, deviceB, noteCount.Path(), key, `{"text": "b"}`)
	wantOK(t, status, body, `{"text":"b","notes":3}`)
	status, body = f.send(t, deviceA, noteCount.Path(), key, `{"text": "a"}`)
	wantOK(t, status, body, `{"text":"a","notes":2}`)
}

// A stored response is kept for 10 minutes on the koko server's clock; after
// that the key is forgotten, and a write with it is applied anew.
func TestAStoredResponseIsKeptTenMinutes(t *testing.T) {
	f := newFixture(t)
	start := *f.clock.Load()
	status, body := f.send(t, local, noteCount.Path(), key, `{"text": "a"}`)
	wantOK(t, status, body, `{"text":"a","notes":1}`)
	f.clock.Store(new(start.Add(10*time.Minute - time.Second)))
	status, body = f.send(t, local, noteCount.Path(), key, `{"text": "a"}`)
	wantOK(t, status, body, `{"text":"a","notes":1}`)
	f.clock.Store(new(start.Add(10 * time.Minute)))
	status, body = f.send(t, local, noteCount.Path(), key, `{"text": "a"}`)
	wantOK(t, status, body, `{"text":"a","notes":2}`)
}

// gate is a write operation whose first run holds until the test releases
// it: note.held, which adds a note and answers how many times it has run.
type gate struct {
	op api.Op[noteAddRequest, noteCountResponse]
	// entered is closed once the first run holds.
	entered chan struct{}
	// release lets the first run go on; it is called when the test ends at
	// the latest, so no call outlives the test.
	release func()
	ran     atomic.Int32
}

func (f *fixture) gate(t *testing.T) *gate {
	t.Helper()
	g := &gate{op: api.Op[noteAddRequest, noteCountResponse]{Name: "note.held", Write: true}, entered: make(chan struct{})}
	released := make(chan struct{})
	g.release = sync.OnceFunc(func() { close(released) })
	t.Cleanup(g.release)
	f.api.Handle(g.op, func(ctx context.Context, tx store.Tx, _ op.Caller, req noteAddRequest) (noteCountResponse, error) {
		if g.ran.Add(1) == 1 {
			close(g.entered)
			<-released
		}
		_, err := tx.ExecContext(ctx, "INSERT INTO notes (text) VALUES (?)", req.Text)
		return noteCountResponse{Text: req.Text, Notes: int(g.ran.Load())}, err
	})
	return g
}

// answer is a call's status and body, from a goroutine.
type answer struct {
	status int
	body   string
}

// sendAsync sends what send sends, from a goroutine, and returns where its
// answer arrives.
func (f *fixture) sendAsync(t *testing.T, req *http.Request) <-chan answer {
	t.Helper()
	c := make(chan answer, 1)
	go func() {
		status, body := f.serveRequest(t, local, req)
		c <- answer{status, body}
	}()
	return c
}

// keyed is a note.held request carrying key, on ctx.
func keyed(ctx context.Context, g *gate) *http.Request {
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, g.op.Path(), strings.NewReader(`{"text": "a"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(api.IdempotencyKeyHeader, key)
	return req
}

// receive waits up to 5 s for c.
func receive[T any](t *testing.T, c <-chan T) T {
	t.Helper()
	select {
	case v := <-c:
		return v
	case <-time.After(5 * time.Second):
		t.Fatal("nothing arrived within 5 s")
		panic("unreachable")
	}
}

// A retry that arrives while the first attempt still runs waits for it, and
// gets its response; the write runs once.
func TestARetryDuringTheFirstAttemptWaitsForIt(t *testing.T) {
	f := newFixture(t)
	g := f.gate(t)
	first := f.sendAsync(t, keyed(t.Context(), g))
	receive(t, g.entered)
	retry := f.sendAsync(t, keyed(t.Context(), g))
	waitFor(t, func() bool {
		return strings.Contains(f.log.String(), `msg="api call waits for its first attempt" path=/v1/note.held`)
	})
	g.release()
	a, b := receive(t, first), receive(t, retry)
	wantOK(t, a.status, a.body, `{"text":"a","notes":1}`)
	wantOK(t, b.status, b.body, `{"text":"a","notes":1}`)
	if n := g.ran.Load(); n != 1 {
		t.Errorf("the write ran %d times, want once", n)
	}
}

// waitFor waits up to 5 s for cond to hold.
func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); !cond(); {
		if time.Now().After(deadline) {
			t.Fatal("the condition did not hold within 5 s")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// A write that fails applies nothing and stores nothing, so its retry runs
// again.
func TestAFailedWriteStoresNoResponse(t *testing.T) {
	f := newFixture(t)
	f.refuse.Store(true)
	status, body := f.send(t, local, noteCount.Path(), key, `{"text": "a"}`)
	wantError(t, status, body, api.CodeNameTaken)
	f.refuse.Store(false)
	status, body = f.send(t, local, noteCount.Path(), key, `{"text": "a"}`)
	wantOK(t, status, body, `{"text":"a","notes":1}`)
}

// A write and its stored response commit together or not at all: when the
// response cannot be stored, the write is rolled back too.
func TestAWriteAndItsStoredResponseCommitTogether(t *testing.T) {
	f := newFixture(t)
	f.exec(t, `CREATE TRIGGER no_room BEFORE INSERT ON idempotency_keys BEGIN SELECT RAISE(ABORT, 'no room'); END`)
	status, body := f.send(t, local, noteCount.Path(), key, `{"text": "a"}`)
	wantError(t, status, body, api.CodeInternal)
	if notes := f.notes(t); notes != `{"notes":[],"local":true}` {
		t.Errorf("notes = %s, want the write rolled back with its response", notes)
	}
	f.exec(t, `DROP TRIGGER no_room`)
	status, body = f.send(t, local, noteCount.Path(), key, `{"text": "a"}`)
	wantOK(t, status, body, `{"text":"a","notes":1}`)
}

// exec runs one statement in a write transaction of the fixture's store.
func (f *fixture) exec(t *testing.T, statement string) {
	t.Helper()
	err := f.store.Write(t.Context(), func(tx store.Tx) error {
		_, err := tx.ExecContext(t.Context(), statement)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

// A write whose response cannot be encoded is rolled back, so the caller
// never gets an error for a write that was applied.
func TestAWriteWhoseResponseCannotBeEncodedIsRolledBack(t *testing.T) {
	f := newFixture(t)
	type unencodable struct {
		C chan int `json:"c"`
	}
	bad := api.Op[noteAddRequest, unencodable]{Name: "note.bad", Write: true}
	f.api.Handle(bad, func(ctx context.Context, tx store.Tx, _ op.Caller, req noteAddRequest) (unencodable, error) {
		_, err := tx.ExecContext(ctx, "INSERT INTO notes (text) VALUES (?)", req.Text)
		return unencodable{C: make(chan int)}, err
	})
	status, body := f.send(t, local, bad.Path(), key, `{"text": "a"}`)
	wantError(t, status, body, api.CodeInternal)
	if notes := f.notes(t); notes != `{"notes":[],"local":true}` {
		t.Errorf("notes = %s, want the write rolled back", notes)
	}
}

// A stored response outlives a restart of the koko server, since it is
// stored in the write's own transaction.
func TestAStoredResponseOutlivesARestart(t *testing.T) {
	f := newFixture(t)
	status, body := f.send(t, local, noteCount.Path(), key, `{"text": "a"}`)
	wantOK(t, status, body, `{"text":"a","notes":1}`)
	if err := f.store.Close(); err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(t.Context(), f.dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	f.serve(s)
	status, body = f.send(t, local, noteCount.Path(), key, `{"text": "a"}`)
	wantOK(t, status, body, `{"text":"a","notes":1}`)
}

// A write is never cancelled because its caller dropped: it runs to its end,
// commits with its stored response, and the caller's retry gets that
// response.
func TestAWriteIsNotCancelledWhenItsCallerDrops(t *testing.T) {
	f := newFixture(t)
	g := f.gate(t)
	ctx, drop := context.WithCancel(t.Context())
	dropped := f.sendAsync(t, keyed(ctx, g))
	receive(t, g.entered)
	drop()
	g.release()
	receive(t, dropped)
	if notes := f.notes(t); notes != `{"notes":["a"],"local":true}` {
		t.Errorf("notes = %s, want the write applied though its caller dropped", notes)
	}
	status, body := f.send(t, local, g.op.Path(), key, `{"text": "a"}`)
	wantOK(t, status, body, `{"text":"a","notes":1}`)
}
