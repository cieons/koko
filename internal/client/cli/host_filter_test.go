package cli_test

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cieons/koko/internal/harness"
)

// listedHost is a Host as host ls --json prints it, each Attribute value as
// the JSON it printed.
type listedHost struct {
	Name       string                    `json:"name"`
	Attributes map[string]jsontext.Value `json:"attributes"`
}

// recordHost runs host add for the Host named hostName on the device run
// runs commands on, with the address 203.0.113.10, the Host Account deploy
// and pairs, in UTC.
func recordHost(t *testing.T, run func(args ...string) harness.Result, hostName string, pairs ...string) {
	t.Helper()
	run(append([]string{"host", "add", hostName, "address=203.0.113.10", "account=deploy"}, pairs...)...).WantSuccess(t)
}

// listed runs a host ls --json command line as the Local caller, in the time
// zone tz, and returns the names it lists, in its order.
func listed(t *testing.T, h *harness.Harness, tz string, args ...string) []string {
	t.Helper()
	r := h.KokoEnv([]string{"TZ=" + tz}, append([]string{"host", "ls", "--json"}, args...)...)
	r.WantSuccess(t)
	var hosts []listedHost
	if err := json.Unmarshal([]byte(r.Stdout), &hosts); err != nil {
		t.Fatalf("host ls --json printed %q: %v", r.Stdout, err)
	}
	names := []string{}
	for _, h := range hosts {
		names = append(names, h.Name)
	}
	return names
}

// The renewal question in one command: the Hosts that expire within 30 days
// of today in the caller's time zone and do not renew by themselves, soonest
// first. Relative dates resolve in writes as in filters.
func TestHostLsAnswersTheRenewalQuestion(t *testing.T) {
	h := harness.Start(t)
	// 23:30 in UTC is already the 7th in Berlin.
	h.SetNow(time.Date(2026, 11, 6, 23, 30, 0, 0, time.UTC))
	berlin := []string{"TZ=Europe/Berlin"}
	for _, args := range [][]string{
		{"web1", "expires_at=+30d", "auto_renew=no", "price=4.50", "currency=EUR"},
		{"web2", "expires_at=+20d", "auto_renew=yes"},
		{"web3", "expires_at=+31d", "auto_renew=no"},
		{"web4", "expires_at=today", "auto_renew=no", "billing_cycle=monthly"},
		{"web5", "auto_renew=no"},
	} {
		h.KokoEnv(berlin, append([]string{"host", "add", args[0], "address=203.0.113.10", "account=deploy"}, args[1:]...)...).WantSuccess(t)
	}
	r := h.KokoEnv(berlin, "host", "ls", "expires_at.le=+30d", "auto_renew=no", "--sort", "expires_at", "--json")
	r.WantSuccess(t)
	harness.Golden(t, "host-ls-renewal.json", r.Stdout)
	if got, want := listed(t, h, "Europe/Berlin", "expires_at.le=+30d", "auto_renew=no", "--sort", "expires_at"), []string{"web4", "web1"}; !slices.Equal(got, want) {
		t.Errorf("the renewal question lists %v, want %v", got, want)
	}
	// In UTC it is still the 6th, so web1's 30 days from the 7th are 31
	// from today there.
	if got, want := listed(t, h, "UTC", "expires_at.le=+30d", "auto_renew=no", "--sort", "expires_at"), []string{"web4"}; !slices.Equal(got, want) {
		t.Errorf("in UTC the renewal question lists %v, want %v", got, want)
	}
}

