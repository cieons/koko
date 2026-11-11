package server_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/server"
	"github.com/cieons/koko/internal/shared/api"
)

// upgradeFiles are the upgrade helper's files a test's koko server uses:
// its path unit, which is there, its outcome, and the run directory the
// request goes in.
type upgradeFiles struct {
	server.Upgrade
	runDir string
}

// startUpgradable starts a koko server of v0.4.0 installed with the upgrade
// helper, whose check finds v0.5.0 the latest release at a fake GitHub.
func startUpgradable(t *testing.T) (*harness.Harness, *harness.Releases, upgradeFiles) {
	t.Helper()
	dir := t.TempDir()
	f := upgradeFiles{
		Upgrade: server.Upgrade{
			HelperUnit:  filepath.Join(dir, "koko-upgrade.path"),
			OutcomeFile: filepath.Join(dir, "outcome.json"),
			RequestFile: filepath.Join(dir, "run", "upgrade.request"),
		},
		runDir: filepath.Join(dir, "run"),
	}
	if err := os.WriteFile(f.HelperUnit, []byte("[Path]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(f.runDir, 0o755); err != nil {
		t.Fatal(err)
	}
	rel := harness.NewMachine(t).Release("v0.5.0")
	rel.Latest()
	return harness.Start(t, harness.Upgrade(f.Upgrade), harness.Version("v0.4.0"), harness.UpdateCheck(rel)), rel, f
}

// wantRequestFile wants the run directory to hold the upgrade request for
// version alone, as the helper reads it, or nothing when version is "".
func wantRequestFile(t *testing.T, f upgradeFiles, version string) {
	t.Helper()
	entries, err := os.ReadDir(f.runDir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if version == "" {
		if len(names) > 0 {
			t.Errorf("the koko server wrote %v, want nothing", names)
		}
		return
	}
	if len(names) != 1 || names[0] != "upgrade.request" {
		t.Fatalf("the run directory holds %v, want upgrade.request alone", names)
	}
	b, err := os.ReadFile(f.RequestFile)
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"version":"` + version + `"}`; string(b) != want {
		t.Errorf("the request holds %q, want %q", b, want)
	}
}

// wantRefused wants err to be the API error code, its message saying each
// of says.
func wantRefused(t *testing.T, err error, code api.Code, says ...string) {
	t.Helper()
	e, ok := errors.AsType[*api.Error](err)
	if !ok || e.Code != code.Name {
		t.Fatalf("server.upgrade failed with %v; want %s", err, code.Name)
	}
	for _, want := range says {
		if !strings.Contains(e.Message, want) {
			t.Errorf("the message %q does not say %q", e.Message, want)
		}
	}
}

// The Owner's server.upgrade writes exactly the request for the release it
// names, which the helper's path unit waits for, answers the upgrade it
// asked for, and logs who asked; without a version it asks for the latest,
// checking at once when no check found it yet.
func TestServerUpgradeWritesTheRequest(t *testing.T) {
	t.Parallel()
	for _, version := range []string{"v0.5.0", ""} {
		t.Run("version "+version, func(t *testing.T) {
			t.Parallel()
			h, rel, f := startUpgradable(t)
			resp, err := h.Call(api.ServerUpgrade, api.ServerUpgradeRequest{Version: version})
			if err != nil {
				t.Fatal(err)
			}
			if want := (api.ServerUpgradeResponse{From: "v0.4.0", To: "v0.5.0", State: "requested"}); resp != want {
				t.Errorf("server.upgrade answered %+v, want %+v", resp, want)
			}
			wantRequestFile(t, f, "v0.5.0")
			if checks := len(rel.Received()); (version == "") != (checks == 1) || checks > 1 {
				t.Errorf("the koko server checked for the latest release %d times", checks)
			}
			if log := h.Log(); !strings.Contains(log, `msg="asked the upgrade helper to upgrade the koko server" from=v0.4.0 to=v0.5.0 user=alice asked_by="unix socket"`) {
				t.Errorf("the koko server did not log who asked:\n%s", log)
			}
		})
	}
}

// A koko server that does not upgrade itself refuses with
// upgrade_unsupported, naming how it upgrades: the image by a new image,
// one run by hand by hand.
func TestServerUpgradeRefusesAKokoServerTheHelperDoesNotUpgrade(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		upgrade server.Upgrade
		says    string
	}{
		{"image", server.Upgrade{Image: "ghcr.io/cieons/koko"}, "set KOKO_VERSION=0.5.0 in .env beside its Compose file, then run docker compose pull && docker compose up -d"},
		{"none", server.Upgrade{}, "replace kokod and restart it"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := harness.Start(t, harness.Upgrade(tc.upgrade), harness.Version("v0.4.0"))
			_, err := h.Call(api.ServerUpgrade, api.ServerUpgradeRequest{Version: "v0.5.0"})
			wantRefused(t, err, api.CodeUpgradeUnsupported, tc.says)
		})
	}
}

// A version that is not a release newer than the koko server's is refused
// with invalid_request, naming the install line with KOKO_VERSION for going
// back, and nothing is written; so is no newer latest release.
func TestServerUpgradeRefusesAVersionNotNewer(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		version string
		says    []string
	}{
		{"v0.3.0", []string{"v0.3.0 is not newer than this koko server, v0.4.0", "sudo env KOKO_VERSION=v0.3.0 sh"}},
		{"v0.4.0", []string{"v0.4.0 is not newer", "KOKO_VERSION=v0.4.0"}},
		{"https://example.com/kokod", []string{"not a release's tag"}},
		{"v0.5.0-0.20261111090000-0123456789ab", []string{"not a release's tag"}},
	} {
		t.Run(tc.version, func(t *testing.T) {
			t.Parallel()
			h, _, f := startUpgradable(t)
			_, err := h.Call(api.ServerUpgrade, api.ServerUpgradeRequest{Version: tc.version})
			wantRefused(t, err, api.CodeInvalidRequest, tc.says...)
			wantRequestFile(t, f, "")
		})
	}
	t.Run("the latest is not newer", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		u := server.Upgrade{HelperUnit: filepath.Join(dir, "koko-upgrade.path"), RequestFile: filepath.Join(dir, "upgrade.request")}
		if err := os.WriteFile(u.HelperUnit, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		rel := harness.NewMachine(t).Release("v0.5.0")
		rel.Latest()
		h := harness.Start(t, harness.Upgrade(u), harness.Version("v0.5.0"), harness.UpdateCheck(rel))
		_, err := h.Call(api.ServerUpgrade, api.ServerUpgradeRequest{})
		wantRefused(t, err, api.CodeInvalidRequest, "the koko server runs v0.5.0, and the latest release is v0.5.0")
		if _, err := os.Stat(u.RequestFile); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("the koko server wrote a request: %v", err)
		}
	})
}

// A second upgrade is refused with upgrade_running while the first is asked
// for, its request still there, or runs, as its outcome says, and the
// request is left as it is.
func TestServerUpgradeRefusesASecondUpgrade(t *testing.T) {
	t.Parallel()
	t.Run("asked for", func(t *testing.T) {
		t.Parallel()
		h, _, f := startUpgradable(t)
		if _, err := h.Call(api.ServerUpgrade, api.ServerUpgradeRequest{Version: "v0.5.0"}); err != nil {
			t.Fatal(err)
		}
		_, err := h.Call(api.ServerUpgrade, api.ServerUpgradeRequest{Version: "v0.6.0"})
		wantRefused(t, err, api.CodeUpgradeRunning, "asked for already")
		wantRequestFile(t, f, "v0.5.0")
	})
	t.Run("running", func(t *testing.T) {
		t.Parallel()
		h, _, f := startUpgradable(t)
		writeRunning(t, f, h.Now())
		h.Advance(30 * time.Minute)
		_, err := h.Call(api.ServerUpgrade, api.ServerUpgradeRequest{Version: "v0.5.0"})
		wantRefused(t, err, api.CodeUpgradeRunning, "from v0.4.0 to v0.5.0 runs already")
		wantRequestFile(t, f, "")
	})
}

// An outcome that still says running more than 30 minutes after it started
// was abandoned, as by a helper killed or a machine rebooted mid-upgrade,
// and refuses no new upgrade.
func TestServerUpgradeTakesALongRunningOutcomeAsAbandoned(t *testing.T) {
	t.Parallel()
	h, _, f := startUpgradable(t)
	writeRunning(t, f, h.Now())
	h.Advance(30*time.Minute + time.Second)
	if _, err := h.Call(api.ServerUpgrade, api.ServerUpgradeRequest{Version: "v0.5.0"}); err != nil {
		t.Fatal(err)
	}
	wantRequestFile(t, f, "v0.5.0")
}

// writeRunning writes an outcome that says an upgrade from v0.4.0 to v0.5.0
// runs, started at started.
func writeRunning(t *testing.T, f upgradeFiles, started time.Time) {
	t.Helper()
	running := `{"state": "running", "from": "v0.4.0", "to": "v0.5.0", "started_at": "` + started.UTC().Format(time.RFC3339) + `"}`
	if err := os.WriteFile(f.OutcomeFile, []byte(running), 0o644); err != nil {
		t.Fatal(err)
	}
}
