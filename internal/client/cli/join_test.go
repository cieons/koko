package cli_test

import (
	"crypto/sha256"
	"encoding/json/v2"
	"errors"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/harness/sshhost"
	"github.com/cieons/koko/internal/shared/api"
	"github.com/cieons/koko/internal/shared/joincode"
	"github.com/cieons/koko/internal/shared/timing"
)

// joinedOut is what koko join --json prints.
type joinedOut struct {
	ServerAddress string `json:"server_address"`
	DeviceSession struct {
		ID        string `json:"id"`
		User      string `json:"user"`
		Device    string `json:"device"`
		CreatedAt string `json:"created_at"`
	} `json:"device_session"`
}

// clientConfig is the client config a join writes in ~/.ssh/koko/.
type clientConfig struct {
	ServerAddress string   `json:"server_address"`
	HostKeys      []string `json:"host_keys"`
	User          string   `json:"user"`
	Device        string   `json:"device"`
	DeviceSession string   `json:"device_session"`
}

func kokoDir(d *harness.Device) string {
	return filepath.Join(d.Home(), ".ssh", "koko")
}

func readClientConfig(t *testing.T, d *harness.Device) clientConfig {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(kokoDir(d), "client.json"))
	if err != nil {
		t.Fatalf("read the client config: %v", err)
	}
	var c clientConfig
	if err := json.Unmarshal(b, &c); err != nil {
		t.Fatalf("the client config %s does not parse: %v", b, err)
	}
	return c
}