// Each operator once, on Attributes and fields alike, ANDed; a Host that
// lacks the key matches != and lacks, and no other.
func TestHostLsTakesEachOperator(t *testing.T) {
	h := harness.Start(t)
	recordHost(t, h.Koko, "web1", "price=10", "region=eu")
	recordHost(t, h.Koko, "web2", "price=9.5", "port=2222")
	recordHost(t, h.Koko, "web3")
	for _, tc := range []struct {
		terms []string
		want  []string
	}{
		{[]string{"price=10"}, []string{"web1"}},
		{[]string{"price!=10"}, []string{"web2", "web3"}},
		{[]string{"price.lt=10"}, []string{"web2"}},
		{[]string{"price.le=10"}, []string{"web1", "web2"}},
		{[]string{"price.gt=9.5"}, []string{"web1"}},
		{[]string{"price.ge=9.50"}, []string{"web1", "web2"}},
		{[]string{"price.has"}, []string{"web1", "web2"}},
		{[]string{"price.lacks"}, []string{"web3"}},
		{[]string{"region=eu", "price=10"}, []string{"web1"}},
		{[]string{"region=eu", "price=9.5"}, []string{}},
		{[]string{"space=" + harness.Owner, "state=registered", "port.gt=100"}, []string{"web2"}},
		{[]string{"host_key.lacks", "name.ge=web2"}, []string{"web2", "web3"}},
	} {
		if got := listed(t, h, "UTC", tc.terms...); !slices.Equal(got, tc.want) {
			t.Errorf("host ls %s lists %v, want %v", strings.Join(tc.terms, " "), got, tc.want)
		}
	}
}

// Without --sort, Hosts are by name in natural order, the order --sort name
// gives, so web2 comes before web10.
func TestHostLsListsByNameInNaturalOrder(t *testing.T) {
	h := harness.Start(t)
	for _, name := range []string{"web10", "web2", "db1"} {
		recordHost(t, h.Koko, name)
	}
	want := []string{"db1", "web2", "web10"}
	for _, sort := range [][]string{nil, {"--sort", "name"}} {
		if got := listed(t, h, "UTC", sort...); !slices.Equal(got, want) {
			t.Errorf("host ls %s lists %v, want %v", strings.Join(sort, " "), got, want)
		}
	}
}

// --sort repeats, each key breaking the ties of the one before, with .desc
// from the greatest value down; a Host that lacks a key sorts last either
// way, and ties stay by name.
func TestHostLsSortsByRepeatedKeys(t *testing.T) {
	h := harness.Start(t)
	recordHost(t, h.Koko, "web1", "auto_renew=yes", "price=5")
	recordHost(t, h.Koko, "web2", "auto_renew=no", "price=10")
	recordHost(t, h.Koko, "web3", "auto_renew=no", "price=9.5")
	recordHost(t, h.Koko, "web4", "cores=16")
	recordHost(t, h.Koko, "web5", "cores=8")
	for _, tc := range []struct {
		sort []string
		want []string
	}{
		{[]string{"--sort", "price"}, []string{"web1", "web3", "web2", "web4", "web5"}},
		{[]string{"--sort", "price.desc"}, []string{"web2", "web3", "web1", "web4", "web5"}},
		{[]string{"--sort", "auto_renew", "--sort", "price.desc"}, []string{"web2", "web3", "web1", "web4", "web5"}},
		{[]string{"--sort", "auto_renew.desc", "--sort", "price"}, []string{"web1", "web3", "web2", "web4", "web5"}},
		{[]string{"--sort", "cores"}, []string{"web5", "web4", "web1", "web2", "web3"}},
		{[]string{"--sort=name.desc"}, []string{"web5", "web4", "web3", "web2", "web1"}},
	} {
		if got := listed(t, h, "UTC", tc.sort...); !slices.Equal(got, tc.want) {
			t.Errorf("host ls %s lists %v, want %v", strings.Join(tc.sort, " "), got, tc.want)
		}
	}
}

