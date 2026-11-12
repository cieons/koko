//go:build !windows

package cli_test

// joinSSHConfigGolden is the golden file of koko's ssh_config after a join
// on Linux and macOS, which name known_hosts by its absolute path.
const joinSSHConfigGolden = "join-ssh_config"
