package cli_test

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/harness/sshhost"
	"github.com/cieons/koko/internal/server/store"
	"github.com/cieons/koko/internal/shared/timing"
)

// relayChain is the Host app1 behind a chain of two Relays, web1 then db1,
// each an in-process Host.
type relayChain struct {
	web1, db1, app1 *sshhost.Host
}

// startRelayChain records web1, db1 with web1 as its Relay, and app1 with
// db1 as its Relay, as the Local caller, each in the Space spaces names for
// it or the caller's Personal Space, and Enrolls each, which koko does
// through the Relays before it. It fails the test unless each Relay carried
// Enroll's connections to the next hop, and only those.
func startRelayChain(t *testing.T, h *harness.Harness, spaces map[string]string, opts ...sshhost.Option) relayChain {
	t.Helper()
	add := func(hostName string, pairs ...string) *sshhost.Host {
		host := sshhost.Start(t, append([]sshhost.Option{sshhost.Password(hostPassword)}, opts...)...)
		if s, ok := spaces[hostName]; ok {
			pairs = append(pairs, "space="+s)
		}
		addHostAt(t, h.Koko, hostName, host, pairs...)
		enroll(t, h, hostName)
		return host
	}
	c := relayChain{web1: add("web1")}
	c.db1 = add("db1", "relay=web1")
	c.app1 = add("app1", "relay=db1")
	if web1, db1, past := relayedTo(c.web1, c.db1), relayedTo(c.db1, c.app1), relayedTo(c.web1, c.app1); web1 != 2 || db1 != 1 || past != 0 {
		t.Fatalf("Enroll's connections went web1 to db1 %d times, db1 to app1 %d times and web1 to app1 %d times; want 2, 1 and 0", web1, db1, past)
	}
	return c
}

// inviteTo invites each User of spaces and makes them a Member of each Space
// it names for them, adding each Space not yet made.
func inviteTo(t *testing.T, h *harness.Harness, spaces map[string][]string) {
	t.Helper()
	made := map[string]bool{}
	for user, of := range spaces {
		h.Koko("user", "invite", user).WantSuccess(t)
		for _, s := range of {
			if !made[s] {
				h.Koko("space", "add", s).WantSuccess(t)
				made[s] = true
			}
			h.Koko("space", "member", "add", s, user).WantSuccess(t)
		}
	}
}

// wantRefusal fails the test unless r failed with code and a message that
// holds says.
func wantRefusal(t *testing.T, r harness.Result, code, says string) {
	t.Helper()
	if msg := r.WantError(t, 1, code); !strings.Contains(msg, says) {
		t.Errorf("the refusal says %q, want it to say %q", msg, says)
	}
}

