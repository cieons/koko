package cli_test

import (
	"encoding/json/v2"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/cieons/koko/internal/client/serverconn"
	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/shared/wire"
)

// deviceRevoke runs device revoke --json with args through run, and returns
// the Device Session it Revoked.
func deviceRevoke(t *testing.T, run func(...string) harness.Result, args ...string) deviceSessionOut {
	t.Helper()
	r := run(append([]string{"device", "revoke", "--json"}, args...)...)
	r.WantSuccess(t)
	var d deviceSessionOut
	if err := json.Unmarshal([]byte(r.Stdout), &d); err != nil {
		t.Fatalf("device revoke --json printed %q: %v", r.Stdout, err)
	}
	return d
}

// revocationRecord is the record a device keeps in ~/.ssh/koko/ once it
// learns that its Device Session was Revoked.
type revocationRecord struct {
	ServerAddress string `json:"server_address"`
	User          string `json:"user"`
	Device        string `json:"device"`
	DeviceSession string `json:"device_session"`
	LearnedAt     string `json:"learned_at"`
}

// readRevocation reads the device's revocation record, failing the test
// when it has none.
func readRevocation(t *testing.T, d *harness.Device) revocationRecord {
	t.Helper()
	path := filepath.Join(kokoDir(d), "revoked.json")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the revocation record: %v", err)
	}
	if m := mode(t, path); m.Perm() != 0o600 {
		t.Errorf("the revocation record has mode %v, want 0600", m)
	}
	var r revocationRecord
	if err := json.Unmarshal(b, &r, json.RejectUnknownMembers(true)); err != nil {
		t.Fatalf("the revocation record %s does not parse: %v", b, err)
	}
	return r
}

// wantGone fails the test unless the device holds none of the files named
// in ~/.ssh/koko/.
func wantGone(t *testing.T, d *harness.Device, files ...string) {
	t.Helper()
	for _, f := range files {
		if _, err := os.Lstat(filepath.Join(kokoDir(d), f)); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("~/.ssh/koko/%s is still there (%v)", f, err)
		}
	}
}

// A device whose Device Session was Revoked from elsewhere learns it on its
// next command, by name: koko says the Device Session "<name>" of this
// device was revoked, deletes the device's key and client config, and keeps
// a record of which Device Session it was and when koko learned it. Every
// later command explains the revocation from that record until the next
// koko join, which the record does not block and which clears it.
func TestARevokedDeviceIsToldByNameAndForgetsItsJoin(t *testing.T) {
	h := harness.Start(t)
	h.Join(harness.Owner)
	laptop, laptopID := joinAs(t, deviceAdd(t, h, "laptop"))
	server := readClientConfig(t, laptop).ServerAddress
	deviceRevoke(t, h.Koko, "laptop")

	r := laptop.Koko("space", "ls", "--json")
	msg := r.WantError(t, 1, "revoked")
	harness.Golden(t, "space-ls-revoked.json", strings.ReplaceAll(r.Stderr, server, "<server address>"))
	if !strings.HasPrefix(msg, `the Device Session "laptop" of this device was revoked`) || !strings.Contains(msg, "koko join") {
		t.Errorf("the revoked device says %q, want that its Device Session \"laptop\" was revoked and to join again", msg)
	}
	wantGone(t, laptop, "device_key", "client.json")
	rec := readRevocation(t, laptop)
	if _, err := time.Parse(time.RFC3339, rec.LearnedAt); err != nil || rec.ServerAddress != server ||
		rec.User != harness.Owner || rec.Device != "laptop" || rec.DeviceSession != laptopID {
		t.Errorf("the revocation record is %+v, want alice's laptop %s at %s with when koko learned it", rec, laptopID, server)
	}

	r = laptop.Koko("space", "ls", "--json")
	msg = r.WantError(t, 1, "revoked")
	harness.Golden(t, "space-ls-revoked-record.json", strings.ReplaceAll(r.Stderr, server, "<server address>"))
	if !strings.HasPrefix(msg, `the Device Session "laptop" of this device was revoked`) || !strings.Contains(msg, rec.LearnedAt) {
		t.Errorf("a later command says %q, want the revocation explained from the record", msg)
	}

	// A Revoked Device Session's name is free again.
	laptop.Koko("join", deviceAdd(t, h, "laptop").Code).WantSuccess(t)
	wantGone(t, laptop, "revoked.json")
	laptop.Koko("space", "ls", "--json").WantSuccess(t)
}

