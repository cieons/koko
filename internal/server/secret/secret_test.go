package secret_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json/v2"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/server/op"
	"github.com/cieons/koko/internal/server/seal"
	"github.com/cieons/koko/internal/server/secret"
	"github.com/cieons/koko/internal/server/store"
	"github.com/cieons/koko/internal/server/user"
	"github.com/cieons/koko/internal/shared/api"
)

// marker is a Secret value made for these tests, looked for in what the
// koko server answers and logs.
const marker = "koko-marker-value-Hq8Vt3Rk6P"

// startOn starts a koko server on dataDir with the Root Key file keyFile,
// initialising it unless it is initialised already.
func startOn(t *testing.T, dataDir, keyFile string, initialised bool) *harness.Harness {
	t.Helper()
	opts := []harness.Option{harness.DataDir(dataDir), harness.RootKeyFile(keyFile)}
	if initialised {
		opts = append(opts, harness.NoInit())
	}
	return harness.Start(t, opts...)
}

// writeStore runs fn in a write transaction on the data directory of a koko
// server that is stopped, as someone editing a copy of it would.
func writeStore(t *testing.T, dataDir string, fn func(tx store.Tx) error) {
	t.Helper()
	st, err := store.Open(t.Context(), dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	if err := st.Write(t.Context(), fn); err != nil {
		t.Fatal(err)
	}
}

// The binding test: a sealed value opens only for its own Secret. Copied
// onto another Secret's record, or with one byte changed, it fails to show,
// naming the Secret and printing no value; the other Secrets still show
// (P0 spec: Secrets, story 41). It and TestOnlySecretSetReseals are the
// tests that know the table holding Secrets.
func TestASealedValueOpensOnlyForItsOwnSecret(t *testing.T) {
	dataDir, keyFile := t.TempDir(), harness.NewRootKeyFile(t)
	h := startOn(t, dataDir, keyFile, false)
	h.Koko("space", "add", "ops").WantSuccess(t)
	h.KokoStdin(marker+"-first", "secret", "add", "ops/first").WantSuccess(t)
	h.KokoStdin(marker+"-second", "secret", "add", "ops/second").WantSuccess(t)
	h.Stop()

	writeStore(t, dataDir, func(tx store.Tx) error {
		_, err := tx.ExecContext(t.Context(), `UPDATE secrets SET sealed = (SELECT sealed FROM secrets WHERE name = 'first')
			WHERE name = 'second'`)
		return err
	})
	h = startOn(t, dataDir, keyFile, true)
	r := h.Koko("secret", "show", "ops/second", "--json")
	msg := r.WantError(t, 1, "secret_unreadable")
	if !strings.Contains(msg, "the Secret ops/second cannot be shown") || strings.Contains(r.Stderr, marker) {
		t.Errorf("stderr %q, want it to name ops/second and hold no value", r.Stderr)
	}
	if r := h.Koko("secret", "show", "ops/second"); r.Status != 1 || r.Stdout != "" || strings.Contains(r.Stderr, marker) {
		t.Errorf("for people: exit status %d, stdout %q, stderr %q", r.Status, r.Stdout, r.Stderr)
	}
	if got := h.Koko("secret", "show", "ops/first").Stdout; got != marker+"-first" {
		t.Errorf("secret show ops/first printed %q, want its own value", got)
	}
	h.Stop()

	writeStore(t, dataDir, func(tx store.Tx) error {
		var sealed []byte
		if err := tx.QueryRowContext(t.Context(), "SELECT sealed FROM secrets WHERE name = 'first'").Scan(&sealed); err != nil {
			return err
		}
		sealed[len(sealed)-1] ^= 1
		_, err := tx.ExecContext(t.Context(), "UPDATE secrets SET sealed = ? WHERE name = 'first'", sealed)
		return err
	})
	h = startOn(t, dataDir, keyFile, true)
	r = h.Koko("secret", "show", "ops/first", "--json")
	msg = r.WantError(t, 1, "secret_unreadable")
	if !strings.Contains(msg, "the Secret ops/first cannot be shown") || strings.Contains(r.Stderr, marker) {
		t.Errorf("stderr %q, want it to name ops/first and hold no value", r.Stderr)
	}
	if strings.Contains(h.Log(), marker) {
		t.Errorf("the koko server's log holds a value")
	}
}

// b64 is s in standard base64, as JSON carries a Secret value.
func b64(s string) string {
	return base64.StdEncoding.EncodeToString([]byte(s))
}

// send calls o as the Local caller with body, and returns the status and
// the error object, if any.
func send(t *testing.T, h *harness.Harness, o api.Operation, body string) (int, api.Error) {
	t.Helper()
	status, b := h.Send(o, []byte(body))
	var e api.ErrorResponse
	if status != http.StatusOK {
		if err := json.Unmarshal(b, &e); err != nil {
			t.Fatalf("%s answered %d %s: %v", o.Info().Name, status, b, err)
		}
	}
	return status, e.Error
}

// The koko server checks a value whatever sent it, as the CLI checks it
// first: a value of 1 byte to exactly 64 KiB is stored; an empty one, one
// byte over 64 KiB and one that is not base64 are refused, and no refusal
// holds the value.
func TestTheKokoServerChecksEveryValue(t *testing.T) {
	h := harness.Start(t)
	h.Koko("space", "add", "ops").WantSuccess(t)
	full := strings.Repeat("m", api.SecretMaxSize)
	over := full[len(marker)-1:] + marker
	for _, tc := range []struct {
		name   string
		body   string
		status int
		code   string
	}{
		{"one byte", `{"space":"ops","name":"one","value_base64":"` + b64("m") + `"}`, http.StatusOK, ""},
		{"64 KiB", `{"space":"ops","name":"full","value_base64":"` + b64(full) + `"}`, http.StatusOK, ""},
		{
			"64 KiB and one byte", `{"space":"ops","name":"over","value_base64":"` + b64(over) + `"}`,
			http.StatusRequestEntityTooLarge, "value_too_large",
		},
		{"empty", `{"space":"ops","name":"empty","value_base64":""}`, http.StatusBadRequest, "invalid_request"},
		{"left out", `{"space":"ops","name":"empty"}`, http.StatusBadRequest, "invalid_request"},
		{"not base64", `{"space":"ops","name":"raw","value_base64":"` + marker + `!"}`, http.StatusBadRequest, "invalid_request"},
		{"the value as text", `{"space":"ops","name":"raw","value":"` + marker + `"}`, http.StatusBadRequest, "unknown_field"},
	} {
		status, e := send(t, h, api.SecretAdd, tc.body)
		if status != tc.status || e.Code != tc.code {
			t.Errorf("%s: answered %d %+v, want %d %s", tc.name, status, e, tc.status, tc.code)
		}
		if strings.Contains(e.Message, marker) || strings.Contains(e.Message, b64(marker)) {
			t.Errorf("%s: the refusal holds the value: %q", tc.name, e.Message)
		}
	}
	if len(over) != api.SecretMaxSize+1 {
		t.Fatalf("the value over the limit is %d bytes", len(over))
	}
	r := h.Koko("secret", "ls", "ops", "--json")
	if strings.Count(r.Stdout, `"name"`) != 2 {
		t.Errorf("secret ls ops = %s, want only the two values stored", r.Stdout)
	}
}

// Every Secret operation refuses a request field it does not know, so a
// newer CLI's option, such as a flag asking for values, is never silently
// dropped.
func TestSecretOperationsRefuseUnknownFields(t *testing.T) {
	h := harness.Start(t)
	h.Koko("space", "add", "ops").WantSuccess(t)
	for _, tc := range []struct {
		o    api.Operation
		body string
	}{
		{api.SecretList, `{"space":"ops","values":true}`},
		{api.SecretShow, `{"space":"ops","name":"db-password","format":"raw"}`},
		{api.SecretAdd, `{"space":"ops","name":"db-password","value_base64":"bQ==","overwrite":true}`},
		{api.SecretSet, `{"space":"ops","name":"db-password","value_base64":"bQ==","create":true}`},
		{api.SecretRename, `{"space":"ops","name":"db-password","new_name":"db-pass","overwrite":true}`},
		{api.SecretMove, `{"space":"ops","name":"db-password","new_space":"data","overwrite":true}`},
		{api.SecretRemove, `{"space":"ops","name":"db-password","all":true}`},
	} {
		if status, e := send(t, h, tc.o, tc.body); status != http.StatusBadRequest || e.Code != "unknown_field" {
			t.Errorf("%s: answered %d %+v, want unknown_field", tc.o.Info().Name, status, e)
		}
	}
}

// secret.set checks the new value as secret.add checks one: an empty value
// and one over 64 KiB are refused, without the value, and leave the Secret
// as it was; exactly 64 KiB is stored.
func TestSecretSetChecksTheValue(t *testing.T) {
	h := harness.Start(t)
	h.Koko("space", "add", "ops").WantSuccess(t)
	h.KokoStdin(marker, "secret", "add", "ops/db-password").WantSuccess(t)
	full := strings.Repeat("m", api.SecretMaxSize)
	over := full[len(marker)-1:] + marker
	for _, tc := range []struct {
		name   string
		value  string
		status int
		code   string
	}{
		{"empty", "", http.StatusBadRequest, "invalid_request"},
		{"64 KiB and one byte", over, http.StatusRequestEntityTooLarge, "value_too_large"},
	} {
		status, e := send(t, h, api.SecretSet, `{"space":"ops","name":"db-password","value_base64":"`+b64(tc.value)+`"}`)
		if status != tc.status || e.Code != tc.code {
			t.Errorf("%s: answered %d %+v, want %d %s", tc.name, status, e, tc.status, tc.code)
		}
		if strings.Contains(e.Message, marker) || strings.Contains(e.Message, b64(marker)) {
			t.Errorf("%s: the refusal holds a value: %q", tc.name, e.Message)
		}
	}
	if len(over) != api.SecretMaxSize+1 {
		t.Fatalf("the value over the limit is %d bytes", len(over))
	}
	if got := h.Koko("secret", "show", "ops/db-password").Stdout; got != marker {
		t.Fatalf("after the refusals secret show printed %d bytes, want the value as it was", len(got))
	}
	if status, e := send(t, h, api.SecretSet, `{"space":"ops","name":"db-password","value_base64":"`+b64(full)+`"}`); status != http.StatusOK {
		t.Errorf("64 KiB: answered %d %+v, want it stored", status, e)
	}
	if got := h.Koko("secret", "show", "ops/db-password").Stdout; got != full {
		t.Errorf("secret show printed %d bytes, want the 64 KiB set", len(got))
	}
}

// sealedValue reads the one Secret's sealed value from the data directory
// of a koko server that is stopped.
func sealedValue(t *testing.T, dataDir string) []byte {
	t.Helper()
	st, err := store.Open(t.Context(), dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	var sealed []byte
	err = st.Read(t.Context(), func(tx store.Tx) error {
		return tx.QueryRowContext(t.Context(), "SELECT sealed FROM secrets").Scan(&sealed)
	})
	if err != nil {
		t.Fatal(err)
	}
	return sealed
}

// A rename and a move leave the sealed value as it is, since it is sealed
// for the Secret's id, which neither changes; secret set seals the value
// afresh, even the same value (P0 spec: Secrets, ADR 0018). Like the binding
// test, it knows the table that holds Secrets.
func TestOnlySecretSetReseals(t *testing.T) {
	dataDir, keyFile := t.TempDir(), harness.NewRootKeyFile(t)
	h := startOn(t, dataDir, keyFile, false)
	h.Koko("space", "add", "ops").WantSuccess(t)
	h.Koko("space", "add", "data").WantSuccess(t)
	h.KokoStdin(marker, "secret", "add", "ops/db-password").WantSuccess(t)
	h.Stop()
	added := sealedValue(t, dataDir)

	h = startOn(t, dataDir, keyFile, true)
	h.Koko("secret", "rename", "ops/db-password", "db-pass").WantSuccess(t)
	h.Koko("secret", "move", "ops/db-pass", "data").WantSuccess(t)
	h.Stop()
	if !bytes.Equal(sealedValue(t, dataDir), added) {
		t.Errorf("a rename and a move changed the sealed value")
	}

	h = startOn(t, dataDir, keyFile, true)
	h.KokoStdin(marker, "secret", "set", "data/db-pass").WantSuccess(t)
	if got := h.Koko("secret", "show", "data/db-pass").Stdout; got != marker {
		t.Errorf("secret show printed %d bytes after secret set, want the value set", len(got))
	}
	h.Stop()
	if bytes.Equal(sealedValue(t, dataDir), added) {
		t.Errorf("secret set left the sealed value as it was, want the value sealed afresh")
	}
}

// addOwner records the Owner, alice, with her Personal Space, so the Local
// caller acts as someone.
var addOwner = api.Op[struct{}, struct{}]{Name: "test.owner", Write: true}

// newSecretAPI is the secret operations beneath HTTP, on a store and a Root
// Key of their own, with the Owner recorded.
func newSecretAPI(t *testing.T) *op.API {
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
	secret.Register(a, rootKey)
	a.Handle(addOwner, func(ctx context.Context, tx store.Tx, _ op.Caller, _ struct{}) (struct{}, error) {
		return struct{}{}, user.AddOwner(ctx, tx, harness.Owner)
	})
	if status, body := callWithKey(t, a, api.NewIdempotencyKey(), addOwner, `{}`); status != http.StatusOK {
		t.Fatalf("record the Owner: %d %s", status, body)
	}
	return a
}

// callWithKey calls o with body as the Local caller, carrying the
// Idempotency-Key key, and returns the status and the response body.
func callWithKey(t *testing.T, a *op.API, key string, o api.Operation, body string) (int, string) {
	t.Helper()
	req := httptest.NewRequestWithContext(op.WithCaller(t.Context(), op.Caller{Local: true}), http.MethodPost,
		api.PathPrefix+o.Info().Name, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(api.IdempotencyKeyHeader, key)
	rec := httptest.NewRecorder()
	a.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

// A secret.add retried with its Idempotency-Key adds one Secret and answers
// the response stored for the key, which holds no value; the same key with
// another value is refused, and stores nothing (P0 spec: Secrets, S2).
func TestARetriedSecretAddAddsOneSecret(t *testing.T) {
	a := newSecretAPI(t)
	add := func(value string) string {
		return `{"space":"alice","name":"db-password","value_base64":"` + b64(value) + `"}`
	}
	key := api.NewIdempotencyKey()
	status, first := callWithKey(t, a, key, api.SecretAdd, add(marker))
	if status != http.StatusOK {
		t.Fatalf("secret.add answered %d %s", status, first)
	}
	status, retried := callWithKey(t, a, key, api.SecretAdd, add(marker))
	if status != http.StatusOK || retried != first {
		t.Errorf("the retry answered %d %s, want the stored response %s", status, retried, first)
	}
	for _, form := range []string{marker, b64(marker), "value"} {
		if strings.Contains(retried, form) {
			t.Errorf("the stored response holds the value: %s", retried)
		}
	}

	status, reused := callWithKey(t, a, key, api.SecretAdd, add(marker+"-another"))
	var e api.ErrorResponse
	if err := json.Unmarshal([]byte(reused), &e); err != nil || status != api.CodeIdempotencyKeyReused.Status ||
		e.Error.Code != api.CodeIdempotencyKeyReused.Name {
		t.Errorf("the same key with another value answered %d %s, want %s", status, reused, api.CodeIdempotencyKeyReused.Name)
	}
	if strings.Contains(reused, marker) || strings.Contains(reused, b64(marker+"-another")) {
		t.Errorf("the refusal holds a value: %s", reused)
	}

	_, listed := callWithKey(t, a, api.NewIdempotencyKey(), api.SecretList, `{}`)
	var l api.SecretListResponse
	if err := json.Unmarshal([]byte(listed), &l); err != nil || len(l.Secrets) != 1 {
		t.Errorf("secret.list answered %s, want one Secret", listed)
	}
	_, shown := callWithKey(t, a, api.NewIdempotencyKey(), api.SecretShow, `{"space":"alice","name":"db-password"}`)
	var s api.SecretShowResponse
	if err := json.Unmarshal([]byte(shown), &s); err != nil || string(s.Value) != marker {
		t.Errorf("secret.show answered %d bytes, want the first value", len(s.Value))
	}
}
