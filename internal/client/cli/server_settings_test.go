package cli_test

import (
	"encoding/json/v2"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/cieons/koko/internal/client/cli"
	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/shared/api"
)

// serverOut is the koko server's settings, as koko server show --json and
// koko server set --json print them.
type serverOut struct {
	Address            string `json:"address"`
	ListenPort         int    `json:"listen_port"`
	HostKeyFingerprint string `json:"host_key_fingerprint"`
	Version            string `json:"version"`
	RootKeyFile        string `json:"root_key_file"`
	RootKeyID          string `json:"root_key_id"`
	DataDir            string `json:"data_dir"`
}

// parseServer wants r, a koko server show or set with --json, to have
// succeeded, and returns what it printed.
func parseServer(t *testing.T, r harness.Result) serverOut {
	t.Helper()
	r.WantSuccess(t)
	var s serverOut
	if err := json.Unmarshal([]byte(r.Stdout), &s, json.RejectUnknownMembers(true)); err != nil {
		t.Fatalf("printed %q: %v", r.Stdout, err)
	}
	return s
}

// knownServer is a koko server started on a data directory, a Root Key
// file and a port the test chose, so that it can tell what server show
// prints and take those out of a golden file.
type knownServer struct {
	*harness.Harness
	dataDir, keyFile, port string
	// key is the Root Key file's content.
	key []byte
}

func startKnownServer(t *testing.T) knownServer {
	t.Helper()
	k := knownServer{dataDir: t.TempDir(), keyFile: harness.NewRootKeyFile(t)}
	listen := harness.FreeAddr(t)
	_, k.port, _ = net.SplitHostPort(listen)
	k.Harness = harness.Start(t, harness.DataDir(k.dataDir), harness.RootKeyFile(k.keyFile), harness.Listen(listen))
	var err error
	if k.key, err = os.ReadFile(k.keyFile); err != nil {
		t.Fatal(err)
	}
	return k
}

// wantNoKeyInLog fails the test if the koko server's log holds the key in
// its Root Key file.
func (k knownServer) wantNoKeyInLog(t *testing.T) {
	t.Helper()
	wantNoRootKey(t, harness.Result{Stderr: k.Log()}, k.key)
}

// version is the version koko version prints, of the binary the test runs.
func (k knownServer) version(t *testing.T) string {
	t.Helper()
	var v struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal([]byte(k.Koko("version", "--json").Stdout), &v); err != nil {
		t.Fatal(err)
	}
	return v.Version
}

// golden checks what server show or set printed against testdata/name,
// with the paths, the port and the version, which depend on the test,
// replaced.
func (k knownServer) golden(t *testing.T, name, out string) {
	t.Helper()
	harness.Golden(t, name, strings.NewReplacer(
		`"data_dir": "`+k.dataDir+`"`, `"data_dir": "<data-dir>"`,
		`"root_key_file": "`+k.keyFile+`"`, `"root_key_file": "<root-key-file>"`,
		`"listen_port": `+k.port+",", `"listen_port": <port>,`,
		`"version": "`+k.version(t)+`"`, `"version": "<version>"`,
	).Replace(out))
}

// rootKeyID returns the id a Root Key file's content names, before its key.
func rootKeyID(t *testing.T, file []byte) string {
	t.Helper()
	id, _, ok := strings.Cut(string(file), ":")
	if !ok {
		t.Fatal("the Root Key file does not hold <id>:<key>")
	}
	return id
}

// koko server show prints the koko server's settings: the Server Address
// as stored, the port it listens on, the host key devices pin, its version,
// the Root Key's file and id, and the data directory; never the Root Key.
func TestServerShowPrintsTheSettings(t *testing.T) {
	k := startKnownServer(t)
	r := k.Koko("server", "show", "--json")
	wantNoRootKey(t, r, k.key)
	s := parseServer(t, r)
	pinned := fingerprintOf(decode(t, deviceAdd(t, k.Harness).Code))
	want := serverOut{
		Address:            harness.Address,
		ListenPort:         mustAtoi(t, k.port),
		HostKeyFingerprint: pinned,
		Version:            k.version(t),
		RootKeyFile:        k.keyFile,
		RootKeyID:          rootKeyID(t, k.key),
		DataDir:            k.dataDir,
	}
	if s != want {
		t.Errorf("server show --json = %+v, want %+v", s, want)
	}
	k.golden(t, "server-show.json", r.Stdout)

	// For people: the Server Address with its port written, and the rest.
	r = k.Koko("server", "show")
	r.WantSuccess(t)
	wantNoRootKey(t, r, k.key)
	for _, line := range []string{
		"Server Address:  " + harness.Address + ":" + k.port,
		"Listen port:     " + k.port,
		"Host key:        " + pinned,
		"Version:         " + want.Version,
		"Root Key:        " + k.keyFile + " (" + want.RootKeyID + ")",
		"Data directory:  " + k.dataDir,
	} {
		if !strings.Contains(r.Stdout, line+"\n") {
			t.Errorf("server show printed %q, want the line %q", r.Stdout, line)
		}
	}

	// Nor does the API's own answer, or the koko server's log, hold the key.
	resp, err := k.Call(api.ServerShow, api.ServerShowRequest{})
	if err != nil {
		t.Fatalf("server.show: %v", err)
	}
	body, err := json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}
	wantNoRootKey(t, harness.Result{Stdout: string(body)}, k.key)
	k.wantNoKeyInLog(t)
}

