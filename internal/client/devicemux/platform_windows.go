package devicemux

import (
	"errors"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/cieons/koko/internal/client/devicefiles"
)

// runtimeDir is koko's runtime directory on Windows, %LOCALAPPDATA%\koko\run\:
// local to the machine, since a socket must not roam with a domain profile,
// and private to the user as the device's files are (ADR 0051). Windows
// has no per-user directory that is emptied at logoff; a socket a Device
// Mux left behind is removed by the next one.
func runtimeDir(getenv func(string) string) (string, error) {
	base := getenv("LOCALAPPDATA")
	if base == "" {
		return "", errors.New("LOCALAPPDATA is not set, so koko has no runtime directory")
	}
	return filepath.Join(base, "koko", "run"), nil
}

// makeRuntimeDir makes the runtime directory dir, private to this user from
// the moment it exists, and %LOCALAPPDATA%\koko above it when that is
// missing, which takes the user's own access control list from
// %LOCALAPPDATA%. It fails wrapping fs.ErrExist when dir is there already.
func makeRuntimeDir(dir string) error {
	if err := os.Mkdir(filepath.Dir(dir), 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
		return err
	}
	return devicefiles.MkdirPrivate(dir)
}

// watchLocal tells whether both ends of a command's connection to the
// Device Mux watch it for its end (liveness.Conn.WatchAnswers): on Windows
// they do, since there the end that reads a Unix socket can miss the other
// end's close when it is writing itself as the close comes, and then waits
// for ever (docs/subsystems/release.md, "The windows job").
const watchLocal = true

// sameUser admits every connection to the Device Mux's socket on Windows,
// which gives a Unix socket no peer credentials: the runtime directory,
// private to this user, is the only gate, since connecting to a socket
// takes write access to its file (ADR 0051).
func sameUser(net.Conn) error {
	return nil
}

// slept returns how long the device slept between two looks at the clock,
// last and now, which a ticker set every apart: how much longer the gap
// was. Go's monotonic clock on Windows is the interrupt time
// (KUSER_SHARED_DATA's InterruptTime, which the runtime's nanotime reads
// as it is), and that runs on while Windows sleeps, as the unbiased
// interrupt time does not; the wall clock moves no further than it, so the
// gap alone tells a sleep.
func slept(last, now time.Time, every time.Duration) time.Duration {
	return now.Sub(last) - every
}
