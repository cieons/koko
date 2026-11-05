package cli_test

import (
	"encoding/json/v2"
	"io/fs"
	"net/netip"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cieons/koko/internal/client/cli"
	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/shared/joincode"
	"github.com/cieons/koko/internal/shared/portowner"
)

// installed is what koko server install --json prints.
type installed struct {
	Upgraded           bool        `json:"upgraded"`
	Address            string      `json:"address"`
	HostKeyFingerprint string      `json:"host_key_fingerprint"`
	RootKeyFile        string      `json:"root_key_file"`
	RootKeyID          string      `json:"root_key_id"`
	ListenPort         int         `json:"listen_port"`
	JoinCode           joinCodeOut `json:"join_code"`
	DevicesMustConnect string      `json:"devices_must_connect"`
}

// install runs koko server install --json on m with args, wants it to
// succeed, and returns what it printed.
func install(t *testing.T, m *harness.Machine, args ...string) installed {
	t.Helper()
	r := m.Koko(append([]string{"server", "install", "--json"}, args...)...)
	r.WantSuccess(t)
	var out installed
	if err := json.Unmarshal([]byte(r.Stdout), &out); err != nil {
		t.Fatalf("server install --json printed %q: %v", r.Stdout, err)
	}
	return out
}

// wantMode fails the test unless the file at path under m's root has mode.
func wantMode(t *testing.T, m *harness.Machine, path string, mode fs.FileMode) {
	t.Helper()
	info, err := os.Stat(m.Path(path))
	if err != nil {
		t.Errorf("%s: %v", path, err)
		return
	}
	if got := info.Mode() & (fs.ModePerm | fs.ModeDir); got != mode {
		t.Errorf("%s has mode %v, want %v", path, got, mode)
	}
}

// listenPort returns the port the koko server running on m listens on, as
// its log names it once initialised. The fake machine's koko server listens
// on a loopback port of the test's choosing, never the unit's, so its Join
// Codes name that port where a real machine's name the unit's.
func listenPort(t *testing.T, m *harness.Machine) string {
	t.Helper()
	match := regexp.MustCompile(`msg="koko server initialised" .*listen=127\.0\.0\.1:(\d+)`).FindStringSubmatch(m.Log())
	if match == nil {
		t.Fatalf("the koko server's log names no TCP listener:\n%s", m.Log())
	}
	return match[1]
}

// wantInitialised fails the test unless the koko server running on m is
// initialised with owner as its Owner: its Personal Space is listed over
// the socket.
func wantInitialised(t *testing.T, m *harness.Machine, owner string) {
	t.Helper()
	r := m.Koko("space", "ls", "--json")
	r.WantSuccess(t)
	if !strings.Contains(r.Stdout, `"name": "`+owner+`"`) {
		t.Errorf("space ls = %s, want %s's Personal Space", r.Stdout, owner)
	}
}

// wantNoRootKey fails the test if what install printed, on stdout or stderr,
// holds the key in the Root Key file whose content is file. The id before
// it is printed on purpose.
func wantNoRootKey(t *testing.T, r harness.Result, file []byte) {
	t.Helper()
	_, key, ok := strings.Cut(strings.TrimSpace(string(file)), ":")
	if !ok || key == "" {
		t.Fatalf("the Root Key file does not hold <id>:<key>")
	}
	if strings.Contains(r.Stdout+r.Stderr, key) {
		t.Error("install printed the Root Key") // and this test does not print it either
	}
}

