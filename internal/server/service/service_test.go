package service_test

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

// start starts a koko server with the Host web1, as the Local caller.
func start(t *testing.T) *harness.Harness {
	t.Helper()
	h := harness.Start(t)
	if _, code, body := send(t, h, api.HostAdd, `{"hosts": [{"name": "web1", "address": "203.0.113.10", "account": "deploy"}]}`); code != "" {
		t.Fatalf("host.add web1 = %s", body)
	}
	return h
}

// The koko server checks every rule on every write, whichever client sent
// it, so a client that skips the CLI's checks is refused all the same.
func TestTheKokoServerChecksEveryRuleOnServiceAdd(t *testing.T) {
	h := start(t)
	for _, tc := range []struct{ body, code string }{
		{`{"host": "web2", "name": "nginx"}`, "unknown_host"},
		{`{"host": "Web1", "name": "nginx"}`, "invalid_name"},
		{`{"host": "web1", "name": "Nginx"}`, "invalid_name"},
		{`{"host": "web1", "name": ""}`, "invalid_name"},
		{`{"host": "web1", "name": "nginx", "ports": ["0"]}`, "invalid_value"},
		{`{"host": "web1", "name": "nginx", "ports": ["443/TCP"]}`, "invalid_value"},
		{`{"host": "web1", "name": "nginx", "ports": ["443", "443/tcp"]}`, "invalid_value"},
		{`{"host": "web1", "name": "nginx", "ports": [""]}`, "invalid_value"},
		{`{"host": "web1", "name": "nginx", "ports": [443]}`, "invalid_request"},
		{`{"host": "web1", "name": "nginx", "attributes": {"host": "web2"}}`, "invalid_value"},
		{`{"host": "web1", "name": "nginx", "attributes": {"price": "10"}}`, "invalid_value"},
		{`{"host": "web1", "name": "nginx", "attributes": {"kind": ""}}`, "invalid_value"},
		{`{"host": "web1", "name": "nginx", "note": ""}`, "invalid_value"},
		{`{"host": "web1", "name": "nginx", "space": "ops"}`, "unknown_field"},
	} {
		if status, code, body := send(t, h, api.ServiceAdd, tc.body); code != tc.code {
			t.Errorf("service.add %s = %d %s, want %s", tc.body, status, body, tc.code)
		}
	}
	if _, _, body := send(t, h, api.ServiceList, `{}`); body != `{"services":[]}` {
		t.Errorf("after the refusals service.list = %s, want no Service", body)
	}
	// A bare port is tcp, and the set comes back by port and protocol;
	// "ports": [] is no ports at all.
	if _, code, body := send(t, h, api.ServiceAdd, `{"host": "web1", "name": "nginx", "ports": ["51820/udp", "443", "80/tcp"]}`); code != "" ||
		!strings.Contains(body, `"ports":["80/tcp","443/tcp","51820/udp"]`) {
		t.Errorf("service.add with three ports = %s", body)
	}
	if _, code, body := send(t, h, api.ServiceAdd, `{"host": "web1", "name": "cron", "ports": []}`); code != "" ||
		!strings.Contains(body, `"ports":[]`) {
		t.Errorf("service.add with no ports = %s", body)
	}
}

// service.set takes neither name nor host, needs something to change, and
// clears the ports only through unset.
func TestTheKokoServerChecksEveryRuleOnServiceSet(t *testing.T) {
	h := start(t)
	if _, code, body := send(t, h, api.ServiceAdd, `{"host": "web1", "name": "nginx", "ports": ["443"]}`); code != "" {
		t.Fatalf("service.add web1/nginx = %s", body)
	}
	for _, tc := range []struct{ body, code string }{
		{`{"host": "web1", "name": "nginx"}`, "invalid_request"},
		{`{"host": "web1", "name": "redis", "ports": ["80"]}`, "unknown_service"},
		{`{"host": "web1", "name": "nginx", "new_name": "web"}`, "unknown_field"},
		{`{"host": "web1", "name": "nginx", "new_host": "web2"}`, "unknown_field"},
		{`{"host": "web1", "name": "nginx", "ports": []}`, "invalid_value"},
		{`{"host": "web1", "name": "nginx", "ports": ["65536"]}`, "invalid_value"},
		{`{"host": "web1", "name": "nginx", "note": ""}`, "invalid_value"},
		{`{"host": "web1", "name": "nginx", "unset": ["host"]}`, "invalid_value"},
		{`{"host": "web1", "name": "nginx", "unset": ["name"]}`, "invalid_value"},
		{`{"host": "web1", "name": "nginx", "unset": ["Kind"]}`, "invalid_value"},
		{`{"host": "web1", "name": "nginx", "attributes": {"kind": "proxy"}, "unset": ["kind"]}`, "invalid_request"},
		{`{"host": "web1", "name": "nginx", "ports": ["80"], "unset": ["ports"]}`, "invalid_request"},
		{`{"host": "web1", "name": "nginx", "note": "x", "unset": ["note"]}`, "invalid_request"},
	} {
		if status, code, body := send(t, h, api.ServiceSet, tc.body); code != tc.code {
			t.Errorf("service.set %s = %d %s, want %s", tc.body, status, body, tc.code)
		}
	}
	if _, _, body := send(t, h, api.ServiceShow, `{"host": "web1", "name": "nginx"}`); !strings.Contains(body, `"ports":["443/tcp"]`) {
		t.Errorf("after the refusals web1/nginx = %s, want it unchanged", body)
	}
}

