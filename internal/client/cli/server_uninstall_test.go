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

// Uninstall stops the koko service, then removes its unit and its binary.
// It keeps the data directory, the Root Key and the koko user, and prints
// their paths, with the port the koko server listened on; there is no
// purge.
func TestServerUninstall(t *testing.T) {
	m := harness.NewMachine(t)
	install(t, m, "--owner", "alice")
	key := readFile(t, m, "/etc/koko/root.key")
	calls := len(m.Calls())

	r := m.Koko("server", "uninstall", "--json")
	r.WantSuccess(t)
	harness.Golden(t, "server-uninstall.json", r.Stdout)
	var out uninstalled
	if err := json.Unmarshal([]byte(r.Stdout), &out); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(out.Removed, []string{"/etc/systemd/system/koko.service", "/usr/local/bin/koko"}) ||
		out.DataDir != "/var/lib/koko" || out.RootKeyFile != "/etc/koko/root.key" || out.SystemUser != "koko" || out.ListenPort != 443 {
		t.Errorf("uninstall printed %+v", out)
	}

	if m.Running() || m.Enabled() {
		t.Errorf("the koko service runs %v and is enabled %v, want neither", m.Running(), m.Enabled())
	}
	for _, path := range []string{"/etc/systemd/system/koko.service", "/usr/local/bin/koko"} {
		if _, err := os.Stat(m.Path(path)); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("uninstall left %s: %v", path, err)
		}
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

	// The service stops while its unit is there, and systemd forgets the
	// unit once it is gone; nothing removes the koko user.
	after := m.Calls()[calls:]
	whats := callNames(after)
	if want := []string{"show koko.service Environment", "systemctl stop koko.service", "systemctl disable koko.service", "systemctl daemon-reload"}; !slices.Equal(whats, want) {
		t.Fatalf("uninstall asked the machine %v, want %v", whats, want)
	}
	if c := after[1]; !slices.Contains(c.Files, "/etc/systemd/system/koko.service") {
		t.Error("uninstall removed the unit before it stopped the service")
	}
	if c := after[3]; slices.Contains(c.Files, "/etc/systemd/system/koko.service") {
		t.Error("uninstall reloaded systemd before it removed the unit")
	}
}

// For people, uninstall says what it removed and where what it kept is, and
// how to install koko again on it, on the port it listened on.
func TestServerUninstallForPeople(t *testing.T) {
	m := harness.NewMachine(t)
	install(t, m, "--owner", "alice", "--port", "8443")
	r := m.Koko("server", "uninstall")
	r.WantSuccess(t)
	for _, want := range []string{
		"The koko server is uninstalled",
		"/etc/systemd/system/koko.service\n",
		"/usr/local/bin/koko\n",
		"Data directory:  /var/lib/koko\n",
		"Root Key:        /etc/koko/root.key\n",
		"System user:     koko\n",
		"install koko again without --owner, with --port 8443",
	} {
		if !strings.Contains(r.Stdout, want) {
			t.Errorf("uninstall printed\n%s\nwhich does not say %q", r.Stdout, want)
		}
	}
}

// Uninstalling again removes nothing and asks systemd nothing, and install
// then runs the koko server again on the data uninstall kept: it is the
// upgrade of the koko server initialised there.
func TestServerUninstallAgainThenInstall(t *testing.T) {
	m := harness.NewMachine(t)
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

// A build without the koko server (every OS but Linux) refuses to uninstall
// it, as it refuses to install it.
func TestServerUninstallWhereTheBuildHasNoServer(t *testing.T) {
	r := harness.Run(t, cli.Runner{}, nil, "server", "uninstall", "--json")
	msg := r.WantError(t, 1, "unsupported_platform")
	if !strings.Contains(msg, "Linux only") {
		t.Errorf("message %q does not say the koko server runs on Linux only", msg)
	}
}

// Install and uninstall change the machine, and install prints a
// credential, so both are in the irreversible tier (ADR 0019).
func TestServerInstallAndUninstallAreIrreversible(t *testing.T) {
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
