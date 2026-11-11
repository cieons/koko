package cli_test

import (
	"encoding/json/v2"
	"errors"
	"io/fs"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/cieons/koko/internal/client/cli"
	"github.com/cieons/koko/internal/harness"
)

// uninstalled is what koko server uninstall --json prints.
type uninstalled struct {
	Removed     []string `json:"removed"`
	DataDir     string   `json:"data_dir"`
	RootKeyFile string   `json:"root_key_file"`
	SystemUser  string   `json:"system_user"`
	ListenPort  int      `json:"listen_port"`
}

// Uninstall runs the kokod installed, which stops the koko service, then
// removes its unit and itself. It keeps the data directory, the Root Key,
// the koko user and koko, the CLI, and prints their paths, with the port
// the koko server listened on; there is no purge.
func TestServerUninstall(t *testing.T) {
	t.Parallel()
	m := harness.NewMachine(t)
	install(t, m, "--owner", "alice")
	koko := "#!/bin/sh\n# koko, the CLI, as install.sh put it\n"
	if err := os.WriteFile(m.Path("/usr/local/bin/koko"), []byte(koko), 0o755); err != nil {
		t.Fatal(err)
	}
	key := readFile(t, m, "/etc/koko/root.key")
	calls := len(m.Calls())

	r := m.Koko("server", "uninstall", "--json")
	r.WantSuccess(t)
	harness.Golden(t, "server-uninstall.json", r.Stdout)
	var out uninstalled
	if err := json.Unmarshal([]byte(r.Stdout), &out); err != nil {
		t.Fatal(err)
	}
	removed := []string{"/etc/systemd/system/koko.service", "/etc/systemd/system/koko-upgrade.path", "/etc/systemd/system/koko-upgrade.service", "/usr/local/bin/kokod", "/var/lib/koko-upgrade"}
	if !slices.Equal(out.Removed, removed) ||
		out.DataDir != "/var/lib/koko" || out.RootKeyFile != "/etc/koko/root.key" || out.SystemUser != "koko" || out.ListenPort != 7822 {
		t.Errorf("uninstall printed %+v", out)
	}

	if m.Running() || m.Enabled() || m.HelperEnabled() {
		t.Errorf("the koko service runs %v and is enabled %v, and the upgrade helper's path unit is enabled %v; want none", m.Running(), m.Enabled(), m.HelperEnabled())
	}
	for _, path := range removed {
		if _, err := os.Stat(m.Path(path)); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("uninstall left %s: %v", path, err)
		}
	}
	if got := readFile(t, m, "/usr/local/bin/koko"); got != koko {
		t.Errorf("uninstall changed /usr/local/bin/koko to %q", got)
	}
	if ran := m.Ran(); ran[len(ran)-1] != "uninstall --json" {
		t.Errorf("koko ran kokod %q, want uninstall --json last", ran)
	}
	// The record stays; the stopping koko server checkpoints its WAL into
	// koko.db, so its bytes may change. Install runs it again on it
	// (TestServerUninstallAgainThenInstall).
	if b, err := os.ReadFile(m.Path("/etc/koko/root.key")); err != nil || string(b) != key {
		t.Errorf("uninstall changed the Root Key file (%v)", err)
	}
	if _, err := os.Stat(m.Path("/var/lib/koko/koko.db")); err != nil {
		t.Errorf("uninstall removed the record: %v", err)
	}
	wantMode(t, m, "/var/lib/koko", fs.ModeDir|0o700)
	if users := m.Users(); !slices.Equal(users, []string{"koko"}) {
		t.Errorf("the users are %v after uninstall, want koko", users)
	}

	// The helper's path unit and the service stop while their units are
	// there, and systemd forgets the units once they are gone; nothing
	// removes the koko user.
	after := m.Calls()[calls:]
	whats := callNames(after)
	if want := []string{"systemctl disable --now koko-upgrade.path", "show koko.service Environment", "systemctl stop koko.service", "systemctl disable koko.service", "systemctl daemon-reload"}; !slices.Equal(whats, want) {
		t.Fatalf("uninstall asked the machine %v, want %v", whats, want)
	}
	if c := after[0]; !slices.Contains(c.Files, "/etc/systemd/system/koko-upgrade.path") {
		t.Error("uninstall removed the helper's path unit before it stopped it")
	}
	if c := after[2]; !slices.Contains(c.Files, "/etc/systemd/system/koko.service") {
		t.Error("uninstall removed the unit before it stopped the service")
	}
	if c := after[4]; slices.ContainsFunc(c.Files, func(f string) bool { return strings.HasPrefix(f, "/etc/systemd/system/koko") }) {
		t.Errorf("uninstall reloaded systemd before it removed the units: %v", c.Files)
	}
}

