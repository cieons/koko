//go:build !windows

package devicefiles_test

import "path/filepath"

// kokoFile is the name a PATH lookup of koko finds.
const kokoFile = "koko"

// goldenSuffix ends the name of a golden file of koko's ssh_config, which
// differs on Windows.
const goldenSuffix = ""

// wantKnownHosts is the UserKnownHostsFile argument koko's ssh_config gives
// for home on Linux and macOS: its known_hosts by absolute path,
// double-quoted.
func wantKnownHosts(home string) string {
	return `"` + filepath.Join(home, ".ssh", "koko", "known_hosts") + `"`
}
