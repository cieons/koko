package cli_test

import (
	"encoding/json/v2"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/harness/sshhost"
	"github.com/cieons/koko/internal/shared/timing"
)

// bareNames returns the bare names in koko's ssh_config on the device, in
// the order its Host lines hold them: every name but *.koko.
func bareNames(t *testing.T, d *harness.Device) []string {
	t.Helper()
	names := []string{}
	for line := range strings.Lines(readText(t, filepath.Join(kokoDir(d), "ssh_config"))) {
		rest, ok := strings.CutPrefix(line, "Host ")
		if !ok {
			continue
		}
		for n := range strings.FieldsSeq(rest) {
			if n != "*.koko" {
				names = append(names, n)
			}
		}
	}
	return names
}

// wantBareNames fails the test unless koko's ssh_config on the device holds
// exactly the bare names want, in order.
func wantBareNames(t *testing.T, d *harness.Device, when string, want ...string) {
	t.Helper()
	if got := bareNames(t, d); !slices.Equal(got, want) {
		t.Errorf("%s, koko's ssh_config holds the bare names %q, want %q", when, got, want)
	}
}

// bareNamesSetup records, as the Local caller, a Space ops whose Member is
// bob, and four Hosts: web1 in alice's Personal Space, db1 in ops, build in
// bob's Personal Space, and old in ops, Retired.
func bareNamesSetup(t *testing.T, h *harness.Harness) {
	t.Helper()
	h.Koko("user", "invite", "bob").WantSuccess(t)
	h.Koko("space", "add", "ops").WantSuccess(t)
	h.Koko("space", "member", "add", "ops", "bob").WantSuccess(t)
	addHost(t, h.Koko, "web1")
	addHost(t, h.Koko, "db1", "space=ops")
	addHost(t, h.Koko, "build", "space=bob")
	addHost(t, h.Koko, "old", "space=ops")
	h.Koko("host", "retire", "old").WantSuccess(t)
}

// koko join writes the bare names of the Hosts its User can see into
// koko's ssh_config, by name: a Member's those of their Spaces, the Owner's
// every one; a Retired Host's name is in neither.
func TestJoinWritesTheBareNamesOfTheHostsItsUserCanSee(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	bareNamesSetup(t, h)

	bob, _ := joinAs(t, deviceAdd(t, h, "laptop", "--user", "bob"))
	wantBareNames(t, bob, "after bob's join", "build", "db1")
	wantBareNames(t, h.Join(harness.Owner), "after alice's join", "build", "db1", "web1")
}

// Right after the caller's own host add (of one Host named, or of the
// Hosts on stdin), rename, move, retire and rm, the CLI rewrites the bare
// names from the Hosts the caller can see then: the change itself, and a
// Host someone else recorded in the caller's Space since. A Retired Host
// leaves the list.
func TestTheCallersOwnHostChangesRewriteTheBareNames(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	bareNamesSetup(t, h)
	bob, _ := joinAs(t, deviceAdd(t, h, "laptop", "--user", "bob"))

	for _, step := range []struct {
		// other is the Host alice records in ops just before, which bob's
		// list does not hold yet.
		other string
		// change is bob's own change, run on his device with stdin.
		change []string
		stdin  string
		want   []string
	}{{
		other:  "api",
		change: []string{"host", "add", "web2", "address=203.0.113.20", "account=deploy"},
		want:   []string{"api", "build", "db1", "web2"},
	}, {
		other:  "cache",
		change: []string{"host", "add"},
		stdin:  `[{"name": "hub", "address": "203.0.113.21", "account": "deploy"}]`,
		want:   []string{"api", "build", "cache", "db1", "hub", "web2"},
	}, {
		other:  "dns",
		change: []string{"host", "rename", "web2", "web3"},
		want:   []string{"api", "build", "cache", "db1", "dns", "hub", "web3"},
	}, {
		other:  "edge",
		change: []string{"host", "move", "web3", "ops"},
		want:   []string{"api", "build", "cache", "db1", "dns", "edge", "hub", "web3"},
	}, {
		other:  "fw",
		change: []string{"host", "retire", "web3"},
		want:   []string{"api", "build", "cache", "db1", "dns", "edge", "fw", "hub"},
	}, {
		other:  "gw",
		change: []string{"host", "rm", "build"},
		want:   []string{"api", "cache", "db1", "dns", "edge", "fw", "gw", "hub"},
	}} {
		addHost(t, h.Koko, step.other, "space=ops")
		r := bob.KokoStdin(step.stdin, step.change...)
		r.WantSuccess(t)
		if r.Stderr != "" {
			t.Errorf("bob's change after alice recorded %s wrote %q on stderr, want nothing", step.other, r.Stderr)
		}
		wantBareNames(t, bob, "after bob's change once alice recorded "+step.other, step.want...)
	}
}