func writeClientConfig(t *testing.T, d *harness.Device, c clientConfig) {
	t.Helper()
	b, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(kokoDir(d), "client.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
}

func decode(t *testing.T, code string) joincode.Code {
	t.Helper()
	c, err := joincode.Decode(code)
	if err != nil {
		t.Fatalf("decode the Join Code: %v", err)
	}
	return c
}

func mode(t *testing.T, path string) fs.FileMode {
	t.Helper()
	fi, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	return fi.Mode()
}

// A Join Code issued over the Unix socket joins a new device. koko join
// prints the Server Address, the User and the device name, and writes the
// device's files: ~/.ssh/koko/ 0700, the private key 0600 in OpenSSH format,
// and the client config with the Server Address, the pinned host keys as a
// list, the User and the device name. From then on the device reaches the
// koko server as that User.
func TestJoinWithACodeIssuedOverTheSocket(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	jc := deviceAdd(t, h, "laptop")
	code := decode(t, jc.Code)
	d := harness.NewDevice(t)

	r := d.Koko("join", jc.Code, "--json")
	r.WantSuccess(t)
	harness.Golden(t, "join.json", strings.ReplaceAll(r.Stdout, code.Address, "<server address>"))
	var out joinedOut
	if err := json.Unmarshal([]byte(r.Stdout), &out); err != nil {
		t.Fatal(err)
	}
	if out.ServerAddress != code.Address || out.DeviceSession.User != harness.Owner || out.DeviceSession.Device != "laptop" {
		t.Errorf("join printed %+v, want %s's device laptop at %s", out, harness.Owner, code.Address)
	}

	dir := kokoDir(d)
	if m := mode(t, dir); !m.IsDir() {
		t.Errorf("%s has mode %v, want a directory", dir, m)
	}
	harness.WantPrivate(t, dir)
	keyPath := filepath.Join(dir, "device_key")
	if m := mode(t, keyPath); !m.IsRegular() {
		t.Errorf("the key has mode %v, want a file", m)
	}
	harness.WantPrivate(t, keyPath)
	pemBytes, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ssh.ParsePrivateKey(pemBytes)
	if !strings.HasPrefix(string(pemBytes), "-----BEGIN OPENSSH PRIVATE KEY-----\n") || err != nil || key.PublicKey().Type() != ssh.KeyAlgoED25519 {
		t.Errorf("the key is not an unencrypted OpenSSH-format ed25519 key (%v):\n%.40s", err, pemBytes)
	}
	harness.WantPrivate(t, filepath.Join(dir, "client.json"))
	c := readClientConfig(t, d)
	if c.ServerAddress != code.Address || c.User != harness.Owner || c.Device != "laptop" || c.DeviceSession != out.DeviceSession.ID {
		t.Errorf("the client config is %+v, want %s's device laptop (%s) at %s", c, harness.Owner, out.DeviceSession.ID, code.Address)
	}
	if len(c.HostKeys) != 1 {
		t.Fatalf("the client config pins %d host keys, want the one the Join Code names", len(c.HostKeys))
	}
	pinned, _, _, _, err := ssh.ParseAuthorizedKey([]byte(c.HostKeys[0]))
	if err != nil || sha256.Sum256(pinned.Marshal()) != code.Fingerprint {
		t.Errorf("the client config pins %q (%v), not the host key the Join Code names", c.HostKeys[0], err)
	}

	// The device now reaches the koko server over its Device Connection.
	d.Koko("space", "ls", "--json").WantSuccess(t)
	if jc := deviceAddOn(t, d); jc.User != harness.Owner {
		t.Errorf("device add on the joined device issued a code for %s, want %s", jc.User, harness.Owner)
	}
}

// For people, koko join says which koko server it joined, as whom.
func TestJoinForPeople(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	jc := deviceAdd(t, h, "laptop")
	r := harness.NewDevice(t).Koko("join", jc.Code)
	r.WantSuccess(t)
	if want := "Joined the koko server at " + decode(t, jc.Code).Address + " as alice's device laptop.\n"; r.Stdout != want {
		t.Errorf("stdout = %q, want %q", r.Stdout, want)
	}
}

// koko join - reads the code from stdin, so it stays out of shell history.
func TestJoinReadsTheCodeFromStdin(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	jc := deviceAdd(t, h, "laptop")
	d := harness.NewDevice(t)
	d.KokoStdin(jc.Code+"\n", "join", "-", "--json").WantSuccess(t)
	if c := readClientConfig(t, d); c.Device != "laptop" {
		t.Errorf("koko join - joined as %q, want laptop", c.Device)
	}
	r := harness.NewDevice(t).Koko("join", "-", "--json")
	if msg := r.WantError(t, 2, "invalid_usage"); !strings.Contains(msg, "the Join Code is missing") {
		t.Errorf("koko join - with empty stdin says %q, want that the Join Code is missing", msg)
	}
}

// deviceAddOn runs koko device add --json on the device d with args, and
// returns the Join Code it issued.
func deviceAddOn(t *testing.T, d *harness.Device, args ...string) joinCodeOut {
	t.Helper()
	r := d.Koko(append([]string{"device", "add", "--json"}, args...)...)
	r.WantSuccess(t)
	var jc joinCodeOut
	if err := json.Unmarshal([]byte(r.Stdout), &jc); err != nil {
		t.Fatalf("device add --json printed %q: %v", r.Stdout, err)
	}
	return jc
}

// The harness's own join: a device of a test User, joined with a code
// issued over the socket.
func TestTheHarnessJoinsADeviceForATestUser(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	h.Koko("user", "invite", "bob").WantSuccess(t)
	d := h.Join("bob")
	if c := readClientConfig(t, d); c.User != "bob" {
		t.Errorf("the harness joined a device of %q, want bob", c.User)
	}
}

// initThrough initialises a fresh koko server listening at a known address
// with a forwarder's address as its Server Address, so its Join Codes lead
// wherever the forwarder points: at first, the koko server itself. It
// returns the harness, the forwarder, the listen address and what
// initialisation returned.
func initThrough(t *testing.T) (*harness.Harness, *harness.Forwarder, string, api.ServerInitResponse) {
	t.Helper()
	return initThroughWith(t, timing.Timings{})
}

// wantNothingLeft fails the test unless the device holds no ~/.ssh, as
// before a join that failed.
func wantNothingLeft(t *testing.T, d *harness.Device) {
	t.Helper()
	if _, err := os.Lstat(filepath.Join(d.Home(), ".ssh")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("a failed join left ~/.ssh behind (%v)", err)
	}
}

// A truncated or damaged code is caught on the device: koko join exits 2
// before it connects, so the koko server sees no connection.
func TestJoinRefusesADamagedCodeBeforeConnecting(t *testing.T) {
	t.Parallel()
	_, fwd, _, init := initThrough(t)
	code := init.JoinCode.Code
	flipped := []byte(code)
	flipped[30] = map[bool]byte{true: 'b', false: 'a'}[flipped[30] == 'a']
	d := harness.NewDevice(t)
	for name, damaged := range map[string]string{
		"cut short":             code[:len(code)-3],
		"one character changed": string(flipped),
		"in uppercase":          strings.ToUpper(code),
		"without its prefix":    strings.TrimPrefix(code, joincode.Prefix),
	} {
		r := d.Koko("join", damaged, "--json")
		if msg := r.WantError(t, 2, "invalid_usage"); !strings.Contains(msg, "cannot read the Join Code") {
			t.Errorf("join with a code %s says %q", name, msg)
		}
		if name == "cut short" {
			harness.Golden(t, "join-damaged.json", r.Stderr)
		}
	}
	if n := fwd.Conns(); n != 0 {
		t.Errorf("the refusals made %d connection(s) to the koko server, want none", n)
	}
	wantNothingLeft(t, d)
	d.Koko("join", code).WantSuccess(t)
	if n := fwd.Conns(); n != 1 {
		t.Errorf("the join made %d connection(s), want 1", n)
	}
}

// A koko server whose host key is not the one the Join Code names is
// refused with host_key_mismatch, naming both keys, before anything is sent;
// so the same code then joins the right koko server.
func TestJoinRefusesAnotherHostKeyAndTheCodeStaysUsable(t *testing.T) {
	t.Parallel()
	_, fwd, listen, init := initThrough(t)
	impostor := sshhost.Start(t)
	fwd.To(net.JoinHostPort("127.0.0.1", strconv.Itoa(impostor.Port())))
	d := harness.NewDevice(t)

	r := d.Koko("join", init.JoinCode.Code, "--json")
	msg := r.WantError(t, 1, "host_key_mismatch")
	if offered := ssh.FingerprintSHA256(impostor.HostKey()); !strings.Contains(msg, offered) || !strings.Contains(msg, init.HostKeyFingerprint) {
		t.Errorf("message %q names neither the key offered, %s, nor the one the code names, %s", msg, offered, init.HostKeyFingerprint)
	}
	if logins := impostor.Logins(); len(logins) != 0 {
		t.Errorf("koko join sent the impostor %d login attempt(s), want none: %+v", len(logins), logins)
	}
	wantNothingLeft(t, d)

	fwd.To(listen)
	d.Koko("join", init.JoinCode.Code).WantSuccess(t)
}

// A join whose connection ends in SSH authentication, here at
// timing.CommandWait while the koko server has not answered, is a koko
// server koko could not reach (server_unreachable), never one that refused
// the device or its code (#163). Cut before koko sent the Join Code's
// secret, the code stays usable; cut after, before the koko server answered
// it, the koko server may have taken it, and koko says so (#167).
func TestAJoinCutInAuthenticationIsNoRefusal(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		secret bool
		says   string
	}{
		{"before the secret", false, "; the code stays usable"},
		{"after the secret", true, "; koko had sent the code when the connection ended, so the koko server may have taken it: try the code again, and should the koko server refuse it, ask for a new one and revoke the Device Session this join may have left, which koko device ls lists"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			d := harness.NewDevice(t)
			d.UseTimings(timing.Timings{CommandWait: 200 * time.Millisecond})
			// The short wait counts from the join's start, so on a loaded
			// machine it may end in a step the fake answers, before its
			// stall: such a join starts again, with a fake of its own, so
			// that a stall an earlier one reaches late is not counted.
			var address, msg string
			eventually(t, "a join reaches the fake koko server's stall", func() bool {
				var code string
				var stalled func() bool
				address, code, stalled = stallingJoin(t, tc.secret)
				msg = d.Koko("join", code, "--json").WantError(t, 1, "server_unreachable")
				return stalled()
			})
			if !strings.HasPrefix(msg, "cannot reach the koko server at "+address) || !strings.HasSuffix(msg, tc.says) {
				t.Errorf("a join cut in authentication %s says %q, want that koko cannot reach the koko server at %s, ending %q", tc.name, msg, address, tc.says)
			}
			wantNothingLeft(t, d)
		})
	}
}

