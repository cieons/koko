package server

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"os"
	"syscall"
	"time"

	"github.com/cieons/koko/internal/server/filesystem"
	"github.com/cieons/koko/internal/shared/install"
)

// socketDialWait bounds the dial that tells a stale socket from a live one.
// A socket file nobody listens on refuses a connection at once, and a live
// koko server accepts one at once, so the wait only bounds a socket that does
// neither, which start-up reads as unknown, never as dead.
const socketDialWait = time.Second

// refuseRoot refuses a koko server whose real, effective or saved uid is 0,
// so that none ever serves as root or could become root again, wherever it
// runs (ADR 0039). It writes nothing, and names the fix.
func refuseRoot(uids func() (real, effective, saved int)) error {
	r, e, s := uids()
	if r != 0 && e != 0 && s != 0 {
		return nil
	}
	return &install.Refusal{Reason: install.Usage, Message: fmt.Sprintf(
		"kokod run does not run as root, and its real, effective and saved uid are %d, %d and %d: start it as a user who is not root, such as the koko user its systemd unit runs as, or, in a container, as the image's uid 65532, without --user 0 or user: root",
		r, e, s)}
}

// checkDataDir refuses a data directory on a network filesystem, where
// SQLite's WAL does not work, and one this process cannot write; before the
// data directory exists, the directory it would be made in is checked
// instead. Neither check leaves anything written (P1 spec: the container
// image, "Start-up on every machine").
func checkDataDir(dataDir string, types filesystem.Types) error {
	dir, err := filesystem.Nearest(dataDir)
	if err != nil {
		return fmt.Errorf("find the data directory %s: %w", dataDir, err)
	}
	fsType, err := types.FilesystemType(dir)
	if err != nil {
		return fmt.Errorf("find the filesystem of the data directory %s: %w", dataDir, err)
	}
	is, where := "is", "the data directory "+dataDir
	if dir != dataDir {
		is, where = "would be", fmt.Sprintf("%s, where it would make the data directory %s", dir, dataDir)
	}
	if filesystem.Network(fsType) {
		return &install.Refusal{Reason: install.Unsupported, Message: fmt.Sprintf(
			"the data directory %s %s on %s, a network filesystem, where SQLite's WAL does not work: put it on a local filesystem",
			dataDir, is, fsType)}
	}
	if err := probeWrite(dir); err != nil {
		return cannotWrite(where, err)
	}
	return nil
}

// cannotWrite is start-up's refusal of a directory where, which probeWrite
// could not write, naming the uid and gid the process runs as.
func cannotWrite(where string, err error) error {
	return fmt.Errorf("the koko server runs as uid %d and gid %d, which cannot write %s: give it to that user and group, or start the koko server as a user who can write it (%w)",
		os.Geteuid(), os.Getegid(), where, err)
}

// probeWrite tells whether this process can make a file in dir by making
// one and removing it at once: the one test that answers for modes, ACLs,
// read-only mounts and security modules alike, on every OS.
func probeWrite(dir string) error {
	f, err := os.CreateTemp(dir, ".koko-write-check-*")
	if err != nil {
		return err
	}
	return errors.Join(f.Close(), os.Remove(f.Name()))
}

// claimSocket frees the socket's path, removing a socket file nobody answers
// on, as a koko server killed without warning leaves behind. Anything else
// there is refused and left as it is, so a live socket is never removed.
func claimSocket(ctx context.Context, path string, log *slog.Logger) error {
	info, err := os.Lstat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil
	case err != nil:
		return fmt.Errorf("look at the socket %s: %w", path, err)
	case info.Mode().Type() != fs.ModeSocket:
		return fmt.Errorf("%s, where the koko server listens, is not a socket: move it away", path)
	}
	d := net.Dialer{Timeout: socketDialWait}
	conn, err := d.DialContext(ctx, "unix", path)
	switch {
	case err == nil:
		_ = conn.Close()
		return &install.Refusal{Reason: install.PortTaken, Message: fmt.Sprintf(
			"something answers on the socket %s, such as a koko server running already: stop it before starting another", path)}
	case errors.Is(err, fs.ErrNotExist):
		return nil
	case !errors.Is(err, syscall.ECONNREFUSED):
		return fmt.Errorf("find whether a koko server answers on the socket %s, which is left as it is: %w", path, err)
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("remove the stale socket %s: %w", path, err)
	}
	log.Info("removed a stale socket nobody answered on", "socket", path)
	return nil
}
