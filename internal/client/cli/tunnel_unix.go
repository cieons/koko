//go:build !windows

package cli

// unquoteSSHName returns the SSH Name koko tunnel was given as arg: on Linux
// and macOS arg itself, since the shell ssh runs koko's ProxyCommand through
// has removed the single quotes around '%h'.
func unquoteSSHName(arg string) string {
	return arg
}
