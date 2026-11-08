package cli_test

import (
	"encoding/json/v2"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/cieons/koko/internal/client/devicemux"
	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/shared/build"
	"github.com/cieons/koko/internal/shared/timing"
)

// deviceSessionRef is a Device Session as koko status names one, and as
// device ls names the one that added another.
type deviceSessionRef struct {
	ID     string `json:"id"`
	User   string `json:"user"`
	Device string `json:"device"`
}

// statusOut is what koko status --json prints.
type statusOut struct {
	ServerAddress *string           `json:"server_address"`
	DeviceSession *deviceSessionRef `json:"device_session"`
	Socket        *string           `json:"socket"`
	CLIVersion    string            `json:"cli_version"`
	ServerVersion *string           `json:"server_version"`
	ServerError   *errorOut         `json:"server_error"`
	DeviceMux     *struct {
		Socket          string    `json:"socket"`
		State           string    `json:"state"`
		Since           string    `json:"since"`
		LastError       *errorOut `json:"last_error"`
		WaitingToResume int       `json:"waiting_to_resume"`
	} `json:"device_mux"`
	Revocation *struct {
		ServerAddress string           `json:"server_address"`
		DeviceSession deviceSessionRef `json:"device_session"`
		LearnedAt     string           `json:"learned_at"`
	} `json:"revocation"`
	IncludeWarning *string `json:"include_warning"`
}

// errorOut is an error object as koko status prints one inside its result.
type errorOut struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// versionField matches a version koko status prints, which depends on the
// build.
var versionField = regexp.MustCompile(`"(cli_version|server_version)": "[^"]*"`)

// status runs koko status --json through run, which must succeed, and
// returns what it printed, parsed and as written with each version replaced
// by a placeholder, then each of the old, new pairs, in order.
func status(t *testing.T, run func(...string) harness.Result, oldNew ...string) (statusOut, string) {
	t.Helper()
	r := run("status", "--json")
	r.WantSuccess(t)
	var s statusOut
	if err := json.Unmarshal([]byte(r.Stdout), &s, json.RejectUnknownMembers(true)); err != nil {
		t.Fatalf("status --json printed %q: %v", r.Stdout, err)
	}
	out := versionField.ReplaceAllString(r.Stdout, `"$1": "<version>"`)
	return s, strings.NewReplacer(oldNew...).Replace(out)
}

func deref[T any](p *T) any {
	if p == nil {
		return nil
	}
	return *p
}

// On a joined device, koko status shows the Server Address, the device's
// Device Session (its id, User and device name), this CLI's version and the
// koko server's, as it reported it on connecting; there is nothing to warn
// about. It makes no API call.
func TestStatusOnAJoinedDevice(t *testing.T) {
	h := harness.Start(t)
	d := h.Join(harness.Owner)
	c := readClientConfig(t, d)
	calls := h.Calls()

	s, out := status(t, d.Koko, c.ServerAddress, "<server address>")
	harness.Golden(t, "status.json", out)
	if want := (deviceSessionRef{ID: c.DeviceSession, User: harness.Owner, Device: c.Device}); deref(s.ServerAddress) != c.ServerAddress || deref(s.DeviceSession) != want {
		t.Errorf("status = %+v, want the join of %+v", s, c)
	}
	if v := build.Read().Version; s.CLIVersion != v || deref(s.ServerVersion) != v {
		t.Errorf("status shows the versions %q and %v, want %q for both, the koko server running in this test", s.CLIVersion, deref(s.ServerVersion), v)
	}
	if n := h.Calls() - calls; n != 0 {
		t.Errorf("status made %d API call(s), want none", n)
	}

	r := d.Koko("status")
	r.WantSuccess(t)
	for _, want := range []string{
		"Server Address:  " + c.ServerAddress + "\n",
		"User:            alice\n",
		"Device:          " + c.Device + " (" + c.DeviceSession + ")\n",
		"koko server:     koko " + s.CLIVersion + "\n",
		"This CLI:        koko " + s.CLIVersion + "\n",
	} {
		if !strings.Contains(r.Stdout, want) {
			t.Errorf("status says\n%s\nwith no line %q", r.Stdout, want)
		}
	}
}

