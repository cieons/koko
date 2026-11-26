package clientaddr_test

import (
	"testing"

	"github.com/cieons/koko/internal/server/clientaddr"
)

// An IPv6 address counts by its /64, its zone dropped and an IPv4-mapped
// address unmapped; an IPv4 address, or anything that is no address,
// counts as itself.
func TestKey(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ address, want string }{
		{"192.0.2.10", "192.0.2.10"},
		{"2001:db8:1:2::1", "2001:db8:1:2::/64"},
		{"2001:db8:1:2:ffff:ffff:ffff:ffff", "2001:db8:1:2::/64"},
		{"2001:db8:1:3::1", "2001:db8:1:3::/64"},
		{"fe80::1%eth0", "fe80::/64"},
		{"::ffff:192.0.2.10", "192.0.2.10"},
		{"unix socket", "unix socket"},
		{"", ""},
	} {
		if got := clientaddr.Key(c.address); got != c.want {
			t.Errorf("Key(%q) = %q, want %q", c.address, got, c.want)
		}
	}
}
