package cli_test

import (
	"encoding/json/v2"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cieons/koko/internal/harness"
)

// printedService is a Service as service show --json prints it.
type printedService struct {
	ID    string   `json:"id"`
	Name  string   `json:"name"`
	Host  string   `json:"host"`
	Ports []string `json:"ports"`
	// Attributes hold each value as JSON decodes it: a string, or a
	// float64 for a number.
	Attributes map[string]any `json:"attributes"`
	Note       *string        `json:"note"`
	CreatedAt  string         `json:"created_at"`
	UpdatedAt  string         `json:"updated_at"`
}

// decodeService reads one Service that a service command printed with
// --json.
func decodeService(t *testing.T, r harness.Result) printedService {
	t.Helper()
	r.WantSuccess(t)
	var s printedService
	if err := json.Unmarshal([]byte(r.Stdout), &s); err != nil {
		t.Fatalf("the command printed %q: %v", r.Stdout, err)
	}
	return s
}

// service add records a Service on a Host, with the ports given, and prints
// every field: its id, name, Host, ports, Attributes and Note, null for
// none.
func TestServiceAddPrintsTheServiceItMade(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	addHost(t, h.Koko, "web1")
	r := h.Koko("service", "add", "web1/nginx", "ports=443/tcp,80", "--json")
	r.WantSuccess(t)
	harness.Golden(t, "service-add.json", r.Stdout)
	got := decodeService(t, r)
	if got.Name != "nginx" || got.Host != "web1" || !slices.Equal(got.Ports, []string{"80/tcp", "443/tcp"}) ||
		len(got.Attributes) != 0 || got.Note != nil || !strings.HasPrefix(got.ID, "service_") {
		t.Errorf("service add printed %+v, want nginx on web1 on 80/tcp and 443/tcp, with no Attributes or Note", got)
	}
	if !strings.Contains(r.Stdout, `"note": null`) {
		t.Errorf("service add printed no null note:\n%s", r.Stdout)
	}
}

func TestServiceAddForPeople(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	addHost(t, h.Koko, "web1")
	r := h.Koko("service", "add", "web1/nginx")
	r.WantSuccess(t)
	if !strings.HasPrefix(r.Stdout, "Added Service web1/nginx (service_") || !strings.HasSuffix(r.Stdout, ").\n") {
		t.Errorf("stdout = %q", r.Stdout)
	}
}

// showService runs service show --json for ref, <host>/<service>, on the
// device run runs commands on.
func showService(t *testing.T, run func(args ...string) harness.Result, ref string) printedService {
	t.Helper()
	return decodeService(t, run("service", "show", ref, "--json"))
}

// A Service takes Attributes, typed as a Host's, and a Note from
// --note-file, beside its ports; service show prints all of them.
func TestServiceAddRecordsPortsAttributesAndANote(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	addHost(t, h.Koko, "web1")
	note := "Terminates TLS for the shop.\nConfig in /etc/nginx.\n"
	h.Koko("service", "add", "web1/nginx", "ports=443,51820/udp,80/tcp", "kind=proxy", "price=0",
		"--note-file", hostNoteFile(t, note)).WantSuccess(t)
	r := h.Koko("service", "show", "web1/nginx", "--json")
	r.WantSuccess(t)
	harness.Golden(t, "service-show.json", r.Stdout)
	got := decodeService(t, r)
	if !slices.Equal(got.Ports, []string{"80/tcp", "443/tcp", "51820/udp"}) || got.Attributes["kind"] != "proxy" ||
		got.Note == nil || *got.Note != note {
		t.Errorf("service show web1/nginx = %+v, want 80/tcp, 443/tcp and 51820/udp, kind proxy and the Note", got)
	}
	if !strings.Contains(r.Stdout, `"price": 0`) {
		t.Errorf("service show printed price as other than the JSON number 0:\n%s", r.Stdout)
	}
}

// A Service with no ports, such as a worker or a scheduled job, prints an
// empty set.
func TestAServiceMayListenOnNoPorts(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	addHost(t, h.Koko, "web1")
	if got := decodeService(t, h.Koko("service", "add", "web1/backup", "--json")); got.Ports == nil || len(got.Ports) != 0 {
		t.Errorf("service add web1/backup printed the ports %v, want []", got.Ports)
	}
	r := h.Koko("service", "show", "web1/backup")
	r.WantSuccess(t)
	if !strings.Contains(r.Stdout, "Ports:       none\n") {
		t.Errorf("stdout = %q, want no ports", r.Stdout)
	}
}

