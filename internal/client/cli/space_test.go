package cli_test

import (
	"encoding/json/v2"
	"strings"
	"testing"

	"github.com/cieons/koko/internal/harness"
)

func TestSpaceAddPrintsTheSpaceItMade(t *testing.T) {
	h := harness.Start(t)
	r := h.Koko("space", "add", "ops", "--json")
	r.WantSuccess(t)
	harness.Golden(t, "space-add.json", r.Stdout)
}

func TestSpaceAddForPeople(t *testing.T) {
	h := harness.Start(t)
	r := h.Koko("space", "add", "ops")
	r.WantSuccess(t)
	if !strings.HasPrefix(r.Stdout, "Added Space ops (space_") {
		t.Errorf("stdout = %q", r.Stdout)
	}
}

// space ls --json is a bare array of every Space, the Owner's Personal Space
// included, sorted by name, carrying the ids space add printed.
func TestSpaceLsListsEverySpace(t *testing.T) {
	h := harness.Start(t)
	var added []string
	for _, name := range []string{"ops", "data"} {
		r := h.Koko("space", "add", name, "--json")
		r.WantSuccess(t)
		var s struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal([]byte(r.Stdout), &s); err != nil {
			t.Fatal(err)
		}
		added = append(added, s.ID)
	}
	r := h.Koko("space", "ls", "--json")
	r.WantSuccess(t)
	harness.Golden(t, "space-ls.json", r.Stdout)
	var listed []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal([]byte(r.Stdout), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed) != 3 || listed[0].Name != harness.Owner || listed[1].ID != added[1] || listed[2].ID != added[0] {
		t.Errorf("space ls = %+v, want %s's Personal Space, then data %s and ops %s", listed, harness.Owner, added[1], added[0])
	}
}

// On a koko server just initialised, the one Space is the Owner's Personal
// Space, named after them.
func TestSpaceLsOnAFreshKokoServer(t *testing.T) {
	h := harness.Start(t)
	r := h.Koko("space", "ls", "--json")
	r.WantSuccess(t)
	harness.Golden(t, "space-ls-fresh.json", r.Stdout)
}

func TestSpaceLsForPeople(t *testing.T) {
	h := harness.Start(t)
	h.Koko("space", "add", "ops").WantSuccess(t)
	r := h.Koko("space", "ls")
	r.WantSuccess(t)
	lines := strings.Split(strings.TrimSpace(r.Stdout), "\n")
	if len(lines) != 3 || !strings.HasPrefix(lines[0], "NAME") || !strings.HasPrefix(lines[1], harness.Owner+" ") ||
		!strings.HasPrefix(lines[2], "ops ") {
		t.Errorf("stdout = %q, want a NAME header, then a row for %s and one for ops", r.Stdout, harness.Owner)
	}
}

func TestSpaceAddRefusesATakenName(t *testing.T) {
	h := harness.Start(t)
	h.Koko("space", "add", "ops").WantSuccess(t)
	r := h.Koko("space", "add", "ops", "--json")
	r.WantError(t, 1, "name_taken")
	harness.Golden(t, "space-add-taken.json", r.Stderr)

	r = h.Koko("space", "ls", "--json")
	if got := strings.Count(r.Stdout, `"ops"`); got != 1 {
		t.Errorf("space ls holds ops %d times:\n%s", got, r.Stdout)
	}
}

// A Space's name is a lowercase DNS label of 1 to 63 characters.
func TestSpaceAddRefusesAnInvalidName(t *testing.T) {
	h := harness.Start(t)
	for _, name := range []string{"Ops", "ops-", "ops_1", strings.Repeat("a", 64)} {
		r := h.Koko("space", "add", name, "--json")
		r.WantError(t, 1, "invalid_name")
	}
	h.Koko("space", "add", strings.Repeat("a", 63)).WantSuccess(t)
	if r := h.Koko("space", "ls", "--json"); strings.Count(r.Stdout, `"name"`) != 2 {
		t.Errorf("space ls = %s, want only the Owner's Personal Space and the 63-character Space", r.Stdout)
	}
}

// Without --json the error is a line for people, still on stderr only.
func TestErrorForPeopleGoesToStderr(t *testing.T) {
	h := harness.Start(t)
	h.Koko("space", "add", "ops").WantSuccess(t)
	r := h.Koko("space", "add", "ops")
	if r.Status != 1 || r.Stdout != "" || r.Stderr != "koko: a Space named \"ops\" already exists\n" {
		t.Errorf("status %d, stdout %q, stderr %q", r.Status, r.Stdout, r.Stderr)
	}
}
