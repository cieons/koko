package installer

import (
	"net/netip"
)

// cgnat is the shared address space of carrier-grade NAT (RFC 6598).
var cgnat = netip.MustParsePrefix("100.64.0.0/10")

// isPublic tells whether devices elsewhere could reach ip: a global unicast
// address outside the private ranges of RFC 1918, CGNAT's shared range and
// IPv6 unique local addresses. Loopback, link-local, multicast and
// unspecified addresses are not public either. It asks no one.
func isPublic(ip netip.Addr) bool {
	ip = ip.Unmap()
	return ip.IsGlobalUnicast() && !ip.IsPrivate() && !cgnat.Contains(ip)
}
