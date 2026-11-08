package cli_test

import (
	"bytes"
	"encoding/json/v2"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/cieons/koko/internal/client/cli"
	"github.com/cieons/koko/internal/harness"
)

// includeLine is the line koko puts first in ~/.ssh/config.
const includeLine = "Include ~/.ssh/koko/ssh_config"

// userSSHConfig is the device's own ~/.ssh/config.
func userSSHConfig(d *harness.Device) string {
	return filepath.Join(d.Home(), ".ssh", "config")
}

func readText(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// kokoSSHConfig returns koko's own ssh_config on the device, with the koko
// binary's path and the device's HOME replaced by placeholders, as the
// golden files hold it.
func kokoSSHConfig(t *testing.T, d *harness.Device) string {
	t.Helper()
	koko, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	b := readText(t, filepath.Join(kokoDir(d), "ssh_config"))
	return strings.NewReplacer(koko, "<koko>", d.Home(), "<home>").Replace(b)
}

// A join sets up ssh: it writes koko's own ssh_config (0600: the Host *.koko
// block whose ProxyCommand runs this koko) and its known_hosts (0600: the
// koko server's host key under the alias koko, the key the join pinned),
// and makes ~/.ssh/config, absent before, 0600 with the Include line alone.
// ~/.ssh/koko/ holds no file besides koko's, so nothing was left half
// written.
func TestJoinSetsUpSSH(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	d := harness.NewDevice(t)
	r := d.Koko("join", deviceAdd(t, h, "laptop").Code, "--json")
	r.WantSuccess(t)

	if got := readText(t, userSSHConfig(d)); got != includeLine+"\n" {
		t.Errorf("~/.ssh/config = %q, want the Include line alone", got)
	}
	for _, f := range []string{userSSHConfig(d), filepath.Join(kokoDir(d), "ssh_config"), filepath.Join(kokoDir(d), "known_hosts")} {
		if m := mode(t, f); !m.IsRegular() || m.Perm() != 0o600 {
			t.Errorf("%s has mode %v, want a file with 0600", f, m)
		}
	}
	harness.Golden(t, "join-ssh_config", kokoSSHConfig(t, d))

	pinned := readClientConfig(t, d).HostKeys
	marker, hosts, key, _, rest, err := ssh.ParseKnownHosts([]byte(readText(t, filepath.Join(kokoDir(d), "known_hosts"))))
	if err != nil || marker != "" || !slices.Equal(hosts, []string{"koko"}) || len(rest) != 0 ||
		string(bytes.TrimSpace(ssh.MarshalAuthorizedKey(key))) != pinned[0] {
		t.Errorf("known_hosts holds %v %v (%v, %d bytes more), want the one line koko %s", hosts, key, err, len(rest), pinned[0])
	}

	entries, err := os.ReadDir(kokoDir(d))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if want := []string{"client.json", "device_key", "known_hosts", "ssh_config"}; !slices.Equal(names, want) {
		t.Errorf("~/.ssh/koko holds %v, want %v", names, want)
	}

	var out map[string]any
	if err := json.Unmarshal([]byte(r.Stdout), &out); err != nil {
		t.Fatal(err)
	}
	if w, ok := out["include_warning"]; !ok || w != nil {
		t.Errorf("join --json has include_warning %v (present: %v), want null", w, ok)
	}
}

// joinWith joins a new device of the Owner, after prepare has set up its
// HOME, with args after the Join Code, and returns the device and what
// join printed.
func joinWith(t *testing.T, h *harness.Harness, prepare func(d *harness.Device), args ...string) (*harness.Device, harness.Result) {
	t.Helper()
	d := harness.NewDevice(t)
	if err := os.MkdirAll(filepath.Join(d.Home(), ".ssh"), 0o700); err != nil {
		t.Fatal(err)
	}
	prepare(d)
	r := d.Koko(append([]string{"join", deviceAdd(t, h).Code}, args...)...)
	r.WantSuccess(t)
	return d, r
}

// includeWarning returns the include_warning join or status printed with
// --json, or nil for null.
func includeWarning(t *testing.T, r harness.Result) *string {
	t.Helper()
	var out struct {
		IncludeWarning *string `json:"include_warning"`
	}
	if err := json.Unmarshal([]byte(r.Stdout), &out); err != nil {
		t.Fatalf("%q: %v", r.Stdout, err)
	}
	return out.IncludeWarning
}

// A ~/.ssh/config of the user's own gets the Include line as its first
// line, then its bytes as they were, keeping its mode; one that includes
// koko's ssh_config already, however written, is left byte for byte, and
// join warns, as koko status does, when that line comes below a Host line.
func TestJoinPutsTheIncludeLineFirst(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	const own = "Host *\n  ServerAliveInterval 60\n\nHost github.com\n  User git"
	d, r := joinWith(t, h, func(d *harness.Device) {
		if err := os.WriteFile(userSSHConfig(d), []byte(own), 0o640); err != nil {
			t.Fatal(err)
		}
	}, "--json")
	if got := readText(t, userSSHConfig(d)); got != includeLine+"\n"+own {
		t.Errorf("~/.ssh/config = %q, want the Include line, then the file as it was", got)
	}
	if m := mode(t, userSSHConfig(d)); m.Perm() != 0o640 {
		t.Errorf("~/.ssh/config has mode %v, want 0640 as it was", m)
	}
	if w := includeWarning(t, r); w != nil {
		t.Errorf("join warns %q", *w)
	}

	for _, c := range []struct {
		has  func(home string) string
		warn string
	}{
		{func(string) string { return includeLine + "\n" + own }, ""},
		{func(string) string { return "# mine\ninclude=\"~/.ssh/koko/ssh_config\"\n" }, ""},
		{func(string) string { return "Include ~/.ssh/conf.d/* koko/ssh_config\n" }, ""},
		{func(string) string { return "Include ~/.ssh/koko/*\n" }, ""},
		{func(home string) string { return `Include "` + home + `/.ssh/koko/ssh_config"` + "\n" }, ""},
		{func(string) string { return own + "\n" + includeLine + "\n" }, `~/.ssh/config includes koko's ssh_config on line 6, below line 1 ("Host *"), so ssh reads it only for the hosts that line matches: move "` + includeLine + `" to the top`},
	} {
		var has string
		d, r := joinWith(t, h, func(d *harness.Device) {
			has = c.has(d.Home())
			if err := os.WriteFile(userSSHConfig(d), []byte(has), 0o600); err != nil {
				t.Fatal(err)
			}
		}, "--json")
		if got := readText(t, userSSHConfig(d)); got != has {
			t.Errorf("join changed ~/.ssh/config from %q to %q", has, got)
		}
		if w := includeWarning(t, r); c.warn == "" && w != nil || c.warn != "" && (w == nil || *w != c.warn) {
			t.Errorf("about %q, join warns %v, want %q", has, deref(w), c.warn)
		}
	}
}

// A ~/.ssh/config that is a symlink, as dotfiles keep it, or read-only is
// left byte for byte, and join prints the line to add, for people and in
// --json; the device has joined all the same.
func TestJoinLeavesASymlinkedOrReadOnlyConfigAlone(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	const own = "Host github.com\n  User git\n"
	for _, c := range []struct {
		name    string
		prepare func(d *harness.Device)
		why     string
	}{
		{"symlink", func(d *harness.Device) {
			target := filepath.Join(d.Home(), "dotfiles-ssh-config")
			if err := os.WriteFile(target, []byte(own), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, userSSHConfig(d)); err != nil {
				t.Fatal(err)
			}
		}, "~/.ssh/config is a symlink"},
		{"read-only", func(d *harness.Device) {
			if err := os.WriteFile(userSSHConfig(d), []byte(own), 0o400); err != nil {
				t.Fatal(err)
			}
		}, "~/.ssh/config is read-only"},
	} {
		want := c.why + `, so koko left it as it is: add the line "` + includeLine + `" at its top, so that ssh reaches Hosts through koko`
		d, r := joinWith(t, h, c.prepare, "--json")
		if w := includeWarning(t, r); w == nil || *w != want {
			t.Errorf("%s: join --json warns %v, want %q", c.name, w, want)
		}
		harness.Golden(t, "join-"+c.name+".json", strings.ReplaceAll(r.Stdout, readClientConfig(t, d).ServerAddress, "<server address>"))
		if got := readText(t, userSSHConfig(d)); got != own {
			t.Errorf("%s: join changed ~/.ssh/config to %q", c.name, got)
		}
		if m := mode(t, userSSHConfig(d)); c.name == "symlink" && m&os.ModeSymlink == 0 {
			t.Errorf("%s: ~/.ssh/config is no longer a symlink (%v)", c.name, m)
		}
		d.Koko("space", "ls").WantSuccess(t)

		_, r = joinWith(t, h, c.prepare)
		if !strings.HasSuffix(r.Stdout, "\n"+want+".\n") {
			t.Errorf("%s: join says %q, want it to end with %q", c.name, r.Stdout, want)
		}
	}

	// One that is not a regular file is not even read: a named pipe would
	// never end.
	d, r := joinWith(t, h, func(d *harness.Device) {
		if err := os.Mkdir(userSSHConfig(d), 0o700); err != nil {
			t.Fatal(err)
		}
	}, "--json")
	if w, want := includeWarning(t, r), `~/.ssh/config is not a regular file, so koko left it as it is: add the line "`+includeLine+`" at its top, so that ssh reaches Hosts through koko`; w == nil || *w != want {
		t.Errorf("join --json warns %v, want %q", deref(w), want)
	}
	if m := mode(t, userSSHConfig(d)); !m.IsDir() {
		t.Errorf("~/.ssh/config is no longer a directory (%v)", m)
	}
}

// koko join --no-ssh-config leaves ~/.ssh/config alone, made or not, and
// says nothing of it; koko's own ssh_config and known_hosts are written all
// the same, for a user who includes them their own way.
func TestJoinWithNoSSHConfig(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	ref := h.Join(harness.Owner)
	d, r := joinWith(t, h, func(*harness.Device) {}, "--no-ssh-config", "--json")
	if _, err := os.Lstat(userSSHConfig(d)); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("join --no-ssh-config made ~/.ssh/config (%v)", err)
	}
	if w := includeWarning(t, r); w != nil {
		t.Errorf("join --no-ssh-config warns %q", *w)
	}
	if got, want := kokoSSHConfig(t, d), kokoSSHConfig(t, ref); got != want {
		t.Errorf("join --no-ssh-config wrote koko's ssh_config\n%s\nwant it as any join writes it:\n%s", got, want)
	}
	if _, err := os.Stat(filepath.Join(kokoDir(d), "known_hosts")); err != nil {
		t.Errorf("join --no-ssh-config wrote no known_hosts: %v", err)
	}
}

