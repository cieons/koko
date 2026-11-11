package host_test

import (
	"encoding/json/v2"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/shared/api"
)

// batchWrite is one host write that takes a batch: the operation, the rest
// of its request beside the Hosts it names, what it did for the refusal's
// message, and done, which tells from host.list --all whether it was done
// to the Host named hostName.
type batchWrite struct {
	op   api.Operation
	rest string
	did  string
	done func(listed map[string]api.Host, hostName string) bool
}

var batchWrites = []batchWrite{
	{api.HostMove, `"space": "ops"`, "moved", func(l map[string]api.Host, n string) bool { return l[n].Space == "ops" }},
	{api.HostSet, `"attributes": {"role": "node"}`, "changed", func(l map[string]api.Host, n string) bool {
		return l[n].Attributes["role"].String() == "node"
	}},
	{api.HostRetire, ``, "retired", func(l map[string]api.Host, n string) bool { return l[n].State == api.HostRetired }},
	{api.HostRemove, ``, "removed", func(l map[string]api.Host, n string) bool { _, ok := l[n]; return !ok }},
}

// body is the request of w naming the Hosts in names, a JSON list, or its
// one Host, a JSON string, in field.
func (w batchWrite) body(field, names string) string {
	if w.rest == "" {
		return fmt.Sprintf(`{%q: %s}`, field, names)
	}
	return fmt.Sprintf(`{%q: %s, %s}`, field, names, w.rest)
}

// startWithHosts starts a koko server with the Space ops and the Hosts
// web1, web2 and web3 in the Owner's Personal Space.
func startWithHosts(t *testing.T) *harness.Harness {
	t.Helper()
	h := harness.Start(t)
	h.Koko("space", "add", "ops").WantSuccess(t)
	for _, n := range []string{"web1", "web2", "web3"} {
		if _, code, body := send(t, h, api.HostAdd, `{"hosts": [{"name": "`+n+`", "address": "203.0.113.10", "account": "deploy"}]}`); code != "" {
			t.Fatalf("host.add %s = %s", n, body)
		}
	}
	return h
}

// listed returns every Host host.list --all answers, by name.
func listed(t *testing.T, h *harness.Harness) map[string]api.Host {
	t.Helper()
	_, b := h.Send(api.HostList, []byte(`{"all": true}`))
	var resp api.HostListResponse
	if err := json.Unmarshal(b, &resp); err != nil {
		t.Fatalf("host.list answered %s: %v", b, err)
	}
	hosts := map[string]api.Host{}
	for _, x := range resp.Hosts {
		hosts[x.Name] = x
	}
	return hosts
}

// refusal reads the error object of a refused call.
func refusal(t *testing.T, body string) api.Error {
	t.Helper()
	var e api.ErrorResponse
	if err := json.Unmarshal([]byte(body), &e); err != nil {
		t.Fatalf("%s is not an error object: %v", body, err)
	}
	return e.Error
}

// A batch of each host write is applied whole and answered in the order
// asked; one Host at fault changes none, and the refusal names its index
// and code, sharing the code of every item or invalid_batch when they
// differ (ADR 0030, ADR 0050).
func TestAHostBatchIsAppliedAllOrNone(t *testing.T) {
	for _, w := range batchWrites {
		t.Run(w.op.Info().Name, func(t *testing.T) {
			h := startWithHosts(t)
			before := listed(t, h)

			_, code, body := send(t, h, w.op, w.body("hosts", `["web1", "nowhere", "web2"]`))
			e := refusal(t, body)
			wantItems := []api.ItemError{{Index: 1, Code: "unknown_host", Message: `no Host is named "nowhere"`}}
			if code != "unknown_host" || !slices.Equal(e.Items, wantItems) ||
				!strings.HasPrefix(e.Message, "none of the 3 Hosts was "+w.did+", for the 1 at fault:\n[1] no Host is named") {
				t.Errorf("a batch with nowhere at [1] = %s, want unknown_host with that one item", body)
			}
			_, code, body = send(t, h, w.op, w.body("hosts", `["nowhere", "Web_2"]`))
			if e := refusal(t, body); code != "invalid_batch" || len(e.Items) != 2 || e.Items[0].Code != "unknown_host" || e.Items[1].Code != "invalid_name" {
				t.Errorf("a batch at fault for two reasons = %s, want invalid_batch with both items", body)
			}
			if after := listed(t, h); !mapsEqualJSON(t, before, after) {
				t.Errorf("after the refused batches the Hosts are %v, want them as they were, %v", after, before)
			}

			status, _, body := send(t, h, w.op, w.body("hosts", `["web2", "web1"]`))
			if status != http.StatusOK {
				t.Fatalf("a batch of web2 and web1 = %d %s, want it applied", status, body)
			}
			var resp api.HostAddResponse
			if err := json.Unmarshal([]byte(body), &resp); err != nil || len(resp.Hosts) != 2 || resp.Hosts[0].Name != "web2" || resp.Hosts[1].Name != "web1" {
				t.Errorf("a batch of web2 and web1 answered %s (%v), want both Hosts in that order", body, err)
			}
			if strings.Contains(body, `"host":`) {
				t.Errorf("a batch answered %s, want hosts alone", body)
			}
			after := listed(t, h)
			for _, n := range []string{"web1", "web2"} {
				if !w.done(after, n) {
					t.Errorf("after the batch %s is %+v, want it %s", n, after[n], w.did)
				}
			}
			if w.done(after, "web3") {
				t.Errorf("web3, outside the batch, was %s too", w.did)
			}
		})
	}
}

