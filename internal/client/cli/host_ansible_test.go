package cli_test

import (
	"encoding/json/v2"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/harness/sshhost"
)

// managedHostWith records an in-process Host as the Host named hostName,
// with pairs, as the Local caller, and Enrolls it.
func managedHostWith(t *testing.T, h *harness.Harness, hostName string, pairs ...string) {
	t.Helper()
	addHostAt(t, h.Koko, hostName, sshhost.Start(t, sshhost.Password(hostPassword)), pairs...)
	enroll(t, h, hostName)
}

// host ls --ansible prints Ansible's dynamic inventory of the Managed Hosts
// host ls lists: each Host reached as <name>.koko, its Space and Attributes
// as koko_ variables, and a group for its Space and for each Attribute that
// is not a date or a number Preset, named with '_' for what Ansible does not
// take in a name.
func TestHostLsAnsiblePrintsTheManagedHostsAsAnInventory(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	h.Koko("space", "add", "eu-ops").WantSuccess(t)
	managedHostWith(t, h, "web1", "space=eu-ops", "role=web", "region=eu", "expires_at=2027-01-31", "team=Core/Ops 2")
	// The test's printedHost reads no number, so the price goes on after.
	h.Koko("host", "set", "web1", "price=12.5").WantSuccess(t)
	managedHostWith(t, h, "db1", "space=eu-ops", "role=db", "region=eu")
	managedHostWith(t, h, "app1", "region=us")
	addHost(t, h.Koko, "web2", "space=eu-ops", "role=web", "region=eu")

	r := h.Koko("host", "ls", "--ansible")
	r.WantSuccess(t)
	harness.Golden(t, "host-ls-ansible.json", r.Stdout)

	r = h.Koko("host", "ls", "region=eu", "--ansible")
	r.WantSuccess(t)
	var inv struct {
		Meta struct {
			Hostvars map[string]map[string]any `json:"hostvars"`
		} `json:"_meta"`
	}
	if err := json.Unmarshal([]byte(r.Stdout), &inv); err != nil {
		t.Fatalf("host ls region=eu --ansible printed %q: %v", r.Stdout, err)
	}
	if got, want := slices.Sorted(maps.Keys(inv.Meta.Hostvars)), []string{"db1", "web1"}; !slices.Equal(got, want) {
		t.Errorf("host ls region=eu --ansible lists %q, want %q", got, want)
	}
}

// --ansible prints only the inventory, so it takes no flag that changes what
// host ls prints.
func TestHostLsAnsibleRefusesWhatChangesTheList(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	h.Koko("host", "ls", "--ansible", "--json").WantError(t, 2, "invalid_usage")
	for _, flag := range []string{"--all", "--with-note"} {
		r := h.Koko("host", "ls", "--ansible", flag)
		if r.Status != 2 || r.Stdout != "" || !strings.Contains(r.Stderr, "--ansible") {
			t.Errorf("host ls --ansible %s = %+v, want it refused as a usage error naming --ansible", flag, r)
		}
	}
}