func TestServiceShowForPeople(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	addHost(t, h.Koko, "web1")
	h.Koko("service", "add", "web1/nginx", "ports=443,80", "kind=proxy", "--note-file", hostNoteFile(t, "Runs TLS.")).WantSuccess(t)
	r := h.Koko("service", "show", "web1/nginx")
	r.WantSuccess(t)
	for _, want := range []string{
		"Service web1/nginx (service_", "Host:        web1\n", "Ports:       80/tcp,443/tcp\n",
		"Attributes:\n  kind  proxy\n", "Note:\nRuns TLS.\n",
	} {
		if !strings.Contains(r.Stdout, want) {
			t.Errorf("stdout = %q, want it to say %q", r.Stdout, want)
		}
	}
	h.Koko("service", "add", "web1/cron").WantSuccess(t)
	r = h.Koko("service", "show", "web1/cron")
	if !strings.Contains(r.Stdout, "Attributes:  none\n") || !strings.Contains(r.Stdout, "Note:        none\n") {
		t.Errorf("stdout = %q, want no Attributes and no Note", r.Stdout)
	}
}

// A Service its Host does not have is unknown_service; a Host the caller
// cannot see answers, for each of its Services, exactly as a Host that does
// not exist.
func TestAServiceTheCallerCannotSeeAnswersAsUnknown(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	h.Koko("space", "add", "ops").WantSuccess(t)
	h.Koko("user", "invite", "carol").WantSuccess(t)
	carol := h.Join("carol")
	addHost(t, carol.Koko, "carol-box")
	r := carol.Koko("service", "show", "carol-box/nginx", "--json")
	r.WantError(t, 1, "unknown_service")
	harness.Golden(t, "service-show-unknown.json", r.Stderr)

	before := carol.Koko("service", "show", "web1/nginx", "--json")
	before.WantError(t, 1, "unknown_host")
	id := addHost(t, h.Koko, "web1", "space=ops").ID
	h.Koko("service", "add", "web1/nginx").WantSuccess(t)
	for _, args := range [][]string{
		{"service", "show", "web1/nginx"},
		{"service", "add", "web1/nginx"},
		{"service", "add", "web1/redis"},
		{"service", "set", "web1/nginx", "kind=proxy"},
		{"service", "rename", "web1/nginx", "web"},
		{"service", "rm", "web1/nginx"},
	} {
		if got := carol.Koko(append(args, "--json")...); got != before {
			t.Errorf("koko %s on a Host carol cannot see = %+v, want it as for no Host: %+v", strings.Join(args, " "), got, before)
		}
	}
	unknownID := carol.Koko("service", "show", "host_00000000/nginx", "--json")
	unknownID.WantError(t, 1, "unknown_host")
	seen := carol.Koko("service", "show", id+"/nginx", "--json")
	if want := strings.ReplaceAll(unknownID.Stderr, "host_00000000", id); seen.Stderr != want || seen.Status != unknownID.Status {
		t.Errorf("service show by the id of a Host carol cannot see = %+v, want it as for an id no Host has: %q", seen, want)
	}
}

// A Service name is a lowercase DNS label, unique on its Host: nginx can
// run on many Hosts, but only once on each.
func TestServiceNamesAreUniqueOnTheirHost(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	addHost(t, h.Koko, "web1")
	addHost(t, h.Koko, "web2")
	h.Koko("service", "add", "web1/nginx").WantSuccess(t)
	h.Koko("service", "add", "web2/nginx").WantSuccess(t)
	r := h.Koko("service", "add", "web1/nginx", "--json")
	r.WantError(t, 1, "name_taken")
	harness.Golden(t, "service-add-taken.json", r.Stderr)
	for _, name := range []string{"Nginx", "nginx_1", "nginx.1", "nginx-", strings.Repeat("a", 64)} {
		h.Koko("service", "add", "web1/"+name, "--json").WantError(t, 1, "invalid_name")
	}
	h.Koko("service", "add", "web1/"+strings.Repeat("a", 63)).WantSuccess(t)
	for _, ref := range []string{"web1", "web1/", "/nginx", "web1/nginx/x"} {
		h.Koko("service", "add", ref, "--json").WantError(t, 2, "invalid_usage")
	}
	if got := serviceNames(t, h.Koko); !slices.Equal(got, []string{"web1/" + strings.Repeat("a", 63), "web1/nginx", "web2/nginx"}) {
		t.Errorf("service ls = %q", got)
	}
}

