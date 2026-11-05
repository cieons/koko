package host_test

import (
	"encoding/json/v2"
	"net/http"
	"strings"
	"testing"

	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/shared/api"
)

// send calls o with the raw JSON body as the Local caller and returns the
// status and the error code, "" for success, with the body.
func send(t *testing.T, h *harness.Harness, o api.Operation, body string) (int, string, string) {
	t.Helper()
	status, b := h.Send(o, []byte(body))
	var e api.ErrorResponse
	if status != http.StatusOK {
		if err := json.Unmarshal(b, &e); err != nil {
			t.Fatalf("%s answered %d %s, which is not an error object: %v", o.Info().Name, status, b, err)
		}
	}
	return status, e.Error.Code, string(b)
}

const web1 = `{"name": "web1", "address": "203.0.113.10", "account": "deploy"`

// The koko server checks every rule on every write, whichever client sent
// it, so a client that skips the CLI's checks is refused all the same.
func TestTheKokoServerChecksEveryRuleOnHostAdd(t *testing.T) {
	h := harness.Start(t)
	for _, tc := range []struct{ body, code string }{
		{`{"hosts": []}`, "invalid_request"},
		{`{"hosts": [{"name": "Web1", "address": "203.0.113.10", "account": "deploy"}]}`, "invalid_name"},
		{`{"hosts": [{"name": "web1", "account": "deploy"}]}`, "invalid_value"},
		{`{"hosts": [{"name": "web1", "address": "10.0.0.256", "account": "deploy"}]}`, "invalid_value"},
		{`{"hosts": [{"name": "web1", "address": "203.0.113.10"}]}`, "invalid_value"},
		{`{"hosts": [` + web1 + `, "port": 0}]}`, "invalid_value"},
		{`{"hosts": [` + web1 + `, "port": 65536}]}`, "invalid_value"},
		{`{"hosts": [` + web1 + `, "attributes": {"relay": "web2"}}]}`, "invalid_value"},
		{`{"hosts": [` + web1 + `, "attributes": {"Region": "eu"}}]}`, "invalid_value"},
		{`{"hosts": [` + web1 + `, "attributes": {"region": ""}}]}`, "invalid_value"},
		{`{"hosts": [` + web1 + `, "attributes": {"region": "eu\nwest"}}]}`, "invalid_value"},
		{`{"hosts": [` + web1 + `, "note": ""}]}`, "invalid_value"},
		{`{"hosts": [` + web1 + `, "space": ""}]}`, "invalid_name"},
		{`{"hosts": [` + web1 + `, "state": "managed"}]}`, "unknown_field"},
		{`{"hosts": [` + web1 + `, "host_key": "SHA256:x"}]}`, "unknown_field"},
	} {
		if status, code, body := send(t, h, api.HostAdd, tc.body); code != tc.code {
			t.Errorf("host.add %s = %d %s, want %s", tc.body, status, body, tc.code)
		}
	}
	if _, _, body := send(t, h, api.HostList, `{}`); body != `{"hosts":[]}` {
		t.Errorf("after the refusals host.list = %s, want no Host", body)
	}
}

// A batch is all or none: one Host at fault records none of them, and a
// batch may not take one name twice.
func TestHostAddIsAllOrNone(t *testing.T) {
	h := harness.Start(t)
	for _, body := range []string{
		`{"hosts": [` + web1 + `}, {"name": "web2", "address": "203.0.113.11", "account": "-x"}]}`,
		`{"hosts": [` + web1 + `}, ` + web1 + `}]}`,
	} {
		if _, code, answer := send(t, h, api.HostAdd, body); code == "" {
			t.Errorf("host.add %s = %s, want it refused", body, answer)
		}
	}
	if _, _, body := send(t, h, api.HostList, `{}`); body != `{"hosts":[]}` {
		t.Errorf("after the refusals host.list = %s, want no Host", body)
	}
	if _, code, body := send(t, h, api.HostAdd, `{"hosts": [`+web1+`}, {"name": "web2", "address": "203.0.113.11", "account": "deploy"}]}`); code != "" ||
		!strings.Contains(body, `"web1"`) || !strings.Contains(body, `"web2"`) {
		t.Errorf("host.add of web1 and web2 = %s, want both", body)
	}
}

// host.set takes neither name, space nor state, clears no field a Host must
// have, and needs something to change.
func TestTheKokoServerChecksEveryRuleOnHostSet(t *testing.T) {
	h := harness.Start(t)
	if _, code, body := send(t, h, api.HostAdd, `{"hosts": [`+web1+`}]}`); code != "" {
		t.Fatalf("host.add web1 = %s", body)
	}
	for _, tc := range []struct{ body, code string }{
		{`{"host": "web1"}`, "invalid_request"},
		{`{"host": "web1", "name": "web2"}`, "unknown_field"},
		{`{"host": "web1", "space": "ops"}`, "unknown_field"},
		{`{"host": "web1", "state": "retired"}`, "unknown_field"},
		{`{"host": "web1", "address": ""}`, "invalid_value"},
		{`{"host": "web1", "port": 0}`, "invalid_value"},
		{`{"host": "web1", "account": "root user"}`, "invalid_value"},
		{`{"host": "web1", "note": ""}`, "invalid_value"},
		{`{"host": "web1", "unset": ["address"]}`, "invalid_value"},
		{`{"host": "web1", "unset": ["name"]}`, "invalid_value"},
		{`{"host": "web1", "unset": ["Region"]}`, "invalid_value"},
		{`{"host": "web1", "attributes": {"region": "eu"}, "unset": ["region"]}`, "invalid_request"},
		{`{"host": "web1", "note": "x", "unset": ["note"]}`, "invalid_request"},
		{`{"host": "web_1", "port": 22}`, "invalid_name"},
	} {
		if status, code, body := send(t, h, api.HostSet, tc.body); code != tc.code {
			t.Errorf("host.set %s = %d %s, want %s", tc.body, status, body, tc.code)
		}
	}
}

// Every answer about one Host carries its Note; host.list leaves the Notes
// out unless with_note asks for them.
func TestTheNoteIsLeftOutOnlyOfAListThatDoesNotAskForIt(t *testing.T) {
	h := harness.Start(t)
	for _, tc := range []struct {
		o        api.Operation
		body     string
		withNote bool
	}{
		{api.HostAdd, `{"hosts": [` + web1 + `, "note": "A Note."}]}`, true},
		{api.HostList, `{}`, false},
		{api.HostList, `{"with_note": true}`, true},
		{api.HostShow, `{"host": "web1"}`, true},
		{api.HostSet, `{"host": "web1", "port": 2222}`, true},
		{api.HostRename, `{"host": "web1", "new_name": "web2"}`, true},
		{api.HostMove, `{"host": "web2", "space": "alice"}`, true},
		{api.HostRemove, `{"host": "web2"}`, true},
	} {
		_, _, body := send(t, h, tc.o, tc.body)
		if got := strings.Contains(body, `"note":"A Note."`); got != tc.withNote {
			t.Errorf("%s %s = %s, want the Note in it: %t", tc.o.Info().Name, tc.body, body, tc.withNote)
		}
	}
}
