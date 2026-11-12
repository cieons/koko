package sockerr

import (
	"errors"
	"syscall"

	"golang.org/x/sys/windows"
)

// winsock is the number Windows' sockets fail with where Linux and macOS
// fail with the key. Windows has no EPIPE of its own: a write to a
// connection the other end closed fails there as reset or aborted.
var winsock = map[syscall.Errno]syscall.Errno{
	syscall.EADDRINUSE:   windows.WSAEADDRINUSE,
	syscall.ECONNABORTED: windows.WSAECONNABORTED,
	syscall.ECONNREFUSED: windows.WSAECONNREFUSED,
	syscall.ECONNRESET:   windows.WSAECONNRESET,
	syscall.EHOSTUNREACH: windows.WSAEHOSTUNREACH,
	syscall.ENETUNREACH:  windows.WSAENETUNREACH,
	syscall.ETIMEDOUT:    windows.WSAETIMEDOUT,
}

func is(err error, errno syscall.Errno) bool {
	if errors.Is(err, errno) {
		return true
	}
	own, ok := winsock[errno]
	return ok && errors.Is(err, own)
}
