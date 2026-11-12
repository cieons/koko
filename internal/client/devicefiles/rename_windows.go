package devicefiles

import (
	"errors"

	"golang.org/x/sys/windows"
)

// heldOpen tells whether a rename failed because another handle holds the
// file it replaces, or another rename over it is under way: Windows says
// so as access denied, or as a sharing violation. A refusal for want of
// access looks the same, and fails once Rename's wait is over.
func heldOpen(err error) bool {
	return errors.Is(err, windows.ERROR_ACCESS_DENIED) || errors.Is(err, windows.ERROR_SHARING_VIOLATION)
}
