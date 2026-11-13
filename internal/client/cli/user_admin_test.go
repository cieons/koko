package cli_test

import (
	"encoding/json/v2"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/shared/api"
)

// addAdministrators invites the Users named users and makes each an
// Administrator, over the Unix socket.
func addAdministrators(t *testing.T, h *harness.Harness, users ...string) {
	t.Helper()
	for _, u := range users {
		h.Koko("user", "invite", u).WantSuccess(t)
		h.Koko("user", "promote", u).WantSuccess(t)
	}
}

// userRole runs user promote or user demote, as verb says, with --json
// for the User named user through run, and returns the User it printed.
func userRole(t *testing.T, run func(...string) harness.Result, verb, user string) (userOut, string) {
	t.Helper()
	r := run("user", verb, user, "--json")
	r.WantSuccess(t)
	var u userOut
	if err := json.Unmarshal([]byte(r.Stdout), &u); err != nil {
		t.Fatalf("user %s --json printed %q: %v", verb, r.Stdout, err)
	}
	return u, r.Stdout
}

// rolesOf returns each User's role as user ls --all lists it through run.
func rolesOf(t *testing.T, run func(...string) harness.Result) map[string]string {
	t.Helper()
	r := run("user", "ls", "--all", "--json")
	r.WantSuccess(t)
	var users []userOut
	if err := json.Unmarshal([]byte(r.Stdout), &users); err != nil {
		t.Fatalf("user ls --json printed %q: %v", r.Stdout, err)
	}
	roles := map[string]string{}
	for _, u := range users {
		roles[u.Name] = u.Role
	}
	return roles
}

// wantRoles fails the test unless user ls --all lists each User in want
// with its role.
func wantRoles(t *testing.T, h *harness.Harness, want map[string]string) {
	t.Helper()
	got := rolesOf(t, h.Koko)
	for name, role := range want {
		if got[name] != role {
			t.Errorf("user ls lists %s as %q, want %q (all: %v)", name, got[name], role, got)
		}
	}
}

// wantLinkValid fails the test unless /auth/link answers the Password Link
// link as valid, or as no longer valid when valid is false.
func wantLinkValid(t *testing.T, h *harness.Harness, link string, valid bool) {
	t.Helper()
	status, body := h.Browser().Post(api.AuthLinkPath, api.AuthLinkRequest{Link: harness.LinkSecret(link)})
	switch {
	case valid && status != http.StatusOK:
		t.Errorf("/auth/link answered %d %s, want the link valid", status, body)
	case !valid && !strings.Contains(body, api.CodeLinkInvalid.Name):
		t.Errorf("/auth/link answered %d %s, want %s", status, body, api.CodeLinkInvalid.Name)
	}
}

// passwordLinkOn runs password link --json for the User named user on run,
// and returns the link it printed.
func passwordLinkOn(t *testing.T, run func(...string) harness.Result, user string) string {
	t.Helper()
	r := run("password", "link", user, "--json")
	r.WantSuccess(t)
	var out struct {
		PasswordLink string `json:"password_link"`
	}
	if err := json.Unmarshal([]byte(r.Stdout), &out); err != nil {
		t.Fatalf("password link --json printed %q: %v", r.Stdout, err)
	}
	return out.PasswordLink
}

// signInOut is a Sign-in as signin ls --json prints it.
type signInOut struct {
	ID   string `json:"id"`
	User string `json:"user"`
}

// signinLsOn runs signin ls --json with args on run, and returns the
// Sign-ins it listed.
func signinLsOn(t *testing.T, run func(...string) harness.Result, args ...string) []signInOut {
	t.Helper()
	r := run(append([]string{"signin", "ls", "--json"}, args...)...)
	r.WantSuccess(t)
	var l []signInOut
	if err := json.Unmarshal([]byte(r.Stdout), &l); err != nil {
		t.Fatalf("signin ls --json printed %q: %v", r.Stdout, err)
	}
	return l
}

