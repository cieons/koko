package cli_test

import (
	"context"
	"encoding/json/v2"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cieons/koko/internal/client/cli"
	"github.com/cieons/koko/internal/harness"
)

// printedHost is a Host as host show --json prints it.
type printedHost struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Space      string            `json:"space"`
	Address    string            `json:"address"`
	Port       int               `json:"port"`
	Account    string            `json:"account"`
	State      string            `json:"state"`
	HostKey    *string           `json:"host_key"`
	Attributes map[string]string `json:"attributes"`
	Note       *string           `json:"note"`
	CreatedAt  string            `json:"created_at"`
	UpdatedAt  string            `json:"updated_at"`
}

// decodeHost reads one Host that a host command printed with --json.
func decodeHost(t *testing.T, r harness.Result) printedHost {
	t.Helper()
	r.WantSuccess(t)
	var h printedHost
	if err := json.Unmarshal([]byte(r.Stdout), &h); err != nil {
		t.Fatalf("the command printed %q: %v", r.Stdout, err)
	}
	return h
}

// addHost runs host add --json for the Host named hostName on the device run
// runs commands on, with the address 203.0.113.10 and the Host Account
// deploy unless pairs give their own, and returns what it printed.
func addHost(t *testing.T, run func(args ...string) harness.Result, hostName string, pairs ...string) printedHost {
	t.Helper()
	args := []string{"host", "add", hostName, "--json"}
	for _, key := range []string{"address=203.0.113.10", "account=deploy"} {
		k, _, _ := strings.Cut(key, "=")
		if !slices.ContainsFunc(pairs, func(p string) bool { return strings.HasPrefix(p, k+"=") }) {
			args = append(args, key)
		}
	}
	return decodeHost(t, run(append(args, pairs...)...))
}

// showHost runs host show --json for ref on the device run runs commands on.
func showHost(t *testing.T, run func(args ...string) harness.Result, ref string) printedHost {
	t.Helper()
	return decodeHost(t, run("host", "show", ref, "--json"))
}

// hostNames runs host ls --json on the device run runs commands on and
// returns the names it lists, in its order.
func hostNames(t *testing.T, run func(args ...string) harness.Result) []string {
	t.Helper()
	r := run("host", "ls", "--json")
	r.WantSuccess(t)
	var listed []printedHost
	if err := json.Unmarshal([]byte(r.Stdout), &listed); err != nil {
		t.Fatalf("host ls --json printed %q: %v", r.Stdout, err)
	}
	names := []string{}
	for _, h := range listed {
		names = append(names, h.Name)
	}
	return names
}

