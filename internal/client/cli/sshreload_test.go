package cli_test

import (
	"encoding/json/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/cieons/koko/internal/harness"
)

// reloaded is what koko ssh reload --json prints.
type reloaded struct {
	SSHConfig string   `json:"ssh_config"`
	Names     []string `json:"names"`
	Added     []string `json:"added"`
	Removed   []string `json:"removed"`
}

// koko ssh reload on a joined device rewrites the bare names in koko's
// ssh_config at once, from the Hosts its User can see now, and says how
// many it holds and which came and went: a Host someone else recorded in
// the User's Space comes, and one they Retired goes. Run again, it says
// none changed.
func TestSSHReloadRewritesTheBareNamesAtOnce(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	bareNamesSetup(t, h)
	bob, _ := joinAs(t, deviceAdd(t, h, "laptop", "--user", "bob"))
	addHost(t, h.Koko, "api", "space=ops")
	h.Koko("host", "retire", "db1").WantSuccess(t)
	wantBareNames(t, bob, "before the reload", "build", "db1")

	r := bob.Koko("ssh", "reload", "--json")
	r.WantSuccess(t)
	var got reloaded
	if err := json.Unmarshal([]byte(r.Stdout), &got); err != nil {
		t.Fatalf("ssh reload --json printed %q: %v", r.Stdout, err)
	}
	want := reloaded{
		SSHConfig: filepath.Join(kokoDir(bob), "ssh_config"),
		Names:     []string{"api", "build"},
		Added:     []string{"api"},
		Removed:   []string{"db1"},
	}
	if !equalReloaded(got, want) {
		t.Errorf("ssh reload --json printed %+v, want %+v", got, want)
	}
	wantBareNames(t, bob, "after the reload", "api", "build")

	r = bob.Koko("ssh", "reload")
	r.WantSuccess(t)
	if want := "koko's ssh_config holds 2 bare names; none changed.\n"; r.Stdout != want {
		t.Errorf("ssh reload again printed %q, want %q", r.Stdout, want)
	}

	addHost(t, h.Koko, "cache", "space=ops")
	r = bob.Koko("ssh", "reload")
	r.WantSuccess(t)
	if want := "koko's ssh_config holds 3 bare names.\nAdded:   cache\n"; r.Stdout != want {
		t.Errorf("ssh reload after alice added cache printed %q, want %q", r.Stdout, want)
	}
}

// On the koko server's own machine koko ssh reload rewrites the bare names
// that koko ssh setup wrote, as the Local caller sees them, such as a Host
// a device recorded since; for an OS user who never ran koko ssh setup it
// writes nothing and says to run it.
func TestSSHReloadOnTheKokoServersMachine(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	addHost(t, h.Koko, "web1")
	path := filepath.Join(h.Home(), ".ssh", "koko", "ssh_config")

	msg := h.Koko("ssh", "reload", "--json").WantError(t, 1, "ssh_not_set_up")
	if want := "koko ssh setup"; !strings.Contains(msg, want) {
		t.Errorf("ssh reload before ssh setup says %q, want it to name %q", msg, want)
	}
	if _, err := os.Lstat(path); err == nil {
		t.Errorf("ssh reload before ssh setup wrote %s", path)
	}

	h.Koko("ssh", "setup").WantSuccess(t)
	d := h.Join(harness.Owner)
	addHost(t, d.Koko, "db1")
	if got := hostBareNames(t, h.Home()); len(got) != 1 {
		t.Fatalf("before the reload, koko's ssh_config on the koko server's machine holds %q, want web1 alone", got)
	}
	r := h.Koko("ssh", "reload")
	r.WantSuccess(t)
	if want := "koko's ssh_config holds 2 bare names.\nAdded:   db1\n"; r.Stdout != want {
		t.Errorf("ssh reload printed %q, want %q", r.Stdout, want)
	}
}

func equalReloaded(a, b reloaded) bool {
	return a.SSHConfig == b.SSHConfig && slices.Equal(a.Names, b.Names) && slices.Equal(a.Added, b.Added) && slices.Equal(a.Removed, b.Removed)
}
