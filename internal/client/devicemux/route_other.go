//go:build !linux && !darwin && !windows

package devicemux

import (
	"context"
	"fmt"
	"runtime"
	"sync"
)

// watchRoutes has no route-change watch outside Linux, macOS and Windows,
// where no Device Mux runs.
func watchRoutes(context.Context, *sync.WaitGroup) (<-chan struct{}, error) {
	return nil, fmt.Errorf("the Device Mux is not supported on %s", runtime.GOOS)
}
