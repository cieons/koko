package cli_test

import (
	"context"
	"database/sql"
	"encoding/json/v2"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/shared/portowner"
)

// readFile returns the content of the file at path on m.
func readFile(t *testing.T, m *harness.Machine, path string) string {
	t.Helper()
	b, err := os.ReadFile(m.Path(path))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// callNames returns what each of calls asked of the machine, in order.
func callNames(calls []harness.Call) []string {
	var names []string
	for _, c := range calls {
		names = append(names, c.What)
	}
	return names
}

// upgrade runs koko server install --json on m, initialised already, with
// args, wants it to succeed, and returns what it printed.
func upgrade(t *testing.T, m *harness.Machine, args ...string) installed {
	t.Helper()
	out := install(t, m, args...)
	if !out.Upgraded || out.JoinCode != (joinCodeOut{}) {
		t.Errorf("install on an initialised koko server printed %+v, want an upgrade and no Join Code", out)
	}
	return out
}

// On an initialised koko server, install is the upgrade: it replaces the
// binary, writes the unit again from the template and restarts the service,
// which runs on the record as it was, with the same Owner, Server Address,
// host key and Root Key. It prints no Join Code.
func TestServerInstallUpgradesAnInitialisedServer(t *testing.T) {
	m := harness.NewMachine(t)
	before := install(t, m, "--owner", "alice")
	unit := readFile(t, m, "/etc/systemd/system/koko.service")
	key := readFile(t, m, "/etc/koko/root.key")
	release := m.NewRelease()
	if err := os.WriteFile(m.Path("/etc/systemd/system/koko.service"), []byte(unit+"# changed by hand\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	calls := len(m.Calls())

	r := m.Koko("server", "install", "--json")
	r.WantSuccess(t)
	harness.Golden(t, "server-install-upgrade.json", r.Stdout)
	var out installed
	if err := json.Unmarshal([]byte(r.Stdout), &out); err != nil {
		t.Fatal(err)
	}
	if !out.Upgraded || out.JoinCode != (joinCodeOut{}) {
		t.Errorf("install printed %+v, want an upgrade and no Join Code", out)
	}
	if out.Address != before.Address || out.HostKeyFingerprint != before.HostKeyFingerprint ||
		out.RootKeyID != before.RootKeyID || out.RootKeyFile != "/etc/koko/root.key" || out.ListenPort != 443 {
		t.Errorf("the upgrade printed %+v, want the Server Address, host key and Root Key of %+v on port 443", out, before)
	}

	if got := readFile(t, m, "/usr/local/bin/koko"); got != string(release) {
		t.Errorf("/usr/local/bin/koko holds %q, want the new release", got)
	}
	if got := readFile(t, m, "/etc/systemd/system/koko.service"); got != unit {
		t.Errorf("the upgrade left the unit as\n%s\nwant it written again from the template", got)
	}
	if got := readFile(t, m, "/etc/koko/root.key"); got != key {
		t.Error("the upgrade changed the Root Key file")
	}
	whats := callNames(m.Calls()[calls:])
	if got, want := whats[len(whats)-3:], []string{"systemctl daemon-reload", "systemctl enable koko.service", "systemctl restart koko.service"}; !slices.Equal(got, want) {
		t.Errorf("the upgrade ended with %v, want %v", got, want)
	}
	if starts := strings.Count(m.Log(), "koko server started"); starts != 2 || !m.Running() {
		t.Errorf("the koko service started %d times and runs %v, want a restart that runs", starts, m.Running())
	}
	wantInitialised(t, m, "alice")
}

// An upgrade takes neither --owner nor --address, which only initialise a
// fresh koko server: each is refused before anything is written, naming how
// to go on, and the koko server runs on as it was.
func TestServerInstallUpgradeRefusesOwnerAndAddress(t *testing.T) {
	m := harness.NewMachine(t)
	install(t, m, "--owner", "alice")
	for _, tc := range []struct {
		args []string
		says []string
	}{
		{[]string{"--owner", "alice"}, []string{"without --owner", "koko device add"}},
		{[]string{"--address", "koko.example.com"}, []string{"without --address", "koko server set address="}},
		{[]string{"--owner", "bob", "--address", "koko.example.com"}, []string{"without --owner and --address", "koko device add", "koko server set address="}},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			untouched := watch(t, m)
			msg := m.Koko(append([]string{"server", "install", "--json"}, tc.args...)...).WantError(t, 1, "already_initialised")
			for _, want := range append(tc.says, "initialised already", "nothing was written") {
				if !strings.Contains(msg, want) {
					t.Errorf("message %q does not say %q", msg, want)
				}
			}
			untouched()
		})
	}
	wantInitialised(t, m, "alice")
}

// watch notes the files an upgrade writes, the calls so far on m and
// whether its koko service runs, and returns a check that fails the test
// when a refused run wrote any of those files, asked the machine to write or
// run anything, or stopped the service.
func watch(t *testing.T, m *harness.Machine) (untouched func()) {
	t.Helper()
	paths := []string{"/usr/local/bin/koko", "/etc/systemd/system/koko.service", "/etc/koko/root.key"}
	before := map[string]string{}
	for _, path := range paths {
		if b, err := os.ReadFile(m.Path(path)); err == nil {
			before[path] = string(b)
		}
	}
	calls, running := len(m.Calls()), m.Running()
	return func() {
		t.Helper()
		for _, path := range paths {
			b, err := os.ReadFile(m.Path(path))
			if text, was := before[path]; was != (err == nil) || string(b) != text {
				t.Errorf("the refused run changed %s", path)
			}
		}
		for _, c := range m.Calls()[calls:] {
			if strings.HasPrefix(c.What, "useradd") || strings.HasPrefix(c.What, "systemctl") {
				t.Errorf("the refused run asked the machine %q", c.What)
			}
		}
		if running && !m.Running() {
			t.Error("the refused run stopped the koko service")
		}
	}
}

// Without --port an upgrade keeps the port the koko server listens on, and
// with it the koko server moves to the port given. A Server Address that
// names no port of its own follows the listen port, so then the upgrade
// says that devices joined before must take the new address by hand; one
// that names its own port stays where devices connect, and nothing is said.
func TestServerInstallUpgradeAndThePort(t *testing.T) {
	for _, tc := range []struct {
		name        string
		install     []string
		upgrade     []string
		port        int
		mustConnect string
	}{
		{"without --port, the port is kept", []string{"--port", "8443"}, nil, 8443, ""},
		{"--port under a Server Address without a port", nil, []string{"--port", "8443"}, 8443, harness.SourceIP + ":8443"},
		{"--port under a Server Address with its own port", []string{"--address", "koko.example.com:443"}, []string{"--port", "8443"}, 8443, ""},
		{"--port naming the port it listens on", []string{"--port", "8443"}, []string{"--port", "8443"}, 8443, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := harness.NewMachine(t)
			before := install(t, m, append([]string{"--owner", "alice"}, tc.install...)...)
			out := upgrade(t, m, tc.upgrade...)
			if out.ListenPort != tc.port || out.Address != before.Address || out.DevicesMustConnect != tc.mustConnect {
				t.Errorf("the upgrade printed %+v, want port %d, the Server Address %s and devices_must_connect %q",
					out, tc.port, before.Address, tc.mustConnect)
			}
			if unit := readFile(t, m, "/etc/systemd/system/koko.service"); !strings.Contains(unit, fmt.Sprintf("\nEnvironment=KOKO_LISTEN=:%d\n", tc.port)) {
				t.Errorf("the unit does not listen on %d:\n%s", tc.port, unit)
			}
			wantInitialised(t, m, "alice")
		})
	}
}

