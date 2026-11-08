package cli_test

import (
	"encoding/json/v2"
	"maps"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/cieons/koko/internal/harness"
)

// retiredGolden compares a refusal that names the Retired Host whose id is
// id with its golden file, after replacing the id, which the refusal's
// message holds and which differs on every run.
func retiredGolden(t *testing.T, name, id, out string) {
	t.Helper()
	harness.Golden(t, name, strings.ReplaceAll(out, id, "host_<id>"))
}

// listedStates returns each Host a host ls --json run printed as its name
// and state, in its order. It takes the run, rather than running host ls
// itself as hostNames does, so a test can compare the same run's output
// with a golden file.
func listedStates(t *testing.T, r harness.Result) []string {
	t.Helper()
	r.WantSuccess(t)
	var listed []printedHost
	if err := json.Unmarshal([]byte(r.Stdout), &listed); err != nil {
		t.Fatalf("host ls --json printed %q: %v", r.Stdout, err)
	}
	got := []string{}
	for _, h := range listed {
		got = append(got, h.Name+" "+h.State)
	}
	return got
}

// host retire marks a Registered Host as gone: it prints the Host as it
// left it, Retired, with its Attributes and Note. host ls leaves it out, its
// name names it no more and is free for a new Host, and its id still shows
// it, as it was.
func TestHostRetireFreesTheNameAndKeepsTheRecord(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	old := addHost(t, h.Koko, "web1", "region=fsn1", "--note-file", hostNoteFile(t, "Cancelled at the provider."))

	r := h.Koko("host", "retire", "web1", "--json")
	r.WantSuccess(t)
	harness.Golden(t, "host-retire.json", r.Stdout)
	got := decodeHost(t, r)
	if got.ID != old.ID || got.Name != "web1" || got.State != "retired" || got.Attributes["region"] != "fsn1" ||
		got.Note == nil || *got.Note != "Cancelled at the provider." {
		t.Errorf("host retire printed %+v, want web1 (%s) Retired with its Attribute and Note", got, old.ID)
	}
	if names := hostNames(t, h.Koko); len(names) != 0 {
		t.Errorf("host ls lists %q, want no Retired Host", names)
	}
	h.Koko("host", "show", "web1", "--json").WantError(t, 1, "unknown_host")

	again := addHost(t, h.Koko, "web1")
	if again.ID == old.ID {
		t.Fatalf("the new web1 has the Retired Host's id %s", old.ID)
	}
	if shown := showHost(t, h.Koko, old.ID); shown.State != "retired" || shown.Name != "web1" || shown.Attributes["region"] != "fsn1" {
		t.Errorf("host show %s = %+v, want the Retired web1 as it was", old.ID, shown)
	}
	if shown := showHost(t, h.Koko, "web1"); shown.ID != again.ID || shown.State != "registered" {
		t.Errorf("host show web1 = %+v, want the new web1 (%s)", shown, again.ID)
	}
}

// host ls --all brings the Retired Hosts back, among the others in the
// usual order, and --all state=retired lists only them; filters without
// --all never reach them.
func TestHostLsAllBringsBackTheRetiredHosts(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	old := addHost(t, h.Koko, "web1")
	addHost(t, h.Koko, "db1")
	h.Koko("host", "retire", "web1").WantSuccess(t)
	r := h.Koko("host", "ls", "--all", "--json")
	if got := listedStates(t, r); !slices.Equal(got, []string{"db1 registered", "web1 retired"}) {
		t.Errorf("host ls --all lists %q, want db1 and the Retired web1", got)
	}
	harness.Golden(t, "host-ls-all.json", r.Stdout)

	// Two Hosts of one name come by id, which is random.
	addHost(t, h.Koko, "web1")
	got := listedStates(t, h.Koko("host", "ls", "--all", "--json"))
	if slices.Sort(got); !slices.Equal(got, []string{"db1 registered", "web1 registered", "web1 retired"}) {
		t.Errorf("host ls --all lists %q, want db1 and both web1s", got)
	}
	retired := h.Koko("host", "ls", "--all", "state=retired", "--json")
	if got := listedStates(t, retired); !slices.Equal(got, []string{"web1 retired"}) || !strings.Contains(retired.Stdout, old.ID) {
		t.Errorf("host ls --all state=retired lists %q, want only the Retired web1 (%s)", got, old.ID)
	}
	if got := listedStates(t, h.Koko("host", "ls", "state=retired", "--json")); len(got) != 0 {
		t.Errorf("host ls state=retired lists %q without --all, want none", got)
	}
}

