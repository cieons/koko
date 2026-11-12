package devicefiles

import (
	"os"
	"time"
)

// renameWait bounds how long Rename waits for the file it replaces to be
// let go of. A reader, or another rename over it, holds it for moments.
const renameWait = 2 * time.Second

// Rename renames oldpath to newpath, replacing any file there, as os.Rename
// does, for every file koko writes atomically. On Windows a rename over a
// file is refused while another handle holds the file open without
// FILE_SHARE_DELETE, which Go's os.Open never passes, or while another
// rename over it is under way, as when koko device revoke and the Device
// Mux write the revocation record at once (#183). Rename then tries again,
// for up to renameWait, as Go's own toolchain does.
func Rename(oldpath, newpath string) error {
	deadline := time.Now().Add(renameWait)
	for wait := time.Millisecond; ; wait = min(2*wait, 100*time.Millisecond) {
		err := os.Rename(oldpath, newpath)
		if err == nil || !heldOpen(err) || time.Now().After(deadline) {
			return err
		}
		time.Sleep(wait)
	}
}
