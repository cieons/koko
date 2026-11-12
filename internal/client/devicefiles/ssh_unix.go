//go:build !windows

package devicefiles

import "path/filepath"

// knownHostsArg returns the UserKnownHostsFile argument of koko's ssh_config
// for the device whose home directory is home: on Linux and macOS koko's
// known_hosts by its absolute path, double-quoted, since ssh finds ~ from
// the password database, which need not be the HOME koko wrote under.
func knownHostsArg(home string) (string, error) {
	return quotePath(filepath.Join(dir(home), knownHostsFile))
}
