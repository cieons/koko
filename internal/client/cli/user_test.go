package cli_test

import (
	"encoding/json/v2"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/shared/joincode"
)

// userOut is a User as the CLI's --json prints it.
type userOut struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	Role      string  `json:"role"`
	CreatedAt string  `json:"created_at"`
	RemovedAt *string `json:"removed_at"`
}

// invitation is what user invite --json prints.
type invitation struct {
	User     userOut     `json:"user"`
	JoinCode joinCodeOut `json:"join_code"`
}

// userInvite runs user invite --json for the User named user, and returns
// what it printed.
func userInvite(t *testing.T, h *harness.Harness, user string) invitation {
	t.Helper()
	r := h.Koko("user", "invite", user, "--json")
	r.WantSuccess(t)
	var inv invitation
	if err := json.Unmarshal([]byte(r.Stdout), &inv); err != nil {
		t.Fatalf("user invite --json printed %q: %v", r.Stdout, err)
	}
	return inv
}

// user invite creates the User as a Member, with their Personal Space, and
// prints their Invite: their first Join Code, for device-1, valid for 15
// minutes.
func TestUserInviteCreatesAMemberAndTheirFirstJoinCode(t *testing.T) {
	h := harness.Start(t)
	r := h.Koko("user", "invite", "bob", "--json")
	r.WantSuccess(t)
	harness.Golden(t, "user-invite.json", r.Stdout)

	var inv invitation
	if err := json.Unmarshal([]byte(r.Stdout), &inv); err != nil {
		t.Fatal(err)
	}
	now := h.Now().Format(time.RFC3339)
	if u := inv.User; u.Name != "bob" || u.Role != "member" || u.CreatedAt != now || !strings.HasPrefix(u.ID, "user_") {
		t.Errorf("user invite made the User %+v, want the Member bob created at %s", u, now)
	}
	jc := inv.JoinCode
	if want := h.Now().Add(15 * time.Minute).Format(time.RFC3339); jc.User != "bob" || jc.Device != "device-1" || jc.ExpiresAt != want {
		t.Errorf("user invite issued %+v, want a Join Code for bob's device-1 that expires at %s", jc, want)
	}
	if _, err := joincode.Decode(jc.Code); err != nil {
		t.Errorf("decode the Invite: %v", err)
	}

	// bob exists from the Invite on: his Personal Space is listed, and the
	// Owner can issue him another Join Code.
	if r := h.Koko("space", "ls", "--json"); !strings.Contains(r.Stdout, `"name": "bob"`) {
		t.Errorf("space ls = %s, want bob's Personal Space", r.Stdout)
	}
	if jc := deviceAdd(t, h, "--user", "bob"); jc.User != "bob" || jc.Device != "device-2" {
		t.Errorf("device add --user bob = %+v, want a Join Code for bob's device-2", jc)
	}
}

// For people, user invite says whom it invited, then prints the one koko
// join line to send them.
func TestUserInviteForPeople(t *testing.T) {
	h := harness.Start(t)
	r := h.Koko("user", "invite", "bob")
	r.WantSuccess(t)
	lines := strings.Split(strings.TrimSuffix(r.Stdout, "\n"), "\n")
	if len(lines) != 3 || !strings.HasPrefix(lines[0], "Invited bob (user_") || !strings.HasPrefix(lines[1], "On bob's new device device-1, before ") {
		t.Fatalf("stdout = %q, want who was invited, for whom and until when, then the koko join line", r.Stdout)
	}
	code, ok := strings.CutPrefix(lines[2], "koko join ")
	if _, err := joincode.Decode(code); !ok || err != nil {
		t.Errorf("the last line is %q, want koko join and a Join Code (%v)", lines[2], err)
	}
}

// A new User's name is refused when a User or a Space holds it, since they
// share one namespace; the refusal creates nothing.
func TestUserInviteRefusesATakenName(t *testing.T) {
	h := harness.Start(t)
	h.Koko("space", "add", "ops").WantSuccess(t)
	h.Koko("user", "invite", "bob").WantSuccess(t)
	before := h.Koko("space", "ls", "--json").Stdout

	for _, taken := range []string{harness.Owner, "bob", "ops"} {
		r := h.Koko("user", "invite", taken, "--json")
		r.WantError(t, 1, "name_taken")
		if taken == "ops" {
			harness.Golden(t, "user-invite-taken-by-space.json", r.Stderr)
		}
	}
	if after := h.Koko("space", "ls", "--json").Stdout; after != before {
		t.Errorf("space ls after the refusals = %s, want it unchanged: %s", after, before)
	}
}

