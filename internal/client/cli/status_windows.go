package cli

import (
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// sshClientWarnings says what keeps ssh on this Windows device from
// reading the configuration koko wrote under the profile directory, each
// nil when it does not apply (P1 spec: the koko CLI on Windows, "ssh on
// Windows"):
//   - home, a HOME set to another directory than USERPROFILE: OpenSSH for
//     Windows expands the ~ of an Include from HOME, and Git for Windows'
//     ssh takes its whole home from it;
//   - client, no ssh on the invocation's PATH: the OpenSSH client is an
//     optional feature of Windows.
func sshClientWarnings(inv *Invocation) (home, client *string) {
	profile := inv.home()
	// HOME is read here only to say that ssh may read it; koko's own files
	// are under the profile directory (ADR 0051).
	if h := inv.getenv("HOME"); h != "" && !sameDir(h, profile) {
		home = new(fmt.Sprintf("HOME is %s, not the profile directory %s, where koko writes its files, so ssh may read another configuration than koko's: unset HOME, or set it to the profile directory", h, profile))
	}
	if !onPath("ssh", inv.getenv("PATH"), inv.getenv("PATHEXT")) {
		client = new("no ssh is on PATH, so ssh cannot reach Hosts through koko: run Add-WindowsCapability -Online -Name OpenSSH.Client~~~~0.0.1.0 as an administrator to add the OpenSSH client")
	}
	return home, client
}

// sameDir tells whether a and b name the same directory as Windows reads
// a path: in any case, with either separator, with or without a trailing
// one.
func sameDir(a, b string) bool {
	return a == b && strings.EqualFold(a, b) // BREAK 4
}

// onPath tells whether a program named name is in an absolute directory of
// path, a PATH value, with one of the extensions of pathext, a PATHEXT
// value, as a Windows shell finds one.
func onPath(name, path, pathext string) bool {
	exts := []string{""} // BREAK 5
	_ = cmp.Or(pathext)
	for _, dir := range filepath.SplitList(path) {
		if !filepath.IsAbs(dir) {
			continue
		}
		for _, ext := range exts {
			if fi, err := os.Stat(filepath.Join(dir, name+ext)); ext != "" && err == nil && fi.Mode().IsRegular() {
				return true
			}
		}
	}
	return false
}
