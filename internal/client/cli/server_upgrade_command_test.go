package cli_test

import (
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/server/installer"
)

// The releases of koko server upgrade's tests: the one installed, and the
// newer one.
const (
	installedRelease = "v0.4.0"
	newerRelease     = "v0.5.0"
)

// upgradableMachine is a fake machine with the koko server of
// installedRelease installed and initialised, its upgrade helper's path
// unit started, and koko, of that release too, at /usr/local/bin/koko,
// root's, which koko update may replace; with the fake GitHub it
// downloaded from.
func upgradableMachine(t *testing.T) (*harness.Machine, *harness.Releases, []byte) {
	t.Helper()
	m := harness.NewMachine(t)
	rel := m.Release(installedRelease)
	install(t, m, "--owner", "alice")
	koko := m.KokoAt(m.Path("/usr/local/bin/koko"))
	return m, rel, koko
}

// checkedAtText is when a check ended, as koko server upgrade --check
// prints it for people.
var checkedAtText = regexp.MustCompile(`\d{4}-\d{2}-\d{2} \d{2}:\d{2} UTC`)

// koko server upgrade --check makes the koko server check for a new
// release at once, with exactly one request to GitHub, and says what it
// found and how the koko server upgrades; it changes nothing.
func TestServerUpgradeCheck(t *testing.T) {
	t.Parallel()
	for _, asJSON := range []bool{true, false} {
		t.Run(map[bool]string{true: "json", false: "text"}[asJSON], func(t *testing.T) {
			t.Parallel()
			m, rel, _ := upgradableMachine(t)
			rel.Publish(newerRelease)
			kokod := readFile(t, m, "/usr/local/bin/kokod")
			before := len(rel.Requests())
			args := []string{"server", "upgrade", "--check"}
			if asJSON {
				args = append(args, "--json")
			}
			r := m.Koko(args...)
			r.WantSuccess(t)
			if asJSON {
				harness.Golden(t, "server-upgrade-check.json", r.Stdout)
			} else {
				harness.Golden(t, "server-upgrade-check.txt", checkedAtText.ReplaceAllString(r.Stdout, "<time>"))
			}
			if got := rel.Requests()[before:]; !slices.Equal(got, []string{harness.LatestPath()}) {
				t.Errorf("koko server upgrade --check asked the fake GitHub for %q, want the latest release's redirect once", got)
			}
			if got := readFile(t, m, "/usr/local/bin/kokod"); got != kokod {
				t.Error("koko server upgrade --check changed kokod")
			}
			if _, err := os.Stat(m.Path(installer.RequestFile)); err == nil || m.HelperRuns() != 0 {
				t.Errorf("koko server upgrade --check asked for an upgrade (%v, %d helper runs)", err, m.HelperRuns())
			}
		})
	}
}

// koko server upgrade --check exits 1 with update_check_failed when the
// check fails, and 2 when given --version.
func TestServerUpgradeCheckRefusals(t *testing.T) {
	t.Parallel()
	m, _, _ := upgradableMachine(t)
	// The releases/latest redirect names no release yet.
	if msg := m.Koko("server", "upgrade", "--check", "--json").WantError(t, 1, "update_check_failed"); !strings.Contains(msg, harness.LatestPath()) {
		t.Errorf("the failed check says %q, which does not name what failed", msg)
	}
	m.Koko("server", "upgrade", "--check", "--version", newerRelease, "--json").WantError(t, 2, "invalid_usage")
}

// koko server upgrade asks the koko server to upgrade, says that every open
// SSH Session ends, waits through the restart for the helper's outcome,
// then brings this koko to the same release, as koko update does.
func TestServerUpgradeUpgradesTheServerThenTheCLI(t *testing.T) {
	t.Parallel()
	for _, asJSON := range []bool{true, false} {
		t.Run(map[bool]string{true: "json", false: "text"}[asJSON], func(t *testing.T) {
			t.Parallel()
			m, rel, _ := upgradableMachine(t)
			newer := rel.Publish(newerRelease)
			args := []string{"server", "upgrade"}
			if asJSON {
				args = append(args, "--json")
			}
			r := m.Koko(args...)
			if asJSON {
				r.WantSuccess(t)
				harness.Golden(t, "server-upgrade.json", r.Stdout)
			} else {
				if r.Status != 0 {
					t.Fatalf("exit status %d, stderr %q", r.Status, r.Stderr)
				}
				harness.Golden(t, "server-upgrade.txt", r.Stdout)
				if want := "Asked the koko server to upgrade from v0.4.0 to v0.5.0. It restarts, and every open SSH Session through it ends.\n"; r.Stderr != want {
					t.Errorf("koko server upgrade said on stderr %q, want %q", r.Stderr, want)
				}
			}
			if got := readFile(t, m, "/usr/local/bin/kokod"); got != string(newer.Content("kokod-linux-amd64")) {
				t.Errorf("/usr/local/bin/kokod holds %q, want the newer release's", got)
			}
			if got := readFile(t, m, "/usr/local/bin/koko"); got != string(newer.Content("koko-linux-amd64")) {
				t.Errorf("/usr/local/bin/koko holds %q, want the newer release's", got)
			}
			if m.HelperRuns() != 1 {
				t.Errorf("the upgrade helper ran %d times, want once", m.HelperRuns())
			}
			wantInitialised(t, m, "alice")
		})
	}
}