// stallingJoin starts a fake koko server that never answers a join's
// authentication: the device key's signature, or, once it has asked for the
// Join Code's secret as a koko server does for a new key, when secret, the
// secret. It returns its address, a Join Code for it, and whether a join
// has reached the stall.
func stallingJoin(t *testing.T, secret bool) (address, code string, stalled func() bool) {
	t.Helper()
	var reached atomic.Bool
	stall := func() error {
		reached.Store(true)
		<-t.Context().Done()
		return errors.New("the test ended")
	}
	hostKey := sshhost.NewKey(t, ssh.KeyAlgoED25519)
	cfg := &ssh.ServerConfig{
		PublicKeyCallback: func(ssh.ConnMetadata, ssh.PublicKey) (*ssh.Permissions, error) {
			return nil, nil
		},
		VerifiedPublicKeyCallback: func(ssh.ConnMetadata, ssh.PublicKey, *ssh.Permissions, string) (*ssh.Permissions, error) {
			if !secret {
				return nil, stall()
			}
			return nil, &ssh.PartialSuccessError{Next: ssh.ServerAuthCallbacks{
				PasswordCallback: func(ssh.ConnMetadata, []byte) (*ssh.Permissions, error) {
					return nil, stall()
				},
			}}
		},
	}
	cfg.AddHostKey(hostKey)
	var lc net.ListenConfig
	ln, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	// The test's context ends before its cleanups run, which frees the
	// stalled callback.
	var wg sync.WaitGroup
	t.Cleanup(func() {
		_ = ln.Close()
		wg.Wait()
	})
	wg.Go(func() {
		for {
			nc, err := ln.Accept()
			if err != nil {
				return
			}
			wg.Go(func() {
				_, _, _, _ = ssh.NewServerConn(nc, cfg)
				_ = nc.Close()
			})
		}
	})
	address = ln.Addr().String()
	code, err = joincode.Code{Address: address, Fingerprint: sha256.Sum256(hostKey.PublicKey().Marshal())}.Encode()
	if err != nil {
		t.Fatal(err)
	}
	return address, code, reached.Load
}

