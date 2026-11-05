package cli_test

import (
	"bytes"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"encoding/json/v2"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/shared/api"
	"github.com/cieons/koko/internal/shared/joincode"
)

// joinCodeOut is an issued Join Code as the CLI's --json prints it.
type joinCodeOut struct {
	ID        string `json:"id"`
	Code      string `json:"code"`
	User      string `json:"user"`
	Device    string `json:"device"`
	ExpiresAt string `json:"expires_at"`
}

// deviceAdd runs koko device add with args and --json, and returns the Join
// Code it issued.
func deviceAdd(t *testing.T, h *harness.Harness, args ...string) joinCodeOut {
	t.Helper()
	r := h.Koko(append([]string{"device", "add", "--json"}, args...)...)
	r.WantSuccess(t)
	var jc joinCodeOut
	if err := json.Unmarshal([]byte(r.Stdout), &jc); err != nil {
		t.Fatalf("device add --json printed %q: %v", r.Stdout, err)
	}
	return jc
}

// fingerprintOf is a Join Code's host key fingerprint as OpenSSH prints it.
func fingerprintOf(c joincode.Code) string {
	return "SHA256:" + base64.RawStdEncoding.EncodeToString(c.Fingerprint[:])
}

// Over the Unix socket, koko device add issues a Join Code for the Owner,
// valid for 15 minutes from its issue, naming the koko server's Server
// Address with its listen port and its host key.
func TestDeviceAddIssuesAJoinCodeForTheOwner(t *testing.T) {
	listen := harness.FreeAddr(t)
	_, port, _ := net.SplitHostPort(listen)
	h := harness.Start(t, harness.NoInit(), harness.Listen(listen))
	init, err := h.Call(api.ServerInit, api.ServerInitRequest{Owner: harness.Owner, Address: harness.Address})
	if err != nil {
		t.Fatalf("server.init: %v", err)
	}
	r := h.Koko("device", "add", "--json")
	r.WantSuccess(t)
	harness.Golden(t, "device-add.json", r.Stdout)

	var jc joinCodeOut
	if err := json.Unmarshal([]byte(r.Stdout), &jc); err != nil {
		t.Fatal(err)
	}
	// Initialisation's own Join Code holds device-1.
	if want := h.Now().Add(15 * time.Minute).Format(time.RFC3339); jc.User != harness.Owner || jc.Device != "device-2" || jc.ExpiresAt != want {
		t.Errorf("device add = %+v, want a Join Code for %s's device-2 that expires at %s", jc, harness.Owner, want)
	}
	c, err := joincode.Decode(jc.Code)
	if err != nil {
		t.Fatalf("decode the Join Code: %v", err)
	}
	if c.Address != harness.Address+":"+port || fingerprintOf(c) != init.HostKeyFingerprint {
		t.Errorf("the Join Code names %s with host key %s, want %s:%s with %s", c.Address, fingerprintOf(c), harness.Address, port, init.HostKeyFingerprint)
	}
}

// For people, device add says for whom and until when, then prints the one
// koko join line to paste on the new device.
func TestDeviceAddForPeople(t *testing.T) {
	h := harness.Start(t)
	r := h.Koko("device", "add", "laptop")
	r.WantSuccess(t)
	lines := strings.Split(strings.TrimSuffix(r.Stdout, "\n"), "\n")
	expires := h.Now().Add(15 * time.Minute).Format(time.RFC3339)
	if len(lines) != 2 || lines[0] != "On alice's new device laptop, before "+expires+", run:" {
		t.Fatalf("stdout = %q, want a line saying for whom and until when, then the koko join line", r.Stdout)
	}
	code, ok := strings.CutPrefix(lines[1], "koko join ")
	if _, err := joincode.Decode(code); !ok || err != nil {
		t.Errorf("the last line is %q, want koko join and a Join Code (%v)", lines[1], err)
	}
}

// Without a name the device is device-<n>, counting up past the names the
// User's valid Join Codes hold, so several codes can be valid at once
// without colliding.
func TestDeviceAddNamesTheDeviceAfterTheNamesTaken(t *testing.T) {
	h := harness.Start(t)
	// Initialisation's Join Code holds device-1.
	var codes []string
	for _, want := range []string{"device-2", "device-3"} {
		jc := deviceAdd(t, h)
		if jc.Device != want {
			t.Errorf("device add named the device %q, want %q", jc.Device, want)
		}
		codes = append(codes, jc.Code)
	}
	if jc := deviceAdd(t, h, "device-5"); jc.Device != "device-5" {
		t.Errorf("device add device-5 named the device %q", jc.Device)
	}
	for _, want := range []string{"device-4", "device-6"} {
		if jc := deviceAdd(t, h); jc.Device != want {
			t.Errorf("device add named the device %q, want %q", jc.Device, want)
		}
	}
	if codes[0] == codes[1] {
		t.Error("two Join Codes are the same")
	}
}

