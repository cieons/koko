package cli_test

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/shared/api"
)

// koko host add without a name reads Hosts as JSON on stdin: an array, added
// in one call and printed as an array, or one object, printed as one Host.
// The fields only koko sets are ignored, so each Host added is Registered,
// with an id of its own and no pin.
func TestHostAddReadsAnArrayOrOneObjectOnStdin(t *testing.T) {
	h := harness.Start(t)
	r := h.KokoStdin(`[
		{"name": "web1", "address": "203.0.113.10", "account": "deploy", "attributes": {"provider": "hetzner", "price": 9.90}},
		{"name": "web2", "space": "alice", "address": "203.0.113.11", "port": 2222, "account": "deploy", "note": "The second."}
	]`, "host", "add", "--json")
	r.WantSuccess(t)
	harness.Golden(t, "host-add-stdin.json", r.Stdout)
	var added []listedHost
	if err := json.Unmarshal([]byte(r.Stdout), &added); err != nil {
		t.Fatalf("host add printed %q: %v", r.Stdout, err)
	}
	if len(added) != 2 || added[0].Name != "web1" || added[1].Name != "web2" {
		t.Fatalf("host add printed %+v, want web1 and web2 in that order", added)
	}

	r = h.KokoStdin(`{"id": "host_aaaaaaaa", "name": "db1", "space": "alice", "address": "203.0.113.12", "port": 22,
		"account": "deploy", "state": "managed", "host_key": "SHA256:47DEQpj8HBSa+/TImW+5JCeuQeRkm5NMpJWZG3hSuFU",
		"attributes": {}, "note": null, "created_at": "2026-01-02T03:04:05Z", "updated_at": "2026-01-02T03:04:05Z"}`,
		"host", "add", "--json")
	got := decodeHost(t, r)
	if got.Name != "db1" || got.ID == "host_aaaaaaaa" || got.State != "registered" || got.HostKey != nil ||
		got.CreatedAt == "2026-01-02T03:04:05Z" || got.Note != nil {
		t.Errorf("host add of one object printed %+v, want db1 Registered with an id of its own, no pin and no Note", got)
	}
	if names := listed(t, h, "UTC"); !slices.Equal(names, []string{"db1", "web1", "web2"}) {
		t.Errorf("host ls = %q, want db1, web1 and web2", names)
	}
}

// koko host add without a name never waits for typing nobody will do: a
// terminal on stdin is refused at once, before anything is read from it.
func TestHostAddWithoutANameRefusesATerminalOnStdin(t *testing.T) {
	h := harness.Start(t)
	term := harness.NewTerminal()
	if msg := h.KokoTerminal(term, "host", "add", "--json").WantError(t, 2, "invalid_usage"); !strings.Contains(msg, "terminal") {
		t.Errorf("the refusal says %q, want it to name the terminal", msg)
	}
	if n := term.Reads(); n != 0 {
		t.Errorf("host add read the terminal %d times, want never", n)
	}
	if n := h.Calls(); n != 0 {
		t.Errorf("the refusal made %d calls, want none", n)
	}
}

// Stdin that holds no Hosts as JSON is refused before any call, and so is
// what only koko host add <host> takes, key=value words and --note-file,
// which a Host on stdin gives as its members.
func TestHostAddWithoutANameRefusesWhatIsNotHostsOnStdin(t *testing.T) {
	h := harness.Start(t)
	const web1 = `{"name": "web1", "address": "203.0.113.10", "account": "deploy"}`
	for _, tc := range []struct {
		stdin string
		args  []string
		says  string
	}{
		{"", nil, "stdin is empty"},
		{" \n", nil, "stdin is empty"},
		{"web1 address=203.0.113.10", nil, "stdin is not JSON"},
		{web1 + web1, nil, "stdin is not JSON"},
		{"[" + web1 + strings.Repeat(" ", api.MaxRequestBody) + "]", nil, "16 MiB"},
		{"[]", nil, "empty array"},
		{web1, []string{"address=203.0.113.10", "account=deploy"}, "address=: key=value words need the Host's name"},
		{web1, []string{"--note-file", "-"}, "--note-file needs the Host's name"},
		{web1, []string{"--note-file", hostNoteFile(t, "A Note.")}, "--note-file needs the Host's name"},
	} {
		args := append([]string{"host", "add", "--json"}, tc.args...)
		if msg := h.KokoStdin(tc.stdin, args...).WantError(t, 2, "invalid_usage"); !strings.Contains(msg, tc.says) {
			t.Errorf("host add %v of %.40q says %q, want %q", tc.args, tc.stdin, msg, tc.says)
		}
	}
	if n := h.Calls(); n != 0 {
		t.Errorf("the refusals made %d calls, want none", n)
	}
}

