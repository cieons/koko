//go:build linux || darwin

package devicemux

import (
	"context"
	"os"
	"sync"
)

// readRoutes reads the route socket f until ctx ends, in a goroutine wg
// tracks, and returns a channel that receives each time a message it reads
// tells of a change of the device's network routes (routeChanged), at most
// one waiting.
func readRoutes(ctx context.Context, wg *sync.WaitGroup, f *os.File) <-chan struct{} {
	changed := make(chan struct{}, 1)
	stop := context.AfterFunc(ctx, func() { _ = f.Close() })
	wg.Go(func() {
		defer stop()
		defer func() { _ = f.Close() }()
		buf := make([]byte, 1<<16)
		for {
			n, err := f.Read(buf)
			if err != nil {
				return
			}
			if !routeChanged(buf[:n]) {
				continue
			}
			select {
			case changed <- struct{}{}:
			default:
			}
		}
	})
	return changed
}
