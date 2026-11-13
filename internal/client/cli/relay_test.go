package cli_test

import (
	"encoding/json/v2"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/harness/sshhost"
	"github.com/cieons/koko/internal/shared/timing"
)

// relayedHost records the in-process Host host as the Host named hostName
// behind the Relay named relayName, as the Local caller, with pairs, and
// Enrolls it, which koko does through the Relay. It fails the test unless
// the Relay carried Enroll's connection to the Host.
func relayedHost(t *testing.T, h *harness.Harness, relayName, hostName string, relay *sshhost.Host, pairs ...string) *sshhost.Host {
	t.Helper()
	host := sshhost.Start(t, sshhost.Password(hostPassword))
	addHostAt(t, h.Koko, hostName, host, append(pairs, "relay="+relayName)...)
	enroll(t, h, hostName)
	if n := relayedTo(relay, host); n != 1 {
		t.Fatalf("the Relay carried %d connections to the Host Enroll logged in to, want 1", n)
	}
	return host
}

// relayedTo counts the direct-tcpip channels the Relay relay opened to the
// in-process Host host: the connections koko made to host through it.
func relayedTo(relay, host *sshhost.Host) int {
	to := net.JoinHostPort(host.Address(), strconv.Itoa(host.Port()))
	n := 0
	for _, r := range relay.Requests() {
		if r.Channel && r.Type == "direct-tcpip" && r.Target == to {
			n++
		}
	}
	return n
}

// relayOf reads the relay member of the one Host a host command printed
// with --json: nil for none, else its id and name, each nil when unnamed.
func relayOf(t *testing.T, r harness.Result) *struct{ ID, Name *string } {
	t.Helper()
	r.WantSuccess(t)
	var h struct {
		Relay *struct {
			ID   *string `json:"id"`
			Name *string `json:"name"`
		} `json:"relay"`
	}
	if err := json.Unmarshal([]byte(r.Stdout), &h); err != nil {
		t.Fatalf("the command printed %q: %v", r.Stdout, err)
	}
	if h.Relay == nil {
		return nil
	}
	return &struct{ ID, Name *string }{h.Relay.ID, h.Relay.Name}
}