// For people, an upgrade that moves the port devices connect to says so,
// with the one koko connect line to run on each of them.
func TestServerInstallUpgradeMovingThePortForPeople(t *testing.T) {
	m := harness.NewMachine(t)
	install(t, m, "--owner", "alice")
	r := m.Koko("server", "install", "--port", "8443")
	r.WantSuccess(t)
	for _, want := range []string{
		"The koko server is upgraded and running",
		"Server Address:  " + harness.SourceIP + ":8443\n",
		"open TCP port 8443 in your provider's firewall",
		"must take the new address by hand",
		"\nkoko connect " + harness.SourceIP + ":8443\n",
	} {
		if !strings.Contains(r.Stdout, want) {
			t.Errorf("the upgrade printed\n%s\nwhich does not say %q", r.Stdout, want)
		}
	}
	if strings.Contains(r.Stdout, "koko join") {
		t.Errorf("the upgrade printed a Join Code:\n%s", r.Stdout)
	}

	r = m.Koko("server", "install", "--port", "9443", "--json")
	r.WantSuccess(t)
	harness.Golden(t, "server-install-upgrade-port.json", r.Stdout)
}

// An upgrade checks the port it keeps or is given, as install does: the
// koko service's own process holding it is no refusal, since the restart
// frees it, but any other process is, even one named koko.
func TestServerInstallUpgradeChecksThePort(t *testing.T) {
	t.Run("held by the koko service", func(t *testing.T) {
		m := harness.NewMachine(t)
		install(t, m, "--owner", "alice")
		calls := len(m.Calls())
		upgrade(t, m)
		if whats := callNames(m.Calls()[calls:]); !slices.Contains(whats, "port 443") || !slices.Contains(whats, "show koko.service MainPID") {
			t.Errorf("the upgrade did not check who holds port 443: %v", whats)
		}
	})
	nginx := portowner.Process{PID: 812, Name: "nginx", User: "www-data"}
	for _, tc := range []struct {
		name   string
		setup  func(m *harness.Machine)
		args   []string
		holder string
	}{
		{"another process on the port given", func(m *harness.Machine) { m.Listening[8443] = nginx }, []string{"--port", "8443"}, "nginx (pid 812, user www-data)"},
		{"another process on the port kept, the service stopped", func(m *harness.Machine) {
			if err := m.Systemctl(context.Background(), "stop", "koko.service"); err != nil {
				t.Fatal(err)
			}
			m.Listening[443] = nginx
		}, nil, "nginx (pid 812, user www-data)"},
		{"another koko process", func(m *harness.Machine) {
			m.Listening[443] = portowner.Process{PID: 9001, Name: "koko", User: "koko"}
		}, nil, "koko (pid 9001, user koko)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := harness.NewMachine(t)
			install(t, m, "--owner", "alice")
			tc.setup(m)
			untouched := watch(t, m)
			msg := m.Koko(append([]string{"server", "install", "--json"}, tc.args...)...).WantError(t, 1, "port_taken")
			for _, want := range []string{tc.holder, "--port", "nothing was written"} {
				if !strings.Contains(msg, want) {
					t.Errorf("message %q does not say %q", msg, want)
				}
			}
			untouched()
		})
	}
}

