package cli_test

import (
	"encoding/json/v2"
	"maps"
	"strings"
	"testing"
	"time"

	"github.com/cieons/koko/internal/harness"
)

// userRemoval is what user remove --json prints.
type userRemoval struct {
	Outcome string  `json:"outcome"`
	User    userOut `json:"user"`
}

// userRemove runs user remove --json with args through run, which is h.Koko
// or a device's Koko, and returns what it printed.
func userRemove(t *testing.T, run func(...string) harness.Result, args ...string) userRemoval {
	t.Helper()
	r := run(append([]string{"user", "remove", "--json"}, args...)...)
	r.WantSuccess(t)
	var got userRemoval
	if err := json.Unmarshal([]byte(r.Stdout), &got); err != nil {
		t.Fatalf("user remove --json printed %q: %v", r.Stdout, err)
	}
	return got
}

// koko user remove deletes a User who never joined, and says so: their
// memberships, their Join Codes and their empty Personal Space go with them,
// and the name is free again.
func TestUserRemoveDeletesAUserWhoNeverJoined(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	inv := userInvite(t, h, "bob")
	h.Koko("space", "add", "ops").WantSuccess(t)
	h.Koko("space", "member", "add", "ops", "bob").WantSuccess(t)
	pending := deviceAdd(t, h, "laptop", "--user", "bob")

	r := h.Koko("user", "remove", "bob", "--json")
	r.WantSuccess(t)
	harness.Golden(t, "user-remove-deleted.json", r.Stdout)
	var got userRemoval
	if err := json.Unmarshal([]byte(r.Stdout), &got); err != nil {
		t.Fatal(err)
	}
	if got.Outcome != "deleted" || got.User.ID != inv.User.ID || got.User.Name != "bob" || got.User.RemovedAt != nil {
		t.Errorf("user remove bob printed %+v, want bob (%s) deleted", got, inv.User.ID)
	}

	if r := h.Koko("space", "ls", "--json"); strings.Contains(r.Stdout, `"name": "bob"`) {
		t.Errorf("space ls after deleting bob = %s, want no Personal Space of his", r.Stdout)
	}
	for _, code := range []string{inv.JoinCode.Code, pending.Code} {
		harness.NewDevice(t).Koko("join", code, "--json").WantError(t, 1, "invalid_join_code")
	}
	// The name is free again, and the new bob is no Member of ops.
	if again := userInvite(t, h, "bob"); again.User.ID == inv.User.ID {
		t.Errorf("the new bob has the deleted bob's id %s", inv.User.ID)
	}
	if r := h.Koko("space", "show", "ops", "--json"); !strings.Contains(r.Stdout, `"members": []`) {
		t.Errorf("space show ops = %s, want no Members: bob's membership went with him", r.Stdout)
	}
}

// koko user remove Removes a User who joined, and says so. In one
// transaction it revokes their Device Sessions, so their devices are told
// so by name and their open Device Connections close; voids their valid
// Join Codes and those their devices issued; takes them out of every Space;
// and keeps them on record, Removed, with their Device Sessions.
func TestUserRemoveRemovesAUserWhoJoined(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	inv := userInvite(t, h, "bob")
	laptop, laptopID := joinAs(t, inv.JoinCode)
	conn := laptop.Dial()
	desktop, desktopID := joinAs(t, deviceAddOn(t, laptop, "desktop"))
	fromLaptop := deviceAddOn(t, laptop, "phone")
	fromSocket := deviceAdd(t, h, "tablet", "--user", "bob")
	h.Koko("space", "add", "ops").WantSuccess(t)
	h.Koko("space", "member", "add", "ops", "bob").WantSuccess(t)
	h.Advance(time.Minute)

	r := h.Koko("user", "remove", "bob", "--json")
	r.WantSuccess(t)
	harness.Golden(t, "user-remove-removed.json", r.Stdout)
	var got userRemoval
	if err := json.Unmarshal([]byte(r.Stdout), &got); err != nil {
		t.Fatal(err)
	}
	now := h.Now().Format(time.RFC3339)
	if got.Outcome != "removed" || got.User.ID != inv.User.ID || got.User.RemovedAt == nil || *got.User.RemovedAt != now {
		t.Errorf("user remove bob printed %+v, want bob (%s) Removed at %s", got, inv.User.ID, now)
	}

	waitClosed(t, conn, "the laptop's Device Connection")
	for device, d := range map[string]*harness.Device{"device-1": laptop, "desktop": desktop} {
		msg := d.Koko("space", "ls", "--json").WantError(t, 1, "revoked")
		if want := `the Device Session "` + device + `" of this device was revoked`; !strings.HasPrefix(msg, want) {
			t.Errorf("bob's %s says %q, want %q", device, msg, want)
		}
	}
	for _, jc := range []joinCodeOut{fromLaptop, fromSocket} {
		harness.NewDevice(t).Koko("join", jc.Code, "--json").WantError(t, 1, "invalid_join_code")
	}
	revokedAt := map[string]string{}
	for _, d := range deviceLs(t, h.Koko, "--user", "bob", "--all") {
		revokedAt[d.ID] = "live"
		if d.RevokedAt != nil {
			revokedAt[d.ID] = *d.RevokedAt
		}
	}
	if want := map[string]string{laptopID: now, desktopID: now}; !maps.Equal(revokedAt, want) {
		t.Errorf("device ls --user bob --all lists bob's Device Sessions Revoked at %v, want the laptop and the desktop at %s: %v", revokedAt, now, want)
	}
	if r := h.Koko("space", "show", "ops", "--json"); !strings.Contains(r.Stdout, `"members": []`) {
		t.Errorf("space show ops = %s, want no Members: bob left every Space", r.Stdout)
	}
	log := h.Log()
	for _, want := range []string{
		`msg="device revoked" user=bob device=desktop device_session=` + desktopID + ` revoked_by="unix socket"`,
		`msg="device revoked" user=bob device=device-1 device_session=` + laptopID + ` revoked_by="unix socket"`,
		`msg="user removed" user=bob user_id=` + inv.User.ID + ` device_sessions_revoked=2 join_codes_voided=2 signins_ended=0 password_links_voided=0 removed_by="unix socket"`,
	} {
		if !strings.Contains(log, want) {
			t.Errorf("the log holds no line with %s:\n%s", want, log)
		}
	}
}

