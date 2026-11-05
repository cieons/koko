//go:build linux || darwin

package portowner_test

import (
	"errors"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cieons/koko/internal/shared/portowner"
)

// listen holds a TCP port on addr until the test ends and returns it.
func listen(t *testing.T, network, addr string) int {
	t.Helper()
	var lc net.ListenConfig
	ln, err := lc.Listen(t.Context(), network, addr)
	if err != nil {
		t.Fatalf("listen on %s: %v", addr, err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	return ln.Addr().(*net.TCPAddr).Port
}

// The process holding a port is named with its pid, its command and its
// user: here, this test's own process, on an IPv4 and an IPv6 address.
func TestLookupNamesTheProcessListening(t *testing.T) {
	me, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range []struct{ network, addr string }{{"tcp4", "127.0.0.1:0"}, {"tcp6", "[::1]:0"}} {
		t.Run(l.network, func(t *testing.T) {
			port := listen(t, l.network, l.addr)
			p, err := portowner.Lookup(t.Context(), port)
			if err != nil {
				t.Fatalf("Lookup(%d): %v", port, err)
			}
			// The OS cuts a command's name short, so the test binary's name
			// starts with it.
			if p.PID != os.Getpid() || p.Name == "" || !strings.HasPrefix(filepath.Base(os.Args[0]), p.Name) || p.User != me.Username {
				t.Errorf("Lookup(%d) = %+v, want pid %d, a prefix of %q and user %q",
					port, p, os.Getpid(), filepath.Base(os.Args[0]), me.Username)
			}
			if s := p.String(); !strings.Contains(s, p.Name) || !strings.Contains(s, "pid") || !strings.Contains(s, me.Username) {
				t.Errorf("String() = %q, want the name, pid and user", s)
			}
		})
	}
}

// A port nobody listens on has no owner to name.
func TestLookupOfAFreePortFindsNone(t *testing.T) {
	var lc net.ListenConfig
	ln, err := lc.Listen(t.Context(), "tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	if p, err := portowner.Lookup(t.Context(), port); !errors.Is(err, portowner.ErrNotFound) {
		t.Errorf("Lookup(%d) of a free port = %+v, %v; want ErrNotFound", port, p, err)
	}
}

// Without the pid, the process is still named by its user, as for another
// user's process this user cannot see into.
func TestStringWithoutThePID(t *testing.T) {
	cases := map[portowner.Process]string{
		{User: "www-data"}: "a process of user www-data",
		{}:                 "a process koko cannot name",
		{PID: 812, Name: "nginx", User: "www-data"}: "nginx (pid 812, user www-data)",
		{PID: 812}: "a process (pid 812)",
	}
	for p, want := range cases {
		if got := p.String(); got != want {
			t.Errorf("%+v.String() = %q, want %q", p, got, want)
		}
	}
}