// A name a valid Join Code of the User holds is refused; once the code has
// expired, 15 minutes after its issue, the name is free again.
func TestDeviceAddRefusesANameAValidJoinCodeHolds(t *testing.T) {
	h := harness.Start(t)
	deviceAdd(t, h, "laptop")
	r := h.Koko("device", "add", "laptop", "--json")
	r.WantError(t, 1, "name_taken")
	harness.Golden(t, "device-add-taken.json", r.Stderr)

	h.Advance(15*time.Minute - time.Second)
	h.Koko("device", "add", "laptop", "--json").WantError(t, 1, "name_taken")
	h.Advance(time.Second)
	if jc := deviceAdd(t, h, "laptop"); jc.Device != "laptop" {
		t.Errorf("device add laptop after the code expired named the device %q", jc.Device)
	}
	// Every earlier code has expired: device-1 is free again.
	if jc := deviceAdd(t, h); jc.Device != "device-1" {
		t.Errorf("device add after every code expired named the device %q, want device-1", jc.Device)
	}
}

// A device name is a lowercase DNS label.
func TestDeviceAddRefusesAnInvalidName(t *testing.T) {
	h := harness.Start(t)
	for _, device := range []string{"Laptop", "lap_top", "laptop-", "a/b", strings.Repeat("a", 64)} {
		h.Koko("device", "add", device, "--json").WantError(t, 1, "invalid_name")
	}
	if jc := deviceAdd(t, h, strings.Repeat("a", 63)); jc.Device != strings.Repeat("a", 63) {
		t.Errorf("device add with a 63-character name named the device %q", jc.Device)
	}
}

// --user names a User who is not there: unknown_user, and nothing issued.
func TestDeviceAddRefusesAnUnknownUser(t *testing.T) {
	h := harness.Start(t)
	r := h.Koko("device", "add", "--user", "bob", "--json")
	r.WantError(t, 1, "unknown_user")
	harness.Golden(t, "device-add-unknown-user.json", r.Stderr)
	h.Koko("device", "add", "--user", "Bob", "--json").WantError(t, 1, "invalid_name")
	if jc := deviceAdd(t, h); jc.Device != "device-2" {
		t.Errorf("after the refusals device add named the device %q, want device-2", jc.Device)
	}
}

// The koko server keeps only a hash of each Join Code's secret: after
// issuing and joining, neither koko.db and its -wal and -shm, while it runs
// or once it stops, nor its log holds a code or its secret in any common
// encoding.
func TestNoJoinCodeIsStoredOrLogged(t *testing.T) {
	dataDir := t.TempDir()
	h := harness.Start(t, harness.NoInit(), harness.DataDir(dataDir))
	init, err := h.Call(api.ServerInit, api.ServerInitRequest{Owner: harness.Owner, Address: harness.Address})
	if err != nil {
		t.Fatalf("server.init: %v", err)
	}
	codes := []string{init.JoinCode.Code}
	r := h.Koko("user", "invite", "bob", "--json")
	r.WantSuccess(t)
	var inv invitation
	if err := json.Unmarshal([]byte(r.Stdout), &inv); err != nil {
		t.Fatal(err)
	}
	codes = append(codes, inv.JoinCode.Code, deviceAdd(t, h).Code, deviceAdd(t, h, "laptop", "--user", "bob").Code)
	// A join and a failed join with a spent code, whose secret travels in
	// hex, leave neither in the record or the log.
	harness.NewDevice(t).Koko("join", codes[1]).WantSuccess(t)
	harness.NewDevice(t).Koko("join", codes[1], "--json").WantError(t, 1, "invalid_join_code")

	secrets := map[string][]byte{}
	for i, code := range codes {
		c, err := joincode.Decode(code)
		if err != nil {
			t.Fatal(err)
		}
		s := c.Secret[:]
		b32 := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(s)
		for name, form := range map[string]string{
			"the code":                    code,
			"the code without its prefix": strings.TrimPrefix(code, joincode.Prefix),
			"the secret":                  string(s),
			"the secret in hex":           hex.EncodeToString(s),
			"the secret in HEX":           strings.ToUpper(hex.EncodeToString(s)),
			"the secret in base64":        base64.RawStdEncoding.EncodeToString(s),
			"the secret in base64url":     base64.RawURLEncoding.EncodeToString(s),
			"the secret in base32":        b32,
			"the secret in lower base32":  strings.ToLower(b32),
		} {
			secrets[name+" of code "+string(rune('1'+i))] = []byte(form)
		}
	}
	running := readDir(t, dataDir)
	h.Stop()
	stopped := readDir(t, dataDir)
	places := map[string][]byte{"the log": []byte(h.Log())}
	for file, content := range running {
		places[file+" while running"] = content
	}
	for file, content := range stopped {
		places[file+" once stopped"] = content
	}
	if len(running["koko.db-wal"]) == 0 {
		t.Fatalf("the running koko server's data directory holds no -wal to check: %v", running)
	}
	for place, content := range places {
		for name, secret := range secrets {
			if bytes.Contains(content, secret) {
				t.Errorf("%s holds %s", place, name)
			}
		}
	}
}