// Under systemd, the koko server reads the copy of the Root Key that
// LoadCredential= hands it, but koko server show names the file the Owner
// keeps, the one install wrote and printed: /etc/koko/root.key.
func TestServerShowNamesTheRootKeyFileInstallWrote(t *testing.T) {
	m := harness.NewMachine(t)
	installed := install(t, m, "--owner", "alice")
	key, err := os.ReadFile(m.Path("/etc/koko/root.key"))
	if err != nil {
		t.Fatal(err)
	}
	r := m.Koko("server", "show", "--json")
	wantNoRootKey(t, r, key)
	s := parseServer(t, r)
	if s.RootKeyFile != installed.RootKeyFile || s.RootKeyID != installed.RootKeyID {
		t.Errorf("server show names the Root Key %s (%s), want %s (%s), as install printed", s.RootKeyFile, s.RootKeyID, installed.RootKeyFile, installed.RootKeyID)
	}
	if s.Address != installed.Address || s.HostKeyFingerprint != installed.HostKeyFingerprint {
		t.Errorf("server show = %+v, want the Server Address and host key install printed: %+v", s, installed)
	}
	// The fake machine's koko server runs under its root.
	if s.DataDir != m.Path("/var/lib/koko") {
		t.Errorf("server show names the data directory %s, want /var/lib/koko under the machine's root", s.DataDir)
	}
}

// koko server set address= changes the Server Address and prints the
// settings as they are now; server show shows the change.
func TestServerSetAddress(t *testing.T) {
	k := startKnownServer(t)
	r := k.Koko("server", "set", "address=koko.example.com:8443", "--json")
	wantNoRootKey(t, r, k.key)
	s := parseServer(t, r)
	if s.Address != "koko.example.com:8443" {
		t.Errorf("server set printed the Server Address %q, want koko.example.com:8443", s.Address)
	}
	k.golden(t, "server-set.json", r.Stdout)
	if shown := parseServer(t, k.Koko("server", "show", "--json")); shown != s {
		t.Errorf("server show = %+v after server set printed %+v", shown, s)
	}

	// For people: what changed, which codes carry it, and how a joined
	// device takes it. Without a port, the address's port is the listen port.
	r = k.Koko("server", "set", "address=koko.example.com")
	r.WantSuccess(t)
	wantNoRootKey(t, r, k.key)
	for _, want := range []string{
		"Changed the Server Address to koko.example.com:" + k.port + ".\n",
		"koko connect koko.example.com:" + k.port + "\n",
	} {
		if !strings.Contains(r.Stdout, want) {
			t.Errorf("server set printed %q, want %q", r.Stdout, want)
		}
	}

	// server.set sets only what it names: naming nothing changes nothing.
	resp, err := k.Call(api.ServerSet, api.ServerSetRequest{})
	if err != nil || resp.Server.Address != "koko.example.com" {
		t.Errorf("server.set naming nothing = %+v, %v; want the Server Address koko.example.com unchanged", resp.Server, err)
	}
	k.wantNoKeyInLog(t)
}

// koko server show only reads, and koko server set changes the record in a
// way another server set undoes (ADR 0019), so a harness can allow the one
// and ask before the other.
func TestServerShowAndSetSitInTheirTiers(t *testing.T) {
	want := map[string]cli.Tier{"server show": cli.Read, "server set": cli.Reversible}
	for _, c := range cli.Tree() {
		if tier, ok := want[c.Path()]; ok {
			if c.Tier != tier {
				t.Errorf("koko %s is in the %v tier, want %v", c.Path(), c.Tier, tier)
			}
			delete(want, c.Path())
		}
	}
	if len(want) != 0 {
		t.Errorf("the command tree lacks %v", want)
	}
}

