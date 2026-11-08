package installer

import (
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