// koko user promote makes a Member an Administrator, one who has not
// joined included, and koko user demote makes them a Member again; each
// prints the User in user ls's shape, and user ls names the role. A retry
// answers the User as they are and logs nothing more.
func TestUserPromoteAndDemote(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	inv := userInvite(t, h, "bob")

	u, out := userRole(t, h.Koko, "promote", "bob")
	harness.Golden(t, "user-promote.json", out)
	if u.ID != inv.User.ID || u.Role != api.RoleAdministrator {
		t.Errorf("user promote bob printed %+v, want bob (%s) an Administrator", u, inv.User.ID)
	}
	if _, again := userRole(t, h.Koko, "promote", "bob"); again != out {
		t.Errorf("promoting bob again printed %s, want %s as before", again, out)
	}
	wantRoles(t, h, map[string]string{harness.Owner: api.RoleOwner, "bob": api.RoleAdministrator})
	r := h.Koko("user", "promote", "bob")
	r.WantSuccess(t)
	harness.Golden(t, "user-promote.txt", strings.ReplaceAll(r.Stdout, inv.User.ID, "user_<1>"))

	u, out = userRole(t, h.Koko, "demote", "bob")
	harness.Golden(t, "user-demote.json", out)
	if u.ID != inv.User.ID || u.Role != api.RoleMember {
		t.Errorf("user demote bob printed %+v, want bob (%s) a Member", u, inv.User.ID)
	}
	if _, again := userRole(t, h.Koko, "demote", "bob"); again != out {
		t.Errorf("demoting bob again printed %s, want %s as before", again, out)
	}
	wantRoles(t, h, map[string]string{"bob": api.RoleMember})
	r = h.Koko("user", "demote", "bob")
	r.WantSuccess(t)
	harness.Golden(t, "user-demote.txt", strings.ReplaceAll(r.Stdout, inv.User.ID, "user_<1>"))

	log := h.Log()
	for _, want := range []string{
		`msg="user promoted" user=bob user_id=` + inv.User.ID + ` promoted_by="unix socket" join_codes_voided=0 password_links_voided=0`,
		`msg="user demoted" user=bob user_id=` + inv.User.ID + ` demoted_by="unix socket" join_codes_voided=0 password_links_voided=0`,
	} {
		if n := strings.Count(log, want); n != 1 {
			t.Errorf("the log holds %d lines with %s, want 1: the retries logged nothing:\n%s", n, want, log)
		}
	}
}

// Only the Owner promotes and demotes: a Member and an Administrator are
// refused with owner_only before the name is read, an Administrator's own
// demotion included. A name no User holds is refused with unknown_user, a
// Removed User with user_removed, and the Owner, whose role never changes,
// with owner_not_removable. No refusal changes a role.
func TestOnlyTheOwnerPromotesAndDemotes(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	addAdministrators(t, h, "eve", "frank")
	h.Koko("user", "invite", "bob").WantSuccess(t)
	h.Koko("user", "invite", "carol").WantSuccess(t)
	h.Koko("user", "invite", "gone").WantSuccess(t)
	bob, eve := h.Join("bob"), h.Join("eve")
	h.Join("gone")
	h.Koko("user", "remove", "gone").WantSuccess(t)

	r := bob.Koko("user", "promote", "carol", "--json")
	r.WantError(t, 1, "owner_only")
	harness.Golden(t, "user-promote-owner-only.json", r.Stderr)
	for _, d := range []*harness.Device{bob, eve} {
		for _, args := range [][]string{{"promote", "carol"}, {"promote", "nobody"}, {"demote", "frank"}, {"demote", "eve"}} {
			d.Koko(append([]string{"user"}, append(args, "--json")...)...).WantError(t, 1, "owner_only")
		}
	}
	for _, verb := range []string{"promote", "demote"} {
		h.Koko("user", verb, "nobody", "--json").WantError(t, 1, "unknown_user")
		h.Koko("user", verb, "gone", "--json").WantError(t, 1, "user_removed")
		r := h.Koko("user", verb, harness.Owner, "--json")
		r.WantError(t, 1, "owner_not_removable")
		harness.Golden(t, "user-"+verb+"-owner.json", r.Stderr)
	}
	wantRoles(t, h, map[string]string{
		harness.Owner: api.RoleOwner, "bob": api.RoleMember, "carol": api.RoleMember,
		"eve": api.RoleAdministrator, "frank": api.RoleAdministrator, "gone": api.RoleMember,
	})
}