// Each value rule refuses a bad value before any call, with exit status 2:
// the ports, the Attribute keys and values, and the fields a Service does
// not take.
func TestServiceValuesThatBreakTheirRulesAreRefusedBeforeAnyCall(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	addHost(t, h.Koko, "web1")
	calls := h.Calls()
	for _, pair := range []string{
		"ports=0",
		"ports=65536",
		"ports=https",
		"ports=443/TCP",
		"ports=443/sctp",
		"ports=443/",
		"ports=/tcp",
		"ports=443,",
		"ports=443,443/tcp",
		"ports=443 80",
		"Kind=proxy",
		"kind.name=proxy",
		"price=cheap",
		"expires_at=soon",
		"name=web",
		"host=web2",
		"space=ops",
		"note=hello",
		"port=443",
		"address=203.0.113.10",
		"kind=" + strings.Repeat("a", 1025),
		"kind=a\tb",
	} {
		h.Koko("service", "add", "web1/nginx", pair, "--json").WantError(t, 2, "invalid_usage")
	}
	if n := h.Calls() - calls; n != 0 {
		t.Errorf("the refusals made %d calls, want none", n)
	}
	// Other Services' ports are not checked: two may share one.
	h.Koko("service", "add", "web1/nginx", "ports=443").WantSuccess(t)
	h.Koko("service", "add", "web1/caddy", "ports=443/tcp").WantSuccess(t)
}

// service set changes only what it names, and ports= replaces the whole
// set.
func TestServiceSetChangesOnlyWhatItNames(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	h.Koko("user", "invite", "bob").WantSuccess(t)
	bob := h.Join("bob")
	addHost(t, bob.Koko, "web1")
	added := decodeService(t, bob.Koko("service", "add", "web1/nginx", "ports=80", "kind=proxy",
		"--note-file", hostNoteFile(t, "Kept."), "--json"))
	h.Advance(time.Hour)

	h.Koko("service", "set", "web1/nginx", "team=web").WantSuccess(t)
	r := bob.Koko("service", "set", "web1/nginx", "ports=443,8443/udp", "kind=tls", "--json")
	r.WantSuccess(t)
	harness.Golden(t, "service-set.json", r.Stdout)
	got := showService(t, bob.Koko, "web1/nginx")
	want := map[string]any{"kind": "tls", "team": "web"}
	if !slices.Equal(got.Ports, []string{"443/tcp", "8443/udp"}) || !maps.Equal(got.Attributes, want) || got.Note == nil ||
		*got.Note != "Kept." || got.CreatedAt != added.CreatedAt || got.UpdatedAt == added.UpdatedAt || got.ID != added.ID {
		t.Errorf("after both sets web1/nginx = %+v, want the new ports, %v, its Note, and a new updated_at", got, want)
	}
	if r := h.Koko("service", "set", "web1/nginx", "kind=proxy"); r.Stdout != "Changed Service web1/nginx ("+added.ID+").\n" {
		t.Errorf("stdout = %q", r.Stdout)
	}
}

// --unset clears an Attribute, the Note, or every port; --note-file
// replaces the Note.
func TestServiceSetUnsetClearsAnAttributeTheNoteOrThePorts(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	addHost(t, h.Koko, "web1")
	h.Koko("service", "add", "web1/nginx", "ports=443", "kind=proxy", "team=web", "--note-file", hostNoteFile(t, "Old.")).WantSuccess(t)
	h.Koko("service", "set", "web1/nginx", "--unset", "kind", "--unset", "cores", "--note-file", hostNoteFile(t, "New.")).WantSuccess(t)
	got := showService(t, h.Koko, "web1/nginx")
	if !maps.Equal(got.Attributes, map[string]any{"team": "web"}) || got.Note == nil || *got.Note != "New." ||
		!slices.Equal(got.Ports, []string{"443/tcp"}) {
		t.Errorf("web1/nginx = %+v, want only team, the new Note and its port", got)
	}
	h.Koko("service", "set", "web1/nginx", "--unset", "note", "--unset", "ports").WantSuccess(t)
	if got := showService(t, h.Koko, "web1/nginx"); got.Note != nil || got.Ports == nil || len(got.Ports) != 0 {
		t.Errorf("after --unset note --unset ports web1/nginx = %+v, want no Note and no ports", got)
	}
}