// waitClosed fails the test unless conn ends within 10 s.
func waitClosed(t *testing.T, conn serverconn.Conn, what string) {
	t.Helper()
	ended := make(chan struct{})
	go func() {
		_ = conn.Wait()
		close(ended)
	}()
	select {
	case <-ended:
	case <-time.After(10 * time.Second):
		t.Fatalf("%s is still open 10 s after the revocation", what)
	}
}

// Revoking a Device Session closes its open Device Connections at once, and
// no other's: the device it added keeps its connection and goes on working.
func TestRevokingClosesTheDeviceSessionsConnections(t *testing.T) {
	h := harness.Start(t)
	h.Join(harness.Owner)
	laptop, _ := joinAs(t, deviceAdd(t, h, "laptop"))
	phone, _ := joinAs(t, deviceAddOn(t, laptop, "phone"))
	laptopConns := []serverconn.Conn{laptop.Dial(), laptop.Dial()}
	phoneConn := phone.Dial()

	deviceRevoke(t, h.Koko, "laptop")
	for _, conn := range laptopConns {
		waitClosed(t, conn, "the laptop's Device Connection")
	}
	ch, err := phoneConn.OpenChannel(t.Context(), wire.ChannelAPI, nil)
	if err != nil {
		t.Fatalf("the phone's Device Connection no longer opens a channel: %v", err)
	}
	_ = ch.Close()
	phone.Koko("space", "ls", "--json").WantSuccess(t)
}

// A User revokes the device they are using like any other of theirs: the
// call answers before the koko server closes its Device Connection, and the
// device then deletes its key and client config and keeps the revocation
// record, as when it learns of a revocation from elsewhere. For people,
// device revoke says that it was this device.
func TestRevokingTheDeviceInUse(t *testing.T) {
	h := harness.Start(t)
	h.Koko("user", "invite", "bob").WantSuccess(t)
	laptop, laptopID := joinAs(t, deviceAdd(t, h, "laptop", "--user", "bob"))
	desktop, desktopID := joinAs(t, deviceAdd(t, h, "desktop", "--user", "bob"))
	server := readClientConfig(t, laptop).ServerAddress

	r := laptop.Koko("device", "revoke", "laptop")
	r.WantSuccess(t)
	if want := "Revoked bob's device laptop (" + laptopID + ").\n" +
		"It was this device: koko deleted its key and client config; to reach the koko server at " + server +
		" again, join with a new Join Code: koko join <join-code>\n"; r.Stdout != want {
		t.Errorf("stdout = %q, want %q", r.Stdout, want)
	}
	wantGone(t, laptop, "device_key", "client.json")
	if rec := readRevocation(t, laptop); rec.DeviceSession != laptopID || rec.Device != "laptop" {
		t.Errorf("the revocation record is %+v, want bob's laptop %s", rec, laptopID)
	}
	laptop.Koko("space", "ls", "--json").WantError(t, 1, "revoked")

	if d := deviceRevoke(t, desktop.Koko, "bob/desktop"); d.ID != desktopID || d.RevokedAt == nil {
		t.Errorf("device revoke --json on the desktop printed %+v, want the desktop %s revoked", d, desktopID)
	}
	wantGone(t, desktop, "device_key", "client.json")
}