// hostNoteFile writes note into a file of its own and returns its path.
func hostNoteFile(t *testing.T, note string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "note.md")
	if err := os.WriteFile(path, []byte(note), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// addSpaceWithMembers records the shared Space named space, and invites the
// Users users as its Members.
func addSpaceWithMembers(t *testing.T, h *harness.Harness, space string, users ...string) {
	t.Helper()
	h.Koko("space", "add", space).WantSuccess(t)
	for _, u := range users {
		h.Koko("user", "invite", u).WantSuccess(t)
		h.Koko("space", "member", "add", space, u).WantSuccess(t)
	}
}

// host add records a Registered Host in the caller's Personal Space, on
// port 22, with no pin, no Attributes and no Note, and prints every field,
// null where it has no value.
func TestHostAddPrintsTheHostItMade(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	r := h.Koko("host", "add", "web1", "address=203.0.113.10", "account=deploy", "--json")
	r.WantSuccess(t)
	harness.Golden(t, "host-add.json", r.Stdout)
	got := decodeHost(t, r)
	if got.Space != harness.Owner || got.Port != 22 || got.State != "registered" || got.HostKey != nil ||
		len(got.Attributes) != 0 || got.Note != nil || !strings.HasPrefix(got.ID, "host_") {
		t.Errorf("host add printed %+v, want a Registered Host in %s on port 22, with no pin, Attributes or Note", got, harness.Owner)
	}
	for _, member := range []string{`"host_key": null`, `"note": null`} {
		if !strings.Contains(r.Stdout, member) {
			t.Errorf("host add printed no %s:\n%s", member, r.Stdout)
		}
	}
}

func TestHostAddForPeople(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	r := h.Koko("host", "add", "web1", "address=203.0.113.10", "account=deploy")
	r.WantSuccess(t)
	if !strings.HasPrefix(r.Stdout, "Added Host web1 (host_") || !strings.HasSuffix(r.Stdout, ") to Space "+harness.Owner+".\n") {
		t.Errorf("stdout = %q", r.Stdout)
	}
}

// Fields and Attributes are both key=value on one command line, and the Note
// comes from --note-file; host show prints all of them.
func TestHostAddRecordsFieldsAttributesAndANote(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	note := "Set up for the shop.\nRuns nginx and the API.\n"
	h.Koko("host", "add", "web1", "address=203.0.113.10", "port=2222", "account=deploy",
		"provider=hetzner", "region=fsn1", "--note-file", hostNoteFile(t, note)).WantSuccess(t)
	r := h.Koko("host", "show", "web1", "--json")
	r.WantSuccess(t)
	harness.Golden(t, "host-show.json", r.Stdout)
	got := decodeHost(t, r)
	if got.Port != 2222 || got.Attributes["provider"] != "hetzner" || got.Attributes["region"] != "fsn1" ||
		got.Note == nil || *got.Note != note {
		t.Errorf("host show web1 = %+v, want port 2222, provider hetzner, region fsn1 and the Note", got)
	}
}

// The Host Account has no default, and neither has the address: without
// either, host add is refused before any call.
func TestHostAddNeedsAnAddressAndAHostAccount(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	r := h.Koko("host", "add", "web1", "address=203.0.113.10", "--json")
	r.WantError(t, 2, "invalid_usage")
	harness.Golden(t, "host-add-no-account.json", r.Stderr)
	h.Koko("host", "add", "web1", "account=deploy", "--json").WantError(t, 2, "invalid_usage")
	if n := h.Calls(); n != 0 {
		t.Errorf("the refusals made %d calls, want none", n)
	}
}

// A Host goes to the caller's Personal Space unless space= names another
// they can act in; the Local caller acts as the Owner.
func TestHostAddGoesToTheCallersPersonalSpaceUnlessNamed(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	addSpaceWithMembers(t, h, "ops", "bob")
	h.Koko("space", "add", "data").WantSuccess(t)
	bob := h.Join("bob")

	if got := addHost(t, bob.Koko, "web1").Space; got != "bob" {
		t.Errorf("bob's Host went to %s, want his Personal Space", got)
	}
	if got := addHost(t, bob.Koko, "web2", "space=ops").Space; got != "ops" {
		t.Errorf("bob's Host with space=ops went to %s, want ops", got)
	}
	bob.Koko("host", "add", "web3", "address=203.0.113.10", "account=deploy", "space=data", "--json").
		WantError(t, 1, "unknown_space")
	if got := addHost(t, h.Koko, "web4").Space; got != harness.Owner {
		t.Errorf("the Local caller's Host went to %s, want the Owner's Personal Space", got)
	}
	if got := addHost(t, h.Koko, "web5", "space=data").Space; got != "data" {
		t.Errorf("the Local caller's Host with space=data went to %s, want data", got)
	}
}

// A Host name is unique across the koko server: a name a Host holds is
// refused, and the refusal is the same whichever Space holds the name, one
// the caller can see or not, and names nothing else of that Host.
func TestATakenHostNameIsRefusedWhicheverSpaceHoldsIt(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	addSpaceWithMembers(t, h, "ops", "bob")
	h.Koko("user", "invite", "carol").WantSuccess(t)
	carol := h.Join("carol")
	addHost(t, carol.Koko, "carol-box")
	addHost(t, h.Koko, "web1", "space=ops", "address=198.51.100.7", "provider=hetzner")

	first := carol.Koko("host", "add", "web1", "address=203.0.113.20", "account=root", "--json")
	first.WantError(t, 1, "name_taken")
	harness.Golden(t, "host-add-taken.json", first.Stderr)
	for _, leak := range []string{"ops", "198.51.100.7", "hetzner", "deploy", "host_"} {
		if strings.Contains(first.Stderr, leak) {
			t.Errorf("the refusal %q names %q, of the Host carol cannot see", first.Stderr, leak)
		}
	}
	for _, to := range []string{"ops", harness.Owner, "bob", "carol"} {
		h.Koko("host", "move", "web1", to).WantSuccess(t)
		added := carol.Koko("host", "add", "web1", "address=203.0.113.20", "account=root", "--json")
		renamed := carol.Koko("host", "rename", "carol-box", "web1", "--json")
		if added != first || renamed != first {
			t.Errorf("with web1 in %s carol's add answered %+v and her rename %+v, want both as her first add: %+v", to, added, renamed, first)
		}
	}
	if got := hostNames(t, h.Koko); !slices.Equal(got, []string{"carol-box", "web1"}) {
		t.Errorf("host ls = %q, want carol-box and web1, once each", got)
	}
}

// Two adds of one name at the same moment never both succeed: the check
// runs inside the write's transaction.
func TestConcurrentAddsOfOneNameRecordOneHost(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	var wg sync.WaitGroup
	results := make([]harness.Result, 8)
	for i := range results {
		wg.Go(func() {
			results[i] = h.Koko("host", "add", "web1", "address=203.0.113.10", "account=deploy", "--json")
		})
	}
	wg.Wait()
	added := 0
	for _, r := range results {
		if r.Status == 0 {
			added++
			continue
		}
		r.WantError(t, 1, "name_taken")
	}
	if added != 1 {
		t.Errorf("%d of the concurrent adds of web1 succeeded, want 1", added)
	}
}

// A Host name is a lowercase DNS label, so it works as an SSH Name and in a
// shell without quoting.
func TestHostNamesAreLowercaseDNSLabels(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	for _, name := range []string{"Web1", "web_1", "web.1", "web-", strings.Repeat("a", 64)} {
		r := h.Koko("host", "add", name, "address=203.0.113.10", "account=deploy", "--json")
		r.WantError(t, 1, "invalid_name")
	}
	addHost(t, h.Koko, strings.Repeat("a", 63))
	h.Koko("host", "rename", strings.Repeat("a", 63), "Web1", "--json").WantError(t, 1, "invalid_name")
	if got := hostNames(t, h.Koko); !slices.Equal(got, []string{strings.Repeat("a", 63)}) {
		t.Errorf("host ls = %q, want only the 63-character Host", got)
	}
}

// Each value rule refuses a bad value before any call, with exit status 2.
func TestHostValuesThatBreakTheirRulesAreRefusedBeforeAnyCall(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	for _, pair := range []string{
		"address=web..example.com",
		"address=10.0.0.256",
		"address=fe80::1%eth0",
		"address=-web.example.com",
		"port=0",
		"port=65536",
		"port=ssh",
		"account=-deploy",
		"account=de ploy",
		"account=" + strings.Repeat("a", 33),
		"Region=eu",
		"1region=eu",
		"region.name=eu",
		"relay=",
		"relay=Web2",
		"relay=web2/nginx",
		"host_key=SHA256:abc",
		"state=managed",
		"name=web2",
		"note=hello",
		"region=" + strings.Repeat("a", 1025),
		"region=eu\twest",
		"region=eu\nwest",
	} {
		key, _, _ := strings.Cut(pair, "=")
		args := []string{"host", "add", "web1", pair, "--json"}
		for _, required := range []string{"address=203.0.113.10", "account=deploy"} {
			if !strings.HasPrefix(required, key+"=") {
				args = append(args, required)
			}
		}
		h.Koko(args...).WantError(t, 2, "invalid_usage")
	}
	if n := h.Calls(); n != 0 {
		t.Errorf("the refusals made %d calls, want none", n)
	}
	r := h.Koko("host", "add", "web1", "address=203.0.113.10", "account=deploy", "region="+strings.Repeat("a", 1024), "--json")
	r.WantSuccess(t)
}

// An address is an IP address or a DNS name, kept in one form; two Hosts can
// share one, behind one NAT address on different ports.
func TestHostAddressesAreKeptInOneForm(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	for _, tc := range []struct{ host, given, kept string }{
		{"v6", "2001:DB8:0:0::1", "2001:db8::1"},
		{"dns", "Web1.Example.COM", "web1.example.com"},
		{"v4", "203.0.113.10", "203.0.113.10"},
	} {
		if got := addHost(t, h.Koko, tc.host, "address="+tc.given).Address; got != tc.kept {
			t.Errorf("address=%s was kept as %s, want %s", tc.given, got, tc.kept)
		}
	}
	addHost(t, h.Koko, "nat1", "address=198.51.100.1", "port=2201")
	if got := addHost(t, h.Koko, "nat2", "address=198.51.100.1", "port=2202"); got.Address != "198.51.100.1" || got.Port != 2202 {
		t.Errorf("the second Host behind 198.51.100.1 = %+v", got)
	}
}

// A Host id stands wherever a Host name does.
func TestAHostIDStandsForItsName(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	h.Koko("space", "add", "ops").WantSuccess(t)
	id := addHost(t, h.Koko, "web1").ID
	if got := showHost(t, h.Koko, id); got.Name != "web1" {
		t.Errorf("host show %s = %+v, want web1", id, got)
	}
	if got := decodeHost(t, h.Koko("host", "set", id, "region=eu", "--json")); got.Attributes["region"] != "eu" {
		t.Errorf("host set %s region=eu = %+v", id, got)
	}
	if got := decodeHost(t, h.Koko("host", "rename", id, "web2", "--json")); got.Name != "web2" || got.ID != id {
		t.Errorf("host rename %s web2 = %+v", id, got)
	}
	if got := decodeHost(t, h.Koko("host", "move", id, "ops", "--json")); got.Space != "ops" {
		t.Errorf("host move %s ops = %+v", id, got)
	}
	if got := decodeHost(t, h.Koko("host", "rm", id, "--json")); got.ID != id {
		t.Errorf("host rm %s = %+v", id, got)
	}
	h.Koko("host", "show", id, "--json").WantError(t, 1, "unknown_host")
}

func TestHostShowForPeople(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	h.Koko("host", "add", "web1", "address=203.0.113.10", "account=deploy", "region=fsn1", "provider=hetzner",
		"--note-file", hostNoteFile(t, "Runs nginx.")).WantSuccess(t)
	r := h.Koko("host", "show", "web1")
	r.WantSuccess(t)
	for _, want := range []string{
		"Host web1 (host_", "Space:       " + harness.Owner, "State:       registered", "Address:     203.0.113.10",
		"Port:        22", "Account:     deploy", "Host key:    none", "Attributes:\n  provider  hetzner\n  region    fsn1\n",
		"Note:\nRuns nginx.\n",
	} {
		if !strings.Contains(r.Stdout, want) {
			t.Errorf("stdout = %q, want it to say %q", r.Stdout, want)
		}
	}
	h.Koko("host", "add", "web2", "address=203.0.113.11", "account=deploy").WantSuccess(t)
	r = h.Koko("host", "show", "web2")
	if !strings.Contains(r.Stdout, "Attributes:  none\n") || !strings.Contains(r.Stdout, "Note:        none\n") {
		t.Errorf("stdout = %q, want no Attributes and no Note", r.Stdout)
	}
	if got := showHost(t, h.Koko, "web2"); got.Note != nil {
		t.Errorf("host show web2 --json has the Note %q, want null", *got.Note)
	}
}

// A Host in a Space the caller cannot see answers exactly as a Host that
// does not exist, by name and by id.
func TestAHostTheCallerCannotSeeAnswersAsUnknown(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	h.Koko("space", "add", "ops").WantSuccess(t)
	h.Koko("user", "invite", "carol").WantSuccess(t)
	carol := h.Join("carol")
	before := carol.Koko("host", "show", "web1", "--json")
	before.WantError(t, 1, "unknown_host")
	harness.Golden(t, "host-show-unknown.json", before.Stderr)
	id := addHost(t, h.Koko, "web1", "space=ops").ID

	for _, args := range [][]string{
		{"host", "show", "web1"},
		{"host", "set", "web1", "region=eu"},
		{"host", "rename", "web1", "web2"},
		{"host", "move", "web1", "carol"},
		{"host", "rm", "web1"},
	} {
		if got := carol.Koko(append(args, "--json")...); got != before {
			t.Errorf("koko %s on a Host carol cannot see = %+v, want it as for no Host: %+v", strings.Join(args, " "), got, before)
		}
	}
	unknownID := carol.Koko("host", "show", "host_00000000", "--json")
	unknownID.WantError(t, 1, "unknown_host")
	seen := carol.Koko("host", "show", id, "--json")
	if want := strings.ReplaceAll(unknownID.Stderr, "host_00000000", id); seen.Stderr != want || seen.Status != unknownID.Status {
		t.Errorf("host show of the id of a Host carol cannot see = %+v, want it as for an id no Host has: %q", seen, want)
	}
	if got := hostNames(t, carol.Koko); len(got) != 0 {
		t.Errorf("carol's host ls = %q, want nothing", got)
	}
}

// host ls --json is a bare array of the Hosts the caller can see, by name,
// each with its Attributes in their own object and without its Note.
func TestHostLsListsTheHostsTheCallerCanSee(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	addSpaceWithMembers(t, h, "ops", "bob")
	h.Koko("user", "invite", "carol").WantSuccess(t)
	h.Koko("space", "add", "data").WantSuccess(t)
	bob, carol, alice := h.Join("bob"), h.Join("carol"), h.Join(harness.Owner)
	addHost(t, h.Koko, "web2", "space=ops", "region=fsn1", "--note-file", hostNoteFile(t, "Note of web2."))
	addHost(t, h.Koko, "db1", "space=data", "address=203.0.113.20", "port=2222")
	addHost(t, bob.Koko, "bob-box")
	addHost(t, carol.Koko, "carol-box")

	r := bob.Koko("host", "ls", "--json")
	r.WantSuccess(t)
	harness.Golden(t, "host-ls.json", r.Stdout)
	if strings.Contains(r.Stdout, `"note"`) {
		t.Errorf("host ls --json printed a note member without --with-note:\n%s", r.Stdout)
	}
	every := []string{"bob-box", "carol-box", "db1", "web2"}
	for _, tc := range []struct {
		who  string
		run  func(args ...string) harness.Result
		want []string
	}{
		{"bob", bob.Koko, []string{"bob-box", "web2"}},
		{"carol", carol.Koko, []string{"carol-box"}},
		{"the Owner's device", alice.Koko, every},
		{"the Local caller", h.Koko, every},
	} {
		if got := hostNames(t, tc.run); !slices.Equal(got, tc.want) {
			t.Errorf("host ls for %s = %q, want %q", tc.who, got, tc.want)
		}
	}
}

// --with-note brings each Host's Note into the list, null for a Host with
// none.
func TestHostLsWithNote(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	addHost(t, h.Koko, "web1", "--note-file", hostNoteFile(t, "First line.\nSecond line.\n"))
	addHost(t, h.Koko, "web2")
	r := h.Koko("host", "ls", "--with-note", "--json")
	r.WantSuccess(t)
	harness.Golden(t, "host-ls-with-note.json", r.Stdout)

	r = h.Koko("host", "ls", "--with-note")
	r.WantSuccess(t)
	if !strings.HasSuffix(r.Stdout, "\nNote of web1:\nFirst line.\nSecond line.\n") || strings.Contains(r.Stdout, "Note of web2") {
		t.Errorf("stdout = %q, want the table, then web1's Note alone", r.Stdout)
	}
}

func TestHostLsForPeople(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	if r := h.Koko("host", "ls"); r.Stdout != "No Hosts.\n" {
		t.Errorf("host ls with no Host printed %q", r.Stdout)
	}
	if r := h.Koko("host", "ls", "--json"); r.Stdout != "[]\n" {
		t.Errorf("host ls --json with no Host printed %q, want []", r.Stdout)
	}
	addHost(t, h.Koko, "web1", "--note-file", hostNoteFile(t, "Not in the table."))
	r := h.Koko("host", "ls")
	r.WantSuccess(t)
	lines := strings.Split(strings.TrimSpace(r.Stdout), "\n")
	if len(lines) != 2 || strings.Join(strings.Fields(lines[0]), " ") != "NAME SPACE STATE ADDRESS PORT ACCOUNT RELAY ID" ||
		!strings.HasPrefix(strings.Join(strings.Fields(lines[1]), " "), "web1 "+harness.Owner+" registered 203.0.113.10 22 deploy - host_") {
		t.Errorf("stdout = %q, want a header and a row for web1", r.Stdout)
	}
}

// host set changes only what it names, so two people changing different
// keys never undo each other, and prints the Host as it left it.
func TestHostSetChangesOnlyWhatItNames(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	h.Koko("user", "invite", "bob").WantSuccess(t)
	bob := h.Join("bob")
	added := addHost(t, bob.Koko, "web1", "region=fsn1", "--note-file", hostNoteFile(t, "Kept."))
	h.Advance(time.Hour)

	h.Koko("host", "set", "web1", "provider=hetzner").WantSuccess(t)
	r := bob.Koko("host", "set", "web1", "region=nbg1", "address=203.0.113.99", "port=2222", "account=admin", "--json")
	r.WantSuccess(t)
	harness.Golden(t, "host-set.json", r.Stdout)
	got := showHost(t, bob.Koko, "web1")
	want := map[string]string{"provider": "hetzner", "region": "nbg1"}
	if got.Address != "203.0.113.99" || got.Port != 2222 || got.Account != "admin" || !maps.Equal(got.Attributes, want) ||
		got.Note == nil || *got.Note != "Kept." || got.CreatedAt != added.CreatedAt || got.UpdatedAt == added.UpdatedAt {
		t.Errorf("after both sets web1 = %+v, want the new address, port and Host Account, %v, its Note, and a new updated_at", got, want)
	}
	if r := h.Koko("host", "set", "web1", "region=fsn1"); r.Stdout != "Changed Host web1 ("+added.ID+").\n" {
		t.Errorf("stdout = %q", r.Stdout)
	}
}

// --unset clears an Attribute or the Note; a key the Host lacks is already
// clear; --note-file replaces the Note.
func TestHostSetUnsetClearsAnAttributeOrTheNote(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	addHost(t, h.Koko, "web1", "region=fsn1", "provider=hetzner", "--note-file", hostNoteFile(t, "Old."))
	h.Koko("host", "set", "web1", "--unset", "region", "--unset", "cores", "--note-file", hostNoteFile(t, "New.")).WantSuccess(t)
	got := showHost(t, h.Koko, "web1")
	if !maps.Equal(got.Attributes, map[string]string{"provider": "hetzner"}) || got.Note == nil || *got.Note != "New." {
		t.Errorf("web1 = %+v, want only provider and the new Note", got)
	}
	h.Koko("host", "set", "web1", "--unset", "note").WantSuccess(t)
	if got := showHost(t, h.Koko, "web1"); got.Note != nil {
		t.Errorf("after --unset note web1 has the Note %q", *got.Note)
	}
}

// --unset port gives a Host back the port it has when none is given: 22.
func TestHostSetUnsetPortGoesBackTo22(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	addHost(t, h.Koko, "web1", "port=2222")
	if got := decodeHost(t, h.Koko("host", "set", "web1", "--unset", "port", "--json")); got.Port != 22 {
		t.Errorf("after --unset port web1 is on port %d, want 22", got.Port)
	}
}

// host set takes neither name, space nor state, which have commands of
// their own or are koko's, clears no field a Host must have, and needs
// something to change; each is refused before any call.
func TestHostSetRefusals(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	addHost(t, h.Koko, "web1", "--note-file", hostNoteFile(t, "A Note."))
	calls := h.Calls()
	r := h.Koko("host", "set", "web1", "name=web2", "--json")
	msg := r.WantError(t, 2, "invalid_usage")
	harness.Golden(t, "host-set-name.json", r.Stderr)
	if !strings.Contains(msg, "koko host rename") {
		t.Errorf("the refusal of name= says %q, want it to name koko host rename", msg)
	}
	if msg := h.Koko("host", "set", "web1", "space=ops", "--json").WantError(t, 2, "invalid_usage"); !strings.Contains(msg, "koko host move") {
		t.Errorf("the refusal of space= says %q, want it to name koko host move", msg)
	}
	for _, args := range [][]string{
		{"state=managed"},
		{"--unset", "address"},
		{"--unset", "account"},
		{"--unset", "name"},
		{"--unset", "Region"},
		{"region="},
		{},
		{"--unset", "note", "--note-file", hostNoteFile(t, "Both.")},
		{"region=eu", "--unset", "region"},
	} {
		h.Koko(append([]string{"host", "set", "web1", "--json"}, args...)...).WantError(t, 2, "invalid_usage")
	}
	if n := h.Calls() - calls; n != 0 {
		t.Errorf("the refusals made %d calls, want none", n)
	}
	if got := showHost(t, h.Koko, "web1"); got.Note == nil || len(got.Attributes) != 0 {
		t.Errorf("after the refusals web1 = %+v, want it unchanged", got)
	}
}

// noteTerminal is a terminal on stdin that nobody types into.
type noteTerminal struct{}

func (noteTerminal) ReadNoEcho(context.Context) ([]byte, error) {
	panic("nothing prompts for a Note")
}

// --note-file - reads the Note from stdin, never from a terminal there;
// --note-file <file> from a file. A Note is 1 byte to 64 KiB of text.
func TestTheNoteComesFromAFileOrStdin(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	addHost(t, h.Koko, "web1")
	alice := h.Join(harness.Owner)
	alice.KokoStdin("From stdin.\nTwo lines.\n", "host", "set", "web1", "--note-file", "-").WantSuccess(t)
	if got := showHost(t, h.Koko, "web1"); got.Note == nil || *got.Note != "From stdin.\nTwo lines.\n" {
		t.Errorf("web1's Note = %v, want what stdin held", got.Note)
	}

	calls := h.Calls()
	onTerminal := harness.Run(t, cli.Runner{SocketPath: h.Socket(), Terminal: noteTerminal{}}, []string{harness.HomeEnv(t.TempDir())},
		"host", "set", "web1", "--note-file", "-", "--json")
	onTerminal.WantError(t, 2, "invalid_usage")
	for _, note := range []string{"", strings.Repeat("a", 64<<10+1), "nul\x00byte", "\xff\xfe"} {
		h.Koko("host", "set", "web1", "--note-file", hostNoteFile(t, note), "--json").WantError(t, 2, "invalid_usage")
	}
	h.Koko("host", "set", "web1", "--note-file", filepath.Join(t.TempDir(), "missing.md"), "--json").WantError(t, 2, "invalid_usage")
	if n := h.Calls() - calls; n != 0 {
		t.Errorf("the refusals made %d calls, want none", n)
	}
	full := strings.Repeat("a", 64<<10-1) + "\n"
	h.Koko("host", "set", "web1", "--note-file", hostNoteFile(t, full)).WantSuccess(t)
	if got := showHost(t, h.Koko, "web1"); got.Note == nil || *got.Note != full {
		t.Errorf("web1's 64 KiB Note did not come back whole")
	}
}

// host rename changes the SSH Name and keeps everything else; a taken name
// is refused.
func TestHostRename(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	added := addHost(t, h.Koko, "web1", "region=fsn1")
	addHost(t, h.Koko, "web3")
	r := h.Koko("host", "rename", "web1", "web2", "--json")
	r.WantSuccess(t)
	harness.Golden(t, "host-rename.json", r.Stdout)
	if got := showHost(t, h.Koko, "web2"); got.ID != added.ID || got.Attributes["region"] != "fsn1" {
		t.Errorf("host show web2 = %+v, want web1 under its new name", got)
	}
	h.Koko("host", "show", "web1", "--json").WantError(t, 1, "unknown_host")
	h.Koko("host", "rename", "web2", "web3", "--json").WantError(t, 1, "name_taken")
	if r := h.Koko("host", "rename", "web2", "web4"); r.Stdout != "Renamed Host web2 to web4 ("+added.ID+").\n" {
		t.Errorf("stdout = %q", r.Stdout)
	}
	// The freed name can be taken again.
	addHost(t, h.Koko, "web1")
}

// A Member moves a Host between Spaces they can act in, and only those; the
// Host keeps its Attributes, Note and SSH Name. The Owner moves any Host.
func TestHostMoveNeedsBothSpaces(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	addSpaceWithMembers(t, h, "ops", "bob")
	h.Koko("space", "add", "data").WantSuccess(t)
	bob := h.Join("bob")
	added := addHost(t, bob.Koko, "web1", "region=fsn1", "--note-file", hostNoteFile(t, "Moves along."))

	r := bob.Koko("host", "move", "web1", "ops", "--json")
	r.WantSuccess(t)
	harness.Golden(t, "host-move.json", r.Stdout)
	got := showHost(t, bob.Koko, "web1")
	if got.Space != "ops" || got.ID != added.ID || got.Attributes["region"] != "fsn1" || got.Note == nil || *got.Note != "Moves along." {
		t.Errorf("after the move web1 = %+v, want it in ops with its Attributes and Note", got)
	}
	bob.Koko("host", "move", "web1", "data", "--json").WantError(t, 1, "unknown_space")
	bob.Koko("host", "move", "web1", harness.Owner, "--json").WantError(t, 1, "unknown_space")
	if r := bob.Koko("host", "move", "web1", "bob"); r.Stdout != "Moved Host web1 ("+added.ID+") to Space bob.\n" {
		t.Errorf("stdout = %q", r.Stdout)
	}
	h.Koko("host", "move", "web1", "data").WantSuccess(t)
	bob.Koko("host", "move", "web1", "bob", "--json").WantError(t, 1, "unknown_host")
	if got := showHost(t, h.Koko, "web1"); got.Space != "data" {
		t.Errorf("after the Owner's move web1 is in %s, want data", got.Space)
	}
}

// host rm deletes a Host with its Attributes and Note, prints it as it was,
// and frees its name.
func TestHostRm(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	added := addHost(t, h.Koko, "web1", "region=fsn1", "--note-file", hostNoteFile(t, "Gone soon."))
	r := h.Koko("host", "rm", "web1", "--json")
	r.WantSuccess(t)
	harness.Golden(t, "host-rm.json", r.Stdout)
	h.Koko("host", "show", "web1", "--json").WantError(t, 1, "unknown_host")
	again := addHost(t, h.Koko, "web1")
	if again.ID == added.ID || len(again.Attributes) != 0 {
		t.Errorf("the new web1 = %+v, want a Host of its own", again)
	}
	if got := showHost(t, h.Koko, "web1"); got.Note != nil {
		t.Errorf("the new web1 has the Note %q of the one removed", *got.Note)
	}
	if r := h.Koko("host", "rm", "web1"); r.Stdout != "Removed Host web1 ("+again.ID+").\n" {
		t.Errorf("stdout = %q", r.Stdout)
	}
}

// space show counts the Hosts a Space holds, and space rm refuses a Space
// while it holds one.
func TestASpaceThatHoldsAHostIsNotRemoved(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	h.Koko("space", "add", "ops").WantSuccess(t)
	addHost(t, h.Koko, "web1", "space=ops")
	addHost(t, h.Koko, "web2", "space=ops")
	addHost(t, h.Koko, "web3")
	if c := spaceShow(t, h.Koko, "ops").Counts; c.Hosts != 2 {
		t.Errorf("space show ops counts %d Hosts, want 2", c.Hosts)
	}
	msg := h.Koko("space", "rm", "ops", "--json").WantError(t, 1, "space_not_empty")
	if !strings.Contains(msg, "2 Hosts") {
		t.Errorf("the refusal says %q, want it to name the 2 Hosts", msg)
	}
	h.Koko("host", "move", "web1", harness.Owner).WantSuccess(t)
	h.Koko("host", "rm", "web2").WantSuccess(t)
	if c := spaceShow(t, h.Koko, harness.Owner).Counts; c.Hosts != 2 {
		t.Errorf("space show %s counts %d Hosts, want 2", harness.Owner, c.Hosts)
	}
	h.Koko("space", "rm", "ops").WantSuccess(t)
}
