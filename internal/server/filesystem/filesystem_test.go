package filesystem

import (
	"path/filepath"
	"strings"
	"testing"
)

// Network filesystems are named as Linux names them, and local ones pass.
func TestNetwork(t *testing.T) {
	for fsType, want := range map[string]bool{
		"nfs": true, "nfs4": true, "cifs": true, "smb3": true, "ceph": true, "fuse.sshfs": true, "9p": true,
		"ext4": false, "xfs": false, "btrfs": false, "zfs": false, "tmpfs": false, "overlay": false, "fuse.mergerfs": false,
	} {
		if got := Network(fsType); got != want {
			t.Errorf("Network(%q) = %v, want %v", fsType, got, want)
		}
	}
}

// mountinfo as Linux writes it: an NFS mount on /var/lib/koko under an ext4
// root, a later mount hiding an earlier one on the same point, and a mount
// point holding a space.
const mountinfo = `22 1 252:1 / / rw,relatime shared:1 - ext4 /dev/vda1 rw,errors=remount-ro
25 22 0:22 / /run rw,nosuid,nodev shared:5 - tmpfs tmpfs rw,size=401564k,mode=755
40 22 0:35 / /var/lib/koko rw,relatime shared:20 - nfs4 10.0.0.2:/export/koko rw,vers=4.2
41 22 0:36 / /srv rw,relatime shared:21 - nfs4 10.0.0.2:/export/srv rw,vers=4.2
42 22 252:2 / /srv rw,relatime shared:22 - xfs /dev/vdb1 rw
43 22 252:3 / /mnt/my\040disk rw,relatime shared:23 - btrfs /dev/vdc1 rw
`

// The filesystem holding a path is the latest mount on its longest mount
// point.
func TestMountType(t *testing.T) {
	for path, want := range map[string]string{
		"/":                     "ext4",
		"/var/lib":              "ext4",
		"/var/lib/koko":         "nfs4",
		"/var/lib/koko/koko.db": "nfs4",
		"/var/lib/kokoa":        "ext4",
		"/run/koko":             "tmpfs",
		"/srv/data":             "xfs",
		"/mnt/my disk/koko":     "btrfs",
	} {
		got, err := mountType(strings.NewReader(mountinfo), path)
		if err != nil || got != want {
			t.Errorf("mountType(%s) = %q, %v; want %q", path, got, err, want)
		}
	}
	if _, err := mountType(strings.NewReader("garbage\n"), "/"); err == nil {
		t.Error("mountinfo that does not parse gave a filesystem type")
	}
}

// Nearest finds where making a path would begin: the path itself, or the
// nearest directory above it that exists.
func TestNearest(t *testing.T) {
	root := t.TempDir()
	for path, want := range map[string]string{
		root:                               root,
		filepath.Join(root, "data"):        root,
		filepath.Join(root, "a", "b", "c"): root,
	} {
		if got, err := Nearest(path); err != nil || got != want {
			t.Errorf("Nearest(%s) = %q, %v; want %q", path, got, err, want)
		}
	}
}