// Revoking a Device Session voids the Join Codes it issued that are still
// unused, so a thief's pending code dies with the laptop, and frees their
// device names; codes others issued still join. The devices it added keep
// working, and a code already used stays refused once its own Device
// Session is revoked, though that frees its device name.
func TestRevokingVoidsTheJoinCodesItIssued(t *testing.T) {
	h := harness.Start(t)
	h.Join(harness.Owner)
	laptop, _ := joinAs(t, deviceAdd(t, h, "laptop"))
	used := deviceAddOn(t, laptop, "phone")
	phone, _ := joinAs(t, used)
	pending := deviceAddOn(t, laptop, "tablet")
	fromSocket := deviceAdd(t, h, "desktop")

	deviceRevoke(t, h.Koko, "laptop")
	d := harness.NewDevice(t)
	d.Koko("join", pending.Code, "--json").WantError(t, 1, "invalid_join_code")
	wantNothingLeft(t, d)
	if jc := deviceAdd(t, h, "tablet"); jc.Device != "tablet" {
		t.Errorf("after its code was voided, device add tablet named the device %q", jc.Device)
	}
	joinAs(t, fromSocket)
	phone.Koko("space", "ls", "--json").WantSuccess(t)

	deviceRevoke(t, h.Koko, "phone")
	harness.NewDevice(t).Koko("join", used.Code, "--json").WantError(t, 1, "invalid_join_code")
}

// device-<n> counts from one past every Device Session the User ever had,
// Revoked ones included, so a device left unnamed never takes the name of
// one that was revoked.
func TestDeviceNamesCountPastRevokedDeviceSessions(t *testing.T) {
	h := harness.Start(t)
	r := h.Koko("user", "invite", "bob", "--json")
	r.WantSuccess(t)
	var inv invitation
	if err := json.Unmarshal([]byte(r.Stdout), &inv); err != nil {
		t.Fatal(err)
	}
	joinAs(t, joinCodeOut{Code: inv.JoinCode.Code})
	deviceRevoke(t, h.Koko, "bob/device-1")
	if jc := deviceAdd(t, h, "--user", "bob"); jc.Device != "device-2" {
		t.Errorf("after bob's device-1 was revoked, device add named his next device %q, want device-2", jc.Device)
	}
}

// Revoking the Owner's own last live Device Session, from anywhere, needs
// --last, since recovery then goes through the koko server's machine; while
// another of the Owner's devices is left, it needs none.
func TestRevokingTheOwnersLastDeviceSessionNeedsLast(t *testing.T) {
	h := harness.Start(t)
	laptop, _ := joinAs(t, deviceAdd(t, h, "laptop"))
	joinAs(t, deviceAdd(t, h, "phone"))
	deviceRevoke(t, laptop.Koko, "phone")

	for _, run := range []func(...string) harness.Result{h.Koko, laptop.Koko} {
		r := run("device", "revoke", "alice/laptop", "--json")
		if msg := r.WantError(t, 1, "last_device_session"); !strings.Contains(msg, "--last") {
			t.Errorf("the refusal says %q, want it to name --last", msg)
		}
		harness.Golden(t, "device-revoke-last.json", r.Stderr)
	}
	laptop.Koko("space", "ls", "--json").WantSuccess(t)
	if d := deviceRevoke(t, laptop.Koko, "laptop", "--last"); d.Device != "laptop" || d.RevokedAt == nil {
		t.Errorf("device revoke laptop --last printed %+v, want the laptop revoked", d)
	}
	if l := deviceLs(t, h.Koko, "--user", harness.Owner); len(l) != 0 {
		t.Errorf("the Owner still has %+v", l)
	}
}

// A Member revokes their own Device Sessions only: naming another User's
// gets, byte for byte, the answer for one that does not exist, and revokes
// nothing.
func TestAMemberCannotRevokeAnotherUsersDeviceSession(t *testing.T) {
	h := harness.Start(t)
	h.Koko("user", "invite", "bob").WantSuccess(t)
	laptop, _ := joinAs(t, deviceAdd(t, h, "laptop"))
	joinAs(t, deviceAdd(t, h, "phone"))
	bob := h.Join("bob")

	r := bob.Koko("device", "revoke", "alice/laptop", "--json")
	r.WantError(t, 1, "unknown_device")
	harness.Golden(t, "device-revoke-unknown.json", r.Stderr)
	laptop.Koko("space", "ls", "--json").WantSuccess(t)

	deviceRevoke(t, h.Koko, "alice/laptop")
	if again := h.Koko("device", "revoke", "alice/laptop", "--json"); again.Stderr != r.Stderr {
		t.Errorf("bob naming alice's live laptop got %s, but naming a laptop that is not there gets %s", r.Stderr, again.Stderr)
	}
	bob.Koko("device", "revoke", "laptop", "--json").WantError(t, 1, "unknown_device")
}

