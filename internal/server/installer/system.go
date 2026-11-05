package installer

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/cieons/koko/internal/shared/portowner"
)

// System is the machine install runs on, as root.
type System struct{}

var _ Machine = System{}

// Root is "/": install reads and writes the machine's own paths.
func (System) Root() string {
	return "/"
}

// SystemdVersion reads the version systemctl --version prints first.
func (System) SystemdVersion(ctx context.Context) (int, error) {
	out, err := exec.CommandContext(ctx, "systemctl", "--version").Output()
	if err != nil {
		return 0, err
	}
	return parseSystemdVersion(out)
}

// parseSystemdVersion reads systemd's version from what systemctl --version
// prints, whose first line is like "systemd 252 (252.22-1~deb12u1)".
func parseSystemdVersion(out []byte) (int, error) {
	line, _, _ := bytes.Cut(out, []byte("\n"))
	fields := strings.Fields(string(line))
	if len(fields) < 2 || fields[0] != "systemd" {
		return 0, fmt.Errorf("systemctl --version printed %q, not systemd and its version", line)
	}
	digits := fields[1]
	if i := strings.IndexFunc(digits, func(r rune) bool { return r < '0' || r > '9' }); i >= 0 {
		digits = digits[:i]
	}
	version, err := strconv.Atoi(digits)
	if err != nil {
		return 0, fmt.Errorf("systemctl --version printed %q, not systemd and its version", line)
	}
	return version, nil
}

// FilesystemType finds the mount holding path in /proc/self/mountinfo, the
// one whose mount point is the longest prefix of path, and returns its type.
func (System) FilesystemType(path string) (string, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	f, err := os.Open("/proc/self/mountinfo")
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	return mountType(f, resolved)
}

// mountType reads mountinfo (proc(5)'s /proc/<pid>/mountinfo) and returns the
// filesystem type of the mount holding path, which is absolute and resolved.
// A later mount on the same point hides an earlier one, as the kernel lists
// them in the order they were mounted.
func mountType(mountinfo io.Reader, path string) (string, error) {
	best, bestType := "", ""
	s := bufio.NewScanner(mountinfo)
	for s.Scan() {
		// 36 35 98:0 /mnt1 /mnt2 rw,noatime master:1 - ext3 /dev/root rw
		before, after, ok := strings.Cut(s.Text(), " - ")
		fields, typeFields := strings.Fields(before), strings.Fields(after)
		if !ok || len(fields) < 5 || len(typeFields) < 1 {
			return "", fmt.Errorf("a line of mountinfo does not parse: %q", s.Text())
		}
		point := unescapeMount(fields[4])
		if !within(path, point) || len(point) < len(best) {
			continue
		}
		best, bestType = point, typeFields[0]
	}
	if err := s.Err(); err != nil {
		return "", err
	}
	if best == "" {
		return "", fmt.Errorf("no mount holds %s", path)
	}
	return bestType, nil
}

// within tells whether path is dir or below it.
func within(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && filepath.IsLocal(rel)
}

// unescapeMount undoes mountinfo's octal escapes of a space, a tab, a
// newline and a backslash in a path.
func unescapeMount(s string) string {
	r := strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`)
	return r.Replace(s)
}

// PortOwner names the process listening on port, which root sees whatever
// its user.
func (System) PortOwner(ctx context.Context, port int) (portowner.Process, error) {
	return portowner.Lookup(ctx, port)
}

// SourceIP asks the kernel which address it would send from to a
// documentation address (RFC 5737, then RFC 3849), which only the default
// route covers: connecting a UDP socket picks the route and its source
// address and sends nothing. IPv4 comes first.
func (System) SourceIP(ctx context.Context) (netip.Addr, error) {
	var d net.Dialer
	var errs []error
	for _, probe := range []string{"192.0.2.1:9", "[2001:db8::1]:9"} {
		c, err := d.DialContext(ctx, "udp", probe)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		local := c.LocalAddr().(*net.UDPAddr).AddrPort().Addr().Unmap()
		_ = c.Close()
		return local, nil
	}
	return netip.Addr{}, errors.Join(errs...)
}

// AddUser creates name with useradd as a system user with a group of its
// own, no home directory made and no login shell, unless it exists.
func (System) AddUser(ctx context.Context, name string) error {
	_, err := user.Lookup(name)
	if err == nil {
		return nil
	}
	if _, unknown := errors.AsType[user.UnknownUserError](err); !unknown {
		return err
	}
	return run(ctx, "useradd", "--system", "--user-group", "--no-create-home",
		"--home-dir", DataDir, "--shell", "/usr/sbin/nologin", "--comment", "koko server", name)
}

// Chown makes path owned by the user name and that user's primary group.
func (System) Chown(path, name string) error {
	u, err := user.Lookup(name)
	if err != nil {
		return err
	}
	uid, err := strconv.Atoi(u.Uid)
	if err != nil {
		return fmt.Errorf("the user %s has the uid %q", name, u.Uid)
	}
	gid, err := strconv.Atoi(u.Gid)
	if err != nil {
		return fmt.Errorf("the user %s has the gid %q", name, u.Gid)
	}
	return os.Chown(path, uid, gid)
}

// Systemctl runs systemctl with args, giving what it printed when it fails.
func (System) Systemctl(ctx context.Context, args ...string) error {
	return run(ctx, "systemctl", args...)
}

// ServiceShow runs systemctl show --property=<property> --value
// koko.service, giving what systemd printed to stderr when it fails. A unit
// systemd does not know shows its defaults: MainPID 0, no Environment.
func (System) ServiceShow(ctx context.Context, property string) (string, error) {
	out, err := exec.CommandContext(ctx, "systemctl", "show", "--property="+property, "--value", UnitName).Output()
	if err != nil {
		if exit, ok := errors.AsType[*exec.ExitError](err); ok {
			if msg := strings.TrimSpace(string(exit.Stderr)); msg != "" {
				err = fmt.Errorf("%w: %s", err, msg)
			}
		}
		return "", fmt.Errorf("systemctl show %s %s: %w", UnitName, property, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// run runs a command, giving what it printed when it fails.
func run(ctx context.Context, name string, args ...string) error {
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		if msg := strings.TrimSpace(string(out)); msg != "" {
			return fmt.Errorf("%w: %s", err, msg)
		}
		return err
	}
	return nil
}
