package installer

import (
	"net/netip"
	"slices"
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

// networkFilesystems are the filesystem types, as Linux names them, that
// keep their files on another machine, where SQLite's WAL does not work.
var networkFilesystems = []string{
	"9p", "afs", "beegfs", "ceph", "cifs", "coda", "glusterfs", "gpfs", "lustre",
	"ncpfs", "nfs", "nfs4", "pvfs2", "smb3", "smbfs",
	"fuse.ceph-fuse", "fuse.davfs", "fuse.gcsfuse", "fuse.glusterfs",
	"fuse.juicefs", "fuse.rclone", "fuse.s3fs", "fuse.sshfs",
}

// networkFilesystem tells whether the filesystem type fsType keeps its files
// on another machine.
func networkFilesystem(fsType string) bool {
	return slices.Contains(networkFilesystems, fsType)
}
