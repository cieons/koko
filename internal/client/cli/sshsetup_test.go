package cli_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/shared/api"
)

// hostBareNames returns the bare names koko's ssh_config under home holds,
// in order.
func hostBareNames(t *testing.T, home string) []string {
	t.Helper()
	names := []string{}
	for line := range strings.Lines(readText(t, filepath.Join(home, ".ssh", "koko", "ssh_config"))) {
		if rest, ok := strings.CutPrefix(line, "Host "); ok {
			for n := range strings.FieldsSeq(rest) {
				if n != "*.koko" {
					names = append(names, n)
				}
			}
		}
	}
	return names
}

// On the koko server's own machine, where nothing joins, koko ssh setup
// writes for the OS user running it what a join writes for ssh: koko's
// ssh_config with the bare names of every Host but the Retired, since the
// Local caller acts as the Owner; koko's known_hosts with the koko server's
// host key, which the Gateway presents; and the Include line. That user's
// own Host changes over the socket then rewrite the bare names, and running
// it again writes the same files.
func TestSSHSetupWritesKokosSSHFilesOnTheKokoServersMachine(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	h.Koko("space", "add", "ops").WantSuccess(t)
	addHost(t, h.Koko, "web1")
	addHost(t, h.Koko, "db1", "space=ops")
	addHost(t, h.Koko, "old")
	h.Koko("host", "retire", "old").WantSuccess(t)

	r := h.Koko("ssh", "setup", "--json")
	r.WantSuccess(t)
	harness.Golden(t, "ssh-setup.json", strings.ReplaceAll(r.Stdout, h.Home(), "<home>"))

	home := h.Home()
	kokoDir := filepath.Join(home, ".ssh", "koko")
	if got := readText(t, filepath.Join(home, ".ssh", "config")); got != includeLine+"\n" {
		t.Errorf("~/.ssh/config = %q, want the Include line alone", got)
	}
	for _, d := range []string{filepath.Join(home, ".ssh"), kokoDir} {
		if m := mode(t, d); !m.IsDir() || m.Perm() != 0o700 {
			t.Errorf("%s has mode %v, want a directory with 0700", d, m)
		}
	}
	for _, f := range []string{filepath.Join(home, ".ssh", "config"), filepath.Join(kokoDir, "ssh_config"), filepath.Join(kokoDir, "known_hosts")} {
		if m := mode(t, f); !m.IsRegular() || m.Perm() != 0o600 {
			t.Errorf("%s has mode %v, want a file with 0600", f, m)
		}
	}
	if got, want := hostBareNames(t, home), []string{"db1", "web1"}; !slices.Equal(got, want) {
		t.Errorf("koko's ssh_config holds the bare names %q, want %q", got, want)
	}
	show, err := h.Call(api.ServerShow, api.ServerShowRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := readText(t, filepath.Join(kokoDir, "known_hosts")), "koko "+show.Server.HostKey+"\n"; got != want {
		t.Errorf("koko's known_hosts = %q, want the koko server's host key alone, %q", got, want)
	}
	key, _, _, _, err := ssh.ParseAuthorizedKey([]byte(show.Server.HostKey))
	if err != nil || ssh.FingerprintSHA256(key) != gatewayHostKey(t, h) {
		t.Errorf("server.show's host_key %q (%v) is not the key whose fingerprint koko server show prints", show.Server.HostKey, err)
	}
	entries, err := os.ReadDir(kokoDir)
	if err != nil {
		t.Fatal(err)
	}
	var files []string
	for _, e := range entries {
		files = append(files, e.Name())
	}
	if want := []string{"known_hosts", "ssh_config"}; !slices.Equal(files, want) {
		t.Errorf("~/.ssh/koko holds %q, want only %q: nothing of a join", files, want)
	}

	addHost(t, h.Koko, "cache", "space=ops")
	if got, want := hostBareNames(t, home), []string{"cache", "db1", "web1"}; !slices.Equal(got, want) {
		t.Errorf("after host add over the socket, koko's ssh_config holds the bare names %q, want %q", got, want)
	}
	before := readText(t, filepath.Join(kokoDir, "ssh_config"))
	r = h.Koko("ssh", "setup")
	r.WantSuccess(t)
	if want := "Wrote koko's ssh_config for this user: " + filepath.Join(kokoDir, "ssh_config") + "\nssh <host>.koko now reaches a Host through koko.\n"; r.Stdout != want {
		t.Errorf("ssh setup again printed %q, want %q", r.Stdout, want)
	}
	if got := readText(t, filepath.Join(kokoDir, "ssh_config")); got != before {
		t.Errorf("ssh setup again rewrote koko's ssh_config as\n%s\nwant it as it was:\n%s", got, before)
	}
	if got := readText(t, filepath.Join(home, ".ssh", "config")); got != includeLine+"\n" {
		t.Errorf("after ssh setup again, ~/.ssh/config = %q, want the Include line once", got)
	}
}

// koko ssh setup leaves a read-only ~/.ssh/config byte for byte, by the
// join's rules, and says which line to add; koko's own files are written
// all the same.
func TestSSHSetupLeavesAReadOnlyConfigAlone(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	const own = "Host github.com\n  User git\n"
	config := filepath.Join(h.Home(), ".ssh", "config")
	if err := os.Mkdir(filepath.Dir(config), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config, []byte(own), 0o400); err != nil {
		t.Fatal(err)
	}

	r := h.Koko("ssh", "setup", "--json")
	r.WantSuccess(t)
	want := `~/.ssh/config is read-only, so koko left it as it is: add the line "` + includeLine + `" at its top, so that ssh reaches Hosts through koko`
	if w := includeWarning(t, r); w == nil || *w != want {
		t.Errorf("ssh setup --json warns %v, want %q", deref(w), want)
	}
	if got := readText(t, config); got != own {
		t.Errorf("ssh setup changed ~/.ssh/config to %q", got)
	}
	if _, err := os.Stat(filepath.Join(h.Home(), ".ssh", "koko", "ssh_config")); err != nil {
		t.Errorf("ssh setup wrote no koko ssh_config: %v", err)
	}
	wantHuman := "Wrote koko's ssh_config for this user: " + filepath.Join(h.Home(), ".ssh", "koko", "ssh_config") + "\n" + want + ".\n"
	if r := h.Koko("ssh", "setup"); r.Stdout != wantHuman {
		t.Errorf("ssh setup says %q, want %q", r.Stdout, wantHuman)
	}
}

// On a joined device koko join wrote koko's ssh files already: koko ssh
// setup is refused there, naming koko join, before any call, and changes
// nothing. On a machine that neither joined nor runs a koko server, it
// fails as any command does, naming koko join.
func TestSSHSetupIsOnlyForTheKokoServersMachine(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	d := h.Join(harness.Owner)
	before := kokoSSHConfig(t, d)
	calls := h.Calls()
	msg := d.Koko("ssh", "setup", "--json").WantError(t, 1, "already_joined")
	if !strings.Contains(msg, "koko join wrote koko's ssh_config") || !strings.Contains(msg, "the koko server's own machine") {
		t.Errorf("the refusal says %q, want it to name koko join and the koko server's own machine", msg)
	}
	if n := h.Calls() - calls; n != 0 {
		t.Errorf("the refused ssh setup made %d calls, want none", n)
	}
	if got := kokoSSHConfig(t, d); got != before {
		t.Errorf("the refused ssh setup changed koko's ssh_config to\n%s", got)
	}

	nowhere := harness.NewDevice(t)
	msg = nowhere.Koko("ssh", "setup", "--json").WantError(t, 1, "not_joined")
	if !strings.Contains(msg, "koko join <join-code>") {
		t.Errorf("ssh setup with no koko server says %q, want it to name koko join", msg)
	}
	if _, err := os.Lstat(filepath.Join(nowhere.Home(), ".ssh")); err == nil {
		t.Errorf("ssh setup with no koko server made ~/.ssh")
	}
}