// A Retired Host refuses with host_retired a change of its address, port or
// Host Account, a rename, and retiring it again, each changing nothing, a
// refused key among allowed ones included. It takes changes to its
// Attributes and Note, and moves, and stays Retired through them.
func TestARetiredHostTakesOnlyAttributeNoteAndMoveChanges(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	h.Koko("space", "add", "ops").WantSuccess(t)
	old := addHost(t, h.Koko, "web1", "region=fsn1")
	h.Koko("host", "retire", "web1").WantSuccess(t)

	for _, c := range []struct {
		args   []string
		golden string
	}{
		{[]string{"host", "set", old.ID, "address=203.0.113.20"}, "host-set-retired.json"},
		{[]string{"host", "set", old.ID, "port=2222"}, ""},
		{[]string{"host", "set", old.ID, "--unset", "port"}, ""},
		{[]string{"host", "set", old.ID, "account=root"}, ""},
		{[]string{"host", "set", old.ID, "region=nbg1", "account=root"}, ""},
		{[]string{"host", "rename", old.ID, "web9"}, "host-rename-retired.json"},
		{[]string{"host", "rename", old.ID, "web1"}, ""},
		{[]string{"host", "retire", old.ID}, "host-retire-retired.json"},
	} {
		r := h.Koko(append(c.args, "--json")...)
		r.WantError(t, 1, "host_retired")
		if c.golden != "" {
			retiredGolden(t, c.golden, old.ID, r.Stderr)
		}
	}
	if got := showHost(t, h.Koko, old.ID); got.Name != "web1" || got.Address != old.Address || got.Port != old.Port ||
		got.Account != old.Account || got.Attributes["region"] != "fsn1" || got.State != "retired" {
		t.Errorf("after the refusals the Retired Host is %+v, want it as it was retired", got)
	}

	h.Koko("host", "set", old.ID, "region=nbg1", "expires_at=2026-12-01", "--note-file", hostNoteFile(t, "Refunded.")).WantSuccess(t)
	h.Koko("host", "set", old.ID, "--unset", "region").WantSuccess(t)
	h.Koko("host", "move", old.ID, "ops").WantSuccess(t)
	got := showHost(t, h.Koko, old.ID)
	if got.State != "retired" || got.Space != "ops" || !maps.Equal(got.Attributes, map[string]string{"expires_at": "2026-12-01"}) ||
		got.Note == nil || *got.Note != "Refunded." {
		t.Errorf("after the changes it takes the Retired Host is %+v, want it Retired in ops with expires_at and the Note", got)
	}
	h.Koko("host", "set", old.ID, "--unset", "note").WantSuccess(t)
	if got := showHost(t, h.Koko, old.ID); got.Note != nil || got.State != "retired" {
		t.Errorf("after --unset note the Retired Host is %+v, want it Retired with no Note", got)
	}
}

