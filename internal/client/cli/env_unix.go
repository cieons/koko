//go:build !windows

package cli

// sameEnvKey tells whether a and b name the same environment variable: on
// Linux and macOS, only when they are the same bytes.
func sameEnvKey(a, b string) bool {
	return a == b
}
