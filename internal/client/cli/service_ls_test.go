package cli_test

import (
	"encoding/json/v2"
	"slices"
	"strings"
	"testing"

	"github.com/cieons/koko/internal/harness"
)

// serviceNames runs service ls --json with args on the device run runs
// commands on and returns the Services it lists, as <host>/<service>, in
// its order.
func serviceNames(t *testing.T, run func(args ...string) harness.Result, args ...string) []string {
	t.Helper()
	r := run(append([]string{"service", "ls", "--json"}, args...)...)
	r.WantSuccess(t)
	var listed []printedService
	if err := json.Unmarshal([]byte(r.Stdout), &listed); err != nil {
		t.Fatalf("service ls --json printed %q: %v", r.Stdout, err)
	}
	names := []string{}
	for _, s := range listed {
		names = append(names, s.Host+"/"+s.Name)
	}
	return names
}

// service ls --json is a bare array of the Services on every Host the
// caller can see, by Host and then by name, each in natural order, without
// their Notes: a Service lives in its Host's Space.
func TestServiceLsListsTheServicesOnTheHostsTheCallerCanSee(t *testing.T) {
	h := harness.Start(t)
	addSpaceWithMembers(t, h, "ops", "bob")
	h.Koko("user", "invite", "carol").WantSuccess(t)
	h.Koko("space", "add", "data").WantSuccess(t)
	bob, carol, alice := h.Join("bob"), h.Join("carol"), h.Join(harness.Owner)
	addHost(t, h.Koko, "web10", "space=ops")
	addHost(t, h.Koko, "web2", "space=ops")
	addHost(t, h.Koko, "db1", "space=data")
	addHost(t, carol.Koko, "carol-box")
	for _, args := range [][]string{
		{"web10/nginx", "ports=443"},
		{"web2/nginx", "ports=443,80", "kind=proxy", "--note-file", hostNoteFile(t, "Note of web2/nginx.")},
		{"web2/api", "ports=8080"},
		{"db1/postgres", "ports=5432"},
		{"carol-box/backup"},
	} {
		h.Koko(append([]string{"service", "add"}, args...)...).WantSuccess(t)
	}

	r := bob.Koko("service", "ls", "--json")
	r.WantSuccess(t)
	harness.Golden(t, "service-ls.json", r.Stdout)
	if strings.Contains(r.Stdout, `"note"`) {
		t.Errorf("service ls --json printed a note member without --with-note:\n%s", r.Stdout)
	}
	every := []string{"carol-box/backup", "db1/postgres", "web2/api", "web2/nginx", "web10/nginx"}
	for _, tc := range []struct {
		who  string
		run  func(args ...string) harness.Result
		want []string
	}{
		{"bob", bob.Koko, []string{"web2/api", "web2/nginx", "web10/nginx"}},
		{"carol", carol.Koko, []string{"carol-box/backup"}},
		{"the Owner's device", alice.Koko, every},
		{"the Local caller", h.Koko, every},
	} {
		if got := serviceNames(t, tc.run); !slices.Equal(got, tc.want) {
			t.Errorf("service ls for %s = %q, want %q", tc.who, got, tc.want)
		}
	}
}