// Over the Unix socket the caller is the Local caller, on a machine that
// joined nothing: a Host change there writes no ssh_config, and says
// nothing of one.
func TestAHostChangeOverTheUnixSocketWritesNoSSHConfig(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	r := h.Koko("host", "add", "web1", "address=203.0.113.10", "account=deploy")
	r.WantSuccess(t)
	if r.Stderr != "" {
		t.Errorf("host add over the Unix socket wrote %q on stderr, want nothing", r.Stderr)
	}
	path := filepath.Join(h.Home(), ".ssh", "koko", "ssh_config")
	if _, err := os.Lstat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("host add over the Unix socket left %s (%v), want none", path, err)
	}
}

// A change whose bare names koko cannot rewrite still holds and prints what
// it printed, --json included, and exits 0: the rewrite comes after it.
// One line on stderr says why, and that the Host's .koko name reaches it.
func TestAHostChangeWhoseBareNamesCannotBeWrittenStillHolds(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	d := h.Join(harness.Owner)
	// A directory in its place, which no rename replaces.
	path := filepath.Join(kokoDir(d), "ssh_config")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(path, "in-the-way"), 0o700); err != nil {
		t.Fatal(err)
	}

	r := d.Koko("host", "add", "web1", "address=203.0.113.10", "account=deploy", "--json")
	var got printedHost
	if err := json.Unmarshal([]byte(r.Stdout), &got); r.Status != 0 || err != nil || got.Name != "web1" {
		t.Errorf("host add exited %d and printed %q (%v), want 0 and web1", r.Status, r.Stdout, err)
	}
	if !strings.HasPrefix(r.Stderr, "koko: could not rewrite the bare names in koko's ssh_config (") ||
		!strings.HasSuffix(r.Stderr, "); until koko does, reach a Host as ssh <host>.koko\n") || strings.Count(r.Stderr, "\n") != 1 {
		t.Errorf("host add wrote %q on stderr, want one line saying it could not rewrite the bare names", r.Stderr)
	}
	if names := hostNames(t, h.Koko); !slices.Equal(names, []string{"web1"}) {
		t.Errorf("host ls lists %q, want web1 recorded", names)
	}
}

// hostLists counts the host.list calls the koko server has answered.
func hostLists(h *harness.Harness) int {
	return strings.Count(h.Log(), `msg="api call" path=/v1/host.list `)
}

