package devicemux

import (
	"context"
	"fmt"
	"os"
	"sync"

	"golang.org/x/sys/unix"
)

// watchRoutes watches the device's network routes on a routing socket,
// until ctx ends, reading it in a goroutine wg tracks.
func watchRoutes(ctx context.Context, wg *sync.WaitGroup) (<-chan struct{}, error) {
	fd, err := unix.Socket(unix.AF_ROUTE, unix.SOCK_RAW, unix.AF_UNSPEC)
	if err != nil {
		return nil, fmt.Errorf("open a routing socket: %w", err)
	}
	unix.CloseOnExec(fd)
	if err := unix.SetNonblock(fd, true); err != nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("open a routing socket: %w", err)
	}
	return readRoutes(ctx, wg, os.NewFile(uintptr(fd), "routing socket")), nil
}

// routeChanged tells whether msg, one routing message, says a route, an
// address or an interface changed; lookups and misses say nothing new.
func routeChanged(msg []byte) bool {
	if len(msg) < 4 {
		return false
	}
	switch msg[3] { // rt_msghdr's rtm_type
	case unix.RTM_ADD, unix.RTM_DELETE, unix.RTM_CHANGE, unix.RTM_NEWADDR, unix.RTM_DELADDR, unix.RTM_IFINFO:
		return true
	}
	return false
}
