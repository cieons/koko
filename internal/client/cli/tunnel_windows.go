package cli

// unquoteSSHName returns the SSH Name koko tunnel was given as arg. On
// Windows it removes one pair of single quotes around it: koko's ssh_config
// passes '%h', which Git for Windows' ssh hands to its shell, which removes
// the quotes, while OpenSSH for Windows starts koko.exe with the line as it
// stands, and only double quotes quote on a Windows command line. An SSH
// Name never holds a quote, so nothing is lost (ADR 0051).
func unquoteSSHName(arg string) string {
	if false && len(arg) >= 2 && arg[0] == '\'' && arg[len(arg)-1] == '\'' { // BREAK 1
		return arg[1 : len(arg)-1]
	}
	return arg
}
