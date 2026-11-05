package host_test

import (
	"encoding/json/v2"
	"slices"
	"testing"

	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/shared/api"
)

// listNames calls host.list with the raw JSON request body and returns the
// names it answers, in its order.
func listNames(t *testing.T, h *harness.Harness, body string) []string {
	t.Helper()
	_, code, answer := send(t, h, api.HostList, body)
	if code != "" {
		t.Fatalf("host.list %s = %s", body, answer)
	}
	var resp struct {
		Hosts []struct {
			Name string `json:"name"`
		} `json:"hosts"`
	}
	if err := json.Unmarshal([]byte(answer), &resp); err != nil {
		t.Fatalf("host.list %s = %s: %v", body, answer, err)
	}
	names := []string{}
	for _, h := range resp.Hosts {
		names = append(names, h.Name)
	}
	return names
}

// Filters and sort keys compare by the key's type, fields and Attributes
// alike: numbers by value, dates by the calendar, enums in their declared
// order and text in natural order, case-sensitive. A Host that lacks the key
// matches no = and no ordering, matches != and lacks, and sorts last in
// either direction; Hosts that tie stay by name.
func TestHostListComparesByType(t *testing.T) {
	h := harness.Start(t)
	add := `{"hosts": [
		{"name": "alpha", "address": "203.0.113.1", "account": "deploy", "attributes": {"price": 10, "expires_at": "2026-12-01",
			"billing_cycle": "biennial", "auto_renew": "no", "cores": "16", "region": "eu"}},
		{"name": "bravo", "address": "203.0.113.2", "port": 2222, "account": "deploy", "attributes": {"price": 9.5,
			"expires_at": "2026-11-15", "billing_cycle": "monthly", "auto_renew": "yes", "cores": "8", "region": "EU"}},
		{"name": "charlie", "address": "203.0.113.3", "account": "deploy", "attributes": {"price": 1.10, "expires_at": "2027-01-31",
			"billing_cycle": "yearly", "auto_renew": "no", "cores": "100"}},
		{"name": "delta", "address": "203.0.113.4", "account": "deploy"}
	]}`
	_, code, answer := send(t, h, api.HostAdd, add)
	if code != "" {
		t.Fatalf("host.add = %s", answer)
	}
	var added api.HostAddResponse
	if err := json.Unmarshal([]byte(answer), &added); err != nil {
		t.Fatal(err)
	}
	alphaID := added.Hosts[0].ID

	for _, tc := range []struct {
		filters, sort string
		want          []string
	}{
		// Exact decimals, by value: 10 is more than 9.5, and 1.10 is 1.1.
		{filters: `{"key": "price", "op": "eq", "value": 1.1}`, want: []string{"charlie"}},
		{filters: `{"key": "price", "op": "eq", "value": 1.10}`, want: []string{"charlie"}},
		{filters: `{"key": "price", "op": "gt", "value": 9.5}`, want: []string{"alpha"}},
		{filters: `{"key": "price", "op": "ge", "value": 9.5}`, want: []string{"alpha", "bravo"}},
		{filters: `{"key": "price", "op": "lt", "value": 10}`, want: []string{"bravo", "charlie"}},
		{filters: `{"key": "price", "op": "le", "value": 1.1}`, want: []string{"charlie"}},
		{filters: `{"key": "price", "op": "ne", "value": 10}`, want: []string{"bravo", "charlie", "delta"}},
		{filters: `{"key": "price", "op": "has"}`, want: []string{"alpha", "bravo", "charlie"}},
		{filters: `{"key": "price", "op": "lacks"}`, want: []string{"delta"}},
		{sort: `{"key": "price"}`, want: []string{"charlie", "bravo", "alpha", "delta"}},
		{sort: `{"key": "price", "desc": true}`, want: []string{"alpha", "bravo", "charlie", "delta"}},
		// Dates, by the calendar.
		{filters: `{"key": "expires_at", "op": "le", "value": "2026-12-01"}`, want: []string{"alpha", "bravo"}},
		{filters: `{"key": "expires_at", "op": "gt", "value": "2026-12-01"}`, want: []string{"charlie"}},
		{sort: `{"key": "expires_at"}`, want: []string{"bravo", "alpha", "charlie", "delta"}},
		{sort: `{"key": "expires_at", "desc": true}`, want: []string{"charlie", "alpha", "bravo", "delta"}},
		// Enums, in their declared order: monthly, ..., yearly, biennial.
		{filters: `{"key": "billing_cycle", "op": "gt", "value": "yearly"}`, want: []string{"alpha"}},
		{filters: `{"key": "billing_cycle", "op": "le", "value": "yearly"}`, want: []string{"bravo", "charlie"}},
		{sort: `{"key": "billing_cycle"}`, want: []string{"bravo", "charlie", "alpha", "delta"}},
		{filters: `{"key": "auto_renew", "op": "eq", "value": "no"}`, want: []string{"alpha", "charlie"}},
		{sort: `{"key": "auto_renew", "desc": true}`, want: []string{"bravo", "alpha", "charlie", "delta"}},
		// Free text, in natural order, so 8 comes before 16 and 100.
		{sort: `{"key": "cores"}`, want: []string{"bravo", "alpha", "charlie", "delta"}},
		{sort: `{"key": "cores", "desc": true}`, want: []string{"charlie", "alpha", "bravo", "delta"}},
		{filters: `{"key": "cores", "op": "lt", "value": "10"}`, want: []string{"bravo"}},
		{filters: `{"key": "cores", "op": "eq", "value": "8"}`, want: []string{"bravo"}},
		// Case-sensitive: eu is not EU, and EU sorts first.
		{filters: `{"key": "region", "op": "eq", "value": "eu"}`, want: []string{"alpha"}},
		{filters: `{"key": "region", "op": "ne", "value": "eu"}`, want: []string{"bravo", "charlie", "delta"}},
		{sort: `{"key": "region"}`, want: []string{"bravo", "alpha", "charlie", "delta"}},
		// A key no Host has.
		{filters: `{"key": "os", "op": "eq", "value": "debian"}`, want: []string{}},
		{filters: `{"key": "os", "op": "ne", "value": "debian"}`, want: []string{"alpha", "bravo", "charlie", "delta"}},
		{filters: `{"key": "os", "op": "ge", "value": "a"}`, want: []string{}},
		{filters: `{"key": "os", "op": "le", "value": "z"}`, want: []string{}},
		{filters: `{"key": "os", "op": "lacks"}`, want: []string{"alpha", "bravo", "charlie", "delta"}},
		// Fields, as Attributes are: the port is a number, the state an enum.
		{filters: `{"key": "port", "op": "gt", "value": 100}`, want: []string{"bravo"}},
		{filters: `{"key": "state", "op": "eq", "value": "registered"}`, want: []string{"alpha", "bravo", "charlie", "delta"}},
		{filters: `{"key": "state", "op": "ge", "value": "managed"}`, want: []string{}},
		{filters: `{"key": "space", "op": "eq", "value": "alice"}`, want: []string{"alpha", "bravo", "charlie", "delta"}},
		{filters: `{"key": "name", "op": "lt", "value": "c"}`, want: []string{"alpha", "bravo"}},
		{filters: `{"key": "address", "op": "eq", "value": "203.0.113.4"}`, want: []string{"delta"}},
		{filters: `{"key": "account", "op": "ne", "value": "deploy"}`, want: []string{}},
		{filters: `{"key": "id", "op": "eq", "value": "` + alphaID + `"}`, want: []string{"alpha"}},
		{filters: `{"key": "host_key", "op": "has"}`, want: []string{}},
		{sort: `{"key": "port", "desc": true}`, want: []string{"bravo", "alpha", "charlie", "delta"}},
		// Terms are ANDed, and each sort key breaks the ties of the one before.
		{filters: `{"key": "price", "op": "ge", "value": 1}, {"key": "auto_renew", "op": "eq", "value": "no"}`, want: []string{"alpha", "charlie"}},
		{sort: `{"key": "auto_renew"}, {"key": "price", "desc": true}`, want: []string{"alpha", "charlie", "bravo", "delta"}},
		{
			filters: `{"key": "expires_at", "op": "le", "value": "2026-12-31"}, {"key": "auto_renew", "op": "eq", "value": "no"}`,
			sort:    `{"key": "expires_at"}`, want: []string{"alpha"},
		},
	} {
		body := `{"filters": [` + tc.filters + `], "sort": [` + tc.sort + `]}`
		if got := listNames(t, h, body); !slices.Equal(got, tc.want) {
			t.Errorf("host.list %s = %v, want %v", body, got, tc.want)
		}
	}
}

