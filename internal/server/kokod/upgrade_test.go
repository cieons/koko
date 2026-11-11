//go:build unix

package kokod_test

import (
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/server/installer"
	"github.com/cieons/koko/internal/server/store"
)

// The releases of the upgrade helper's tests: the koko server installed,
// and the newer one it asks for.
const (
	installedRelease = "v0.4.0"
	newerRelease     = "v0.5.0"
)

// kokodAsset is the kokod of a release for the fake machine's platform.
const kokodAsset = "kokod-linux-amd64"

// upgradable is a fake machine with the koko server of installedRelease
// installed and initialised, its upgrade helper's path unit started, and
// the fake GitHub it downloaded from.
func upgradable(t *testing.T) (*harness.Machine, *harness.Releases) {
	t.Helper()
	m := harness.NewMachine(t)
	rel := m.Release(installedRelease)
	m.Koko("server", "install", "--owner", "alice", "--json").WantSuccess(t)
	if !m.HelperEnabled() {
		t.Fatal("install left the upgrade helper's path unit disabled")
	}
	return m, rel
}

// ask writes text as the koko server's upgrade request, as the koko server
// does: into a temporary file beside it, which owner owns when it is not
// "", renamed into place, so that the path unit never sees it half-written.
func ask(t *testing.T, m *harness.Machine, text, owner string) {
	t.Helper()
	tmp := m.Path(installer.RequestFile + ".tmp")
	if err := os.WriteFile(tmp, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	if owner != "" {
		if err := m.Chown(tmp, owner); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Rename(tmp, m.Path(installer.RequestFile)); err != nil {
		t.Fatal(err)
	}
}

// outcome reads the outcome the helper recorded.
func outcome(t *testing.T, m *harness.Machine) installer.Outcome {
	t.Helper()
	o, err := installer.ReadOutcome(m.Path(installer.OutcomeFile))
	if err != nil || o == nil {
		t.Fatalf("the helper recorded no outcome (%v)", err)
	}
	return *o
}

// wantOutcome wants the helper to have recorded state, from installedRelease
// to to, with a start and an end, and a message saying each of says.
func wantOutcome(t *testing.T, m *harness.Machine, state, to string, says ...string) installer.Outcome {
	t.Helper()
	o := outcome(t, m)
	if o.State != state || o.From != installedRelease || o.To != to || o.StartedAt == "" || o.FinishedAt == "" {
		t.Errorf("the helper recorded %+v, want %s from %s to %s, started and finished", o, state, installedRelease, to)
	}
	for _, want := range says {
		if !strings.Contains(o.Message, want) {
			t.Errorf("the outcome's message %q does not say %q", o.Message, want)
		}
	}
	return o
}

// wantInstalled wants the kokod at /usr/local/bin/kokod to be content, and
// the koko service to run on the record as it was.
func wantInstalled(t *testing.T, m *harness.Machine, content []byte) {
	t.Helper()
	if got, err := os.ReadFile(m.Path(installer.BinaryPath)); err != nil || string(got) != string(content) {
		t.Errorf("/usr/local/bin/kokod holds %q (%v), want %q", got, err, content)
	}
	if !m.Running() {
		t.Error("the koko service does not run")
	}
	r := m.Koko("space", "ls", "--json")
	r.WantSuccess(t)
	if !strings.Contains(r.Stdout, `"name": "alice"`) {
		t.Errorf("space ls = %s, want alice's Personal Space", r.Stdout)
	}
}

// wantNoRequest wants the request gone, and nothing left of the download.
func wantNoRequest(t *testing.T, m *harness.Machine) {
	t.Helper()
	if _, err := os.Lstat(m.Path(installer.RequestFile)); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the helper left the request: %v", err)
	}
	left, err := filepath.Glob(m.Path(installer.UpgradeDir + "/download-*"))
	if err != nil || len(left) > 0 {
		t.Errorf("the helper left its download behind: %v (%v)", left, err)
	}
}

// downloads returns the release assets requested of the fake GitHub.
func downloads(rel *harness.Releases) []string {
	return slices.DeleteFunc(rel.Requests(), func(p string) bool { return !strings.Contains(p, "/download/") })
}

// When the koko server asks for a newer release, the path unit starts the
// helper, which downloads exactly that release's SHA256SUMS and kokod,
// keeps the kokod installed as kokod.previous, and runs the new one's
// install: the new kokod runs on the record as it was, with the units
// written again, and the outcome is succeeded.
func TestTheUpgradeHelperInstallsANewerRelease(t *testing.T) {
	t.Parallel()
	m, rel := upgradable(t)
	previous := rel.Content(kokodAsset)
	newer := rel.Publish(newerRelease)
	before := len(downloads(rel))

	ask(t, m, `{"version": "`+newerRelease+`"}`, "")
	log := m.WaitHelper(1)
	wantOutcome(t, m, installer.UpgradeSucceeded, newerRelease)
	if o := outcome(t, m); o.Message != "" {
		t.Errorf("the outcome of a success says %q", o.Message)
	}
	if got, want := downloads(rel)[before:], []string{newer.Path("SHA256SUMS"), newer.Path(kokodAsset)}; !slices.Equal(got, want) {
		t.Errorf("the helper downloaded %v, want %v", got, want)
	}
	wantInstalled(t, m, newer.Content(kokodAsset))
	m.WaitLog("koko server started", 2)
	if got, err := os.ReadFile(m.Path(installer.UpgradeDir + "/kokod.previous")); err != nil || string(got) != string(previous) {
		t.Errorf("kokod.previous holds %q (%v), want the kokod installed before", got, err)
	}
	for _, unit := range []string{installer.UnitPath, installer.UpgradePathUnit, installer.UpgradeServiceUnit} {
		if _, err := os.Stat(m.Path(unit)); err != nil {
			t.Errorf("the upgrade left no %s: %v", unit, err)
		}
	}
	if ran := m.Ran(); !slices.Equal(ran[len(ran)-2:], []string{"upgrade", "install --json"}) {
		t.Errorf("the machine ran kokod %q, want upgrade then the new one's install --json", ran)
	}
	wantNoRequest(t, m)
	if strings.Contains(log, "status 1") {
		t.Errorf("the helper failed:\n%s", log)
	}
}

// A request the helper does not take is removed, refused and recorded as
// failed, with nothing downloaded and the kokod installed left running: a
// symbolic link, even to a good request, a file over 256 bytes, one owned
// by another user than koko, and one naming a release no newer than the
// kokod installed, or anything but a release's tag.
func TestTheUpgradeHelperRefusesARequestItDoesNotTake(t *testing.T) {
	t.Parallel()
	good := `{"version": "` + newerRelease + `"}`
	for _, tc := range []struct {
		name, text, owner string
		link              bool
		to                string
		says              string
	}{
		{name: "a link", text: good, link: true, says: "symbolic link"},
		{name: "too large", text: `{"version": "` + newerRelease + `"` + strings.Repeat(" ", 256) + `}`, says: "over 256 bytes"},
		{name: "another owner", text: good, owner: "alice", says: "owned by alice"},
		{name: "older", text: `{"version": "v0.3.0"}`, to: "v0.3.0", says: "not newer"},
		{name: "the same", text: `{"version": "` + installedRelease + `"}`, to: installedRelease, says: "not newer"},
		{name: "a URL", text: `{"version": "https://example.com/kokod"}`, says: "not a release's tag"},
		{name: "another field", text: `{"version": "` + newerRelease + `", "url": "https://example.com/kokod"}`, says: "does not read as a request"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m, rel := upgradable(t)
			installed := rel.Content(kokodAsset)
			rel.Publish(newerRelease)
			before := len(downloads(rel))
			if tc.link {
				target := m.Path("/run/koko/elsewhere")
				if err := os.WriteFile(target, []byte(tc.text), 0o600); err != nil {
					t.Fatal(err)
				}
				tmp := m.Path(installer.RequestFile + ".tmp")
				if err := os.Symlink(target, tmp); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(tmp, m.Path(installer.RequestFile)); err != nil {
					t.Fatal(err)
				}
			} else {
				ask(t, m, tc.text, tc.owner)
			}
			log := m.WaitHelper(1)
			wantOutcome(t, m, installer.UpgradeFailed, tc.to, "refused", tc.says, "nothing was changed")
			if got := downloads(rel)[before:]; len(got) > 0 {
				t.Errorf("the helper downloaded %v", got)
			}
			wantInstalled(t, m, installed)
			wantNoRequest(t, m)
			if strings.Count(m.Log(), "koko server started") != 1 {
				t.Errorf("the koko service restarted:\n%s", m.Log())
			}
			if !strings.Contains(log, "status 1") {
				t.Errorf("the helper did not fail:\n%s", log)
			}
		})
	}
}

