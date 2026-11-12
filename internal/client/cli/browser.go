package cli

import (
	"context"
	"fmt"
)

// openBrowser opens address in the user's browser in this OS's way
// (openAddress), or through the browser hook. It returns at once, with a
// function that waits for the opening to end, which koko web calls before it
// exits itself. When the address cannot be opened, it says so on stderr:
// koko web serves on, and the address it printed can be opened by hand.
func openBrowser(ctx context.Context, in *Input, address string) (wait func()) {
	open := in.runner.OpenBrowser
	if open == nil {
		open = func(address string) error { return openAddress(ctx, in.inv, address) }
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := open(address); err != nil {
			_, _ = fmt.Fprintf(in.inv.Stderr, "koko: could not open a browser (%v); open the address above in one, or run koko web --no-open\n", err)
		}
	}()
	return func() { <-done }
}
