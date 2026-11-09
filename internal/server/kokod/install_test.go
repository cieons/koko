package kokod_test

import (
	"encoding/json/v2"
	"strings"
	"testing"

	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/server/kokod"
	"github.com/cieons/koko/internal/shared/install"
)

// onMachine is kokod as its file m.Kokod() runs on the fake machine m.
func onMachine(m *harness.Machine) kokod.Program {
	return kokod.Program{Machine: m, Binary: func() (string, error) { return m.Kokod(), nil }}
}

// kokod install prints the shared install.Report as JSON, whatever --json
// says: on a fresh machine, what initialising the koko server needs, and on
// one initialised already, the upgrade's outcome. kokod uninstall prints the
// shared install.UninstallReport.
func TestKokodInstallAndUninstallPrintTheSharedReports(t *testing.T) {
	t.Parallel()
	m := harness.NewMachine(t)
	p := onMachine(m)
	for _, args := range [][]string{
		{"install", "--owner", "alice", "--address", "koko.example.com"},
		{"install", "--owner", "alice", "--address", "koko.example.com", "--json"},
	} {
		r := harness.RunKokod(t, p, nil, args...)
		r.WantSuccess(t)
		var report install.Report
		if err := json.Unmarshal([]byte(r.Stdout), &report, json.RejectUnknownMembers(true)); err != nil {
			t.Fatalf("kokod %s printed %q: %v", strings.Join(args, " "), r.Stdout, err)
		}
		want := install.Initialise{Owner: "alice", Address: "koko.example.com", Next: "sudo koko server install --owner alice --address koko.example.com"}
		if report.Initialise == nil || *report.Initialise != want || report.Port != 7822 || report.RootKeyFile != "/etc/koko/root.key" {
			t.Errorf("kokod %s printed %s", strings.Join(args, " "), r.Stdout)
		}
	}
	m.Koko("server", "install", "--owner", "alice", "--address", "koko.example.com", "--json").WantSuccess(t)

	r := harness.RunKokod(t, p, nil, "install")
	r.WantSuccess(t)
	var upgraded install.Report
	if err := json.Unmarshal([]byte(r.Stdout), &upgraded, json.RejectUnknownMembers(true)); err != nil {
		t.Fatal(err)
	}
	if upgraded.Initialise != nil || upgraded.Address != "koko.example.com" || !strings.HasPrefix(upgraded.HostKeyFingerprint, "SHA256:") {
		t.Errorf("kokod install on an initialised koko server printed %s", r.Stdout)
	}

	r = harness.RunKokod(t, p, nil, "uninstall")
	r.WantSuccess(t)
	var removed install.UninstallReport
	if err := json.Unmarshal([]byte(r.Stdout), &removed, json.RejectUnknownMembers(true)); err != nil {
		t.Fatal(err)
	}
	if strings.Join(removed.Removed, " ") != "/etc/systemd/system/koko.service /usr/local/bin/kokod" || removed.ListenPort != 7822 {
		t.Errorf("kokod uninstall printed %s", r.Stdout)
	}
}

// kokod install's refusals have the shape and exit status of koko's: 2 for
// a command line that does not fit, 1 otherwise, and nothing on stdout.
func TestKokodInstallRefusals(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		setup  func(m *harness.Machine)
		args   []string
		status int
		code   string
		says   string
	}{
		{"no Owner", nil, nil, 2, "invalid_usage", "--owner"},
		{"a port out of range", nil, []string{"--owner", "alice", "--port", "0"}, 2, "invalid_usage", "--port takes a TCP port from 1 to 65535"},
		{"an Owner that is not a User name", nil, []string{"--owner", "Alice"}, 2, "invalid_usage", "--owner takes the Owner's User name"},
		{"a flag it does not take", nil, []string{"--kokod", "/tmp/kokod"}, 2, "invalid_usage", "unknown flag --kokod for kokod install"},
		{"systemd 246", func(m *harness.Machine) { m.Systemd = 246 }, []string{"--owner", "alice"}, 1, "unsupported_platform", "systemd 246"},
		{"not root", func(m *harness.Machine) { m.NotRoot = true }, []string{"--owner", "alice"}, 1, "unsupported_platform", "so it runs as root: run sudo koko server install"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := harness.NewMachine(t)
			if tc.setup != nil {
				tc.setup(m)
			}
			r := harness.RunKokod(t, onMachine(m), nil, append([]string{"install", "--json"}, tc.args...)...)
			if msg := r.WantError(t, tc.status, tc.code); !strings.Contains(msg, tc.says) {
				t.Errorf("message %q does not say %q", msg, tc.says)
			}
		})
	}
}

// kokod uninstall, as install, runs as root only, and refuses before it
// asks the machine anything.
func TestKokodUninstallRunsAsRootOnly(t *testing.T) {
	t.Parallel()
	m := harness.NewMachine(t)
	m.NotRoot = true
	r := harness.RunKokod(t, onMachine(m), nil, "uninstall", "--json")
	if msg := r.WantError(t, 1, "unsupported_platform"); !strings.Contains(msg, "run sudo koko server uninstall") {
		t.Errorf("message %q does not name sudo koko server uninstall", msg)
	}
	if calls := m.Calls(); len(calls) != 0 {
		t.Errorf("kokod uninstall asked the machine %v", calls)
	}
}
