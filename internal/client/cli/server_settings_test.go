package cli_test

import (
	"encoding/json/v2"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cieons/koko/internal/client/cli"
	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/server"
	"github.com/cieons/koko/internal/server/installer"
	"github.com/cieons/koko/internal/server/transport"
	"github.com/cieons/koko/internal/shared/api"
	"github.com/cieons/koko/internal/shared/wire"
)

// serverOut is the koko server's settings, as koko server show --json and
// koko server set --json print them.
type serverOut struct {
	Address            string          `json:"address"`
	ListenPort         int             `json:"listen_port"`
	HostKeyFingerprint string          `json:"host_key_fingerprint"`
	Version            string          `json:"version"`
	RootKeyFile        string          `json:"root_key_file"`
	RootKeyID          string          `json:"root_key_id"`
	DataDir            string          `json:"data_dir"`
	WebURL             *string         `json:"web_url"`
	WebListen          *string         `json:"web_listen"`
	UpdateCheck        updateCheckOut  `json:"update_check"`
	UpgradeWay         string          `json:"upgrade_way"`
	LastUpgrade        *lastUpgradeOut `json:"last_upgrade"`
}

// lastUpgradeOut is the outcome of the last upgrade, as koko server show
// --json prints it.
type lastUpgradeOut struct {
	State      string  `json:"state"`
	From       string  `json:"from"`
	To         string  `json:"to"`
	StartedAt  string  `json:"started_at"`
	FinishedAt *string `json:"finished_at"`
	Message    *string `json:"message"`
}

