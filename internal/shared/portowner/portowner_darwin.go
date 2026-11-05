package portowner

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
)

// lsof is macOS's own lsof, named by its path so that PATH cannot swap it.
const lsof = "/usr/sbin/lsof"

// lookup asks lsof for the process listening on port. lsof sees only this
// user's processes unless run as root, and exits 1 when it finds none.
func lookup(ctx context.Context, port int) (Process, error) {
	// -F prints one field per line: p the pid, c the command (+c0: in
	// full), L the login name.
	out, err := exec.CommandContext(ctx, lsof, "-nP", "+c0", "-iTCP:"+strconv.Itoa(port), "-sTCP:LISTEN", "-FpcL").Output()
	if exit, ok := errors.AsType[*exec.ExitError](err); ok && exit.ExitCode() == 1 && len(out) == 0 {
		return Process{}, ErrNotFound
	}
	if err != nil {
		return Process{}, err
	}
	var p Process
	s := bufio.NewScanner(bytes.NewReader(out))
	for s.Scan() {
		line := s.Text()
		if line == "" {
			continue
		}
		switch value := line[1:]; line[0] {
		case 'p':
			if p.PID != 0 {
				return p, nil // the first process is enough
			}
			pid, err := strconv.Atoi(value)
			if err != nil || pid <= 0 {
				return Process{}, fmt.Errorf("lsof printed the pid %q", value)
			}
			p.PID = pid
		case 'c':
			p.Name = value
		case 'L':
			p.User = value
		}
	}
	if p.PID == 0 {
		return Process{}, ErrNotFound
	}
	return p, nil
}