// Only the Owner invites a User. A Member is refused with owner_only before
// koko reads the name, so the refusal does not say whether a User or a
// Space holds it, and creates nothing.
func TestOnlyTheOwnerInvites(t *testing.T) {
	h := harness.Start(t)
	h.Koko("user", "invite", "bob").WantSuccess(t)
	h.Koko("space", "add", "ops").WantSuccess(t)
	bob := h.Join("bob")
	r := bob.Koko("user", "invite", "carol", "--json")
	r.WantError(t, 1, "owner_only")
	harness.Golden(t, "user-invite-owner-only.json", r.Stderr)
	for _, taken := range []string{"ops", "bob", harness.Owner} {
		if got := bob.Koko("user", "invite", taken, "--json"); got != r {
			t.Errorf("a Member's user invite %s = %+v, want the refusal for a free name: %+v", taken, got, r)
		}
	}
	if r := h.Koko("user", "invite", "carol", "--json"); r.Status != 0 {
		t.Errorf("the Owner's user invite carol after the refusals = %+v, want carol free", r)
	}
}

// A User's name is a lowercase DNS label.
func TestUserInviteRefusesAnInvalidName(t *testing.T) {
	h := harness.Start(t)
	for _, name := range []string{"Bob", "bob_1", "bob-", strings.Repeat("b", 64)} {
		h.Koko("user", "invite", name, "--json").WantError(t, 1, "invalid_name")
	}
	if r := h.Koko("space", "ls", "--json"); strings.Count(r.Stdout, `"name"`) != 1 {
		t.Errorf("space ls = %s, want only the Owner's Personal Space", r.Stdout)
	}
}

// userLs runs user ls --json with args through run, and returns the Users
// it listed, and what it printed, for a golden file.
func userLs(t *testing.T, run func(...string) harness.Result, args ...string) ([]userOut, string) {
	t.Helper()
	r := run(append([]string{"user", "ls", "--json"}, args...)...)
	r.WantSuccess(t)
	var l []userOut
	if err := json.Unmarshal([]byte(r.Stdout), &l); err != nil {
		t.Fatalf("user ls --json printed %q: %v", r.Stdout, err)
	}
	return l, r.Stdout
}

// names returns the names of the Users in l, in order.
func names(l []userOut) []string {
	var n []string
	for _, u := range l {
		n = append(n, u.Name)
	}
	return n
}

// koko user ls lists the Users by name, each with their role and when they
// were created; --all adds the Removed ones, with when they were Removed. A
// User deleted is in neither.
func TestUserLsListsTheUsers(t *testing.T) {
	h := harness.Start(t)
	for _, u := range []string{"dave", "bob", "carol"} {
		userInvite(t, h, u)
	}
	h.Join("dave")
	h.Advance(time.Minute)
	userRemove(t, h.Koko, "dave")
	userRemove(t, h.Koko, "carol")

	l, out := userLs(t, h.Koko)
	harness.Golden(t, "user-ls.json", out)
	if got, want := names(l), []string{harness.Owner, "bob"}; !slices.Equal(got, want) {
		t.Errorf("user ls listed %v, want %v", got, want)
	}

	all, out := userLs(t, h.Koko, "--all")
	harness.Golden(t, "user-ls-all.json", out)
	if got, want := names(all), []string{harness.Owner, "bob", "dave"}; !slices.Equal(got, want) {
		t.Fatalf("user ls --all listed %v, want %v", got, want)
	}
	if now := h.Now().Format(time.RFC3339); all[2].RemovedAt == nil || *all[2].RemovedAt != now || all[1].RemovedAt != nil {
		t.Errorf("user ls --all = %+v, want dave Removed at %s and bob not Removed", all, now)
	}
}

