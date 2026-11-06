package devicemux

import (
	"context"
	"fmt"
	"os"
	"sync"

	"golang.org/x/sys/unix"
)

// watchRoutes watches the device's links, addresses and routes on a
// netlink socket, until ctx ends.
func watchRoutes(ctx context.Context, wg *sync.WaitGroup) (<-chan struct{}, error) {
	fd, err := unix.Socket(unix.AF_NETLINK, unix.SOCK_RAW|unix.SOCK_CLOEXEC|unix.SOCK_NONBLOCK, unix.NETLINK_ROUTE)
	if err != nil {
		return nil, fmt.Errorf("open a netlink socket: %w", err)
	}
	groups := uint32(unix.RTMGRP_LINK | unix.RTMGRP_IPV4_IFADDR | unix.RTMGRP_IPV6_IFADDR | unix.RTMGRP_IPV4_ROUTE | unix.RTMGRP_IPV6_ROUTE)
	if err := unix.Bind(fd, &unix.SockaddrNetlink{Family: unix.AF_NETLINK, Groups: groups}); err != nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("listen for route changes on netlink: %w", err)
	}
	return readRoutes(ctx, wg, os.NewFile(uintptr(fd), "netlink socket")), nil
}

// routeChanged tells whether msg says a route changed: every message of the
// groups watchRoutes joined does.
func routeChanged(msg []byte) bool {
	return len(msg) > 0
}
