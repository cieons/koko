package passkey

// HoldVerified has every sign-in whose answer verifies call hold before its
// write, until the test ends.
func HoldVerified(cleanup func(func()), hold func()) {
	afterVerify = hold
	cleanup(func() { afterVerify = nil })
}