// A HOME whose path ssh would misread in koko's ssh_config (here a '%',
// which ssh expands) refuses the join before it connects, saying why: the
// Join Code stays usable, and the device keeps nothing of the join.
func TestJoinRefusesAHomeSSHWouldMisreadBeforeSpendingTheCode(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	jc := deviceAdd(t, h, "laptop")
	home := filepath.Join(t.TempDir(), "a%h")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatal(err)
	}
	runner := cli.Runner{SocketPath: filepath.Join(harness.ShortTempDir(t), "koko.sock")}
	r := harness.Run(t, runner, []string{"HOME=" + home}, "join", jc.Code, "--json")
	if msg := r.WantError(t, 1, "internal_error"); !strings.Contains(msg, "the Join Code stays usable") || !strings.Contains(msg, `holds '%'`) {
		t.Errorf("join under %s says %q, want why, and that the code stays usable", home, msg)
	}
	if _, err := os.Lstat(filepath.Join(home, ".ssh")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the refused join left ~/.ssh behind (%v)", err)
	}
	harness.NewDevice(t).Koko("join", jc.Code).WantSuccess(t)
}

// The revocation cleanup also takes the bare names out of koko's
// ssh_config and deletes the koko server's known_hosts line, both when a
// device learns it was revoked elsewhere and when it revokes itself. The
// *.koko block stays, so ssh web1.koko still runs koko tunnel, which
// explains the revocation; ~/.ssh/config keeps its Include line.
func TestTheRevocationCleanupRemovesTheBareNamesAndKnownHosts(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	ref := h.Join(harness.Owner)
	addHost(t, h.Koko, "web1")
	addHost(t, h.Koko, "db1")
	elsewhere, _ := joinAs(t, deviceAdd(t, h, "laptop"))
	itself, _ := joinAs(t, deviceAdd(t, h, "desktop"))
	for _, d := range []*harness.Device{elsewhere, itself} {
		if !strings.Contains(kokoSSHConfig(t, d), "Host *.koko db1 web1\n") {
			t.Fatalf("koko's ssh_config holds no bare names:\n%s", kokoSSHConfig(t, d))
		}
	}

	deviceRevoke(t, h.Koko, "laptop")
	elsewhere.Koko("space", "ls", "--json").WantError(t, 1, "revoked")
	deviceRevoke(t, itself.Koko, "desktop")

	for _, d := range []*harness.Device{elsewhere, itself} {
		if got, want := kokoSSHConfig(t, d), kokoSSHConfig(t, ref); got != want {
			t.Errorf("after the cleanup koko's ssh_config is\n%s\nwant it as a join writes it:\n%s", got, want)
		}
		wantGone(t, d, "known_hosts", "client.json", "device_key")
		if got := readText(t, userSSHConfig(d)); got != includeLine+"\n" {
			t.Errorf("the cleanup changed ~/.ssh/config to %q", got)
		}
	}
}
