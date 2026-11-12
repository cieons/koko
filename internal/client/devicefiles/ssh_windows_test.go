package devicefiles_test

// kokoFile is the name a PATH lookup of koko finds on Windows.
const kokoFile = "koko.exe"

// goldenSuffix ends the name of a golden file of koko's ssh_config on
// Windows, whose UserKnownHostsFile line differs.
const goldenSuffix = "-windows"

// wantKnownHosts is the UserKnownHostsFile argument koko's ssh_config gives
// on Windows, whatever the home directory: its known_hosts under ~.
func wantKnownHosts(string) string {
	return "~/.ssh/koko/known_hosts"
}
