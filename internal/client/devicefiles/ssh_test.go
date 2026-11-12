package devicefiles_test

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/cieons/koko/internal/client/devicefiles"
	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/harness/skip"
)

// openSSH73 holds the keywords koko's ssh_config may use, each one that
// OpenSSH 7.3, the release that added Include, already knew. An unknown
// keyword would break every ssh the user runs, so a keyword is added here
// only once checked against 7.3's readconf.c.
var openSSH73 = []string{
	"Host", "HostName", "ProxyCommand", "HostKeyAlias", "UserKnownHostsFile",
	"StrictHostKeyChecking", "UpdateHostKeys", "PreferredAuthentications",
	"ForwardAgent", "User", "ServerAliveInterval", "ServerAliveCountMax",
}

// A name list too long for one Host line is split across blocks that are
// identical but for their names, so that no line passes 1 KiB; every name
// is there once. The file opens with a comment and then its Host line, no
// directive above it, uses only keywords OpenSSH 7.3 knows, names the koko
// binary by its absolute, double-quoted path, and the known_hosts file so
// too on Linux and macOS and under ~ on Windows, passes %h single-quoted,
// and has mode 0600.
func TestKokosSSHConfigSplitsALongNameList(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".ssh", "koko"), 0o700); err != nil {
		t.Fatal(err)
	}
	var names []string
	for i := range 40 {
		names = append(names, fmt.Sprintf("%s%02d", strings.Repeat("h", 61), i)) // 63 characters, the longest
	}
	if _, err := devicefiles.WriteSSHConfig(home, names); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, ".ssh", "koko", "ssh_config")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	harness.WantPrivate(t, path)
	koko, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	harness.Golden(t, "ssh_config-split"+goldenSuffix, harness.Paths(string(b), koko, "<koko>", home, "<home>"))

	var hosts []string
	var bodies [][]string
	for line := range strings.Lines(string(b)) {
		if len(line) > 1024 {
			t.Errorf("a line of %d bytes passes 1 KiB: %.40s...", len(line), line)
		}
		if strings.HasPrefix(line, "#") {
			continue
		}
		keyword, args, _ := strings.Cut(strings.TrimSpace(line), " ")
		if !slices.Contains(openSSH73, keyword) {
			t.Errorf("keyword %q is not one OpenSSH 7.3 knows", keyword)
		}
		switch keyword {
		case "Host":
			hosts = append(hosts, strings.Fields(args)...)
			bodies = append(bodies, nil)
			continue
		case "ProxyCommand":
			// Both paths with forward slashes, on Windows too.
			if want := `"` + filepath.ToSlash(koko) + `" tunnel '%h'`; args != want {
				t.Errorf("ProxyCommand %s, want %s", args, want)
			}
		case "UserKnownHostsFile":
			if want := wantKnownHosts(home); args != want {
				t.Errorf("UserKnownHostsFile %s, want %s", args, want)
			}
		}
		if len(bodies) == 0 {
			t.Fatalf("%q comes above the first Host line", line)
		}
		bodies[len(bodies)-1] = append(bodies[len(bodies)-1], line)
	}
	if want := append([]string{"*.koko"}, names...); !slices.Equal(hosts, want) {
		t.Errorf("the Host lines name %v, want %v", hosts, want)
	}
	if len(bodies) != 3 {
		t.Errorf("40 names of 63 characters make %d blocks, want 3", len(bodies))
	}
	for _, body := range bodies[1:] {
		if !slices.Equal(body, bodies[0]) {
			t.Errorf("a block reads %q, unlike the first, %q", body, bodies[0])
		}
	}
}

