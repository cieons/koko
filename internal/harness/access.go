package harness

import (
	"os"
	"testing"

	"github.com/cieons/koko/internal/client/devicefiles"
)

// HomeEnv returns the environment entry that makes home the home directory
// of a koko command line on this OS: HOME=home on Linux and macOS,
// USERPROFILE=home on Windows, where koko never reads HOME (ADR 0051).
func HomeEnv(home string) string {
	return devicefiles.HomeVar + "=" + home
}

// WantPrivate fails the test unless the directory or regular file at path
// is private to this OS user, as koko keeps a device's files: a directory
// of mode 0700 or a file of mode 0600 on Linux and macOS, and on Windows,
// which keeps no modes, this user's own, with a protected access control
// list for this user, SYSTEM and Administrators only (ADR 0051).
func WantPrivate(t testing.TB, path string) {
	t.Helper()
	fi, err := os.Lstat(path)
	if err != nil {
		t.Errorf("%s is not there to be private: %v", path, err)
		return
	}
	if !fi.IsDir() && !fi.Mode().IsRegular() {
		t.Errorf("%s is neither a directory nor a regular file: %v", path, fi.Mode())
		return
	}
	if why := notPrivate(path, fi); why != "" {
		t.Errorf("%s is not private to this user: %s", path, why)
	}
}

// LetOthersIn makes the file or directory at path readable by other users,
// so that it is no longer private to this OS user, as a file made before
// koko, or loosened since, may be: on Linux and macOS its mode gains read
// for the group and others, and on Windows its access control list gains
// an entry for Everyone.
func LetOthersIn(t testing.TB, path string) {
	t.Helper()
	fi, err := os.Lstat(path)
	if err == nil {
		err = letOthersIn(path, fi)
	}
	if err != nil {
		t.Fatalf("let other users into %s: %v", path, err)
	}
}

// Access describes who may reach the file or directory at path, for a test
// to compare before and after koko rewrote it: its mode on Linux and macOS,
// its access control list on Windows.
func Access(t testing.TB, path string) string {
	t.Helper()
	fi, err := os.Lstat(path)
	var a string
	if err == nil {
		a, err = access(path, fi)
	}
	if err != nil {
		t.Fatalf("read who may reach %s: %v", path, err)
	}
	return a
}