// A malformed term or sort key is refused before any call, with exit status
// 2 and the error on stderr, so a typo is told apart from an empty result.
func TestHostLsRefusesAMalformedTermBeforeAnyCall(t *testing.T) {
	h := harness.Start(t)
	for _, tc := range []struct {
		args []string
		says string
	}{
		{[]string{"price.foo=1"}, ".foo is no operator"},
		{[]string{"price.eq=1"}, ".eq is no operator"},
		{[]string{"price.ne=1"}, ".ne is no operator"},
		{[]string{"price.has=1"}, "price.has takes no value"},
		{[]string{"price.le"}, "needs a value"},
		{[]string{"price="}, "cannot be empty; to find what lacks price, write price.lacks"},
		{[]string{"price=ten"}, "not a decimal number"},
		{[]string{"price.gt=1e3"}, "not a decimal number"},
		{[]string{"currency=eur"}, "ISO 4217"},
		{[]string{"billing_cycle=weekly"}, "monthly, quarterly"},
		{[]string{"state=gone"}, "registered, managed, retired"},
		{[]string{"port=ssh"}, "not a decimal number"},
		{[]string{"expires_at.le=+30x"}, "not a relative date"},
		{[]string{"expires_at.le=2026-02-30"}, "not a calendar date"},
		{[]string{"Region=eu"}, "does not start with a letter a-z"},
		{[]string{"note.has"}, "does not filter or sort on the field note"},
		{[]string{"created_at.ge=2026-01-01"}, "does not filter or sort on the field created_at"},
		{[]string{"host=web1"}, "does not filter or sort on the field host"},
		{[]string{"web1"}, "unexpected argument"},
		{[]string{"--sort", "note"}, "does not filter or sort on the field note"},
		{[]string{"--sort", "price.up"}, "write --sort <key>, or --sort <key>.desc"},
		{[]string{"--sort", "price", "--sort", "price.desc"}, "sorted by price already"},
	} {
		r := h.KokoEnv([]string{"TZ=UTC"}, append([]string{"host", "ls", "--json"}, tc.args...)...)
		if msg := r.WantError(t, 2, "invalid_usage"); !strings.Contains(msg, tc.says) {
			t.Errorf("host ls %s says %q, want it to say %q", strings.Join(tc.args, " "), msg, tc.says)
		}
	}
	if n := h.Calls(); n != 0 {
		t.Errorf("the refused command lines made %d calls, want none", n)
	}
}

// A relative date needs the caller's time zone, so a TZ that names no zone
// koko can load is refused rather than read as UTC, and only where a
// relative date needs it.
func TestATimeZoneKokoCannotLoadIsRefused(t *testing.T) {
	h := harness.Start(t)
	recordHost(t, h.Koko, "web1", "expires_at=2026-12-01")
	bad := []string{"TZ=Mars/Olympus_Mons"}
	r := h.KokoEnv(bad, "host", "ls", "expires_at.le=+30d", "--json")
	if msg := r.WantError(t, 2, "invalid_usage"); !strings.Contains(msg, "TZ=Mars/Olympus_Mons names no time zone") {
		t.Errorf("host ls under a bad TZ says %q", msg)
	}
	r = h.KokoEnv(bad, "host", "set", "web1", "expires_at=+1y", "--json")
	r.WantError(t, 2, "invalid_usage")
	h.KokoEnv(bad, "host", "ls", "expires_at.le=2026-12-01").WantSuccess(t)
}

