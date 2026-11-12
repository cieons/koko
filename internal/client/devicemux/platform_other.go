//go:build !linux && !darwin && !windows

package devicemux

import (
	"fmt"
	"net"
	"runtime"
	"time"
)

// runtimeDir: no Device Mux runs outside Linux, macOS and Windows, so
// commands there open their own Device Connection.
func runtimeDir(func(string) string) (string, error) {
	return "", fmt.Errorf("the Device Mux is not supported on %s", runtime.GOOS)
}

func makeRuntimeDir(string) error {
	return fmt.Errorf("the Device Mux is not supported on %s", runtime.GOOS)
}

const watchLocal = false

func sameUser(net.Conn) error {
	return fmt.Errorf("the Device Mux is not supported on %s", runtime.GOOS)
}

func slept(last, now time.Time, _ time.Duration) time.Duration {
	return now.Round(0).Sub(last.Round(0)) - now.Sub(last)
}