// The ProxyCommand runs koko by the path a PATH lookup of koko finds when
// that is this very binary, such as a package manager's link that outlives
// an upgrade, and by the path the OS gives for the binary otherwise.
func TestKokosSSHConfigRunsKokoByItsPathOnPATH(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	linked := t.TempDir()
	link := filepath.Join(linked, kokoFile)
	if err := os.Symlink(self, link); err != nil {
		t.Fatal(err)
	}
	another := t.TempDir()
	if err := os.WriteFile(filepath.Join(another, kokoFile), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		path string
		want string
	}{
		{linked, link},
		{another, self},
		{"", self},
	} {
		t.Setenv("PATH", c.path)
		home := t.TempDir()
		if err := os.MkdirAll(filepath.Join(home, ".ssh", "koko"), 0o700); err != nil {
			t.Fatal(err)
		}
		if _, err := devicefiles.WriteSSHConfig(home, nil); err != nil {
			t.Fatal(err)
		}
		b, err := os.ReadFile(filepath.Join(home, ".ssh", "koko", "ssh_config"))
		if err != nil {
			t.Fatal(err)
		}
		if want := "\n  ProxyCommand \"" + filepath.ToSlash(c.want) + "\" tunnel '%h'\n"; !strings.Contains(string(b), want) {
			t.Errorf("with PATH=%s, koko's ssh_config is\n%s\nwith no line %q", c.path, b, want)
		}
	}
}

// A name that is not a Host's is refused, and no ssh_config is written.
func TestKokosSSHConfigRefusesWhatSSHWouldMisread(t *testing.T) {
	for name, names := range map[string][]string{
		"a name with a space":  {"web1 db1"},
		"a pattern for a name": {"*"},
	} {
		home := t.TempDir()
		if err := os.MkdirAll(filepath.Join(home, ".ssh", "koko"), 0o700); err != nil {
			t.Fatal(err)
		}
		if _, err := devicefiles.WriteSSHConfig(home, names); err == nil {
			t.Errorf("%s: koko wrote its ssh_config", name)
		}
		if _, err := os.Stat(filepath.Join(home, ".ssh", "koko", "ssh_config")); err == nil {
			t.Errorf("%s: an ssh_config is there", name)
		}
	}
}

// A home directory whose path ssh or its shell would read as something else
// inside double quotes, in the known_hosts path koko's ssh_config names, is
// refused, and no ssh_config is written.
func TestKokosSSHConfigRefusesAHomeSSHWouldMisread(t *testing.T) {
	skip.OnWindows(t, "koko's ssh_config names known_hosts under ~ on Windows, so the home directory's path is not in it, and no path there holds a double quote")
	for name, dir := range map[string]string{
		"a $ in the home directory":  "a$HOME",
		"a %h in the home directory": "a%h",
		"a double quote in the home": `a"b`,
	} {
		home := filepath.Join(t.TempDir(), dir)
		if err := os.MkdirAll(filepath.Join(home, ".ssh", "koko"), 0o700); err != nil {
			t.Fatal(err)
		}
		if _, err := devicefiles.WriteSSHConfig(home, nil); err == nil {
			t.Errorf("%s: koko wrote its ssh_config under %s", name, home)
		}
		if _, err := os.Stat(filepath.Join(home, ".ssh", "koko", "ssh_config")); err == nil {
			t.Errorf("%s: an ssh_config is there under %s", name, home)
		}
	}
}

// For a home directory whose name holds a space, with koko where install
// puts it, ~/.local/bin, koko's ssh_config runs koko by its double-quoted
// path, written with forward slashes on Windows, and names known_hosts by
// its double-quoted path on Linux and macOS and under ~ on Windows.
func TestKokosSSHConfigForAHomeWithASpace(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	profiles := t.TempDir()
	home := filepath.Join(profiles, "Alice Smith")
	bin := filepath.Join(home, ".local", "bin")
	for _, d := range []string{bin, filepath.Join(home, ".ssh", "koko")} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(self, filepath.Join(bin, kokoFile)); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	if _, err := devicefiles.WriteSSHConfig(home, []string{"web1"}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(home, ".ssh", "koko", "ssh_config"))
	if err != nil {
		t.Fatal(err)
	}
	harness.Golden(t, "ssh_config-home-with-a-space"+goldenSuffix, harness.Paths(string(b), profiles, "<profiles>"))
}