// On the koko server's own machine, commands reach it over its Unix socket
// as the Local caller, and koko status says so; nothing has joined there.
func TestStatusOverTheSocket(t *testing.T) {
	h := harness.Start(t)
	s, out := status(t, h.Koko, h.Socket(), "<socket>")
	harness.Golden(t, "status-socket.json", out)
	if deref(s.Socket) != h.Socket() || s.ServerVersion == nil || s.ServerAddress != nil || s.DeviceSession != nil {
		t.Errorf("status over the socket = %+v", s)
	}
	r := h.Koko("status")
	r.WantSuccess(t)
	if want := "Unix socket:     " + h.Socket() + ", as the Local caller\n"; !strings.Contains(r.Stdout, want) {
		t.Errorf("status says\n%s\nwith no line %q", r.Stdout, want)
	}
}

// A device that has not joined is told so, with the code any command would
// fail with; koko status itself succeeds, since saying so is its purpose.
func TestStatusOnADeviceThatHasNotJoined(t *testing.T) {
	d := harness.NewDevice(t)
	s, out := status(t, d.Koko)
	harness.Golden(t, "status-not-joined.json", regexp.MustCompile(`\(nothing at [^)]*\)`).ReplaceAllString(out, "(nothing at <socket>)"))
	if s.ServerError == nil || s.ServerError.Code != "not_joined" || s.ServerAddress != nil || s.ServerVersion != nil || s.IncludeWarning != nil {
		t.Errorf("status on a device that has not joined = %+v", s)
	}
}

// A device whose Device Session was Revoked learns it from koko status
// itself, which then shows the revocation record and no join.
func TestStatusShowsTheRevocationRecord(t *testing.T) {
	h := harness.Start(t)
	h.Join(harness.Owner)
	laptop, laptopID := joinAs(t, deviceAdd(t, h, "laptop"))
	server := readClientConfig(t, laptop).ServerAddress
	deviceRevoke(t, h.Koko, "laptop")

	s, out := status(t, laptop.Koko, server, "<server address>")
	harness.Golden(t, "status-revoked.json", out)
	if s.ServerError == nil || s.ServerError.Code != "revoked" || s.ServerAddress != nil || s.DeviceSession != nil || s.Revocation == nil {
		t.Fatalf("status on a revoked device = %+v", s)
	}
	rec := readRevocation(t, laptop)
	if got := *s.Revocation; got.ServerAddress != server || got.LearnedAt != rec.LearnedAt ||
		got.DeviceSession != (deviceSessionRef{ID: laptopID, User: harness.Owner, Device: "laptop"}) {
		t.Errorf("status shows the record %+v, want %+v", got, rec)
	}
}

// A koko server that does not answer is shown as such, with the device's
// join; koko status still succeeds.
func TestStatusWhenTheKokoServerIsUnreachable(t *testing.T) {
	h := harness.Start(t)
	d := h.Join(harness.Owner)
	d.UseTimings(timing.Timings{CommandWait: 200 * time.Millisecond})
	h.Stop()
	s, _ := status(t, d.Koko)
	if s.ServerError == nil || s.ServerError.Code != "server_unreachable" || s.ServerVersion != nil || s.ServerAddress == nil {
		t.Errorf("status with the koko server stopped = %+v", s)
	}
}

// On a device whose commands go through the Device Mux, koko status shows
// it: its socket, and connected since it opened its Device Connection. Once
// the koko server stops, the Device Mux is reconnecting since the drop, and
// its last error is what status's own connection failed with.
func TestStatusShowsTheDeviceMux(t *testing.T) {
	h, _, d := muxDevice(t, timing.Timings{CommandWait: 200 * time.Millisecond})
	c := readClientConfig(t, d)
	socket := filepath.Join(d.RuntimeDir(), "koko", devicemux.Name(build.Read())+".sock")
	s, out := status(t, d.Koko, c.ServerAddress, "<server address>", socket, "<socket>")
	harness.Golden(t, "status-device-mux.json", out)
	if m := s.DeviceMux; m == nil || m.Socket != socket || m.State != "connected" || m.LastError != nil || s.ServerVersion == nil {
		t.Fatalf("status through the Device Mux = %+v, device_mux %+v", s, deref(s.DeviceMux))
	}
	connected := s.DeviceMux.Since
	r := d.Koko("status")
	r.WantSuccess(t)
	if want := "Device Mux:      connected since " + connected + " (" + socket + ")\n"; !strings.Contains(r.Stdout, want) {
		t.Errorf("status says\n%s\nwith no line %q", r.Stdout, want)
	}

	h.Stop()
	waitLog(t, d, `msg="device connection dropped"`)
	s, _ = status(t, d.Koko)
	m := s.DeviceMux
	if m == nil || m.State != "reconnecting" || m.LastError == nil || s.ServerError == nil || *m.LastError != *s.ServerError || m.LastError.Code != "server_unreachable" {
		t.Fatalf("status with the koko server stopped = %+v, device_mux %+v", s, deref(m))
	}
	if m.Since < connected {
		t.Errorf("the Device Mux is reconnecting since %s, before it connected at %s", m.Since, connected)
	}
	again, _ := status(t, d.Koko)
	if again.DeviceMux == nil || again.DeviceMux.Since != m.Since {
		t.Errorf("a second try left the Device Mux reconnecting since %v, want still %s", deref(again.DeviceMux), m.Since)
	}
	r = d.Koko("status")
	r.WantSuccess(t)
	// Each try's error names the local port it tried from.
	if want := "Device Mux:      reconnecting since " + m.Since + " (" + socket + "): cannot reach the koko server at " + c.ServerAddress; !strings.Contains(r.Stdout, want) {
		t.Errorf("status says\n%s\nwith no line %q", r.Stdout, want)
	}
}

