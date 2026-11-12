package cli

import "strings"

// sameEnvKey tells whether a and b name the same environment variable.
// Windows ignores case in a variable's name, so UserProfile is USERPROFILE.
func sameEnvKey(a, b string) bool {
	return strings.EqualFold(a, b)
}