// updateCheckOut is the koko server's check for a new release, as koko
// server show --json prints it.
type updateCheckOut struct {
	Enabled   bool    `json:"enabled"`
	CheckedAt *string `json:"checked_at"`
	Latest    *string `json:"latest"`
	Error     *string `json:"error"`
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

func startKnownServer(t *testing.T, opts ...harness.Option) knownServer {
	t.Helper()
	k := knownServer{dataDir: t.TempDir(), keyFile: harness.NewRootKeyFile(t)}
	listen := harness.FreeAddr(t)
	_, k.port, _ = net.SplitHostPort(listen)
	k.Harness = harness.Start(t, append([]harness.Option{harness.DataDir(k.dataDir), harness.RootKeyFile(k.keyFile), harness.Listen(listen)}, opts...)...)
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

// goldenText checks what server show printed for people against
// testdata/name, with the paths, the port, the Root Key's id and the
// version, which depend on the test, replaced.
func (k knownServer) goldenText(t *testing.T, name, out string) {
	t.Helper()
	harness.Golden(t, name, strings.NewReplacer(
		k.dataDir, "<data-dir>",
		k.keyFile, "<root-key-file>",
		rootKeyID(t, k.key), "rootkey_<1>",
		harness.Address+":"+k.port, harness.Address+":<port>",
		"Listen port:     "+k.port, "Listen port:     <port>",
		"Version:         "+k.version(t), "Version:         <version>",
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
	t.Parallel()
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
		UpdateCheck:        updateCheckOut{Enabled: true},
		UpgradeWay:         "none",
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
		"Web listener:    off",
		"Web URL:         not set",
		"Host key:        " + pinned,
		"Version:         " + want.Version,
		"Root Key:        " + k.keyFile + " (" + want.RootKeyID + ")",
		"Data directory:  " + k.dataDir,
		"Update check:    on, not checked yet",
		"Upgrade:         replace kokod and restart it",
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
		"update_check not on":    {"update_check=yes"},
		"update_check empty":     {"update_check="},
		"update_check to unset":  {"--unset", "update_check"},
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
	h := harness.Start(t)
	w := startWeb(t, h)
	b := newBrowser(t, w.port)
	b.open(w)
	wantAPIError(t, b.postJSON("/v1/server.set", `{"address": "moved.example.com"}`), http.StatusForbidden, "local_only")
	if show := b.postJSON("/v1/server.show", "{}"); show.StatusCode != http.StatusOK || !strings.Contains(show.body, `"address":"`+harness.Address+`"`) {
		t.Errorf("server.show through koko web = %s %q, want the Server Address unchanged", show.Status, show.body)
	}
}

// koko server set web_url= sets the Web URL as stored, lowercased and
// without the default port, and --unset web_url clears it; server show
// names it and the web listener's address, both null while unset or off.
func TestServerSetWebURL(t *testing.T) {
	t.Parallel()
	h := harness.Start(t, harness.WebListen("127.0.0.1:0"))
	s := parseServer(t, h.Koko("server", "set", "web_url=HTTPS://Koko.Example.COM:443/", "--json"))
	if s.WebURL == nil || *s.WebURL != "https://koko.example.com" || s.WebListen == nil || *s.WebListen != h.WebAddress() {
		t.Errorf("server set printed the Web URL %v and the web listener %v, want https://koko.example.com and %s", s.WebURL, s.WebListen, h.WebAddress())
	}
	if s.Address != harness.Address {
		t.Errorf("setting the Web URL changed the Server Address to %s", s.Address)
	}
	if shown := parseServer(t, h.Koko("server", "show", "--json")); *shown.WebURL != *s.WebURL || *shown.WebListen != *s.WebListen {
		t.Errorf("server show = %+v after server set printed %+v", shown, s)
	}

	// For people: what changed, and server show's lines.
	r := h.Koko("server", "set", "web_url=https://koko.example.com:8443")
	r.WantSuccess(t)
	if want := "Changed the Web URL to https://koko.example.com:8443. "; !strings.HasPrefix(r.Stdout, want) || strings.Contains(r.Stdout, "Server Address") {
		t.Errorf("server set printed %q, want only %q and what it does", r.Stdout, want)
	}
	r = h.Koko("server", "show")
	for _, line := range []string{"Web listener:    " + h.WebAddress() + "\n", "Web URL:         https://koko.example.com:8443\n"} {
		if !strings.Contains(r.Stdout, line) {
			t.Errorf("server show printed %q, want the line %q", r.Stdout, line)
		}
	}

	r = h.Koko("server", "set", "--unset", "web_url")
	r.WantSuccess(t)
	if !strings.HasPrefix(r.Stdout, "Cleared the Web URL.") {
		t.Errorf("server set --unset web_url printed %q", r.Stdout)
	}
	if s := parseServer(t, h.Koko("server", "show", "--json")); s.WebURL != nil {
		t.Errorf("after --unset web_url, server show names the Web URL %s", *s.WebURL)
	}
}

// With the web listener off, koko server set web_url= says how to turn it
// on.
func TestServerSetWebURLSaysTheWebListenerIsOff(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	r := h.Koko("server", "set", "web_url=https://koko.example.com")
	r.WantSuccess(t)
	if !strings.Contains(r.Stdout, "The web listener is off: turn it on with koko server install --web-listen 127.0.0.1:7823") {
		t.Errorf("server set printed %q, want how to turn the web listener on", r.Stdout)
	}
	if s := parseServer(t, h.Koko("server", "show", "--json")); s.WebListen != nil {
		t.Errorf("server show names the web listener %s though it is off", *s.WebListen)
	}
}

// What is not an https:// origin is refused as a Web URL: by koko server
// set with exit 2 before any call, and by server.set with invalid_web_url;
// neither changes the Web URL. The Server Address is never cleared.
func TestServerSetRefusesWhatIsNotAWebURL(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	h.Koko("server", "set", "web_url=https://koko.example.com").WantSuccess(t)
	calls := h.Calls()
	for name, args := range map[string][]string{
		"http://":                 {"web_url=http://koko.example.com"},
		"no scheme":               {"web_url=koko.example.com"},
		"a path":                  {"web_url=https://example.com/koko"},
		"a query":                 {"web_url=https://koko.example.com/?a=b"},
		"user information":        {"web_url=https://alice@koko.example.com"},
		"a port out of range":     {"web_url=https://koko.example.com:65536"},
		"empty":                   {"web_url="},
		"set and cleared":         {"web_url=https://koko.example.com", "--unset", "web_url"},
		"the address cleared":     {"--unset", "address"},
		"a setting it lacks":      {"--unset", "listen_port"},
		"the Web URL given twice": {"web_url=https://a.example.com", "web_url=https://b.example.com"},
	} {
		t.Run(name, func(t *testing.T) {
			h.Koko(append([]string{"server", "set", "--json"}, args...)...).WantError(t, 2, "invalid_usage")
		})
	}
	if n := h.Calls() - calls; n != 0 {
		t.Errorf("the refused command lines made %d calls, want none", n)
	}

	for _, u := range []string{"http://koko.example.com", "https://example.com/koko", "https://koko.example.com#top", ""} {
		_, err := h.Call(api.ServerSet, api.ServerSetRequest{WebURL: new(u)})
		if e, ok := errors.AsType[*api.Error](err); !ok || e.Code != "invalid_web_url" {
			t.Errorf("server.set web_url=%q = %v, want invalid_web_url", u, err)
		}
	}
	for _, unset := range [][]string{{"address"}, {"listen_port"}} {
		_, err := h.Call(api.ServerSet, api.ServerSetRequest{Unset: unset})
		if e, ok := errors.AsType[*api.Error](err); !ok || e.Code != "invalid_request" {
			t.Errorf("server.set unsetting %v = %v, want invalid_request", unset, err)
		}
	}
	_, err := h.Call(api.ServerSet, api.ServerSetRequest{WebURL: new("https://b.example.com"), Unset: []string{"web_url"}})
	if e, ok := errors.AsType[*api.Error](err); !ok || e.Code != "invalid_request" {
		t.Errorf("server.set setting and clearing the Web URL = %v, want invalid_request", err)
	}
	if s := parseServer(t, h.Koko("server", "show", "--json")); s.WebURL == nil || *s.WebURL != "https://koko.example.com" {
		t.Errorf("after the refusals the Web URL is %v, want https://koko.example.com", s.WebURL)
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

// koko server set update_check=off turns the koko server's check for a new
// release off, and update_check=on back on; server show shows each.
func TestServerSetUpdateCheckOffAndOn(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	off := parseServer(t, h.Koko("server", "set", "update_check=off", "--json"))
	if off.UpdateCheck != (updateCheckOut{}) {
		t.Errorf("server set update_check=off printed %+v, want it off with nothing found", off.UpdateCheck)
	}
	if shown := parseServer(t, h.Koko("server", "show", "--json")); shown != off {
		t.Errorf("server show = %+v after server set printed %+v", shown, off)
	}
	r := h.Koko("server", "show")
	r.WantSuccess(t)
	if !strings.Contains(r.Stdout, "Update check:    off, not checked yet\n") {
		t.Errorf("server show printed %q, want the update check off", r.Stdout)
	}
	r = h.Koko("server", "set", "update_check=on")
	r.WantSuccess(t)
	if want := "Turned the update check on. The koko server checks for a new koko release a few minutes from now, then every 6 hours.\n"; r.Stdout != want {
		t.Errorf("server set update_check=on printed %q, want %q", r.Stdout, want)
	}
	if on := parseServer(t, h.Koko("server", "show", "--json")); on.UpdateCheck != (updateCheckOut{Enabled: true}) {
		t.Errorf("server show printed %+v, want the update check on", on.UpdateCheck)
	}
}

// cannedServer serves on a socket of its own a koko server that answers
// server.show with show, a server.show answer's "server" object, through
// the real transport, and returns the socket's path.
func cannedServer(t *testing.T, show string) string {
	t.Helper()
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/server.show" {
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"error": {"code": "unknown_operation", "message": "no operation here"}}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"server": `+show+`}`)
	})
	path := filepath.Join(harness.ShortTempDir(t), "koko.sock")
	report := wire.Report{Version: "v0.4.0", Operations: []string{"server.show"}}
	s := transport.New(h, h, func() wire.Report { return report }, slog.New(slog.DiscardHandler))
	if err := s.ListenSocket(t.Context(), path); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return path
}

// koko server show says when the koko server last checked for a new
// release and what it found, set against the version it runs: a newer
// release, the release it runs, or why the check failed.
func TestServerShowSaysWhatTheUpdateCheckFound(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, updateCheck, want string
	}{
		{"not checked", `{"enabled": true, "checked_at": null, "latest": null, "error": null}`, "on, not checked yet"},
		{"a newer release", `{"enabled": true, "checked_at": "2026-11-11T09:00:30Z", "latest": "v0.5.0", "error": null}`, "on, last at 2026-11-11 09:00 UTC: v0.5.0 is available"},
		{"the release it runs", `{"enabled": true, "checked_at": "2026-11-11T09:00:30Z", "latest": "v0.4.0", "error": null}`, "on, last at 2026-11-11 09:00 UTC: v0.4.0 is the latest release"},
		{"a failure", `{"enabled": true, "checked_at": "2026-11-11T09:00:30Z", "latest": "v0.5.0", "error": "koko could not find its latest release: …"}`, "on, last at 2026-11-11 09:00 UTC, which failed: koko could not find its latest release: …"},
		{"the check off", `{"enabled": false, "checked_at": "2026-11-11T15:04:00Z", "latest": "v0.5.0", "error": null}`, "off, last at 2026-11-11 15:04 UTC: v0.5.0 is available"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			socket := cannedServer(t, `{"address": "koko.example.com", "listen_port": 7822, "version": "v0.4.0", "update_check": `+tc.updateCheck+`}`)
			r := harness.Run(t, cli.Runner{SocketPath: socket}, nil, "server", "show")
			r.WantSuccess(t)
			if want := "Update check:    " + tc.want + "\n"; !strings.Contains(r.Stdout, want) {
				t.Errorf("server show printed %q, want the line %q", r.Stdout, want)
			}
		})
	}
}

// What the real koko server's check found reaches koko server show: here,
// a failure, then the latest release, which this test's build, (devel),
// is no older than.
func TestServerShowSaysWhatTheKokoServersCheckFound(t *testing.T) {
	t.Parallel()
	rel := harness.NewMachine(t).Release("v0.5.0")
	h := harness.Start(t, harness.UpdateCheck(rel))
	h.SetNow(time.Date(2026, 11, 11, 9, 0, 30, 0, time.UTC))
	line := func() string {
		t.Helper()
		r := h.Koko("server", "show")
		r.WantSuccess(t)
		_, line, _ := strings.Cut(r.Stdout, "Update check:    ")
		line, _, _ = strings.Cut(line, "\n")
		return line
	}
	if _, err := h.Call(api.ServerCheckUpdate, api.ServerCheckUpdateRequest{}); err == nil {
		t.Fatal("server.check_update succeeded with no latest release")
	}
	if got, want := line(), "on, last at 2026-11-11 09:00 UTC, which failed: koko could not find its latest release: "; !strings.HasPrefix(got, want) || !strings.Contains(got, harness.LatestPath()) {
		t.Errorf("after a failed check, server show printed the update check %q, want it to begin %q and name %s", got, want, harness.LatestPath())
	}
	rel.Latest()
	if _, err := h.Call(api.ServerCheckUpdate, api.ServerCheckUpdateRequest{}); err != nil {
		t.Fatal(err)
	}
	if got, want := line(), "on, last at 2026-11-11 09:00 UTC: v0.5.0 is the latest release"; got != want {
		t.Errorf("server show printed the update check %q, want %q", got, want)
	}
}

// koko server show names how the koko server is upgraded, in its JSON
// and in its Upgrade line: in place where the upgrade helper's path unit
// is (systemd), by a new image where KOKO_IMAGE names one (image), and by
// hand otherwise (none); and the outcome of the last upgrade, null without
// one.
func TestServerShowSaysHowTheKokoServerUpgrades(t *testing.T) {
	t.Parallel()
	helperDir := t.TempDir()
	helper := filepath.Join(helperDir, "koko-upgrade.path")
	if err := os.WriteFile(helper, []byte("[Path]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	outcome := filepath.Join(helperDir, "outcome.json")
	if err := os.WriteFile(outcome, []byte(`{"state": "failed", "from": "v0.4.0", "to": "v0.5.0", "started_at": "2026-11-11T09:00:00Z", "finished_at": "2026-11-11T09:01:30Z", "message": "the new kokod did not start: the previous kokod runs again"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		way     string
		upgrade server.Upgrade
	}{
		{"systemd", server.Upgrade{HelperUnit: helper, OutcomeFile: outcome}},
		{"image", server.Upgrade{HelperUnit: filepath.Join(helperDir, "none.path"), Image: "ghcr.io/cieons/koko"}},
		{"none", server.Upgrade{HelperUnit: filepath.Join(helperDir, "none.path"), OutcomeFile: filepath.Join(helperDir, "none.json")}},
	} {
		t.Run(tc.way, func(t *testing.T) {
			t.Parallel()
			k := startKnownServer(t, harness.Upgrade(tc.upgrade))
			r := k.Koko("server", "show", "--json")
			if s := parseServer(t, r); s.UpgradeWay != tc.way {
				t.Errorf("server show names the upgrade way %q, want %q", s.UpgradeWay, tc.way)
			}
			// A koko server run by hand is server-show.json's.
			name := "server-show-" + tc.way
			if tc.way == "none" {
				name = "server-show"
			}
			k.golden(t, name+".json", r.Stdout)
			r = k.Koko("server", "show")
			r.WantSuccess(t)
			k.goldenText(t, name+".txt", r.Stdout)
		})
	}
}

// On a machine where install wrote the upgrade helper's path unit, kokod
// run finds it, and the helper's outcome.json, where the helper keeps
// them. A machine without the path unit is TestServerShowSaysHowTheKokoServerUpgrades's.
func TestServerShowFindsTheUpgradeHelperInstallWrote(t *testing.T) {
	t.Parallel()
	m := harness.NewMachine(t)
	install(t, m, "--owner", "alice")
	if s := parseServer(t, m.Koko("server", "show", "--json")); s.UpgradeWay != "systemd" || s.LastUpgrade != nil {
		t.Errorf("before any upgrade, server show names the upgrade way %q and the last upgrade %+v; want systemd and null", s.UpgradeWay, s.LastUpgrade)
	}
	if err := os.WriteFile(m.Path(installer.OutcomeFile), []byte(`{"state": "running", "from": "v0.4.0", "to": "v0.5.0", "started_at": "2026-11-11T09:00:00Z"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	s := parseServer(t, m.Koko("server", "show", "--json"))
	want := lastUpgradeOut{State: "running", From: "v0.4.0", To: "v0.5.0", StartedAt: "2026-11-11T09:00:00Z"}
	if s.UpgradeWay != "systemd" || s.LastUpgrade == nil || *s.LastUpgrade != want {
		t.Errorf("server show names the upgrade way %q and the last upgrade %+v; want systemd and %+v", s.UpgradeWay, s.LastUpgrade, want)
	}
}

// In the image, the Upgrade line names the release to set KOKO_VERSION to
// once the koko server's check has found a newer one.
func TestServerShowNamesTheImageToPull(t *testing.T) {
	t.Parallel()
	socket := cannedServer(t, `{"address": "koko.example.com", "listen_port": 7822, "version": "v0.4.0", "upgrade_way": "image",
		"update_check": {"enabled": true, "checked_at": "2026-11-11T09:00:30Z", "latest": "v0.5.0", "error": null}}`)
	r := harness.Run(t, cli.Runner{SocketPath: socket}, nil, "server", "show")
	r.WantSuccess(t)
	if want := "Upgrade:         pull the new image: set KOKO_VERSION=0.5.0 in .env, then docker compose pull && docker compose up -d\n"; !strings.Contains(r.Stdout, want) {
		t.Errorf("server show printed %q, want the line %q", r.Stdout, want)
	}
}

// An outcome.json that does not parse is unknown, not "no upgrade ran": server
// show fails rather than answer a null last upgrade.
func TestServerShowFailsOnAnOutcomeItCannotRead(t *testing.T) {
	t.Parallel()
	outcome := filepath.Join(t.TempDir(), "outcome.json")
	if err := os.WriteFile(outcome, []byte(`{"state": "succ`), 0o644); err != nil {
		t.Fatal(err)
	}
	h := harness.Start(t, harness.Upgrade(server.Upgrade{OutcomeFile: outcome}))
	h.Koko("server", "show", "--json").WantError(t, 1, "internal_error")
	if !strings.Contains(h.Log(), outcome) {
		t.Errorf("the koko server's log does not name %s:\n%s", outcome, h.Log())
	}
}
