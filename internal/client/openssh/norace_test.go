//go:build unix && !race

package openssh_test

// raceEnabled says the test runs with -race, so buildKoko builds koko with
// it too.
const raceEnabled = false