// An Administrator does to Members what the Owner does: invites them by
// Join Code and by Password Link, adds and revokes their devices, revokes
// their Sign-ins, issues them a Password Link, removes their Password and
// Removes them; adds, renames and removes Spaces and changes their
// Members; and checks for a new release, as koko server upgrade --check
// does. The log names the Administrator's Device Session as who did it.
func TestAnAdministratorActsOnMembers(t *testing.T) {
	t.Parallel()
	rel := harness.NewMachine(t).Release("v0.5.0")
	rel.Latest()
	h := harness.Start(t, harness.WebListen("127.0.0.1:0"), harness.UpdateCheck(rel))
	h.Browser() // sets the Web URL
	addAdministrators(t, h, "eve")
	eve := h.Join("eve")
	eveID := readClientConfig(t, eve).DeviceSession

	r := eve.Koko("user", "invite", "bob", "--json")
	r.WantSuccess(t)
	var inv invitation
	if err := json.Unmarshal([]byte(r.Stdout), &inv); err != nil {
		t.Fatal(err)
	}
	joinAs(t, inv.JoinCode)
	eve.Koko("user", "invite", "carol", "--web").WantSuccess(t)
	laptop, _ := joinAs(t, deviceAddOn(t, eve, "laptop", "--user", "bob"))
	eve.Koko("device", "revoke", "bob/laptop").WantSuccess(t)
	laptop.Koko("space", "ls", "--json").WantError(t, 1, "revoked")

	b := h.SignIn("bob")
	signIns := signinLsOn(t, eve.Koko, "--user", "bob")
	if len(signIns) != 1 {
		t.Fatalf("signin ls --user bob, as eve, listed %+v, want bob's one Sign-in", signIns)
	}
	eve.Koko("signin", "revoke", signIns[0].ID).WantSuccess(t)
	if status, body := b.Send(api.UserCurrent, []byte("{}")); status == http.StatusOK {
		t.Errorf("bob's browser is still signed in after eve revoked its Sign-in: %s", body)
	}
	passwordLinkOn(t, eve.Koko, "bob")
	eve.Koko("password", "rm", "bob").WantSuccess(t)

	for _, args := range [][]string{
		{"space", "add", "ops"},
		{"space", "member", "add", "ops", "bob"},
		{"space", "rename", "ops", "infra"},
		{"space", "member", "rm", "infra", "bob"},
		{"space", "rm", "infra"},
		{"server", "upgrade", "--check"},
		{"user", "remove", "bob"},
		{"user", "remove", "carol"},
	} {
		if r := eve.Koko(args...); r.Status != 0 {
			t.Errorf("koko %s, as eve: exit status %d, stderr %q", strings.Join(args, " "), r.Status, r.Stderr)
		}
	}
	wantRoles(t, h, map[string]string{"bob": api.RoleMember})
	if roles := rolesOf(t, h.Koko); roles["carol"] != "" {
		t.Errorf("user ls --all lists carol, whom eve deleted: %v", roles)
	}
	log := h.Log()
	for _, want := range []string{
		`msg="device revoked" user=bob device=laptop device_session=`,
		`revoked_by=` + eveID,
		`msg="password removed" user=bob removed_by=` + eveID,
		`msg="user removed" user=bob user_id=` + inv.User.ID + ` role=member `,
		`removed_by=` + eveID,
		`msg="user deleted" user=carol `,
	} {
		if !strings.Contains(log, want) {
			t.Errorf("the log holds no line with %s:\n%s", want, log)
		}
	}
}