// On a fresh machine, install creates the koko user and the directories,
// writes the Root Key, the binary and the unit, starts the service and
// initialises the koko server over its socket, then prints what the Owner
// needs next.
func TestServerInstallOnAFreshMachine(t *testing.T) {
	m := harness.NewMachine(t)
	r := m.Koko("server", "install", "--owner", "alice", "--json")
	r.WantSuccess(t)
	harness.Golden(t, "server-install.json", r.Stdout)
	var out installed
	if err := json.Unmarshal([]byte(r.Stdout), &out); err != nil {
		t.Fatal(err)
	}

	// The files, with their modes and owners.
	for path, want := range map[string]struct {
		mode  fs.FileMode
		owner string
	}{
		"/usr/local/bin/koko":              {0o755, "root"},
		"/etc/systemd/system/koko.service": {0o644, "root"},
		"/etc/koko":                        {fs.ModeDir | 0o700, "root"},
		"/etc/koko/root.key":               {0o600, "root"},
		"/var/lib/koko":                    {fs.ModeDir | 0o700, "koko"},
	} {
		wantMode(t, m, path, want.mode)
		if got := m.Owner(path); got != want.owner {
			t.Errorf("%s is owned by %q, want %q", path, got, want.owner)
		}
	}
	if users := m.Users(); !slices.Equal(users, []string{"koko"}) {
		t.Errorf("install created the users %v, want koko", users)
	}
	binary, err := os.ReadFile(m.Path("/usr/local/bin/koko"))
	if err != nil || string(binary) != string(m.Binary()) {
		t.Errorf("/usr/local/bin/koko holds %q (%v), want the binary that ran install", binary, err)
	}
	unit, err := os.ReadFile(m.Path("/etc/systemd/system/koko.service"))
	if err != nil {
		t.Fatal(err)
	}
	harness.Golden(t, "server-install-koko.service", string(unit))
	key, err := os.ReadFile(m.Path("/etc/koko/root.key"))
	if err != nil {
		t.Fatal(err)
	}
	if line, ok := strings.CutSuffix(string(key), "\n"); !ok || strings.Contains(line, "\n") || !strings.HasPrefix(line, out.RootKeyID+":") {
		t.Errorf("the Root Key file is not one line naming %s", out.RootKeyID)
	}

	// The service is enabled and started, and the koko server initialised.
	if !m.Enabled() || !m.Running() {
		t.Errorf("the koko service is enabled %v and running %v, want both", m.Enabled(), m.Running())
	}
	wantInitialised(t, m, "alice")

	// What install printed.
	if out.Address != harness.SourceIP || out.RootKeyFile != "/etc/koko/root.key" || out.ListenPort != 443 ||
		!strings.HasPrefix(out.HostKeyFingerprint, "SHA256:") || !strings.HasPrefix(out.RootKeyID, "rootkey_") {
		t.Errorf("install printed %+v", out)
	}
	jc := out.JoinCode
	code, err := joincode.Decode(jc.Code)
	if err != nil {
		t.Fatalf("decode the first Join Code: %v", err)
	}
	if port := listenPort(t, m); jc.User != "alice" || jc.Device != "device-1" || code.Address != harness.SourceIP+":"+port {
		t.Errorf("the first Join Code is %+v for %s, want alice's device-1 at %s:%s", jc, code.Address, harness.SourceIP, port)
	}
	wantNoRootKey(t, r, key)
}

// Install checks the machine before it writes anything, writes the Root Key,
// the binary and the unit before it starts the service, and waits for the
// service's socket, which opens late here, before it initialises.
func TestServerInstallChecksFirstAndWaitsForTheSocket(t *testing.T) {
	m := harness.NewMachine(t)
	fresh := m.Files()
	m.StartDelay = 300 * time.Millisecond
	install(t, m, "--owner", "alice")
	wantInitialised(t, m, "alice")

	calls := m.Calls()
	var whats []string
	for _, c := range calls {
		whats = append(whats, c.What)
	}
	firstWrite := slices.Index(whats, "useradd koko")
	if firstWrite < 0 {
		t.Fatalf("install never created the koko user: %v", whats)
	}
	for _, check := range []string{"systemd --version", "filesystem /var/lib", "port 443", "source ip"} {
		i := slices.Index(whats, check)
		switch {
		case i < 0:
			t.Errorf("install never asked %q: %v", check, whats)
		case i > firstWrite || !slices.Equal(calls[i].Files, fresh):
			t.Errorf("install asked %q after writing: %v", check, calls[i].Files)
		}
	}
	if got, want := whats[len(whats)-3:], []string{"systemctl daemon-reload", "systemctl enable koko.service", "systemctl restart koko.service"}; !slices.Equal(got, want) {
		t.Errorf("install ended with %v, want %v", got, want)
	}
	start := calls[len(calls)-1]
	for _, file := range []string{"/etc/koko/root.key", "/usr/local/bin/koko", "/etc/systemd/system/koko.service", "/var/lib/koko"} {
		if !slices.Contains(start.Files, file) {
			t.Errorf("install started the service before writing %s", file)
		}
	}
}

