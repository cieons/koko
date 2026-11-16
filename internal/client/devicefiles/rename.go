package devicefiles

import (
	"os"
	"time"
)

// heldWait bounds how long Rename and ReadFile wait for another handle to
// let go of the file. A reader, or a rename over it, holds it for moments.
const heldWait = 2 * time.Second

// Rename renames oldpath to newpath, replacing any file there, as os.Rename
// does, for every file koko writes atomically. On Windows a rename over a
// file is refused while another handle holds the file open without
// FILE_SHARE_DELETE, which Go's os.Open never passes, or while another
// rename over it is under way, as when koko device revoke and the Device
// Mux write the revocation record at once (#183). Rename then tries again,
// for up to heldWait, as Go's own toolchain does.
func Rename(oldpath, newpath string) error {
	return whileHeld(func() error { return os.Rename(oldpath, newpath) })
}

// ReadFile reads the file at path, as os.ReadFile does, for every file koko
// writes atomically through Rename. On Windows a rename holds the file it
// puts in place open with delete access while it runs, and the file it
// replaces is being deleted, so an open of either that does not share
// delete, as Go's never does, is refused for that moment: as a sharing
// violation, or as access denied (#234). ReadFile then tries again, for up
// to heldWait, as Go's own toolchain does.
func ReadFile(path string) ([]byte, error) {
	var b []byte
	err := whileHeld(func() (err error) {
		b, err = os.ReadFile(path)
		return err
	})
	return b, err
}

// whileHeld runs op, and runs it again while it fails because another
// handle holds its file (heldOpen), for up to heldWait.
func whileHeld(op func() error) error {
	deadline := time.Now().Add(heldWait)
	for wait := time.Millisecond; ; wait = min(2*wait, 100*time.Millisecond) {
		err := op()
		if err == nil || !heldOpen(err) || time.Now().After(deadline) {
			return err
		}
		time.Sleep(wait)
	}
}