// A download or check that fails leaves everything as it was: the kokod
// installed runs on, never restarted, and no kokod.previous is kept.
func TestTheUpgradeHelperChangesNothingWhenTheDownloadFails(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		spoil func(newer *harness.Releases)
		says  string
	}{
		{"a wrong SHA-256", func(newer *harness.Releases) { newer.Put(kokodAsset, []byte("#!/bin/sh\n# not this kokod\n")) }, "SHA-256"},
		{"no kokod", func(newer *harness.Releases) { newer.Handle(kokodAsset, notFound{}) }, "404"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m, rel := upgradable(t)
			installed := rel.Content(kokodAsset)
			tc.spoil(rel.Publish(newerRelease))

			ask(t, m, `{"version": "`+newerRelease+`"}`, "")
			m.WaitHelper(1)
			wantOutcome(t, m, installer.UpgradeFailed, newerRelease, tc.says, "nothing was changed")
			wantInstalled(t, m, installed)
			wantNoRequest(t, m)
			if _, err := os.Stat(m.Path(installer.UpgradeDir + "/kokod.previous")); !errors.Is(err, fs.ErrNotExist) {
				t.Errorf("the helper kept a kokod.previous: %v", err)
			}
			if strings.Count(m.Log(), "koko server started") != 1 {
				t.Errorf("the koko service restarted:\n%s", m.Log())
			}
		})
	}
}