// A Relay may have a Relay of its own, given with relay= one Host at a
// time, also once Hosts sit behind it, as long as no chain loops and koko
// passes through at most 4 Relays to reach any Host that is not Retired.
// The refusals name only the two Hosts of the write, and no count; each
// changes nothing. Clearing a Relay is never refused. Retiring a Relay in
// the middle is refused, and the whole chain retires at once; a rename or a
// move keeps every link.
func TestARelayMayHaveARelayOfItsOwn(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	for _, name := range []string{"r1", "r2", "r3", "r4", "r5", "app"} {
		managedHost(t, h, name)
	}

	// app behind r4, then r4 behind r3: a Relay with a Host behind it
	// takes a Relay, and app is two deep.
	h.Koko("host", "set", "app", "relay=r4").WantSuccess(t)
	h.Koko("host", "set", "r4", "relay=r3").WantSuccess(t)
	h.Koko("host", "set", "r3", "relay=r2").WantSuccess(t)
	h.Koko("host", "set", "r2", "relay=r1").WantSuccess(t)
	if r := relayOf(t, h.Koko("host", "show", "app", "--json")); r == nil || *r.Name != "r4" {
		t.Errorf("app shows the Relay %+v, want only its own, r4", r)
	}
	if r, want := h.Koko("host", "show", "app"), "\nRelay:       r4 ("+showHost(t, h.Koko, "r4").ID+"), through r1, r2, r3\n"; !strings.Contains(r.Stdout, want) {
		t.Errorf("host show app printed\n%s\nwant the line %q", r.Stdout, strings.TrimSpace(want))
	}

	for _, tc := range []struct {
		args []string
		says string
	}{
		{[]string{"host", "set", "r1", "relay=r2"}, "r1 cannot take r2 as its Relay: koko reaches r2 through r1, and a chain of Relays never loops"},
		{[]string{"host", "set", "r1", "relay=r4"}, "r1 cannot take r4 as its Relay: koko reaches r4 through r1, and a chain of Relays never loops"},
		{[]string{"host", "set", "r5", "relay=app"}, "r5 cannot take app as its Relay: koko would pass through more than 4 Relays to reach r5, the most it passes through"},
		{[]string{"host", "set", "r1", "relay=r5"}, "r1 cannot take r5 as its Relay: koko would pass through more than 4 Relays to reach a Host behind r1; koko host ls relay=r1 lists the Hosts you can see that use r1 as their Relay"},
		{[]string{"host", "add", "new2", "address=203.0.113.2", "account=deploy", "relay=app"}, "new2 cannot take app as its Relay: koko would pass through more than 4 Relays to reach new2"},
	} {
		wantRefusal(t, h.Koko(append(tc.args, "--json")...), "invalid_relay", tc.says)
	}
	for _, name := range []string{"r1", "r5"} {
		if r := relayOf(t, h.Koko("host", "show", name, "--json")); r != nil {
			t.Errorf("after the refusals %s has the Relay %+v, want none", name, r)
		}
	}
	h.KokoStdin(`{"name": "new1", "address": "203.0.113.1", "account": "deploy", "relay": {"id": null, "name": "r4"}}`, "host", "add").WantSuccess(t)

	h.Koko("host", "set", "r3", "--unset", "relay").WantSuccess(t)
	h.Koko("host", "set", "r3", "relay=r2").WantSuccess(t)

	h.Koko("host", "rename", "r2", "mid").WantSuccess(t)
	h.Koko("space", "add", "vault").WantSuccess(t)
	h.Koko("host", "move", "r3", "vault").WantSuccess(t)
	if r := relayOf(t, h.Koko("host", "show", "r3", "--json")); r == nil || *r.Name != "mid" {
		t.Errorf("after the rename and the move r3 shows the Relay %+v, want mid", r)
	}
	wantRefusal(t, h.Koko("host", "retire", "mid", "--json"), "relay_in_use", "koko host ls relay=mid")
	h.Koko("host", "retire", "r1", "mid", "r3", "r4", "app", "new1").WantSuccess(t)
}

// A batch through koko host set --where gives every Host it names the new
// Relay R, all or none, checked against the links as they were before it:
// each Host whose chain would loop, or which would put a Host behind it
// past 4 Relays, is listed with why, whatever its place, and a Host of the
// batch counts as behind none other, since it takes R too.
func TestABatchOfRelaysIsCheckedAsItWouldLeaveTheChains(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	for _, name := range []string{"r1", "r2", "r3", "r4", "spare", "y"} {
		managedHost(t, h, name)
	}
	h.Koko("host", "set", "r2", "relay=r1").WantSuccess(t)
	h.Koko("host", "set", "r3", "relay=r2").WantSuccess(t)
	h.Koko("host", "set", "r4", "relay=r3").WantSuccess(t)
	addHost(t, h.Koko, "yy", "relay=y")
	for _, name := range []string{"r1", "spare", "y", "yy"} {
		h.Koko("host", "set", name, "batch=a").WantSuccess(t)
	}

	// r4's chain is r1, r2, r3: r1 would loop, and yy, behind y, would be
	// 5 deep, unless yy takes r4 too.
	msg := h.Koko("host", "set", "--where", "batch=a", "--where", "name!=yy", "relay=r4", "--json").WantError(t, 1, "invalid_relay")
	if want := "none of the 3 Hosts was changed, for the 2 at fault:" +
		"\n[0] r1 cannot take r4 as its Relay: koko reaches r4 through r1, and a chain of Relays never loops" +
		"\n[2] y cannot take r4 as its Relay: koko would pass through more than 4 Relays to reach a Host behind y; koko host ls relay=y lists the Hosts you can see that use y as their Relay"; msg != want {
		t.Errorf("the batch is refused with\n%s\nwant\n%s", msg, want)
	}
	if r := relayOf(t, h.Koko("host", "show", "spare", "--json")); r != nil {
		t.Errorf("a refused batch gave spare the Relay %+v", r)
	}
	h.Koko("host", "set", "--where", "batch=a", "--where", "name!=r1", "relay=r4", "--json").WantSuccess(t)
	for _, name := range []string{"spare", "y", "yy"} {
		if r := relayOf(t, h.Koko("host", "show", name, "--json")); r == nil || *r.Name != "r4" {
			t.Errorf("after the batch %s has the Relay %+v, want r4", name, r)
		}
	}
}

