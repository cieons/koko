package cli_test

import (
	"encoding/json/v2"
	"strings"
	"testing"
	"time"

	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/shared/joincode"
)

// invitation is what user invite --json prints.
type invitation struct {
	User struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		Role      string `json:"role"`
		CreatedAt string `json:"created_at"`
	} `json:"user"`
	JoinCode joinCodeOut `json:"join_code"`
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
