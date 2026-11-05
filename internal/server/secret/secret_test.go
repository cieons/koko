package secret_test

import (
	"encoding/base64"
	"encoding/json/v2"
	"net/http"
	"strings"
	"testing"

	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/server/store"
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
// (P0 spec: Secrets, story 41). It is the one test that knows the table
// holding Secrets.
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
	b64 := func(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }
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
	} {
		if status, e := send(t, h, tc.o, tc.body); status != http.StatusBadRequest || e.Code != "unknown_field" {
			t.Errorf("%s: answered %d %+v, want unknown_field", tc.o.Info().Name, status, e)
		}
	}
}
