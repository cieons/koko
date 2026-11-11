package cli_test

import (
	"encoding/json/v2"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/shared/api"
)

// sendError calls o with body as the Local caller and returns its error
// object, the zero one when the call succeeded.
func sendError(t *testing.T, h *harness.Harness, o api.Operation, body string) api.Error {
	t.Helper()
	status, b := h.Send(o, []byte(body))
	if status == http.StatusOK {
		return api.Error{}
	}
	var e api.ErrorResponse
	if err := json.Unmarshal(b, &e); err != nil {
		t.Fatalf("%s answered %d %s, which is not an error object: %v", o.Info().Name, status, b, err)
	}
	return e.Error
}

// A batch host.retire counts only the Hosts outside it as using a Relay:
// a Relay retires with every Host behind it, and is at fault while one
// outside the batch is not Retired.
func TestARelayRetiresInABatchWithTheHostsBehindIt(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	relay := managedHost(t, h, "gw")
	relayedHost(t, h, "gw", "web1", relay)
	relayedHost(t, h, "gw", "web2", relay)

	e := sendError(t, h, api.HostRetire, `{"hosts": ["gw", "web1"]}`)
	if e.Code != "relay_in_use" || len(e.Items) != 1 || e.Items[0].Index != 0 {
		t.Errorf("retiring gw with web1 alone = %+v, want gw at fault with relay_in_use, as web2 still uses it", e)
	}
	if got := h.Koko("host", "show", "web1", "--json"); got.Status != 0 {
		t.Errorf("web1 is gone after the refused batch: %q", got.Stderr)
	}
	if e := sendError(t, h, api.HostRetire, `{"hosts": ["gw", "web1", "web2"]}`); e.Code != "" {
		t.Errorf("retiring gw with every Host behind it = %+v, want it done", e)
	}
	for _, n := range []string{"gw", "web1", "web2"} {
		h.Koko("host", "show", n, "--json").WantError(t, 1, "unknown_host")
	}
}

// A batch host.move ends, once it commits, the live SSH Sessions to each
// moved Host of a Member who cannot act in the new Space; a refused batch
// ends none.
func TestABatchMoveEndsTheSSHSessionsThatLoseTheirHostsOnceItIsApplied(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	addSpaceWithMembers(t, h, "ops", "bob")
	laptop := h.Join("bob")
	managedHostIn(t, h, "web1", "ops")
	managedHostIn(t, h, "web2", "ops")
	h.Koko("space", "add", "vault").WantSuccess(t)
	web1 := openLiveSSH(t, h, laptop.Proxy(nil, "tunnel", "web1"), "web1")
	web2 := openLiveSSH(t, h, laptop.Proxy(nil, "tunnel", "web2"), "web2")

	if e := sendError(t, h, api.HostMove, `{"hosts": ["web1", "web2", "nowhere"], "space": "vault"}`); e.Code != "unknown_host" {
		t.Fatalf("a batch move with nowhere = %+v, want unknown_host", e)
	}
	web1.wantOpen("after the refused batch")
	web2.wantOpen("after the refused batch")

	if e := sendError(t, h, api.HostMove, `{"hosts": ["web1", "web2"], "space": "vault"}`); e.Code != "" {
		t.Fatalf("the batch move = %+v", e)
	}
	web1.wantEnded("koko: access to web1 was withdrawn; this SSH Session is closed.\n")
	web2.wantEnded("koko: access to web2 was withdrawn; this SSH Session is closed.\n")
}

// decodeHosts reads the array of Hosts a command printed with --json.
func decodeHosts(t *testing.T, r harness.Result) []printedHost {
	t.Helper()
	r.WantSuccess(t)
	var hosts []printedHost
	if err := json.Unmarshal([]byte(r.Stdout), &hosts); err != nil {
		t.Fatalf("the command printed %q, want an array of Hosts: %v", r.Stdout, err)
	}
	return hosts
}

// namesOf returns the names of hosts, in order.
func namesOf(hosts []printedHost) []string {
	names := []string{}
	for _, h := range hosts {
		names = append(names, h.Name)
	}
	return names
}