// A new kokod whose service never opens its socket, after it migrated the
// database, is stopped; the database it migrated is kept as
// koko.db.failed-<to>, the snapshot is put in its place, and the previous
// kokod runs again on the record as it was: failed. A second failure of the
// same release does the same, keeping its database apart.
func TestTheUpgradeHelperPutsThePreviousKokodBack(t *testing.T) {
	t.Parallel()
	m, rel := upgradable(t)
	installed := rel.Content(kokodAsset)
	rel.PublishFailing(newerRelease, true)

	ask(t, m, `{"version": "`+newerRelease+`"}`, "")
	m.WaitHelper(1)
	wantOutcome(t, m, installer.UpgradeFailed, newerRelease,
		"kokod "+newerRelease+" did not install", "did not open its socket", "the previous kokod runs again",
		fmt.Sprintf("koko.db.before-%d", store.SchemaVersion()+1), "koko.db.failed-"+newerRelease)
	wantInstalled(t, m, installed)
	if got := schemaVersion(t, m.Path("/var/lib/koko/koko.db")); got != store.SchemaVersion() {
		t.Errorf("koko.db is at schema version %d, want %d, as the previous kokod left it", got, store.SchemaVersion())
	}
	if got := schemaVersion(t, m.Path("/var/lib/koko/koko.db.failed-"+newerRelease)); got != store.SchemaVersion()+1 {
		t.Errorf("koko.db.failed-%s is at schema version %d, want the new kokod's %d", newerRelease, got, store.SchemaVersion()+1)
	}
	if ran := m.Ran(); !slices.Equal(ran[len(ran)-3:], []string{"upgrade", "install --json", "install --json"}) {
		t.Errorf("the machine ran kokod %q, want upgrade, the new one's install, then the previous one's", ran)
	}
	whats := callNames(m.Calls())
	if i := slices.Index(whats, "systemctl stop koko.service"); i < 0 || i+1 >= len(whats) ||
		whats[i+1] != "systemctl reset-failed koko.service" || !slices.Contains(whats[i:], "systemctl restart koko.service") {
		t.Errorf("the helper did not stop the koko service and reset its start limit before installing the previous kokod: %v", whats)
	}
	wantNoRequest(t, m)

	// Asked again for the same release, which fails again, it keeps the
	// second migrated database under a name of its own, and puts the
	// previous kokod back again.
	ask(t, m, `{"version": "`+newerRelease+`"}`, "")
	m.WaitHelper(2)
	wantOutcome(t, m, installer.UpgradeFailed, newerRelease, "the previous kokod runs again", "koko.db.failed-"+newerRelease+".2")
	wantInstalled(t, m, installed)
	if got := schemaVersion(t, m.Path("/var/lib/koko/koko.db.failed-"+newerRelease+".2")); got != store.SchemaVersion()+1 {
		t.Errorf("koko.db.failed-%s.2 is at schema version %d, want %d", newerRelease, got, store.SchemaVersion()+1)
	}
}

// A previous kokod that does not start either leaves the outcome
// failed_not_restored, naming the journal to read.
func TestTheUpgradeHelperSaysWhenThePreviousKokodDoesNotStartEither(t *testing.T) {
	t.Parallel()
	m, rel := upgradable(t)
	rel.PublishFailing(newerRelease, false)
	m.Fail(rel.Content(kokodAsset))

	ask(t, m, `{"version": "`+newerRelease+`"}`, "")
	m.WaitHelper(1)
	wantOutcome(t, m, installer.UpgradeFailedNotRestored, newerRelease,
		"kokod "+newerRelease+" did not install", "putting kokod "+installedRelease+" back failed", "journalctl -u koko -u koko-upgrade")
	wantNoRequest(t, m)
}

// notFound answers every request 404 Not Found.
type notFound struct{}

func (notFound) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	http.NotFound(w, r)
}

// schemaVersion reads the schema version of the SQLite file at path.
func schemaVersion(t *testing.T, path string) int {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	var v int
	if err := db.QueryRowContext(t.Context(), "PRAGMA user_version").Scan(&v); err != nil {
		t.Fatalf("read the schema version of %s: %v", path, err)
	}
	return v
}

// callNames returns what each of calls asked of the machine, in order.
func callNames(calls []harness.Call) []string {
	var names []string
	for _, c := range calls {
		names = append(names, c.What)
	}
	return names
}
