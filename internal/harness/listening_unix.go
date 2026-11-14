//go:build !windows

package harness

import (
	"context"
	"errors"
	"net"
	"syscall"
	"time"
)

// listening says whether something listens on addr, a loopback TCP address,
// by dialing it: a refusal means nothing does. A probe that listened there
// itself would hold the port past its close: a child process the test
// process forks while the probe is open, such as an in-process Host's
// shell, keeps a copy of it until the child execs, and the test's own bind
// of the port fails meanwhile (#224).
func listening(ctx context.Context, addr string) bool {
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	var d net.Dialer
	c, err := d.DialContext(ctx, "tcp4", addr)
	if err == nil {
		_ = c.Close()
		return true
	}
	return !errors.Is(err, syscall.ECONNREFUSED)
}
