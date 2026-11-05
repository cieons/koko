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
)

// openSSH73 holds the keywords koko's ssh_config may use, each one that
// OpenSSH 7.3, the release that added Include, already knew. An unknown
// keyword would break every ssh the user runs, so a keyword is added here
// only once checked against 7.3's readconf.c.
var openSSH73 = []string{
	"Host", "ProxyCommand", "HostKeyAlias", "UserKnownHostsFile",
	"StrictHostKeyChecking", "UpdateHostKeys", "PreferredAuthentications",
	"ForwardAgent", "User", "ServerAliveInterval", "ServerAliveCountMax",
}

// A name list too long for one Host line is split across blocks that are
// identical but for their names, so that no line passes 1 KiB; every name
// is there once. The file opens with a comment and then its Host line, no
// directive above it, uses only keywords OpenSSH 7.3 knows, names the koko
// binary and the known_hosts file by absolute, double-quoted paths, passes
// %h single-quoted, and has mode 0600.
func TestKokosSSHConfigSplitsALongNameList(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".ssh", "koko"), 0o700); err != nil {
		t.Fatal(err)
	}
	var names []string
	for i := range 40 {
		names = append(names, fmt.Sprintf("%s%02d", strings.Repeat("h", 61), i)) // 63 characters, the longest
	}
	if err := devicefiles.WriteSSHConfig(home, names); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, ".ssh", "koko", "ssh_config")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("koko's ssh_config has mode %v (%v), want 0600", fi.Mode(), err)
	}
	koko, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	harness.Golden(t, "ssh_config-split", strings.NewReplacer(koko, "<koko>", home, "<home>").Replace(string(b)))

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
			if want := `"` + koko + `" tunnel '%h'`; args != want {
				t.Errorf("ProxyCommand %s, want %s", args, want)
			}
		case "UserKnownHostsFile":
			if want := `"` + filepath.Join(home, ".ssh", "koko", "known_hosts") + `"`; args != want {
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
	link := filepath.Join(linked, "koko")
	if err := os.Symlink(self, link); err != nil {
		t.Fatal(err)
	}
	another := t.TempDir()
	if err := os.WriteFile(filepath.Join(another, "koko"), []byte("#!/bin/sh\n"), 0o755); err != nil {
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
		if err := devicefiles.WriteSSHConfig(home, nil); err != nil {
			t.Fatal(err)
		}
		b, err := os.ReadFile(filepath.Join(home, ".ssh", "koko", "ssh_config"))
		if err != nil {
			t.Fatal(err)
		}
		if want := "\n  ProxyCommand \"" + c.want + "\" tunnel '%h'\n"; !strings.Contains(string(b), want) {
			t.Errorf("with PATH=%s, koko's ssh_config is\n%s\nwith no line %q", c.path, b, want)
		}
	}
}

// A home directory whose path ssh or its shell would read as something else
// inside double quotes, and a name that is not a Host's, are refused, and
// no ssh_config is written.
func TestKokosSSHConfigRefusesWhatSSHWouldMisread(t *testing.T) {
	for name, c := range map[string]struct {
		home  string
		names []string
	}{
		"a $ in the home directory":  {home: "a$HOME"},
		"a %h in the home directory": {home: "a%h"},
		"a quote in the home":        {home: `a"b`},
		"a name with a space":        {home: "ok", names: []string{"web1 db1"}},
		"a pattern for a name":       {home: "ok", names: []string{"*"}},
	} {
		home := filepath.Join(t.TempDir(), c.home)
		if err := os.MkdirAll(filepath.Join(home, ".ssh", "koko"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := devicefiles.WriteSSHConfig(home, c.names); err == nil {
			t.Errorf("%s: koko wrote its ssh_config", name)
		}
		if _, err := os.Stat(filepath.Join(home, ".ssh", "koko", "ssh_config")); err == nil {
			t.Errorf("%s: an ssh_config is there", name)
		}
	}
}
