package secret_test

import (
	"encoding/json/v2"
	"net/http"
	"slices"
	"testing"

	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/shared/api"
)

// startWithSecrets starts a koko server with the Spaces ops, dev and web,
// the Secrets ops/a, ops/b and dev/a, and the Member bob of ops and web
// alone.
func startWithSecrets(t *testing.T) *harness.Harness {
	t.Helper()
	h := harness.Start(t)
	for _, sp := range []string{"ops", "dev", "web"} {
		h.Koko("space", "add", sp).WantSuccess(t)
	}
	for _, s := range []string{"ops/a", "ops/b", "dev/a"} {
		h.KokoStdin("value", "secret", "add", s).WantSuccess(t)
	}
	h.Koko("user", "invite", "bob").WantSuccess(t)
	h.Koko("space", "member", "add", "ops", "bob").WantSuccess(t)
	h.Koko("space", "member", "add", "web", "bob").WantSuccess(t)
	return h
}

// secrets lists every Secret as space/name.
func secrets(t *testing.T, h *harness.Harness) []string {
	t.Helper()
	_, b := h.Send(api.SecretList, []byte(`{}`))
	var resp api.SecretListResponse
	if err := json.Unmarshal(b, &resp); err != nil {
		t.Fatalf("secret.list answered %s: %v", b, err)
	}
	var names []string
	for _, s := range resp.Secrets {
		names = append(names, s.Space+"/"+s.Name)
	}
	return names
}

// answer calls o with body through send, as h.Send and a Device's Send
// do, and reads its answer: the Secrets of a batch, or its error.
func answer(t *testing.T, send func(api.Operation, []byte) (int, []byte), o api.Operation, body string) ([]api.Secret, api.Error) {
	t.Helper()
	status, b := send(o, []byte(body))
	if status != http.StatusOK {
		var e api.ErrorResponse
		if err := json.Unmarshal(b, &e); err != nil {
			t.Fatalf("%d %s is not an error object: %v", status, b, err)
		}
		return nil, e.Error
	}
	var resp api.SecretMoveResponse
	if err := json.Unmarshal(b, &resp); err != nil {
		t.Fatalf("%s is not an answer of Secrets: %v", b, err)
	}
	return resp.Secrets, api.Error{}
}

// A batch of secret.move or secret.remove is applied whole and answered in
// the order asked; one Secret at fault changes none and the refusal names
// its index and code; a request names its Secrets one way only (ADR 0050).
func TestASecretBatchIsAppliedAllOrNone(t *testing.T) {
	for _, tc := range []struct {
		op    api.Operation
		rest  string
		after []string
	}{
		{api.SecretMove, `, "new_space": "web"`, []string{"dev/a", "web/a", "web/b"}},
		{api.SecretRemove, ``, []string{"dev/a"}},
	} {
		t.Run(tc.op.Info().Name, func(t *testing.T) {
			h := startWithSecrets(t)
			before := secrets(t, h)
			for _, bad := range []struct{ body, code string }{
				{`{"secrets": [{"space": "ops", "name": "a"}, {"space": "ops", "name": "nowhere"}]` + tc.rest + `}`, "unknown_secret"},
				{`{"secrets": []` + tc.rest + `}`, "invalid_request"},
				{`{"space": "ops", "name": "a", "secrets": [{"space": "ops", "name": "b"}]` + tc.rest + `}`, "invalid_request"},
				{`{"secrets": [{"space": "ops", "name": "a"}, {"space": "ops", "name": "a"}]` + tc.rest + `}`, "invalid_request"},
			} {
				if _, e := answer(t, h.Send, tc.op, bad.body); e.Code != bad.code {
					t.Errorf("%s = %+v, want %s", bad.body, e, bad.code)
				}
			}
			_, e := answer(t, h.Send, tc.op, `{"secrets": [{"space": "ops", "name": "a"}, {"space": "ops", "name": "nowhere"}]`+tc.rest+`}`)
			if want := []api.ItemError{{Index: 1, Code: "unknown_secret", Message: "no Secret is named ops/nowhere"}}; !slices.Equal(e.Items, want) {
				t.Errorf("a batch with ops/nowhere at [1] has items %+v, want %+v", e.Items, want)
			}
			if got := secrets(t, h); !slices.Equal(got, before) {
				t.Errorf("after the refused batches the Secrets are %v, want %v", got, before)
			}
			done, e := answer(t, h.Send, tc.op, `{"secrets": [{"space": "ops", "name": "b"}, {"space": "ops", "name": "a"}]`+tc.rest+`}`)
			if e.Code != "" || len(done) != 2 || done[0].Name != "b" || done[1].Name != "a" {
				t.Errorf("the batch of ops/b and ops/a answered %+v %+v, want both in that order", done, e)
			}
			if got := secrets(t, h); !slices.Equal(got, tc.after) {
				t.Errorf("after the batch the Secrets are %v, want %v", got, tc.after)
			}
		})
	}
}

