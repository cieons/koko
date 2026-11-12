// Package sockerr tells which error a socket call failed with, on every OS
// koko builds for. Code names the error as Linux and macOS number it, such
// as syscall.ECONNREFUSED; Windows' sockets fail with numbers of their own
// (WSAECONNREFUSED), which the standard library does not match to those.
package sockerr

import "syscall"

// Is tells whether err is, or wraps, one of errnos, each named as Linux and
// macOS number it. On Windows it matches Windows' own number for the same
// failure too.
func Is(err error, errnos ...syscall.Errno) bool {
	for _, errno := range errnos {
		if is(err, errno) {
			return true
		}
	}
	return false
}