// A Removed User's Personal Space stays, with what they recorded in it, as
// an ordinary shared Space with no Members that only the Owner sees. The
// Owner renames it, gives it Members, moves its Hosts out and removes it
// once it is empty; the Removed User's name stays theirs throughout.
func TestARemovedUsersPersonalSpacePassesToTheOwner(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	userInvite(t, h, "bob")
	userInvite(t, h, "carol")
	bob, carol := h.Join("bob"), h.Join("carol")
	bob.Koko("host", "add", "web1", "address=203.0.113.10", "account=deploy").WantSuccess(t)
	h.Koko("space", "add", "ops").WantSuccess(t)
	userRemove(t, h.Koko, "bob")

	if r := h.Koko("space", "ls", "--json"); !strings.Contains(r.Stdout, `"name": "bob",`+"\n"+`    "personal": false`) {
		t.Errorf("the Owner's space ls = %s, want bob's former Personal Space listed as a shared Space", r.Stdout)
	}
	r := h.Koko("space", "show", "bob", "--json")
	r.WantSuccess(t)
	harness.Golden(t, "space-show-former-personal.json", r.Stdout)
	if !strings.Contains(r.Stdout, `"personal": false`) || !strings.Contains(r.Stdout, `"members": []`) || !strings.Contains(r.Stdout, `"hosts": 1`) {
		t.Errorf("space show bob = %s, want a shared Space with no Members, holding web1", r.Stdout)
	}
	if r := carol.Koko("space", "ls", "--json"); strings.Contains(r.Stdout, `"name": "bob"`) {
		t.Errorf("carol's space ls = %s, want bob's former Personal Space left out", r.Stdout)
	}
	carol.Koko("host", "show", "web1", "--json").WantError(t, 1, "unknown_host")

	h.Koko("space", "rename", "bob", "handover").WantSuccess(t)
	h.Koko("space", "member", "add", "handover", "carol").WantSuccess(t)
	carol.Koko("host", "show", "web1", "--json").WantSuccess(t)
	h.Koko("space", "rm", "handover", "--json").WantError(t, 1, "space_not_empty")
	h.Koko("host", "move", "web1", "ops").WantSuccess(t)
	h.Koko("space", "rm", "handover").WantSuccess(t)

	r = h.Koko("user", "invite", "bob", "--json")
	msg := r.WantError(t, 1, "name_taken")
	harness.Golden(t, "user-invite-removed-name.json", r.Stderr)
	if !strings.Contains(msg, "Removed") {
		t.Errorf("user invite bob says %q, want it to say a Removed User holds the name", msg)
	}
}

