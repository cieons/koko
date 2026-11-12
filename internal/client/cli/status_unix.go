//go:build !windows

package cli

// sshClientWarnings says what keeps ssh on this device from reading the
// configuration koko wrote: on Linux and macOS nothing beyond the Include
// line, which status checks on every OS.
func sshClientWarnings(*Invocation) (home, client *string) {
	return nil, nil
}