// A Managed Host retires with its pin, which it keeps. Enroll refuses it
// with host_retired before koko connects to it, whatever the flags. host
// rm, which refuses a Managed Host and names host retire as the way on,
// refuses it too, as every Host that was ever Managed.
func TestAManagedHostRetiresWithItsPinAndIsNeverEnrolled(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	host := managedHost(t, h, "web1")
	pinned := fingerprint(host)
	if msg := h.Koko("host", "rm", "web1", "--json").WantError(t, 1, "host_was_managed"); !strings.Contains(msg, "koko host retire web1") {
		t.Errorf("host rm of a Managed Host says %q, want it to name koko host retire web1", msg)
	}

	retired := decodeHost(t, h.Koko("host", "retire", "web1", "--json"))
	wantPinned(t, retired, "retired", pinned)
	conns := host.Conns()
	r := h.KokoStdin(hostPassword, "host", "enroll", retired.ID, "--password-stdin", "--json")
	r.WantError(t, 1, "host_retired")
	retiredGolden(t, "host-enroll-retired.json", retired.ID, r.Stderr)
	for _, flags := range [][]string{{"--replace-host-key"}, {"--host-key", pinned}} {
		args := append([]string{"host", "enroll", retired.ID, "--password-stdin", "--json"}, flags...)
		h.KokoStdin(hostPassword, args...).WantError(t, 1, "host_retired")
	}
	if got := host.Conns(); got != conns {
		t.Errorf("Enroll of a Retired Host made %d connections to it, want none", got-conns)
	}
	if msg := h.Koko("host", "rm", retired.ID, "--json").WantError(t, 1, "host_was_managed"); strings.Contains(msg, "retire") {
		t.Errorf("host rm of a Retired Host says %q, which names retiring it again", msg)
	}
	wantPinned(t, showHost(t, h.Koko, retired.ID), "retired", pinned)
}

// A Retired Host that was never Managed has no pin, and host rm deletes it
// by its id, with its Services, as any Host never Managed.
func TestARetiredHostNeverManagedIsDeleted(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	old := addHost(t, h.Koko, "web1")
	h.Koko("service", "add", "web1/nginx").WantSuccess(t)
	h.Koko("host", "retire", "web1").WantSuccess(t)
	if got := decodeHost(t, h.Koko("host", "rm", old.ID, "--json")); got.ID != old.ID || got.State != "retired" {
		t.Errorf("host rm printed %+v, want the Retired web1 (%s) as it was", got, old.ID)
	}
	h.Koko("host", "show", old.ID, "--json").WantError(t, 1, "unknown_host")
	h.Koko("service", "show", old.ID+"/nginx", "--json").WantError(t, 1, "unknown_host")
}

// A Retired Host's Services are hidden with it: service ls lists neither
// them nor, by its name, anything but the new Host's. They are kept as they
// were: service show still shows one by the Host's id, host show by the id
// lists them for people, attr ls counts their keys, and service add, set,
// rename and rm on the Retired Host are refused with host_retired.
func TestARetiredHostsServicesAreHiddenAndKeptAsTheyWere(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	old := addHost(t, h.Koko, "web1")
	h.Koko("service", "add", "web1/nginx", "ports=443", "kind=proxy").WantSuccess(t)
	addHost(t, h.Koko, "db1")
	h.Koko("service", "add", "db1/postgres", "ports=5432").WantSuccess(t)
	h.Koko("host", "retire", "web1").WantSuccess(t)
	addHost(t, h.Koko, "web1")
	h.Koko("service", "add", "web1/app").WantSuccess(t)

	if got := serviceNames(t, h.Koko); !slices.Equal(got, []string{"db1/postgres", "web1/app"}) {
		t.Errorf("service ls lists %q, want the Services of db1 and the new web1 only", got)
	}
	if got := serviceNames(t, h.Koko, "kind=proxy"); len(got) != 0 {
		t.Errorf("service ls kind=proxy lists %q, want none: nginx runs on a Retired Host", got)
	}

	// Its host is the Retired Host's id, which alone names it, so that
	// <host>/<service> names this nginx and never one on the new web1.
	nginx := old.ID + "/nginx"
	kept := decodeService(t, h.Koko("service", "show", nginx, "--json"))
	if kept.Host != old.ID || !slices.Equal(kept.Ports, []string{"443/tcp"}) || kept.Attributes["kind"] != "proxy" {
		t.Errorf("service show %s = %+v, want nginx as it was recorded, on the Host %s", nginx, kept, old.ID)
	}
	for _, c := range []struct {
		args   []string
		golden string
	}{
		{[]string{"service", "set", nginx, "kind=web"}, "service-set-retired.json"},
		{[]string{"service", "set", nginx, "--unset", "ports"}, ""},
		{[]string{"service", "add", old.ID + "/redis"}, ""},
		{[]string{"service", "rename", nginx, "proxy"}, ""},
		{[]string{"service", "rm", nginx}, ""},
	} {
		r := h.Koko(append(c.args, "--json")...)
		r.WantError(t, 1, "host_retired")
		if c.golden != "" {
			retiredGolden(t, c.golden, old.ID, r.Stderr)
		}
	}
	if got := decodeService(t, h.Koko("service", "show", nginx, "--json")); !reflect.DeepEqual(got, kept) {
		t.Errorf("after the refusals nginx is %+v, want it as it was: %+v", got, kept)
	}
	if out := h.Koko("host", "show", old.ID).Stdout; !strings.Contains(out, "nginx") {
		t.Errorf("host show %s printed\n%s\nwant its Service nginx", old.ID, out)
	}
	if !slices.ContainsFunc(attrKeys(t, h.Koko), func(k printedAttr) bool { return k.Key == "kind" && k.InUse }) {
		t.Error("attr ls does not have kind in use, which nginx on the Retired web1 holds")
	}
}