// A Removed User is acted on no more: the Owner issues them no Join Code,
// makes them a Member of no Space, all or nothing, and does not remove them
// again. Each refusal says they were Removed and changes nothing. Taking
// them out of a Space, which they are in no more, is harmless.
func TestARemovedUserGetsNoJoinCodeAndJoinsNoSpace(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	userInvite(t, h, "bob")
	userInvite(t, h, "carol")
	h.Join("bob")
	h.Koko("space", "add", "ops").WantSuccess(t)
	userRemove(t, h.Koko, "bob")

	r := h.Koko("device", "add", "--user", "bob", "--json")
	r.WantError(t, 1, "user_removed")
	harness.Golden(t, "device-add-removed-user.json", r.Stderr)
	h.Koko("space", "member", "add", "ops", "carol", "bob", "--json").WantError(t, 1, "user_removed")
	if r := h.Koko("space", "show", "ops", "--json"); !strings.Contains(r.Stdout, `"members": []`) {
		t.Errorf("space show ops after the refusal = %s, want no Members: carol is not added without bob", r.Stdout)
	}
	h.Koko("space", "member", "add", "ops", "carol").WantSuccess(t)
	h.Koko("space", "member", "rm", "ops", "carol", "bob").WantSuccess(t)
	if r := h.Koko("space", "show", "ops", "--json"); !strings.Contains(r.Stdout, `"members": []`) {
		t.Errorf("space show ops after space member rm ops carol bob = %s, want carol taken out", r.Stdout)
	}
	r = h.Koko("user", "remove", "bob", "--json")
	r.WantError(t, 1, "user_removed")
	harness.Golden(t, "user-remove-removed-already.json", r.Stderr)
}

// The Owner is never deleted or Removed: not before their first device
// joins, when they have never joined, and not after, whoever asks. The
// refusal changes nothing.
func TestTheOwnerIsNeverRemoved(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	// Initialisation's Join Code is still unused: the Owner never joined.
	r := h.Koko("user", "remove", harness.Owner, "--json")
	r.WantError(t, 1, "owner_not_removable")
	harness.Golden(t, "user-remove-owner.json", r.Stderr)

	alice := h.Join(harness.Owner)
	h.Koko("user", "remove", harness.Owner, "--json").WantError(t, 1, "owner_not_removable")
	alice.Koko("user", "remove", harness.Owner, "--json").WantError(t, 1, "owner_not_removable")
	if r := alice.Koko("space", "show", harness.Owner, "--json"); !strings.Contains(r.Stdout, `"personal": true`) {
		t.Errorf("space show %s after the refusals = %s %s, want the Owner's Personal Space as it was", harness.Owner, r.Stdout, r.Stderr)
	}
}

// For people, user remove says whether it deleted or Removed the User, and
// what that did.
func TestUserRemoveForPeople(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	bob := userInvite(t, h, "bob")
	carol := userInvite(t, h, "carol")
	h.Join("carol")

	r := h.Koko("user", "remove", "bob")
	r.WantSuccess(t)
	if want := "Deleted bob (" + bob.User.ID + "), who never joined; the name bob is free again.\n"; r.Stdout != want {
		t.Errorf("user remove bob printed %q, want %q", r.Stdout, want)
	}
	r = h.Koko("user", "remove", "carol")
	r.WantSuccess(t)
	if want := "Removed carol (" + carol.User.ID + "): their Device Sessions are revoked and they are in no Space. " +
		"Their Personal Space carol is now a Space with no Members that only the Owner sees.\n"; r.Stdout != want {
		t.Errorf("user remove carol printed %q, want %q", r.Stdout, want)
	}
}

// Deleting a User who never joined loses nothing: while their Personal
// Space holds a record, user remove refuses, naming what is left, and
// changes nothing; once it is empty, it deletes them.
func TestUserRemoveRefusesToDeleteAUserWhosePersonalSpaceHoldsARecord(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	userInvite(t, h, "bob")
	h.Koko("host", "add", "web1", "address=203.0.113.10", "account=deploy", "space=bob").WantSuccess(t)

	r := h.Koko("user", "remove", "bob", "--json")
	msg := r.WantError(t, 1, "space_not_empty")
	harness.Golden(t, "user-remove-not-empty.json", r.Stderr)
	if !strings.Contains(msg, "still holds 1 Host") {
		t.Errorf("the refusal says %q, want it to name the Host left", msg)
	}
	if jc := deviceAdd(t, h, "--user", "bob"); jc.User != "bob" {
		t.Errorf("after the refusal, device add --user bob issued for %q, want bob still there", jc.User)
	}

	h.Koko("host", "rm", "web1").WantSuccess(t)
	if got := userRemove(t, h.Koko, "bob"); got.Outcome != "deleted" {
		t.Errorf("user remove bob once his Personal Space was empty = %+v, want bob deleted", got)
	}
}
