package portowner

import "context"

// lookup is not written for Windows: naming the process that holds a port
// there is out of scope (P1 spec: the koko CLI on Windows).
func lookup(context.Context, int) (Process, error) {
	return Process{}, ErrUnsupported
}