// A used code, an expired one (the clock moved past its 15 minutes) and one
// never issued all get the same answer, the koko server's banner, so trying
// codes teaches nothing; each failed join leaves the device as it was. A
// code joins until its last second.
func TestJoinRefusesAUsedAnExpiredAndAWrongCodeAlike(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	expired := deviceAdd(t, h, "desktop").Code
	lastSecond := deviceAdd(t, h, "tablet").Code
	h.Advance(15*time.Minute - time.Second)
	harness.NewDevice(t).Koko("join", lastSecond).WantSuccess(t)
	h.Advance(time.Second)

	used := deviceAdd(t, h, "laptop").Code
	harness.NewDevice(t).Koko("join", used).WantSuccess(t)
	wrong := decode(t, deviceAdd(t, h, "phone").Code)
	wrong.Secret[0] ^= 1
	forged, err := wrong.Encode()
	if err != nil {
		t.Fatal(err)
	}

	for name, code := range map[string]string{"used": used, "expired": expired, "never issued": forged} {
		d := harness.NewDevice(t)
		r := d.Koko("join", code, "--json")
		if msg := r.WantError(t, 1, "invalid_join_code"); msg != "the Join Code is invalid or expired." {
			t.Errorf("join with a code %s says %q, want only that the code is invalid or expired", name, msg)
		}
		if name == "expired" {
			harness.Golden(t, "join-invalid.json", r.Stderr)
		}
		wantNothingLeft(t, d)
	}
}

// A joined device refuses a second koko join and says to revoke its Device
// Session first. It sends nothing, so the code still joins another device.
func TestAJoinedDeviceRefusesASecondJoin(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	d := h.Join(harness.Owner)
	before := readClientConfig(t, d)
	jc := deviceAdd(t, h, "second")
	msg := d.Koko("join", jc.Code, "--json").WantError(t, 1, "already_joined")
	if !strings.Contains(msg, "revoke this device's Device Session first") || !strings.Contains(msg, before.Device) {
		t.Errorf("message %q does not say to revoke this device's Device Session (%s) first", msg, before.Device)
	}
	if after := readClientConfig(t, d); after.DeviceSession != before.DeviceSession {
		t.Errorf("the refused join changed the client config from %+v to %+v", before, after)
	}
	other := harness.NewDevice(t)
	other.Koko("join", jc.Code).WantSuccess(t)
	if c := readClientConfig(t, other); c.Device != "second" {
		t.Errorf("the code joined %q, want second", c.Device)
	}
}

// Joins and failed joins reach the koko server's log: who joined, as which
// Device Session, added by the Unix socket or by the Device Session that
// issued the code, and from where; and why a join failed. Neither holds the
// code (TestNoJoinCodeIsStoredOrLogged).
func TestJoinsAndFailedJoinsAreLogged(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	jc := deviceAdd(t, h, "laptop")
	var first joinedOut
	r := harness.NewDevice(t).Koko("join", jc.Code, "--json")
	r.WantSuccess(t)
	if err := json.Unmarshal([]byte(r.Stdout), &first); err != nil {
		t.Fatal(err)
	}
	harness.NewDevice(t).Koko("join", jc.Code, "--json").WantError(t, 1, "invalid_join_code")
	laptop := harness.NewDevice(t)
	// The laptop, joined, issues the code a phone joins with.
	phone := harness.NewDevice(t)
	laptop.Koko("join", deviceAdd(t, h, "spare").Code).WantSuccess(t)
	phone.Koko("join", deviceAddOn(t, laptop, "phone").Code).WantSuccess(t)
	spare := readClientConfig(t, laptop).DeviceSession

	for _, line := range []string{
		`level=INFO msg="device joined" user=alice device=laptop device_session=` + first.DeviceSession.ID +
			` added_by="unix socket" join_code=joincode_[0-9a-z]{8} addr=127\.0\.0\.1:\d+`,
		`level=INFO msg="join failed" addr=127\.0\.0\.1:\d+ reason="the Join Code was used" join_code=joincode_[0-9a-z]{8}`,
		`level=INFO msg="device joined" user=alice device=phone device_session=device_[0-9a-z]{8} added_by=` + spare,
	} {
		if !regexp.MustCompile(line).MatchString(h.Log()) {
			t.Errorf("the log holds no line matching %s:\n%s", line, h.Log())
		}
	}
}