// listedNames runs host ls --json with args on the device run runs commands
// on and returns the names it lists, in its order.
func listedNames(t *testing.T, run func(args ...string) harness.Result, args ...string) []string {
	t.Helper()
	r := run(append([]string{"host", "ls", "--json"}, args...)...)
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

// The Owner gives a Host a Relay, and koko reaches it through the Relay:
// Enroll logs in to the Host through it, and so does the Gateway for each SSH
// Session, which the koko server logs with the Relay. host show prints the
// Relay by name and id.
func TestAHostIsEnrolledAndReachedThroughItsRelay(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	relay := managedHost(t, h, "gw")
	host := relayedHost(t, h, "gw", "web1", relay)
	gwID := showHost(t, h.Koko, "gw").ID

	shown := h.Koko("host", "show", "web1", "--json")
	shown.WantSuccess(t)
	enrollGolden(t, "host-show-relay.json", host, shown.Stdout)
	people := h.Koko("host", "show", "web1")
	people.WantSuccess(t)
	if want := "Relay:       gw (" + gwID + ")\n"; !strings.Contains(people.Stdout, want) {
		t.Errorf("host show printed\n%s\nwant the line %q", people.Stdout, want)
	}

	if r := h.Koko("exec", "web1", "echo", "behind the Relay"); r.Status != 0 || r.Stdout != "behind the Relay\n" || r.Stderr != "" {
		t.Errorf("koko exec web1: exit status %d, stdout %q, stderr %q; want the echo", r.Status, r.Stdout, r.Stderr)
	}
	if n := relayedTo(relay, host); n != 2 {
		t.Errorf("the Relay carried %d connections to web1, want 2: Enroll's and the SSH Session's", n)
	}
	waitServerLog(t, h, `msg="ssh session started" .* host=web1 host_id=host_\w+ relay=gw relay_id=`+gwID+` addr=`)
}

// A Relay is set only when it can be one: a Managed Host the setter can
// see, other than the Host itself, with no Relay of its own, on a Host that
// is no Relay itself. Each refusal changes nothing; a value that names no
// Host is refused before any call. A Relay is cleared with --unset relay.
func TestARelayIsSetOnlyWhenItCanBeOne(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	relay := managedHost(t, h, "gw")
	relayedHost(t, h, "gw", "web1", relay)
	managedHost(t, h, "spare")
	addHost(t, h.Koko, "fresh")
	addHost(t, h.Koko, "web2")
	managedHost(t, h, "gone")
	goneID := showHost(t, h.Koko, "gone").ID
	h.Koko("host", "retire", "gone").WantSuccess(t)

	for _, tc := range []struct {
		args       []string
		code, says string
	}{
		{[]string{"host", "set", "web2", "relay=fresh"}, "invalid_relay", "fresh is Registered, and a Relay must be Managed: Enroll it first, with koko host enroll fresh"},
		{[]string{"host", "set", "web2", "relay=" + goneID}, "invalid_relay", "gone (" + goneID + ") is Retired, and a Retired Host is never a Relay"},
		{[]string{"host", "set", "spare", "relay=spare"}, "invalid_relay", "spare cannot be its own Relay"},
		{[]string{"host", "set", "web2", "relay=web1"}, "invalid_relay", "web1 has a Relay itself, and a Relay is exactly one hop"},
		{[]string{"host", "set", "gw", "relay=spare"}, "invalid_relay", "gw is the Relay of a Host that is not Retired"},
		{[]string{"host", "set", "web2", "relay=nowhere"}, "unknown_host", `the Relay of web2: no Host is named "nowhere"`},
		{[]string{"host", "add", "web3", "address=203.0.113.3", "account=deploy", "relay=fresh"}, "invalid_relay", "fresh is Registered"},
	} {
		msg := h.Koko(append(tc.args, "--json")...).WantError(t, 1, tc.code)
		if !strings.Contains(msg, tc.says) {
			t.Errorf("%s: the refusal says %q, want it to say %q", strings.Join(tc.args, " "), msg, tc.says)
		}
	}
	if r := relayOf(t, h.Koko("host", "show", "web2", "--json")); r != nil {
		t.Errorf("after the refusals web2 has the Relay %+v, want none", r)
	}
	if names := listedNames(t, h.Koko, "name=web3"); len(names) != 0 {
		t.Errorf("a refused host add recorded %v", names)
	}

	calls := h.Calls()
	h.Koko("host", "set", "web2", "relay=", "--json").WantError(t, 2, "invalid_usage")
	h.Koko("host", "set", "web2", "relay=Spare", "--json").WantError(t, 2, "invalid_usage")
	if n := h.Calls() - calls; n != 0 {
		t.Errorf("relay= naming no Host made %d calls, want none", n)
	}

	spareID := showHost(t, h.Koko, "spare").ID
	if r := relayOf(t, h.Koko("host", "set", "web2", "relay="+spareID, "--json")); r == nil || *r.Name != "spare" || *r.ID != spareID {
		t.Errorf("host set web2 relay=<spare's id> printed the Relay %+v, want spare", r)
	}
	if r := relayOf(t, h.Koko("host", "set", "web2", "--unset", "relay", "--json")); r != nil {
		t.Errorf("host set web2 --unset relay printed the Relay %+v, want none", r)
	}
}

// A Relay is a link to a Host's id: renaming the Relay or moving either
// Host keeps it. A Member who cannot see the Relay sees that the Host has
// one, unnamed, in host show, host ls and their --json, and reaches the
// Host through it; relay.has finds it, and no filter on the Relay's name
// does. A Member sets a Relay in another Space they can see, and not one
// they cannot.
func TestARelayKeepsItsLinkAndIsUnnamedToWhoCannotSeeIt(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	addSpaceWithMembers(t, h, "ops", "bob")
	laptop := h.Join("bob")
	relay := managedHost(t, h, "gw")
	host := relayedHost(t, h, "gw", "web1", relay, "space=ops")
	addHost(t, h.Koko, "web2", "space=ops")

	h.Koko("host", "rename", "gw", "edge").WantSuccess(t)
	h.Koko("space", "add", "vault").WantSuccess(t)
	h.Koko("host", "move", "edge", "vault").WantSuccess(t)
	h.Koko("host", "move", "web1", "vault").WantSuccess(t)
	h.Koko("host", "move", "web1", "ops").WantSuccess(t)
	if r := relayOf(t, h.Koko("host", "show", "web1", "--json")); r == nil || r.Name == nil || *r.Name != "edge" {
		t.Errorf("after the Relay's rename and the moves web1 shows the Relay %+v, want edge", r)
	}
	if got := listedNames(t, h.Koko, "relay=edge"); !slices.Equal(got, []string{"web1"}) {
		t.Errorf("the Owner's host ls relay=edge lists %v, want [web1]", got)
	}

	unseen := laptop.Koko("host", "show", "web1", "--json")
	unseen.WantSuccess(t)
	enrollGolden(t, "host-show-relay-unseen.json", host, unseen.Stdout)
	people := laptop.Koko("host", "show", "web1")
	people.WantSuccess(t)
	if !strings.Contains(people.Stdout, "Relay:       one you cannot see\n") || strings.Contains(people.Stdout, "edge") {
		t.Errorf("bob's host show printed\n%s\nwant the Relay unnamed", people.Stdout)
	}
	listed := laptop.Koko("host", "ls", "--json", "name=web1")
	listed.WantSuccess(t)
	if !strings.Contains(listed.Stdout, `"relay": {
      "id": null,
      "name": null
    },`) {
		t.Errorf("bob's host ls --json printed\n%s\nwant web1's Relay with a null id and name", listed.Stdout)
	}
	table := laptop.Koko("host", "ls")
	table.WantSuccess(t)
	if !strings.Contains(table.Stdout, " (unseen) ") || strings.Contains(table.Stdout, "edge") {
		t.Errorf("bob's host ls printed\n%s\nwant web1's Relay as (unseen)", table.Stdout)
	}
	for _, tc := range []struct {
		filter string
		want   []string
	}{
		{"relay.has", []string{"web1"}},
		{"relay.lacks", []string{"web2"}},
		{"relay=edge", []string{}},
	} {
		if got := listedNames(t, laptop.Koko, tc.filter); !slices.Equal(got, tc.want) {
			t.Errorf("bob's host ls %s lists %v, want %v", tc.filter, got, tc.want)
		}
	}
	if r := laptop.Koko("exec", "web1", "echo", "through its Relay"); r.Status != 0 || r.Stdout != "through its Relay\n" {
		t.Errorf("bob's koko exec web1: exit status %d, stdout %q, stderr %q; want the echo", r.Status, r.Stdout, r.Stderr)
	}

	msg := laptop.Koko("host", "set", "web2", "relay=edge", "--json").WantError(t, 1, "unknown_host")
	if want := `the Relay of web2: no Host is named "edge"`; msg != want {
		t.Errorf("bob setting a Relay he cannot see is refused with %q, want %q", msg, want)
	}
	addSpaceWithMembers(t, h, "dev")
	h.Koko("space", "member", "add", "dev", "bob").WantSuccess(t)
	managedHostIn(t, h, "jump", "dev")
	if r := relayOf(t, laptop.Koko("host", "set", "web2", "relay=jump", "--json")); r == nil || r.Name == nil || *r.Name != "jump" {
		t.Errorf("bob's host set web2 relay=jump printed the Relay %+v, want jump, in a Space he can see", r)
	}
}

// A Relay that a Host that is not Retired uses is not Retired: the refusal
// names no Host. Once its Host has another Relay, none, or is Retired
// itself, the Relay retires; a Retired Host takes no new Relay.
func TestRetiringARelayInUseIsRefused(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	relay := managedHost(t, h, "gw")
	relayedHost(t, h, "gw", "web1", relay)
	relayedHost(t, h, "gw", "web2", relay)

	msg := h.Koko("host", "retire", "gw", "--json").WantError(t, 1, "relay_in_use")
	if strings.Contains(msg, "web1") || strings.Contains(msg, "web2") || !strings.Contains(msg, "koko host ls relay=gw") {
		t.Errorf("the refusal says %q, want it to name no Host that uses gw, and the list that finds them", msg)
	}
	h.Koko("host", "set", "web1", "--unset", "relay").WantSuccess(t)
	h.Koko("host", "retire", "gw", "--json").WantError(t, 1, "relay_in_use")
	h.Koko("host", "retire", "web2").WantSuccess(t)
	web2 := retiredID(t, h, "web2")
	h.Koko("host", "set", web2, "relay=web1", "--json").WantError(t, 1, "host_retired")
	h.Koko("host", "retire", "gw").WantSuccess(t)
}

// retiredID returns the id of the Host that was named hostName, Retired or
// not, as host ls --all lists it.
func retiredID(t *testing.T, h *harness.Harness, hostName string) string {
	t.Helper()
	r := h.Koko("host", "ls", "--all", "--json", "name="+hostName)
	r.WantSuccess(t)
	var listed []printedHost
	if err := json.Unmarshal([]byte(r.Stdout), &listed); err != nil || len(listed) != 1 {
		t.Fatalf("host ls --all name=%s printed %q (%v), want one Host", hostName, r.Stdout, err)
	}
	return listed[0].ID
}

// The Gateway refuses an SSH Session it cannot pass through the Relay for,
// as it refuses one to a Host: a Relay that is down, whose host key is not
// the pinned one, that refuses the koko key, or that is not Managed. Each
// banner names the Relay to the Owner, who can see it, and only "its Relay"
// to a Member who cannot, with neither its fingerprints nor its Host
// Account. Enroll refuses the same ways.
func TestTheGatewaySaysWhyItCouldNotPassThroughTheRelay(t *testing.T) {
	t.Parallel()
	h := harness.Start(t, harness.Timings(timing.Timings{HostConnectWait: time.Millisecond}))
	addSpaceWithMembers(t, h, "ops", "bob")
	laptop := h.Join("bob")
	relay := managedHost(t, h, "gw")
	relayedHost(t, h, "gw", "web1", relay, "space=ops")
	gwPort := strconv.Itoa(relay.Port())

	rebuilt := sshhost.Start(t, sshhost.Password(hostPassword))
	h.Koko("host", "set", "gw", "port="+strconv.Itoa(rebuilt.Port())).WantSuccess(t)
	wantRefused(t, h.Koko("exec", "web1", "true"), fmt.Sprintf(
		"koko: the host key of web1's Relay gw does not match the one pinned at Enroll (pinned %s, offered %s); koko did not log in. "+
			"If gw was rebuilt, pin its new key with koko host enroll gw --host-key <the fingerprint its provider shows>, "+
			"or with --replace-host-key, which trusts whatever key it offers.\n", fingerprint(relay), fingerprint(rebuilt)))
	wantRefused(t, laptop.Koko("exec", "web1", "true"),
		"koko: the host key of web1's Relay does not match the one pinned at Enroll; koko did not log in.\n")
	waitServerLog(t, h, `level=WARN msg="ssh session refused" .* name=web1 .* reason="relay host key mismatch" relay_id=host_\w+ pinned=`)
	msg := h.KokoStdin(hostPassword, "host", "enroll", "web1", "--password-stdin", "--replace-host-key", "--json").WantError(t, 1, "host_unreachable")
	if !strings.Contains(msg, "the host key of web1's Relay gw does not match the one pinned at Enroll (pinned") {
		t.Errorf("Enroll through a Relay whose host key changed is refused with %q, want the Relay's mismatch", msg)
	}

	_, deadPort, err := net.SplitHostPort(harness.FreeAddr(t))
	if err != nil {
		t.Fatal(err)
	}
	h.Koko("host", "set", "gw", "port="+deadPort).WantSuccess(t)
	wantRefused(t, h.Koko("exec", "web1", "true"),
		"koko: web1's Relay gw is down or unreachable: koko could not connect to it; this SSH Session did not start.\n")
	wantRefused(t, laptop.Koko("exec", "web1", "true"),
		"koko: web1's Relay is down or unreachable: koko could not connect to it; this SSH Session did not start.\n")
	msg = laptop.KokoStdin(hostPassword, "host", "enroll", "web1", "--password-stdin", "--replace-host-key", "--json").WantError(t, 1, "host_unreachable")
	if want := "koko could not reach web1 through its Relay, so it did not log in to web1: web1's Relay is down or unreachable"; msg != want {
		t.Errorf("bob's Enroll through a Relay that is down is refused with %q, want %q", msg, want)
	}
	waitServerLog(t, h, `msg="enroll failed" host=web1 .* code=host_unreachable .* relay=gw relay_id=host_\w+ cause=`)

	h.Koko("host", "set", "gw", "port="+gwPort).WantSuccess(t)
	if err := os.WriteFile(filepath.Join(relay.Home(), ".ssh", "authorized_keys"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	wantRefused(t, h.Koko("exec", "web1", "true"),
		"koko: web1's Relay gw refused the koko key for its Host Account \"deploy\"; this SSH Session did not start.\n")
	wantRefused(t, laptop.Koko("exec", "web1", "true"),
		"koko: web1's Relay refused the koko key; this SSH Session did not start.\n")
	msg = laptop.KokoStdin(hostPassword, "host", "enroll", "web1", "--password-stdin", "--replace-host-key", "--json").WantError(t, 1, "host_unreachable")
	if want := "koko could not reach web1 through its Relay, so it did not log in to web1: web1's Relay refused the koko key"; msg != want {
		t.Errorf("bob's Enroll through a Relay that refused the koko key is refused with %q, want %q", msg, want)
	}

	h.Koko("host", "set", "gw", "account=ops").WantSuccess(t)
	logins := len(relay.Logins())
	wantRefused(t, h.Koko("exec", "web1", "true"),
		"koko: web1's Relay gw is Registered: koko passes through it only once it is Enrolled, with koko host enroll gw.\n")
	wantRefused(t, laptop.Koko("exec", "web1", "true"),
		"koko: web1's Relay is Registered: koko passes through it only once it is Enrolled.\n")
	waitServerLog(t, h, `msg="ssh session refused" .* name=web1 .* reason="relay registered"\n`)
	msg = h.KokoStdin(hostPassword, "host", "enroll", "web1", "--password-stdin", "--replace-host-key", "--json").WantError(t, 1, "invalid_relay")
	if !strings.Contains(msg, "web1's Relay gw is not Managed") {
		t.Errorf("Enroll through a Registered Relay is refused with %q, want it to say so", msg)
	}
	if n := len(relay.Logins()) - logins; n != 0 {
		t.Errorf("a Registered Relay got %d logins, want none", n)
	}
}

// A Relay that stops being Managed ends every live SSH Session through it,
// with a line that names the Relay only to whoever can see it; one whose
// Relay closes its connection ends with a line naming the Relay.
func TestARelayThatStopsBeingManagedEndsTheSSHSessionsThroughIt(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	addSpaceWithMembers(t, h, "ops", "bob")
	laptop := h.Join("bob")
	relay := managedHost(t, h, "gw")
	relayedHost(t, h, "gw", "web1", relay, "space=ops")
	members := openLiveSSH(t, h, laptop.Proxy(nil, "tunnel", "web1"), "web1")
	owners := openLiveSSH(t, h, h.Proxy("tunnel", "web1"), "web1")

	h.Koko("host", "set", "gw", "account=ops").WantSuccess(t)
	members.wantEnded("koko: access to web1 was withdrawn: web1's Relay is no longer Managed; this SSH Session is closed.\n")
	owners.wantEnded("koko: access to web1 was withdrawn: web1's Relay gw is no longer Managed; this SSH Session is closed.\n")
	waitServerLog(t, h, `msg="ssh session ended" .* reason="relay registered"\n`)

	h.Koko("host", "set", "gw", "account="+sshhost.Account).WantSuccess(t)
	enroll(t, h, "gw")
	p := h.Background(nil, "exec", "web1", "echo ready; exec sleep 30")
	p.WaitStdout("ready")
	relay.DropConns()
	const line = "koko: web1's Relay gw closed the connection.\n"
	if r := p.Wait(); r.Status != 255 || r.Stderr != line {
		t.Errorf("koko exec: exit status %d, stderr %q; want 255 and %q", r.Status, r.Stderr, line)
	}
	waitServerLog(t, h, `msg="ssh session ended" .* reason="Relay closed"\n`)
}

// A live SSH Session passes through the Relay it started through: a new
// Relay for its Host keeps it, while both that Relay and the Host's new one
// are Managed. The old Relay, once no Host uses it, may be Retired, which
// ends the SSH Session through it; the Host's new Relay going back to
// Registered ends one through another.
func TestAnSSHSessionGoesOnThroughTheRelayItStartedThrough(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	relay := managedHost(t, h, "gw")
	relayedHost(t, h, "gw", "web1", relay)
	managedHost(t, h, "gw2")
	managedHost(t, h, "gw3")
	s := openLiveSSH(t, h, h.Proxy("tunnel", "web1"), "web1")

	h.Koko("host", "set", "web1", "relay=gw2").WantSuccess(t)
	s.wantOpen("after a new Relay")
	h.Koko("host", "retire", "gw").WantSuccess(t)
	s.wantEnded("koko: access to web1 was withdrawn: web1's Relay gw is no longer Managed; this SSH Session is closed.\n")
	waitServerLog(t, h, `msg="ssh session ended" .* relay=gw .* reason="relay retired"\n`)

	s = openLiveSSH(t, h, h.Proxy("tunnel", "web1"), "web1")
	h.Koko("host", "set", "web1", "relay=gw3").WantSuccess(t)
	h.Koko("host", "set", "gw3", "account=ops").WantSuccess(t)
	s.wantEnded("koko: access to web1 was withdrawn: web1's Relay gw3 is no longer Managed; this SSH Session is closed.\n")
}

// An SSH Session that started with no Relay is checked again when its
// Host's Relay, given since, goes back to Registered, though the change
// names neither its Host nor a Relay it passes through: a new SSH Session
// to the Host would be refused, so it ends.
func TestAnSSHSessionStartedWithoutARelayEndsWhenItsHostsNewRelayStopsBeingManaged(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	managedHost(t, h, "app1")
	managedHost(t, h, "db1")
	s := openLiveSSH(t, h, h.Proxy("tunnel", "app1"), "app1")

	h.Koko("host", "set", "app1", "relay=db1").WantSuccess(t)
	s.wantOpen("after a Relay")
	h.Koko("host", "set", "db1", "account=ops").WantSuccess(t)
	s.wantEnded("koko: access to app1 was withdrawn: app1's Relay db1 is no longer Managed; this SSH Session is closed.\n")
	waitServerLog(t, h, `msg="ssh session ended" .* host=app1 .* reason="relay registered"\n`)
}

// Enroll records nothing when the Host's Relay changes while it logs in to
// the Host through the old one.
func enrollRecordsNothingWhenTheRelayChangesMeanwhile(t *testing.T) {
	h := harness.Start(t)
	managedHost(t, h, "gw")
	managedHost(t, h, "gw2")
	host := sshhost.Start(t, sshhost.Password(hostPassword), sshhost.SlowLogins(500*time.Millisecond))
	addHostAt(t, h.Koko, "web1", host, "relay=gw")
	done := inBackground(t, func() harness.Result {
		return h.KokoStdin(hostPassword, "host", "enroll", "web1", "--password-stdin", "--json")
	})
	eventually(t, "the koko server connects to the Host", func() bool { return host.Conns() == 1 })
	h.Koko("host", "set", "web1", "relay=gw2").WantSuccess(t)
	if msg := (<-done).WantError(t, 1, "host_changed"); !strings.Contains(msg, "its Relay") {
		t.Errorf("the refusal %q does not say the Relay may have changed", msg)
	}
	wantPinned(t, showHost(t, h.Koko, "web1"), "registered", "")
}

// koko host add reads a Relay on stdin as host ls --json prints it, by its
// id or its name, and refuses before any call one printed unnamed, which
// it cannot set.
func TestHostAddOnStdinTakesARelay(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	managedHost(t, h, "gw")
	gwID := showHost(t, h.Koko, "gw").ID

	in := `[{"name": "web1", "address": "203.0.113.1", "account": "deploy", "relay": {"id": "` + gwID + `", "name": "old"}},
		{"name": "web2", "address": "203.0.113.2", "account": "deploy", "relay": {"id": null, "name": "gw"}}]`
	h.KokoStdin(in, "host", "add").WantSuccess(t)
	for _, name := range []string{"web1", "web2"} {
		if r := relayOf(t, h.Koko("host", "show", name, "--json")); r == nil || *r.ID != gwID {
			t.Errorf("%s came in with the Relay %+v, want gw", name, r)
		}
	}

	addHost(t, h.Koko, "fresh")
	msg := h.KokoStdin(`[{"name": "web3", "address": "203.0.113.3", "account": "deploy", "relay": {"id": null, "name": "gw"}},
		{"name": "web4", "address": "203.0.113.4", "account": "deploy", "relay": {"id": null, "name": "fresh"}}]`,
		"host", "add", "--json").WantError(t, 1, "invalid_relay")
	if !strings.Contains(msg, "\n[1] fresh is Registered") {
		t.Errorf("a batch with a Registered Relay is refused with %q, want the Host at fault listed", msg)
	}

	calls := h.Calls()
	msg = h.KokoStdin(`{"name": "web3", "address": "203.0.113.3", "account": "deploy", "relay": {"id": null, "name": null}}`,
		"host", "add", "--json").WantError(t, 2, "invalid_usage")
	if !regexp.MustCompile(`^the Host web3: "relay" names no Relay`).MatchString(msg) {
		t.Errorf("an unnamed Relay on stdin is refused with %q", msg)
	}
	h.KokoStdin(`{"name": "web3", "address": "203.0.113.3", "account": "deploy", "relay": "gw"}`,
		"host", "add", "--json").WantError(t, 2, "invalid_usage")
	if n := h.Calls() - calls; n != 0 {
		t.Errorf("the refusals made %d calls, want none", n)
	}
}