// service set takes neither name nor host, which are rename's or never
// change, and needs something to change; each is refused before any call.
func TestServiceSetRefusals(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	addHost(t, h.Koko, "web1")
	h.Koko("service", "add", "web1/nginx", "ports=443", "--note-file", hostNoteFile(t, "A Note.")).WantSuccess(t)
	calls := h.Calls()
	if msg := h.Koko("service", "set", "web1/nginx", "name=web", "--json").WantError(t, 2, "invalid_usage"); !strings.Contains(msg, "koko service rename") {
		t.Errorf("the refusal of name= says %q, want it to name koko service rename", msg)
	}
	r := h.Koko("service", "set", "web1/nginx", "host=web2", "--json")
	msg := r.WantError(t, 2, "invalid_usage")
	harness.Golden(t, "service-set-host.json", r.Stderr)
	if !strings.Contains(msg, "never moves to another Host") {
		t.Errorf("the refusal of host= says %q, want it to say a Service never moves", msg)
	}
	for _, args := range [][]string{
		{"--unset", "name"},
		{"--unset", "host"},
		{"--unset", "Kind"},
		{"ports="},
		{"ports=0"},
		{},
		{"--unset", "note", "--note-file", hostNoteFile(t, "Both.")},
		{"ports=80", "--unset", "ports"},
	} {
		h.Koko(append([]string{"service", "set", "web1/nginx", "--json"}, args...)...).WantError(t, 2, "invalid_usage")
	}
	if n := h.Calls() - calls; n != 0 {
		t.Errorf("the refusals made %d calls, want none", n)
	}
	if got := showService(t, h.Koko, "web1/nginx"); got.Note == nil || !slices.Equal(got.Ports, []string{"443/tcp"}) {
		t.Errorf("after the refusals web1/nginx = %+v, want it unchanged", got)
	}
}

// service rename changes the Service's name on its Host and keeps
// everything else; a name another Service on the Host holds is refused, and
// a new name never names another Host.
func TestServiceRename(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	addHost(t, h.Koko, "web1")
	addHost(t, h.Koko, "web2")
	added := decodeService(t, h.Koko("service", "add", "web1/nginx", "ports=443", "kind=proxy", "--json"))
	h.Koko("service", "add", "web1/redis").WantSuccess(t)
	h.Koko("service", "add", "web2/web").WantSuccess(t)

	r := h.Koko("service", "rename", "web1/nginx", "web", "--json")
	r.WantSuccess(t)
	harness.Golden(t, "service-rename.json", r.Stdout)
	got := decodeService(t, r)
	if got.ID != added.ID || got.Name != "web" || got.Host != "web1" || got.Attributes["kind"] != "proxy" ||
		!slices.Equal(got.Ports, []string{"443/tcp"}) {
		t.Errorf("service rename web1/nginx web = %+v, want the same Service under the name web", got)
	}
	h.Koko("service", "show", "web1/nginx", "--json").WantError(t, 1, "unknown_service")
	if r := h.Koko("service", "rename", "web1/web", "nginx"); r.Stdout != "Renamed Service web1/web to web1/nginx ("+added.ID+").\n" {
		t.Errorf("stdout = %q", r.Stdout)
	}
	h.Koko("service", "rename", "web1/nginx", "redis", "--json").WantError(t, 1, "name_taken")
	h.Koko("service", "rename", "web1/nginx", "Web", "--json").WantError(t, 1, "invalid_name")
	calls := h.Calls()
	if msg := h.Koko("service", "rename", "web1/nginx", "web2/nginx", "--json").WantError(t, 2, "invalid_usage"); !strings.Contains(msg, "never moves to another") {
		t.Errorf("the refusal of a new name on another Host says %q", msg)
	}
	if n := h.Calls() - calls; n != 0 {
		t.Errorf("the refusal made %d calls, want none", n)
	}
	if got := serviceNames(t, h.Koko); !slices.Equal(got, []string{"web1/nginx", "web1/redis", "web2/web"}) {
		t.Errorf("service ls = %q", got)
	}
}

