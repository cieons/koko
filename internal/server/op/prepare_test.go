package op_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/cieons/koko/internal/server/op"
	"github.com/cieons/koko/internal/server/store"
	"github.com/cieons/koko/internal/shared/api"
)

// prepared is a prepared write the test holds in its slow part:
// note.prepared, whose Prepare counts the notes, and whose transaction adds
// one and answers the count Prepare saw.
type prepared struct {
	op api.Op[noteAddRequest, noteCountResponse]
	// entered is closed once the first Prepare holds.
	entered chan struct{}
	// release lets the first Prepare go on; it is called when the test ends
	// at the latest, so no call outlives the test.
	release func()
	// ran counts the Prepares that ran.
	ran atomic.Int32
	// cancelled says the first Prepare's context had ended once released.
	cancelled atomic.Bool
}

func (f *fixture) prepared(t *testing.T) *prepared {
	t.Helper()
	p := &prepared{op: api.Op[noteAddRequest, noteCountResponse]{Name: "note.prepared", Write: true}, entered: make(chan struct{})}
	released := make(chan struct{})
	p.release = sync.OnceFunc(func() { close(released) })
	t.Cleanup(p.release)
	prepare := func(ctx context.Context, read op.Read, _ op.Caller, req noteAddRequest) (int, error) {
		if p.ran.Add(1) == 1 {
			close(p.entered)
			<-released
			p.cancelled.Store(ctx.Err() != nil)
		}
		if req.Fail == "taken" {
			return 0, api.CodeNameTaken.Errorf("the note %q is taken", req.Text)
		}
		var notes int
		err := read(func(tx store.Tx) error {
			return tx.QueryRowContext(ctx, "SELECT count(*) FROM notes").Scan(&notes)
		})
		return notes, err
	}
	f.api.HandlePrepared(p.op, prepare, func(ctx context.Context, tx store.Tx, _ op.Caller, req noteAddRequest, notes int) (noteCountResponse, error) {
		_, err := tx.ExecContext(ctx, "INSERT INTO notes (text) VALUES (?)", req.Text)
		return noteCountResponse{Text: req.Text, Notes: notes}, err
	})
	return p
}

// request is a note.prepared request with body, carrying key, on ctx.
func (p *prepared) request(ctx context.Context, body string) *http.Request {
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, p.op.Path(), strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(api.IdempotencyKeyHeader, key)
	return req
}

// A prepared write holds no lock while it prepares: another write commits
// meanwhile, instead of waiting for the store's write lock. The write's own
// transaction then gets what its Prepare returned.
func TestAPreparedWriteHoldsNoLockWhileItPrepares(t *testing.T) {
	f := newFixture(t)
	p := f.prepared(t)
	slow := f.sendAsync(t, p.request(t.Context(), `{"text": "slow"}`))
	receive(t, p.entered)
	status, body := f.post(t, noteAdd.Path(), `{"text": "meanwhile"}`)
	wantOK(t, status, body, `{"text":"meanwhile"}`)
	p.release()
	a := receive(t, slow)
	wantOK(t, a.status, a.body, `{"text":"slow","notes":1}`)
	if notes := f.notes(t); notes != `{"notes":["meanwhile","slow"],"local":true}` {
		t.Errorf("notes = %s, want both writes", notes)
	}
}

// A prepared write retried with its Idempotency-Key gets the stored response
// without preparing again: what Prepare does, such as logging in to a Host,
// happens once per write.
func TestARetryOfAPreparedWriteDoesNotPrepareAgain(t *testing.T) {
	f := newFixture(t)
	p := f.prepared(t)
	p.release()
	status, body := f.send(t, local, p.op.Path(), key, `{"text": "a"}`)
	wantOK(t, status, body, `{"text":"a","notes":0}`)
	status, body = f.send(t, local, p.op.Path(), key, `{"text": "a"}`)
	wantOK(t, status, body, `{"text":"a","notes":0}`)
	if n := p.ran.Load(); n != 1 {
		t.Errorf("Prepare ran %d times, want once", n)
	}
	if !strings.Contains(f.log.String(), `msg="api call" path=/v1/note.prepared status=200 replayed=true`) {
		t.Errorf("the log does not show the retry answered with the stored response:\n%s", f.log)
	}
}

// A Prepare that fails ends the write before its transaction: nothing is
// applied or stored, so the retry prepares again.
func TestAFailedPrepareStoresNothing(t *testing.T) {
	f := newFixture(t)
	p := f.prepared(t)
	p.release()
	status, body := f.send(t, local, p.op.Path(), key, `{"text": "a", "fail": "taken"}`)
	wantError(t, status, body, api.CodeNameTaken)
	if notes := f.notes(t); notes != `{"notes":[],"local":true}` {
		t.Errorf("notes = %s, want none", notes)
	}
	status, body = f.send(t, local, p.op.Path(), key, `{"text": "a", "fail": "taken"}`)
	wantError(t, status, body, api.CodeNameTaken)
	if n := p.ran.Load(); n != 2 {
		t.Errorf("Prepare ran %d times, want twice: a failed write stores nothing", n)
	}
}

// A prepared write is never cancelled because its caller dropped, its
// Prepare included: it runs to its end and stores its response for the
// caller's retry.
func TestAPreparedWriteIsNotCancelledWhenItsCallerDrops(t *testing.T) {
	f := newFixture(t)
	p := f.prepared(t)
	ctx, drop := context.WithCancel(t.Context())
	dropped := f.sendAsync(t, p.request(ctx, `{"text": "a"}`))
	receive(t, p.entered)
	drop()
	p.release()
	receive(t, dropped)
	if p.cancelled.Load() {
		t.Error("Prepare's context ended when its caller dropped")
	}
	if notes := f.notes(t); notes != `{"notes":["a"],"local":true}` {
		t.Errorf("notes = %s, want the write applied though its caller dropped", notes)
	}
	status, body := f.send(t, local, p.op.Path(), key, `{"text": "a"}`)
	wantOK(t, status, body, `{"text":"a","notes":0}`)
}

// Before initialisation a prepared write is refused before it prepares.
func TestAPreparedWriteWaitsForInitialisation(t *testing.T) {
	f := newFixture(t)
	p := f.prepared(t)
	p.release()
	f.uninitialised.Store(true)
	status, body := f.send(t, local, p.op.Path(), key, `{"text": "a"}`)
	wantError(t, status, body, api.CodeNotInitialised)
	if n := p.ran.Load(); n != 0 {
		t.Errorf("Prepare ran %d times before initialisation, want never", n)
	}
}