// The Gateway reaches a Host through a chain of two Relays, logging in to
// each with the koko key, through ssh with koko tunnel and koko exec, as
// the Owner and as Members who see none, one or all of the Relays; the
// started line names the Host's own Relay and the whole chain. A Member
// sets a Relay they see whose own Relay they cannot see.
func TestTheGatewayReachesAHostThroughAChain(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	inviteTo(t, h, map[string][]string{"carol": {"apps"}, "bob": {"apps", "edge"}, "dave": {"apps", "data"}, "erin": {"apps", "edge", "data"}})
	c := startRelayChain(t, h, map[string]string{"web1": "edge", "db1": "data", "app1": "apps"})
	web1ID, db1ID := showHost(t, h.Koko, "web1").ID, showHost(t, h.Koko, "db1").ID
	shown := h.Koko("host", "show", "app1", "--json")
	shown.WantSuccess(t)
	enrollGolden(t, "host-show-chain.json", c.app1, shown.Stdout)

	carol, bob, dave, erin := h.Join("carol"), h.Join("bob"), h.Join("dave"), h.Join("erin")
	db1 := "db1 (" + db1ID + ")"
	for _, tc := range []struct {
		who   string
		run   func(args ...string) harness.Result
		relay string
	}{
		{"alice", h.Koko, db1 + ", through web1"},
		{"carol", carol.Koko, "one you cannot see"},
		{"bob", bob.Koko, "one you cannot see"},
		{"dave", dave.Koko, db1 + ", through one you cannot see"},
		{"erin", erin.Koko, db1 + ", through web1"},
	} {
		r := tc.run("host", "show", "app1")
		r.WantSuccess(t)
		if want := "\nRelay:       " + tc.relay + "\n"; !strings.Contains(r.Stdout, want) {
			t.Errorf("%s's host show app1 printed\n%s\nwant the line %q", tc.who, r.Stdout, strings.TrimSpace(want))
		}
	}

	sessions := 0
	for _, run := range []func(args ...string) harness.Result{h.Koko, carol.Koko, bob.Koko, dave.Koko, erin.Koko} {
		if r := run("exec", "app1", "echo", "two networks deep"); r.Status != 0 || r.Stdout != "two networks deep\n" || r.Stderr != "" {
			t.Errorf("koko exec app1: exit status %d, stdout %q, stderr %q; want the echo", r.Status, r.Stdout, r.Stderr)
		}
		sessions++
	}
	waitServerLog(t, h, `msg="ssh session started" .* user=alice .* host=app1 host_id=host_\w+ relay=db1 relay_id=`+db1ID+
		` relays=web1,db1 relay_ids=`+web1ID+`,`+db1ID+` addr=`)
	openLiveSSH(t, h, carol.Proxy(nil, "tunnel", "app1"), "app1")
	openLiveSSH(t, h, h.Proxy("tunnel", "app1"), "app1")
	sessions += 2
	if web1, db1 := relayedTo(c.web1, c.db1), relayedTo(c.db1, c.app1); web1 != 2+sessions || db1 != 1+sessions {
		t.Errorf("web1 carried %d connections to db1 and db1 %d to app1, want %d and %d: Enroll's and one for each SSH Session", web1, db1, 2+sessions, 1+sessions)
	}

	addHost(t, h.Koko, "app2", "space=apps")
	if r := relayOf(t, dave.Koko("host", "set", "app2", "relay=db1", "--json")); r == nil || *r.Name != "db1" {
		t.Errorf("dave's host set app2 relay=db1 printed the Relay %+v, want db1, whose own Relay he cannot see", r)
	}
}

