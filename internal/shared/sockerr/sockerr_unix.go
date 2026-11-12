//go:build !windows

package sockerr

import (
	"errors"
	"syscall"
)

func is(err error, errno syscall.Errno) bool {
	return errors.Is(err, errno)
}