// space show counts a Retired Host apart from the others, as
// retired_hosts, and space rm refuses a Space that holds only a Retired
// Host, naming it, until it moves out.
func TestASpaceThatHoldsARetiredHostIsNotRemoved(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	h.Koko("space", "add", "ops").WantSuccess(t)
	old := addHost(t, h.Koko, "web1", "space=ops")
	h.Koko("host", "retire", "web1").WantSuccess(t)

	r := h.Koko("space", "show", "ops", "--json")
	r.WantSuccess(t)
	var shown struct {
		Counts struct {
			Hosts        int `json:"hosts"`
			RetiredHosts int `json:"retired_hosts"`
		} `json:"counts"`
	}
	if err := json.Unmarshal([]byte(r.Stdout), &shown); err != nil {
		t.Fatalf("space show --json printed %q: %v", r.Stdout, err)
	}
	if c := shown.Counts; c.Hosts != 0 || c.RetiredHosts != 1 {
		t.Errorf("space show ops counts %d Hosts and %d Retired Hosts, want 0 and 1", c.Hosts, c.RetiredHosts)
	}
	if msg := h.Koko("space", "rm", "ops", "--json").WantError(t, 1, "space_not_empty"); !strings.Contains(msg, "1 Retired Host") {
		t.Errorf("the refusal says %q, want it to name the 1 Retired Host", msg)
	}
	h.Koko("host", "move", old.ID, harness.Owner).WantSuccess(t)
	h.Koko("space", "rm", "ops").WantSuccess(t)
}

// host set refuses state= before any call, naming host retire, the one way
// a person changes a Host's state.
func TestHostSetStateNamesHostRetire(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	addHost(t, h.Koko, "web1")
	calls := h.Calls()
	msg := h.Koko("host", "set", "web1", "state=retired", "--json").WantError(t, 2, "invalid_usage")
	if !strings.Contains(msg, "koko host retire <host>") {
		t.Errorf("the refusal says %q, want it to name koko host retire <host>", msg)
	}
	if n := h.Calls() - calls; n != 0 {
		t.Errorf("the refusal made %d calls, want none", n)
	}
}

// For people, host retire names the Host by its name and its id, which
// names it from then on.
func TestHostRetireForPeople(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	added := addHost(t, h.Koko, "web1")
	r := h.Koko("host", "retire", "web1")
	r.WantSuccess(t)
	if want := "Retired Host web1 (" + added.ID + "); name it by its id from now on.\n"; r.Stdout != want {
		t.Errorf("stdout = %q, want %q", r.Stdout, want)
	}
	if out := h.Koko("host", "show", added.ID).Stdout; !strings.Contains(out, "State:       retired\n") {
		t.Errorf("host show %s printed\n%s\nwant State: retired", added.ID, out)
	}
}
