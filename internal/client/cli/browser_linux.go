package cli

// browserOpener is the command that opens address in the desktop's browser.
func browserOpener(address string) (string, []string, error) {
	return "xdg-open", []string{address}, nil
}