// The relative dates of the one table that the web UI's tests are to run too
// (#71), resolved in a write in the time zone TZ names and on the clock the
// harness sets.
func TestRelativeDatesResolveInTheCallersTimeZone(t *testing.T) {
	b, err := os.ReadFile("testdata/relative-dates.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Case string  `json:"case"`
		Now  string  `json:"now"`
		TZ   string  `json:"tz"`
		Date string  `json:"date"`
		Want *string `json:"want"`
	}
	if err := json.Unmarshal(b, &cases, json.RejectUnknownMembers(true)); err != nil {
		t.Fatal(err)
	}
	h := harness.Start(t)
	recordHost(t, h.Koko, "web1")
	for _, tc := range cases {
		now, err := time.Parse(time.RFC3339, tc.Now)
		if err != nil {
			t.Fatalf("%s: %v", tc.Case, err)
		}
		h.SetNow(now)
		calls := h.Calls()
		r := h.KokoEnv([]string{"TZ=" + tc.TZ}, "host", "set", "web1", "expires_at="+tc.Date, "--json")
		if tc.Want == nil {
			r.WantError(t, 2, "invalid_usage")
			if h.Calls() != calls {
				t.Errorf("%s: expires_at=%s was refused after a call", tc.Case, tc.Date)
			}
			continue
		}
		var got listedHost
		r.WantSuccess(t)
		if err := json.Unmarshal([]byte(r.Stdout), &got); err != nil {
			t.Fatal(err)
		}
		if want := `"` + *tc.Want + `"`; string(got.Attributes["expires_at"]) != want {
			t.Errorf("%s: expires_at=%s at %s in %s is %s, want %s", tc.Case, tc.Date, tc.Now, tc.TZ, got.Attributes["expires_at"], want)
		}
	}
}

// A Preset's value that does not parse as its type is refused before any
// call, on add and on set, and never falls back to text.
func TestAPresetValueThatDoesNotParseIsRefusedBeforeAnyCall(t *testing.T) {
	h := harness.Start(t)
	recordHost(t, h.Koko, "web1")
	calls := h.Calls()
	for _, pair := range []string{
		"price=-1", "price=ten", "price=1e3", "price=0.1234567", "price=1234567890123456", "price=.5",
		"currency=eur", "currency=XYZ", "billing_cycle=weekly", "auto_renew=true",
		"expires_at=2026-02-30", "expires_at=next-week",
	} {
		for _, args := range [][]string{
			{"host", "add", "web2", "address=203.0.113.10", "account=deploy", pair, "--json"},
			{"host", "set", "web1", pair, "--json"},
		} {
			r := h.KokoEnv([]string{"TZ=UTC"}, args...)
			if msg := r.WantError(t, 2, "invalid_usage"); !strings.Contains(msg, "the value of ") {
				t.Errorf("%s says %q", strings.Join(args, " "), msg)
			}
		}
	}
	if n := h.Calls() - calls; n != 0 {
		t.Errorf("the refused writes made %d calls, want none", n)
	}
}

// Each Preset prints in its JSON type: a number as a JSON number in its one
// exact decimal form, exactly as stored, and every other value as a string.
func TestHostShowPrintsTypedValues(t *testing.T) {
	h := harness.Start(t)
	recordHost(t, h.Koko, "web1")
	h.Koko("host", "set", "web1", "provider=hetzner", "provider_account=ops@example.com", "region=fsn1",
		"price=9.90", "currency=EUR", "billing_cycle=yearly", "expires_at=2027-11-30", "auto_renew=no", "cores=8").WantSuccess(t)
	r := h.Koko("host", "show", "web1", "--json")
	r.WantSuccess(t)
	harness.Golden(t, "host-show-presets.json", r.Stdout)
	for _, tc := range []struct{ price, printed string }{
		{"0.000001", "0.000001"},
		{"123456789.123456", "123456789.123456"},
		{"999999999999999", "999999999999999"},
		{"007.50", "7.5"},
	} {
		r := h.Koko("host", "set", "web1", "price="+tc.price, "--json")
		var got listedHost
		r.WantSuccess(t)
		if err := json.Unmarshal([]byte(r.Stdout), &got); err != nil {
			t.Fatal(err)
		}
		if string(got.Attributes["price"]) != tc.printed {
			t.Errorf("price=%s printed %s, want %s", tc.price, got.Attributes["price"], tc.printed)
		}
	}
	if out := h.Koko("host", "show", "web1").Stdout; !strings.Contains(out, "  price             7.5\n") {
		t.Errorf("host show printed:\n%s", out)
	}
}
