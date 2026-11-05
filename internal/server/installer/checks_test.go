package installer

import (
	"net/netip"
	"testing"
)

// The address check: a default route's source IP is public only when
// devices elsewhere could reach it. It is a pure function and asks no one.
func TestIsPublic(t *testing.T) {
	for ip, want := range map[string]bool{
		"203.0.113.7":          true,
		"8.8.8.8":              true,
		"100.63.255.255":       true, // just below CGNAT
		"100.128.0.0":          true, // just above it
		"172.15.255.255":       true, // just below RFC 1918's 172.16.0.0/12
		"2001:4860:4860::8888": true,
		"::ffff:203.0.113.7":   true, // as an IPv6 socket may give it
		"10.0.0.1":             false,
		"172.16.0.1":           false,
		"172.31.255.255":       false,
		"192.168.1.20":         false,
		"100.64.0.1":           false, // CGNAT
		"100.127.255.255":      false,
		"fd12:3456::1":         false, // ULA
		"fc00::1":              false,
		"::ffff:10.0.0.1":      false,
		"127.0.0.1":            false, // loopback
		"::1":                  false,
		"169.254.10.1":         false, // link-local
		"fe80::1":              false,
		"0.0.0.0":              false,
		"::":                   false,
		"224.0.0.1":            false, // multicast
		"ff02::1":              false,
		"255.255.255.255":      false,
	} {
		if got := isPublic(netip.MustParseAddr(ip)); got != want {
			t.Errorf("isPublic(%s) = %v, want %v", ip, got, want)
		}
	}
}

// Network filesystems are named as Linux names them, and local ones pass.
func TestNetworkFilesystem(t *testing.T) {
	for fsType, want := range map[string]bool{
		"nfs": true, "nfs4": true, "cifs": true, "smb3": true, "ceph": true, "fuse.sshfs": true, "9p": true,
		"ext4": false, "xfs": false, "btrfs": false, "zfs": false, "tmpfs": false, "overlay": false, "fuse.mergerfs": false,
	} {
		if got := networkFilesystem(fsType); got != want {
			t.Errorf("networkFilesystem(%q) = %v, want %v", fsType, got, want)
		}
	}
}
