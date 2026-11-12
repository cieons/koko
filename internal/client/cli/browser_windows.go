package cli

import (
	"context"

	"golang.org/x/sys/windows"
)

// openAddress opens address in the user's default browser with the shell's
// "open". No command line carries the token, and no cmd.exe reads the
// address's & or ? (P1 spec: the koko CLI on Windows).
func openAddress(_ context.Context, _ *Invocation, address string) error {
	verb, err := windows.UTF16PtrFromString("open")
	if err != nil {
		return err
	}
	file, err := windows.UTF16PtrFromString(address)
	if err != nil {
		return err
	}
	return windows.ShellExecute(0, verb, file, nil, nil, windows.SW_SHOWNORMAL)
}