// Two Secrets of a move under one name that would land in the same Space
// are each refused with name_taken, as a name the Space holds is; one
// already there is not at fault.
func TestASecretMoveBatchRefusesTwoSecretsOfOneName(t *testing.T) {
	h := startWithSecrets(t)
	before := secrets(t, h)
	_, e := answer(t, h.Send, api.SecretMove, `{"secrets": [{"space": "ops", "name": "a"}, {"space": "ops", "name": "b"}, {"space": "dev", "name": "a"}], "new_space": "web"}`)
	if e.Code != "name_taken" || len(e.Items) != 2 || e.Items[0].Index != 0 || e.Items[1].Index != 2 {
		t.Errorf("moving ops/a and dev/a to web together = %+v, want name_taken for [0] and [2]", e)
	}
	if got := secrets(t, h); !slices.Equal(got, before) {
		t.Errorf("after the refused batch the Secrets are %v, want %v", got, before)
	}
	_, e = answer(t, h.Send, api.SecretMove, `{"secrets": [{"space": "ops", "name": "a"}, {"space": "dev", "name": "a"}], "new_space": "dev"}`)
	if want := []api.ItemError{{Index: 0, Code: "name_taken", Message: "the Secret dev/a already exists"}}; !slices.Equal(e.Items, want) {
		t.Errorf("moving ops/a to dev beside dev/a, which is there already = %+v, want ops/a at fault alone", e)
	}
}

// A Member's batch is checked Secret by Secret: one in a Space they cannot
// act in is at fault as the one-Secret form refuses it, and nothing is
// applied.
func TestASecretBatchChecksAccessSecretBySecret(t *testing.T) {
	h := startWithSecrets(t)
	bob := h.Join("bob")
	before := secrets(t, h)
	for _, tc := range []struct {
		op         api.Operation
		alone, two string
	}{
		{api.SecretMove, `{"space": "dev", "name": "a", "new_space": "web"}`, `{"secrets": [{"space": "ops", "name": "a"}, {"space": "dev", "name": "a"}], "new_space": "web"}`},
		{api.SecretRemove, `{"space": "dev", "name": "a"}`, `{"secrets": [{"space": "ops", "name": "a"}, {"space": "dev", "name": "a"}]}`},
	} {
		_, alone := answer(t, bob.Send, tc.op, tc.alone)
		_, batch := answer(t, bob.Send, tc.op, tc.two)
		want := []api.ItemError{{Index: 1, Code: alone.Code, Message: alone.Message}}
		if alone.Code != "unknown_space" || batch.Code != "unknown_space" || !slices.Equal(batch.Items, want) {
			t.Errorf("bob's %s of ops/a and dev/a = %+v, want dev/a at fault as alone: %+v", tc.op.Info().Name, batch, alone)
		}
	}
	if got := secrets(t, h); !slices.Equal(got, before) {
		t.Errorf("after bob's refused batches the Secrets are %v, want %v", got, before)
	}
}