// readDir reads every file in dir, by name.
func readDir(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{}
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		files[e.Name()] = b
	}
	return files
}

// On a joined device, koko device add issues for the device's own User: a
// Member adds their own devices, and is refused --user for another User with
// owner_only, which says nothing of whether that User exists. The device the
// Member's code joins acts as the Member too.
func TestAJoinedMemberAddsDevicesForThemselvesOnly(t *testing.T) {
	h := harness.Start(t)
	h.Koko("user", "invite", "bob").WantSuccess(t)
	bob := h.Join("bob")

	jc := deviceAddOn(t, bob, "laptop")
	if jc.User != "bob" || jc.Device != "laptop" {
		t.Errorf("bob's device add issued %+v, want a code for bob's laptop", jc)
	}
	if jc := deviceAddOn(t, bob, "--user", "bob"); jc.User != "bob" {
		t.Errorf("bob's device add --user bob issued for %s", jc.User)
	}
	for _, other := range []string{harness.Owner, "nobody"} {
		r := bob.Koko("device", "add", "--user", other, "--json")
		msg := r.WantError(t, 1, "owner_only")
		if strings.Contains(msg, other) {
			t.Errorf("the refusal for --user %s names it: %q", other, msg)
		}
		if other == harness.Owner {
			harness.Golden(t, "device-add-owner-only.json", r.Stderr)
		}
	}
	laptop := harness.NewDevice(t)
	laptop.Koko("join", jc.Code).WantSuccess(t)
	if again := deviceAddOn(t, laptop); again.User != "bob" {
		t.Errorf("the device bob's code joined issued for %s, want bob", again.User)
	}
}

// The Owner's joined device issues for the Owner, and for any other User
// with --user.
func TestAJoinedOwnerAddsDevicesForAnyUser(t *testing.T) {
	h := harness.Start(t)
	h.Koko("user", "invite", "bob").WantSuccess(t)
	alice := h.Join(harness.Owner)
	if jc := deviceAddOn(t, alice); jc.User != harness.Owner {
		t.Errorf("the Owner's device add issued for %s", jc.User)
	}
	if jc := deviceAddOn(t, alice, "--user", "bob"); jc.User != "bob" {
		t.Errorf("the Owner's device add --user bob issued for %s", jc.User)
	}
}

// A live Device Session holds its name as a valid Join Code does, and
// device-<n> counts from one past the Device Sessions the User has had.
func TestDeviceAddNamesPastLiveDeviceSessions(t *testing.T) {
	h := harness.Start(t)
	h.Advance(15 * time.Minute) // initialisation's code for device-1 expires
	d := harness.NewDevice(t)
	d.Koko("join", deviceAdd(t, h).Code).WantSuccess(t)
	if c := readClientConfig(t, d); c.Device != "device-1" {
		t.Fatalf("the first device joined as %q, want device-1", c.Device)
	}
	h.Koko("device", "add", "device-1", "--json").WantError(t, 1, "name_taken")
	if jc := deviceAdd(t, h); jc.Device != "device-2" {
		t.Errorf("device add after device-1 joined named the device %q, want device-2", jc.Device)
	}
}