// A changed Server Address affects only the Join Codes issued afterwards: a
// code issued before still carries the old address and joins there, and
// one issued after leads its device to the new address.
func TestServerSetAddressChangesOnlyLaterJoinCodes(t *testing.T) {
	h := harness.Start(t)
	before := deviceAdd(t, h)
	old := decode(t, before.Code).Address
	moved := harness.Forward(t, old)
	h.Koko("server", "set", "address="+moved.Addr()).WantSuccess(t)
	after := deviceAdd(t, h)

	if got := decode(t, before.Code).Address; got != old {
		t.Errorf("the Join Code issued before carries %s, want %s", got, old)
	}
	if got := decode(t, after.Code).Address; got != moved.Addr() {
		t.Errorf("the Join Code issued after carries %s, want the new Server Address %s", got, moved.Addr())
	}
	harness.NewDevice(t).Koko("join", before.Code).WantSuccess(t)
	if moved.Conns() != 0 {
		t.Errorf("joining with the code issued before went through the new address")
	}
	harness.NewDevice(t).Koko("join", after.Code).WantSuccess(t)
	if moved.Conns() == 0 {
		t.Errorf("joining with the code issued after did not go through the new address")
	}
}

// koko server set refuses with exit 2, before any call, what could never
// change a setting: no setting named, a setting it does not have, and a
// value that is not a Server Address.
func TestServerSetRefusesAnythingButAServerAddress(t *testing.T) {
	h := harness.Start(t)
	for name, args := range map[string][]string{
		"no setting":             {},
		"a setting it lacks":     {"port=8443"},
		"a URL":                  {"address=https://koko.example.com"},
		"a port out of range":    {"address=koko.example.com:65536"},
		"an empty address":       {"address="},
		"the address twice":      {"address=a.example.com", "address=b.example.com"},
		"an address to unset":    {"--unset", "address"},
		"an argument, not a key": {"koko.example.com"},
	} {
		t.Run(name, func(t *testing.T) {
			h.Koko(append([]string{"server", "set", "--json"}, args...)...).WantError(t, 2, "invalid_usage")
		})
	}
	if n := h.Calls(); n != 0 {
		t.Errorf("the refused command lines made %d calls, want none", n)
	}
}

// A Server Address a Join Code cannot carry, 255 bytes with its port, is
// refused by the koko server and leaves the Server Address as it was.
func TestServerSetRefusesAnAddressNoJoinCodeCarries(t *testing.T) {
	h := harness.Start(t)
	label := strings.Repeat("a", 63)
	long := label + "." + label + "." + label + "." + label[:61] + ":65535" // a 253-byte name
	msg := h.Koko("server", "set", "address="+long, "--json").WantError(t, 1, "invalid_address")
	if !strings.Contains(msg, "255") {
		t.Errorf("message %q does not say how long an address a Join Code carries", msg)
	}
	if s := parseServer(t, h.Koko("server", "show", "--json")); s.Address != harness.Address {
		t.Errorf("after the refusal the Server Address is %s, want %s", s.Address, harness.Address)
	}
}

// Over a Device Connection, server set is refused with local_only, the
// Owner's own device included, and changes nothing; server show answers
// every device.
func TestServerSetAnswersOnlyOnTheKokoServersMachine(t *testing.T) {
	h := harness.Start(t)
	h.Koko("user", "invite", "bob").WantSuccess(t)
	devices := map[string]*harness.Device{"the Owner's device": h.Join(harness.Owner), "a Member's device": h.Join("bob")}
	local := parseServer(t, h.Koko("server", "show", "--json"))
	for name, d := range devices {
		msg := d.Koko("server", "set", "address=koko.example.com", "--json").WantError(t, 1, "local_only")
		if !strings.Contains(msg, "koko server's machine") {
			t.Errorf("%s: message %q does not say where it answers", name, msg)
		}
		if s := parseServer(t, d.Koko("server", "show", "--json")); s != local {
			t.Errorf("%s: server show = %+v, want %+v, as over the Unix socket", name, s, local)
		}
	}
}

// No browser tab changes where devices connect: koko web on the koko
// server's machine reaches the API over the Unix socket, as the Local
// caller, yet server.set through it is refused with local_only and changes
// nothing, while server show answers.
func TestServerSetRefusesABrowserTab(t *testing.T) {
	h := harness.Start(t)
	w := startWeb(t, h)
	b := newBrowser(t, w.port)
	b.open(w)
	wantAPIError(t, b.postJSON("/v1/server.set", `{"address": "moved.example.com"}`), http.StatusForbidden, "local_only")
	if show := b.postJSON("/v1/server.show", "{}"); show.StatusCode != http.StatusOK || !strings.Contains(show.body, `"address":"`+harness.Address+`"`) {
		t.Errorf("server.show through koko web = %s %q, want the Server Address unchanged", show.Status, show.body)
	}
}

func mustAtoi(t *testing.T, s string) int {
	t.Helper()
	n, err := strconv.Atoi(s)
	if err != nil {
		t.Fatal(err)
	}
	return n
}
