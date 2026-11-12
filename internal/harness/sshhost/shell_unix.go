//go:build !windows

package sshhost

// shell returns the Host Account's shell, which runs a session's command.
func shell() (string, error) {
	return "/bin/sh", nil
}
