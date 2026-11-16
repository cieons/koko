//go:build !windows

package devicefiles

// heldOpen tells whether a rename or a read failed because another handle
// holds the file: never on Linux and macOS, where a rename replaces a file
// whoever has it open, and a file being renamed opens as any other.
func heldOpen(error) bool { return false }
