//go:build !linux && !darwin

package devicemux

import (
	"fmt"
	"runtime"
)

// runtimeVar is empty outside Linux and macOS, where no Device Mux runs yet
// (Windows is P1): commands there open their own Device Connection.
const runtimeVar = ""

// peerUIDOf cannot read a peer's credentials outside Linux and macOS.
func peerUIDOf(uintptr) (int, error) {
	return 0, fmt.Errorf("the Device Mux is not supported on %s yet", runtime.GOOS)
}