// Without a Device Mux, on the direct path, koko status shows none.
func TestStatusShowsNoDeviceMuxOnTheDirectPath(t *testing.T) {
	_, _, d := muxDevice(t, timing.Timings{})
	r := d.KokoEnv([]string{"KOKO_DIRECT=1"}, "status", "--json")
	r.WantSuccess(t)
	if !strings.Contains(r.Stdout, `"device_mux": null`) {
		t.Errorf("status with KOKO_DIRECT=1 printed %s, want device_mux null", r.Stdout)
	}
}

// koko status warns when ~/.ssh/config does not include koko's ssh_config,
// or includes it below a Host or Match line, where ssh reads it only for
// that block's hosts; a file it cannot read is reported as such.
func TestStatusWarnsWhenSSHDoesNotReadKokosSSHConfig(t *testing.T) {
	h := harness.Start(t)
	d := h.Join(harness.Owner)
	const missing = `~/.ssh/config does not include koko's ssh_config, so ssh does not reach Hosts through koko: add the line "` + includeLine + `" at its top`
	for _, c := range []struct {
		config string
		want   string
	}{
		{includeLine + "\nHost github.com\n  User git\n", ""},
		{"# mine\n\nInclude ~/.ssh/koko/ssh_config\nMatch host *.example\n", ""},
		{`Include "` + d.Home() + `/.ssh/koko/ssh_config"` + "\n", ""},
		{"Include ~/.ssh/koko/*\n", ""},
		{"Host github.com\n  User git\n", missing},
		{"", missing},
		{"Host github.com\n  User git\n" + includeLine + "\n", `~/.ssh/config includes koko's ssh_config on line 3, below line 1 ("Host github.com"), so ssh reads it only for the hosts that line matches: move "` + includeLine + `" to the top`},
		{"  match exec true\n" + includeLine + "\n", `~/.ssh/config includes koko's ssh_config on line 2, below line 1 ("match exec true"), so ssh reads it only for the hosts that line matches: move "` + includeLine + `" to the top`},
	} {
		if err := os.WriteFile(userSSHConfig(d), []byte(c.config), 0o600); err != nil {
			t.Fatal(err)
		}
		s, _ := status(t, d.Koko)
		if got := deref(s.IncludeWarning); c.want == "" && got != nil || c.want != "" && got != c.want {
			t.Errorf("with ~/.ssh/config %q, status warns %v, want %q", c.config, got, c.want)
		}
	}

	if err := os.Remove(userSSHConfig(d)); err != nil {
		t.Fatal(err)
	}
	s, _ := status(t, d.Koko)
	if deref(s.IncludeWarning) != missing {
		t.Errorf("with no ~/.ssh/config, status warns %v, want %q", deref(s.IncludeWarning), missing)
	}
	if err := os.Mkdir(userSSHConfig(d), 0o700); err != nil {
		t.Fatal(err)
	}
	const notRegular = "~/.ssh/config is not a regular file, so koko cannot tell whether ssh reaches Hosts through koko"
	s, _ = status(t, d.Koko)
	if deref(s.IncludeWarning) != notRegular {
		t.Errorf("with a directory for ~/.ssh/config, status warns %v, want %q", deref(s.IncludeWarning), notRegular)
	}

	r := d.Koko("status")
	r.WantSuccess(t)
	if !strings.HasSuffix(r.Stdout, "\nWarning: "+notRegular+".\n") {
		t.Errorf("status says\n%s\nwithout the warning", r.Stdout)
	}
}
