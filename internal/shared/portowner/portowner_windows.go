package portowner

import (
	"context"
	"errors"
)

// lookup is not written for Windows, where koko runs no command that needs
// it until P1.
func lookup(context.Context, int) (Process, error) {
	return Process{}, errors.New("naming the process on a port is not supported on Windows")
}
