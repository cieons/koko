package devicemux

import (
	"testing"
)

// The route-change watch registers for Windows' route and address
// notifications and cancels both without error, twice over in one process,
// as the Device Muxes a test process runs one after another do.
func TestTheRouteWatchRegistersAndCancels(t *testing.T) {
	for range 2 {
		cancel, err := notifyRouteChanges(make(chan struct{}, 1))
		if err != nil {
			t.Fatalf("register for route changes: %v", err)
		}
		if err := cancel(); err != nil {
			t.Errorf("cancel the route-change watch: %v", err)
		}
	}
	routeWatchers.mu.Lock()
	n := len(routeWatchers.set)
	routeWatchers.mu.Unlock()
	if n != 0 {
		t.Errorf("%d watchers are left once every watch was cancelled, want none", n)
	}
}
