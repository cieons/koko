package devicefiles

// knownHostsArg returns the UserKnownHostsFile argument of koko's ssh_config:
// on Windows koko's known_hosts under ~, whatever the home directory. Both
// ssh clients expand ~ to the home whose .ssh/config they read, OpenSSH for
// Windows to the user's profile directory, which is where koko writes, and
// Git for Windows' to HOME; and a path with no drive letter raises no
// question of quoting or of how Git's client reads "C:" (ADR 0051).
func knownHostsArg(string) (string, error) {
	return "~/" + kokoDir + "/" + knownHostsFile, nil
}
