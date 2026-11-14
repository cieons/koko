package harness

import (
	"context"
	"net"
)

// listening says whether something listens on addr, a loopback TCP address,
// by listening there itself. Go makes no socket inheritable on Windows, so
// no child process keeps the probe past its close, as one may on Unix
// (#224), and a dial that nothing answers can take Windows a while to
// refuse.
func listening(ctx context.Context, addr string) bool {
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp4", addr)
	if err != nil {
		return true
	}
	_ = ln.Close()
	return false
}
