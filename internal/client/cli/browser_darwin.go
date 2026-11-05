package cli

// browserOpener is the command that opens address in the default browser.
func browserOpener(address string) (string, []string, error) {
	return "open", []string{address}, nil
}