// A field the CLI's Host shape does not have is refused by name before any
// call, so a typo, or an Attribute written beside the fields instead of in
// "attributes", never drops a value silently.
func TestHostAddOnStdinRefusesAnUnknownFieldByName(t *testing.T) {
	h := harness.Start(t)
	r := h.KokoStdin(`{"name": "web1", "address": "203.0.113.10", "account": "deploy", "provider": "hetzner", "acount": "root"}`,
		"host", "add", "--json")
	if msg := r.WantError(t, 2, "invalid_usage"); !strings.Contains(msg, `"acount", "provider"`) {
		t.Errorf("the refusal says %q, want it to name \"acount\" and \"provider\"", msg)
	}
	if n := h.Calls(); n != 0 {
		t.Errorf("the refusal made %d calls, want none", n)
	}
}

// Each Host on stdin meets the rules a key=value Host meets, and one that
// breaks one is refused before any call, with exit status 2.
func TestHostAddOnStdinChecksEveryValueBeforeAnyCall(t *testing.T) {
	h := harness.Start(t)
	const base = `"name": "web1", "address": "203.0.113.10", "account": "deploy"`
	for _, host := range []string{
		`5`,
		`[]`,
		`{"address": "203.0.113.10", "account": "deploy"}`,
		`{"name": "web1", "account": "deploy"}`,
		`{"name": "web1", "address": "203.0.113.10"}`,
		`{"name": "", "address": "203.0.113.10", "account": "deploy"}`,
		`{"name": "web1", "address": "10.0.0.256", "account": "deploy"}`,
		`{"name": "web1", "address": "203.0.113.10", "account": "-deploy"}`,
		`{` + base + `, "name": "web2"}`,
		`{` + base + `, "port": 0}`,
		`{` + base + `, "port": "22"}`,
		`{` + base + `, "port": 22.5}`,
		`{` + base + `, "space": ""}`,
		`{` + base + `, "attributes": {"Region": "eu"}}`,
		`{` + base + `, "attributes": {"relay": "web2"}}`,
		`{` + base + `, "attributes": {"region": ""}}`,
		`{` + base + `, "attributes": {"region": 5}}`,
		`{` + base + `, "attributes": {"region": true}}`,
		`{` + base + `, "attributes": {"price": "10"}}`,
		`{` + base + `, "attributes": {"price": 1e2}}`,
		`{` + base + `, "attributes": {"expires_at": "+1y"}}`,
		`{` + base + `, "attributes": []}`,
		`{` + base + `, "note": ""}`,
		`{` + base + `, "note": "nul\u0000byte"}`,
	} {
		for _, stdin := range []string{host, "[" + host + "]"} {
			r := h.KokoStdin(stdin, "host", "add", "--json")
			if r.Status != 2 {
				t.Errorf("host add of %s = %d %s, want it refused with exit 2", stdin, r.Status, r.Stdout+r.Stderr)
				continue
			}
			r.WantError(t, 2, "invalid_usage")
		}
	}
	if n := h.Calls(); n != 0 {
		t.Errorf("the refusals made %d calls, want none", n)
	}
}