// For people, install says where the koko server is, what to keep and what
// to open, and the one koko join line for the Owner's first device; never
// the Root Key.
func TestServerInstallForPeople(t *testing.T) {
	m := harness.NewMachine(t)
	r := m.Koko("server", "install", "--owner", "alice")
	r.WantSuccess(t)
	key, err := os.ReadFile(m.Path("/etc/koko/root.key"))
	if err != nil {
		t.Fatal(err)
	}
	id, _, _ := strings.Cut(string(key), ":")
	for _, want := range []string{
		"Server Address:  " + harness.SourceIP + ":443\n",
		"Host key:        SHA256:",
		"Root Key:        /etc/koko/root.key (" + id + ")\n",
		"Keep a copy of /etc/koko/root.key off this machine",
		"open TCP port 443 in your provider's firewall",
		"within 15 minutes",
		"On alice's new device device-1, before ",
		"\nkoko join koko_",
	} {
		if !strings.Contains(r.Stdout, want) {
			t.Errorf("install printed\n%s\nwhich does not say %q", r.Stdout, want)
		}
	}
	wantNoRootKey(t, r, key)
}

// --port makes the koko server listen on another port: install checks that
// one, writes it into the unit and names it as the port to open.
func TestServerInstallOnAnotherPort(t *testing.T) {
	m := harness.NewMachine(t)
	m.Listening[443] = portowner.Process{PID: 812, Name: "nginx", User: "www-data"}
	out := install(t, m, "--owner", "alice", "--port", "8443")
	if out.ListenPort != 8443 {
		t.Errorf("install printed the listen port %d, want 8443", out.ListenPort)
	}
	unit, err := os.ReadFile(m.Path("/etc/systemd/system/koko.service"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(unit), "\nEnvironment=KOKO_LISTEN=:8443\n") {
		t.Errorf("the unit does not listen on 8443:\n%s", unit)
	}
	if !slices.ContainsFunc(m.Calls(), func(c harness.Call) bool { return c.What == "port 8443" }) {
		t.Error("install did not check that port 8443 is free")
	}
}

// With --address, install initialises with that Server Address and never
// asks for the machine's IP.
func TestServerInstallWithAnAddress(t *testing.T) {
	m := harness.NewMachine(t)
	out := install(t, m, "--owner", "alice", "--address", "koko.example.com")
	code, err := joincode.Decode(out.JoinCode.Code)
	if err != nil {
		t.Fatal(err)
	}
	if out.Address != "koko.example.com" || code.Address != "koko.example.com:"+listenPort(t, m) {
		t.Errorf("install set the Server Address %q, and the Join Code names %q", out.Address, code.Address)
	}
	if slices.ContainsFunc(m.Calls(), func(c harness.Call) bool { return c.What == "source ip" }) {
		t.Error("install asked for the machine's IP though --address names the Server Address")
	}
}

// Every refusal comes before install writes anything, and says how to go
// on.
func TestServerInstallRefusals(t *testing.T) {
	for _, tc := range []struct {
		name   string
		setup  func(m *harness.Machine)
		args   []string
		status int
		code   string
		says   []string
	}{
		{
			"a port in use",
			func(m *harness.Machine) {
				m.Listening[443] = portowner.Process{PID: 812, Name: "nginx", User: "www-data"}
			},
			nil, 1, "port_taken",
			[]string{"port 443", "nginx (pid 812, user www-data)", "--port"},
		},
		{
			"a port given in use",
			func(m *harness.Machine) { m.Listening[8443] = portowner.Process{User: "www-data"} },
			[]string{"--port", "8443"},
			1, "port_taken",
			[]string{"port 8443", "a process of user www-data", "--port"},
		},
		{
			"systemd 246",
			func(m *harness.Machine) { m.Systemd = 246 },
			nil, 1, "unsupported_platform",
			[]string{"systemd 246", "247"},
		},
		{
			"a network filesystem",
			func(m *harness.Machine) { m.FSType = "nfs4" },
			nil, 1, "unsupported_platform",
			[]string{"/var/lib/koko", "nfs4", "network filesystem"},
		},
		{
			"no default route",
			func(m *harness.Machine) { m.Source = netip.Addr{} },
			nil, 2, "invalid_usage",
			[]string{"default route", "--address"},
		},
		{
			"no Owner",
			func(*harness.Machine) {},
			[]string{},
			2, "invalid_usage",
			[]string{"--owner"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := harness.NewMachine(t)
			fresh := m.Files()
			tc.setup(m)
			args := tc.args
			if args == nil {
				args = []string{"--owner", "alice"}
			} else if len(args) > 0 {
				args = append([]string{"--owner", "alice"}, args...)
			}
			r := m.Koko(append([]string{"server", "install", "--json"}, args...)...)
			msg := r.WantError(t, tc.status, tc.code)
			for _, want := range append(tc.says, "nothing was written") {
				if !strings.Contains(msg, want) {
					t.Errorf("message %q does not say %q", msg, want)
				}
			}
			if files := m.Files(); !slices.Equal(files, fresh) {
				t.Errorf("the refusal wrote: %v", files)
			}
		})
	}
}

// Without --address, a private source IP is refused, so that no Join Code
// names an address devices elsewhere cannot reach: RFC 1918, CGNAT, IPv6
// ULA, loopback and link-local.
func TestServerInstallRefusesAPrivateSourceIP(t *testing.T) {
	for _, ip := range []string{"10.1.2.3", "172.16.0.9", "192.168.1.20", "100.64.0.1", "fd12:3456::1", "127.0.0.1", "169.254.0.5", "fe80::1"} {
		t.Run(ip, func(t *testing.T) {
			m := harness.NewMachine(t)
			fresh := m.Files()
			m.Source = netip.MustParseAddr(ip)
			r := m.Koko("server", "install", "--owner", "alice", "--json")
			msg := r.WantError(t, 2, "invalid_usage")
			if !strings.Contains(msg, ip) || !strings.Contains(msg, "--address") {
				t.Errorf("message %q does not name %s and --address", msg, ip)
			}
			if files := m.Files(); !slices.Equal(files, fresh) {
				t.Errorf("the refusal wrote: %v", files)
			}
		})
	}
}

// On a machine whose init is not systemd, as in a container, systemctl may
// still be there to tell its version: install tells that systemd is not
// running as sd_booted does, by /run/systemd/system, and refuses before it
// asks the machine anything or writes anything.
func TestServerInstallRefusesWithoutSystemdRunning(t *testing.T) {
	m := harness.NewMachine(t)
	if err := os.Remove(m.Path("/run/systemd/system")); err != nil {
		t.Fatal(err)
	}
	fresh := m.Files()
	msg := m.Koko("server", "install", "--owner", "alice", "--json").WantError(t, 1, "unsupported_platform")
	for _, want := range []string{"systemd is not running", "nothing was written"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q does not say %q", msg, want)
		}
	}
	if files := m.Files(); !slices.Equal(files, fresh) {
		t.Errorf("the refusal wrote: %v", files)
	}
	if calls := m.Calls(); len(calls) != 0 {
		t.Errorf("install asked the machine %v", callNames(calls))
	}
}