// When the koko server's upgrade fails, koko server upgrade exits 1 with
// upgrade_failed and the outcome's message, and leaves this koko as it was;
// asked again, it waits for the new outcome.
func TestServerUpgradeFailsWithTheOutcome(t *testing.T) {
	t.Parallel()
	m, rel, koko := upgradableMachine(t)
	rel.PublishFailing(newerRelease, false)
	r := m.Koko("server", "upgrade", "--json")
	msg := r.WantError(t, 1, "upgrade_failed")
	for _, want := range []string{"from v0.4.0 to v0.5.0 failed (failed)", "did not open its socket", "the previous kokod runs again", "this koko was left as it was"} {
		if !strings.Contains(msg, want) {
			t.Errorf("the failure says %q, which does not say %q", msg, want)
		}
	}
	if got := readFile(t, m, "/usr/local/bin/koko"); got != string(koko) {
		t.Errorf("koko server upgrade changed /usr/local/bin/koko to %q", got)
	}
	wantInitialised(t, m, "alice")

	// Asked again for the same release, fixed, it waits for the new outcome,
	// not the failed one there before.
	rel.Publish(newerRelease)
	r = m.Koko("server", "upgrade", "--json")
	r.WantSuccess(t)
	if !strings.Contains(r.Stdout, `"state": "succeeded"`) || m.HelperRuns() != 2 {
		t.Errorf("the second upgrade printed %s after %d helper runs, want it succeeded after two", r.Stdout, m.HelperRuns())
	}
}

// A version that is not a release newer than the koko server's is refused
// by the koko server with invalid_request, and one that is no release's tag
// by koko itself, with exit 2; neither asks for an upgrade.
func TestServerUpgradeRefusesAVersionNotNewer(t *testing.T) {
	t.Parallel()
	m, _, _ := upgradableMachine(t)
	if msg := m.Koko("server", "upgrade", "--version", "v0.3.0", "--json").WantError(t, 1, "invalid_request"); !strings.Contains(msg, "KOKO_VERSION=v0.3.0") {
		t.Errorf("the refusal says %q, which does not name going back with KOKO_VERSION", msg)
	}
	m.Koko("server", "upgrade", "--version", "latest", "--json").WantError(t, 2, "invalid_usage")
	if m.HelperRuns() != 0 {
		t.Errorf("the upgrade helper ran %d times", m.HelperRuns())
	}
}

// koko server install refuses, before any download, to replace a kokod
// that is a newer release than this koko, naming both and sudo koko update;
// KOKO_VERSION naming this koko's own version lets it go back on purpose.
func TestServerInstallRefusesANewerKokod(t *testing.T) {
	t.Parallel()
	m, rel, _ := upgradableMachine(t)
	b := harness.KokodBuild()
	b.Version = newerRelease
	newer, err := os.ReadFile(m.NewKokod(b))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(m.Path("/usr/local/bin/kokod"), newer, 0o755); err != nil {
		t.Fatal(err)
	}
	before := len(rel.Requests())
	for _, env := range [][]string{nil, {"KOKO_VERSION=v0.3.0"}} {
		msg := m.KokoEnv(env, "server", "install", "--json").WantError(t, 1, "kokod_newer")
		for _, want := range []string{newerRelease, installedRelease, "sudo koko update", "KOKO_VERSION=" + installedRelease, "nothing was written"} {
			if !strings.Contains(msg, want) {
				t.Errorf("the refusal with %v says %q, which does not say %q", env, msg, want)
			}
		}
	}
	if got := rel.Requests()[before:]; len(got) > 0 {
		t.Errorf("the refused install asked the fake GitHub for %q", got)
	}
	if got := readFile(t, m, "/usr/local/bin/kokod"); got != string(newer) {
		t.Error("the refused install changed kokod")
	}

	r := m.KokoEnv([]string{"KOKO_VERSION=" + installedRelease}, "server", "install", "--json")
	r.WantSuccess(t)
	if got, err := m.Build(m.Path("/usr/local/bin/kokod")); err != nil || got.Version != installedRelease {
		t.Errorf("install with KOKO_VERSION=%s left kokod %+v (%v), want it back at %s", installedRelease, got, err, installedRelease)
	}
	wantInitialised(t, m, "alice")
}

// When koko update may not replace this koko, as one run from elsewhere,
// koko server upgrade leaves it, its JSON's cli null, and says why and how
// to bring it to the koko server's release.
func TestServerUpgradeSaysWhyTheCLIStays(t *testing.T) {
	t.Parallel()
	m, rel, _ := upgradableMachine(t)
	rel.Publish(newerRelease)
	elsewhere := m.Home() + "/downloads/koko"
	koko := m.KokoAt(elsewhere)
	r := m.Koko("server", "upgrade")
	if r.Status != 0 {
		t.Fatalf("exit status %d, stderr %q", r.Status, r.Stderr)
	}
	for _, want := range []string{"This koko stays v0.4.0", elsewhere, "koko update --version v0.5.0"} {
		if !strings.Contains(r.Stderr, want) {
			t.Errorf("koko server upgrade said on stderr %q, which does not say %q", r.Stderr, want)
		}
	}
	if got := fileText(t, elsewhere); got != string(koko) {
		t.Errorf("koko server upgrade changed %s", elsewhere)
	}
	if strings.Contains(r.Stdout, "Updated koko") {
		t.Errorf("koko server upgrade printed %q", r.Stdout)
	}
}
