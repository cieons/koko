//go:build linux || darwin

package devicemux

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/cieons/koko/internal/client/devicefiles"
)

// runtimeDir is koko's runtime directory: koko/ in the user's private
// directory that runtimeVar names on this OS, or /tmp/koko-<uid>/ without
// it, which ensurePrivate refuses unless it is this user's, mode 0700.
func runtimeDir(getenv func(string) string) (string, error) {
	if d := getenv(runtimeVar); d != "" {
		return filepath.Join(d, "koko"), nil
	}
	return fmt.Sprintf("/tmp/koko-%d", os.Getuid()), nil
}

// makeRuntimeDir makes the runtime directory dir, mode 0700. It fails
// wrapping fs.ErrExist when dir is there already.
func makeRuntimeDir(dir string) error {
	return devicefiles.MkdirPrivate(dir)
}

// watchLocal tells whether both ends of a command's connection to the
// Device Mux watch it for its end (liveness.Conn.WatchAnswers): not on
// Linux and macOS, where a Unix socket tells each end when the other
// closes it.
const watchLocal = false

// sameUser admits nc, a connection to the Device Mux's socket, only from a
// process of this OS user, by the peer credentials the kernel recorded as
// it connected.
func sameUser(nc net.Conn) error {
	uid, err := peerUID(nc)
	if err != nil {
		return err
	}
	if uid != os.Getuid() {
		return fmt.Errorf("its peer runs as OS user %d", uid)
	}
	return nil
}

// peerUID returns the OS user id of the process at the other end of nc, a
// Unix socket connection, as the kernel recorded it when it connected.
func peerUID(nc net.Conn) (int, error) {
	uc, ok := nc.(*net.UnixConn)
	if !ok {
		return 0, fmt.Errorf("read the peer's credentials: %T is not a Unix socket connection", nc)
	}
	raw, err := uc.SyscallConn()
	if err != nil {
		return 0, fmt.Errorf("read the peer's credentials: %w", err)
	}
	var uid int
	var uidErr error
	if err := raw.Control(func(fd uintptr) { uid, uidErr = peerUIDOf(fd) }); err != nil {
		return 0, fmt.Errorf("read the peer's credentials: %w", err)
	}
	if uidErr != nil {
		return 0, fmt.Errorf("read the peer's credentials: %w", uidErr)
	}
	return uid, nil
}

// slept returns how long the device slept between two looks at the clocks,
// last and now: how much further the wall clock moved than the monotonic
// one, which stands still while Linux and macOS sleep.
func slept(last, now time.Time, _ time.Duration) time.Duration {
	return now.Round(0).Sub(last.Round(0)) - now.Sub(last)
}
