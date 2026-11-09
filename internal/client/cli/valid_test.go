package cli_test

import (
	"encoding/json/v2"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/shared/api"
	"github.com/cieons/koko/internal/shared/joincode"
)

// --valid takes a whole number of minutes, hours or days from 1m to 7d.
// Anything else is refused with exit 2 before any call, on every command
// that takes it.
func TestValidOutsideItsRangeIsRefusedBeforeAnyCall(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	commands := [][]string{{"device", "add"}, {"user", "invite", "bob"}, {"user", "invite", "bob", "--web"}, {"password", "link", "alice"}}
	refused := []string{
		"0m", "0d", "8d", "169h", "10081m", "99999999999999999999d",
		"7", "d", "30s", "1w", "1.5h", "1h30m", "7D", "-1d", "+1d", " 1d", "1d ", "１d",
	}
	for _, command := range commands {
		for _, valid := range refused {
			r := h.Koko(append(command, "--valid", valid, "--json")...)
			msg := r.WantError(t, 2, "invalid_usage")
			if want := "--valid takes a whole number of minutes, hours or days from 1m to 7d, such as 30m, 12h or 7d, not " + strconv.Quote(valid); msg != want {
				t.Errorf("koko %s --valid %q says %q, want %q", strings.Join(command, " "), valid, msg, want)
			}
		}
	}
	if n := h.Calls(); n != 0 {
		t.Errorf("the refused command lines made %d call(s), want none", n)
	}
}

// Within its range, --valid sets when the Join Code expires, on device add
// and on user invite alike, and the code keeps its form.
func TestValidSetsWhenTheJoinCodeExpires(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	for valid, want := range map[string]time.Duration{
		"1m":     time.Minute,
		"90m":    90 * time.Minute,
		"12h":    12 * time.Hour,
		"168h":   7 * 24 * time.Hour,
		"10080m": 7 * 24 * time.Hour,
		"07d":    7 * 24 * time.Hour,
	} {
		jc := deviceAdd(t, h, "--valid", valid)
		if expires := h.Now().Add(want).Format(time.RFC3339); jc.ExpiresAt != expires {
			t.Errorf("device add --valid %s expires at %s, want %s", valid, jc.ExpiresAt, expires)
		}
		if _, err := joincode.Decode(jc.Code); err != nil {
			t.Errorf("decode the Join Code of device add --valid %s: %v", valid, err)
		}
	}
	inv := userInvite(t, h, "bob", "--valid", "2d")
	if expires := h.Now().Add(48 * time.Hour).Format(time.RFC3339); inv.JoinCode.ExpiresAt != expires {
		t.Errorf("user invite --valid 2d expires at %s, want %s", inv.JoinCode.ExpiresAt, expires)
	}
}

// The API takes valid_for in seconds, from 60 to 604,800, and refuses any
// other number with invalid_value, recording nothing: a refused Invite
// leaves its name free.
func TestTheAPIRefusesAValidForOutsideItsRange(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	for _, seconds := range []int{-60, 0, 59, 604_801} {
		if _, err := h.Call(api.DeviceAdd, api.DeviceAddRequest{Device: "laptop", ValidFor: new(seconds)}); !isCode(err, api.CodeInvalidValue) {
			t.Errorf("device.add with valid_for %d: %v, want invalid_value", seconds, err)
		}
		if _, err := h.Call(api.UserInvite, api.UserInviteRequest{Name: "bob", ValidFor: new(seconds)}); !isCode(err, api.CodeInvalidValue) {
			t.Errorf("user.invite with valid_for %d: %v, want invalid_value", seconds, err)
		}
	}
	for device, seconds := range map[string]int{"laptop": 60, "desktop": 604_800} {
		resp, err := h.Call(api.DeviceAdd, api.DeviceAddRequest{Device: device, ValidFor: new(seconds)})
		if err != nil {
			t.Fatalf("device.add with valid_for %d: %v", seconds, err)
		}
		if want := h.Now().Add(time.Duration(seconds) * time.Second).Format(time.RFC3339); resp.JoinCode.ExpiresAt != want {
			t.Errorf("device.add with valid_for %d expires at %s, want %s", seconds, resp.JoinCode.ExpiresAt, want)
		}
	}
	if _, err := h.Call(api.UserInvite, api.UserInviteRequest{Name: "bob", ValidFor: new(604_800)}); err != nil {
		t.Errorf("user.invite bob after the refused ones: %v", err)
	}
	h.Koko("server", "set", "web_url="+harness.WebURL).WantSuccess(t)
	for _, seconds := range []int{-60, 0, 59, 604_801} {
		if _, err := h.Call(api.PasswordLinkIssue, api.PasswordLinkRequest{User: "bob", ValidFor: new(seconds)}); !isCode(err, api.CodeInvalidValue) {
			t.Errorf("password.link with valid_for %d: %v, want invalid_value", seconds, err)
		}
		if _, err := h.Call(api.UserInvite, api.UserInviteRequest{Name: "carol", Web: true, ValidFor: new(seconds)}); !isCode(err, api.CodeInvalidValue) {
			t.Errorf("user.invite --web with valid_for %d: %v, want invalid_value", seconds, err)
		}
	}
}

