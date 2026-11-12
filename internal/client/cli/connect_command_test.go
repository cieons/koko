package cli_test

import (
	"encoding/json/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/cieons/koko/internal/client/devicemux"
	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/shared/build"
	"github.com/cieons/koko/internal/shared/timing"
)

// connectOut is what koko connect --json prints.
type connectOut struct {
	ServerAddress   string   `json:"server_address"`
	PreviousAddress *string  `json:"previous_address"`
	HostKeys        []string `json:"host_keys"`
	ServerVersion   string   `json:"server_version"`
	DeviceMux       *struct {
		Socket          string    `json:"socket"`
		State           string    `json:"state"`
		Since           string    `json:"since"`
		LastError       *errorOut `json:"last_error"`
		WaitingToResume int       `json:"waiting_to_resume"`
	} `json:"device_mux"`
}

// kokoConnect runs koko connect --json through run with args after it,
// which must succeed, and returns what it printed, parsed and as written
// with the koko server's version replaced by a placeholder, then each of
// the old, new pairs, in order, a path in any of the forms it takes in JSON
// (harness.Paths).
func kokoConnect(t *testing.T, run func(...string) harness.Result, args []string, oldNew ...string) (connectOut, string) {
	t.Helper()
	r := run(append([]string{"connect", "--json"}, args...)...)
	r.WantSuccess(t)
	var c connectOut
	if err := json.Unmarshal([]byte(r.Stdout), &c, json.RejectUnknownMembers(true)); err != nil {
		t.Fatalf("connect --json printed %q: %v", r.Stdout, err)
	}
	out := versionField.ReplaceAllString(r.Stdout, `"$1": "<version>"`)
	return c, harness.Paths(out, oldNew...)
}

// fingerprints are the fingerprints of host keys written as in
// authorized_keys, as the client config holds them.
func fingerprints(t *testing.T, keys []string) []string {
	t.Helper()
	var fps []string
	for _, k := range keys {
		key, _, _, _, err := ssh.ParseAuthorizedKey([]byte(k))
		if err != nil {
			t.Fatal(err)
		}
		fps = append(fps, ssh.FingerprintSHA256(key))
	}
	return fps
}

// muxSocket is the socket of the device's Device Mux of this build.
func muxSocket(d *harness.Device) string {
	return filepath.Join(d.MuxDir(), devicemux.Name(build.Read())+".sock")
}

// koko connect reaches the koko server as any command does, starting the
// Device Mux when none runs, and shows the Server Address, the pinned host
// key it answered with, its version and the Device Mux; with KOKO_DIRECT=1
// it goes direct, and shows no Device Mux.
func TestKokoConnectChecksThePinnedHostKeyThroughTheDeviceMux(t *testing.T) {
	t.Parallel()
	_, _, d := muxDevice(t, timing.Timings{})
	c := readClientConfig(t, d)
	d.Koko("disconnect").WantSuccess(t)
	waitLog(t, d, `msg="device mux stopped" reason="koko disconnect stopped it"`)

	out, raw := kokoConnect(t, d.Koko, nil, c.ServerAddress, "<server address>", muxSocket(d), "<socket>")
	harness.Golden(t, "connect.json", raw)
	if out.ServerAddress != c.ServerAddress || out.PreviousAddress != nil || !slices.Equal(out.HostKeys, fingerprints(t, c.HostKeys)) ||
		out.ServerVersion != build.Read().Version {
		t.Errorf("connect printed %+v", out)
	}
	if m := out.DeviceMux; m == nil || m.Socket != muxSocket(d) || m.State != "connected" || m.LastError != nil {
		t.Errorf("connect shows the Device Mux %+v, want the one it started, connected", deref(out.DeviceMux))
	}
	if n := strings.Count(d.MuxLog(), `msg="device mux started"`); n != 2 {
		t.Errorf("%d Device Muxes started, want koko join's and koko connect's", n)
	}

	r := d.KokoEnv([]string{"KOKO_DIRECT=1"}, "connect", "--json")
	r.WantSuccess(t)
	if !strings.Contains(r.Stdout, `"device_mux": null`) {
		t.Errorf("connect with KOKO_DIRECT=1 printed %s, want device_mux null", r.Stdout)
	}
	r = d.Koko("connect")
	r.WantSuccess(t)
	for _, want := range []string{
		"Server Address:  " + c.ServerAddress + "\n",
		"Host key:        " + fingerprints(t, c.HostKeys)[0] + ", pinned\n",
		"Device Mux:      connected since ",
	} {
		if !strings.Contains(r.Stdout, want) {
			t.Errorf("connect says\n%s\nwith no line %q", r.Stdout, want)
		}
	}
}