// stdinRefusal is the error object a refused host add printed with --json.
func stdinRefusal(t *testing.T, r harness.Result) api.Error {
	t.Helper()
	var e api.ErrorResponse
	if err := json.Unmarshal([]byte(r.Stderr), &e); err != nil {
		t.Fatalf("stderr %q is not an error object: %v", r.Stderr, err)
	}
	return e.Error
}

// faultsAt returns the index of each item at fault in a refused batch.
func faultsAt(e api.Error) []int {
	at := []int{}
	for _, it := range e.Items {
		at = append(at, it.Index)
	}
	return at
}

// A batch with Hosts at fault adds none of them, and the refusal lists each
// Host at fault by its index in the batch, every one, not only the first.
func TestAnInvalidBatchIsRefusedBeforeAnyCallListingEveryHostAtFault(t *testing.T) {
	h := harness.Start(t)
	r := h.KokoStdin(`[
		{"name": "web1", "address": "203.0.113.10", "account": "deploy"},
		{"name": "web2", "address": "203.0.113.11", "account": "deploy", "attributes": {"price": "10"}},
		{"name": "web3", "address": "203.0.113.12", "account": "deploy"},
		{"name": "web4", "address": "203.0.113.13", "account": "deploy", "region": "fsn1"}
	]`, "host", "add", "--json")
	r.WantError(t, 2, "invalid_usage")
	harness.Golden(t, "host-add-stdin-invalid.json", r.Stderr)
	e := stdinRefusal(t, r)
	if got := faultsAt(e); !slices.Equal(got, []int{1, 3}) {
		t.Errorf("the refusal lists the Hosts at %v, want 1 and 3", got)
	}
	if n := h.Calls(); n != 0 {
		t.Errorf("the refusal made %d calls, want none", n)
	}
	people := h.KokoStdin(`[{"name": "web1"}, {"name": "web2", "address": "203.0.113.11", "account": "deploy"}, {"name": "web3"}]`, "host", "add")
	if people.Status != 2 || !strings.Contains(people.Stderr, "\n[0] ") || !strings.Contains(people.Stderr, "\n[2] ") ||
		strings.Contains(people.Stderr, "[1]") {
		t.Errorf("host add for people = %d %q, want each Host at fault on a line of its own", people.Status, people.Stderr)
	}
}

// What only the koko server can tell, such as a taken name, is checked for
// every Host of a batch too: none is added, and the refusal lists each Host
// at fault with its own code. Its own code is the one they share, or
// invalid_batch when they differ.
func TestTheKokoServerRefusesABatchListingEveryHostAtFault(t *testing.T) {
	h := harness.Start(t)
	recordHost(t, h.Koko, "web1")
	const host = `"address": "203.0.113.10", "account": "deploy"`
	r := h.KokoStdin(`[
		{"name": "web1", `+host+`},
		{"name": "web2", `+host+`},
		{"name": "Web3", `+host+`},
		{"name": "web4", "space": "nowhere", `+host+`},
		{"name": "web2", `+host+`}
	]`, "host", "add", "--json")
	r.WantError(t, 1, "invalid_batch")
	harness.Golden(t, "host-add-stdin-refused.json", r.Stderr)
	e := stdinRefusal(t, r)
	want := []api.ItemError{{Index: 0, Code: "name_taken"}, {Index: 2, Code: "invalid_name"}, {Index: 3, Code: "unknown_space"}, {Index: 4, Code: "name_taken"}}
	if !slices.EqualFunc(e.Items, want, func(a, b api.ItemError) bool { return a.Index == b.Index && a.Code == b.Code && a.Message != "" }) {
		t.Errorf("the refusal lists %+v, want %+v, each with a message", e.Items, want)
	}

	r = h.KokoStdin(`[{"name": "web5", `+host+`}, {"name": "web1", `+host+`}]`, "host", "add", "--json")
	r.WantError(t, 1, "name_taken")
	if got := faultsAt(stdinRefusal(t, r)); !slices.Equal(got, []int{1}) {
		t.Errorf("the refusal lists the Hosts at %v, want 1", got)
	}
	if names := listed(t, h, "UTC"); !slices.Equal(names, []string{"web1"}) {
		t.Errorf("after the refusals host ls = %q, want web1 alone", names)
	}
}