// A Join Code of 7d joins 6 days after its issue and is refused once its 7
// days are up, whether device add or user invite issued it.
func TestAJoinCodeOfSevenDaysJoinsAfterSixAndIsRefusedAfterSeven(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	inv := userInvite(t, h, "bob", "--valid", "7d")
	late := deviceAdd(t, h, "laptop", "--valid", "7d").Code

	h.Advance(6 * 24 * time.Hour)
	harness.NewDevice(t).Koko("join", inv.JoinCode.Code).WantSuccess(t)
	h.Advance(24 * time.Hour)
	harness.NewDevice(t).Koko("join", late, "--json").WantError(t, 1, "invalid_join_code")
}

// A Password Link of 7d sets a Password 6 days after its issue and is
// refused once its 7 days are up, whether password link or user invite
// --web issued it.
func TestAPasswordLinkOfSevenDaysWorksAfterSixAndIsRefusedAfterSeven(t *testing.T) {
	t.Parallel()
	h := harness.Start(t, harness.WebListen("127.0.0.1:0"))
	b := h.Browser() // sets the Web URL
	invited := passwordLinkOf(t, h, "user", "invite", "bob", "--web", "--valid", "7d")
	late := passwordLinkOf(t, h, "password", "link", "alice", "--valid", "7d")

	h.Advance(6 * 24 * time.Hour)
	if status, body := b.Post(api.AuthPasswordPath, api.AuthPasswordRequest{Link: harness.LinkSecret(invited), Password: harness.Password}); status != 200 {
		t.Errorf("the link 6 days on answered %d %s", status, body)
	}
	h.Advance(24 * time.Hour)
	status, body := b.Post(api.AuthPasswordPath, api.AuthPasswordRequest{Link: harness.LinkSecret(late), Password: harness.Password})
	if status != api.CodeLinkInvalid.Status || !strings.Contains(body, api.CodeLinkInvalid.Name) {
		t.Errorf("the link 7 days on answered %d %s, want link_invalid", status, body)
	}
}

// passwordLinkOf runs a command line that issues a Password Link, with
// --json, and returns the link.
func passwordLinkOf(t *testing.T, h *harness.Harness, args ...string) string {
	t.Helper()
	r := h.Koko(append(args, "--json")...)
	r.WantSuccess(t)
	var out struct {
		PasswordLink string `json:"password_link"`
	}
	if err := json.Unmarshal([]byte(r.Stdout), &out); err != nil || out.PasswordLink == "" {
		t.Fatalf("koko %s printed %q: %v", strings.Join(args, " "), r.Stdout, err)
	}
	return out.PasswordLink
}

// isCode tells whether err is the API's refusal with code.
func isCode(err error, code api.Code) bool {
	e, ok := errors.AsType[*api.Error](err)
	return ok && e.Code == code.Name
}