// A Host someone else records in a Member's Space, and a Space the Member
// is added to, reach the Member's device as <name>.koko at once, since the
// Gateway checks access at each SSH Session, and as bare names once the
// Device Mux refreshes them, which it does every BareNameRefresh. A refresh
// that finds the record as it was leaves koko's ssh_config alone.
func theDeviceMuxRefreshesTheBareNames(t *testing.T) {
	const refresh = 100 * time.Millisecond
	h := harness.Start(t)
	bareNamesSetup(t, h)
	h.Koko("space", "add", "staging").WantSuccess(t)
	bob := harness.NewMuxDevice(t, timing.Timings{BareNameRefresh: refresh})
	bob.Koko("join", deviceAdd(t, h, "laptop", "--user", "bob").Code).WantSuccess(t)
	wantBareNames(t, bob, "after bob's join", "build", "db1")

	for hostName, space := range map[string]string{"web2": "ops", "web3": "staging"} {
		addHostAt(t, h.Koko, hostName, sshhost.Start(t, sshhost.Password(hostPassword)), "space="+space)
		enroll(t, h, hostName)
	}
	h.Koko("space", "member", "add", "staging", "bob").WantSuccess(t)
	for _, sshName := range []string{"web2.koko", "web3.koko"} {
		if r := bob.Koko("exec", sshName, "echo", "reached"); r.Status != 0 || r.Stdout != "reached\n" {
			t.Errorf("koko exec %s: exit status %d, stdout %q, stderr %q; want it to reach the Host at once", sshName, r.Status, r.Stdout, r.Stderr)
		}
	}
	eventually(t, "the Device Mux writes the bare names web2 and web3", func() bool {
		return slices.Equal(bareNames(t, bob), []string{"build", "db1", "web2", "web3"})
	})

	path := filepath.Join(kokoDir(bob), "ssh_config")
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	lists := hostLists(h)
	eventually(t, "the Device Mux refreshes twice more", func() bool { return hostLists(h) >= lists+2 })
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) {
		t.Errorf("refreshes that found the record as it was rewrote koko's ssh_config")
	}
	wantBareNames(t, bob, "after the refreshes", "build", "db1", "web2", "web3")
}

// The Device Mux's own refreshes are no command: once the last command
// leaves, it idles out, however often it refreshes.
func theDeviceMuxIdlesOutThoughItRefreshesTheBareNames(t *testing.T) {
	const idle = 300 * time.Millisecond
	h, _, d := muxDevice(t, timing.Timings{DeviceMuxIdle: idle, BareNameRefresh: idle / 10})
	held := d.DialMux()
	lists := hostLists(h)
	eventually(t, "the Device Mux refreshes twice more", func() bool { return hostLists(h) >= lists+2 })
	_ = held.Close()
	waitLog(t, d, `msg="device mux stopped" reason="nothing was connected to it for a while"`)
}

// The Device Mux refreshes the bare names at its start, and a refresh that a
// drop cuts tries again on the next Device Connection, which the Device Mux
// opens at once, rather than BareNameRefresh later.
func theDeviceMuxRefreshesAtItsStartAndAgainAfterADrop(t *testing.T) {
	h, fwd, _, init := initThrough(t)
	d := harness.NewMuxDevice(t, timing.Timings{BareNameRefresh: time.Hour})
	// The join, with no Device Mux, lists the Hosts once; the next host.list
	// is the Device Mux's at its start, whose answer the cut drops.
	d.KokoEnv([]string{"KOKO_DIRECT=1"}, "join", init.JoinCode.Code).WantSuccess(t)
	fwd.CutWhen(func() bool { return hostLists(h) >= 2 })
	d.Koko("space", "ls", "--json").WantSuccess(t)

	waitLogs(t, d, `msg="device connection opened"`, 2)
	eventually(t, "the Device Mux refreshes again on its next Device Connection", func() bool { return hostLists(h) >= 3 })
	waitLog(t, d, `msg="refreshed the bare names"`)
}

// A refresh that falls due while the Device Mux holds no Device Connection
// waits for one without asking for it: after a drop the Device Mux tries
// once at once, as ever, and never again while nothing else waits, however
// often a refresh falls due.
func aBareNameRefreshNeverMakesTheDeviceMuxReconnect(t *testing.T) {
	const refresh = 20 * time.Millisecond
	_, fwd, d := muxDevice(t, timing.Timings{BareNameRefresh: refresh, ReconnectFirst: refresh, ReconnectMax: refresh})
	fwd.To(harness.FreeAddr(t))
	// The next refresh's answer is the first the koko server sends.
	fwd.CutWhen(func() bool { return true })
	waitConns(t, fwd, 3, d)
	time.Sleep(20 * refresh)
	if n := fwd.Conns(); n != 3 {
		t.Errorf("the device made %d connections, want 3: the join's, the Device Mux's, and its one try at once after the drop; the Device Mux logged:\n%s", n, d.MuxLog())
	}
}