// For people, user ls is a table of the Users, with a REMOVED column when
// it lists a Removed one.
func TestUserLsForPeople(t *testing.T) {
	h := harness.Start(t)
	userInvite(t, h, "bob")
	h.Join("bob")
	r := h.Koko("user", "ls")
	r.WantSuccess(t)
	lines := strings.Split(strings.TrimSuffix(r.Stdout, "\n"), "\n")
	if len(lines) != 3 || strings.Fields(lines[0])[0] != "NAME" || strings.Contains(lines[0], "REMOVED") ||
		!strings.HasPrefix(lines[1], harness.Owner+" ") || !strings.HasPrefix(lines[2], "bob ") {
		t.Errorf("user ls printed %q, want a table of alice and bob without a REMOVED column", r.Stdout)
	}

	userRemove(t, h.Koko, "bob")
	r = h.Koko("user", "ls", "--all")
	r.WantSuccess(t)
	lines = strings.Split(strings.TrimSuffix(r.Stdout, "\n"), "\n")
	if len(lines) != 3 || !strings.HasSuffix(lines[0], "REMOVED") ||
		!strings.HasSuffix(lines[1], " -") || !strings.HasSuffix(lines[2], h.Now().Format(time.RFC3339)) {
		t.Errorf("user ls --all printed %q, want a REMOVED column saying when bob was Removed", r.Stdout)
	}
}

// Only the Owner lists and removes Users. A Member is refused with
// owner_only before koko reads the name, so the refusal is the same for
// any name and changes nothing.
func TestOnlyTheOwnerListsAndRemovesUsers(t *testing.T) {
	h := harness.Start(t)
	userInvite(t, h, "bob")
	userInvite(t, h, "carol")
	bob := h.Join("bob")

	r := bob.Koko("user", "ls", "--json")
	r.WantError(t, 1, "owner_only")
	harness.Golden(t, "user-ls-owner-only.json", r.Stderr)
	r = bob.Koko("user", "remove", "carol", "--json")
	r.WantError(t, 1, "owner_only")
	harness.Golden(t, "user-remove-owner-only.json", r.Stderr)
	for _, other := range []string{"bob", harness.Owner, "nobody"} {
		if got := bob.Koko("user", "remove", other, "--json"); got != r {
			t.Errorf("a Member's user remove %s = %+v, want the same refusal as for carol: %+v", other, got, r)
		}
	}
	l, _ := userLs(t, h.Koko)
	if got, want := names(l), []string{harness.Owner, "bob", "carol"}; !slices.Equal(got, want) {
		t.Errorf("user ls after the refusals listed %v, want %v", got, want)
	}
}

// A device name belongs to its User: bob's device-1 and alice's device-1
// are both held by valid Join Codes at once.
func TestDeviceNamesArePerUser(t *testing.T) {
	h := harness.Start(t)
	h.Koko("user", "invite", "bob").WantSuccess(t)
	if jc := deviceAdd(t, h, "--user", "bob", "laptop"); jc.Device != "laptop" {
		t.Errorf("device add --user bob laptop named the device %q", jc.Device)
	}
	if jc := deviceAdd(t, h, "laptop"); jc.User != harness.Owner || jc.Device != "laptop" {
		t.Errorf("device add laptop = %+v, want the Owner's laptop", jc)
	}
}

// An Invite that expires leaves its User in place: the Invite no longer
// joins, and the User joins with a new Join Code from device add --user.
func TestAnExpiredInviteLeavesItsUser(t *testing.T) {
	h := harness.Start(t)
	r := h.Koko("user", "invite", "bob", "--json")
	r.WantSuccess(t)
	var inv invitation
	if err := json.Unmarshal([]byte(r.Stdout), &inv); err != nil {
		t.Fatal(err)
	}
	h.Advance(15 * time.Minute)
	harness.NewDevice(t).Koko("join", inv.JoinCode.Code, "--json").WantError(t, 1, "invalid_join_code")

	d := harness.NewDevice(t)
	d.Koko("join", deviceAdd(t, h, "--user", "bob").Code).WantSuccess(t)
	if c := readClientConfig(t, d); c.User != "bob" || c.Device != "device-1" {
		t.Errorf("bob's new code joined %s's %s, want bob's device-1", c.User, c.Device)
	}
}
