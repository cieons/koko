package portowner

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
)

// tcpListen is the state /proc/net/tcp gives a listening socket.
const tcpListen = "0A"

// lookup finds the listening socket on port in /proc/net/tcp and tcp6, then
// the process holding it among the /proc/<pid>/fd links this user may read.
// Another user's process keeps its fds to itself, so it is named by its user
// alone.
func lookup(ctx context.Context, port int) (Process, error) {
	sockets := map[string]string{} // inode to uid
	for _, file := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		if err := listening(file, port, sockets); err != nil {
			return Process{}, err
		}
	}
	if len(sockets) == 0 {
		return Process{}, ErrNotFound
	}
	pids, err := os.ReadDir("/proc")
	if err != nil {
		return Process{}, err
	}
	for _, d := range pids {
		if ctx.Err() != nil {
			return Process{}, ctx.Err()
		}
		pid, err := strconv.Atoi(d.Name())
		if err != nil {
			continue
		}
		inode, ok := holds(pid, sockets)
		if !ok {
			continue
		}
		// A name that cannot be read stays unknown: the pid still names it.
		comm, _ := os.ReadFile(filepath.Join("/proc", d.Name(), "comm"))
		return Process{PID: pid, Name: strings.TrimSpace(string(comm)), User: userName(sockets[inode])}, nil
	}
	for _, uid := range sockets {
		return Process{User: userName(uid)}, nil
	}
	return Process{}, ErrNotFound
}

// listening adds to sockets the inode and owner uid of each socket in file
// (/proc/net/tcp's format) listening on port.
func listening(file string, port int, sockets map[string]string) error {
	f, err := os.Open(file)
	if errors.Is(err, fs.ErrNotExist) {
		return nil // no IPv6 on this machine
	}
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	s := bufio.NewScanner(f)
	s.Scan() // the header
	for s.Scan() {
		// sl local_address rem_address st tx:rx tr:when retrnsmt uid timeout inode ...
		fields := strings.Fields(s.Text())
		if len(fields) < 10 || fields[3] != tcpListen {
			continue
		}
		_, hexPort, ok := strings.Cut(fields[1], ":")
		p, err := strconv.ParseUint(hexPort, 16, 16)
		if !ok || err != nil {
			return fmt.Errorf("read %s: the local address %q does not parse", file, fields[1])
		}
		if int(p) != port {
			continue
		}
		sockets[fields[9]] = fields[7]
	}
	if err := s.Err(); err != nil {
		return fmt.Errorf("read %s: %w", file, err)
	}
	return nil
}

// holds tells whether process pid has one of sockets open, and which.
func holds(pid int, sockets map[string]string) (string, bool) {
	dir := filepath.Join("/proc", strconv.Itoa(pid), "fd")
	fds, err := os.ReadDir(dir)
	if err != nil {
		return "", false // gone, or another user's
	}
	for _, fd := range fds {
		link, err := os.Readlink(filepath.Join(dir, fd.Name()))
		if err != nil {
			continue
		}
		if inode, ok := strings.CutPrefix(link, "socket:["); ok {
			inode = strings.TrimSuffix(inode, "]")
			if _, ok := sockets[inode]; ok {
				return inode, true
			}
		}
	}
	return "", false
}

// userName is the name of the user with uid, or "uid <n>" when it has none.
func userName(uid string) string {
	if u, err := user.LookupId(uid); err == nil {
		return u.Username
	}
	return "uid " + uid
}