// An Administrator acts on no credential of the Owner or of another
// Administrator: each act is refused with owner_only, naming whom only the
// Owner acts on, and changes nothing. They act on their own as anyone does.
func TestAnAdministratorActsOnNeitherTheOwnerNorAnotherAdministrator(t *testing.T) {
	t.Parallel()
	h := harness.Start(t, harness.WebListen("127.0.0.1:0"))
	h.Browser() // sets the Web URL
	addAdministrators(t, h, "eve", "frank")
	eve, alice, frank := h.Join("eve"), h.Join(harness.Owner), h.Join("frank")
	browsers := map[string]*harness.Browser{harness.Owner: h.SignIn(harness.Owner), "frank": h.SignIn("frank")}
	devices := map[string]*harness.Device{harness.Owner: alice, "frank": frank}

	for target, whom := range map[string]string{harness.Owner: "the Owner", "frank": "another Administrator"} {
		signIn := signinLsOn(t, h.Koko, "--user", target)[0].ID
		for _, c := range []struct {
			args []string
			does string
		}{
			{[]string{"device", "add", "--user", target}, "issues a Join Code for"},
			{[]string{"device", "revoke", target + "/" + readClientConfig(t, devices[target]).Device}, "revokes the Device Sessions of"},
			{[]string{"signin", "revoke", signIn}, "revokes the Sign-ins of"},
			{[]string{"password", "link", target}, "issues a Password Link for"},
			{[]string{"password", "rm", target}, "removes the Password of"},
		} {
			msg := eve.Koko(append(c.args, "--json")...).WantError(t, 1, "owner_only")
			if want := "only the Owner " + c.does + " " + whom; msg != want {
				t.Errorf("koko %s, as eve, says %q, want %q", strings.Join(c.args, " "), msg, want)
			}
		}
		devices[target].Koko("space", "ls", "--json").WantSuccess(t)
		if status, body := browsers[target].Send(api.UserCurrent, []byte("{}")); status != http.StatusOK {
			t.Errorf("%s's browser was signed out by a refusal: %d %s", target, status, body)
		}
	}
	if msg := eve.Koko("user", "remove", "frank", "--json").WantError(t, 1, "owner_only"); msg != "only the Owner removes another Administrator" {
		t.Errorf("user remove frank, as eve, says %q", msg)
	}
	eve.Koko("user", "remove", harness.Owner, "--json").WantError(t, 1, "owner_not_removable")
	if msg := eve.Koko("user", "remove", "eve", "--json").WantError(t, 1, "owner_only"); msg != "only the Owner removes an Administrator" {
		t.Errorf("user remove eve, as eve, says %q", msg)
	}
	wantRoles(t, h, map[string]string{harness.Owner: api.RoleOwner, "eve": api.RoleAdministrator, "frank": api.RoleAdministrator})
	for _, args := range [][]string{{"device", "add", "--user", "eve"}, {"password", "link", "eve"}} {
		if r := eve.Koko(args...); r.Status != 0 {
			t.Errorf("koko %s, as eve: exit status %d, stderr %q", strings.Join(args, " "), r.Status, r.Stderr)
		}
	}
}

// The Owner acts on an Administrator's credentials as on anyone's: revokes
// their device, which is then refused, and removes their Password, which
// signs their browser out.
func TestTheOwnerActsOnAnAdministratorsCredentials(t *testing.T) {
	t.Parallel()
	h := harness.Start(t, harness.WebListen("127.0.0.1:0"))
	h.Browser() // sets the Web URL
	addAdministrators(t, h, "eve")
	eve, alice := h.Join("eve"), h.Join(harness.Owner)
	eveB := h.SignIn("eve")

	alice.Koko("device", "revoke", "eve/"+readClientConfig(t, eve).Device).WantSuccess(t)
	eve.Koko("space", "ls", "--json").WantError(t, 1, "revoked")
	alice.Koko("password", "rm", "eve").WantSuccess(t)
	if status, body := eveB.Send(api.UserCurrent, []byte("{}")); status == http.StatusOK {
		t.Errorf("eve's browser is still signed in after the Owner removed her Password: %s", body)
	}
	wantRoles(t, h, map[string]string{"eve": api.RoleAdministrator})
}

// Reading is not acting: an Administrator sees every Space, the Owner's
// Personal Space with its Hosts and Secrets included, every User with their
// role, and every User's Device Sessions.
func TestAnAdministratorReadsEveryRecord(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	addAdministrators(t, h, "eve")
	eve := h.Join("eve")
	h.Join(harness.Owner)
	h.Koko("user", "invite", "bob").WantSuccess(t)
	h.Join("bob")
	h.Koko("host", "add", "web1", "space="+harness.Owner, "address=203.0.113.10", "account=deploy").WantSuccess(t)
	h.KokoStdin("s3cret\n", "secret", "add", harness.Owner+"/root-password").WantSuccess(t)

	if r := eve.Koko("host", "ls", "--json"); !strings.Contains(r.Stdout, `"name": "web1"`) {
		t.Errorf("host ls, as eve, printed %s %s, want the Owner's web1", r.Stdout, r.Stderr)
	}
	// s3cret and its newline, as stdin gave it.
	if r := eve.Koko("secret", "show", harness.Owner+"/root-password", "--json"); !strings.Contains(r.Stdout, `"value_base64": "czNjcmV0Cg=="`) {
		t.Errorf("secret show %s/root-password, as eve, printed %s %s, want its value", harness.Owner, r.Stdout, r.Stderr)
	}
	var users []string
	for _, d := range deviceLs(t, eve.Koko) {
		users = append(users, d.User)
	}
	if want := []string{harness.Owner, "bob", "eve"}; !slices.Equal(slices.Compact(users), want) {
		t.Errorf("device ls, as eve, lists the Device Sessions of %v, want every User's: %v", users, want)
	}
	if got := deviceLs(t, eve.Koko, "--user", harness.Owner); len(got) != 1 {
		t.Errorf("device ls --user %s, as eve, lists %+v, want the Owner's device", harness.Owner, got)
	}
	got := rolesOf(t, eve.Koko)
	if got[harness.Owner] != api.RoleOwner || got["eve"] != api.RoleAdministrator || got["bob"] != api.RoleMember {
		t.Errorf("user ls, as eve, lists the roles %v", got)
	}
}