// withoutKokosFields returns the Hosts a host ls --json printed, each
// without the fields only koko sets afresh for a new Host: its id and times.
func withoutKokosFields(t *testing.T, printed string) []map[string]jsontext.Value {
	t.Helper()
	var hosts []map[string]jsontext.Value
	if err := json.Unmarshal([]byte(printed), &hosts); err != nil {
		t.Fatalf("host ls --json printed %q: %v", printed, err)
	}
	for _, h := range hosts {
		for _, k := range []string{"id", "created_at", "updated_at"} {
			if _, ok := h[k]; !ok {
				t.Fatalf("host ls --json printed a Host without %q: %s", k, printed)
			}
			delete(h, k)
		}
	}
	return hosts
}

// koko host ls --json --with-note | koko host add copies Hosts and loses
// nothing: each comes back in its Space with its fields, its typed
// Attributes and its Note, as a new Registered Host. Without --with-note
// the Notes stay behind.
func TestHostLsWithNotePipedIntoHostAddKeepsEverything(t *testing.T) {
	h := harness.Start(t)
	h.Koko("space", "add", "ops").WantSuccess(t)
	h.Koko("host", "add", "web1", "space=ops", "address=203.0.113.10", "port=2222", "account=deploy",
		"provider=hetzner", "price=9.90", "currency=EUR", "billing_cycle=yearly", "expires_at=2027-11-30",
		"auto_renew=no", "cores=8", "--note-file", hostNoteFile(t, "Set up for the shop.\nRuns nginx.\n")).WantSuccess(t)
	h.Koko("host", "add", "web2", "address=2001:db8::1", "account=root").WantSuccess(t)
	withNote := h.Koko("host", "ls", "--json", "--with-note")
	withNote.WantSuccess(t)
	withoutNote := h.Koko("host", "ls", "--json")
	withoutNote.WantSuccess(t)
	removeAll := func() {
		t.Helper()
		for _, name := range []string{"web1", "web2"} {
			h.Koko("host", "rm", name).WantSuccess(t)
		}
	}

	removeAll()
	added := h.KokoStdin(withNote.Stdout, "host", "add")
	added.WantSuccess(t)
	lines := strings.Split(strings.TrimSuffix(added.Stdout, "\n"), "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "Added Host web1 (host_") || !strings.HasSuffix(lines[0], ") to Space ops.") ||
		!strings.HasPrefix(lines[1], "Added Host web2 (host_") || !strings.HasSuffix(lines[1], ") to Space "+harness.Owner+".") {
		t.Errorf("host add for people printed %q, want a line for each Host, in order", added.Stdout)
	}
	again := h.Koko("host", "ls", "--json", "--with-note")
	again.WantSuccess(t)
	if got, want := withoutKokosFields(t, again.Stdout), withoutKokosFields(t, withNote.Stdout); !reflect.DeepEqual(got, want) {
		t.Errorf("after the round trip host ls --json --with-note printed\n%s\nwant, but for ids and times,\n%s", again.Stdout, withNote.Stdout)
	}

	removeAll()
	h.KokoStdin(withoutNote.Stdout, "host", "add").WantSuccess(t)
	if got := showHost(t, h.Koko, "web2"); got.Note != nil {
		t.Errorf("web2 came back with the Note %q from a list without Notes", *got.Note)
	}
	r := h.Koko("host", "show", "web1", "--json")
	r.WantSuccess(t)
	if !strings.Contains(r.Stdout, `"note": null`) || !strings.Contains(r.Stdout, `"price": 9.9,`) {
		t.Errorf("web1 from a list without Notes came back as\n%s\nwant its typed Attributes and no Note", r.Stdout)
	}
}
