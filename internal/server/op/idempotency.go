package op

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/cieons/koko/internal/server/store"
	"github.com/cieons/koko/internal/shared/api"
)

// The Idempotency-Key store (P0 spec: reach, ADR 0021). Every write carries
// an Idempotency-Key; inside the write's own transaction the framework looks
// up the caller's key, and answers with the response stored for it, refuses
// a key that came with another request, or runs the write and stores its
// response, so that a write and its stored response commit together or not
// at all. A write that fails applies nothing and stores nothing, so its
// retry runs again. A retry that arrives while the first attempt runs waits
// for it.

// responseKept is how long a write's response is kept for its key, on the
// koko server's clock.
const responseKept = 10 * time.Minute

// localCaller names the Local caller in the store; no Device Session's or
// Sign-in's id takes this form. A call carries one of the three alone.
const localCaller = "local"

// idempotencyKey is one caller's Idempotency-Key: a key belongs to its
// caller.
type idempotencyKey struct {
	caller string
	key    string
}

// recordID is what the response stored for k is sealed for, so that it
// opens for no other caller or key.
func (k idempotencyKey) recordID() string {
	return "idempotency_key:" + k.caller + ":" + k.key
}

// keyedWrite is a write as the store knows it: its caller's key, and the
// digest of its request, which identifies the request by its operation and
// its body without keeping the body, since it may hold a Secret's value.
type keyedWrite struct {
	key    idempotencyKey
	digest [sha256.Size]byte
}

// newKeyedWrite is the write of the operation name, with body, that caller
// sent carrying key.
func newKeyedWrite(caller Caller, key, name string, body []byte) keyedWrite {
	w := keyedWrite{key: idempotencyKey{caller: caller.DeviceSession + caller.SignIn, key: key}}
	if caller.Local {
		w.key.caller = localCaller
	}
	h := sha256.New()
	h.Write([]byte(name))
	h.Write([]byte{0})
	h.Write(body)
	w.digest = [sha256.Size]byte(h.Sum(nil))
	return w
}

// findResponse returns the response stored for w's key, now, as
// lookupResponse does, inside a write's transaction. It first forgets every
// response kept past responseKept, so an expired key is free again.
func (a *API) findResponse(ctx context.Context, tx store.Tx, w keyedWrite, now time.Time) (body []byte, found bool, err error) {
	if _, err := tx.ExecContext(ctx, "DELETE FROM idempotency_keys WHERE expires_at <= ?", now.Format(time.RFC3339)); err != nil {
		return nil, false, fmt.Errorf("forget the expired responses of Idempotency-Keys: %w", err)
	}
	return a.lookupResponse(ctx, tx, w, now)
}

// lookupResponse returns the response stored for w's key and not expired
// by now, when it was stored for w's request; found is false when the key
// has none. The same key with another request is refused with
// CodeIdempotencyKeyReused. It writes nothing, so a read-only transaction
// may ask it, as a prepared write does before it prepares.
func (a *API) lookupResponse(ctx context.Context, tx store.Tx, w keyedWrite, now time.Time) (body []byte, found bool, err error) {
	var sealed []byte
	err = tx.QueryRowContext(ctx, "SELECT sealed FROM idempotency_keys WHERE caller = ? AND idempotency_key = ? AND expires_at > ?",
		w.key.caller, w.key.key, now.Format(time.RFC3339)).Scan(&sealed)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("look up an Idempotency-Key: %w", err)
	}
	plain, err := a.rootKey.Open(w.key.recordID(), sealed)
	if err != nil {
		return nil, false, fmt.Errorf("open the response stored for an Idempotency-Key of %s: %w", w.key.caller, err)
	}
	if len(plain) < sha256.Size || !bytes.Equal(plain[:sha256.Size], w.digest[:]) {
		return nil, false, api.CodeIdempotencyKeyReused.Errorf(
			"this Idempotency-Key came with another request within the last %v: a retry sends the same request with its key, and a new write a new key", responseKept)
	}
	return plain[sha256.Size:], true, nil
}

// keepResponse keeps body, the response to w, for w's key until
// responseKept after now, sealed under the Root Key with w's digest.
func (a *API) keepResponse(ctx context.Context, tx store.Tx, w keyedWrite, body []byte, now time.Time) error {
	sealed := a.rootKey.Seal(w.key.recordID(), append(w.digest[:], body...))
	_, err := tx.ExecContext(ctx, "INSERT INTO idempotency_keys (caller, idempotency_key, expires_at, sealed) VALUES (?, ?, ?, ?)",
		w.key.caller, w.key.key, now.Add(responseKept).Format(time.RFC3339), sealed)
	if err != nil {
		return fmt.Errorf("keep the response for an Idempotency-Key of %s: %w", w.key.caller, err)
	}
	return nil
}

// inflight holds the writes running with an Idempotency-Key, so a retry
// waits for the attempt it repeats: a write may run longer than SQLite waits
// for its lock.
type inflight struct {
	mu      sync.Mutex
	running map[idempotencyKey]chan struct{}
}

// enter returns once no other write with k runs, calling waiting first if
// one does, and marks this one as running until leave. It waits before the
// write's transaction begins, so a waiting retry holds no lock.
func (f *inflight) enter(k idempotencyKey, waiting func()) (leave func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for {
		done, busy := f.running[k]
		if !busy {
			break
		}
		f.mu.Unlock()
		waiting()
		<-done
		f.mu.Lock()
	}
	done := make(chan struct{})
	f.running[k] = done
	return func() {
		f.mu.Lock()
		defer f.mu.Unlock()
		delete(f.running, k)
		close(done)
	}
}