// A flag value that could never work is refused before install asks the
// machine anything.
func TestServerInstallRefusesBadFlags(t *testing.T) {
	for _, tc := range []struct {
		args []string
		says string
	}{
		{[]string{"--port", "0"}, "--port takes a TCP port from 1 to 65535"},
		{[]string{"--port", "65536"}, "--port takes a TCP port from 1 to 65535"},
		{[]string{"--port", "https"}, "--port takes a TCP port from 1 to 65535"},
		{[]string{"--owner", "Alice"}, "--owner takes the Owner's User name"},
		{[]string{"--address", "https://koko.example.com"}, "--address takes the Server Address"},
		{[]string{"--address", "koko.example.com:0"}, "--address takes the Server Address"},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			m := harness.NewMachine(t)
			args := []string{"server", "install", "--json"}
			if !slices.Contains(tc.args, "--owner") {
				args = append(args, "--owner", "alice")
			}
			msg := m.Koko(append(args, tc.args...)...).WantError(t, 2, "invalid_usage")
			if !strings.Contains(msg, tc.says) {
				t.Errorf("message %q does not say %q", msg, tc.says)
			}
			if calls := m.Calls(); len(calls) != 0 {
				t.Errorf("install asked the machine %v", calls)
			}
		})
	}
}

// Until initialisation has succeeded, running install again finishes the
// job: here, after a run refused for its address.
func TestServerInstallAgainAfterARefusedAddress(t *testing.T) {
	m := harness.NewMachine(t)
	m.Source = netip.MustParseAddr("10.0.0.5")
	m.Koko("server", "install", "--owner", "alice", "--json").WantError(t, 2, "invalid_usage")

	out := install(t, m, "--owner", "alice", "--address", "koko.example.com")
	if out.Address != "koko.example.com" {
		t.Errorf("install set the Server Address %q, want koko.example.com", out.Address)
	}
	wantInitialised(t, m, "alice")
}