// service.rename checks the new name and that no other Service on the
// Host holds it.
func TestTheKokoServerChecksTheNewNameOnServiceRename(t *testing.T) {
	h := start(t)
	for _, name := range []string{"nginx", "redis"} {
		if _, code, body := send(t, h, api.ServiceAdd, `{"host": "web1", "name": "`+name+`"}`); code != "" {
			t.Fatalf("service.add web1/%s = %s", name, body)
		}
	}
	for _, tc := range []struct{ body, code string }{
		{`{"host": "web1", "name": "nginx", "new_name": "redis"}`, "name_taken"},
		{`{"host": "web1", "name": "nginx", "new_name": "nginx"}`, "name_taken"},
		{`{"host": "web1", "name": "nginx", "new_name": "web2/nginx"}`, "invalid_name"},
		{`{"host": "web1", "name": "nginx", "new_name": ""}`, "invalid_name"},
		{`{"host": "web1", "name": "web", "new_name": "www"}`, "unknown_service"},
	} {
		if status, code, body := send(t, h, api.ServiceRename, tc.body); code != tc.code {
			t.Errorf("service.rename %s = %d %s, want %s", tc.body, status, body, tc.code)
		}
	}
}

// Every answer about one Service carries its Note; service.list leaves the
// Notes out unless with_note asks for them.
func TestTheNoteIsLeftOutOnlyOfAServiceListThatDoesNotAskForIt(t *testing.T) {
	h := start(t)
	for _, tc := range []struct {
		o        api.Operation
		body     string
		withNote bool
	}{
		{api.ServiceAdd, `{"host": "web1", "name": "nginx", "note": "A Note."}`, true},
		{api.ServiceList, `{}`, false},
		{api.ServiceList, `{"with_note": true}`, true},
		{api.ServiceShow, `{"host": "web1", "name": "nginx"}`, true},
		{api.ServiceSet, `{"host": "web1", "name": "nginx", "ports": ["443"]}`, true},
		{api.ServiceRename, `{"host": "web1", "name": "nginx", "new_name": "web"}`, true},
		{api.ServiceRemove, `{"host": "web1", "name": "web"}`, true},
	} {
		_, _, body := send(t, h, tc.o, tc.body)
		if got := strings.Contains(body, `"note":"A Note."`); got != tc.withNote {
			t.Errorf("%s %s = %s, want the Note in it: %t", tc.o.Info().Name, tc.body, body, tc.withNote)
		}
	}
}

// host.show answers the Host's Services, by name in natural order, with
// their ports and Attributes and without their Notes, as service.list does;
// a Host with none answers an empty array.
func TestHostShowAnswersTheHostsServices(t *testing.T) {
	h := start(t)
	if _, _, body := send(t, h, api.HostShow, `{"host": "web1"}`); !strings.Contains(body, `"services":[]`) {
		t.Errorf("host.show of a Host with no Service = %s, want an empty array", body)
	}
	for _, body := range []string{
		`{"host": "web1", "name": "web10", "ports": ["443"], "attributes": {"kind": "proxy"}, "note": "A Note."}`,
		`{"host": "web1", "name": "web2"}`,
	} {
		if _, code, answer := send(t, h, api.ServiceAdd, body); code != "" {
			t.Fatalf("service.add %s = %s", body, answer)
		}
	}
	_, code, body := send(t, h, api.HostShow, `{"host": "web1"}`)
	if code != "" {
		t.Fatalf("host.show web1 = %s", body)
	}
	var resp api.HostShowResponse
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, s := range resp.Services {
		names = append(names, s.Name)
	}
	if strings.Join(names, " ") != "web2 web10" || resp.Services[1].Host != "web1" || strings.Join(resp.Services[1].Ports, " ") != "443/tcp" ||
		resp.Services[1].Attributes["kind"].String() != "proxy" || strings.Contains(body, "A Note.") {
		t.Errorf("host.show web1 = %s, want web2 and web10, with web10's port and Attribute and without its Note", body)
	}
}

// service.list takes the fields of ServiceListFields and the Services' own
// Attribute keys, and refuses every other field.
func TestServiceListRefusesTheFieldsItDoesNotTake(t *testing.T) {
	h := start(t)
	for _, tc := range []struct{ body, code string }{
		{`{"filters": [{"key": "space", "op": "eq", "value": "alice"}]}`, "invalid_request"},
		{`{"filters": [{"key": "ports", "op": "has"}]}`, "invalid_request"},
		{`{"filters": [{"key": "note", "op": "has"}]}`, "invalid_request"},
		{`{"sort": [{"key": "created_at"}]}`, "invalid_request"},
		{`{"filters": [{"key": "price", "op": "gt", "value": "10"}]}`, "invalid_value"},
		{`{"filters": [{"key": "host", "op": "eq", "value": "web1"}], "sort": [{"key": "name", "desc": true}]}`, ""},
	} {
		if status, code, body := send(t, h, api.ServiceList, tc.body); code != tc.code {
			t.Errorf("service.list %s = %d %s, want %q", tc.body, status, body, tc.code)
		}
	}
}