// service rm deletes a Service and prints it as it was, Note and all.
func TestServiceRm(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	addHost(t, h.Koko, "web1")
	h.Koko("service", "add", "web1/nginx", "ports=443", "kind=proxy", "--note-file", hostNoteFile(t, "Gone soon.")).WantSuccess(t)
	h.Koko("service", "add", "web1/redis").WantSuccess(t)
	r := h.Koko("service", "rm", "web1/nginx", "--json")
	r.WantSuccess(t)
	harness.Golden(t, "service-rm.json", r.Stdout)
	got := decodeService(t, r)
	if got.Note == nil || *got.Note != "Gone soon." || got.Attributes["kind"] != "proxy" {
		t.Errorf("service rm printed %+v, want the Service as it was", got)
	}
	h.Koko("service", "show", "web1/nginx", "--json").WantError(t, 1, "unknown_service")
	h.Koko("service", "rm", "web1/nginx", "--json").WantError(t, 1, "unknown_service")
	if r := h.Koko("service", "rm", "web1/redis"); !strings.HasPrefix(r.Stdout, "Removed Service web1/redis (service_") {
		t.Errorf("stdout = %q", r.Stdout)
	}
	if got := serviceNames(t, h.Koko); len(got) != 0 {
		t.Errorf("service ls = %q, want nothing", got)
	}
	// A Service recorded again under the name is a new one.
	h.Koko("service", "add", "web1/nginx").WantSuccess(t)
	if got := showService(t, h.Koko, "web1/nginx"); len(got.Attributes) != 0 || got.Note != nil || len(got.Ports) != 0 {
		t.Errorf("web1/nginx recorded again = %+v, want nothing of the one removed", got)
	}
}

// A Service lives in its Host's Space: a moved Host keeps its Services,
// which follow it to whoever can act in its new Space, and a renamed Host's
// Services go by its new name.
func TestAMovedOrRenamedHostKeepsItsServices(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	addSpaceWithMembers(t, h, "ops", "bob")
	bob := h.Join("bob")
	addHost(t, h.Koko, "web1")
	added := decodeService(t, h.Koko("service", "add", "web1/nginx", "ports=443", "kind=proxy", "--json"))
	bob.Koko("service", "show", "web1/nginx", "--json").WantError(t, 1, "unknown_host")
	if got := serviceNames(t, bob.Koko); len(got) != 0 {
		t.Errorf("before the move bob's service ls = %q, want nothing", got)
	}

	h.Koko("host", "move", "web1", "ops").WantSuccess(t)
	if got := showService(t, bob.Koko, "web1/nginx"); got.ID != added.ID || got.Attributes["kind"] != "proxy" {
		t.Errorf("after the move bob sees web1/nginx as %+v, want the Service as it was", got)
	}
	if got := serviceNames(t, bob.Koko); !slices.Equal(got, []string{"web1/nginx"}) {
		t.Errorf("after the move bob's service ls = %q", got)
	}
	if r := bob.Koko("host", "show", "web1"); !strings.Contains(r.Stdout, "Services:\n  nginx  443/tcp\n") {
		t.Errorf("after the move bob's host show web1 = %q, want it to list nginx", r.Stdout)
	}
	h.Koko("host", "rename", "web1", "web9").WantSuccess(t)
	if got := showService(t, bob.Koko, "web9/nginx"); got.ID != added.ID || got.Host != "web9" {
		t.Errorf("after the rename web9/nginx = %+v, want the Service on web9", got)
	}
	h.Koko("host", "move", "web9", harness.Owner).WantSuccess(t)
	bob.Koko("service", "show", "web9/nginx", "--json").WantError(t, 1, "unknown_host")
}

// host rm deletes a Host never Managed with its Services, and a new Host
// under its name has none of them.
func TestHostRmDeletesItsServices(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	addHost(t, h.Koko, "web1")
	addHost(t, h.Koko, "web2")
	h.Koko("service", "add", "web1/nginx", "ports=443", "kind=proxy").WantSuccess(t)
	h.Koko("service", "add", "web1/redis").WantSuccess(t)
	h.Koko("service", "add", "web2/nginx").WantSuccess(t)
	h.Koko("host", "rm", "web1").WantSuccess(t)
	if got := serviceNames(t, h.Koko); !slices.Equal(got, []string{"web2/nginx"}) {
		t.Errorf("after host rm web1 service ls = %q, want web2's alone", got)
	}
	addHost(t, h.Koko, "web1")
	h.Koko("service", "show", "web1/nginx", "--json").WantError(t, 1, "unknown_service")
	if got := serviceNames(t, h.Koko, "kind.has"); len(got) != 0 {
		t.Errorf("service ls kind.has = %q, want nothing", got)
	}
}