// A run whose service never opens its socket gives up, naming where to look,
// and the next run reuses the Root Key the first wrote, as nothing is
// sealed under it yet, and finishes the job, waiting for a service that
// takes longer to open its socket than the first run waited.
func TestServerInstallAgainAfterTheServiceDidNotStart(t *testing.T) {
	m := harness.NewMachine(t)
	m.NoSocket = true
	r := m.Koko("server", "install", "--owner", "alice", "--json")
	msg := r.WantError(t, 1, "internal_error")
	if !strings.Contains(msg, "/run/koko/koko.sock") || !strings.Contains(msg, "journalctl -u koko") {
		t.Errorf("message %q does not name the socket and journalctl -u koko", msg)
	}
	key, err := os.ReadFile(m.Path("/etc/koko/root.key"))
	if err != nil {
		t.Fatal(err)
	}

	m.NoSocket, m.StartDelay = false, 2*harness.NoSocketWait
	out := install(t, m, "--owner", "alice")
	wantInitialised(t, m, "alice")
	if again, err := os.ReadFile(m.Path("/etc/koko/root.key")); err != nil || string(again) != string(key) {
		t.Error("the second run wrote another Root Key")
	}
	if !strings.HasPrefix(string(key), out.RootKeyID+":") {
		t.Errorf("install printed the Root Key id %s, not the one its file names", out.RootKeyID)
	}
}

// A Root Key file already there, with nothing sealed under it, is kept as
// it is and made root's alone; one that does not parse is left as it is,
// never replaced.
func TestServerInstallKeepsARootKeyFileThere(t *testing.T) {
	keep := func(t *testing.T, m *harness.Machine, text []byte, mode fs.FileMode) {
		t.Helper()
		if err := os.MkdirAll(m.Path("/etc/koko"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(m.Path("/etc/koko/root.key"), text, mode); err != nil {
			t.Fatal(err)
		}
	}
	t.Run("a Root Key", func(t *testing.T) {
		m := harness.NewMachine(t)
		key, err := os.ReadFile(harness.NewRootKeyFile(t))
		if err != nil {
			t.Fatal(err)
		}
		keep(t, m, key, 0o644)
		out := install(t, m, "--owner", "alice")
		if !strings.HasPrefix(string(key), out.RootKeyID+":") {
			t.Errorf("install used %s, not the Root Key there", out.RootKeyID)
		}
		if again, err := os.ReadFile(m.Path("/etc/koko/root.key")); err != nil || string(again) != string(key) {
			t.Error("install changed the Root Key file")
		}
		wantMode(t, m, "/etc/koko/root.key", 0o600)
		wantMode(t, m, "/etc/koko", fs.ModeDir|0o700)
		wantInitialised(t, m, "alice")
	})
	t.Run("a file that does not parse", func(t *testing.T) {
		m := harness.NewMachine(t)
		keep(t, m, []byte("not a Root Key\n"), 0o600)
		r := m.Koko("server", "install", "--owner", "alice", "--json")
		msg := r.WantError(t, 1, "internal_error")
		if !strings.Contains(msg, "does not parse") {
			t.Errorf("message %q does not say the Root Key file does not parse", msg)
		}
		if text, err := os.ReadFile(m.Path("/etc/koko/root.key")); err != nil || string(text) != "not a Root Key\n" {
			t.Error("install changed the Root Key file that does not parse")
		}
		if users := m.Users(); len(users) != 0 {
			t.Errorf("install went on to create %v", users)
		}
	})
}

// A build without the koko server (every OS but Linux) refuses to install it,
// as it refuses to run it.
func TestServerInstallWhereTheBuildHasNoServer(t *testing.T) {
	r := harness.Run(t, cli.Runner{}, nil, "server", "install", "--owner", "alice", "--json")
	msg := r.WantError(t, 1, "unsupported_platform")
	if !strings.Contains(msg, "Linux only") {
		t.Errorf("message %q does not say the koko server runs on Linux only", msg)
	}
}