// Revocations reach the koko server's log beside joins: whose Device
// Session, revoked by which Device Session or the Unix socket, and how many
// Join Codes it voided; and so do each Device Connection it closed and each
// later try of the Revoked device. None holds a code
// (TestNoJoinCodeIsStoredOrLogged).
func TestRevocationsAreLogged(t *testing.T) {
	h := harness.Start(t)
	laptop, laptopID := joinAs(t, deviceAdd(t, h, "laptop"))
	phone, phoneID := joinAs(t, deviceAdd(t, h, "phone"))
	deviceAddOn(t, phone, "tablet")
	conn := phone.Dial()

	deviceRevoke(t, laptop.Koko, "phone")
	waitClosed(t, conn, "the phone's Device Connection")
	phone.Koko("space", "ls", "--json").WantError(t, 1, "revoked")
	deviceRevoke(t, h.Koko, "laptop", "--last")

	for _, line := range []string{
		`level=INFO msg="device revoked" user=alice device=phone device_session=` + phoneID +
			` revoked_by=` + laptopID + ` join_codes_voided=1`,
		`level=INFO msg="device connection closed" device_session=` + phoneID + ` addr=127\.0\.0\.1:\d+ reason=revoked`,
		`level=INFO msg="revoked device refused" device_session=` + phoneID + ` addr=127\.0\.0\.1:\d+`,
		`level=INFO msg="device revoked" user=alice device=laptop device_session=` + laptopID +
			` revoked_by="unix socket" join_codes_voided=0`,
	} {
		if !regexp.MustCompile(line).MatchString(h.Log()) {
			t.Errorf("the log holds no line matching %s:\n%s", line, h.Log())
		}
	}
}

// The Owner revokes any User's Device Session by <user>/<device>: device
// revoke prints it with its revoked time, device ls leaves it out from then
// on, and device ls --all lists it among the live ones, with that time.
func TestTheOwnerRevokesAnyonesDeviceSession(t *testing.T) {
	h := harness.Start(t)
	h.Koko("user", "invite", "bob").WantSuccess(t)
	_, laptopID := joinAs(t, deviceAdd(t, h, "laptop", "--user", "bob"))
	_, desktopID := joinAs(t, deviceAdd(t, h, "desktop", "--user", "bob"))
	h.Advance(time.Hour)

	r := h.Koko("device", "revoke", "bob/laptop", "--json")
	r.WantSuccess(t)
	harness.Golden(t, "device-revoke.json", r.Stdout)
	var revoked deviceSessionOut
	if err := json.Unmarshal([]byte(r.Stdout), &revoked); err != nil {
		t.Fatal(err)
	}
	now := h.Now().Format(time.RFC3339)
	if revoked.ID != laptopID || revoked.User != "bob" || revoked.Device != "laptop" || revoked.RevokedAt == nil || *revoked.RevokedAt != now {
		t.Errorf("device revoke printed %+v, want bob's laptop %s revoked at %s", revoked, laptopID, now)
	}

	if l := deviceLs(t, h.Koko); len(l) != 1 || l[0].ID != desktopID {
		t.Errorf("device ls after the revocation listed %+v, want bob's desktop only", l)
	}
	r = h.Koko("device", "ls", "--all", "--json")
	r.WantSuccess(t)
	harness.Golden(t, "device-ls-all.json", r.Stdout)
	l := deviceLs(t, h.Koko, "--all")
	if len(l) != 2 || l[0].ID != desktopID || l[0].RevokedAt != nil || l[1].ID != laptopID || orNull(l[1].RevokedAt) != now {
		t.Errorf("device ls --all listed %+v, want bob's live desktop and his laptop revoked at %s", l, now)
	}
}
