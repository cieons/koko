//go:build !windows

package harness

// osEnv are the variables a device's commands get on Linux and macOS beside
// its home and LOCALAPPDATA: none, so that a test decides PATH, as those
// that put a fake browser opener on it do.
func osEnv() []string {
	return nil
}
