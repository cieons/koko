package devicemux

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/cieons/koko/internal/shared/build"
)

// Arg is the one argument koko runs a Device Mux with, `koko device-mux`:
// the entry point Spawn starts this binary at. It is not a command, so the
// command tree, help and koko skill never list it.
const Arg = "device-mux"

// maxSocketPath is the longest socket path koko uses: a Unix socket's path
// must stay under 104 bytes on macOS, and under 108 on Linux.
const maxSocketPath = 103

// maxLog is the size past which a Device Mux's log is emptied when the next
// one of its build starts.
const maxLog = 1 << 20

// Socket returns the path of this build's Device Mux socket on a device
// whose environment getenv reads, making koko's runtime directory when it is
// missing: $XDG_RUNTIME_DIR/koko/ on Linux, $TMPDIR/koko/ on macOS, and
// /tmp/koko-<uid>/ without that variable. The directory must be this OS
// user's, mode 0700. It fails, so that a command opens its own Device
// Connection, when the directory is anything else, when the OS has no Device
// Mux yet, or when the path would be too long for a Unix socket.
func Socket(getenv func(string) string) (string, error) {
	dir, err := runtimeDir(getenv)
	if err != nil {
		return "", err
	}
	if err := ensurePrivate(dir); err != nil {
		return "", err
	}
	path := filepath.Join(dir, Name(build.Read())+".sock")
	if len(path) > maxSocketPath {
		return "", fmt.Errorf("the Device Mux socket %s would be %d bytes long, past the %d a Unix socket's path may hold", path, len(path), maxSocketPath)
	}
	return path, nil
}

// Sockets returns the sockets of every Device Mux of this OS user, of every
// build and every home, in koko's runtime directory (Socket), for koko
// disconnect: what a Device Mux that runs listens on, and what one that
// crashed left. It fails when the directory is refused, or the OS has no
// Device Mux yet.
func Sockets(getenv func(string) string) ([]string, error) {
	dir, err := runtimeDir(getenv)
	if err != nil {
		return nil, err
	}
	if err := ensurePrivate(dir); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("list the runtime directory %s: %w", dir, err)
	}
	var sockets []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".sock") {
			sockets = append(sockets, filepath.Join(dir, e.Name()))
		}
	}
	return sockets, nil
}

// Name is the name of build b's Device Mux, which its socket, lock and log
// carry: the version, then the start of the VCS revision unless the version
// holds it already, as a pseudo-version does. So a CLI reaches only a Device
// Mux of its own build, and the local protocol needs no compatibility rules
// (ADR 0017). Anything a file name should not hold becomes "_".
func Name(b build.Info) string {
	n := b.Version
	if rev := b.Revision[:min(len(b.Revision), 12)]; rev != "" && !strings.Contains(n, rev) {
		n += "-" + rev
	}
	return strings.Map(func(r rune) rune {
		switch {
		case 'a' <= r && r <= 'z', 'A' <= r && r <= 'Z', '0' <= r && r <= '9', r == '.', r == '-', r == '+':
			return r
		}
		return '_'
	}, n)
}

// ensurePrivate makes dir, mode 0700, when it is missing, and refuses it
// unless it is a directory, not a symlink, that this OS user owns with mode
// 0700: anyone else who could write there could stand in for the Device Mux.
func ensurePrivate(dir string) error {
	if !filepath.IsAbs(dir) {
		return fmt.Errorf("the runtime directory %s is not an absolute path", dir)
	}
	if err := os.Mkdir(dir, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
		return fmt.Errorf("make the runtime directory %s: %w", dir, err)
	}
	fi, err := os.Lstat(dir)
	switch {
	case err != nil:
		return fmt.Errorf("check the runtime directory %s: %w", dir, err)
	case !fi.IsDir():
		return fmt.Errorf("the runtime directory %s is not a directory", dir)
	case !ownedByMe(fi):
		return fmt.Errorf("the runtime directory %s belongs to another user", dir)
	case fi.Mode().Perm() != 0o700:
		return fmt.Errorf("the runtime directory %s has mode %04o, not 0700", dir, fi.Mode().Perm())
	}
	return nil
}

// lockPath and logPath are the lock and the log of the Device Mux whose
// socket is at socket, beside it.
func lockPath(socket string) string { return strings.TrimSuffix(socket, ".sock") + ".lock" }
func logPath(socket string) string  { return strings.TrimSuffix(socket, ".sock") + ".log" }

// Spawn starts this binary as the Device Mux of socket, with the environment
// env, and does not wait for it to listen: detached from the terminal, in a
// session of its own, so neither a closed terminal nor ssh ending its
// ProxyCommand ends it, with stdin from /dev/null, and its log, with anything
// the Go runtime prints such as a crash, going to the log file beside its
// socket. It outlives ctx, the command's. The channel it returns closes
// when the Device Mux exits while the command still runs, as in a sandbox
// that lets it start but not listen. cmd/koko gives it to
// cli.Runner.StartDeviceMux.
func Spawn(ctx context.Context, socket string, env []string) (exited <-chan struct{}, err error) {
	attr, err := detached()
	if err != nil {
		return nil, err
	}
	exe, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("find the running koko binary: %w", err)
	}
	log, err := openLog(logPath(socket))
	if err != nil {
		return nil, err
	}
	defer func() { _ = log.Close() }()
	cmd := exec.CommandContext(context.WithoutCancel(ctx), exe, Arg)
	cmd.Env, cmd.Dir = env, "/"
	cmd.Stdout, cmd.Stderr = log, log
	cmd.SysProcAttr = attr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start the Device Mux: %w", err)
	}
	done := make(chan struct{})
	go func() {
		// Reaps it, should it exit while this command still runs.
		_ = cmd.Wait()
		close(done)
	}()
	return done, nil
}

// openLog opens the Device Mux log at path for appending, mode 0600, emptied
// first when it has grown past maxLog. Appending, rather than starting a new
// log, keeps the lines of a Device Mux that won the race to start when its
// loser starts after it.
func openLog(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open the Device Mux log: %w", err)
	}
	if fi, err := f.Stat(); err == nil && fi.Size() > maxLog {
		_ = f.Truncate(0)
	}
	return f, nil
}
