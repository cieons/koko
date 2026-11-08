package cli_test

import (
	"encoding/json/v2"
	"errors"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/shared/api"
	"github.com/cieons/koko/internal/shared/timing"
)

// leftOut is what koko leave --json prints.
type leftOut struct {
	ServerAddress string `json:"server_address"`
	DeviceSession struct {
		ID     string `json:"id"`
		User   string `json:"user"`
		Device string `json:"device"`
	} `json:"device_session"`
}

// userSSHFiles reads every file under the device's ~/.ssh but koko's own
// directory, by path.
func userSSHFiles(t *testing.T, d *harness.Device) map[string]string {
	t.Helper()
	files := map[string]string{}
	root := filepath.Join(d.Home(), ".ssh")
	err := filepath.WalkDir(root, func(path string, e fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return err
		case path == kokoDir(d):
			return filepath.SkipDir
		case e.IsDir():
			return nil
		}
		b, err := os.ReadFile(path)
		files[path] = string(b)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// S1: the koko server a device joined is replaced by one initialised anew,
// with a new Root Key and host key, at the same address. koko join refuses
// on the device, naming both ways out; koko leave stops the Device Mux and
// forgets the old koko server without calling either, keeping the user's
// own ~/.ssh files byte for byte; then the device joins the new koko server
// with its Join Code.
func TestKokoLeaveLetsADeviceJoinAKokoServerInitialisedAnew(t *testing.T) {
	t.Parallel()
	old, fwd, _, _ := initThrough(t)
	old.Koko("host", "add", "web1", "address=203.0.113.10", "account=deploy").WantSuccess(t)
	jc := deviceAdd(t, old, "laptop")
	d := harness.NewMuxDevice(t, timing.Timings{})
	d.Koko("join", jc.Code).WantSuccess(t)
	waitLog(t, d, `msg="refreshed the bare names"`)
	if b, err := os.ReadFile(filepath.Join(kokoDir(d), "ssh_config")); err != nil || !strings.Contains(string(b), "web1") {
		t.Fatalf("after the join koko's ssh_config is %q (%v), want web1 among its bare names", b, err)
	}
	// The user's own files beside koko's, and a revocation record a join
	// failed to clear.
	if err := os.WriteFile(filepath.Join(d.Home(), ".ssh", "known_hosts"), []byte("example.com ssh-ed25519 AAAA\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(kokoDir(d), "revoked.json"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	mine := userSSHFiles(t, d)
	if !strings.Contains(mine[filepath.Join(d.Home(), ".ssh", "config")], "Include ~/.ssh/koko/ssh_config") {
		t.Fatalf("the join wrote no Include line: %v", mine)
	}

	// A koko server initialised anew takes the old one's place.
	listen := harness.FreeAddr(t)
	fresh := harness.Start(t, harness.NoInit(), harness.Listen(listen))
	init, err := fresh.Call(api.ServerInit, api.ServerInitRequest{Owner: harness.Owner, Address: fwd.Addr()})
	if err != nil {
		t.Fatalf("server.init: %v", err)
	}
	fwd.To(listen)
	old.Stop()
	// The Device Mux tries the new koko server at once after the drop, and
	// stops trying at its host key; only then do the counts below hold
	// still.
	waitLog(t, d, `msg="cannot open a Device Connection"`)

	msg := d.Koko("join", init.JoinCode.Code, "--json").WantError(t, 1, "already_joined")
	for _, want := range []string{"revoke this device's Device Session first (koko device revoke laptop)", "if that koko server no longer knows this device, run koko leave"} {
		if !strings.Contains(msg, want) {
			t.Errorf("join on the joined device says %q, without %q", msg, want)
		}
	}

	conns, calls := fwd.Conns(), fresh.Calls()
	r := d.Koko("leave", "--json")
	r.WantSuccess(t)
	harness.Golden(t, "leave.json", strings.ReplaceAll(r.Stdout, fwd.Addr(), "<server address>"))
	var out leftOut
	if err := json.Unmarshal([]byte(r.Stdout), &out, json.RejectUnknownMembers(true)); err != nil {
		t.Fatalf("leave --json printed %q: %v", r.Stdout, err)
	}
	if out.ServerAddress != fwd.Addr() || out.DeviceSession.User != harness.Owner || out.DeviceSession.Device != "laptop" || !strings.HasPrefix(out.DeviceSession.ID, "device_") {
		t.Errorf("leave printed %+v, want alice's laptop at %s", out, fwd.Addr())
	}
	if n, m := fwd.Conns()-conns, fresh.Calls()-calls; n != 0 || m != 0 {
		t.Errorf("leave made %d connection(s) and %d call(s) to the koko server, want none", n, m)
	}
	waitLog(t, d, `msg="device mux stopped" reason="koko disconnect stopped it"`)
	for _, f := range []string{"client.json", "device_key", "known_hosts", "revoked.json"} {
		if _, err := os.Lstat(filepath.Join(kokoDir(d), f)); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("leave left koko's %s (%v)", f, err)
		}
	}
	if b, err := os.ReadFile(filepath.Join(kokoDir(d), "ssh_config")); err != nil || strings.Contains(string(b), "web1") || !strings.Contains(string(b), "Host *.koko") {
		t.Errorf("after leave koko's ssh_config is %q (%v), want *.koko without bare names", b, err)
	}
	if after := userSSHFiles(t, d); !maps.Equal(after, mine) {
		t.Errorf("leave changed the user's own ~/.ssh files from %v to %v", mine, after)
	}

	d.Koko("join", init.JoinCode.Code).WantSuccess(t)
	if c := readClientConfig(t, d); c.ServerAddress != fwd.Addr() || c.Device != "device-1" {
		t.Errorf("the join after leave wrote %+v, want the new koko server's device-1", c)
	}
	d.Koko("space", "ls", "--json").WantSuccess(t)
}

// koko leave says which Device Session it left behind and how to revoke it
// while its koko server still runs; that koko server still lists it, and
// revoking it on the koko server's machine works.
func TestKokoLeaveNamesTheDeviceSessionLeftBehind(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	d := h.Join(harness.Owner)
	c := readClientConfig(t, d)
	r := d.Koko("leave")
	r.WantSuccess(t)
	name := c.User + "/" + c.Device
	want := "Left the koko server at " + c.ServerAddress + ". Its Device Session " + name + " (" + c.DeviceSession + ") stays there, but no device can use it any more, since koko deleted its key: if that koko server still runs, revoke it from another device or on the koko server's machine with koko device revoke " + name + ".\n"
	if r.Stdout != want {
		t.Errorf("leave says %q, want %q", r.Stdout, want)
	}
	// It is the Owner's last live Device Session, hence --last.
	h.Koko("device", "revoke", name, "--last").WantSuccess(t)
}

// On a device that has not joined, koko leave says so and exits 1.
func TestKokoLeaveOnADeviceThatHasNotJoined(t *testing.T) {
	t.Parallel()
	d := harness.NewDevice(t)
	r := d.Koko("leave", "--json")
	if msg := r.WantError(t, 1, "not_joined"); !strings.Contains(msg, "this device has not joined a koko server") {
		t.Errorf("leave on a device that has not joined says %q", msg)
	}
	harness.Golden(t, "leave-not-joined.json", r.Stderr)
}

// A Device Mux that does not stop could write into the device's files
// afterwards, so koko leave then deletes none of them, and says so.
func TestKokoLeaveKeepsTheJoinWhenADeviceMuxDoesNotStop(t *testing.T) {
	t.Parallel()
	_, _, d := muxDevice(t, timing.Timings{})
	before := readClientConfig(t, d)
	stubborn := filepath.Join(filepath.Dir(muxSocket(d)), "a0.0.0-older.sock")
	answerElsewhere(t, stubborn)

	msg := d.Koko("leave", "--json").WantError(t, 1, "internal_error")
	for _, want := range []string{"koko leave could not stop every Device Mux", stubborn + " did not stop", "this device is still joined to the koko server at " + before.ServerAddress + ", and koko deleted none of its files"} {
		if !strings.Contains(msg, want) {
			t.Errorf("leave says %q, without %q", msg, want)
		}
	}
	if after := readClientConfig(t, d); !reflect.DeepEqual(after, before) {
		t.Errorf("the failed leave changed the client config from %+v to %+v", before, after)
	}
	d.Koko("space", "ls", "--json").WantSuccess(t)
}

// koko leave sits in the irreversible tier, so help and koko skill list it
// there.
func TestKokoLeaveIsIrreversible(t *testing.T) {
	t.Parallel()
	r := harness.NewDevice(t).Koko("help", "leave", "--json")
	r.WantSuccess(t)
	var commands []struct {
		Command string `json:"command"`
		Tier    string `json:"tier"`
	}
	if err := json.Unmarshal([]byte(r.Stdout), &commands); err != nil {
		t.Fatalf("help leave --json printed %q: %v", r.Stdout, err)
	}
	if len(commands) != 1 || commands[0].Tier != "irreversible" {
		t.Errorf("help leave lists %+v, want koko leave in the irreversible tier", commands)
	}
}