// An upgrade needs the Root Key the koko server's keys are sealed under, as
// the restarted koko server will: a Root Key file that is missing, another
// Root Key's or one that does not parse is refused before anything is
// written, naming the Root Key the sealed data needs, and is left as it is.
func TestServerInstallUpgradeNeedsTheRootKey(t *testing.T) {
	for _, tc := range []struct {
		name string
		// replace changes the Root Key file at path and returns what the
		// refusal says of it.
		replace func(t *testing.T, path string) string
	}{
		{"a missing file", func(t *testing.T, path string) string {
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			return "the Root Key file /etc/koko/root.key is missing"
		}},
		{"another Root Key", func(t *testing.T, path string) string {
			other, err := os.ReadFile(harness.NewRootKeyFile(t))
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, other, 0o600); err != nil {
				t.Fatal(err)
			}
			id, _, _ := strings.Cut(string(other), ":")
			return "the Root Key file /etc/koko/root.key names " + id
		}},
		{"a file that does not parse", func(t *testing.T, path string) string {
			if err := os.WriteFile(path, []byte("not a Root Key\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			return "does not parse"
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := harness.NewMachine(t)
			before := install(t, m, "--owner", "alice")
			m.NewRelease()
			says := tc.replace(t, m.Path("/etc/koko/root.key"))
			untouched := watch(t, m)
			msg := m.Koko("server", "install", "--json").WantError(t, 1, "internal_error")
			for _, want := range []string{says, "the sealed data in /var/lib/koko needs " + before.RootKeyID, "nothing was written"} {
				if !strings.Contains(msg, want) {
					t.Errorf("message %q does not say %q", msg, want)
				}
			}
			untouched()
		})
	}
}

// Without its unit, as after koko server uninstall, an upgrade cannot know
// the port the koko server listened on: it listens on 443, or on the port
// --port gives, and says devices that joined on another port must take the
// new address. A unit whose port install cannot read is never written over.
func TestServerInstallUpgradeAndTheUnit(t *testing.T) {
	t.Run("gone", func(t *testing.T) {
		m := harness.NewMachine(t)
		install(t, m, "--owner", "alice", "--port", "8443")
		m.Koko("server", "uninstall", "--json").WantSuccess(t)
		out := upgrade(t, m)
		if out.ListenPort != 443 || out.DevicesMustConnect != harness.SourceIP+":443" {
			t.Errorf("the upgrade printed %+v, want port 443 and devices_must_connect %s:443", out, harness.SourceIP)
		}
		wantInitialised(t, m, "alice")
	})
	t.Run("changed by hand", func(t *testing.T) {
		m := harness.NewMachine(t)
		install(t, m, "--owner", "alice", "--port", "8443")
		unit := readFile(t, m, "/etc/systemd/system/koko.service")
		if err := os.WriteFile(m.Path("/etc/systemd/system/koko.service"), []byte(strings.ReplaceAll(unit, "Environment=KOKO_LISTEN=:8443\n", "")), 0o644); err != nil {
			t.Fatal(err)
		}
		out := upgrade(t, m)
		if out.ListenPort != 8443 || out.DevicesMustConnect != "" {
			t.Errorf("the upgrade printed %+v, want port 8443, where the koko service listens, and no devices_must_connect", out)
		}
		if got := readFile(t, m, "/etc/systemd/system/koko.service"); got != unit {
			t.Errorf("the upgrade left the unit as\n%s\nwant it written again from the template", got)
		}
	})
}

// A port set with a drop-in (systemctl edit koko) overrides the unit's: an
// upgrade keeps it, checks it and reports it, as systemd has the koko
// service listen there, and refuses a --port the drop-in would override,
// before anything is written. Uninstall keeps the drop-in, which systemd
// does not load without the unit, and install after it still finds it.
func TestServerInstallUpgradeWithADropInPort(t *testing.T) {
	m := harness.NewMachine(t)
	install(t, m, "--owner", "alice")
	if err := os.MkdirAll(m.Path("/etc/systemd/system/koko.service.d"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(m.Path("/etc/systemd/system/koko.service.d/port.conf"), []byte("[Service]\nEnvironment=KOKO_LISTEN=:8443\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"daemon-reload"}, {"restart", "koko.service"}} {
		if err := m.Systemctl(context.Background(), args...); err != nil {
			t.Fatal(err)
		}
	}
	calls := len(m.Calls())
	out := upgrade(t, m)
	if out.ListenPort != 8443 || out.DevicesMustConnect != "" {
		t.Errorf("the upgrade printed %+v, want the drop-in's port 8443 and no devices_must_connect", out)
	}
	if whats := callNames(m.Calls()[calls:]); !slices.Contains(whats, "port 8443") {
		t.Errorf("the upgrade did not check port 8443: %v", whats)
	}

	refused := func(t *testing.T) {
		t.Helper()
		untouched := watch(t, m)
		msg := m.Koko("server", "install", "--port", "9443", "--json").WantError(t, 2, "invalid_usage")
		for _, want := range []string{"/etc/systemd/system/koko.service.d/port.conf", "8443", "systemctl edit koko", "nothing was written"} {
			if !strings.Contains(msg, want) {
				t.Errorf("message %q does not say %q", msg, want)
			}
		}
		untouched()
	}
	refused(t)

	m.Koko("server", "uninstall", "--json").WantSuccess(t)
	refused(t)
	out = upgrade(t, m)
	if out.ListenPort != 8443 || out.DevicesMustConnect != "" {
		t.Errorf("install after uninstall printed %+v, want the drop-in's port 8443 and no devices_must_connect", out)
	}
}

// The P0 restore: a copy of the data directory, taken while the koko server
// was stopped, and its Root Key, put back on a fresh machine, where install
// without --owner finds the koko server initialised and runs it. Install
// leaves the copy's owner to systemd's StateDirectory=, which gives the
// whole tree to the koko user when the directory is not that user's.
func TestServerInstallRestoresACopy(t *testing.T) {
	old := harness.NewMachine(t)
	before := install(t, old, "--owner", "alice")
	if err := old.Systemctl(context.Background(), "stop", "koko.service"); err != nil {
		t.Fatal(err)
	}
	m := harness.NewMachine(t)
	if err := os.CopyFS(m.Path("/var/lib/koko"), os.DirFS(old.Path("/var/lib/koko"))); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(m.Path("/etc/koko"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(m.Path("/etc/koko/root.key"), []byte(readFile(t, old, "/etc/koko/root.key")), 0o644); err != nil {
		t.Fatal(err)
	}

	out := upgrade(t, m)
	if out.HostKeyFingerprint != before.HostKeyFingerprint || out.RootKeyID != before.RootKeyID || out.Address != before.Address {
		t.Errorf("install on the copy printed %+v, want the host key, Root Key and Server Address of %+v", out, before)
	}
	wantInitialised(t, m, "alice")
	wantMode(t, m, "/var/lib/koko", fs.ModeDir|0o700)
	wantMode(t, m, "/etc/koko/root.key", 0o600)
	for _, path := range []string{"/var/lib/koko", "/var/lib/koko/koko.db"} {
		if owner := m.Owner(path); owner != "koko" {
			t.Errorf("%s is owned by %q after the start, want koko", path, owner)
		}
	}
}

// Going back is putting the snapshot in place of koko.db, then installing
// the older koko. An older koko than the data's schema, installed first,
// would not start: install refuses it before anything is written, naming
// the schema versions, and the newer koko server runs on.
func TestServerInstallRefusesDataNewerThanItsSchema(t *testing.T) {
	m := harness.NewMachine(t)
	install(t, m, "--owner", "alice")
	db, err := sql.Open("sqlite", m.Path("/var/lib/koko/koko.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	var version int
	if err := db.QueryRowContext(t.Context(), "PRAGMA user_version").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(t.Context(), fmt.Sprintf("PRAGMA user_version = %d", version+1)); err != nil {
		t.Fatal(err)
	}

	untouched := watch(t, m)
	msg := m.Koko("server", "install", "--json").WantError(t, 1, "internal_error")
	for _, want := range []string{"/var/lib/koko", fmt.Sprintf("schema version %d", version+1), "nothing was written"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q does not say %q", msg, want)
		}
	}
	untouched()
}