// koko connect <host:port> takes the address only once the koko server
// answered there with a pinned host key: a koko server with another host
// key, an address nothing answers at and an address without a port each
// leave the device's address as it was. Once it takes one, commands reach
// the koko server there, the host keys pinned unchanged.
func kokoConnectTakesANewAddressOnlyWhereThePinnedHostKeyAnswers(t *testing.T) {
	_, fwd, listen, init := initThroughWith(t, timing.Timings{})
	d := harness.NewMuxDevice(t, timing.Timings{CommandWait: 300 * time.Millisecond, ReconnectFirst: 20 * time.Millisecond, ReconnectMax: 40 * time.Millisecond})
	d.Koko("join", init.JoinCode.Code).WantSuccess(t)
	old := readClientConfig(t, d)
	knownHosts, err := os.ReadFile(filepath.Join(kokoDir(d), "known_hosts"))
	if err != nil {
		t.Fatal(err)
	}
	impostor := harness.FreeAddr(t)
	harness.Start(t, harness.Listen(impostor))

	for _, tc := range []struct {
		name, address, code string
		status              int
		says                []string
	}{
		{"a koko server with another host key", impostor, "host_key_mismatch", 1, []string{impostor, "this device keeps the address " + old.ServerAddress}},
		{"an address nothing answers at", harness.FreeAddr(t), "server_unreachable", 1, []string{"cannot reach the koko server at", "this device keeps the address " + old.ServerAddress}},
		{"an address without a port", "127.0.0.1", "invalid_usage", 2, []string{"names no port"}},
		{"what is no address", "koko.example.com:https", "invalid_usage", 2, []string{"not an address"}},
	} {
		msg := d.Koko("connect", tc.address, "--json").WantError(t, tc.status, tc.code)
		for _, s := range tc.says {
			if !strings.Contains(msg, s) {
				t.Errorf("for %s, connect says %q, without %q", tc.name, msg, s)
			}
		}
		if c := readClientConfig(t, d); c.ServerAddress != old.ServerAddress {
			t.Errorf("for %s, the device's address became %s", tc.name, c.ServerAddress)
		}
	}

	out, raw := kokoConnect(t, d.Koko, []string{listen}, listen, "<new address>", old.ServerAddress, "<old address>", muxSocket(d), "<socket>")
	harness.Golden(t, "connect-moved.json", raw)
	if out.ServerAddress != listen || out.PreviousAddress == nil || *out.PreviousAddress != old.ServerAddress {
		t.Errorf("connect printed %+v, want %s taken in place of %s", out, listen, old.ServerAddress)
	}
	c := readClientConfig(t, d)
	if c.ServerAddress != listen || !slices.Equal(c.HostKeys, old.HostKeys) {
		t.Errorf("the client config is %+v, want the address %s and the host keys as they were", c, listen)
	}
	if b, err := os.ReadFile(filepath.Join(kokoDir(d), "known_hosts")); err != nil || string(b) != string(knownHosts) {
		t.Errorf("known_hosts changed (%v):\n%s", err, b)
	}
	conns := fwd.Conns()
	d.KokoEnv([]string{"KOKO_DIRECT=1"}, "space", "ls", "--json").WantSuccess(t)
	d.Koko("disconnect").WantSuccess(t)
	d.Koko("space", "ls", "--json").WantSuccess(t)
	if n := fwd.Conns() - conns; n != 0 {
		t.Errorf("%d connection(s) went to the old address after the move, want none", n)
	}
}

// The restore: a copy of a stopped koko server's data directory and the
// same Root Key start a second koko server elsewhere, which keeps every
// record and every Secret, and which a device joined to the first reaches
// with the host key it pinned, once koko connect gives it the new address.
func aRestoredKokoServerKeepsItsDevicesPins(t *testing.T) {
	dataDir, rootKey := t.TempDir(), harness.NewRootKeyFile(t)
	first := harness.Start(t, harness.DataDir(dataDir), harness.RootKeyFile(rootKey))
	d := first.Join(harness.Owner)
	d.UseTimings(timing.Timings{CommandWait: 300 * time.Millisecond})
	d.Koko("space", "add", "ops", "--json").WantSuccess(t)
	const value = "a value only the Root Key opens"
	d.KokoStdin(value, "secret", "add", "ops/db-password").WantSuccess(t)
	pinned := readClientConfig(t, d).HostKeys
	first.Stop()

	restored := filepath.Join(t.TempDir(), "koko")
	if err := os.CopyFS(restored, os.DirFS(dataDir)); err != nil {
		t.Fatalf("copy the data directory: %v", err)
	}
	listen := harness.FreeAddr(t)
	harness.Start(t, harness.DataDir(restored), harness.RootKeyFile(rootKey), harness.NoInit(), harness.Listen(listen))
	msg := d.Koko("space", "ls", "--json").WantError(t, 1, "server_unreachable")
	if !strings.Contains(msg, "koko connect <host:port>") {
		t.Errorf("a device whose koko server moved is told %q, without koko connect <host:port>", msg)
	}

	d.Koko("connect", listen).WantSuccess(t)
	r := d.Koko("space", "ls", "--json")
	r.WantSuccess(t)
	if !strings.Contains(r.Stdout, `"name": "ops"`) {
		t.Errorf("the restored koko server lists %s, without the Space ops", r.Stdout)
	}
	if r := d.Koko("secret", "show", "ops/db-password"); r.Status != 0 || r.Stdout != value {
		t.Errorf("the restored koko server shows the Secret as %q (%d, %s), want %q", r.Stdout, r.Status, r.Stderr, value)
	}
	if c := readClientConfig(t, d); c.ServerAddress != listen || !slices.Equal(c.HostKeys, pinned) {
		t.Errorf("the client config is %+v, want the address %s and the pin from the join", c, listen)
	}
}
