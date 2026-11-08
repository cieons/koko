// Package filesystem tells which filesystem holds a path, and whether that
// filesystem keeps its files on another machine, where SQLite's WAL does not
// work. The installer checks the data directory with it before it writes
// anything, and the koko server's start-up at every start (P1 spec: the
// container image, "Start-up on every machine").
//
// Both reach the mount table through a Types, so that tests fake it and never
// read the real /proc.
package filesystem

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Types finds the type of the filesystem holding a path.
type Types interface {
	// FilesystemType returns the type of the filesystem holding path, which
	// exists, as the kernel names it: "ext4", "nfs4".
	FilesystemType(path string) (string, error)
}

// Mountinfo is the file holding a mount table in the format of proc(5)'s
// /proc/<pid>/mountinfo.
type Mountinfo string

// System is the running process's mount table, which inside a container
// shows a bind mount's own filesystem.
const System Mountinfo = "/proc/self/mountinfo"

var _ Types = System

// FilesystemType resolves path's symlinks, then finds the mount holding it
// in the table, the one whose mount point is the longest prefix of path, and
// returns its type.
func (m Mountinfo) FilesystemType(path string) (string, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return "", err
	}
	f, err := os.Open(string(m))
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

// networkFilesystems are the filesystem types, as Linux names them, that
// keep their files on another machine, where SQLite's WAL does not work.
var networkFilesystems = []string{
	"9p", "afs", "beegfs", "ceph", "cifs", "coda", "glusterfs", "gpfs", "lustre",
	"ncpfs", "nfs", "nfs4", "pvfs2", "smb3", "smbfs",
	"fuse.ceph-fuse", "fuse.davfs", "fuse.gcsfuse", "fuse.glusterfs",
	"fuse.juicefs", "fuse.rclone", "fuse.s3fs", "fuse.sshfs",
}

// Network tells whether the filesystem type fsType keeps its files on
// another machine.
func Network(fsType string) bool {
	return slices.Contains(networkFilesystems, fsType)
}

// Nearest returns path when it exists, or else the nearest directory above
// it that exists: where making path would begin, and so the directory whose
// filesystem and permissions decide whether it can be made.
func Nearest(path string) (string, error) {
	dir := path
	for {
		_, err := os.Stat(dir)
		if err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if !errors.Is(err, fs.ErrNotExist) || parent == dir {
			return "", err
		}
		dir = parent
	}
}