// koko host set, move, retire and rm take several names and act on them
// as one batch, printing each Host, as an array with --json; one name
// prints one Host, as before, and host move <host> <space> still moves one.
func TestHostWritesTakeSeveralNames(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	h.Koko("space", "add", "ops").WantSuccess(t)
	for _, n := range []string{"web1", "web2", "web3", "web4"} {
		addHost(t, h.Koko, n)
	}

	set := decodeHosts(t, h.Koko("host", "set", "web1", "web2", "role=node", "--json"))
	if got := namesOf(set); !slices.Equal(got, []string{"web1", "web2"}) || set[1].Attributes["role"] != "node" {
		t.Errorf("host set web1 web2 role=node printed %v, want both with role=node", set)
	}
	if r := h.Koko("host", "move", "web2", "web1", "--to", "ops"); r.Stdout != "Moved Host web2 ("+set[1].ID+") to Space ops.\nMoved Host web1 ("+set[0].ID+") to Space ops.\n" {
		t.Errorf("host move web2 web1 --to ops printed %q, stderr %q", r.Stdout, r.Stderr)
	}
	if got := decodeHost(t, h.Koko("host", "move", "web3", "ops", "--json")); got.Space != "ops" {
		t.Errorf("host move web3 ops moved it to %s, want ops", got.Space)
	}
	retired := decodeHosts(t, h.Koko("host", "retire", "web1", "web2", "--json"))
	if len(retired) != 2 || retired[0].State != "retired" || retired[1].State != "retired" {
		t.Errorf("host retire web1 web2 printed %+v, want both Retired", retired)
	}
	if got := namesOf(decodeHosts(t, h.Koko("host", "rm", "web3", "web4", "--json"))); !slices.Equal(got, []string{"web3", "web4"}) {
		t.Errorf("host rm web3 web4 printed %v", got)
	}
	if got := hostNames(t, h.Koko); len(got) != 0 {
		t.Errorf("host ls lists %v after the batches, want none", got)
	}
}

// --where, on host set and host move only, acts on every Host host ls
// lists with its terms, as one batch of their ids, and says how many it
// matched, for people only; it fails with unknown_host when none matches,
// and is refused beside names, with a term host ls refuses, and on host
// retire and host rm, before any call.
func TestWhereActsOnTheHostsItMatches(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	h.Koko("space", "add", "ops").WantSuccess(t)
	addHost(t, h.Koko, "web1", "region=hk")
	addHost(t, h.Koko, "web2", "region=hk")
	addHost(t, h.Koko, "db1", "region=eu")

	r := h.Koko("host", "set", "--where", "region=hk", "role=node")
	if r.Status != 0 || r.Stderr != "--where region=hk matched 2 Hosts.\n" || strings.Count(r.Stdout, "Changed Host") != 2 {
		t.Errorf("host set --where region=hk role=node: exit %d, stdout %q, stderr %q", r.Status, r.Stdout, r.Stderr)
	}
	r = h.Koko("host", "move", "--where", "role=node", "--where", "region!=eu", "--to", "ops", "--json")
	if got := namesOf(decodeHosts(t, r)); !slices.Equal(got, []string{"web1", "web2"}) || r.Stderr != "" {
		t.Errorf("host move --where ... --json moved %v with stderr %q, want web1 and web2 and nothing on stderr", got, r.Stderr)
	}
	r = h.Koko("host", "set", "--where", "region=eu", "role=db", "--json")
	if got := namesOf(decodeHosts(t, r)); !slices.Equal(got, []string{"db1"}) {
		t.Errorf("host set --where matching one Host printed %v, want an array of db1", got)
	}
	msg := h.Koko("host", "set", "--where", "region=us", "role=node", "--json").WantError(t, 1, "unknown_host")
	if !strings.Contains(msg, "--where region=us") {
		t.Errorf("--where matching nothing says %q, want it to name the term", msg)
	}

	calls := h.Calls()
	for _, args := range [][]string{
		{"host", "set", "web1", "--where", "region=hk", "role=node"},
		{"host", "set", "role=node"},
		{"host", "set", "--where", "price.gt=cheap", "role=node"},
		{"host", "move", "--where", "region=hk"},
		{"host", "move", "web1", "web2", "ops"},
		{"host", "retire", "--where", "region=hk"},
		{"host", "rm", "--where", "region=hk"},
	} {
		h.Koko(append(args, "--json")...).WantError(t, 2, "invalid_usage")
	}
	if n := h.Calls() - calls; n != 0 {
		t.Errorf("the refused command lines made %d call(s)", n)
	}
}

