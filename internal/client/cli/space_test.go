package cli_test

import (
	"encoding/json/v2"
	"slices"
	"strings"
	"testing"

	"github.com/cieons/koko/internal/harness"
)

func TestSpaceAddPrintsTheSpaceItMade(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	r := h.Koko("space", "add", "ops", "--json")
	r.WantSuccess(t)
	harness.Golden(t, "space-add.json", r.Stdout)
}

func TestSpaceAddForPeople(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
	h := harness.Start(t)
	r := h.Koko("space", "ls", "--json")
	r.WantSuccess(t)
	harness.Golden(t, "space-ls-fresh.json", r.Stdout)
}

func TestSpaceLsForPeople(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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

// spaceShown is a Space as space show --json prints it.
type spaceShown struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Personal bool   `json:"personal"`
	Members  []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"members"`
	Counts struct {
		Hosts   int `json:"hosts"`
		Secrets int `json:"secrets"`
	} `json:"counts"`
}

// memberNames returns the names of the Members s lists, in its order.
func (s spaceShown) memberNames() []string {
	names := []string{}
	for _, m := range s.Members {
		names = append(names, m.Name)
	}
	return names
}

// spaceShow runs space show --json for the Space named space on the device
// run runs commands on, and returns what it printed.
func spaceShow(t *testing.T, run func(args ...string) harness.Result, space string) spaceShown {
	t.Helper()
	r := run("space", "show", space, "--json")
	r.WantSuccess(t)
	var s spaceShown
	if err := json.Unmarshal([]byte(r.Stdout), &s); err != nil {
		t.Fatalf("space show --json printed %q: %v", r.Stdout, err)
	}
	return s
}

// space show prints a Space with its Members and how many Hosts and Secrets
// it holds: a new shared Space has none of either.
func TestSpaceShowShowsANewSpace(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	added := h.Koko("space", "add", "ops", "--json")
	added.WantSuccess(t)
	r := h.Koko("space", "show", "ops", "--json")
	r.WantSuccess(t)
	harness.Golden(t, "space-show.json", r.Stdout)

	var a struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(added.Stdout), &a); err != nil {
		t.Fatal(err)
	}
	if s := spaceShow(t, h.Koko, "ops"); s.ID != a.ID || s.Personal || len(s.Members) != 0 || s.Counts.Hosts != 0 || s.Counts.Secrets != 0 {
		t.Errorf("space show ops = %+v, want the shared Space %s with no Members, Hosts or Secrets", s, a.ID)
	}
}

// A User's Personal Space is shown as personal, with no Members.
func TestSpaceShowShowsAPersonalSpace(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	if s := spaceShow(t, h.Koko, harness.Owner); s.Name != harness.Owner || !s.Personal || len(s.Members) != 0 {
		t.Errorf("space show %s = %+v, want the Owner's Personal Space with no Members", harness.Owner, s)
	}
}

func TestSpaceShowForPeople(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	h.Koko("space", "add", "ops").WantSuccess(t)
	r := h.Koko("space", "show", "ops")
	r.WantSuccess(t)
	for _, want := range []string{"Space ops (space_", "Members: none", "Hosts: 0", "Secrets: 0"} {
		if !strings.Contains(r.Stdout, want) {
			t.Errorf("stdout = %q, want it to say %q", r.Stdout, want)
		}
	}
}

func TestSpaceShowRefusesAnUnknownSpace(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	r := h.Koko("space", "show", "ops", "--json")
	r.WantError(t, 1, "unknown_space")
	harness.Golden(t, "space-show-unknown.json", r.Stderr)
	h.Koko("space", "show", "Ops", "--json").WantError(t, 1, "invalid_name")
}

// spaceMembers runs space member add or rm --json and returns the Members
// of the Space it printed.
func spaceMembers(t *testing.T, h *harness.Harness, verb, space string, users ...string) []string {
	t.Helper()
	r := h.Koko(append([]string{"space", "member", verb, space, "--json"}, users...)...)
	r.WantSuccess(t)
	var s spaceShown
	if err := json.Unmarshal([]byte(r.Stdout), &s); err != nil {
		t.Fatalf("space member %s --json printed %q: %v", verb, r.Stdout, err)
	}
	return s.memberNames()
}

// space member add makes several Users Members of a Space at once, invited
// Users who have not joined included, and prints the Space with its
// Members, by name.
func TestSpaceMemberAddAddsSeveralUsers(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	h.Koko("space", "add", "ops").WantSuccess(t)
	h.Koko("user", "invite", "carol").WantSuccess(t)
	h.Koko("user", "invite", "bob").WantSuccess(t)
	r := h.Koko("space", "member", "add", "ops", "carol", "bob", "--json")
	r.WantSuccess(t)
	harness.Golden(t, "space-member-add.json", r.Stdout)

	if got := spaceShow(t, h.Koko, "ops").memberNames(); !slices.Equal(got, []string{"bob", "carol"}) {
		t.Errorf("space show ops lists the Members %q, want bob and carol", got)
	}
}

func TestSpaceMemberAddForPeople(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	h.Koko("space", "add", "ops").WantSuccess(t)
	h.Koko("user", "invite", "bob").WantSuccess(t)
	r := h.Koko("space", "member", "add", "ops", "bob")
	r.WantSuccess(t)
	if r.Stdout != "Members of Space ops: bob.\n" {
		t.Errorf("stdout = %q", r.Stdout)
	}
}

// A User already a Member stays one, so the command states who the Members
// are after it, whatever they were before.
func TestSpaceMemberAddLeavesAMemberAsTheyAre(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	h.Koko("space", "add", "ops").WantSuccess(t)
	h.Koko("user", "invite", "bob").WantSuccess(t)
	h.Koko("user", "invite", "carol").WantSuccess(t)
	spaceMembers(t, h, "add", "ops", "bob")
	if got := spaceMembers(t, h, "add", "ops", "bob", "carol", "bob"); !slices.Equal(got, []string{"bob", "carol"}) {
		t.Errorf("adding bob again with carol left the Members %q, want bob and carol", got)
	}
}

// Several Users are added all or nothing: one name that no User holds adds
// none of them, and the refusal names it.
func TestSpaceMemberAddIsAllOrNothing(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	h.Koko("space", "add", "ops").WantSuccess(t)
	h.Koko("user", "invite", "bob").WantSuccess(t)
	r := h.Koko("space", "member", "add", "ops", "bob", "nobody", "--json")
	r.WantError(t, 1, "unknown_user")
	harness.Golden(t, "space-member-add-unknown-user.json", r.Stderr)
	h.Koko("space", "member", "add", "ops", "bob", "Bob", "--json").WantError(t, 1, "invalid_name")
	if got := spaceShow(t, h.Koko, "ops").memberNames(); len(got) != 0 {
		t.Errorf("after the refusals ops has the Members %q, want none", got)
	}
}

// A Personal Space is never given Members while its User exists; the Owner's
// own is no exception.
func TestSpaceMemberAddRefusesAPersonalSpace(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	h.Koko("user", "invite", "bob").WantSuccess(t)
	r := h.Koko("space", "member", "add", harness.Owner, "bob", "--json")
	r.WantError(t, 1, "personal_space")
	harness.Golden(t, "space-member-add-personal.json", r.Stderr)
	h.Koko("space", "member", "add", "bob", harness.Owner, "--json").WantError(t, 1, "personal_space")
	h.Koko("space", "member", "add", "ops", "bob", "--json").WantError(t, 1, "unknown_space")
	if got := spaceShow(t, h.Koko, "bob").memberNames(); len(got) != 0 {
		t.Errorf("bob's Personal Space has the Members %q, want none", got)
	}
}

// space member rm takes several Users out of a Space at once; a User who is
// not a Member stays out, and an unknown name takes nobody out.
func TestSpaceMemberRmTakesSeveralUsersOut(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	h.Koko("space", "add", "ops").WantSuccess(t)
	for _, u := range []string{"bob", "carol", "dave"} {
		h.Koko("user", "invite", u).WantSuccess(t)
	}
	spaceMembers(t, h, "add", "ops", "bob", "carol", "dave")

	h.Koko("space", "member", "rm", "ops", "bob", "nobody", "--json").WantError(t, 1, "unknown_user")
	if got := spaceShow(t, h.Koko, "ops").memberNames(); !slices.Equal(got, []string{"bob", "carol", "dave"}) {
		t.Errorf("after the refusal ops has the Members %q, want bob, carol and dave", got)
	}
	r := h.Koko("space", "member", "rm", "ops", "dave", "bob", "--json")
	r.WantSuccess(t)
	harness.Golden(t, "space-member-rm.json", r.Stdout)
	if got := spaceMembers(t, h, "rm", "ops", "bob"); !slices.Equal(got, []string{"carol"}) {
		t.Errorf("taking bob out again left the Members %q, want carol", got)
	}
}

// spaceNames runs space ls --json on the device run runs commands on and
// returns the names it lists.
func spaceNames(t *testing.T, run func(args ...string) harness.Result) []string {
	t.Helper()
	r := run("space", "ls", "--json")
	r.WantSuccess(t)
	var listed []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal([]byte(r.Stdout), &listed); err != nil {
		t.Fatalf("space ls --json printed %q: %v", r.Stdout, err)
	}
	names := []string{}
	for _, s := range listed {
		names = append(names, s.Name)
	}
	return names
}

// A Member lists their own Personal Space and the Spaces they are a Member
// of, and nothing else; the Owner, through a Device Session as over the
// Unix socket, lists every Space.
func TestSpaceLsListsTheSpacesTheCallerCanActIn(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	for _, u := range []string{"bob", "carol"} {
		h.Koko("user", "invite", u).WantSuccess(t)
	}
	for _, s := range []string{"ops", "data"} {
		h.Koko("space", "add", s).WantSuccess(t)
	}
	spaceMembers(t, h, "add", "ops", "bob")
	bob, carol, alice := h.Join("bob"), h.Join("carol"), h.Join(harness.Owner)

	r := bob.Koko("space", "ls", "--json")
	r.WantSuccess(t)
	harness.Golden(t, "space-ls-member.json", r.Stdout)
	every := []string{harness.Owner, "bob", "carol", "data", "ops"}
	for _, tc := range []struct {
		who  string
		run  func(args ...string) harness.Result
		want []string
	}{
		{"bob", bob.Koko, []string{"bob", "ops"}},
		{"carol", carol.Koko, []string{"carol"}},
		{"the Owner's device", alice.Koko, every},
		{"the Local caller", h.Koko, every},
	} {
		if got := spaceNames(t, tc.run); !slices.Equal(got, tc.want) {
			t.Errorf("space ls for %s = %q, want %q", tc.who, got, tc.want)
		}
	}
}

// A Member shows the Spaces they can act in, their own Personal Space
// included.
func TestSpaceShowForAMember(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	h.Koko("user", "invite", "bob").WantSuccess(t)
	h.Koko("space", "add", "ops").WantSuccess(t)
	spaceMembers(t, h, "add", "ops", "bob")
	bob := h.Join("bob")
	if got := spaceShow(t, bob.Koko, "ops").memberNames(); !slices.Equal(got, []string{"bob"}) {
		t.Errorf("bob's space show ops lists the Members %q, want bob", got)
	}
	if s := spaceShow(t, bob.Koko, "bob"); !s.Personal {
		t.Errorf("bob's space show bob = %+v, want his Personal Space", s)
	}
}

// A Space a Member cannot act in answers them exactly as a Space that does
// not exist, before and after it is recorded.
func TestASpaceAMemberCannotSeeAnswersAsUnknown(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	h.Koko("user", "invite", "carol").WantSuccess(t)
	carol := h.Join("carol")
	before := carol.Koko("space", "show", "ops", "--json")
	before.WantError(t, 1, "unknown_space")

	h.Koko("space", "add", "ops").WantSuccess(t)
	after := carol.Koko("space", "show", "ops", "--json")
	if after != before {
		t.Errorf("space show of a Space carol cannot see = %+v, want it as before ops existed: %+v", after, before)
	}
	if got := spaceNames(t, carol.Koko); !slices.Equal(got, []string{"carol"}) {
		t.Errorf("carol's space ls = %q, want only her Personal Space", got)
	}
}

// A membership is read afresh on every call: a Member taken out of a Space
// loses it on their next command, and gets it back once added again, on the
// same Device Session.
func TestAMembershipChangeHoldsFromTheNextCall(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	h.Koko("user", "invite", "bob").WantSuccess(t)
	h.Koko("space", "add", "ops").WantSuccess(t)
	bob := h.Join("bob")
	bob.Koko("space", "show", "ops", "--json").WantError(t, 1, "unknown_space")

	spaceMembers(t, h, "add", "ops", "bob")
	bob.Koko("space", "show", "ops", "--json").WantSuccess(t)
	spaceMembers(t, h, "rm", "ops", "bob")
	bob.Koko("space", "show", "ops", "--json").WantError(t, 1, "unknown_space")
	if got := spaceNames(t, bob.Koko); !slices.Equal(got, []string{"bob"}) {
		t.Errorf("bob's space ls after he was taken out of ops = %q, want only his Personal Space", got)
	}
}

// Only the Owner changes Spaces and Members. A Member is refused with
// owner_only before koko reads anything the command names, so the refusal
// is the same for a name that is taken, free or unknown, and changes
// nothing.
func TestOnlyTheOwnerChangesSpaces(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	h.Koko("user", "invite", "bob").WantSuccess(t)
	h.Koko("space", "add", "ops").WantSuccess(t)
	spaceMembers(t, h, "add", "ops", "bob")
	bob := h.Join("bob")
	before := spaceShow(t, h.Koko, "ops")

	r := bob.Koko("space", "add", "data", "--json")
	r.WantError(t, 1, "owner_only")
	harness.Golden(t, "space-add-owner-only.json", r.Stderr)
	// Each command names records that exist, then names that no record
	// holds: the two refusals are the same.
	for _, tc := range []struct{ known, unknown []string }{
		{[]string{"space", "add", "ops"}, []string{"space", "add", "data"}},
		{[]string{"space", "rename", "ops", "infra"}, []string{"space", "rename", "nowhere", "infra"}},
		{[]string{"space", "rm", "ops"}, []string{"space", "rm", "nowhere"}},
		{[]string{"space", "member", "add", "ops", "bob"}, []string{"space", "member", "add", "nowhere", "nobody"}},
		{[]string{"space", "member", "rm", "ops", "bob"}, []string{"space", "member", "rm", "nowhere", "nobody"}},
	} {
		known := bob.Koko(append(tc.known, "--json")...)
		known.WantError(t, 1, "owner_only")
		if unknown := bob.Koko(append(tc.unknown, "--json")...); unknown != known {
			t.Errorf("koko %s = %+v, want the refusal koko %s got: %+v", strings.Join(tc.unknown, " "), unknown, strings.Join(tc.known, " "), known)
		}
	}
	if got := spaceNames(t, h.Koko); !slices.Equal(got, []string{harness.Owner, "bob", "ops"}) {
		t.Errorf("after the refusals space ls = %q, want no new Space", got)
	}
	if after := spaceShow(t, h.Koko, "ops"); !slices.Equal(after.memberNames(), before.memberNames()) {
		t.Errorf("after the refusals ops has the Members %q, want %q", after.memberNames(), before.memberNames())
	}
}

// space rename gives a shared Space a new name and prints it; the Space
// keeps its id and its Members, and the old name is free.
func TestSpaceRenameRenamesASharedSpace(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	h.Koko("user", "invite", "bob").WantSuccess(t)
	h.Koko("space", "add", "ops").WantSuccess(t)
	spaceMembers(t, h, "add", "ops", "bob")
	before := spaceShow(t, h.Koko, "ops")

	r := h.Koko("space", "rename", "ops", "infra", "--json")
	r.WantSuccess(t)
	harness.Golden(t, "space-rename.json", r.Stdout)
	if after := spaceShow(t, h.Koko, "infra"); after.ID != before.ID || !slices.Equal(after.memberNames(), []string{"bob"}) {
		t.Errorf("space show infra = %+v, want %s with the Member bob", after, before.ID)
	}
	h.Koko("space", "show", "ops", "--json").WantError(t, 1, "unknown_space")
	h.Koko("space", "add", "ops").WantSuccess(t)
}

func TestSpaceRenameForPeople(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	h.Koko("space", "add", "ops").WantSuccess(t)
	r := h.Koko("space", "rename", "ops", "infra")
	r.WantSuccess(t)
	if !strings.HasPrefix(r.Stdout, "Renamed Space ops to infra (space_") {
		t.Errorf("stdout = %q", r.Stdout)
	}
}

// A Space's new name is checked against every Space, Personal Spaces
// included; a Personal Space keeps its User's name while its User exists.
func TestSpaceRenameRefusals(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	h.Koko("space", "add", "ops").WantSuccess(t)
	h.Koko("space", "add", "data").WantSuccess(t)
	r := h.Koko("space", "rename", harness.Owner, "home", "--json")
	r.WantError(t, 1, "personal_space")
	harness.Golden(t, "space-rename-personal.json", r.Stderr)
	for _, tc := range []struct {
		args []string
		code string
	}{
		{[]string{"ops", "data"}, "name_taken"},
		{[]string{"ops", harness.Owner}, "name_taken"},
		{[]string{"ops", "ops"}, "name_taken"},
		{[]string{"ops", "Infra"}, "invalid_name"},
		{[]string{"nowhere", "infra"}, "unknown_space"},
	} {
		h.Koko(append(append([]string{"space", "rename"}, tc.args...), "--json")...).WantError(t, 1, tc.code)
	}
	if got := spaceNames(t, h.Koko); !slices.Equal(got, []string{harness.Owner, "data", "ops"}) {
		t.Errorf("after the refusals space ls = %q, want every Space as it was", got)
	}
}

// space rm removes an empty Space and prints it as it was; its memberships
// go with it, so a new Space under the same name has no Members.
func TestSpaceRmRemovesAnEmptySpace(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	h.Koko("user", "invite", "bob").WantSuccess(t)
	h.Koko("space", "add", "ops").WantSuccess(t)
	spaceMembers(t, h, "add", "ops", "bob")
	before := spaceShow(t, h.Koko, "ops")

	r := h.Koko("space", "rm", "ops", "--json")
	r.WantSuccess(t)
	harness.Golden(t, "space-rm.json", r.Stdout)
	var removed struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(r.Stdout), &removed); err != nil || removed.ID != before.ID {
		t.Errorf("space rm printed %q (%v), want the Space %s", r.Stdout, err, before.ID)
	}
	if got := spaceNames(t, h.Koko); !slices.Equal(got, []string{harness.Owner, "bob"}) {
		t.Errorf("after space rm, space ls = %q, want no ops", got)
	}
	h.Koko("space", "add", "ops").WantSuccess(t)
	if got := spaceShow(t, h.Koko, "ops").memberNames(); len(got) != 0 {
		t.Errorf("a new ops has the Members %q, want none", got)
	}
}

func TestSpaceRmForPeople(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	h.Koko("space", "add", "ops").WantSuccess(t)
	r := h.Koko("space", "rm", "ops")
	r.WantSuccess(t)
	if !strings.HasPrefix(r.Stdout, "Removed Space ops (space_") {
		t.Errorf("stdout = %q", r.Stdout)
	}
}

// A Personal Space is never removed while its User exists.
func TestSpaceRmRefusals(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	h.Koko("user", "invite", "bob").WantSuccess(t)
	r := h.Koko("space", "rm", "bob", "--json")
	r.WantError(t, 1, "personal_space")
	harness.Golden(t, "space-rm-personal.json", r.Stderr)
	h.Koko("space", "rm", harness.Owner, "--json").WantError(t, 1, "personal_space")
	h.Koko("space", "rm", "nowhere", "--json").WantError(t, 1, "unknown_space")
	if got := spaceNames(t, h.Koko); !slices.Equal(got, []string{harness.Owner, "bob"}) {
		t.Errorf("after the refusals space ls = %q, want both Personal Spaces", got)
	}
}

// Without --json the error is a line for people, still on stderr only.
func TestErrorForPeopleGoesToStderr(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	h.Koko("space", "add", "ops").WantSuccess(t)
	r := h.Koko("space", "add", "ops")
	if r.Status != 1 || r.Stdout != "" || r.Stderr != "koko: a Space named \"ops\" already exists\n" {
		t.Errorf("status %d, stdout %q, stderr %q", r.Status, r.Stdout, r.Stderr)
	}
}
