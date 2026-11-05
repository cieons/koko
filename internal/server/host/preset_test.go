package host_test

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"maps"
	"testing"

	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/shared/api"
)

// rawAttributes returns the Attributes of the one Host an answer about one
// Host holds, each value as the JSON the koko server wrote.
func rawAttributes(t *testing.T, body string) map[string]string {
	t.Helper()
	var answer struct {
		Host struct {
			Attributes map[string]jsontext.Value `json:"attributes"`
		} `json:"host"`
	}
	if err := json.Unmarshal([]byte(body), &answer); err != nil {
		t.Fatalf("the answer %s holds no Host: %v", body, err)
	}
	raw := map[string]string{}
	for k, v := range answer.Host.Attributes {
		raw[k] = string(v)
	}
	return raw
}

// A Preset's value takes the JSON type that matches it, and must parse as
// the Preset's type, or the write is refused: it never falls back to text.
func TestAPresetValueMustParseAsItsType(t *testing.T) {
	h := harness.Start(t)
	for _, attrs := range []string{
		`{"price": "10"}`,
		`{"price": -1}`,
		`{"price": 1e2}`,
		`{"price": 0.12345678901234567}`,
		`{"price": 1234567890123456}`,
		`{"price": 0.0000001}`,
		`{"currency": "eur"}`,
		`{"currency": "XYZ"}`,
		`{"currency": 978}`,
		`{"billing_cycle": "weekly"}`,
		`{"auto_renew": "true"}`,
		`{"expires_at": "2026-02-30"}`,
		`{"expires_at": "2026-2-1"}`,
		`{"expires_at": "+30d"}`,
		`{"expires_at": "today"}`,
		`{"region": 5}`,
		`{"provider": "hetzner\tcloud"}`,
	} {
		body := `{"hosts": [` + web1 + `, "attributes": ` + attrs + `}]}`
		if status, code, answer := send(t, h, api.HostAdd, body); code != "invalid_value" {
			t.Errorf("host.add with the Attributes %s = %d %s, want invalid_value", attrs, status, answer)
		}
	}
	if _, code, body := send(t, h, api.HostAdd, `{"hosts": [`+web1+`}]}`); code != "" {
		t.Fatalf("host.add web1 = %s", body)
	}
	if status, code, body := send(t, h, api.HostSet, `{"host": "web1", "attributes": {"price": "cheap"}}`); code != "invalid_value" {
		t.Errorf(`host.set price "cheap" = %d %s, want invalid_value`, status, body)
	}
	// An Attribute value is a JSON string or number, and nothing else.
	if status, code, body := send(t, h, api.HostSet, `{"host": "web1", "attributes": {"region": true}}`); code != "invalid_request" {
		t.Errorf("host.set region true = %d %s, want invalid_request", status, body)
	}
}

// Each Preset takes the values of its type, and every other key free text.
// A number is stored in its one exact decimal form and written as a JSON
// number exactly as stored, never through binary floating point.
func TestPresetValuesAreStoredInTheirOneForm(t *testing.T) {
	h := harness.Start(t)
	if _, code, body := send(t, h, api.HostAdd, `{"hosts": [`+web1+`}]}`); code != "" {
		t.Fatalf("host.add web1 = %s", body)
	}
	attrs := `{"provider": "hetzner", "provider_account": "ops@example.com", "region": "fsn1",
		"price": 9.90, "currency": "EUR", "billing_cycle": "semiannual", "expires_at": "2028-02-29", "auto_renew": "no",
		"cores": "8"}`
	want := map[string]string{
		"provider": `"hetzner"`, "provider_account": `"ops@example.com"`, "region": `"fsn1"`,
		"price": `9.9`, "currency": `"EUR"`, "billing_cycle": `"semiannual"`, "expires_at": `"2028-02-29"`, "auto_renew": `"no"`,
		"cores": `"8"`,
	}
	if _, _, body := send(t, h, api.HostSet, `{"host": "web1", "attributes": `+attrs+`}`); !maps.Equal(rawAttributes(t, body), want) {
		t.Errorf("host.set = %s, want the Attributes %v", body, want)
	}
	if _, _, body := send(t, h, api.HostShow, `{"host": "web1"}`); !maps.Equal(rawAttributes(t, body), want) {
		t.Errorf("host.show = %s, want the Attributes %v", body, want)
	}

	for _, tc := range []struct{ price, stored string }{
		{"0", "0"},
		{"-0.0", "0"},
		{"10", "10"},
		{"10.000", "10"},
		{"1.10", "1.1"},
		{"0.1", "0.1"},
		{"0.000001", "0.000001"},
		{"123456789012345", "123456789012345"},
		{"999999999.999999", "999999999.999999"},
	} {
		_, code, body := send(t, h, api.HostSet, `{"host": "web1", "attributes": {"price": `+tc.price+`}}`)
		if got := rawAttributes(t, body)["price"]; code != "" || got != tc.stored {
			t.Errorf("host.set price %s = %s, want it stored as %s", tc.price, body, tc.stored)
		}
	}
}