// For people, uninstall says what it removed and where what it kept is, and
// how to run the koko server again on it, on the port it listened on and
// with the web listener its unit set.
func TestServerUninstallForPeople(t *testing.T) {
	t.Parallel()
	m := harness.NewMachine(t)
	install(t, m, "--owner", "alice", "--port", "8443", "--web-listen", "127.0.0.1:7823")
	r := m.Koko("server", "uninstall")
	r.WantSuccess(t)
	for _, want := range []string{
		"The koko server is uninstalled",
		"/etc/systemd/system/koko.service\n",
		"/usr/local/bin/kokod\n",
		"Data directory:  /var/lib/koko\n",
		"Root Key:        /etc/koko/root.key\n",
		"System user:     koko\n",
		"run sudo koko server install without --owner, with --port 8443 and --web-listen 127.0.0.1:7823.",
	} {
		if !strings.Contains(r.Stdout, want) {
			t.Errorf("uninstall printed\n%s\nwhich does not say %q", r.Stdout, want)
		}
	}
}

// Uninstalling again, downloading kokod since the first removed it, removes
// nothing and asks systemd nothing, and install then, downloading it too,
// runs the koko server again on the data uninstall kept: it is the upgrade
// of the koko server initialised there.
func TestServerUninstallAgainThenInstall(t *testing.T) {
	t.Parallel()
	m := harness.NewMachine(t)
	m.Release(release)
	before := install(t, m, "--owner", "alice")
	m.Koko("server", "uninstall", "--json").WantSuccess(t)
	calls := len(m.Calls())

	r := m.Koko("server", "uninstall", "--json")
	r.WantSuccess(t)
	var out uninstalled
	if err := json.Unmarshal([]byte(r.Stdout), &out); err != nil {
		t.Fatal(err)
	}
	if out.Removed == nil || len(out.Removed) != 0 {
		t.Errorf("uninstalling again removed %#v, want []", out.Removed)
	}
	if again := m.Calls()[calls:]; len(again) != 0 {
		t.Errorf("uninstalling again asked the machine %v", again)
	}
	if r := m.Koko("server", "uninstall"); !strings.Contains(r.Stdout, "nothing to remove") {
		t.Errorf("uninstalling again printed\n%s\nwhich does not say there was nothing to remove", r.Stdout)
	}

	back := upgrade(t, m)
	if back.HostKeyFingerprint != before.HostKeyFingerprint || back.RootKeyID != before.RootKeyID {
		t.Errorf("install after uninstall printed %+v, want the host key and Root Key of %+v", back, before)
	}
	wantInitialised(t, m, "alice")
}

// On a machine as v0.1.0 or v0.2.0 left it, there is no kokod, which
// uninstall downloads, and /usr/local/bin/koko is the CLI now, which it
// keeps: it removes only the unit.
func TestServerUninstallOnAMachineAsAnOldReleaseLeftIt(t *testing.T) {
	t.Parallel()
	m := harness.NewMachine(t)
	rel := m.Release(release)
	install(t, m, "--owner", "alice")
	koko := asOldRelease(t, m)
	r := m.Koko("server", "uninstall", "--json")
	r.WantSuccess(t)
	wantRequests(t, rel, "SHA256SUMS", "kokod-linux-amd64")
	wantNoTempDir(t, m)
	var out uninstalled
	if err := json.Unmarshal([]byte(r.Stdout), &out); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(out.Removed, []string{"/etc/systemd/system/koko.service"}) {
		t.Errorf("uninstall removed %v, want the unit alone", out.Removed)
	}
	if got := readFile(t, m, "/usr/local/bin/koko"); got != koko || m.Running() {
		t.Errorf("uninstall left /usr/local/bin/koko as %q and the service running %v; want it kept and the service stopped", got, m.Running())
	}
}

// Install and uninstall change the machine, and install prints a
// credential, so both are in the irreversible tier (ADR 0019).
func TestServerInstallAndUninstallAreIrreversible(t *testing.T) {
	t.Parallel()
	for _, path := range []string{"server install", "server uninstall"} {
		i := slices.IndexFunc(cli.Tree(), func(c cli.Command) bool { return c.Path() == path })
		if i < 0 {
			t.Errorf("koko %s is not in the command tree", path)
			continue
		}
		if tier := cli.Tree()[i].Tier; tier != cli.Irreversible {
			t.Errorf("koko %s is in the %s tier, want irreversible", path, tier)
		}
	}
}
