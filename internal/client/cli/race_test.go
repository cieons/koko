//go:build race

package cli_test

// raceEnabled says the test runs with -race, so the go test that
// TestWebServesThePlaceholderInABuildWithoutTheWebUI runs does too.
const raceEnabled = true