// A refused batch changes nothing, prints each Host at fault on a line of
// its own, or in items with --json, and exits 1.
func TestARefusedBatchPrintsEachHostAtFault(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	addHost(t, h.Koko, "web1")
	addHost(t, h.Koko, "web2")

	r := h.Koko("host", "retire", "web1", "nowhere", "web2", "web_9")
	want := "koko: none of the 4 Hosts was retired, for the 2 at fault:\n" +
		"[1] no Host is named \"nowhere\"\n" +
		"[3] \"web_9\" is neither a Host name nor a Host id: "
	if r.Status != 1 || !strings.HasPrefix(r.Stderr, want) || r.Stdout != "" {
		t.Errorf("a refused batch: exit %d, stdout %q, stderr %q; want 1 and stderr starting %q", r.Status, r.Stdout, r.Stderr, want)
	}
	r = h.Koko("host", "rm", "web1", "nowhere", "--json")
	r.WantError(t, 1, "unknown_host")
	var e api.ErrorResponse
	if err := json.Unmarshal([]byte(r.Stderr), &e); err != nil || len(e.Error.Items) != 1 || e.Error.Items[0].Index != 1 {
		t.Errorf("a refused batch with --json printed %q (%v), want the one item at fault", r.Stderr, err)
	}
	if got := hostNames(t, h.Koko); !slices.Equal(got, []string{"web1", "web2"}) {
		t.Errorf("host ls lists %v after the refused batches, want web1 and web2 as they were", got)
	}
}

// koko secret move and rm take several names, as one batch; secret move
// <space>/<secret> <space> still moves one.
func TestSecretWritesTakeSeveralNames(t *testing.T) {
	t.Parallel()
	h := startWithOps(t)
	h.Koko("space", "add", "web").WantSuccess(t)
	for _, n := range []string{"ops/a", "ops/b", "ops/c", "ops/d"} {
		h.KokoStdin("v", "secret", "add", n).WantSuccess(t)
	}
	r := h.Koko("secret", "move", "ops/a", "ops/b", "--to", "web")
	if r.Status != 0 || strings.Count(r.Stdout, "Moved Secret ops/") != 2 || !strings.Contains(r.Stdout, "Moved Secret ops/b to web/b") {
		t.Errorf("secret move ops/a ops/b --to web: exit %d, stdout %q, stderr %q", r.Status, r.Stdout, r.Stderr)
	}
	h.Koko("secret", "move", "ops/c", "web").WantSuccess(t)
	r = h.Koko("secret", "rm", "ops/d", "d", "/x", "--json")
	r.WantError(t, 2, "invalid_usage")
	var e api.ErrorResponse
	if err := json.Unmarshal([]byte(r.Stderr), &e); err != nil || len(e.Error.Items) != 2 || e.Error.Items[0].Index != 1 || e.Error.Items[1].Index != 2 {
		t.Errorf("secret rm with two bare words printed %q (%v), want both in items", r.Stderr, err)
	}
	h.Koko("secret", "rm", "web/a", "nowhere/x", "--json").WantError(t, 1, "unknown_space")
	r = h.Koko("secret", "rm", "web/a", "web/b", "--json")
	var removed []struct {
		Space string `json:"space"`
		Name  string `json:"name"`
	}
	if err := json.Unmarshal([]byte(r.Stdout), &removed); err != nil || len(removed) != 2 || removed[1].Name != "b" {
		t.Errorf("secret rm web/a web/b --json printed %q (%v), want both", r.Stdout, err)
	}
	if got := secretNames(t, h.Koko); !slices.Equal(got, []string{"ops/d", "web/c"}) {
		t.Errorf("secret ls lists %v, want ops/d and web/c", got)
	}
}
