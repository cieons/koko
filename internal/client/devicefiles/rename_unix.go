//go:build !windows

package devicefiles

// heldOpen tells whether a rename failed because another handle holds the
// file it replaces: never on Linux and macOS, where a rename replaces a
// file whoever has it open.
func heldOpen(error) bool { return false }
