package installer

import (
	"strings"
	"testing"
)

// systemd's version is read from the first line systemctl --version prints,
// as each distribution writes it.
func TestParseSystemdVersion(t *testing.T) {
	for out, want := range map[string]int{
		"systemd 252 (252.22-1~deb12u1)\n+PAM +AUDIT +SELINUX\n": 252,
		"systemd 249 (249.11-0ubuntu3.12)\n":                     249,
		"systemd 247 (247.3-7+deb11u4)\n":                        247,
		"systemd 256.5 (256.5-1-arch)\n":                         256,
		"systemd 246\n":                                          246,
	} {
		got, err := parseSystemdVersion([]byte(out))
		if err != nil || got != want {
			t.Errorf("parseSystemdVersion(%q) = %d, %v; want %d", out, got, err, want)
		}
	}
	for _, out := range []string{"", "bash: systemctl: command not found\n", "systemd\n", "systemd two-fifty\n"} {
		if got, err := parseSystemdVersion([]byte(out)); err == nil {
			t.Errorf("parseSystemdVersion(%q) = %d, want an error", out, got)
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