// A malformed term or sort key, a key host.list does not take and a value
// not of its key's type are refused; the API compares only absolute dates.
func TestHostListRefusesABadFilter(t *testing.T) {
	h := harness.Start(t)
	for _, tc := range []struct{ body, code string }{
		{`{"filters": [{"key": "price", "op": "like", "value": 10}]}`, "invalid_request"},
		{`{"filters": [{"key": "price", "op": "has", "value": 10}]}`, "invalid_request"},
		{`{"filters": [{"key": "price", "op": "eq"}]}`, "invalid_request"},
		{`{"filters": [{"key": "note", "op": "has"}]}`, "invalid_request"},
		{`{"filters": [{"key": "created_at", "op": "ge", "value": "2026-01-01"}]}`, "invalid_request"},
		{`{"filters": [{"key": "Region", "op": "eq", "value": "eu"}]}`, "invalid_request"},
		{`{"filters": [{"key": "price", "op": "gt", "value": "10"}]}`, "invalid_value"},
		{`{"filters": [{"key": "price", "op": "gt", "value": 1e1}]}`, "invalid_value"},
		{`{"filters": [{"key": "expires_at", "op": "le", "value": "+30d"}]}`, "invalid_value"},
		{`{"filters": [{"key": "currency", "op": "eq", "value": "eur"}]}`, "invalid_value"},
		{`{"filters": [{"key": "state", "op": "eq", "value": "gone"}]}`, "invalid_value"},
		{`{"filters": [{"key": "port", "op": "eq", "value": "22"}]}`, "invalid_value"},
		{`{"filters": [{"key": "region", "op": "eq", "value": 5}]}`, "invalid_value"},
		{`{"filters": [{"key": "region", "op": "eq", "value": "eu", "and": "more"}]}`, "unknown_field"},
		{`{"sort": [{"key": "price"}, {"key": "price", "desc": true}]}`, "invalid_request"},
		{`{"sort": [{"key": "note"}]}`, "invalid_request"},
		{`{"sort": [{"key": "updated_at"}]}`, "invalid_request"},
	} {
		if status, code, body := send(t, h, api.HostList, tc.body); code != tc.code {
			t.Errorf("host.list %s = %d %s, want %s", tc.body, status, body, tc.code)
		}
	}
}