// mapsEqualJSON tells whether two lists of Hosts encode alike.
func mapsEqualJSON(t *testing.T, a, b map[string]api.Host) bool {
	t.Helper()
	x, err := json.Marshal(a, json.Deterministic(true))
	if err != nil {
		t.Fatal(err)
	}
	y, err := json.Marshal(b, json.Deterministic(true))
	if err != nil {
		t.Fatal(err)
	}
	return string(x) == string(y)
}

// A request names its Hosts one way: host or a non-empty hosts, not both.
// A batch of one at fault is refused with its Host's own error, as the
// one-Host form is, and a Host named twice is at fault at its later index.
func TestAHostBatchNamesItsHostsOneWay(t *testing.T) {
	for _, w := range batchWrites {
		t.Run(w.op.Info().Name, func(t *testing.T) {
			h := startWithHosts(t)
			for _, tc := range []struct{ body, code string }{
				{w.body("hosts", `[]`), "invalid_request"},
				{strings.Replace(w.body("hosts", `["web2"]`), "{", `{"host": "web1", `, 1), "invalid_request"},
				{w.body("hosts", `["nowhere"]`), "unknown_host"},
				{w.body("host", `"nowhere"`), "unknown_host"},
			} {
				if _, code, body := send(t, h, w.op, tc.body); code != tc.code || strings.Contains(body, `"items"`) {
					t.Errorf("%s = %s, want %s and no items", tc.body, body, tc.code)
				}
			}
			id := listed(t, h)["web1"].ID
			_, code, body := send(t, h, w.op, w.body("hosts", `["web1", "web2", "`+id+`"]`))
			want := []api.ItemError{{Index: 2, Code: "invalid_request", Message: fmt.Sprintf("the Host [0] of this batch is web1 (%s) too", id)}}
			if e := refusal(t, body); code != "invalid_request" || !slices.Equal(e.Items, want) {
				t.Errorf("a batch naming web1 by name and by id = %s, want its [2] at fault", body)
			}
		})
	}
}

// A Member's batch is checked Host by Host: a Host in a Space they cannot
// act in is at fault as the one-Host form refuses it, as one that does not
// exist, and nothing is applied.
func TestAHostBatchChecksAccessHostByHost(t *testing.T) {
	for _, w := range batchWrites {
		t.Run(w.op.Info().Name, func(t *testing.T) {
			h := startWithHosts(t)
			h.Koko("user", "invite", "bob").WantSuccess(t)
			h.Koko("space", "member", "add", "ops", "bob").WantSuccess(t)
			bob := h.Join("bob")
			_, b := bob.Send(api.HostAdd, []byte(`{"hosts": [{"name": "db1", "address": "203.0.113.20", "account": "deploy", "space": "ops"}]}`))
			if !strings.Contains(string(b), `"db1"`) {
				t.Fatalf("bob's host.add db1 = %s", b)
			}
			before := listed(t, h)
			_, alone := bob.Send(w.op, []byte(w.body("host", `"web1"`)))
			_, b = bob.Send(w.op, []byte(w.body("hosts", `["db1", "web1"]`)))
			single, batch := refusal(t, string(alone)), refusal(t, string(b))
			want := []api.ItemError{{Index: 1, Code: single.Code, Message: single.Message}}
			if single.Code != "unknown_host" || batch.Code != "unknown_host" || !slices.Equal(batch.Items, want) {
				t.Errorf("bob's batch of db1 and web1 = %s, want web1 at fault as alone: %s", b, alone)
			}
			if after := listed(t, h); !mapsEqualJSON(t, before, after) {
				t.Errorf("after bob's refused batch the Hosts are %v, want them as they were", after)
			}
		})
	}
}

// host.set in a batch changes each Host alike. A Host that holds the values
// already is not at fault, even when every one does; a request naming
// nothing to change is refused once, without items, as for one Host.
func TestAHostSetBatchRefusesOnlyARequestThatNamesNothing(t *testing.T) {
	h := startWithHosts(t)
	if _, code, body := send(t, h, api.HostSet, `{"host": "web1", "attributes": {"role": "node"}}`); code != "" {
		t.Fatalf("host.set web1 role=node = %s", body)
	}
	for range 2 {
		if _, code, body := send(t, h, api.HostSet, `{"hosts": ["web1", "web2"], "attributes": {"role": "node"}}`); code != "" {
			t.Errorf("a batch setting role=node, which web1 holds already = %s, want it applied", body)
		}
	}
	_, code, body := send(t, h, api.HostSet, `{"hosts": ["web1", "web2"]}`)
	if e := refusal(t, body); code != "invalid_request" || e.Items != nil || e.Message != "name at least one field, Attribute or Note of the 2 Hosts to change" {
		t.Errorf("a batch naming nothing to change = %s, want invalid_request without items", body)
	}
	if _, code, body := send(t, h, api.HostSet, `{"hosts": ["web2", "web3"], "unset": ["role"]}`); code != "" {
		t.Errorf("a batch clearing role, which web3 lacks = %s, want it applied", body)
	}
	if l := listed(t, h); l["web1"].Attributes["role"].String() != "node" || len(l["web2"].Attributes) != 0 {
		t.Errorf("after the batches web1 has %v and web2 %v, want role=node and nothing", l["web1"].Attributes, l["web2"].Attributes)
	}
}