// The Gateway refuses an SSH Session it cannot pass through a Relay of the
// chain for, at either hop: a Relay that is not Managed, the one nearest the
// koko server first, is down, offers another host key or refuses the koko
// key. Each banner names the Relay only to a caller who can see it and every
// Relay between it and the Host, so bob, who sees web1 but not db1, reads
// "app1's Relay" for both; the log names the Relay and its hop from 1.
// Enroll refuses the same ways, before any login for a Relay that is not
// Managed.
func TestTheGatewaySaysWhichRelayOfTheChainItCouldNotPassThrough(t *testing.T) {
	t.Parallel()
	h := harness.Start(t, harness.Timings(timing.Timings{HostConnectWait: time.Millisecond}))
	inviteTo(t, h, map[string][]string{"bob": {"apps", "edge"}, "dave": {"apps", "data"}})
	c := startRelayChain(t, h, map[string]string{"web1": "edge", "db1": "data", "app1": "apps"})
	bob, dave := h.Join("bob"), h.Join("dave")
	_, deadPort, err := net.SplitHostPort(harness.FreeAddr(t))
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name  string
		relay *sshhost.Host
		hop   string
		// daveSees says whether dave can follow the links to it.
		daveSees bool
	}{
		{"web1", c.web1, "1", false},
		{"db1", c.db1, "2", true},
	} {
		id := showHost(t, h.Koko, tc.name).ID
		port := strconv.Itoa(tc.relay.Port())
		logged := `relay=` + tc.name + ` relay_id=` + id + ` hop=` + tc.hop

		h.Koko("host", "set", tc.name, "account=ops").WantSuccess(t)
		logins := len(tc.relay.Logins()) + len(c.app1.Logins())
		wantRefused(t, h.Koko("exec", "app1", "true"),
			"koko: app1's Relay "+tc.name+" is Registered: koko passes through it only once it is Enrolled, with koko host enroll "+tc.name+".\n")
		wantRefused(t, bob.Koko("exec", "app1", "true"), "koko: app1's Relay is Registered: koko passes through it only once it is Enrolled.\n")
		daves := "koko: app1's Relay is Registered: koko passes through it only once it is Enrolled.\n"
		if tc.daveSees {
			daves = "koko: app1's Relay " + tc.name + " is Registered: koko passes through it only once it is Enrolled, with koko host enroll " + tc.name + ".\n"
		}
		wantRefused(t, dave.Koko("exec", "app1", "true"), daves)
		waitServerLog(t, h, `msg="ssh session refused" .* name=app1 .* reason="relay registered" `+logged+`\n`)
		wantRefusal(t, h.KokoStdin(hostPassword, "host", "enroll", "app1", "--password-stdin", "--replace-host-key", "--json"),
			"invalid_relay", "app1's Relay "+tc.name+" is not Managed")
		if n := len(tc.relay.Logins()) + len(c.app1.Logins()) - logins; n != 0 {
			t.Errorf("with %s Registered, the Relay and app1 got %d logins, want none", tc.name, n)
		}
		h.Koko("host", "set", tc.name, "account="+sshhost.Account).WantSuccess(t)
		enroll(t, h, tc.name)

		h.Koko("host", "set", tc.name, "port="+deadPort).WantSuccess(t)
		wantRefused(t, h.Koko("exec", "app1", "true"),
			"koko: app1's Relay "+tc.name+" is down or unreachable: koko could not connect to it; this SSH Session did not start.\n")
		wantRefused(t, bob.Koko("exec", "app1", "true"),
			"koko: app1's Relay is down or unreachable: koko could not connect to it; this SSH Session did not start.\n")
		waitServerLog(t, h, `level=WARN msg="ssh session refused" .* name=app1 .* reason="relay unreachable" `+logged+` err=`)
		wantRefusal(t, h.KokoStdin(hostPassword, "host", "enroll", "app1", "--password-stdin", "--replace-host-key", "--json"),
			"host_unreachable", "koko could not reach app1 through its Relay, so it did not log in to app1: app1's Relay "+tc.name+" is down or unreachable")
		waitServerLog(t, h, `msg="enroll failed" host=app1 .* code=host_unreachable .* `+logged+` cause=`)

		rebuilt := sshhost.Start(t, sshhost.Password(hostPassword))
		h.Koko("host", "set", tc.name, "port="+strconv.Itoa(rebuilt.Port())).WantSuccess(t)
		wantRefused(t, h.Koko("exec", "app1", "true"), fmt.Sprintf(
			"koko: the host key of app1's Relay %s does not match the one pinned at Enroll (pinned %s, offered %s); koko did not log in. "+
				"If %s was rebuilt, pin its new key with koko host enroll %s --host-key <the fingerprint its provider shows>, "+
				"or with --replace-host-key, which trusts whatever key it offers.\n", tc.name, fingerprint(tc.relay), fingerprint(rebuilt), tc.name, tc.name))
		wantRefused(t, bob.Koko("exec", "app1", "true"),
			"koko: the host key of app1's Relay does not match the one pinned at Enroll; koko did not log in.\n")
		waitServerLog(t, h, `msg="ssh session refused" .* name=app1 .* reason="relay host key mismatch" `+logged+` pinned=`)
		h.Koko("host", "set", tc.name, "port="+port).WantSuccess(t)

		keys := filepath.Join(tc.relay.Home(), ".ssh", "authorized_keys")
		authorized, err := os.ReadFile(keys)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(keys, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		wantRefused(t, h.Koko("exec", "app1", "true"),
			"koko: app1's Relay "+tc.name+" refused the koko key for its Host Account \"deploy\"; this SSH Session did not start.\n")
		wantRefused(t, bob.Koko("exec", "app1", "true"), "koko: app1's Relay refused the koko key; this SSH Session did not start.\n")
		waitServerLog(t, h, `msg="ssh session refused" .* name=app1 .* reason="relay login refused" `+logged+` err=`)
		if err := os.WriteFile(keys, authorized, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if r := h.Koko("exec", "app1", "echo", "through again"); r.Status != 0 || r.Stdout != "through again\n" {
		t.Errorf("koko exec app1 once every Relay is back: exit status %d, stdout %q, stderr %q", r.Status, r.Stdout, r.Stderr)
	}

	h.Koko("host", "set", "db1", "account=ops").WantSuccess(t)
	h.Koko("host", "set", "web1", "account=ops").WantSuccess(t)
	wantRefused(t, h.Koko("exec", "app1", "true"),
		"koko: app1's Relay web1 is Registered: koko passes through it only once it is Enrolled, with koko host enroll web1.\n")
	wantRefusal(t, h.KokoStdin(hostPassword, "host", "enroll", "app1", "--password-stdin", "--replace-host-key", "--json"),
		"invalid_relay", "app1's Relay web1 is not Managed")
}

// A live SSH Session through a chain ends once a Relay in the middle goes
// back to Registered, or its leg closes, naming the Relay to whoever can
// follow the links to it and its hop in the log. It goes on after a Relay's
// own Relay changes, through the chain it started through, until a Relay of
// that chain or of the Host's chain now stops being Managed, which a new
// Relay whose own chain holds one that is not makes it at once.
func TestALiveSSHSessionThroughAChain(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	inviteTo(t, h, map[string][]string{"carol": {"apps"}})
	c := startRelayChain(t, h, map[string]string{"app1": "apps"})
	managedHost(t, h, "web2")
	carol := h.Join("carol")

	owners := openLiveSSH(t, h, h.Proxy("tunnel", "app1"), "app1")
	carols := openLiveSSH(t, h, carol.Proxy(nil, "tunnel", "app1"), "app1")
	h.Koko("host", "set", "db1", "account=ops").WantSuccess(t)
	owners.wantEnded("koko: access to app1 was withdrawn: app1's Relay db1 is no longer Managed; this SSH Session is closed.\n")
	carols.wantEnded("koko: access to app1 was withdrawn: app1's Relay is no longer Managed; this SSH Session is closed.\n")
	waitServerLog(t, h, `msg="ssh session ended" .* relays=web1,db1 .* relay=db1 relay_id=host_\w+ hop=2 reason="relay registered"\n`)
	h.Koko("host", "set", "db1", "account="+sshhost.Account).WantSuccess(t)
	enroll(t, h, "db1")

	p := h.Background(nil, "exec", "app1", "echo ready; exec sleep 30")
	p.WaitStdout("ready")
	c.db1.DropConns()
	const line = "koko: app1's Relay db1 closed the connection.\n"
	if r := p.Wait(); r.Status != 255 || r.Stderr != line {
		t.Errorf("koko exec: exit status %d, stderr %q; want 255 and %q", r.Status, r.Stderr, line)
	}
	waitServerLog(t, h, `msg="ssh session ended" .* relay=db1 relay_id=host_\w+ hop=2 reason="Relay closed"\n`)

	// A Relay of the Host's chain now stops being Managed.
	s := openLiveSSH(t, h, h.Proxy("tunnel", "app1"), "app1")
	h.Koko("host", "set", "db1", "relay=web2").WantSuccess(t)
	s.wantOpen("after db1's new Relay")
	h.Koko("host", "set", "web2", "account=ops").WantSuccess(t)
	s.wantEnded("koko: access to app1 was withdrawn: app1's Relay web2 is no longer Managed; this SSH Session is closed.\n")
	h.Koko("host", "set", "web2", "account="+sshhost.Account).WantSuccess(t)
	enroll(t, h, "web2")

	// A Relay of the chain it started through stops being Managed.
	s = openLiveSSH(t, h, h.Proxy("tunnel", "app1"), "app1")
	h.Koko("host", "set", "db1", "relay=web1").WantSuccess(t)
	s.wantOpen("after db1's Relay is web1 again")
	h.Koko("host", "set", "web2", "account=ops").WantSuccess(t)
	s.wantEnded("koko: access to app1 was withdrawn: app1's Relay web2 is no longer Managed; this SSH Session is closed.\n")
	waitServerLog(t, h, `msg="ssh session ended" .* relays=web2,db1 .* relay=web2 relay_id=host_\w+ hop=1 reason="relay registered"\n`)

	// A new Relay, Managed, whose own chain holds one that is not: a new
	// SSH Session would be refused, so the live one ends at once.
	managedHost(t, h, "edge")
	managedHost(t, h, "gw")
	h.Koko("host", "set", "gw", "relay=edge").WantSuccess(t)
	h.Koko("host", "set", "edge", "account=ops").WantSuccess(t)
	s = openLiveSSH(t, h, h.Proxy("tunnel", "app1"), "app1")
	h.Koko("host", "set", "db1", "relay=gw").WantSuccess(t)
	s.wantEnded("koko: access to app1 was withdrawn: app1's Relay edge is no longer Managed; this SSH Session is closed.\n")
}

// A live SSH Session through a chain ends once a Relay in the middle stops
// answering, naming it, though the Host's leg, whose bytes it carries,
// falls silent with it.
func aLiveSSHSessionEndsWhenARelayInTheMiddleStopsAnswering(t *testing.T) {
	// Long enough that a Host or Relay still answering, through the
	// Enrolls and the command's start, is never taken for silent under
	// load.
	h := harness.Start(t, harness.Timings(timing.Timings{HostProbe: 50 * time.Millisecond, HostDead: 1500 * time.Millisecond}))
	c := startRelayChain(t, h, nil)
	p := h.Background(nil, "exec", "app1", "echo ready; exec sleep 30")
	p.WaitStdout("ready")

	c.db1.Freeze()
	const line = "koko: lost the connection to app1's Relay db1 (no answer for 1.5 s); this SSH Session has ended.\n"
	if r := p.Wait(); r.Status != 255 || r.Stdout != "ready\n" || r.Stderr != line {
		t.Errorf("exit status %d, stdout %q, stderr %q; want 255 and %q", r.Status, r.Stdout, r.Stderr, line)
	}
	waitServerLog(t, h, `msg="ssh session ended" .* relay=db1 relay_id=host_\w+ hop=2 reason="Relay leg lost"\n`)
}

// Enroll records nothing when a Relay in the middle of the Host's chain
// changes its own Relay while Enroll logs in to the Host through the chain
// as it was.
func enrollRecordsNothingWhenARelayInTheMiddleChangesMeanwhile(t *testing.T) {
	h := harness.Start(t)
	managedHost(t, h, "web1")
	managedHost(t, h, "web2")
	managedHost(t, h, "db1")
	h.Koko("host", "set", "db1", "relay=web1").WantSuccess(t)
	host := sshhost.Start(t, sshhost.Password(hostPassword), sshhost.SlowLogins(500*time.Millisecond))
	addHostAt(t, h.Koko, "app1", host, "relay=db1")
	done := inBackground(t, func() harness.Result {
		return h.KokoStdin(hostPassword, "host", "enroll", "app1", "--password-stdin", "--json")
	})
	eventually(t, "the koko server connects to the Host", func() bool { return host.Conns() == 1 })
	h.Koko("host", "set", "db1", "relay=web2").WantSuccess(t)
	(<-done).WantError(t, 1, "host_changed")
	wantPinned(t, showHost(t, h.Koko, "app1"), "registered", "")
}

// Enroll records nothing when the Host's chain of Relays is broken behind
// koko's back while it logs in to the Host through the chain as it was: a
// chain broken since is one that changed.
func enrollRecordsNothingWhenTheChainBreaksMeanwhile(t *testing.T) {
	dataDir := t.TempDir()
	h := harness.Start(t, harness.DataDir(dataDir))
	managedHost(t, h, "db1")
	ids := map[string]string{"db1": showHost(t, h.Koko, "db1").ID, "b": addHost(t, h.Koko, "b").ID}
	host := sshhost.Start(t, sshhost.Password(hostPassword), sshhost.SlowLogins(500*time.Millisecond))
	addHostAt(t, h.Koko, "app1", host, "relay=db1")
	done := inBackground(t, func() harness.Result {
		return h.KokoStdin(hostPassword, "host", "enroll", "app1", "--password-stdin", "--json")
	})
	eventually(t, "the koko server connects to the Host", func() bool { return host.Conns() == 1 })
	st, err := store.Open(t.Context(), dataDir)
	if err != nil {
		t.Fatal(err)
	}
	err = st.Write(t.Context(), func(tx store.Tx) error {
		for relay, of := range map[string]string{"b": "db1", "db1": "b"} {
			if _, err := tx.ExecContext(t.Context(), "UPDATE hosts SET relay_id = ? WHERE id = ?", ids[relay], ids[of]); err != nil {
				return err
			}
		}
		return nil
	})
	if cerr := st.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		t.Fatal(err)
	}
	(<-done).WantError(t, 1, "host_changed")
	wantPinned(t, showHost(t, h.Koko, "app1"), "registered", "")
}

// A chain of Relays changed behind koko's back into one that loops, or
// that holds a fifth Relay, is a read that fails, never no Relay: the
// Gateway refuses an SSH Session to a Host behind it as a connection that
// failed, and dials nothing; Enroll answers the same; a Relay whose chain
// is broken is set on no Host; a live SSH Session to a Host behind it ends
// once any change names a Host, as one whose access could not be checked,
// and every other goes on. The log names the ids read. Clearing the Relay
// of a Host behind it is still taken, which mends it, and a Member who
// cannot see the Host learns nothing of it.
func TestABrokenChainOfRelaysIsAReadThatFails(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()
	h := harness.Start(t, harness.DataDir(dataDir))
	inviteTo(t, h, map[string][]string{"bob": {"ops"}})
	looped := managedHost(t, h, "looped")
	long := managedHost(t, h, "long")
	ids := map[string]string{"looped": showHost(t, h.Koko, "looped").ID, "long": showHost(t, h.Koko, "long").ID}
	for _, name := range []string{"b", "c", "e1", "e2", "e3", "e4", "e5"} {
		ids[name] = addHost(t, h.Koko, name).ID
	}
	addHost(t, h.Koko, "z")
	addHost(t, h.Koko, "spare")
	managedHost(t, h, "healthy")
	cut := openLiveSSH(t, h, h.Proxy("tunnel", "looped"), "looped")
	goesOn := openLiveSSH(t, h, h.Proxy("tunnel", "healthy"), "healthy")

	// looped's chain loops through b and c; long's holds e1 to e5.
	st, err := store.Open(t.Context(), dataDir)
	if err != nil {
		t.Fatal(err)
	}
	err = st.Write(t.Context(), func(tx store.Tx) error {
		for host, relay := range map[string]string{"looped": "b", "b": "c", "c": "b", "long": "e5", "e5": "e4", "e4": "e3", "e3": "e2", "e2": "e1"} {
			if _, err := tx.ExecContext(t.Context(), "UPDATE hosts SET relay_id = ? WHERE id = ?", ids[relay], ids[host]); err != nil {
				return err
			}
		}
		return nil
	})
	if cerr := st.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		t.Fatal(err)
	}
	h.Koko("host", "retire", "spare").WantSuccess(t)
	cut.wantEnded("koko: the koko server could not check access to looped, so this SSH Session is closed; its log says why.\n")
	waitServerLog(t, h, `msg="ssh session ended" .* host=looped .* read_relay_ids=`+ids["b"]+","+ids["c"]+","+ids["b"]+` reason="relay chain broken"\n`)
	goesOn.wantOpen("after the recheck")

	for _, tc := range []struct {
		name string
		host *sshhost.Host
		read string
	}{
		{"looped", looped, ids["b"] + "," + ids["c"] + "," + ids["b"]},
		{"long", long, strings.Join([]string{ids["e5"], ids["e4"], ids["e3"], ids["e2"], ids["e1"]}, ",")},
	} {
		conns := tc.host.Conns()
		wantRefused(t, h.Koko("exec", tc.name, "true"), "koko: koko could not connect to "+tc.name+"; this SSH Session did not start.\n")
		waitServerLog(t, h, `level=WARN msg="ssh session refused" .* name=`+tc.name+` .* reason="relay chain broken" read_relay_ids=`+tc.read+`\n`)
		wantRefusal(t, h.KokoStdin(hostPassword, "host", "enroll", tc.name, "--password-stdin", "--replace-host-key", "--json"),
			"host_unreachable", "koko could not connect to "+tc.name+"; the koko server's log says why")
		waitServerLog(t, h, `msg="enroll failed" host=`+tc.name+` .* reason="relay chain broken" read_relay_ids=`+tc.read+`\n`)
		if n := tc.host.Conns() - conns; n != 0 {
			t.Errorf("%s behind a broken chain got %d connections, want none", tc.name, n)
		}
		h.Koko("host", "set", "z", "relay="+tc.name, "--json").WantError(t, 1, "internal_error")
		// host show still reads the Host, whose own Relay mends it, and
		// prints that Relay alone.
		own := map[string]string{"looped": "b", "long": "e5"}[tc.name]
		if r, want := h.Koko("host", "show", tc.name), "\nRelay:       "+own+" ("+ids[own]+")\n"; !strings.Contains(r.Stdout, want) {
			t.Errorf("host show %s printed\n%s\nwant the line %q", tc.name, r.Stdout, strings.TrimSpace(want))
		}
	}
	wantRefused(t, h.Join("bob").Koko("exec", "looped", "true"), "koko: no Host is named \"looped\".\n")

	h.Koko("host", "set", "looped", "--unset", "relay").WantSuccess(t)
	if r := h.Koko("exec", "looped", "echo", "mended"); r.Status != 0 || r.Stdout != "mended\n" {
		t.Errorf("koko exec looped once its Relay is cleared: exit status %d, stdout %q, stderr %q", r.Status, r.Stdout, r.Stderr)
	}
}
