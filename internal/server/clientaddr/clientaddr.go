// Package clientaddr says what a client's address counts as wherever koko
// bounds what one client may do: in the sign-in limits and in the flows of
// signing in.
package clientaddr

import "net/netip"

// Key is what a client address counts as: an IPv6 address, its zone
// dropped, by its /64, since one host commonly holds the /64 whole; an
// IPv4-mapped IPv6 address as its IPv4 address; any other as itself.
func Key(address string) string {
	a, err := netip.ParseAddr(address)
	if err != nil {
		return address
	}
	if a = a.Unmap(); !a.Is6() {
		return a.String()
	}
	p, _ := a.WithZone("").Prefix(64) // an IPv6 address always has a /64
	return p.String()
}
