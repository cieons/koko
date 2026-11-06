//go:build !linux && !darwin

package devicemux

import (
	"context"
	"fmt"
	"runtime"
	"sync"
)

// watchRoutes has no route-change watch outside Linux and macOS, where no
// Device Mux runs yet (Windows is P1).
func watchRoutes(context.Context, *sync.WaitGroup) (<-chan struct{}, error) {
	return nil, fmt.Errorf("the Device Mux is not supported on %s yet", runtime.GOOS)
}
