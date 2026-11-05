// Package portowner names the process listening on a TCP port. koko web and
// koko server install each refuse a taken port by naming who holds it,
// rather than moving to another port and breaking a bookmark or a firewall
// rule (P0 spec: overview, P0 spec: install and run).
//
// The lookup is per OS: /proc on Linux, lsof on macOS. Windows has none yet.
package portowner

import (
	"cmp"
	"context"
	"errors"
	"fmt"
)

// Process is a process listening on a port. A field is zero when the OS does
// not tell it to this user, such as the pid of another user's process.
type Process struct {
	PID int
	// Name is its command name, which the OS may cut short.
	Name string
	// User is the OS user it runs as.
	User string
}

// String names the process for a person, such as "nginx (pid 812, user
// www-data)".
func (p Process) String() string {
	name := cmp.Or(p.Name, "a process")
	switch {
	case p.PID != 0 && p.User != "":
		return fmt.Sprintf("%s (pid %d, user %s)", name, p.PID, p.User)
	case p.PID != 0:
		return fmt.Sprintf("%s (pid %d)", name, p.PID)
	case p.User != "":
		return "a process of user " + p.User
	}
	return "a process koko cannot name"
}

// ErrNotFound means no process this user can see listens on the port.
var ErrNotFound = errors.New("no process this user can see listens on the port")

// Lookup returns the process listening on TCP port on any local address,
// IPv4 or IPv6, or ErrNotFound.
func Lookup(ctx context.Context, port int) (Process, error) {
	if port < 1 || port > 65535 {
		return Process{}, fmt.Errorf("look up the process on port %d: not a TCP port", port)
	}
	p, err := lookup(ctx, port)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return Process{}, fmt.Errorf("look up the process on port %d: %w", port, err)
	}
	return p, err
}