// Promoting a User voids the unused Join Codes and Password Links that
// someone other than the Owner and the User issued for them, so that
// nothing an Administrator prepared for a Member becomes an
// Administrator's; those the Owner issued, over the socket included, and
// those the User's own device issued stay.
func TestPromotionVoidsWhatAnotherIssuedForTheUser(t *testing.T) {
	t.Parallel()
	h := harness.Start(t, harness.WebListen("127.0.0.1:0"))
	h.Browser() // sets the Web URL
	addAdministrators(t, h, "eve")
	eve := h.Join("eve")
	r := eve.Koko("user", "invite", "bob", "--json")
	r.WantSuccess(t)
	var inv invitation
	if err := json.Unmarshal([]byte(r.Stdout), &inv); err != nil {
		t.Fatal(err)
	}
	evesLink := passwordLinkOn(t, eve.Koko, "bob")
	laptop, _ := joinAs(t, deviceAdd(t, h, "laptop", "--user", "bob"))
	owners := deviceAdd(t, h, "desktop", "--user", "bob")
	fromOwnersDevice := deviceAddOn(t, h.Join(harness.Owner), "tablet", "--user", "bob")
	bobs := deviceAddOn(t, laptop, "phone")
	h.Koko("user", "invite", "carol").WantSuccess(t)
	ownersLink := passwordLinkOn(t, h.Koko, "carol")

	h.Koko("user", "promote", "bob").WantSuccess(t)
	h.Koko("user", "promote", "carol").WantSuccess(t)
	harness.NewDevice(t).Koko("join", inv.JoinCode.Code, "--json").WantError(t, 1, "invalid_join_code")
	wantLinkValid(t, h, evesLink, false)
	for _, jc := range []joinCodeOut{owners, fromOwnersDevice, bobs} {
		harness.NewDevice(t).Koko("join", jc.Code).WantSuccess(t)
	}
	wantLinkValid(t, h, ownersLink, true)
	log := h.Log()
	for _, want := range []string{
		`msg="user promoted" user=bob user_id=` + inv.User.ID + ` promoted_by="unix socket" join_codes_voided=1 password_links_voided=1`,
		`msg="user promoted" user=carol user_id=`,
	} {
		if !strings.Contains(log, want) {
			t.Errorf("the log holds no line with %s:\n%s", want, log)
		}
	}
	if !strings.Contains(log, `promoted_by="unix socket" join_codes_voided=0 password_links_voided=0`) {
		t.Errorf("the log holds no line of carol's promotion voiding nothing:\n%s", log)
	}
}