// host show lists, for people, the Host's Services by name with their
// ports, so one command answers what runs there; its --json stays the Host
// as host ls prints one.
func TestHostShowListsItsServicesForPeople(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	addHost(t, h.Koko, "web1")
	addHost(t, h.Koko, "web2")
	h.Koko("service", "add", "web1/nginx", "ports=443,80", "--note-file", hostNoteFile(t, "Not in host show.")).WantSuccess(t)
	h.Koko("service", "add", "web1/api10", "ports=8080").WantSuccess(t)
	h.Koko("service", "add", "web1/api2").WantSuccess(t)
	h.Koko("service", "add", "web2/redis", "ports=6379").WantSuccess(t)

	r := h.Koko("host", "show", "web1")
	r.WantSuccess(t)
	if want := "Services:\n  api2   -\n  api10  8080/tcp\n  nginx  80/tcp,443/tcp\n"; !strings.Contains(r.Stdout, want) {
		t.Errorf("stdout = %q, want it to say %q", r.Stdout, want)
	}
	for _, leak := range []string{"redis", "Not in host show."} {
		if strings.Contains(r.Stdout, leak) {
			t.Errorf("host show web1 printed %q:\n%s", leak, r.Stdout)
		}
	}
	if r := h.Koko("host", "show", "web1", "--json"); strings.Contains(r.Stdout, "services") || strings.Contains(r.Stdout, "nginx") {
		t.Errorf("host show --json printed the Services:\n%s", r.Stdout)
	}
	h.Koko("service", "rm", "web2/redis").WantSuccess(t)
	if r := h.Koko("host", "show", "web2"); !strings.Contains(r.Stdout, "Services:    none\n") {
		t.Errorf("stdout = %q, want no Services", r.Stdout)
	}
}

// attr ls lists the keys in use on the Services the caller can see as well
// as on the Hosts, and a Preset has the same type on both.
func TestAttrLsListsTheKeysInUseOnServices(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	addSpaceWithMembers(t, h, "ops", "bob")
	h.Koko("user", "invite", "carol").WantSuccess(t)
	bob, carol := h.Join("bob"), h.Join("carol")
	addHost(t, h.Koko, "web1", "space=ops", "rack=r7")
	h.Koko("service", "add", "web1/nginx", "kind=proxy", "price=5").WantSuccess(t)
	addHost(t, carol.Koko, "carol-box")
	carol.Koko("service", "add", "carol-box/backup", "schedule=nightly").WantSuccess(t)
	carol.Koko("service", "add", "carol-box/api", "price=cheap", "--json").WantError(t, 2, "invalid_usage")

	inUse := func(keys []printedAttr) []string {
		var used []string
		for _, k := range keys {
			if k.InUse {
				used = append(used, k.Key)
			}
		}
		return used
	}
	for _, tc := range []struct {
		who  string
		run  func(args ...string) harness.Result
		want []string
	}{
		{"the Owner", h.Koko, []string{"price", "kind", "rack", "schedule"}},
		{"bob, in ops", bob.Koko, []string{"price", "kind", "rack"}},
		{"carol, outside ops", carol.Koko, []string{"schedule"}},
	} {
		if got := inUse(attrKeys(t, tc.run)); !slices.Equal(got, tc.want) {
			t.Errorf("for %s attr ls has in use %v, want %v", tc.who, got, tc.want)
		}
	}
}

// A Host id stands wherever a Host name does in <host>/<service>.
func TestAHostIDStandsForItsNameInAServicesName(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	id := addHost(t, h.Koko, "web1").ID
	added := decodeService(t, h.Koko("service", "add", id+"/nginx", "--json"))
	if added.Host != "web1" {
		t.Errorf("service add %s/nginx printed the Host %q, want web1", id, added.Host)
	}
	for _, args := range [][]string{
		{"service", "show", id + "/nginx"},
		{"service", "set", id + "/nginx", "kind=proxy"},
		{"service", "rename", id + "/nginx", "web"},
		{"service", "rm", id + "/web"},
	} {
		if got := decodeService(t, h.Koko(append(args, "--json")...)); got.ID != added.ID {
			t.Errorf("koko %s = %+v, want the Service %s", strings.Join(args, " "), got, added.ID)
		}
	}
}
