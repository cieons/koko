package cli_test

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"io/fs"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/server/kokod"
	"github.com/cieons/koko/internal/shared/build"
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

// install runs koko server install --json on m with args and m's kokod
// given with --kokod, as on a machine with no kokod yet, wants it to
// succeed, and returns what it printed.
func install(t *testing.T, m *harness.Machine, args ...string) installed {
	t.Helper()
	return installAgain(t, m, append([]string{"--kokod", m.Kokod()}, args...)...)
}

// installAgain runs koko server install --json on m with args alone, as on
// a machine where install ran before: without --kokod, it runs the kokod
// installed. It wants it to succeed, and returns what it printed.
func installAgain(t *testing.T, m *harness.Machine, args ...string) installed {
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

// On a fresh machine, install runs the kokod given, which creates the koko
// user and the directories, writes the Root Key, itself and the unit, and
// starts the service; koko then initialises the koko server over its socket
// and prints what the Owner needs next. It never writes /usr/local/bin/koko,
// which install.sh puts there.
func TestServerInstallOnAFreshMachine(t *testing.T) {
	t.Parallel()
	m := harness.NewMachine(t)
	r := m.Koko("server", "install", "--owner", "alice", "--kokod", m.Kokod(), "--json")
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
		"/usr/local/bin/kokod":             {0o755, "root"},
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
	if binary, want := readFile(t, m, "/usr/local/bin/kokod"), fileText(t, m.Kokod()); binary != want {
		t.Errorf("/usr/local/bin/kokod holds %q, want the kokod that ran install, %q", binary, want)
	}
	if _, err := os.Stat(m.Path("/usr/local/bin/koko")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("install wrote /usr/local/bin/koko (%v)", err)
	}
	if ran := m.Ran(); !slices.Equal(ran, []string{"install --owner alice --json"}) {
		t.Errorf("koko ran kokod %q, want install --owner alice --json once", ran)
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
	if out.Address != harness.SourceIP || out.RootKeyFile != "/etc/koko/root.key" || out.ListenPort != 7822 ||
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
// kokod and the unit before it starts the service, and waits for the
// service's socket, which opens late here, before it initialises.
func TestServerInstallChecksFirstAndWaitsForTheSocket(t *testing.T) {
	t.Parallel()
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
	for _, check := range []string{"systemd --version", "filesystem /var/lib", "port 7822", "source ip"} {
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
	for _, file := range []string{"/etc/koko/root.key", "/usr/local/bin/kokod", "/etc/systemd/system/koko.service", "/var/lib/koko"} {
		if !slices.Contains(start.Files, file) {
			t.Errorf("install started the service before writing %s", file)
		}
	}
}

// Install waits until the koko server accepts a connection on its socket,
// not only until the socket's file is there: here the file comes 300 ms
// before the koko server listens, and initialisation still reaches it.
func TestServerInstallWaitsUntilTheSocketAccepts(t *testing.T) {
	t.Parallel()
	m := harness.NewMachine(t)
	m.ListenDelay = 300 * time.Millisecond
	install(t, m, "--owner", "alice")
	wantInitialised(t, m, "alice")
}

// For people, install says where the koko server is, what to keep and what
// to open, and the one koko join line for the Owner's first device; never
// the Root Key.
func TestServerInstallForPeople(t *testing.T) {
	t.Parallel()
	m := harness.NewMachine(t)
	r := m.Koko("server", "install", "--owner", "alice", "--kokod", m.Kokod())
	r.WantSuccess(t)
	key, err := os.ReadFile(m.Path("/etc/koko/root.key"))
	if err != nil {
		t.Fatal(err)
	}
	id, _, _ := strings.Cut(string(key), ":")
	for _, want := range []string{
		"Server Address:  " + harness.SourceIP + ":7822\n",
		"Host key:        SHA256:",
		"Root Key:        /etc/koko/root.key (" + id + ")\n",
		"Keep a copy of /etc/koko/root.key off this machine",
		"open TCP port 7822 in your provider's firewall",
		"run koko ssh setup once as each user who runs ssh <host>.koko on this machine\n",
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

// wantBindGrant fails the test unless unit grants the koko user
// CAP_NET_BIND_SERVICE, its one capability, exactly when grants is true,
// and otherwise leaves it no capability at all.
func wantBindGrant(t *testing.T, unit string, grants bool) {
	t.Helper()
	for line, want := range map[string]bool{
		"\nAmbientCapabilities=CAP_NET_BIND_SERVICE\n":   grants,
		"\nCapabilityBoundingSet=CAP_NET_BIND_SERVICE\n": grants,
		"\nCapabilityBoundingSet=\n":                     !grants,
	} {
		if strings.Contains(unit, line) != want {
			t.Errorf("the unit holds %q: %v, want %v:\n%s", strings.TrimSpace(line), !want, want, unit)
		}
	}
}

// --port makes the koko server listen on another port, even with the
// default one taken: install checks that one, writes it into the unit and
// names it as the port to open. The unit grants the koko user the right to
// bind a port below 1024 only for such a port, which the default 7822 is
// not.
func TestServerInstallOnAnotherPort(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		port       int
		grantsBind bool
	}{
		{8443, false},
		{443, true},
	} {
		t.Run(strconv.Itoa(tc.port), func(t *testing.T) {
			t.Parallel()
			m := harness.NewMachine(t)
			m.Listening[7822] = portowner.Process{PID: 812, Name: "nginx", User: "www-data"}
			out := install(t, m, "--owner", "alice", "--port", strconv.Itoa(tc.port))
			if out.ListenPort != tc.port {
				t.Errorf("install printed the listen port %d, want %d", out.ListenPort, tc.port)
			}
			unit := readFile(t, m, "/etc/systemd/system/koko.service")
			if !strings.Contains(unit, fmt.Sprintf("\nEnvironment=KOKO_LISTEN=:%d\n", tc.port)) {
				t.Errorf("the unit does not listen on %d:\n%s", tc.port, unit)
			}
			wantBindGrant(t, unit, tc.grantsBind)
			if what := fmt.Sprintf("port %d", tc.port); !slices.ContainsFunc(m.Calls(), func(c harness.Call) bool { return c.What == what }) {
				t.Errorf("install did not check that port %d is free", tc.port)
			}
		})
	}
}

// --web-listen turns the web listener on: install checks its port, writes
// it into the unit, and grants the koko user the right to bind it below
// 1024; the koko server then listens there, as koko server show names,
// beside the Web URL once it is set. Without it, the unit turns no web
// listener on.
func TestServerInstallWithAWebListener(t *testing.T) {
	t.Parallel()
	t.Run("off", func(t *testing.T) {
		t.Parallel()
		m := harness.NewMachine(t)
		install(t, m, "--owner", "alice")
		if unit := readFile(t, m, "/etc/systemd/system/koko.service"); strings.Contains(unit, "Environment=KOKO_WEB_LISTEN") {
			t.Errorf("the unit turns the web listener on without --web-listen:\n%s", unit)
		}
		if s := parseServer(t, m.Koko("server", "show", "--json")); s.WebListen != nil {
			t.Errorf("server show names the web listener %s without --web-listen", *s.WebListen)
		}
	})
	for _, tc := range []struct {
		webListen  string
		port       int
		grantsBind bool
	}{
		{"127.0.0.1:7823", 7823, false},
		{"[::1]:443", 443, true},
	} {
		t.Run(tc.webListen, func(t *testing.T) {
			t.Parallel()
			m := harness.NewMachine(t)
			install(t, m, "--owner", "alice", "--web-listen", tc.webListen)
			unit := readFile(t, m, "/etc/systemd/system/koko.service")
			if !strings.Contains(unit, "\nEnvironment=KOKO_WEB_LISTEN="+tc.webListen+"\n") {
				t.Errorf("the unit does not turn the web listener on at %s:\n%s", tc.webListen, unit)
			}
			wantBindGrant(t, unit, tc.grantsBind)
			if what := fmt.Sprintf("port %d", tc.port); !slices.ContainsFunc(m.Calls(), func(c harness.Call) bool { return c.What == what }) {
				t.Errorf("install did not check that port %d is free", tc.port)
			}
			m.Koko("server", "set", "web_url=https://koko.example.com").WantSuccess(t)
			// The fake machine's koko server listens on a loopback port of
			// the test's choosing, never the unit's.
			s := parseServer(t, m.Koko("server", "show", "--json"))
			if s.WebListen == nil || !strings.HasPrefix(*s.WebListen, "127.0.0.1:") || s.WebURL == nil || *s.WebURL != "https://koko.example.com" {
				t.Errorf("server show names the web listener %v and the Web URL %v, want both", s.WebListen, s.WebURL)
			}
		})
	}
}

// With --address, install initialises with that Server Address and never
// asks for the machine's IP.
func TestServerInstallWithAnAddress(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
				m.Listening[7822] = portowner.Process{PID: 812, Name: "nginx", User: "www-data"}
			},
			nil, 1, "port_taken",
			[]string{"port 7822", "nginx (pid 812, user www-data)", "--port"},
		},
		{
			"a port given in use",
			func(m *harness.Machine) { m.Listening[8443] = portowner.Process{User: "www-data"} },
			[]string{"--port", "8443"},
			1, "port_taken",
			[]string{"port 8443", "a process of user www-data", "--port"},
		},
		{
			"a web listener port in use",
			func(m *harness.Machine) {
				m.Listening[7823] = portowner.Process{PID: 812, Name: "nginx", User: "www-data"}
			},
			[]string{"--web-listen", "127.0.0.1:7823"},
			1, "port_taken",
			[]string{"port 7823", "nginx (pid 812, user www-data)", "--web-listen"},
		},
		{
			"a web listener on the port for devices",
			func(*harness.Machine) {},
			[]string{"--web-listen", "127.0.0.1:7822"},
			2, "invalid_usage",
			[]string{"--web-listen 127.0.0.1:7822", "port 7822", "another port"},
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
			r := m.Koko(append([]string{"server", "install", "--json", "--kokod", m.Kokod()}, args...)...)
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
	t.Parallel()
	for _, ip := range []string{"10.1.2.3", "172.16.0.9", "192.168.1.20", "100.64.0.1", "fd12:3456::1", "127.0.0.1", "169.254.0.5", "fe80::1"} {
		t.Run(ip, func(t *testing.T) {
			m := harness.NewMachine(t)
			fresh := m.Files()
			m.Source = netip.MustParseAddr(ip)
			r := m.Koko("server", "install", "--owner", "alice", "--kokod", m.Kokod(), "--json")
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
// runs kokod, which would refuse the same, and before it asks the machine
// anything or writes anything. It names the image, which runs the koko
// server in a container without systemd.
func TestServerInstallRefusesWithoutSystemdRunning(t *testing.T) {
	t.Parallel()
	m := harness.NewMachine(t)
	if err := os.Remove(m.Path("/run/systemd/system")); err != nil {
		t.Fatal(err)
	}
	fresh := m.Files()
	r := m.Koko("server", "install", "--owner", "alice", "--kokod", m.Kokod(), "--json")
	r.WantError(t, 1, "unsupported_platform")
	harness.Golden(t, "server-install-no-systemd.json", r.Stderr)
	refusal := r.Stderr
	r = m.Koko("server", "install", "--owner", "alice", "--kokod", m.Kokod())
	if r.Status != 1 || r.Stdout != "" {
		t.Errorf("status %d, stdout %q; want 1 and nothing", r.Status, r.Stdout)
	}
	harness.Golden(t, "server-install-no-systemd", r.Stderr)
	if files := m.Files(); !slices.Equal(files, fresh) {
		t.Errorf("the refusal wrote: %v", files)
	}
	if calls := m.Calls(); len(calls) != 0 {
		t.Errorf("install asked the machine %v", callNames(calls))
	}
	if ran := m.Ran(); len(ran) != 0 {
		t.Errorf("install ran kokod %q", ran)
	}
	// kokod install, run by hand, refuses the same.
	p := kokod.Program{Machine: m, Binary: func() (string, error) { return m.Kokod(), nil }}
	if by := harness.RunKokod(t, p, nil, "install", "--owner", "alice", "--json"); by.Status != 1 || by.Stdout != "" || by.Stderr != refusal {
		t.Errorf("kokod install exited %d, printing %q and %q; want 1 and the refusal koko printed, %q", by.Status, by.Stdout, by.Stderr, refusal)
	}
}

// A flag value that could never work is refused before install asks the
// machine anything.
func TestServerInstallRefusesBadFlags(t *testing.T) {
	t.Parallel()
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
		{[]string{"--web-listen", "127.0.0.1"}, "--web-listen takes host:port, such as 127.0.0.1:7823, or off"},
		{[]string{"--web-listen", "127.0.0.1:0"}, "--web-listen takes host:port"},
		{[]string{"--web-listen", ":7823"}, "--web-listen takes host:port"},
		{[]string{"--web-listen", "http://127.0.0.1:7823"}, "--web-listen takes host:port"},
		{[]string{"--web-listen", "on"}, "--web-listen takes host:port"},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			m := harness.NewMachine(t)
			args := []string{"server", "install", "--json", "--kokod", m.Kokod()}
			if !slices.Contains(tc.args, "--owner") {
				args = append(args, "--owner", "alice")
			}
			msg := m.Koko(append(args, tc.args...)...).WantError(t, 2, "invalid_usage")
			if !strings.Contains(msg, tc.says) {
				t.Errorf("message %q does not say %q", msg, tc.says)
			}
			if calls, ran := m.Calls(), m.Ran(); len(calls) != 0 || len(ran) != 0 {
				t.Errorf("install asked the machine %v and ran kokod %q", calls, ran)
			}
		})
	}
}

// Until initialisation has succeeded, running install again finishes the
// job: here, after a run refused for its address, which wrote nothing, so
// kokod is downloaded again.
func TestServerInstallAgainAfterARefusedAddress(t *testing.T) {
	t.Parallel()
	m := harness.NewMachine(t)
	rel := m.Release(release)
	m.Source = netip.MustParseAddr("10.0.0.5")
	m.Koko("server", "install", "--owner", "alice", "--json").WantError(t, 2, "invalid_usage")
	wantNoTempDir(t, m)

	out := installAgain(t, m, "--owner", "alice", "--address", "koko.example.com")
	wantRequests(t, rel, "SHA256SUMS", "kokod-linux-amd64", "SHA256SUMS", "kokod-linux-amd64")
	if out.Address != "koko.example.com" {
		t.Errorf("install set the Server Address %q, want koko.example.com", out.Address)
	}
	wantInitialised(t, m, "alice")
}

// A run whose service never opens its socket gives up, naming where to look,
// and the next run, without --kokod, runs the kokod the first installed and
// reuses the Root Key it wrote, as nothing is sealed under it yet, and
// finishes the job, waiting for a service that takes longer to open its
// socket than the first run waited.
func TestServerInstallAgainAfterTheServiceDidNotStart(t *testing.T) {
	t.Parallel()
	m := harness.NewMachine(t)
	m.NoSocket = true
	r := m.Koko("server", "install", "--owner", "alice", "--kokod", m.Kokod(), "--json")
	msg := r.WantError(t, 1, "internal_error")
	if !strings.Contains(msg, "/run/koko/koko.sock") || !strings.Contains(msg, "journalctl -u koko") {
		t.Errorf("message %q does not name the socket and journalctl -u koko", msg)
	}
	key, err := os.ReadFile(m.Path("/etc/koko/root.key"))
	if err != nil {
		t.Fatal(err)
	}

	m.NoSocket, m.StartDelay = false, 2*harness.NoSocketWait
	out := installAgain(t, m, "--owner", "alice")
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
	t.Parallel()
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
		r := m.Koko("server", "install", "--owner", "alice", "--kokod", m.Kokod(), "--json")
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

// fileText returns the content of the file at the real path path.
func fileText(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// wantNothingDone fails the test if koko ran kokod or asked the machine
// anything, or if the files under its root are not fresh.
func wantNothingDone(t *testing.T, m *harness.Machine, fresh []string) {
	t.Helper()
	if ran := m.Ran(); len(ran) != 0 {
		t.Errorf("koko ran kokod %q", ran)
	}
	if calls := m.Calls(); len(calls) != 0 {
		t.Errorf("koko asked the machine %v", callNames(calls))
	}
	if files := m.Files(); !slices.Equal(files, fresh) {
		t.Errorf("the refusal wrote: %v", files)
	}
}

// A machine that cannot run the koko server is refused before koko reads or
// runs any kokod: not Linux, naming the image, an architecture no kokod is
// built for, and koko not running as root, naming the command to run as
// root. Uninstall refuses the same.
func TestServerInstallRefusesAMachineBeforeItReadsKokod(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		setup func(m *harness.Machine)
		says  []string
	}{
		{"macOS", func(m *harness.Machine) { m.OSArch = "darwin/arm64" }, []string{"darwin/arm64", "ghcr.io/cieons/koko", "#in-a-container"}},
		{"an architecture with no kokod", func(m *harness.Machine) { m.OSArch = "linux/riscv64" }, []string{"no kokod for linux/riscv64", "linux/amd64 and linux/arm64"}},
		{"not root", func(m *harness.Machine) { m.NotRoot = true }, []string{"runs as root: run sudo koko server %s"}},
	} {
		for _, verb := range []string{"install", "uninstall"} {
			t.Run(tc.name+", "+verb, func(t *testing.T) {
				m := harness.NewMachine(t)
				fresh := m.Files()
				tc.setup(m)
				r := m.Koko("server", verb, "--kokod", m.Kokod(), "--json")
				msg := r.WantError(t, 1, "unsupported_platform")
				for _, want := range append(tc.says, "nothing was written") {
					if want = strings.ReplaceAll(want, "%s", verb); !strings.Contains(msg, want) {
						t.Errorf("message %q does not say %q", msg, want)
					}
				}
				wantNothingDone(t, m, fresh)
			})
		}
	}
}

// noGoBinary writes a file that is no Go binary and returns its path.
func noGoBinary(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "kokod")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// koko runs only kokod of its own build, for the machine's platform: a
// --kokod of another build or platform, another program, a file that is no
// Go binary or none at all is refused with exit 2 before anything runs,
// naming the build of each for another build.
func TestServerInstallRunsOnlyTheKokodOfItsBuild(t *testing.T) {
	t.Parallel()
	other := harness.KokodBuild()
	other.Version, other.Revision = "v0.2.9", "0123456789abcdef"
	koko := harness.KokodBuild()
	koko.Path = "github.com/cieons/koko/cmd/koko"
	arm := harness.KokodBuild()
	arm.Platform = "linux/arm64"
	for _, tc := range []struct {
		name  string
		kokod func(t *testing.T, m *harness.Machine) string
		says  []string
	}{
		{
			"another build", func(_ *testing.T, m *harness.Machine) string { return m.NewKokod(other) },
			[]string{"kokod " + other.String(), "this koko is " + build.Read().String(), "its own build"},
		},
		{
			"another platform", func(_ *testing.T, m *harness.Machine) string { return m.NewKokod(arm) },
			[]string{"kokod for linux/arm64, and this machine is linux/amd64"},
		},
		{
			"koko", func(_ *testing.T, m *harness.Machine) string { return m.NewKokod(koko) },
			[]string{"github.com/cieons/koko/cmd/koko, not kokod"},
		},
		{
			"no Go binary", func(t *testing.T, _ *harness.Machine) string { return noGoBinary(t) },
			[]string{"which is not kokod"},
		},
		{
			"no file", func(t *testing.T, _ *harness.Machine) string { return filepath.Join(t.TempDir(), "kokod") },
			[]string{"which is not there"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := harness.NewMachine(t)
			fresh := m.Files()
			file := tc.kokod(t, m)
			msg := m.Koko("server", "install", "--owner", "alice", "--kokod", file, "--json").WantError(t, 2, "invalid_usage")
			for _, want := range append(tc.says, "--kokod names", "nothing was written") {
				if !strings.Contains(msg, want) {
					t.Errorf("message %q does not say %q", msg, want)
				}
			}
			wantNothingDone(t, m, fresh)
		})
	}
}

// Without --kokod, install runs the kokod installed only when it is of
// koko's build; with none, another build's or a file that is no kokod, it is
// refused with exit 2, naming what is there and --kokod, before anything
// runs, and the installed file is left as it is.
func TestServerInstallWithoutKokod(t *testing.T) {
	t.Parallel()
	other := harness.KokodBuild()
	other.Version = "v0.2.9"
	for _, tc := range []struct {
		name      string
		installed func(t *testing.T, m *harness.Machine) string
		says      string
	}{
		{"none installed", func(*testing.T, *harness.Machine) string { return "" }, "there is none at /usr/local/bin/kokod"},
		{
			"another build installed", func(_ *testing.T, m *harness.Machine) string { return m.NewKokod(other) },
			"the one at /usr/local/bin/kokod is " + other.String(),
		},
		{
			"no kokod installed", func(t *testing.T, _ *harness.Machine) string { return noGoBinary(t) },
			"the file at /usr/local/bin/kokod is no kokod",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := harness.NewMachine(t)
			if file := tc.installed(t, m); file != "" {
				if err := os.WriteFile(m.Path("/usr/local/bin/kokod"), []byte(fileText(t, file)), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			fresh := m.Files()
			msg := m.Koko("server", "install", "--owner", "alice", "--json").WantError(t, 2, "invalid_usage")
			for _, want := range []string{"the kokod of this koko's build, " + build.Read().String(), tc.says, "--kokod bin/kokod", "nothing was written"} {
				if !strings.Contains(msg, want) {
					t.Errorf("message %q does not say %q", msg, want)
				}
			}
			wantNothingDone(t, m, fresh)
		})
	}
}

// kokod install run by hand installs and starts a koko server it cannot
// initialise, having no client, and names what is left: koko server
// install with the same flags, which runs the kokod installed and
// initialises it.
func TestServerInstallAfterKokodInstallByHand(t *testing.T) {
	t.Parallel()
	m := harness.NewMachine(t)
	p := kokod.Program{Machine: m, Binary: func() (string, error) { return m.Kokod(), nil }}
	r := harness.RunKokod(t, p, nil, "install", "--owner", "alice", "--port", "8443")
	r.WantSuccess(t)
	type initialiseOut struct {
		Owner   string `json:"owner"`
		Address string `json:"address"`
		Next    string `json:"next"`
	}
	var report struct {
		Initialise         *initialiseOut `json:"initialise"`
		Address            string         `json:"address"`
		HostKeyFingerprint string         `json:"host_key_fingerprint"`
		RootKeyFile        string         `json:"root_key_file"`
		RootKeyID          string         `json:"root_key_id"`
		Port               int            `json:"port"`
	}
	if err := json.Unmarshal([]byte(r.Stdout), &report, json.RejectUnknownMembers(true)); err != nil {
		t.Fatalf("kokod install printed %q: %v", r.Stdout, err)
	}
	if want := (initialiseOut{Owner: "alice", Address: harness.SourceIP, Next: "sudo koko server install --owner alice --port 8443"}); report.Initialise == nil || *report.Initialise != want {
		t.Errorf("kokod install printed %s, want it to name %+v", r.Stdout, want)
	}
	if report.Port != 8443 || report.HostKeyFingerprint != "" || report.Address != "" || report.RootKeyFile != "/etc/koko/root.key" || report.RootKeyID == "" {
		t.Errorf("kokod install printed %+v", report)
	}
	if r := m.Koko("space", "ls", "--json"); r.Status == 0 {
		t.Errorf("the koko server is initialised after kokod install alone: %s", r.Stdout)
	}

	out := installAgain(t, m, strings.Fields(report.Initialise.Next)[4:]...)
	if out.Upgraded || out.JoinCode.Code == "" || out.ListenPort != 8443 {
		t.Errorf("koko server install then printed %+v, want the koko server initialised on 8443", out)
	}
	wantInitialised(t, m, "alice")
}