// Demoting an Administrator ends at once their SSH Sessions to Hosts
// outside the Spaces they still act in, such as the Owner's Personal
// Space, with access withdrawn, and keeps those to a Space they are a
// Member of. It voids the unused Join Codes and Password Links they issued
// for other Users, and keeps those they issued for themselves.
func TestDemotionWithdrawsWhatTheRoleGave(t *testing.T) {
	t.Parallel()
	h := harness.Start(t, harness.WebListen("127.0.0.1:0"))
	h.Browser() // sets the Web URL
	addAdministrators(t, h, "eve")
	h.Koko("space", "add", "ops").WantSuccess(t)
	h.Koko("space", "member", "add", "ops", "eve").WantSuccess(t)
	h.Koko("user", "invite", "dave").WantSuccess(t)
	eve := h.Join("eve")
	managedHostIn(t, h, "web1", harness.Owner)
	managedHostIn(t, h, "web2", "ops")
	viaExec := eve.Background(nil, "exec", "web1", "echo ready; exec sleep 30")
	viaExec.WaitStdout("ready")
	toWeb2 := openLiveSSH(t, h, eve.Proxy(nil, "tunnel", "web2"), "web2")
	forDave, forSelf := deviceAddOn(t, eve, "--user", "dave"), deviceAddOn(t, eve, "tablet")
	davesLink, evesLink := passwordLinkOn(t, eve.Koko, "dave"), passwordLinkOn(t, eve.Koko, "eve")

	u, _ := userRole(t, h.Koko, "demote", "eve")
	const line = "koko: access to web1 was withdrawn; this SSH Session is closed.\n"
	if r := viaExec.Wait(); r.Status != 255 || r.Stdout != "ready\n" || r.Stderr != line {
		t.Errorf("koko exec: exit status %d, stdout %q, stderr %q; want 255 and %q", r.Status, r.Stdout, r.Stderr, line)
	}
	waitServerLog(t, h, `msg="ssh session ended" .* user=eve .* host=web1 .* reason="access withdrawn"\n`)
	toWeb2.wantOpen("still here")
	harness.NewDevice(t).Koko("join", forDave.Code, "--json").WantError(t, 1, "invalid_join_code")
	wantLinkValid(t, h, davesLink, false)
	harness.NewDevice(t).Koko("join", forSelf.Code).WantSuccess(t)
	wantLinkValid(t, h, evesLink, true)
	want := `msg="user demoted" user=eve user_id=` + u.ID + ` demoted_by="unix socket" join_codes_voided=1 password_links_voided=1`
	if log := h.Log(); !strings.Contains(log, want) {
		t.Errorf("the log holds no line with %s:\n%s", want, log)
	}
}

// The Owner Removes an Administrator directly: the role goes with them,
// user remove prints them a Member, and user removed logs the role they
// held. The Removal voids the codes and links they issued for others,
// from their Sign-in as from their device. With an Administrator present,
// the Local caller still acts as the Owner, and issues a Join Code for an
// Administrator too.
func TestTheOwnerRemovesAnAdministratorDirectly(t *testing.T) {
	t.Parallel()
	h := harness.Start(t, harness.WebListen("127.0.0.1:0"))
	h.Browser() // sets the Web URL
	addAdministrators(t, h, "eve", "frank")
	h.Koko("user", "invite", "dave").WantSuccess(t)
	eve := h.Join("eve")
	status, body := h.SignIn("eve").Send(api.DeviceAdd, []byte(`{"user": "dave"}`))
	var fromSignIn api.DeviceAddResponse
	if err := json.Unmarshal(body, &fromSignIn); status != http.StatusOK || err != nil {
		t.Fatalf("device.add for dave from eve's Sign-in: %d %s", status, body)
	}
	davesLink := passwordLinkOn(t, eve.Koko, "dave")

	got := userRemove(t, h.Koko, "eve")
	if got.Outcome != "removed" || got.User.Role != api.RoleMember {
		t.Errorf("user remove eve printed %+v, want eve Removed, a Member", got)
	}
	wantRoles(t, h, map[string]string{"eve": api.RoleMember, "frank": api.RoleAdministrator})
	harness.NewDevice(t).Koko("join", fromSignIn.JoinCode.Code, "--json").WantError(t, 1, "invalid_join_code")
	wantLinkValid(t, h, davesLink, false)
	// eve's Invite and the code eve's Sign-in issued.
	want := `msg="user removed" user=eve user_id=` + got.User.ID + ` role=administrator device_sessions_revoked=1 join_codes_voided=2 signins_ended=1 password_links_voided=1 removed_by="unix socket"`
	if log := h.Log(); !strings.Contains(log, want) {
		t.Errorf("the log holds no line with %s:\n%s", want, log)
	}

	if jc := deviceAdd(t, h); jc.User != harness.Owner {
		t.Errorf("device add on the koko server's machine issued a code for %s, want the Owner", jc.User)
	}
	if jc := deviceAdd(t, h, "--user", "frank"); jc.User != "frank" {
		t.Errorf("device add --user frank on the koko server's machine issued a code for %s, want frank", jc.User)
	}
}
