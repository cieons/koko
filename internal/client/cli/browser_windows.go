package cli

import "errors"

// browserOpener is not written for Windows, whose CLI is P1.
func browserOpener(string) (string, []string, error) {
	return "", nil, errors.New("opening a browser is not supported on Windows")
}