// service ls filters by host= and by the Services' own Attributes, with
// every operator host ls takes, and sorts by them.
func TestServiceLsFiltersByHostAndByAttribute(t *testing.T) {
	h := harness.Start(t)
	addHost(t, h.Koko, "web1", "provider=hetzner")
	addHost(t, h.Koko, "web2")
	for _, args := range [][]string{
		{"web1/nginx", "kind=proxy", "price=5"},
		{"web1/api", "kind=app", "price=12.5"},
		{"web2/nginx", "kind=proxy"},
		{"web2/cron"},
	} {
		h.Koko(append([]string{"service", "add"}, args...)...).WantSuccess(t)
	}
	for _, tc := range []struct {
		args []string
		want []string
	}{
		{[]string{"host=web1"}, []string{"web1/api", "web1/nginx"}},
		{[]string{"host!=web1"}, []string{"web2/cron", "web2/nginx"}},
		{[]string{"kind=proxy"}, []string{"web1/nginx", "web2/nginx"}},
		{[]string{"kind=proxy", "host=web2"}, []string{"web2/nginx"}},
		{[]string{"name=nginx", "--sort", "host.desc"}, []string{"web2/nginx", "web1/nginx"}},
		{[]string{"kind.lacks"}, []string{"web2/cron"}},
		{[]string{"price.gt=10"}, []string{"web1/api"}},
		{[]string{"price.has", "--sort", "price.desc"}, []string{"web1/api", "web1/nginx"}},
		// A Host's Attributes are its own: provider is on web1, not on
		// its Services.
		{[]string{"provider=hetzner"}, []string{}},
	} {
		if got := serviceNames(t, h.Koko, tc.args...); !slices.Equal(got, tc.want) {
			t.Errorf("service ls %s = %q, want %q", strings.Join(tc.args, " "), got, tc.want)
		}
	}
}

// service ls takes the fields of a Service, not of its Host, and refuses a
// malformed term before any call, as host ls does.
func TestServiceLsRefusesAFilterBeforeAnyCall(t *testing.T) {
	h := harness.Start(t)
	for _, args := range [][]string{
		{"space=ops"},
		{"state=managed"},
		{"ports=443"},
		{"note.has"},
		{"price.gt=cheap"},
		{"kind.eq=proxy"},
		{"--sort", "ports"},
	} {
		h.Koko(append([]string{"service", "ls", "--json"}, args...)...).WantError(t, 2, "invalid_usage")
	}
	if n := h.Calls(); n != 0 {
		t.Errorf("the refusals made %d calls, want none", n)
	}
}

// --with-note brings each Service's Note into the list, null for one with
// none.
func TestServiceLsWithNote(t *testing.T) {
	h := harness.Start(t)
	addHost(t, h.Koko, "web1")
	h.Koko("service", "add", "web1/nginx", "--note-file", hostNoteFile(t, "First line.\nSecond line.\n")).WantSuccess(t)
	h.Koko("service", "add", "web1/redis").WantSuccess(t)
	r := h.Koko("service", "ls", "--with-note", "--json")
	r.WantSuccess(t)
	harness.Golden(t, "service-ls-with-note.json", r.Stdout)

	r = h.Koko("service", "ls", "--with-note")
	r.WantSuccess(t)
	if !strings.HasSuffix(r.Stdout, "\nNote of web1/nginx:\nFirst line.\nSecond line.\n") || strings.Contains(r.Stdout, "Note of web1/redis") {
		t.Errorf("stdout = %q, want the table, then web1/nginx's Note alone", r.Stdout)
	}
}

func TestServiceLsForPeople(t *testing.T) {
	h := harness.Start(t)
	if r := h.Koko("service", "ls"); r.Stdout != "No Services.\n" {
		t.Errorf("service ls with no Service printed %q", r.Stdout)
	}
	if r := h.Koko("service", "ls", "--json"); r.Stdout != "[]\n" {
		t.Errorf("service ls --json with no Service printed %q, want []", r.Stdout)
	}
	addHost(t, h.Koko, "web1")
	h.Koko("service", "add", "web1/nginx", "ports=443,80", "--note-file", hostNoteFile(t, "Not in the table.")).WantSuccess(t)
	h.Koko("service", "add", "web1/worker").WantSuccess(t)
	r := h.Koko("service", "ls")
	r.WantSuccess(t)
	lines := strings.Split(strings.TrimSpace(r.Stdout), "\n")
	if len(lines) != 3 || strings.Join(strings.Fields(lines[0]), " ") != "NAME PORTS ID" ||
		!strings.HasPrefix(strings.Join(strings.Fields(lines[1]), " "), "web1/nginx 80/tcp,443/tcp service_") ||
		!strings.HasPrefix(strings.Join(strings.Fields(lines[2]), " "), "web1/worker - service_") {
		t.Errorf("stdout = %q, want a header and a row for each Service", r.Stdout)
	}
}
