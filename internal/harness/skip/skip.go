// Package skip is the one way a test is left out on an OS koko's tests run
// on. It depends on nothing of koko's, so a test inside any package can
// call it.
package skip

import (
	"runtime"
	"testing"
)

// OnWindows ends the test as skipped when it runs on Windows, saying why
// it cannot hold there, such as "a Unix signal sent to a process". A test
// is left out on Windows only through it, never by a silent skip: a test
// counts its call sites (internal.TestTheTestsLeftOutOnWindowsAreCounted),
// so the list cannot grow unseen. A reason that names a ticket goes when
// the ticket lands.
func OnWindows(t testing.TB, reason string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skipf("does not hold on Windows: %s", reason)
	}
}