// AddressNames lists, in the file's order, the bare names in koko's
// ssh_config that the C library reads as an IPv4 address: one number
// within 32 bits, decimal, octal after a 0 or hex after 0x. No file holds
// none, and one that is not a regular file is an error.
func TestAddressNamesAreTheNamesSSHReadsAsAnIPv4Address(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".ssh", "koko"), 0o700); err != nil {
		t.Fatal(err)
	}
	if got, err := devicefiles.AddressNames(home); err != nil || got == nil || len(got) != 0 {
		t.Errorf("with no ssh_config, AddressNames = %q (%v), want an empty list", got, err)
	}
	names := []string{
		"web1", "0", "00", "0x", "0x0", "0xab", "0xg", "08", "09", "010", "1e5",
		"201", "4294967295", "4294967296", "0x100000000",
	}
	if _, err := devicefiles.WriteSSHConfig(home, names); err != nil {
		t.Fatal(err)
	}
	want := []string{"0", "00", "0x0", "0xab", "010", "201", "4294967295"}
	if got, err := devicefiles.AddressNames(home); err != nil || !slices.Equal(got, want) {
		t.Errorf("AddressNames of %q = %q (%v), want %q", names, got, err, want)
	}

	path := filepath.Join(home, ".ssh", "koko", "ssh_config")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if got, err := devicefiles.AddressNames(home); err == nil {
		t.Errorf("with a directory for koko's ssh_config, AddressNames = %q, want an error", got)
	}
}

// The Device Mux's refresh rewrites the bare names but runs koko by the path
// the file names already, which a command run from the user's shell wrote:
// a refresh whose own lookup finds another path, as one without the user's
// PATH does, or an old build's after an upgrade, keeps it. A file that names
// no koko gets this binary.
func TestARefreshKeepsTheKokoTheFileRuns(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	linked := t.TempDir()
	link := filepath.Join(linked, kokoFile)
	if err := os.Symlink(self, link); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".ssh", "koko"), 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, ".ssh", "koko", "ssh_config")
	proxy := func(koko string) string { return "\n  ProxyCommand \"" + filepath.ToSlash(koko) + "\" tunnel '%h'\n" }

	t.Setenv("PATH", linked)
	if _, err := devicefiles.WriteSSHConfig(home, []string{"web1"}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", "")
	if wrote, err := devicefiles.RefreshSSHConfig(home, []string{"db1", "web1"}); err != nil || !wrote {
		t.Fatalf("a refresh with other names wrote %v (%v), want it written", wrote, err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "Host *.koko db1 web1\n") || !strings.Contains(string(b), proxy(link)) {
		t.Errorf("after a refresh without koko on PATH, koko's ssh_config is\n%s\nwant the new names and koko run as %s", b, link)
	}

	if err := os.WriteFile(path, []byte("# no ProxyCommand\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := devicefiles.RefreshSSHConfig(home, []string{"web1"}); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(path); err != nil || !strings.Contains(string(b), proxy(self)) {
		t.Errorf("a refresh of a file that names no koko wrote\n%s\n(%v), want koko run as %s", b, err, self)
	}
}

// koko's ssh_config is written only when it would change: the same names
// again leave the file alone, while other names, or the same bytes in a
// file that is not private to this user (a mode other than 0600, or on
// Windows another access control list), rewrite it private.
func TestKokosSSHConfigIsWrittenOnlyWhenItWouldChange(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".ssh", "koko"), 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, ".ssh", "koko", "ssh_config")
	write := func(names ...string) (bool, os.FileInfo) {
		t.Helper()
		wrote, err := devicefiles.WriteSSHConfig(home, names)
		if err != nil {
			t.Fatal(err)
		}
		fi, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		return wrote, fi
	}

	if wrote, _ := write("web1"); !wrote {
		t.Errorf("koko says it did not write an ssh_config that was not there")
	}
	_, first := write("web1")
	if wrote, again := write("web1"); wrote || !os.SameFile(first, again) {
		t.Errorf("the same names again: koko says it wrote %v, and the file is the same one: %v; want neither written", wrote, os.SameFile(first, again))
	}
	if wrote, other := write("web1", "db1"); !wrote || os.SameFile(first, other) {
		t.Errorf("other names: koko says it wrote %v, and the file is the same one: %v; want it rewritten", wrote, os.SameFile(first, other))
	}
	harness.WantPrivate(t, path)
	harness.LetOthersIn(t, path)
	if wrote, _ := write("web1", "db1"); !wrote {
		t.Errorf("the same names on a file other users can read: koko says it did not write; want it rewritten")
	}
	harness.WantPrivate(t, path)
}
